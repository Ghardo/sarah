package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// --- Post-Processes ---
//
// After every scan a post-process can run: any executable file (shell, Ruby or
// Python script with a shebang, or a binary). All of them live in
// SARAH_POST_PROCESS_DIR (default: ./post-process), their metadata is stored in
// manifest.json in the same directory:
//
//   {
//     "processes": [
//       { "id": "3f9a1c2b", "name": "PDF", "description": "…", "default": true, "file": "post-process.sh" }
//     ]
//   }
//
// At most one entry is the default. Scans and watchers pick exactly one via the
// field PostProcess:
//   ""      the default process (if one is marked)
//   "none"  no post-process
//   <id>    this process
//
// Dependencies have to be checked or bundled by the file itself.
//
//   GET    /post-process              list
//   POST   /post-process              create  {name, description, default}
//   PUT    /post-process/:id          update  {name, description, default}
//   DELETE /post-process/:id          delete (including the file)
//   GET    /post-process/:id/file     download the file
//   PUT    /post-process/:id/file     upload the file (multipart, field "file"), overwrites

const (
	postProcessManifest = "manifest.json"
	postProcessNone     = "none"
	// Output sent along with the post-process-finished event
	postProcessEventOutput = 4000
)

type PostProcess struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
	File        string `json:"file,omitempty"`
}

type postProcessFile struct {
	Processes []PostProcess `json:"processes"`
}

// postProcessInfo is a list entry including details about the file.
type postProcessInfo struct {
	PostProcess
	Size       int64      `json:"size,omitempty"`
	Modified   *time.Time `json:"modified,omitempty"`
	Executable bool       `json:"executable"`
	Missing    bool       `json:"missing,omitempty"` // listed in the manifest but not present
}

var (
	postProcessDir string
	// Protects manifest.json and the files during changes via the API
	postProcessMutex sync.Mutex
	// Post-processes run one after another, so that e.g. scripts merging pages
	// don't get in each other's way
	postProcessRunMutex sync.Mutex
)

// initPostProcesses creates the directory and checks the manifest.
func initPostProcesses(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	postProcessDir = abs
	if err := os.MkdirAll(abs, 0755); err != nil {
		return fmt.Errorf("could not create post-process directory %s: %w", abs, err)
	}
	if os.Getenv("SARAH_POST_SCAN_PROCESS") != "" {
		log.Printf("Note: SARAH_POST_SCAN_PROCESS is no longer used, post-processes are managed in %s", abs)
	}

	postProcessMutex.Lock()
	m, err := loadPostProcesses()
	postProcessMutex.Unlock()
	if err != nil {
		// Not fatal: scans keep working, just without post-process
		log.Printf("ERROR: %v", err)
		return nil
	}
	log.Printf("Post-processes: %d configured in %s", len(m.Processes), abs)
	return nil
}

func newPostProcessID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

func loadPostProcesses() (postProcessFile, error) {
	var m postProcessFile
	data, err := os.ReadFile(filepath.Join(postProcessDir, postProcessManifest))
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return m, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("invalid %s: %w", postProcessManifest, err)
	}
	return m, nil
}

