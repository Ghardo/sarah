import { reactive } from 'vue'
import { api } from './api'
import { onLive } from './events'
import type { PostProcess } from './types'

// Post-processes of sarah (GET /post-process), shared by all views.
// Reloaded automatically on "post-processes-changed".

export const NONE = 'none'

export const postProcesses = reactive({
  list: [] as PostProcess[],
  dir: '',
  loaded: false,
  error: '',
})

export async function loadPostProcesses(): Promise<void> {
  try {
    const res = await api.postProcesses()
    postProcesses.list = res.processes
    postProcesses.dir = res.dir
    postProcesses.error = ''
  } catch (e) {
    postProcesses.error = e instanceof Error ? e.message : String(e)
  } finally {
    postProcesses.loaded = true
  }
}

onLive('postProcesses', () => {
  loadPostProcesses()
})

export function defaultPostProcess(): PostProcess | undefined {
  return postProcesses.list.find((p) => p.default)
}

/**
 * Valid selection for a scan or watcher: the saved one if it still exists,
 * otherwise the default or "none". Without any processes "" (= send nothing).
 */
export function validSelection(sel: string | undefined): string {
  if (!postProcesses.list.length) return ''
  if (sel === NONE) return NONE
  if (sel && postProcesses.list.some((p) => p.id === sel)) return sel
  return defaultPostProcess()?.id ?? NONE
}

/** Display name of a selection ("" = default), empty for no process. */
export function postProcessName(sel: string | undefined): string {
  if (sel === NONE) return ''
  const p = sel ? postProcesses.list.find((x) => x.id === sel) : defaultPostProcess()
  return p?.name ?? ''
}
