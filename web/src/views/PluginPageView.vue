<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import DeclarativeNodes from '@/components/declarative'
import UiButton from '@/components/ui/UiButton.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { usePluginHost } from '@/composables/usePluginHost'
import { useAuthStore } from '@/stores/auth'
import type { UIDescNode } from '@/types/proto'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const host = usePluginHost()
const { t } = useAppI18n()

const pluginId = computed(() => Number(route.params.pluginId))
const pageId = computed(() => String(route.params.pageId ?? ''))

/** host.start 幂等：已在聊天页启动过时直接复用现有运行时，不重复挂载 iframe */
const ready = ref(false)
onMounted(async () => {
  try {
    await host.start()
  } finally {
    // 加载失败也要落到明确空态，不能永远卡在「加载中」
    ready.value = true
  }
})

const page = computed(() => {
  if (!Number.isFinite(pluginId.value) || !pageId.value) return null
  const found = host.state.pages[pluginId.value]?.find((p) => p.id === pageId.value) ?? null
  // admin 页只有站长可见：直接输 URL 也挡住
  if (found?.scope === 'admin' && auth.user?.role !== 'admin') return null
  return found
})

/** state 是 readonly 响应式对象，渲染前做稳定副本（与侧栏面板一致） */
const nodes = computed<UIDescNode[]>(() => {
  const p = page.value
  return p ? (JSON.parse(JSON.stringify(p.body)) as UIDescNode[]) : []
})

function onAction(action: string, value?: unknown) {
  host.pageAction(pluginId.value, pageId.value, action, value)
}

function backToChat() {
  void router.push('/chat')
}
</script>

<template>
  <div class="mc-plugin-page">
    <header class="mc-plugin-page__head">
      <div class="mc-plugin-page__heading">
        <span v-if="page?.icon" class="mc-plugin-page__icon" aria-hidden="true">{{ page.icon }}</span>
        <h1>{{ page?.title ?? t('pluginPage.notFoundTitle') }}</h1>
      </div>
      <UiButton variant="outline" size="small" @click="backToChat">{{ t('pluginPage.backToChat') }}</UiButton>
    </header>

    <div v-if="!ready" class="mc-plugin-page__state text-placeholder">{{ t('pluginPage.loading') }}</div>
    <div v-else-if="!page" class="mc-plugin-page__state mc-plugin-page__empty">
      <div class="mc-plugin-page__empty-mark" aria-hidden="true">◇</div>
      <p class="mc-plugin-page__empty-title">{{ t('pluginPage.notFoundTitle') }}</p>
      <p class="mc-plugin-page__empty-hint">{{ t('pluginPage.notFoundHint') }}</p>
      <UiButton theme="primary" size="small" @click="backToChat">{{ t('pluginPage.backToChat') }}</UiButton>
    </div>
    <div v-else class="mc-plugin-page__body">
      <DeclarativeNodes :nodes="nodes" :on-action="onAction" />
    </div>
  </div>
</template>

<style scoped>
.mc-plugin-page {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: var(--td-bg-color-page);
}

.mc-plugin-page__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 24px;
  background: var(--td-bg-color-container);
  border-bottom: 1px solid var(--mc-hairline);
}

.mc-plugin-page__heading {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.mc-plugin-page__icon {
  flex-shrink: 0;
  font-size: var(--mc-fs-body);
  color: var(--td-brand-color);
}

.mc-plugin-page__head h1 {
  margin: 0;
  font-size: var(--mc-fs-title);
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-plugin-page__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  width: min(100%, 760px);
  margin: 0 auto;
  padding: 24px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
}

.mc-plugin-page__state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 48px 24px;
  font-size: var(--mc-fs-sm);
}

.mc-plugin-page__empty-mark {
  font-size: 32px;
  color: var(--td-text-color-placeholder);
}

.mc-plugin-page__empty-title {
  margin: 0;
  font-size: var(--mc-fs-body);
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.mc-plugin-page__empty-hint {
  margin: 0 0 8px;
  color: var(--td-text-color-secondary);
  text-align: center;
}

@media (max-width: 768px) {
  .mc-plugin-page__head {
    padding: 12px 16px;
  }

  .mc-plugin-page__body {
    padding: 16px;
  }
}
</style>