func savePostProcesses(m postProcessFile) error {
	if m.Processes == nil {
		m.Processes = []PostProcess{}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(postProcessDir, postProcessManifest)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// cleanPostProcessFilename validates a file name from an upload.
func cleanPostProcessFilename(name string) (string, error) {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
	switch {
	case name == "" || name == "." || name == "..":
		return "", errors.New("invalid file name")
	case name == postProcessManifest || strings.HasPrefix(name, postProcessManifest+"."):
		return "", fmt.Errorf("file name %q is reserved", name)
	case strings.HasPrefix(name, "."):
		return "", errors.New("file name must not start with a dot")
	}
	return name, nil
}

// writePostProcessFile writes the file atomically and makes it executable.
func writePostProcessFile(name string, r io.Reader) error {
	path := filepath.Join(postProcessDir, name)
	tmp, err := os.CreateTemp(postProcessDir, ".upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func fileUsedByOther(m postProcessFile, file, exceptID string) bool {
	for _, p := range m.Processes {
		if p.ID != exceptID && p.File == file {
			return true
		}
	}
	return false
}

func findPostProcess(m postProcessFile, id string) int {
	for i, p := range m.Processes {
		if p.ID == id {
			return i
		}
	}
	return -1
}

// setDefault makes sure that at most idx is the default.
func setDefault(m *postProcessFile, idx int) {
	for i := range m.Processes {
		m.Processes[i].Default = i == idx
	}
}

// --- Selection and execution ---

// resolvePostProcess returns the process for a selection of a scan or watcher (nil = none).
func resolvePostProcess(sel string) *PostProcess {
	sel = strings.TrimSpace(sel)
	if sel == postProcessNone {
		return nil
	}
	postProcessMutex.Lock()
	m, err := loadPostProcesses()
	postProcessMutex.Unlock()
	if err != nil {
		log.Printf("ERROR: could not load post-processes: %v", err)
		return nil
	}
	for _, p := range m.Processes {
		if (sel == "" && p.Default) || (sel != "" && p.ID == sel) {
			p := p
			return &p
		}
	}
	if sel != "" {
		log.Printf("WARN: post-process '%s' does not exist (anymore), skipping", sel)
	}
	return nil
}

// postProcessJob describes the scan a post-process runs for.
type postProcessJob struct {
	ScanDir  string
	Filename string
	Format   string
	Source   string // "api" or "button"
	Device   string
	Sensor   string
	Doc      int
	Index    int
	NewDoc   bool
}

// runPostProcess runs the selected process (blocking, one after another).
func runPostProcess(sel string, job postProcessJob) {
	p := resolvePostProcess(sel)
	if p == nil {
		return
	}
	if p.File == "" {
		log.Printf("WARN: post-process '%s' has no file, skipping", p.Name)
		publishPostProcessResult(p, job, errors.New("no file uploaded"), "")
		return
	}

	postProcessRunMutex.Lock()
	defer postProcessRunMutex.Unlock()

	path := filepath.Join(postProcessDir, p.File)
	// Files copied by hand (scp, git) are often not executable
	if st, err := os.Stat(path); err == nil && st.Mode().Perm()&0100 == 0 {
		if err := os.Chmod(path, st.Mode().Perm()|0755); err != nil {
			log.Printf("WARN: could not make %s executable: %v", path, err)
		}
	}
	log.Printf("Running post-process '%s' (%s) for %s", p.Name, path, job.Filename)

	scanDir, _ := filepath.Abs(job.ScanDir)
	newDoc := "0"
	if job.NewDoc {
		newDoc = "1"
	}

	cmd := exec.Command(path)
	cmd.Dir = postProcessDir
	cmd.Env = append(os.Environ(),
		"SARAH_SCAN_DIR="+scanDir,
		"SARAH_SCAN_FILENAME="+job.Filename,
		"SARAH_SCAN_PATH="+filepath.Join(scanDir, job.Filename),
		"SARAH_SCAN_FORMAT="+job.Format,
		"SARAH_SCAN_SOURCE="+job.Source,
		"SARAH_SCAN_DEVICE="+job.Device,
		"SARAH_SCAN_SENSOR="+job.Sensor,
		"SARAH_SCAN_DOC="+strconv.Itoa(job.Doc),
		"SARAH_SCAN_INDEX="+strconv.Itoa(job.Index),
		"SARAH_SCAN_NEW_DOC="+newDoc,
		"SARAH_POST_PROCESS_ID="+p.ID,
		"SARAH_POST_PROCESS_NAME="+p.Name,
		"SARAH_POST_PROCESS_DIR="+postProcessDir,
		"SARAH_VERSION="+version,
	)

	output, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(output))
	logOut := outStr
	if logOut == "" {
		logOut = "<no output>"
	}
	if err != nil {
		log.Printf("ERROR: post-process '%s' failed: %v. Output: %s", p.Name, err, logOut)
	} else {
		log.Printf("Post-process '%s' finished successfully. Output: %s", p.Name, logOut)
	}
	publishPostProcessResult(p, job, err, outStr)
}

func publishPostProcessResult(p *PostProcess, job postProcessJob, err error, output string) {
	if len(output) > postProcessEventOutput {
		output = "…" + output[len(output)-postProcessEventOutput:]
	}
	data := gin.H{"id": p.ID, "name": p.Name, "filename": job.Filename, "ok": err == nil, "output": output}
	if err != nil {
		data["error"] = err.Error()
	}
	events.publish("post-process-finished", data)
}

// --- HTTP ---

func registerPostProcessRoutes(router *gin.Engine) {
	router.GET("/post-process", listPostProcesses)
	router.POST("/post-process", createPostProcess)
	router.PUT("/post-process/:id", updatePostProcess)
	router.DELETE("/post-process/:id", deletePostProcess)
	router.GET("/post-process/:id/file", downloadPostProcess)
	router.PUT("/post-process/:id/file", uploadPostProcess)
}

type postProcessInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
}

func (in *postProcessInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func postProcessChanged() {
	events.publish("post-processes-changed", gin.H{})
}

func listPostProcesses(c *gin.Context) {
	postProcessMutex.Lock()
	m, err := loadPostProcesses()
	postProcessMutex.Unlock()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	list := make([]postProcessInfo, 0, len(m.Processes))
	for _, p := range m.Processes {
		info := postProcessInfo{PostProcess: p}
		if p.File != "" {
			if st, err := os.Stat(filepath.Join(postProcessDir, p.File)); err == nil {
				mod := st.ModTime()
				info.Size = st.Size()
				info.Modified = &mod
				info.Executable = st.Mode().Perm()&0111 != 0
			} else {
				info.Missing = true
			}
		}
		list = append(list, info)
	}
	c.JSON(http.StatusOK, gin.H{"processes": list, "dir": postProcessDir})
}

func createPostProcess(c *gin.Context) {
	var in postProcessInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}
	if err := in.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	postProcessMutex.Lock()
	defer postProcessMutex.Unlock()
	m, err := loadPostProcesses()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p := PostProcess{ID: newPostProcessID(), Name: in.Name, Description: in.Description}
	m.Processes = append(m.Processes, p)
	if in.Default {
		setDefault(&m, len(m.Processes)-1)
	}
	if err := savePostProcesses(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	postProcessChanged()
	c.JSON(http.StatusCreated, m.Processes[len(m.Processes)-1])
}

func updatePostProcess(c *gin.Context) {
	var in postProcessInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}
	if err := in.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	postProcessMutex.Lock()
	defer postProcessMutex.Unlock()
	m, err := loadPostProcesses()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	i := findPostProcess(m, c.Param("id"))
	if i < 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "post-process not found"})
		return
	}
	m.Processes[i].Name = in.Name
	m.Processes[i].Description = in.Description
	if in.Default {
		setDefault(&m, i)
	} else {
		m.Processes[i].Default = false
	}
	if err := savePostProcesses(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	postProcessChanged()
	c.JSON(http.StatusOK, m.Processes[i])
}

