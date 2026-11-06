// Spiegelt die JSON-Ausgabe von github.com/Ghardo/sane (Felder ohne JSON-Tags)

export const OptType = {
  Bool: 0,
  Int: 1,
  Fixed: 2,
  String: 3,
  Button: 4,
} as const

export const Unit = {
  None: 0,
  Pixel: 1,
  Bit: 2,
  Mm: 3,
  Dpi: 4,
  Percent: 5,
  Usec: 6,
} as const

export interface SaneRange {
  Min: number
  Max: number
  Quant: number
}

export interface SaneOption {
  Name: string
  Group: string
  Title: string
  Desc: string
  Type: number
  Unit: number
  Length: number
  ConstrSet: (number | string)[] | null
  ConstrRange: SaneRange | null
  IsActive: boolean
  IsSettable: boolean
  IsDetectable: boolean
  IsAutomatic: boolean
  IsEmulated: boolean
  IsAdvanced: boolean
}

export interface Device {
  Name: string
  Vendor: string
  Model: string
  Type: string
}

export type OptionValue = boolean | number | string
export type OptionValues = Record<string, OptionValue>

export interface ScanRequest {
  Device: string
  WatchSensor?: string
  Filename?: string
  NewDoc?: boolean
  Options: OptionValues
}

export interface ScanResult {
  filename: string
  width?: number
  height?: number
  message?: string
}

export interface WatcherStatus {
  config: ScanRequest
  active: boolean
}
