<script setup lang="ts">
/**
 * 设置页统一行：左侧标题/说明，右侧控件，下面可放展开内容。
 * 目的：把原来「label 左控件右 / label 上输入框下 / 标题描述左数值右」三种行样式收敛成一种。
 */
withDefaults(
  defineProps<{
    title: string
    description?: string
    last?: boolean
    link?: boolean
  }>(),
  { description: '', last: false, link: false },
)

const emit = defineEmits<{ click: [] }>()

function onClick() {
  emit('click')
}

function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Enter' && e.key !== ' ') return
  e.preventDefault()
  emit('click')
}
</script>

<template>
  <div
    class="settings-row"
    :class="{ 'settings-row--last': last, 'settings-row--link': link }"
    :role="link ? 'button' : undefined"
    :tabindex="link ? 0 : undefined"
    @click="link && onClick()"
    @keydown="link && onKeydown($event)"
  >
    <div class="settings-row__main">
      <div class="settings-row__text">
        <span class="settings-row__title">{{ title }}</span>
        <span v-if="description" class="settings-row__description">{{ description }}</span>
      </div>
      <div class="settings-row__control">
        <slot />
      </div>
    </div>
    <div v-if="$slots.footer" class="settings-row__footer">
      <slot name="footer" />
    </div>
  </div>
</template>

<style scoped>
.settings-row {
  padding: 14px 10px;
  margin: 0 -10px;
  border-bottom: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-sm);
  transition: background-color var(--mc-dur-instant) var(--mc-ease-out);
}

.settings-row:hover {
  background: var(--td-bg-color-container-hover);
}

.settings-row:focus-within {
  background: var(--td-bg-color-container-hover);
  box-shadow: inset 0 0 0 1px var(--mc-hairline-strong);
}

.settings-row--last {
  border-bottom: none;
}

.settings-row--link {
  cursor: pointer;
}

.settings-row--link:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: -2px;
}

.settings-row__main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  min-width: 0;
}

.settings-row__text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.settings-row__title {
  font-size: 14px;
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.settings-row__description {
  font-size: 12px;
  line-height: 1.45;
  color: var(--td-text-color-secondary);
}

.settings-row__control {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-shrink: 0;
}

.settings-row__footer {
  margin-top: 10px;
  min-width: 0;
}

@media (max-width: 560px) {
  .settings-row__main {
    align-items: flex-start;
    flex-direction: column;
    gap: 10px;
  }

  .settings-row__control {
    width: 100%;
    justify-content: flex-start;
  }
}
</style>
