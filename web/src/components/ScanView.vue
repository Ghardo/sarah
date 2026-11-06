<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import OptionsForm from './OptionsForm.vue'
import { api, errorMessage } from '../api'
import { live, onLive, type ScanInfo } from '../events'
import { t } from '../i18n'
import { filenamePlaceholders, suggestedValues } from '../options'
import { load, save } from '../storage'
import type { OptionValues, SaneOption } from '../types'

const props = defineProps<{
  device: string
  options: SaneOption[]
  optionsLoading: boolean
  optionsError: string
}>()
const emit = defineEmits<{ reloadOptions: [] }>()

interface Settings {
  values: OptionValues
  filename: string
  newDoc: boolean
}

const values = ref<OptionValues>({})
const filename = ref('')
const newDoc = ref(false)

// Einstellungen je Gerät merken
watch(
  () => [props.device, props.options] as const,
  ([device, options]) => {
    if (!device || options.length === 0) return
    const s = load<Settings | null>(`scan:${device}`, null)
    values.value = s?.values ?? suggestedValues(options)
    filename.value = s?.filename ?? ''
    newDoc.value = s?.newDoc ?? false
  },
  { immediate: true },
)
watch([values, filename, newDoc], () => {
  if (props.device && props.options.length) {
    save(`scan:${props.device}`, { values: values.value, filename: filename.value, newDoc: newDoc.value })
  }
})

// --- Ergebnis, auch von Scans über die Hardware-Tasten ---

const result = ref<ScanInfo | null>(null)
const previewUrl = ref('')
const previewFailed = ref(false)

function showResult(s: ScanInfo) {
  result.value = s
  previewFailed.value = false
  previewUrl.value = api.lastPreviewUrl()
}

if (live.lastCompleted && live.lastCompleted.device === props.device) showResult(live.lastCompleted)

/** Scan, der gerade über eine Taste läuft (der eigene wird über `scanning` angezeigt). */
const buttonScan = computed(() => live.scanning.find((s) => s.device === props.device && s.source === 'button'))
const busy = computed(() => live.scanning.some((s) => s.device === props.device))

// --- Scan auslösen ---

const scanning = ref(false)
const error = ref('')
const elapsed = ref(0)
let timer: number | undefined

const canScan = computed(() => !!props.device && props.options.length > 0 && !scanning.value && !busy.value)

async function scan() {
  if (!canScan.value) return
  scanning.value = true
  error.value = ''
  elapsed.value = 0
  const started = Date.now()
  timer = window.setInterval(() => (elapsed.value = Math.floor((Date.now() - started) / 1000)), 250)
  try {
    const res = await api.scan({
      Device: props.device,
      Filename: filename.value.trim() || undefined,
      NewDoc: newDoc.value || undefined,
      Options: values.value,
    })
    // Falls das SSE-Ereignis (noch) nicht da ist, Ergebnis aus der Antwort zeigen
    if (result.value?.filename !== res.filename) {
      showResult({ ...res, id: 0, device: props.device, source: 'api', started: '' })
    }
  } catch (e) {
    error.value = t('scan.failed', { error: errorMessage(e) })
  } finally {
    scanning.value = false
    window.clearInterval(timer)
  }
}

const offCompleted = onLive('completed', (s) => {
  if (s.device === props.device) showResult(s)
})
const offFailed = onLive('failed', (s) => {
  if (s.device === props.device && s.source === 'button') {
    error.value = t('scan.buttonFailed', { button: s.sensor ?? '', error: s.error ?? '' })
  }
})
onBeforeUnmount(() => {
  window.clearInterval(timer)
  offCompleted()
  offFailed()
})
</script>

<template>
  <div class="layout-two">
    <section class="card">
      <header class="card-head">
        <h2>{{ t('scan.settings') }}</h2>
        <button type="button" class="link-btn" :disabled="optionsLoading" @click="emit('reloadOptions')">
          {{ t('scan.reloadOptions') }}
        </button>
      </header>

      <p v-if="!device" class="muted">{{ t('common.noDevice') }}</p>
      <p v-else-if="optionsLoading" class="muted">{{ t('common.loadingOptions') }}</p>
      <p v-else-if="optionsError" class="alert">{{ optionsError }}</p>
      <template v-else>
        <OptionsForm v-model="values" :options="options" />

        <fieldset class="group">
          <legend>{{ t('file.legend') }}</legend>
          <div class="fields">
            <div class="field wide">
              <label for="scan-filename">{{ t('file.template') }}</label>
              <div class="field-input">
                <input id="scan-filename" v-model="filename" type="text" placeholder="scan_{index}_{date}_{time}" />
              </div>
              <small class="hint">
                {{ t('file.placeholders', { list: filenamePlaceholders }) }} · {{ t('file.extension') }}
              </small>
            </div>
            <label class="check">
              <input v-model="newDoc" type="checkbox" />
              {{ t('file.newDocScan') }}
            </label>
          </div>
        </fieldset>
      </template>
    </section>

    <section class="card result">
      <header class="card-head">
        <h2>{{ t('scan.result') }}</h2>
      </header>

      <button type="button" class="primary big" :disabled="!canScan" @click="scan">
        <span v-if="scanning || buttonScan" class="spinner" aria-hidden="true"></span>
        <template v-if="scanning">{{ t('scan.scanning', { seconds: elapsed }) }}</template>
        <template v-else-if="buttonScan">{{ t('scan.buttonScanning', { button: buttonScan.sensor ?? '' }) }}</template>
        <template v-else>{{ t('scan.start') }}</template>
      </button>

      <p v-if="error" class="alert">{{ error }}</p>

      <template v-if="result">
        <dl class="facts">
          <dt>{{ t('scan.file') }}</dt>
          <dd><code>{{ result.filename }}</code></dd>
          <template v-if="result.width">
            <dt>{{ t('scan.size') }}</dt>
            <dd>{{ result.width }} × {{ result.height }} px</dd>
          </template>
          <dt>{{ t('scan.trigger') }}</dt>
          <dd>
            {{ result.source === 'button' ? t('scan.triggerButton', { button: result.sensor ?? '' }) : t('scan.triggerApi') }}
          </dd>
        </dl>

        <a v-if="!previewFailed" class="preview" :href="previewUrl" target="_blank" rel="noopener">
          <img :src="previewUrl" :alt="t('scan.previewAlt', { file: result.filename ?? '' })" @error="previewFailed = true" />
        </a>
        <p v-else class="muted">{{ t('scan.noPreview') }}</p>
        <a v-if="!previewFailed" class="button" :href="previewUrl" :download="result.filename">{{ t('scan.download') }}</a>
      </template>
      <p v-else-if="!scanning && !buttonScan && !error" class="muted">{{ t('scan.empty') }}</p>
    </section>
  </div>
</template>
