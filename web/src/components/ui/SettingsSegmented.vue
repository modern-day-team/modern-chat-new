<script setup lang="ts">
/** 设置页分段选择器：选中态用品牌实心，未选中降透明度，暗色下依然清晰。 */
withDefaults(
  defineProps<{
    modelValue: string
    options: Array<{ value: string; label: string }>
    ariaLabel?: string
  }>(),
  { ariaLabel: '' },
)

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
</script>

<template>
  <div class="settings-segmented" role="group" :aria-label="ariaLabel || undefined">
    <button
      v-for="opt in options"
      :key="opt.value"
      type="button"
      :class="{ 'is-active': modelValue === opt.value }"
      :aria-pressed="modelValue === opt.value"
      @click="emit('update:modelValue', opt.value)"
    >
      {{ opt.label }}
    </button>
  </div>
</template>

<style scoped>
.settings-segmented {
  display: inline-flex;
  align-items: center;
  padding: 4px;
  border-radius: var(--mc-radius-md);
  background: var(--td-bg-color-secondarycontainer);
  flex-wrap: wrap;
  gap: 2px;
}

.settings-segmented button {
  padding: 5px 12px;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  color: var(--td-text-color-secondary);
  font: inherit;
  font-size: 13px;
  white-space: nowrap;
  cursor: pointer;
  opacity: 0.6;
  transition:
    background-color var(--mc-dur-instant) var(--mc-ease-out),
    color var(--mc-dur-instant) var(--mc-ease-out),
    opacity var(--mc-dur-instant) var(--mc-ease-out),
    box-shadow var(--mc-dur-instant) var(--mc-ease-out);
}

.settings-segmented button:hover {
  opacity: 1;
}

.settings-segmented button.is-active {
  background: var(--td-brand-color);
  color: #fff;
  font-weight: 600;
  opacity: 1;
  box-shadow: var(--mc-shadow-1);
}

.settings-segmented button:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 1px;
}
</style>
