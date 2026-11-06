<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import ScanView from './components/ScanView.vue'
import WatchersView from './components/WatchersView.vue'
import DevicesView from './components/DevicesView.vue'
import { api, errorMessage, loadOptions } from './api'
import { connectEvents, live, onLive } from './events'
import { locale, locales, setLocale, t } from './i18n'
import { load, save } from './storage'
import type { Device, SaneOption } from './types'

// --- Tabs (in the URL hash so reloads/bookmarks work) ---

const tabs = ['scan', 'watchers', 'devices'] as const
type Tab = (typeof tabs)[number]

function readTab(): Tab {
  const h = location.hash.slice(1)
  return tabs.find((id) => id === h) ?? 'scan'
}
const tab = ref<Tab>(readTab())
const onHash = () => (tab.value = readTab())
window.addEventListener('hashchange', onHash)

// --- Devices and options ---

const version = ref('')
const devices = ref<Device[]>([])
const devicesLoading = ref(false)
const devicesError = ref('')
const device = ref(load('device', ''))

const options = ref<SaneOption[]>([])
const optionsLoading = ref(false)
const optionsError = ref('')

async function refreshDevices() {
  devicesLoading.value = true
  devicesError.value = ''
  try {
    devices.value = await api.devices()
    if (!devices.value.some((d) => d.Name === device.value)) device.value = devices.value[0]?.Name ?? ''
  } catch (e) {
    devicesError.value = errorMessage(e)
  } finally {
    devicesLoading.value = false
  }
}

let optionsRequest = 0
async function refreshOptions(force = false) {
  const current = device.value
  const req = ++optionsRequest
  optionsError.value = ''
  if (!current) {
    options.value = []
    return
  }
  optionsLoading.value = true
  try {
    const opts = await loadOptions(current, force)
    if (req === optionsRequest) options.value = opts
  } catch (e) {
    if (req === optionsRequest) {
      options.value = []
      optionsError.value = errorMessage(e)
    }
  } finally {
    if (req === optionsRequest) optionsLoading.value = false
  }
}

watch(device, (d) => {
  save('device', d)
  refreshOptions()
})

function changeLocale(e: Event) {
  setLocale((e.target as HTMLSelectElement).value)
}

// --- Toasts for events from the scanner ---

interface Toast {
  id: number
  text: string
  kind: 'info' | 'ok' | 'error'
}
const toasts = ref<Toast[]>([])
let toastId = 0
function toast(text: string, kind: Toast['kind'] = 'info') {
  const id = ++toastId
  toasts.value = [...toasts.value.slice(-3), { id, text, kind }]
  window.setTimeout(() => (toasts.value = toasts.value.filter((x) => x.id !== id)), 5000)
}

const offs = [
  onLive('button', (b) => toast(t(b.watcher ? 'toast.buttonScan' : 'toast.buttonNoWatcher', { button: b.sensor }))),
  onLive('completed', (s) => {
    if (s.source === 'button') toast(t('toast.scanDone', { file: s.filename ?? '' }), 'ok')
  }),
  onLive('failed', (s) => toast(t('toast.scanFailed', { error: s.error ?? '' }), 'error')),
]

onMounted(async () => {
  connectEvents()
  api.version().then((v) => (version.value = v.version)).catch(() => {})
  await refreshDevices()
  refreshOptions()
})
onBeforeUnmount(() => {
  window.removeEventListener('hashchange', onHash)
  offs.forEach((off) => off())
})
</script>

<template>
  <header class="topbar">
    <div class="brand">
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <rect x="2" y="9" width="20" height="9" rx="2" />
        <rect x="4" y="5" width="16" height="3" rx="1" opacity=".5" />
      </svg>
      <span>sarah</span>
      <small v-if="version">{{ version }}</small>
    </div>

    <nav class="tabs">
      <a v-for="id in tabs" :key="id" :href="`#${id}`" :class="{ active: tab === id }">
        {{ t(`app.tabs.${id}`) }}
        <span v-if="id === 'scan' && live.scanning.length" class="dot busy" :title="t('app.scanRunning')"></span>
      </a>
    </nav>

    <div class="topbar-right">
      <select v-if="devices.length > 1" v-model="device" :aria-label="t('app.scanner')">
        <option v-for="d in devices" :key="d.Name" :value="d.Name">{{ d.Vendor }} {{ d.Model }}</option>
      </select>
      <span v-else-if="devices.length === 1" class="device-name">{{ devices[0].Vendor }} {{ devices[0].Model }}</span>

      <select v-if="locales.length > 1" :value="locale" class="compact" :aria-label="t('app.language')" @change="changeLocale">
        <option v-for="l in locales" :key="l.code" :value="l.code">{{ l.name }}</option>
      </select>

      <span class="live" :class="{ on: live.connected }" :title="live.connected ? t('app.liveOn') : t('app.liveOff')">
        <span class="dot"></span>{{ live.connected ? t('app.live') : t('app.offline') }}
      </span>
    </div>
  </header>

  <main>
    <p v-if="devicesError" class="alert">{{ t('app.devicesError', { error: devicesError }) }}</p>

    <ScanView
      v-if="tab === 'scan'"
      :device="device"
      :options="options"
      :options-loading="optionsLoading"
      :options-error="optionsError"
      @reload-options="refreshOptions(true)"
    />
    <WatchersView v-else-if="tab === 'watchers'" :device="device" :devices="devices" />
    <DevicesView
      v-else
      :devices="devices"
      :device="device"
      :devices-loading="devicesLoading"
      :devices-error="devicesError"
      :options="options"
      :options-loading="optionsLoading"
      :options-error="optionsError"
      @select="device = $event"
      @reload-devices="refreshDevices"
      @reload-options="refreshOptions(true)"
    />
  </main>

  <div class="toasts" aria-live="polite">
    <div v-for="x in toasts" :key="x.id" class="toast" :class="x.kind">{{ x.text }}</div>
  </div>
</template>
