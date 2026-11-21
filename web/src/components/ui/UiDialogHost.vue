<script setup lang="ts">
/**
 * 全局确认弹窗挂载点（App.vue 挂一次）：渲染 feedback.ts 的 mcDialog 状态。
 * 行为对齐 TDesign DialogPlugin：确认后由调用点自行 destroy；取消触发
 * onCancel → onClose；ESC / 遮罩只触发 onClose。焦点进卡片、ESC 可退出。
 */
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'

import { useAppI18n } from '@/composables/useAppI18n'

import UiButton from './UiButton.vue'
import { dialogInternals, mcDialog } from './feedback'

const { t } = useAppI18n()
const cardRef = ref<HTMLElement | null>(null)

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') dialogInternals.onDismiss()
}

watch(
  () => mcDialog.visible,
  (v) => {
    if (v) {
      window.addEventListener('keydown', onKeydown)
      void nextTick(() => cardRef.value?.focus())
    } else {
      window.removeEventListener('keydown', onKeydown)
    }
  },
)

onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <Transition name="mc-fb-dialog">
      <div v-if="mcDialog.visible" class="mc-fb-dialog__overlay" @click.self="dialogInternals.onDismiss()">
        <div
          ref="cardRef"
          class="mc-fb-dialog__card"
          role="alertdialog"
          aria-modal="true"
          :aria-label="mcDialog.header"
          tabindex="-1"
        >
          <div class="mc-fb-dialog__title">{{ mcDialog.header }}</div>
          <div class="mc-fb-dialog__body">{{ mcDialog.body }}</div>
          <div class="mc-fb-dialog__foot">
            <UiButton v-if="mcDialog.cancelText" variant="outline" size="small" @click="dialogInternals.onCancelClick()">
              {{ mcDialog.cancelText }}
            </UiButton>
            <UiButton
              size="small"
              :theme="mcDialog.danger ? 'danger' : 'primary'"
              @click="dialogInternals.onConfirmClick()"
            >
              {{ mcDialog.confirmText || t('common.confirm') }}
            </UiButton>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.mc-fb-dialog__overlay {
  position: fixed;
  inset: 0;
  z-index: 2600;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgb(0 0 0 / 40%);
}

.mc-fb-dialog__card {
  width: min(400px, 100%);
  max-height: calc(100vh - 96px);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 20px;
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  box-shadow: var(--mc-shadow-3);
  outline: none;
}

.mc-fb-dialog__title {
  font-size: var(--mc-fs-body);
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.mc-fb-dialog__body {
  font-size: var(--mc-fs-sm);
  line-height: 1.7;
  color: var(--td-text-color-secondary);
  /* 权限清单等 body 用 \n 分段（对齐原 TDesign 调用点的多行文案） */
  white-space: pre-line;
  word-break: break-word;
}

.mc-fb-dialog__foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 4px;
}

.mc-fb-dialog-enter-active,
.mc-fb-dialog-leave-active {
  transition: opacity var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-fb-dialog-enter-from,
.mc-fb-dialog-leave-to {
  opacity: 0;
}

@media (prefers-reduced-motion: reduce) {
  .mc-fb-dialog-enter-active,
  .mc-fb-dialog-leave-active {
    transition: none;
  }
}
</style>