func deletePostProcess(c *gin.Context) {
	postProcessMutex.Lock()
	defer postProcessMutex.Unlock()
	m, err := loadPostProcesses()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	id := c.Param("id")
	i := findPostProcess(m, id)
	if i < 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "post-process not found"})
		return
	}
	p := m.Processes[i]
	m.Processes = append(m.Processes[:i], m.Processes[i+1:]...)
	if err := savePostProcesses(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if p.File != "" && !fileUsedByOther(m, p.File, id) {
		if err := os.Remove(filepath.Join(postProcessDir, p.File)); err != nil && !os.IsNotExist(err) {
			log.Printf("WARN: could not remove post-process file %s: %v", p.File, err)
		}
	}
	postProcessChanged()
	c.JSON(http.StatusOK, gin.H{"message": "post-process deleted"})
}

func downloadPostProcess(c *gin.Context) {
	postProcessMutex.Lock()
	m, err := loadPostProcesses()
	postProcessMutex.Unlock()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	i := findPostProcess(m, c.Param("id"))
	if i < 0 || m.Processes[i].File == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no file"})
		return
	}
	path := filepath.Join(postProcessDir, m.Processes[i].File)
	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.FileAttachment(path, m.Processes[i].File)
}

func uploadPostProcess(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "multipart field 'file' is required"})
		return
	}
	name, err := cleanPostProcessFilename(fh.Filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	postProcessMutex.Lock()
	defer postProcessMutex.Unlock()
	m, err := loadPostProcesses()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	id := c.Param("id")
	i := findPostProcess(m, id)
	if i < 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "post-process not found"})
		return
	}
	if fileUsedByOther(m, name, id) {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("file name %q is already used by another post-process", name)})
		return
	}

	src, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer src.Close()
	if err := writePostProcessFile(name, src); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Remove the old file if it had a different name
	old := m.Processes[i].File
	m.Processes[i].File = name
	if err := savePostProcesses(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if old != "" && old != name && !fileUsedByOther(m, old, id) {
		os.Remove(filepath.Join(postProcessDir, old))
	}
	log.Printf("Post-process '%s': uploaded %s (%d bytes)", m.Processes[i].Name, name, fh.Size)
	postProcessChanged()
	c.JSON(http.StatusOK, m.Processes[i])
}
