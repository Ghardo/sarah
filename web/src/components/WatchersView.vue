<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import FormatFields from './FormatFields.vue'
import PostProcessField from './PostProcessField.vue'
import OptionsForm from './OptionsForm.vue'
import { api, errorMessage, loadOptions } from '../api'
import { live, onLive } from '../events'
import { t } from '../i18n'
import { filenamePlaceholders, formatValue, sensorOptions, suggestedValues } from '../options'
import { defaults } from '../defaults'
import { postProcessName } from '../postprocess'
import type { Device, SaneOption, ScanRequest, WatcherStatus } from '../types'

const props = defineProps<{ device: string; devices: Device[] }>()

const watchers = ref<WatcherStatus[]>([])
const loading = ref(false)
const error = ref('')

const key = (r: Pick<ScanRequest, 'Device' | 'WatchSensor'>) => `${r.Device}:${r.WatchSensor ?? ''}`

async function reload() {
  loading.value = true
  error.value = ''
  try {
    const list = await api.watchers()
    list.sort((a, b) => key(a.config).localeCompare(key(b.config)))
    watchers.value = list
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}
onMounted(reload)

// Device options (for titles and units in the list)
const deviceOptions = ref<Record<string, SaneOption[]>>({})
async function ensureOptions(device: string): Promise<SaneOption[]> {
  if (!deviceOptions.value[device]) {
    const opts = await loadOptions(device)
    deviceOptions.value = { ...deviceOptions.value, [device]: opts }
  }
  return deviceOptions.value[device]
}
watch(watchers, (list) => {
  for (const d of new Set(list.map((w) => w.config.Device))) ensureOptions(d).catch(() => {})
})

function optionTitle(device: string, name: string): string {
  return deviceOptions.value[device]?.find((o) => o.Name === name)?.Title || name
}
function summary(w: WatcherStatus): { name: string; value: string }[] {
  const opts = deviceOptions.value[w.config.Device] ?? []
  // Same order as the device (Go returns the map sorted alphabetically)
  const pos = (name: string) => {
    const i = opts.findIndex((o) => o.Name === name)
    return i < 0 ? Number.MAX_SAFE_INTEGER : i
  }
  return Object.entries(w.config.Options ?? {})
    .sort(([a], [b]) => pos(a) - pos(b) || a.localeCompare(b))
    .map(([name, v]) => ({
      name,
      value: formatValue(v, opts.find((o) => o.Name === name)),
    }))
}
function formatLabel(w: WatcherStatus): string {
  return w.config.Format === 'jpeg' ? `JPEG ${w.config.Quality || defaults.jpegQuality}` : 'PNG'
}
function deviceLabel(name: string): string {
  const d = props.devices.find((x) => x.Name === name)
  return d ? `${d.Vendor} ${d.Model}` : name
}

/** Is a scan currently running for this watcher? */
function isScanning(w: WatcherStatus): boolean {
  return live.scanning.some(
    (s) => s.source === 'button' && s.device === w.config.Device && s.sensor === w.config.WatchSensor,
  )
}

/** Briefly highlight when the button was pressed. */
const flash = ref('')
let flashTimer: number | undefined

// --- Editor ---

/** In the editor format and quality are always set, so the fields show something. */
type Draft = ScanRequest & Required<Pick<ScanRequest, 'Format' | 'Quality' | 'PostProcess'>>

interface Editor {
  original: { Device: string; WatchSensor: string } | null
  draft: Draft
}
const editor = ref<Editor | null>(null)
const editorOptions = ref<SaneOption[]>([])
const editorLoading = ref(false)
const editorError = ref('')
const saving = ref(false)
const pickedByButton = ref(false)

const sensors = computed(() => sensorOptions(editorOptions.value))
const usedSensors = computed(
  () =>
    new Set(
      watchers.value
        .filter((w) => w.config.Device === editor.value?.draft.Device)
        .map((w) => w.config.WatchSensor ?? '')
        .filter((s) => s !== editor.value?.original?.WatchSensor),
    ),
)
const duplicate = computed(() => {
  const s = editor.value?.draft.WatchSensor
  return !!s && usedSensors.value.has(s)
})

async function loadEditorOptions(device: string) {
  editorLoading.value = true
  editorError.value = ''
  editorOptions.value = []
  try {
    editorOptions.value = await ensureOptions(device)
  } catch (e) {
    editorError.value = errorMessage(e)
  } finally {
    editorLoading.value = false
  }
}

async function startNew() {
  const device = props.device || props.devices[0]?.Name || ''
  pickedByButton.value = false
  editor.value = {
    original: null,
    draft: {
      Device: device,
      WatchSensor: '',
      Filename: 'scan_{doc}_{index}_{date}_{time}',
      NewDoc: false,
      Format: 'png',
      Quality: defaults.jpegQuality,
      PostProcess: '',
      Options: {},
    },
  }
  if (!device) return
  await loadEditorOptions(device)
  const ed = editor.value
  if (!ed || ed.draft.Device !== device) return
  ed.draft.Options = suggestedValues(editorOptions.value)
  if (!ed.draft.WatchSensor) {
    ed.draft.WatchSensor = sensors.value.find((s) => !usedSensors.value.has(s.Name))?.Name ?? ''
  }
}

async function edit(w: WatcherStatus) {
  const c = w.config
  pickedByButton.value = false
  editor.value = {
    original: { Device: c.Device, WatchSensor: c.WatchSensor ?? '' },
    draft: {
      Device: c.Device,
      WatchSensor: c.WatchSensor ?? '',
      Filename: c.Filename ?? '',
      NewDoc: !!c.NewDoc,
      Format: c.Format ?? 'png',
      Quality: c.Quality || defaults.jpegQuality,
      PostProcess: c.PostProcess ?? '',
      Options: { ...(c.Options ?? {}) },
    },
  }
  await loadEditorOptions(c.Device)
}

async function changeDevice(e: Event) {
  const ed = editor.value
  if (!ed) return
  const device = (e.target as HTMLSelectElement).value
  ed.draft.Device = device
  ed.draft.WatchSensor = ''
  await loadEditorOptions(device)
  if (editor.value?.draft.Device === device) editor.value.draft.Options = suggestedValues(editorOptions.value)
}

async function saveEditor() {
  const ed = editor.value
  if (!ed || !ed.draft.WatchSensor || duplicate.value) return
  saving.value = true
  editorError.value = ''
  try {
    const d = ed.draft
    await api.saveWatcher({
      Device: d.Device,
      WatchSensor: d.WatchSensor,
      Filename: d.Filename?.trim() || undefined,
      NewDoc: d.NewDoc || undefined,
      Format: d.Format,
      Quality: d.Format === 'jpeg' ? d.Quality : undefined,
      PostProcess: d.PostProcess || undefined,
      Options: d.Options,
    })
    // Button or device changed: remove the old entry
    if (ed.original && key(ed.original) !== key(d)) {
      await api.deleteWatcher(ed.original.Device, ed.original.WatchSensor)
    }
    editor.value = null
    await reload()
  } catch (e) {
    editorError.value = errorMessage(e)
  } finally {
    saving.value = false
  }
}

async function remove(w: WatcherStatus) {
  const c = w.config
  const name = optionTitle(c.Device, c.WatchSensor ?? '')
  if (!confirm(t('watchers.confirmDelete', { button: name }))) return
  try {
    await api.deleteWatcher(c.Device, c.WatchSensor ?? '')
    if (editor.value?.original && key(editor.value.original) === key(c)) editor.value = null
    await reload()
  } catch (e) {
    error.value = errorMessage(e)
  }
}

async function removeAll() {
  if (!confirm(t('watchers.confirmDeleteAll'))) return
  try {
    await api.deleteAllWatchers()
    editor.value = null
    await reload()
  } catch (e) {
    error.value = errorMessage(e)
  }
}

// --- Live events ---

const offWatchers = onLive('watchers', () => {
  if (!saving.value) reload()
})
const offButton = onLive('button', (b) => {
  flash.value = `${b.device}:${b.sensor}`
  window.clearTimeout(flashTimer)
  flashTimer = window.setTimeout(() => (flash.value = ''), 1500)

  // In the editor: take over a pressed (free) button directly
  const ed = editor.value
  if (ed && !b.watcher && b.device === ed.draft.Device && sensors.value.some((s) => s.Name === b.sensor)) {
    ed.draft.WatchSensor = b.sensor
    pickedByButton.value = true
  }
})
onBeforeUnmount(() => {
  offWatchers()
  offButton()
  window.clearTimeout(flashTimer)
})
</script>

<template>
  <div class="layout-two">
    <section class="card">
      <header class="card-head">
        <h2>{{ t('watchers.title') }}</h2>
        <div class="actions">
          <button type="button" class="link-btn" :disabled="loading" @click="reload">{{ t('common.refresh') }}</button>
          <button type="button" class="primary" :disabled="!devices.length" @click="startNew">{{ t('watchers.new') }}</button>
        </div>
      </header>
      <p class="muted">{{ t('watchers.intro') }}</p>

      <p v-if="error" class="alert">{{ error }}</p>
      <p v-else-if="loading && !watchers.length" class="muted">{{ t('common.loading') }}</p>
      <p v-else-if="!watchers.length" class="empty">{{ t('watchers.empty') }}</p>

      <ul class="watcher-list">
        <li
          v-for="w in watchers"
          :key="key(w.config)"
          class="watcher"
          :class="{
            selected: editor?.original && key(editor.original) === key(w.config),
            flash: flash === key(w.config),
          }"
        >
          <div class="watcher-main">
            <div class="watcher-title">
              <span class="key">{{ optionTitle(w.config.Device, w.config.WatchSensor ?? '') }}</span>
              <span v-if="isScanning(w)" class="badge busy"><span class="spinner small"></span> {{ t('watchers.scanning') }}</span>
              <span v-else class="badge" :class="w.active ? 'ok' : 'off'">{{ w.active ? t('watchers.active') : t('watchers.inactive') }}</span>
              <span v-if="w.config.NewDoc" class="badge">{{ t('watchers.newDoc') }}</span>
              <span class="badge">{{ formatLabel(w) }}</span>
              <span v-if="postProcessName(w.config.PostProcess)" class="badge" :title="t('postprocess.field')">
                → {{ postProcessName(w.config.PostProcess) }}
              </span>
            </div>
            <div class="watcher-sub">
              {{ deviceLabel(w.config.Device) }} · <code>{{ w.config.Filename || defaults.filenameTemplate }}</code>
            </div>
            <div v-if="summary(w).length" class="chips">
              <span v-for="s in summary(w)" :key="s.name" class="chip" :title="optionTitle(w.config.Device, s.name)">
                {{ s.name }}: {{ s.value }}
              </span>
            </div>
          </div>
          <div class="watcher-actions">
            <button type="button" @click="edit(w)">{{ t('common.edit') }}</button>
            <button type="button" class="danger" @click="remove(w)">{{ t('common.delete') }}</button>
          </div>
        </li>
      </ul>

      <div v-if="watchers.length > 1" class="form-footer">
        <span></span>
        <button type="button" class="link-btn danger-text" @click="removeAll">{{ t('watchers.deleteAll') }}</button>
      </div>
    </section>

    <section v-if="editor" class="card">
      <header class="card-head">
        <h2>{{ editor.original ? t('watchers.edit') : t('watchers.new') }}</h2>
        <button type="button" class="icon-btn" :aria-label="t('common.close')" @click="editor = null">×</button>
      </header>

      <form class="editor" @submit.prevent="saveEditor">
        <fieldset class="group">
          <legend>{{ t('watchers.trigger') }}</legend>
          <div class="fields">
            <div v-if="devices.length > 1" class="field">
              <label for="w-device">{{ t('app.scanner') }}</label>
              <div class="field-input">
                <select id="w-device" :value="editor.draft.Device" @change="changeDevice">
                  <option v-for="d in devices" :key="d.Name" :value="d.Name">{{ d.Vendor }} {{ d.Model }}</option>
                </select>
              </div>
            </div>
            <div class="field">
              <label for="w-sensor">{{ t('watchers.button') }}</label>
              <div class="field-input">
                <select
                  id="w-sensor"
                  v-model="editor.draft.WatchSensor"
                  :class="{ picked: pickedByButton }"
                  :disabled="editorLoading"
                  required
                  @change="pickedByButton = false"
                >
                  <option value="" disabled>{{ t('watchers.chooseButton') }}</option>
                  <option v-for="s in sensors" :key="s.Name" :value="s.Name" :title="s.Name">
                    {{ s.Title || s.Name }}{{ usedSensors.has(s.Name) ? ` – ${t('watchers.taken')}` : '' }}
                  </option>
                </select>
              </div>
              <small v-if="duplicate" class="hint error">{{ t('watchers.duplicate') }}</small>
              <small v-else-if="!editorLoading && !sensors.length" class="hint error">{{ t('watchers.noButtons') }}</small>
              <small v-else-if="pickedByButton" class="hint ok">{{ t('watchers.picked') }}</small>
              <small v-else-if="live.connected" class="hint">
                {{ t('watchers.tip') }}
              </small>
            </div>
          </div>
        </fieldset>

        <fieldset class="group">
          <legend>{{ t('file.legend') }}</legend>
          <div class="fields">
            <div class="field wide">
              <label for="w-filename">{{ t('file.template') }}</label>
              <div class="field-input">
                <input id="w-filename" v-model="editor.draft.Filename" type="text" :placeholder="defaults.filenameTemplate" />
              </div>
              <small class="hint">
                {{ t('file.placeholders', { list: filenamePlaceholders }) }} · {{ t('file.extension') }}
              </small>
            </div>
            <FormatFields v-model:format="editor.draft.Format" v-model:quality="editor.draft.Quality" id-prefix="w" />
            <PostProcessField v-model="editor.draft.PostProcess" id-prefix="w" />
            <label class="check">
              <input v-model="editor.draft.NewDoc" type="checkbox" />
              {{ t('file.newDocWatcher') }}
            </label>
          </div>
        </fieldset>

        <p v-if="editorLoading" class="muted">{{ t('common.loadingOptions') }}</p>
        <OptionsForm v-else-if="editorOptions.length" v-model="editor.draft.Options" :options="editorOptions" />

        <p v-if="editorError" class="alert">{{ editorError }}</p>

        <div class="form-actions">
          <button type="button" @click="editor = null">{{ t('common.cancel') }}</button>
          <button
            type="submit"
            class="primary"
            :disabled="saving || editorLoading || !editor.draft.WatchSensor || duplicate"
          >
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </form>
    </section>
  </div>
</template>
