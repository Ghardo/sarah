import { reactive, ref } from 'vue'
import { load, save } from './storage'
// Englisch ist fest eingebaut: Fallback für fehlende Schlüssel und falls sarah nicht antwortet
import en from '../../src/locales/en.json'

// Sprachen kommen zur Laufzeit von sarah (GET /i18n, GET /i18n/:code): eingebaute
// aus src/locales/ plus Dateien aus SARAH_LOCALES_DIR auf dem Server. Eine neue
// Sprache braucht also nur eine neue Datei dort – kein Neubau.

type Messages = { [key: string]: string | Messages }

export interface LocaleInfo {
  code: string
  name: string
}

const fallbackCode = 'en'

export const locales = ref<LocaleInfo[]>([{ code: fallbackCode, name: en._name }])
export const locale = ref(fallbackCode)

const catalogs = reactive<Record<string, Messages>>({ [fallbackCode]: en })

function resolve(code: string): string | undefined {
  const c = code.toLowerCase()
  const exact = locales.value.find((l) => l.code.toLowerCase() === c)
  if (exact) return exact.code
  const base = c.split('-')[0]
  return locales.value.find((l) => l.code.toLowerCase() === base)?.code
}

async function fetchJson<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return (await res.json()) as T
}

async function loadCatalog(code: string): Promise<boolean> {
  if (catalogs[code] && code !== fallbackCode) return true
  try {
    catalogs[code] = await fetchJson<Messages>(`/i18n/${encodeURIComponent(code)}`)
    return true
  } catch {
    return code === fallbackCode // Englisch ist zur Not eingebaut
  }
}

function apply(code: string) {
  locale.value = code
  document.documentElement.lang = code
}

/** Wechselt die Sprache und merkt sie sich. */
export async function setLocale(code: string): Promise<void> {
  if (await loadCatalog(code)) {
    apply(code)
    save('locale', code)
  }
}

/** Lädt die verfügbaren Sprachen und die passende Sprache (gespeichert oder vom Browser). */
export async function initI18n(): Promise<void> {
  try {
    const list = await fetchJson<LocaleInfo[]>('/i18n')
    if (Array.isArray(list) && list.length) locales.value = list
  } catch {
    // ohne Server-Liste bleibt Englisch
  }

  const candidates = [load<string | null>('locale', null), ...(navigator.languages ?? [navigator.language])]
  for (const c of candidates) {
    const code = c && resolve(c)
    if (code && (await loadCatalog(code))) {
      apply(code)
      break
    }
  }
  // aktuelle englische Texte vom Server (überschreibt die eingebauten, falls angepasst)
  if (locale.value !== fallbackCode) loadCatalog(fallbackCode)
}

function lookup(messages: Messages | undefined, key: string): string | undefined {
  let node: string | Messages | undefined = messages
  for (const part of key.split('.')) {
    if (!node || typeof node === 'string') return undefined
    node = node[part]
  }
  return typeof node === 'string' ? node : undefined
}

/**
 * Übersetzt einen Schlüssel wie "scan.title". Parameter ersetzen {name} im Text;
 * Platzhalter ohne passenden Parameter bleiben stehen (z. B. {doc} in Hilfetexten).
 */
export function t(key: string, params?: Record<string, string | number>): string {
  const text = lookup(catalogs[locale.value], key) ?? lookup(catalogs[fallbackCode], key) ?? lookup(en, key) ?? key
  if (!params) return text
  return text.replace(/\{(\w+)\}/g, (m, name: string) => (name in params ? String(params[name]) : m))
}
