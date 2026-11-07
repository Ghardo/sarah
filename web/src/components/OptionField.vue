<script setup lang="ts">
import { computed } from 'vue'
import { t } from '../i18n'
import { formatNumber, unitLabel } from '../options'
import { OptType, type OptionValue, type SaneOption } from '../types'

const props = defineProps<{ option: SaneOption; modelValue: OptionValue | undefined }>()
const emit = defineEmits<{ 'update:modelValue': [value: OptionValue | undefined] }>()

const id = computed(() => `opt-${props.option.Name}`)
const unit = computed(() => unitLabel(props.option.Unit))
const isNumber = computed(() => props.option.Type === OptType.Int || props.option.Type === OptType.Fixed)

const kind = computed(() => {
  const o = props.option
  if (o.Type === OptType.Bool) return 'bool'
  if (o.ConstrSet?.length) return 'set'
  if (isNumber.value) return 'number'
  return 'text'
})

const range = computed(() => props.option.ConstrRange)
const step = computed(() => {
  const r = range.value
  if (r && r.Quant > 0) return r.Quant
  return props.option.Type === OptType.Int ? 1 : 'any'
})

const outOfRange = computed(() => {
  const v = props.modelValue
  const r = range.value
  return typeof v === 'number' && r !== null && (v < r.Min || v > r.Max)
})

// <select> works with strings, so the index into the constraint set is used
const setIndex = computed(() => {
  const set = props.option.ConstrSet ?? []
  const i = set.findIndex((x) => x === props.modelValue)
  return i < 0 ? '' : String(i)
})

function onSet(e: Event) {
  const raw = (e.target as HTMLSelectElement).value
  emit('update:modelValue', raw === '' ? undefined : props.option.ConstrSet![Number(raw)])
}

function onBool(e: Event) {
  const raw = (e.target as HTMLSelectElement).value
  emit('update:modelValue', raw === '' ? undefined : raw === 'true')
}

function onNumber(e: Event) {
  const raw = (e.target as HTMLInputElement).value.trim().replace(',', '.')
  if (raw === '') return emit('update:modelValue', undefined)
  let n = Number(raw)
  if (Number.isNaN(n)) return
  if (props.option.Type === OptType.Int) n = Math.round(n)
  emit('update:modelValue', n)
}

function onText(e: Event) {
  const raw = (e.target as HTMLInputElement).value
  emit('update:modelValue', raw === '' ? undefined : raw)
}

/** Tooltip: the backend description and the technical name of the option. */
const tooltip = computed(() => [props.option.Desc, `SANE: ${props.option.Name}`].filter(Boolean).join('\n'))

const setLabel = (x: number | string) => (typeof x === 'number' ? formatNumber(x) : x)
</script>

<template>
  <div class="field" :class="{ 'field-set': modelValue !== undefined }">
    <label :for="id" :title="tooltip">{{ option.Title || option.Name }}</label>

    <div class="field-input">
      <select v-if="kind === 'bool'" :id="id" :value="modelValue === undefined ? '' : String(modelValue)" @change="onBool">
        <option value="">{{ t('options.deviceDefault') }}</option>
        <option value="true">{{ t('options.on') }}</option>
        <option value="false">{{ t('options.off') }}</option>
      </select>

      <select v-else-if="kind === 'set'" :id="id" :value="setIndex" @change="onSet">
        <option value="">{{ t('options.deviceDefault') }}</option>
        <option v-for="(x, i) in option.ConstrSet!" :key="i" :value="String(i)">
          {{ setLabel(x) }}{{ unit && typeof x === 'number' ? ` ${unit}` : '' }}
        </option>
      </select>

      <input
        v-else-if="kind === 'number'"
        :id="id"
        type="number"
        inputmode="decimal"
        :class="{ invalid: outOfRange }"
        :min="range?.Min"
        :max="range?.Max"
        :step="step"
        :value="typeof modelValue === 'number' ? modelValue : ''"
        :placeholder="t('options.deviceDefault')"
        @change="onNumber"
      />

      <input
        v-else
        :id="id"
        type="text"
        :value="modelValue ?? ''"
        :placeholder="t('options.deviceDefault')"
        @change="onText"
      />

      <span v-if="unit && kind === 'number'" class="unit">{{ unit }}</span>
      <button
        v-if="modelValue !== undefined"
        type="button"
        class="icon-btn"
        :title="t('options.reset')"
        :aria-label="t('options.reset')"
        @click="emit('update:modelValue', undefined)"
      >
        ×
      </button>
    </div>

    <small v-if="kind === 'number' && range" class="hint" :class="{ error: outOfRange }">
      {{ formatNumber(range.Min) }} – {{ formatNumber(range.Max) }}{{ unit ? ` ${unit}` : '' }}
    </small>
  </div>
</template>
