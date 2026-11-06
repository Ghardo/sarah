<script setup lang="ts">
import { computed, ref } from 'vue'
import { t } from '../i18n'
import { formatNumber, typeLabel, unitLabel } from '../options'
import type { Device, SaneOption } from '../types'

const props = defineProps<{
  devices: Device[]
  device: string
  devicesLoading: boolean
  devicesError: string
  options: SaneOption[]
  optionsLoading: boolean
  optionsError: string
}>()
const emit = defineEmits<{ select: [name: string]; reloadDevices: []; reloadOptions: [] }>()

const onlyActive = ref(true)
const shown = computed(() => props.options.filter((o) => !onlyActive.value || o.IsActive))

function constraint(o: SaneOption): string {
  const unit = unitLabel(o.Unit)
  if (o.ConstrSet?.length) {
    return o.ConstrSet.map((x) => (typeof x === 'number' ? formatNumber(x) : x)).join(', ') + (unit ? ` ${unit}` : '')
  }
  if (o.ConstrRange) {
    const r = o.ConstrRange
    const step = r.Quant > 0 ? `, ${t('devices.step', { step: formatNumber(r.Quant) })}` : ''
    return `${formatNumber(r.Min)} – ${formatNumber(r.Max)}${unit ? ` ${unit}` : ''}${step}`
  }
  return unit
}

function flags(o: SaneOption): string[] {
  const f: string[] = []
  if (!o.IsActive) f.push(t('devices.flags.inactive'))
  if (!o.IsSettable) f.push(t('devices.flags.readOnly'))
  if (o.IsAdvanced) f.push(t('devices.flags.advanced'))
  if (o.IsAutomatic) f.push(t('devices.flags.auto'))
  if (o.IsEmulated) f.push(t('devices.flags.emulated'))
  if (o.Length > 1) f.push(t('devices.flags.array', { length: o.Length }))
  return f
}
</script>

<template>
  <div class="stack">
    <section class="card">
      <header class="card-head">
        <h2>{{ t('devices.scanners') }}</h2>
        <button type="button" class="link-btn" :disabled="devicesLoading" @click="emit('reloadDevices')">
          {{ t('devices.rescan') }}
        </button>
      </header>
      <p v-if="devicesError" class="alert">{{ devicesError }}</p>
      <p v-else-if="devicesLoading && !devices.length" class="muted">{{ t('devices.searching') }}</p>
      <p v-else-if="!devices.length" class="empty">{{ t('devices.none') }}</p>

      <ul class="device-list">
        <li v-for="d in devices" :key="d.Name">
          <button type="button" class="device" :class="{ active: d.Name === device }" @click="emit('select', d.Name)">
            <strong>{{ d.Vendor }} {{ d.Model }}</strong>
            <span>{{ d.Type }}</span>
            <code>{{ d.Name }}</code>
          </button>
        </li>
      </ul>
    </section>

    <section v-if="device" class="card">
      <header class="card-head">
        <h2>{{ t('devices.options') }}</h2>
        <div class="actions">
          <label class="check">
            <input v-model="onlyActive" type="checkbox" />
            {{ t('devices.onlyActive') }}
          </label>
          <button type="button" class="link-btn" :disabled="optionsLoading" @click="emit('reloadOptions')">
            {{ t('common.refresh') }}
          </button>
        </div>
      </header>

      <p v-if="optionsLoading" class="muted">{{ t('common.loadingOptions') }}</p>
      <p v-else-if="optionsError" class="alert">{{ optionsError }}</p>
      <div v-else class="table-wrap">
        <table class="options-table">
          <thead>
            <tr>
              <th>{{ t('devices.columns.group') }}</th>
              <th>{{ t('devices.columns.name') }}</th>
              <th>{{ t('devices.columns.title') }}</th>
              <th>{{ t('devices.columns.type') }}</th>
              <th>{{ t('devices.columns.values') }}</th>
              <th>{{ t('devices.columns.notes') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="o in shown" :key="o.Name" :class="{ inactive: !o.IsActive }" :title="o.Desc">
              <td>{{ o.Group }}</td>
              <td><code>{{ o.Name }}</code></td>
              <td>{{ o.Title }}</td>
              <td>{{ typeLabel(o.Type) }}</td>
              <td>{{ constraint(o) }}</td>
              <td>
                <span v-for="f in flags(o)" :key="f" class="chip">{{ f }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>
