<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import UiIcon from '@/components/UiIcon.vue'
import { currentLang } from '@/i18n'
import { CHAT_APP, REPO } from '@/links'

// 插件市场样式随路由分包加载
import '@/styles/market.css'

const { t } = useI18n()

interface MarketPlugin {
  file: string
  name: string
  version: string
  cat: 'demo'
  zh: string
  en: string
  perms: string[]
}

/** index.json 条目：由 scripts/sync-plugins.mjs 从 plugins/examples 的 manifest 生成 */
interface IndexEntry {
  file: string
  name: string
  version: string
  description: string
  permissions: string[]
  allowedHosts: string[]
  lang: 'js' | 'ts'
}

/**
 * 内置目录：只有中英文案与分类是人工策划的；name / version / perms
 * 运行时以 index.json（manifest 真源）为准——新增示例没配文案时
 * 用 manifest 描述兜底，版本号永远不会再手写漂移。
 */
const PLUGINS: MarketPlugin[] = [
  {
    file: 'weather.plugin.js',
    name: 'weather-demo',
    version: '1.0.0',
    cat: 'demo',
    zh: '侧边栏天气面板，并把天气作为自定义 weather 消息类型发送到会话。',
    en: 'Weather side panel that also posts weather as a custom message type.',
    perms: ['message:read', 'message:send', 'message:render', 'ui:panel', 'storage'],
  },
  {
    file: 'dice.plugin.js',
    name: 'dice',
    version: '1.0.0',
    cat: 'demo',
    zh: '掷骰子面板，结果以自定义 dice 消息发到当前会话，气泡里直接出点数。',
    en: 'Dice panel that posts rolls as custom dice messages rendered in the bubble.',
    perms: ['message:read', 'message:send', 'message:render', 'ui:panel'],
  },
  {
    file: 'word-count.plugin.js',
    name: 'word-count',
    version: '1.0.0',
    cat: 'demo',
    zh: '监听消息事件，统计消息数与字数，侧边栏实时展示并本地持久化。',
    en: 'Listens to message events, counts messages and characters, persisted locally.',
    perms: ['message:read', 'ui:panel', 'storage'],
  },
  {
    file: 'search-kit.plugin.js',
    name: 'search-kit',
    version: '1.0.0',
    cat: 'demo',
    zh: 'SDK v2 示例：会话列表、全局消息搜索、草稿/剪贴板与存储管理，使用 textarea/checkbox/switch/segmented 控件。',
    en: 'SDK v2 demo: conversation list, global message search, draft/clipboard and storage management with textarea/checkbox/switch/segmented controls.',
    perms: ['ui:panel', 'conv:read', 'message:read', 'ui:host', 'storage'],
  },
  {
    file: 'page-kit.plugin.js',
    name: 'page-kit',
    version: '1.0.0',
    cat: 'demo',
    zh: 'SDK v3 示例：注册带表单的完整插件页面，并用账号级服务端数据保存便签。',
    en: 'SDK v3 demo: register a full plugin page with a form and persist notes in account-level server data.',
    perms: ['ui:panel', 'ui:page', 'ui:host', 'data'],
  },
  {
    file: 'ai-chat.plugin.js',
    name: 'ai-chat',
    version: '1.0.0',
    cat: 'demo',
    zh: 'SDK v4 示例：全站 AI 对话页面，密钥由服务端代理保管，插件只收发消息。',
    en: 'SDK v4 demo: a full AI chat page using the server-side proxy, so the API key never reaches the browser.',
    perms: ['ui:page', 'ai'],
  },
  {
    file: 'conv-stats.plugin.ts',
    name: 'conv-stats',
    version: '1.0.0',
    cat: 'demo',
    zh: 'TypeScript 示例：当前会话消息统计面板 + 消息右键菜单动作，演示 table/progress/badge 控件（.ts 可直接上传，服务端自动转译）。',
    en: 'TypeScript demo: per-conversation message statistics panel plus a message context-menu action using table/progress/badge controls (.ts uploads directly; the server transpiles it).',
    perms: ['ui:panel', 'conv:read', 'ui:actions', 'ui:host'],
  },
  {
    file: 'power-kit.plugin.js',
    name: 'power-kit',
    version: '1.0.0',
    cat: 'demo',
    zh: 'SDK v5 示例：会话管理、消息历史、文件上传与 tabs/steps/empty/loading/upload 控件。',
    en: 'SDK v5 demo: conversation management, message history, file upload and tabs/steps/empty/loading/upload controls.',
    perms: ['ui:panel', 'ui:page', 'conv:read', 'conv:write', 'message:send', 'file:upload'],
  },
]

const keyword = ref('')
const cat = ref<'all' | 'demo'>('all')

/** manifest 真源（构建时由 sync-plugins.mjs 生成）；拉取失败回退内置清单 */
const remote = ref<IndexEntry[]>([])

onMounted(async () => {
  try {
    const res = await fetch(`${import.meta.env.BASE_URL}content/plugins/index.json`, { cache: 'no-cache' })
    if (!res.ok) return
    const data = (await res.json()) as { plugins?: IndexEntry[] }
    if (Array.isArray(data.plugins)) remote.value = data.plugins
  } catch {
    /* 回退到内置 PLUGINS */
  }
})

