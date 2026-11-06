// localStorage kann in privaten Fenstern o. Ä. fehlen oder werfen – dann einfach ohne merken.

const prefix = 'sarah:'

export function load<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(prefix + key)
    return raw === null ? fallback : (JSON.parse(raw) as T)
  } catch {
    return fallback
  }
}

export function save(key: string, value: unknown): void {
  try {
    localStorage.setItem(prefix + key, JSON.stringify(value))
  } catch {
    // ignorieren
  }
}
