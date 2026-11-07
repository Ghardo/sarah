<script setup lang="ts">
import { t } from '../i18n'
import { defaults } from '../defaults'
import type { ImageFormat } from '../types'

// File format (PNG/JPEG) and JPEG quality, for scans and watchers.
// Used inside a .fields grid.
defineProps<{ idPrefix: string }>()
const format = defineModel<ImageFormat>('format', { required: true })
const quality = defineModel<number>('quality', { required: true })

function onQuality(e: Event) {
  const v = Math.round(Number((e.target as HTMLInputElement).value))
  if (Number.isFinite(v)) quality.value = Math.min(100, Math.max(1, v))
}
</script>

<template>
  <div class="field">
    <label :for="`${idPrefix}-format`">{{ t('file.format') }}</label>
    <div class="field-input">
      <select :id="`${idPrefix}-format`" v-model="format">
        <option value="png">{{ t('file.formatPng') }}</option>
        <option value="jpeg">{{ t('file.formatJpeg') }}</option>
      </select>
    </div>
  </div>
  <div v-if="format === 'jpeg'" class="field">
    <label :for="`${idPrefix}-quality`">{{ t('file.quality') }}</label>
    <div class="field-input">
      <input
        :id="`${idPrefix}-quality`"
        class="quality-range"
        type="range"
        min="1"
        max="100"
        step="1"
        :value="quality"
        @input="onQuality"
      />
      <input
        class="quality-number"
        type="number"
        min="1"
        max="100"
        step="1"
        :value="quality"
        :aria-label="t('file.quality')"
        @change="onQuality"
      />
    </div>
    <small class="hint">{{ t('file.qualityHint', { value: defaults.jpegQuality }) }}</small>
  </div>
</template>
