<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, errorMessage } from '../api'
import { t } from '../i18n'
import { loadPostProcesses, postProcesses } from '../postprocess'
import type { PostProcess } from '../types'

// --- Editor ---

interface Editor {
  id: string | null // null = new
  name: string
  description: string
  isDefault: boolean
  /** New entries: file uploaded after creating the entry */
  pendingFile: File | null
}

const editor = ref<Editor | null>(null)
const saving = ref(false)
const uploading = ref(false)
const error = ref('')
const editorError = ref('')
const fileInput = ref<HTMLInputElement | null>(null)

const current = computed(() =>
  editor.value?.id ? postProcesses.list.find((p) => p.id === editor.value!.id) : undefined,
)

function startNew() {
  editorError.value = ''
  editor.value = {
    id: null,
    name: '',
    description: '',
    isDefault: !postProcesses.list.length,
    pendingFile: null,
  }
}

function edit(p: PostProcess) {
  editorError.value = ''
  editor.value = {
    id: p.id,
    name: p.name,
    description: p.description ?? '',
    isDefault: !!p.default,
    pendingFile: null,
  }
}

async function saveEditor() {
  const ed = editor.value
  if (!ed || !ed.name.trim()) return
  saving.value = true
  editorError.value = ''
  try {
    const input = { name: ed.name.trim(), description: ed.description.trim(), default: ed.isDefault }
    if (ed.id) {
      await api.updatePostProcess(ed.id, input)
    } else {
      const created = await api.createPostProcess(input)
      ed.id = created.id
      if (ed.pendingFile) await api.uploadPostProcess(created.id, ed.pendingFile)
    }
    editor.value = null
    await loadPostProcesses()
  } catch (e) {
    editorError.value = errorMessage(e)
    await loadPostProcesses()
  } finally {
    saving.value = false
  }
}

async function remove(p: PostProcess) {
  if (!confirm(t('postprocess.confirmDelete', { name: p.name }))) return
  try {
    await api.deletePostProcess(p.id)
    if (editor.value?.id === p.id) editor.value = null
    await loadPostProcesses()
  } catch (e) {
    error.value = errorMessage(e)
  }
}

// --- File ---

function pickFile() {
  fileInput.value?.click()
}

async function onFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  const ed = editor.value
  if (!file || !ed) return

  if (!ed.id) {
    ed.pendingFile = file
    return
  }
  if (current.value?.file && !confirm(t('postprocess.confirmOverwrite', { file: current.value.file }))) return
  uploading.value = true
  editorError.value = ''
  try {
    await api.uploadPostProcess(ed.id, file)
    await loadPostProcesses()
  } catch (err) {
    editorError.value = errorMessage(err)
  } finally {
    uploading.value = false
  }
}

function formatSize(n?: number): string {
  if (!n) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}
function formatDate(s?: string): string {
  return s ? new Date(s).toLocaleString() : ''
}

// --- Explanation ---

const envVars = [
  'SARAH_SCAN_DIR',
  'SARAH_SCAN_FILENAME',
  'SARAH_SCAN_PATH',
  'SARAH_SCAN_FORMAT',
  'SARAH_SCAN_SOURCE',
  'SARAH_SCAN_DEVICE',
  'SARAH_SCAN_SENSOR',
  'SARAH_SCAN_DOC',
  'SARAH_SCAN_INDEX',
  'SARAH_SCAN_NEW_DOC',
  'SARAH_POST_PROCESS_ID',
  'SARAH_POST_PROCESS_NAME',
  'SARAH_POST_PROCESS_DIR',
  'SARAH_VERSION',
]
</script>

