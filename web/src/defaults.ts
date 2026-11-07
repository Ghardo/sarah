import { reactive } from 'vue'

// Defaults of sarah (GET /defaults, configurable via environment variables).
// Until the response arrives, or with older sarah versions, the built-in values apply.

export const defaults = reactive({
  /** SARAH_FILENAME_TEMPLATE */
  filenameTemplate: 'scan_{index}_{date}_{time}',
  /** SARAH_JPEG_QUALITY */
  jpegQuality: 85,
})

export async function loadDefaults(): Promise<void> {
  try {
    const res = await fetch('/defaults')
    if (!res.ok) return
    const d = (await res.json()) as Partial<typeof defaults>
    if (typeof d.filenameTemplate === 'string' && d.filenameTemplate) defaults.filenameTemplate = d.filenameTemplate
    if (typeof d.jpegQuality === 'number') defaults.jpegQuality = d.jpegQuality
  } catch {
    // sarah not reachable: keep the built-in values
  }
}
