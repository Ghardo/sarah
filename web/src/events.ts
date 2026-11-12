import { reactive } from 'vue'

export interface ScanInfo {
  id: number
  device: string
  source: 'api' | 'button' | 'preview'
  sensor?: string
  started: string
  filename?: string
  width?: number
  height?: number
  error?: string
}

export interface PostProcessResult {
  id: string
  name: string
  filename: string
  ok: boolean
  error?: string
  output?: string
}

export interface ButtonPress {
  device: string
  sensor: string
  watcher: boolean
  at: number
}

type Listener<T> = (data: T) => void

/** Live state from GET /events (Server-Sent Events). */
export const live = reactive({
  connected: false,
  scanning: [] as ScanInfo[],
  lastCompleted: null as ScanInfo | null,
  lastButton: null as ButtonPress | null,
})

const listeners = {
  completed: new Set<Listener<ScanInfo>>(),
  failed: new Set<Listener<ScanInfo>>(),
  button: new Set<Listener<ButtonPress>>(),
  watchers: new Set<Listener<void>>(),
  postProcesses: new Set<Listener<void>>(),
  postProcessFinished: new Set<Listener<PostProcessResult>>(),
}

/** Subscribes to an event, returns an unsubscribe function. */
export function onLive(kind: 'completed' | 'failed', fn: Listener<ScanInfo>): () => void
export function onLive(kind: 'button', fn: Listener<ButtonPress>): () => void
export function onLive(kind: 'watchers' | 'postProcesses', fn: () => void): () => void
export function onLive(kind: 'postProcessFinished', fn: Listener<PostProcessResult>): () => void
export function onLive(kind: keyof typeof listeners, fn: Listener<never>): () => void {
  const set = listeners[kind] as unknown as Set<Listener<never>>
  set.add(fn)
  return () => {
    set.delete(fn)
  }
}

function emit<T>(set: Set<Listener<T>>, data: T) {
  for (const fn of set) fn(data)
}

let source: EventSource | null = null

export function connectEvents(): void {
  if (source) return
  source = new EventSource('/events')

  source.onopen = () => (live.connected = true)
  // EventSource reconnects on its own
  source.onerror = () => (live.connected = false)

  const on = <T>(name: string, fn: (data: T) => void) =>
    source!.addEventListener(name, (e) => {
      try {
        fn(JSON.parse((e as MessageEvent<string>).data) as T)
      } catch {
        // ignore unknown format
      }
    })

  const removeScan = (id: number) => (live.scanning = live.scanning.filter((s) => s.id !== id))

  on<{ scanning: ScanInfo[] | null }>('hello', (d) => {
    live.connected = true
    live.scanning = d.scanning ?? []
  })
  on<ScanInfo>('scan-started', (s) => {
    removeScan(s.id)
    live.scanning = [...live.scanning, s]
  })
  on<ScanInfo>('scan-completed', (s) => {
    removeScan(s.id)
    // Preview scans are not a result, the image only goes to whoever started it
    if (s.source === 'preview') return
    live.lastCompleted = s
    emit(listeners.completed, s)
  })
  on<ScanInfo>('scan-failed', (s) => {
    removeScan(s.id)
    if (s.source === 'preview') return
    emit(listeners.failed, s)
  })
  on<Omit<ButtonPress, 'at'>>('button-pressed', (b) => {
    const press = { ...b, at: Date.now() }
    live.lastButton = press
    emit(listeners.button, press)
  })
  on<unknown>('watchers-changed', () => emit(listeners.watchers, undefined))
  on<unknown>('post-processes-changed', () => emit(listeners.postProcesses, undefined))
  on<PostProcessResult>('post-process-finished', (r) => emit(listeners.postProcessFinished, r))
}