<template>
  <div class="stack">
    <div class="layout-two">
      <section class="card">
        <header class="card-head">
          <h2>{{ t('postprocess.title') }}</h2>
          <div class="actions">
            <button type="button" class="link-btn" @click="loadPostProcesses">{{ t('common.refresh') }}</button>
            <button type="button" class="primary" @click="startNew">{{ t('postprocess.new') }}</button>
          </div>
        </header>
        <p class="muted">{{ t('postprocess.intro') }}</p>

        <p v-if="error || postProcesses.error" class="alert">{{ error || postProcesses.error }}</p>
        <p v-else-if="!postProcesses.list.length" class="empty">{{ t('postprocess.empty') }}</p>

        <ul class="watcher-list">
          <li
            v-for="p in postProcesses.list"
            :key="p.id"
            class="watcher"
            :class="{ selected: editor?.id === p.id }"
          >
            <div class="watcher-main">
              <div class="watcher-title">
                <span class="key">{{ p.name }}</span>
                <span v-if="p.default" class="badge ok">{{ t('postprocess.default') }}</span>
                <span v-if="!p.file" class="badge">{{ t('postprocess.noFile') }}</span>
                <span v-else-if="p.missing" class="badge error">{{ t('postprocess.missing') }}</span>
              </div>
              <div v-if="p.description" class="watcher-sub pp-desc">{{ p.description }}</div>
              <div v-if="p.file" class="watcher-sub">
                <code>{{ p.file }}</code>
                <template v-if="!p.missing"> · {{ formatSize(p.size) }} · {{ formatDate(p.modified) }}</template>
              </div>
            </div>
            <div class="watcher-actions">
              <button type="button" @click="edit(p)">{{ t('common.edit') }}</button>
              <button type="button" class="danger" @click="remove(p)">{{ t('common.delete') }}</button>
            </div>
          </li>
        </ul>
      </section>

      <section v-if="editor" class="card">
        <header class="card-head">
          <h2>{{ editor.id ? t('postprocess.edit') : t('postprocess.new') }}</h2>
          <button type="button" class="icon-btn" :aria-label="t('common.close')" @click="editor = null">×</button>
        </header>

        <form class="editor" @submit.prevent="saveEditor">
          <div class="fields">
            <div class="field wide">
              <label for="pp-name">{{ t('postprocess.name') }}</label>
              <div class="field-input">
                <input id="pp-name" v-model="editor.name" type="text" required />
              </div>
            </div>
            <div class="field wide">
              <label for="pp-desc">{{ t('postprocess.description') }}</label>
              <div class="field-input">
                <textarea id="pp-desc" v-model="editor.description" rows="3"></textarea>
              </div>
            </div>
            <label class="check">
              <input v-model="editor.isDefault" type="checkbox" />
              {{ t('postprocess.isDefault') }}
            </label>

            <div class="field wide">
              <span class="label">{{ t('postprocess.file') }}</span>
              <div class="pp-file">
                <template v-if="editor.id && current?.file">
                  <code>{{ current.file }}</code>
                  <span v-if="current.missing" class="hint error">{{ t('postprocess.missing') }}</span>
                  <span v-else class="hint">{{ formatSize(current.size) }} · {{ formatDate(current.modified) }}</span>
                </template>
                <template v-else-if="editor.pendingFile">
                  <code>{{ editor.pendingFile.name }}</code>
                  <span class="hint">{{ t('postprocess.pending') }}</span>
                </template>
                <span v-else class="muted">{{ t('postprocess.noFile') }}</span>
              </div>
              <div class="pp-file-actions">
                <a
                  v-if="editor.id && current?.file && !current.missing"
                  class="button"
                  :href="api.postProcessFileUrl(editor.id)"
                  :download="current.file"
                >
                  {{ t('postprocess.download') }}
                </a>
                <button v-else type="button" disabled>{{ t('postprocess.download') }}</button>
                <button type="button" :disabled="uploading" @click="pickFile">
                  <span v-if="uploading" class="spinner" aria-hidden="true"></span>
                  {{ t('postprocess.upload') }}
                </button>
                <input ref="fileInput" type="file" hidden @change="onFile" />
              </div>
              <small class="hint">{{ t('postprocess.uploadHint') }}</small>
            </div>
          </div>

          <p v-if="editorError" class="alert">{{ editorError }}</p>

          <div class="form-actions">
            <button type="button" @click="editor = null">{{ t('common.cancel') }}</button>
            <button type="submit" class="primary" :disabled="saving || !editor.name.trim()">
              {{ saving ? t('common.saving') : t('common.save') }}
            </button>
          </div>
        </form>
      </section>
    </div>

    <section class="card pp-help">
      <h2>{{ t('postprocess.help.title') }}</h2>
      <p>{{ t('postprocess.help.run') }}</p>
      <p>
        {{ t('postprocess.help.dir') }}
        <code v-if="postProcesses.dir">{{ postProcesses.dir }}</code>
      </p>
      <table class="options-table">
        <thead>
          <tr>
            <th>{{ t('postprocess.help.variable') }}</th>
            <th>{{ t('postprocess.help.meaning') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="v in envVars" :key="v">
            <td><code>{{ v }}</code></td>
            <td>{{ t(`postprocess.env.${v}`) }}</td>
          </tr>
        </tbody>
      </table>
      <ul class="pp-notes">
        <li>{{ t('postprocess.help.inherit') }}</li>
        <li>{{ t('postprocess.help.cwd') }}</li>
        <li>{{ t('postprocess.help.exit') }}</li>
        <li>{{ t('postprocess.help.serial') }}</li>
        <li>{{ t('postprocess.help.deps') }}</li>
        <li>{{ t('postprocess.help.path') }}</li>
        <li>{{ t('postprocess.help.select') }}</li>
      </ul>
    </section>
  </div>
</template>
