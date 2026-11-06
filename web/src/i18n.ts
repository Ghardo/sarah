import { reactive, ref } from 'vue'
import { load, save } from './storage'
// English is built in: fallback for missing keys and in case sarah does not respond
import en from '../../src/locales/en.json'

// Languages come from sarah at runtime (GET /i18n, GET /i18n/:code): built-in ones
// from src/locales/ plus files from SARAH_LOCALES_DIR on the server. A new
// language therefore only needs a new file there – no rebuild.

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
    return code === fallbackCode // English is built in as a last resort
  }
}

function apply(code: string) {
  locale.value = code
  document.documentElement.lang = code
}

/** Switches the language and remembers it. */
export async function setLocale(code: string): Promise<void> {
  if (await loadCatalog(code)) {
    apply(code)
    save('locale', code)
  }
}

/** Loads the available languages and the matching language (saved or from the browser). */
export async function initI18n(): Promise<void> {
  try {
    const list = await fetchJson<LocaleInfo[]>('/i18n')
    if (Array.isArray(list) && list.length) locales.value = list
  } catch {
    // without a server list, English stays
  }

  const candidates = [load<string | null>('locale', null), ...(navigator.languages ?? [navigator.language])]
  for (const c of candidates) {
    const code = c && resolve(c)
    if (code && (await loadCatalog(code))) {
      apply(code)
      break
    }
  }
  // current English texts from the server (override the built-in ones if customized)
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
 * Translates a key like "scan.title". Parameters replace {name} in the text;
 * placeholders without a matching parameter stay as they are (e.g. {doc} in help texts).
 */
export function t(key: string, params?: Record<string, string | number>): string {
  const text = lookup(catalogs[locale.value], key) ?? lookup(catalogs[fallbackCode], key) ?? lookup(en, key) ?? key
  if (!params) return text
  return text.replace(/\{(\w+)\}/g, (m, name: string) => (name in params ? String(params[name]) : m))
}
