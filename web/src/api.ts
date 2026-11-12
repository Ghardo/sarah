import { t } from './i18n'
import type { Device, PostProcess, PostProcessInput, SaneOption, ScanRequest, ScanResult, WatcherStatus } from './types'

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, t('errors.unreachable'))
  }

  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }

  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`
    if (data && typeof data === 'object') {
      const d = data as { error?: unknown; message?: unknown }
      msg = String(d.error ?? d.message ?? msg)
    }
    throw new ApiError(res.status, msg)
  }
  return data as T
}

const isNotFound = (e: unknown) => e instanceof ApiError && e.status === 404

export const api = {
  version: () => request<{ version: string }>('GET', '/version'),

  async devices(): Promise<Device[]> {
    try {
      return await request<Device[]>('GET', '/list')
    } catch (e) {
      if (isNotFound(e)) return [] // "No devices found."
      throw e
    }
  },

  options: (device: string) =>
    request<SaneOption[]>('GET', `/config?device=${encodeURIComponent(device)}`),

  scan: (req: ScanRequest) => request<ScanResult>('POST', '/scan', req),

  async watchers(): Promise<WatcherStatus[]> {
    const res = await request<{ active_watchers: WatcherStatus[] | null }>('GET', '/watch/list')
    return res.active_watchers ?? []
  },

  saveWatcher: (req: ScanRequest) => request<{ message: string }>('POST', '/watch', req),

  async deleteWatcher(device: string, sensor: string): Promise<void> {
    try {
      await request('DELETE', '/watch', { Device: device, WatchSensor: sensor })
    } catch (e) {
      if (!isNotFound(e)) throw e
    }
  },

  async deleteAllWatchers(): Promise<void> {
    try {
      await request('DELETE', '/watch/all')
    } catch (e) {
      if (!isNotFound(e)) throw e
    }
  },

  /** Preview of the whole bed at a low resolution, without a file or post-scan script. */
  async preview(req: ScanRequest): Promise<Blob> {
    let res: Response
    try {
      res = await fetch('/preview', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(req),
      })
    } catch {
      throw new ApiError(0, t('errors.unreachable'))
    }
    if (!res.ok) {
      let msg = `${res.status} ${res.statusText}`
      try {
        const d = (await res.json()) as { error?: unknown }
        if (d.error) msg = String(d.error)
      } catch {
        // not JSON
      }
      throw new ApiError(res.status, msg)
    }
    return res.blob()
  },

  async postProcesses(): Promise<{ processes: PostProcess[]; dir: string }> {
    const res = await request<{ processes: PostProcess[] | null; dir?: string }>('GET', '/post-process')
    return { processes: res.processes ?? [], dir: res.dir ?? '' }
  },
  createPostProcess: (p: PostProcessInput) => request<PostProcess>('POST', '/post-process', p),
  updatePostProcess: (id: string, p: PostProcessInput) =>
    request<PostProcess>('PUT', `/post-process/${encodeURIComponent(id)}`, p),
  deletePostProcess: (id: string) => request<unknown>('DELETE', `/post-process/${encodeURIComponent(id)}`),
  postProcessFileUrl: (id: string) => `/post-process/${encodeURIComponent(id)}/file`,

  /** Uploads the file of a post-process, replacing the existing one. */
  async uploadPostProcess(id: string, file: File): Promise<PostProcess> {
    const form = new FormData()
    form.append('file', file, file.name)
    let res: Response
    try {
      res = await fetch(`/post-process/${encodeURIComponent(id)}/file`, { method: 'PUT', body: form })
    } catch {
      throw new ApiError(0, t('errors.unreachable'))
    }
    const data = (await res.json().catch(() => null)) as (PostProcess & { error?: string }) | null
    if (!res.ok) throw new ApiError(res.status, data?.error ?? `${res.status} ${res.statusText}`)
    return data as PostProcess
  },

  /** Last scan from sarah's memory (independent of the post-scan script). */
  lastPreviewUrl: () => `/preview/last?t=${Date.now()}`,
}

// A device's options do not change at runtime, hence a simple cache
const optionsCache = new Map<string, Promise<SaneOption[]>>()

export function loadOptions(device: string, force = false): Promise<SaneOption[]> {
  let p = optionsCache.get(device)
  if (force || !p) {
    p = api.options(device)
    optionsCache.set(device, p)
    p.catch(() => optionsCache.delete(device))
  }
  return p
}

export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}
