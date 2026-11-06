import { t } from './i18n'
import { OptType, Unit, type OptionValue, type OptionValues, type SaneOption } from './types'

const valueTypes: number[] = [OptType.Bool, OptType.Int, OptType.Fixed, OptType.String]

/** Optionen, die sich für einen Scan sinnvoll setzen lassen. */
export function isScanOption(o: SaneOption): boolean {
  return (
    o.IsActive &&
    o.IsSettable &&
    o.Length === 1 && // Arrays (z. B. Gamma-Tabellen) lassen wir weg
    o.Group !== 'Sensors' &&
    valueTypes.includes(o.Type)
  )
}

/** Hardware-Tasten, die der Watcher überwachen kann (wie in globalWatcherLoop). */
export function sensorOptions(opts: SaneOption[]): SaneOption[] {
  return opts.filter(
    (o) =>
      o.Group === 'Sensors' &&
      o.IsActive &&
      o.IsDetectable &&
      (o.Type === OptType.Bool || o.Type === OptType.Int),
  )
}

export function groupOptions(opts: SaneOption[]): { group: string; options: SaneOption[] }[] {
  const groups: { group: string; options: SaneOption[] }[] = []
  for (const o of opts) {
    const name = o.Group || t('options.general')
    let g = groups.find((x) => x.group === name)
    if (!g) {
      g = { group: name, options: [] }
      groups.push(g)
    }
    g.options.push(o)
  }
  return groups
}

export function unitLabel(unit: number): string {
  switch (unit) {
    case Unit.Pixel:
      return 'px'
    case Unit.Bit:
      return 'bit'
    case Unit.Mm:
      return 'mm'
    case Unit.Dpi:
      return 'dpi'
    case Unit.Percent:
      return '%'
    case Unit.Usec:
      return 'µs'
    default:
      return ''
  }
}

export function typeLabel(type: number): string {
  return ['bool', 'int', 'fixed', 'string', 'button', 'group'][type] ?? String(type)
}

export function formatNumber(n: number): string {
  return Number.isInteger(n) ? String(n) : n.toFixed(2).replace(/\.?0+$/, '')
}

export function formatValue(v: OptionValue, opt?: SaneOption): string {
  if (typeof v === 'boolean') return v ? t('options.on') : t('options.off')
  const s = typeof v === 'number' ? formatNumber(v) : v
  const unit = opt ? unitLabel(opt.Unit) : ''
  return unit ? `${s} ${unit}` : s
}

function allows(o: SaneOption, v: OptionValue): boolean {
  if (o.ConstrSet) return o.ConstrSet.includes(v as never)
  if (o.ConstrRange && typeof v === 'number') return v >= o.ConstrRange.Min && v <= o.ConstrRange.Max
  return true
}

// --- Papierformate (Scanbereich in mm) ---

export interface PaperSize {
  id: string
  /** fester Name oder Übersetzungsschlüssel (labelKey) */
  label?: string
  labelKey?: string
  width: number
  height: number
}

export const paperSizes: PaperSize[] = [
  { id: 'a4', label: 'A4', width: 210, height: 297 },
  { id: 'a5', label: 'A5', width: 148, height: 210 },
  { id: 'a6', label: 'A6', width: 105, height: 148 },
  { id: 'letter', label: 'Letter', width: 215.9, height: 279.4 },
  { id: 'max', labelKey: 'options.max', width: Infinity, height: Infinity },
]

export function paperLabel(size: PaperSize): string {
  return size.labelKey ? t(size.labelKey) : (size.label ?? size.id)
}

const geometryNames = ['tl-x', 'tl-y', 'br-x', 'br-y'] as const

function geometry(opts: SaneOption[]) {
  const found = geometryNames.map((n) => opts.find((o) => o.Name === n))
  if (found.some((o) => !o || o.Unit !== Unit.Mm || !o.ConstrRange || !isScanOption(o))) return null
  const [, , brX, brY] = found as SaneOption[]
  return { maxX: brX.ConstrRange!.Max, maxY: brY.ConstrRange!.Max }
}

export function hasGeometry(opts: SaneOption[]): boolean {
  return geometry(opts) !== null
}

export function paperValues(opts: SaneOption[], size: PaperSize): OptionValues | null {
  const g = geometry(opts)
  if (!g) return null
  return {
    'tl-x': 0,
    'tl-y': 0,
    'br-x': Math.min(size.width, g.maxX),
    'br-y': Math.min(size.height, g.maxY),
  }
}

export function matchingPaper(opts: SaneOption[], values: OptionValues): string | null {
  for (const size of paperSizes) {
    const pv = paperValues(opts, size)
    if (!pv) return null
    const same = geometryNames.every(
      (n) => typeof values[n] === 'number' && Math.abs((values[n] as number) - (pv[n] as number)) < 0.05,
    )
    if (same) return size.id
  }
  return null
}

/** Vorschlag für einen neuen Scan/Watcher: Farbe, 300 dpi, A4 – soweit das Gerät es kann. */
export function suggestedValues(opts: SaneOption[]): OptionValues {
  const v: OptionValues = {}
  const byName = new Map(opts.filter(isScanOption).map((o) => [o.Name, o]))
  const mode = byName.get('mode')
  if (mode && allows(mode, 'Color')) v.mode = 'Color'
  const res = byName.get('resolution')
  if (res && allows(res, 300)) v.resolution = 300
  Object.assign(v, paperValues(opts, paperSizes[0]) ?? {})
  return v
}

export const filenamePlaceholders = '{doc}, {index}, {date}, {time}'
