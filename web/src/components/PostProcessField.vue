<script setup lang="ts">
import { watch } from 'vue'
import { t } from '../i18n'
import { NONE, postProcesses, validSelection } from '../postprocess'

// Selection of the post-process for scan and watcher (exactly one or none).
// Used inside a .fields grid.
defineProps<{ idPrefix: string }>()
const model = defineModel<string>({ required: true })

// Move deleted processes or an unset selection to the default
watch(
  () => [model.value, postProcesses.list] as const,
  ([sel]) => {
    const valid = validSelection(sel)
    if (valid !== sel) model.value = valid
  },
  { immediate: true, deep: true },
)

const selected = () => postProcesses.list.find((p) => p.id === model.value)
</script>

<template>
  <div v-if="postProcesses.list.length" class="field wide">
    <label :for="`${idPrefix}-postprocess`">{{ t('postprocess.field') }}</label>
    <div class="field-input">
      <select :id="`${idPrefix}-postprocess`" v-model="model">
        <option v-for="p in postProcesses.list" :key="p.id" :value="p.id">
          {{ p.name }}{{ p.default ? ` (${t('postprocess.default')})` : '' }}
        </option>
        <option :value="NONE">{{ t('postprocess.none') }}</option>
      </select>
    </div>
    <small v-if="selected()?.description" class="hint">{{ selected()?.description }}</small>
  </div>
</template>
