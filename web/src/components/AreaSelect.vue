<script setup lang="ts">
import { computed, ref } from 'vue'
import { t } from '../i18n'
import { formatNumber, type ScanBed } from '../options'
import type { OptionValues } from '../types'

/**
 * Scan area as a rectangle over the scan bed (optionally with a preview image).
 * Drag a rectangle, adjust it at the four corners or move it as a whole.
 * The rectangle always stays inside the bed.
 */
const props = defineProps<{ bed: ScanBed; modelValue: OptionValues; image?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: OptionValues] }>()

interface Rect {
  x1: number
  y1: number
  x2: number
  y2: number
}

/** Smallest area in mm; anything smaller is discarded on release. */
const minSize = 5

const bedW = computed(() => props.bed.maxX - props.bed.minX)
const bedH = computed(() => props.bed.maxY - props.bed.minY)

const clamp = (v: number, lo: number, hi: number) => Math.min(Math.max(v, lo), hi)
const num = (v: unknown, fallback: number) => (typeof v === 'number' ? v : fallback)

// Stored values may come from another device, so clamp them to the bed
const rect = computed<Rect>(() => {
  const b = props.bed
  return {
    x1: clamp(num(props.modelValue['tl-x'], b.minX), b.minX, b.maxX),
    y1: clamp(num(props.modelValue['tl-y'], b.minY), b.minY, b.maxY),
    x2: clamp(num(props.modelValue['br-x'], b.maxX), b.minX, b.maxX),
    y2: clamp(num(props.modelValue['br-y'], b.maxY), b.minY, b.maxY),
  }
})

const pct = (v: number, min: number, size: number) => `${((v - min) / size) * 100}%`
const box = computed(() => ({
  left: pct(rect.value.x1, props.bed.minX, bedW.value),
  top: pct(rect.value.y1, props.bed.minY, bedH.value),
  width: `${((rect.value.x2 - rect.value.x1) / bedW.value) * 100}%`,
  height: `${((rect.value.y2 - rect.value.y1) / bedH.value) * 100}%`,
}))

const corners = computed(() => {
  const r = rect.value
  return [
    { id: 'tl', x: r.x1, y: r.y1 },
    { id: 'tr', x: r.x2, y: r.y1 },
    { id: 'bl', x: r.x1, y: r.y2 },
    { id: 'br', x: r.x2, y: r.y2 },
  ] as const
})

const round = (v: number) => Math.round(v * 10) / 10

function emitRect(r: Rect) {
  emit('update:modelValue', {
    ...props.modelValue,
    'tl-x': round(r.x1),
    'tl-y': round(r.y1),
    'br-x': round(r.x2),
    'br-y': round(r.y2),
  })
}

// --- Dragging ---

const surface = ref<HTMLElement | null>(null)

type Drag =
  | { kind: 'corner'; anchorX: number; anchorY: number } // the opposite corner stays fixed
  | { kind: 'move'; startX: number; startY: number; start: Rect }

let drag: Drag | null = null
let before: Rect | null = null

/** Pointer position in mm, clamped to the bed. */
function toBed(e: PointerEvent) {
  const b = surface.value!.getBoundingClientRect()
  return {
    x: clamp(props.bed.minX + ((e.clientX - b.left) / b.width) * bedW.value, props.bed.minX, props.bed.maxX),
    y: clamp(props.bed.minY + ((e.clientY - b.top) / b.height) * bedH.value, props.bed.minY, props.bed.maxY),
  }
}

function begin(e: PointerEvent, d: Drag) {
  if (e.button !== 0) return
  e.preventDefault()
  e.stopPropagation()
  drag = d
  before = { ...rect.value }
  surface.value!.setPointerCapture(e.pointerId)
}

function onCorner(e: PointerEvent, id: string) {
  const r = rect.value
  begin(e, {
    kind: 'corner',
    anchorX: id.endsWith('l') ? r.x2 : r.x1,
    anchorY: id.startsWith('t') ? r.y2 : r.y1,
  })
}

function onBox(e: PointerEvent) {
  const p = toBed(e)
  begin(e, { kind: 'move', startX: p.x, startY: p.y, start: { ...rect.value } })
}

function onSurface(e: PointerEvent) {
  // New rectangle: the start point is the fixed corner
  const p = toBed(e)
  begin(e, { kind: 'corner', anchorX: p.x, anchorY: p.y })
  emitRect({ x1: p.x, y1: p.y, x2: p.x, y2: p.y })
}

function onMove(e: PointerEvent) {
  if (!drag) return
  const p = toBed(e)
  if (drag.kind === 'corner') {
    // Dragging past the fixed corner simply flips the rectangle
    emitRect({
      x1: Math.min(drag.anchorX, p.x),
      y1: Math.min(drag.anchorY, p.y),
      x2: Math.max(drag.anchorX, p.x),
      y2: Math.max(drag.anchorY, p.y),
    })
  } else {
    const s = drag.start
    // Limit the move so the whole rectangle stays on the bed
    const dx = clamp(p.x - drag.startX, props.bed.minX - s.x1, props.bed.maxX - s.x2)
    const dy = clamp(p.y - drag.startY, props.bed.minY - s.y1, props.bed.maxY - s.y2)
    emitRect({ x1: s.x1 + dx, y1: s.y1 + dy, x2: s.x2 + dx, y2: s.y2 + dy })
  }
}

function onUp(e: PointerEvent) {
  if (!drag) return
  surface.value?.releasePointerCapture(e.pointerId)
  const r = rect.value
  // Accidental click or area too small: keep the previous area
  if (before && (r.x2 - r.x1 < minSize || r.y2 - r.y1 < minSize)) emitRect(before)
  drag = null
  before = null
}

function selectAll() {
  emitRect({ x1: props.bed.minX, y1: props.bed.minY, x2: props.bed.maxX, y2: props.bed.maxY })
}

const sizeLabel = computed(
  () => `${formatNumber(round(rect.value.x2 - rect.value.x1))} × ${formatNumber(round(rect.value.y2 - rect.value.y1))} mm`,
)
</script>

<template>
  <div class="area-select">
    <div
      ref="surface"
      class="area-surface"
      :style="{ aspectRatio: `${bedW} / ${bedH}` }"
      @pointerdown="onSurface"
      @pointermove="onMove"
      @pointerup="onUp"
      @pointercancel="onUp"
    >
      <img v-if="image" :src="image" :alt="t('area.previewAlt')" draggable="false" />
      <div class="area-box" :style="box" @pointerdown="onBox">
        <span class="area-size">{{ sizeLabel }}</span>
      </div>
      <span
        v-for="c in corners"
        :key="c.id"
        class="area-handle"
        :class="c.id"
        :style="{ left: pct(c.x, bed.minX, bedW), top: pct(c.y, bed.minY, bedH) }"
        @pointerdown="onCorner($event, c.id)"
      ></span>
    </div>
    <div class="area-foot">
      <small class="hint">{{ t('area.hint') }}</small>
      <button type="button" class="link-btn" @click="selectAll">{{ t('area.all') }}</button>
    </div>
  </div>
</template>
