<script setup lang="ts">
/**
 * 全局轻提示挂载点（App.vue 挂一次）：渲染 feedback.ts 的 mcMessages 队列。
 * 图标内联 SVG（不用 emoji 当图标）；颜色走语义令牌；动效只动
 * transform/opacity，尊重 prefers-reduced-motion。
 */
import { mcMessages, type MessageKind } from './feedback'

const ICON_PATHS: Record<MessageKind, string> = {
  success: 'M9 12.5l2.2 2.2L15.5 10M12 4a8 8 0 1 1 0 16 8 8 0 0 1 0-16z',
  error: 'M12 8.5v5m0 3.2v.1M12 4a8 8 0 1 1 0 16 8 8 0 0 1 0-16z',
  warning: 'M12 9v4.5m0 3v.1M12 5L3.5 19.5h17L12 5z',
  info: 'M12 8.5v.1M12 11.5v5M12 4a8 8 0 1 1 0 16 8 8 0 0 1 0-16z',
}
</script>

<template>
  <Teleport to="body">
    <div class="mc-fb-msgs" aria-live="polite">
      <TransitionGroup name="mc-fb-msg">
        <div v-for="m in mcMessages" :key="m.id" class="mc-fb-msg" :class="`is-${m.kind}`" role="status">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path
              :d="ICON_PATHS[m.kind]"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
          <span class="mc-fb-msg__text">{{ m.text }}</span>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.mc-fb-msgs {
  position: fixed;
  top: 16px;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  z-index: 2800;
  pointer-events: none;
}

.mc-fb-msg {
  pointer-events: auto;
  display: flex;
  align-items: center;
  gap: 7px;
  max-width: min(420px, calc(100vw - 32px));
  padding: 9px 14px;
  border-radius: var(--mc-radius-md);
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  box-shadow: var(--mc-shadow-2);
  font-size: var(--mc-fs-sm);
  color: var(--td-text-color-primary);
}

.mc-fb-msg__text {
  min-width: 0;
  white-space: pre-wrap;
  word-break: break-word;
}

.mc-fb-msg.is-success svg {
  color: var(--td-success-color);
}

.mc-fb-msg.is-error svg {
  color: var(--td-error-color);
}

.mc-fb-msg.is-warning svg {
  color: var(--td-warning-color);
}

.mc-fb-msg.is-info svg {
  color: var(--td-brand-color);
}

.mc-fb-msg-enter-active,
.mc-fb-msg-leave-active {
  transition:
    opacity var(--mc-dur-fast) var(--mc-ease-out),
    transform var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-fb-msg-enter-from,
.mc-fb-msg-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}

@media (prefers-reduced-motion: reduce) {
  .mc-fb-msg-enter-active,
  .mc-fb-msg-leave-active {
    transition: none;
  }
}
</style>
