<script setup lang="ts">
import { computed, ref } from 'vue'
import OptionField from './OptionField.vue'
import { t } from '../i18n'
import { groupOptions, hasGeometry, isScanOption, matchingPaper, paperLabel, paperSizes, paperValues } from '../options'
import type { OptionValue, OptionValues, SaneOption } from '../types'

const props = defineProps<{ options: SaneOption[]; modelValue: OptionValues }>()
const emit = defineEmits<{ 'update:modelValue': [value: OptionValues] }>()

const showAdvanced = ref(false)

const scanOptions = computed(() => props.options.filter(isScanOption))
const advancedCount = computed(() => scanOptions.value.filter((o) => o.IsAdvanced).length)
const groups = computed(() => groupOptions(scanOptions.value.filter((o) => showAdvanced.value || !o.IsAdvanced)))

const geometry = computed(() => hasGeometry(props.options))
const activePaper = computed(() => matchingPaper(props.options, props.modelValue))

function setValue(name: string, value: OptionValue | undefined) {
  const next = { ...props.modelValue }
  if (value === undefined) delete next[name]
  else next[name] = value
  emit('update:modelValue', next)
}

function applyPaper(id: string) {
  const size = paperSizes.find((s) => s.id === id)
  const values = size && paperValues(props.options, size)
  if (values) emit('update:modelValue', { ...props.modelValue, ...values })
}
</script>

<template>
  <div class="options-form">
    <div v-if="geometry" class="paper">
      <span class="paper-label">{{ t('options.format') }}</span>
      <div class="segmented" role="group" :aria-label="t('options.paperSize')">
        <button
          v-for="s in paperSizes"
          :key="s.id"
          type="button"
          :class="{ active: activePaper === s.id }"
          @click="applyPaper(s.id)"
        >
          {{ paperLabel(s) }}
        </button>
      </div>
      <slot name="format" />
    </div>
    <slot v-if="geometry" name="area" />

    <fieldset v-for="g in groups" :key="g.group" class="group">
      <legend>{{ g.group }}</legend>
      <div class="fields">
        <OptionField
          v-for="o in g.options"
          :key="o.Name"
          :option="o"
          :model-value="modelValue[o.Name]"
          @update:model-value="setValue(o.Name, $event)"
        />
      </div>
    </fieldset>

    <div class="form-footer">
      <label v-if="advancedCount > 0" class="check">
        <input v-model="showAdvanced" type="checkbox" />
        {{ t('options.showAdvanced', { count: advancedCount }) }}
      </label>
      <span v-else></span>
      <button
        type="button"
        class="link-btn"
        :disabled="Object.keys(modelValue).length === 0"
        @click="emit('update:modelValue', {})"
      >
        {{ t('options.resetAll') }}
      </button>
    </div>
  </div>
</template>