/** 内置文案与 index.json 合并：名称/版本/权限以 manifest 为准 */
const plugins = computed<MarketPlugin[]>(() => {
  const byFile = new Map(remote.value.map((e) => [e.file, e]))
  const seen = new Set<string>()
  const out = PLUGINS.map((p) => {
    seen.add(p.file)
    const r = byFile.get(p.file)
    return r ? { ...p, name: r.name, version: r.version, perms: r.permissions } : p
  })
  for (const r of remote.value) {
    if (seen.has(r.file)) continue
    out.push({
      file: r.file, name: r.name, version: r.version, cat: 'demo',
      zh: r.description, en: r.description, perms: r.permissions,
    })
  }
  return out
})

const cats = computed(() => [
  { value: 'all' as const, label: t('mkt.cat.all') },
  { value: 'demo' as const, label: t('mkt.cat.demo') },
])

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  return plugins.value.filter((p) => {
    if (cat.value !== 'all' && p.cat !== cat.value) return false
    if (!k) return true
    const desc = currentLang() === 'zh' ? p.zh : p.en
    return [p.name, desc, ...p.perms].some((s) => s.toLowerCase().includes(k))
  })
})

const steps = computed(() => [
  { n: '1', title: t('mkt.step1-t'), desc: t('mkt.step1-d') },
  { n: '2', title: t('mkt.step2-t'), desc: t('mkt.step2-d') },
  { n: '3', title: t('mkt.step3-t'), desc: t('mkt.step3-d') },
])

function descOf(p: MarketPlugin) {
  return currentLang() === 'zh' ? p.zh : p.en
}

// 静态资源放在 content/ 下：/plugins 是前端路由，不能同时是目录
function hrefOf(p: MarketPlugin) {
  return `${import.meta.env.BASE_URL}content/plugins/${p.file}`
}

/**
 * 一键安装深链：把本站插件 URL 带进聊天应用。
 * 插件 URL 用本站 origin（服务端去拉），应用地址用 CHAT_APP；
 * 未登录时聊天应用的路由守卫会带 redirect 先去登录，回来继续装。
 */
function installHref(p: MarketPlugin) {
  const pluginUrl = `${location.origin}${hrefOf(p)}`
  const q = new URLSearchParams({ 'plugin-install': pluginUrl })
  if (p.name) q.set('plugin-name', p.name)
  return `${CHAT_APP}chat?${q.toString()}`
}

function sourceHref(p: MarketPlugin) {
  return `${REPO}/blob/main/plugins/examples/${p.file}`
}
</script>

<template>
  <main class="container mkt">
    <header class="mkt__head">
      <p class="eyebrow">{{ t('nav.plugins') }}</p>
      <h1>{{ t('mkt.h1') }}</h1>
      <p>{{ t('mkt.sub') }}</p>
    </header>

    <section class="mkt__tools">
      <label class="field">
        <UiIcon name="search" :size="15" />
        <input v-model="keyword" type="search" :placeholder="t('mkt.search')" />
      </label>

      <div class="mkt__cats">
        <button
          v-for="c in cats"
          :key="c.value"
          class="tabs__btn"
          :class="{ 'is-on': cat === c.value }"
          type="button"
          @click="cat = c.value"
        >
          {{ c.label }}
        </button>
      </div>

      <span class="mkt__count">{{ filtered.length }} / {{ plugins.length }}</span>
    </section>

    <section class="mkt__grid">
      <article v-for="p in filtered" :key="p.file" class="mkt__card">
        <header class="mkt__card-head">
          <span class="mkt__icon"><UiIcon name="puzzle" :size="18" /></span>
          <div>
            <h3>{{ p.name }}</h3>
            <span class="mkt__ver mono">v{{ p.version }}</span>
          </div>
          <span class="pill pill--brand mkt__badge">{{ t('mkt.official') }}</span>
        </header>

        <p>{{ descOf(p) }}</p>

        <div class="mkt__perms">
          <span
            v-for="perm in p.perms"
            :key="perm"
            class="tag-perm"
            :class="{ 'tag-perm--net': perm === 'net' }"
          >
            {{ perm }}
          </span>
        </div>

        <footer class="mkt__card-actions">
          <a class="btn btn--primary btn--sm" :href="installHref(p)">
            <UiIcon name="puzzle" :size="14" />
            {{ t('mkt.install') }}
          </a>
          <a class="btn btn--quiet btn--sm" :href="hrefOf(p)" download>
            <UiIcon name="download" :size="14" />
            {{ t('mkt.download') }}
          </a>
          <a class="btn btn--quiet btn--sm" :href="sourceHref(p)" target="_blank" rel="noopener">
            {{ t('mkt.source') }}
          </a>
        </footer>
      </article>
    </section>

    <p v-if="!filtered.length" class="mkt__empty">{{ t('mkt.empty') }}</p>

    <section class="mkt__how">
      <h2>{{ t('mkt.how-title') }}</h2>
      <div class="steps">
        <div v-for="s in steps" :key="s.n" class="step">
          <span class="step__n">{{ s.n }}</span>
          <h3>{{ s.title }}</h3>
          <p>{{ s.desc }}</p>
        </div>
      </div>
    </section>

    <section class="mkt__pub">
      <div>
        <h2>{{ t('mkt.pub-title') }}</h2>
        <p>
          {{ t('mkt.pub-short') }}
          <RouterLink to="/docs/design.md">{{ t('nav.docs') }}</RouterLink>
        </p>
      </div>
      <a class="btn btn--ghost" :href="REPO" target="_blank" rel="noopener">
        {{ t('mkt.pub-btn') }}
        <UiIcon name="arrow-up-right" :size="15" />
      </a>
    </section>
  </main>
</template>
