<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import DeclarativeNodes from '@/components/declarative'
import UiButton from '@/components/ui/UiButton.vue'
import { MessagePlugin } from '@/components/ui/feedback'
import { useAppI18n } from '@/composables/useAppI18n'
import { http } from '@/core/network/http'
import { serverPluginApi, type ServerPluginPage } from '@/core/network/endpoints'
import { useAuthStore } from '@/stores/auth'
import type { UIDescNode } from '@/types/proto'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { t, te } = useAppI18n()

const pluginId = computed(() => String(route.params.pluginId ?? ''))
const pageId = computed(() => String(route.params.pageId ?? ''))
const loading = ref(true)
const pages = ref<ServerPluginPage[]>([])
const acting = ref(false)

onMounted(async () => {
  try {
    const list = await serverPluginApi.pages()
    pages.value = Array.isArray(list) ? list : []
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    loading.value = false
  }
})

const page = computed(() => {
  const found = pages.value.find((p) => String(p.pluginId) === pluginId.value && p.id === pageId.value) ?? null
  if (found?.scope === 'admin' && auth.user?.role !== 'admin') return null
  return found
})

const nodes = computed<UIDescNode[]>(() => {
  const p = page.value
  return p ? (JSON.parse(JSON.stringify(p.body ?? [])) as UIDescNode[]) : []
})

/** 服务端插件动作：响应体完全来自清单里该 route 的 response.body，不额外包信封 */
async function onAction(action: string, value?: unknown) {
  const p = page.value
  if (!p || !action || acting.value) return
  acting.value = true
  try {
    const pluginName = String(p.pluginId)
    const res = await http.post<{ message?: string }>(
      `/ext/${encodeURIComponent(pluginName)}/${encodeURIComponent(action)}`,
      { value, pageId: p.id },
    )
    const msg = typeof res?.message === 'string' && res.message ? res.message : t('serverPluginPage.actionOk')
    void MessagePlugin.success(msg)
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    acting.value = false
  }
}

function backToChat() {
  void router.push('/chat')
}
</script>

<template>
  <div class="mc-server-page">
    <header class="mc-server-page__head">
      <div class="mc-server-page__heading">
        <span v-if="page?.icon" class="mc-server-page__icon" aria-hidden="true">{{ page.icon }}</span>
        <h1>{{ page?.title ?? t('serverPluginPage.notFoundTitle') }}</h1>
      </div>
      <UiButton variant="outline" size="small" @click="backToChat">{{ t('serverPluginPage.backToChat') }}</UiButton>
    </header>

    <div v-if="loading" class="mc-server-page__state text-placeholder">{{ t('serverPluginPage.loading') }}</div>
    <div v-else-if="!page" class="mc-server-page__state mc-server-page__empty">
      <div class="mc-server-page__empty-mark" aria-hidden="true">◇</div>
      <p class="mc-server-page__empty-title">{{ t('serverPluginPage.notFoundTitle') }}</p>
      <p class="mc-server-page__empty-hint">{{ t('serverPluginPage.notFoundHint') }}</p>
      <UiButton theme="primary" size="small" @click="backToChat">{{ t('serverPluginPage.backToChat') }}</UiButton>
    </div>
    <div v-else class="mc-server-page__body">
      <DeclarativeNodes :nodes="nodes" :on-action="onAction" />
    </div>
  </div>
</template>

<style scoped>
.mc-server-page {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: var(--td-bg-color-page);
}

.mc-server-page__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 24px;
  background: var(--td-bg-color-container);
  border-bottom: 1px solid var(--mc-hairline);
}

.mc-server-page__heading {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.mc-server-page__icon {
  flex-shrink: 0;
  font-size: var(--mc-fs-body);
  color: var(--td-brand-color);
}

.mc-server-page__head h1 {
  margin: 0;
  font-size: var(--mc-fs-title);
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-server-page__body {
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

.mc-server-page__state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 48px 24px;
  font-size: var(--mc-fs-sm);
}

.mc-server-page__empty-mark {
  font-size: 32px;
  color: var(--td-text-color-placeholder);
}

.mc-server-page__empty-title {
  margin: 0;
  font-size: var(--mc-fs-body);
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.mc-server-page__empty-hint {
  margin: 0 0 8px;
  color: var(--td-text-color-secondary);
  text-align: center;
}

@media (max-width: 768px) {
  .mc-server-page__head {
    padding: 12px 16px;
  }

  .mc-server-page__body {
    padding: 16px;
  }
}
</style>
