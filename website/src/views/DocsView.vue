<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import DOMPurify from 'dompurify'

import { currentLang } from '@/i18n'

// 文档页样式随路由分包加载
import '@/styles/docs.css'

/**
 * marked 动态导入：文档解析器只进入 /docs 分包，不阻塞首页。
 * 模块级 Promise 缓存，切换文档不重复 import。
 */
let markedPromise: Promise<typeof import('marked')['marked']> | null = null
function loadMarked() {
  if (!markedPromise) markedPromise = import('marked').then((m) => m.marked)
  return markedPromise
}

/** 让出主线程：大文档解析放到空闲时段，避免切换文档时卡住输入 */
function whenIdle(): Promise<void> {
  return new Promise((resolve) => {
    if (typeof window.requestIdleCallback === 'function') {
      window.requestIdleCallback(() => resolve(), { timeout: 200 })
    } else {
      setTimeout(resolve, 0)
    }
  })
}

type Lang = 'zh' | 'en'

interface DocItem {
  group?: boolean
  file?: string
  zh: string
  en: string
  descZh?: string
  descEn?: string
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

/** 文档目录：与 docs/ 下的源文件对应（由 scripts/sync-docs 同步到 public/docs） */
const DOCS: DocItem[] = [
  { group: true, zh: '入门', en: 'Getting started' },
  {
    file: 'getting-started.md',
    zh: '项目总览',
    en: 'Project overview',
    descZh: '定位、快速开始、插件开发与升级部署',
    descEn: 'Positioning, quick start, plugin dev, upgrades',
  },
  {
    file: 'changelog.md',
    zh: '更新日志',
    en: 'Changelog',
    descZh: '各版本新增、修复与性能变更记录',
    descEn: 'Features, fixes and perf changes per release',
  },
  {
    file: 'client-guide.md',
    zh: '客户端接入指南',
    en: 'Client integration guide',
    descZh: '桌面 / 移动 / SSO / Hub 接入主站的来源登记、登录、WS 与会话管理',
    descEn: 'Origin setup, auth, WS and sessions for desktop / mobile / SSO / Hub',
  },
  {
    file: 'ecosystem.md',
    zh: '项目组架构与集成',
    en: 'Ecosystem & integration',
    descZh: '核心与组件关系、登录拓扑、契约金样本、部署组合与发版协同',
    descEn: 'Core/components, login topology, contracts, deploys & releases',
  },
  { group: true, zh: '设计与架构', en: 'Design & architecture' },
  {
    file: 'design.md',
    zh: '设计文档',
    en: 'Design doc',
    descZh: '产品定位、架构、数据库、WS 协议、插件系统',
    descEn: 'Product, architecture, DB, WS protocol, plugins',
  },
  {
    file: 'api-spec.md',
    zh: '后端需求规格',
    en: 'Backend API spec',
    descZh: '接口契约、校验规则、错误码、验收标准',
    descEn: 'Contracts, validation, error codes, acceptance',
  },
  {
    file: 'api-reference.md',
    zh: 'API 文档',
    en: 'API reference',
    descZh: 'REST + WebSocket 全量端点、参数、响应与错误码速查',
    descEn: 'Full REST + WebSocket endpoints, params, responses & errors',
  },
  {
    file: 'frontend-guide.md',
    zh: '前端技术方案',
    en: 'Frontend guide',
    descZh: '结构、状态管理、WS 链路、性能约定与踩坑',
    descEn: 'Structure, state, WS, performance notes & pitfalls',
  },
  {
    file: 'ui-design.md',
    zh: 'UI 设计方案',
    en: 'UI design',
    descZh: '设计令牌、动效规范、组件手感与验收标准',
    descEn: 'Design tokens, motion, component feel and acceptance criteria',
  },
  {
    file: 'plugin-guide.md',
    zh: '插件开发',
    en: 'Plugin development',
    descZh: '用户插件 / 服务端 WASM 插件的 manifest、权限、沙箱与宿主 API',
    descEn: 'Manifests, permissions, sandbox and host APIs for user and WASM plugins',
  },
  { group: true, zh: '运维与子项目', en: 'Ops & subprojects' },
  {
    file: 'ops-guide.md',
    zh: '运维手册',
    en: 'Ops manual',
    descZh: '环境变量全表、部署、备份升级、排障 FAQ',
    descEn: 'Env vars, deploy, backup & upgrade, troubleshooting',
  },
  {
    file: 'security-migration-guide.md',
    zh: '安全与迁移指南',
    en: 'Security & migration guide',
    descZh: '附件鉴权、认证与登出、实时通道、插件安全、迁移与部署',
    descEn: 'Attachment auth, sessions, realtime, plugin security, migrations',
  },
  {
    file: 'storage-providers.md',
    zh: '对象存储接入',
    en: 'Object storage providers',
    descZh: '服务商预设、端点/区域/寻址风格、连接自检与已知边界',
    descEn: 'Provider presets, endpoint/region/addressing, connection test & limits',
  },
  {
    file: 'server-readme.md',
    zh: '后端运行手册',
    en: 'Server runbook',
    descZh: '配置、迁移、测试、目录结构与覆盖率',
    descEn: 'Config, migrations, tests, structure & coverage',
  },
  {
    file: 'web-readme.md',
    zh: '前端结构速览',
    en: 'Web structure',
    descZh: '目录、命令、聊天界面与设计约定',
    descEn: 'Layout, commands, chat UI & design conventions',
  },
  {
    file: 'website-readme.md',
    zh: '官网运行手册',
    en: 'Website runbook',
    descZh: '官网结构、命令、文档同步与插件市场维护',
    descEn: 'Website structure, commands, docs sync and plugin market upkeep',
  },
  {
    file: 'legal-readme.md',
    zh: '协议维护说明',
    en: 'Legal docs maintenance',
    descZh: '用户协议 / 隐私协议的合规提示与运营主体替换',
    descEn: 'Compliance notes and operator details for Terms / Privacy',
  },
]

const items = DOCS.filter((d) => !!d.file)
const lang = computed<Lang>(() => currentLang())

function label(item: DocItem) {
  return lang.value === 'zh' ? item.zh : item.en
}
function descOf(item: DocItem) {
  return lang.value === 'zh' ? (item.descZh ?? '') : (item.descEn ?? '')
}

const activeFile = computed(() => {
  const f = route.params.file as string | undefined
  return f && items.some((d) => d.file === f) ? f : items[0].file!
})

const activeIndex = computed(() => items.findIndex((d) => d.file === activeFile.value))
const prevItem = computed(() => (activeIndex.value > 0 ? items[activeIndex.value - 1] : null))
const nextItem = computed(() =>
  activeIndex.value >= 0 && activeIndex.value < items.length - 1 ? items[activeIndex.value + 1] : null,
)

const html = ref('')
const loading = ref(true)
const failed = ref(false)

/**
 * 仓库内相对链接 → 站内文档路由。
 * key 用「仓库根相对路径」而非 basename：server/README.md 与 web/README.md
 * 同名，按 basename 映射会把两者都跳到 getting-started.md。
 */
const LINK_MAP: Record<string, string> = {
  'README.md': 'getting-started.md',
  'CHANGELOG.md': 'changelog.md',
  'docs/客户端接入指南.md': 'client-guide.md',
  'docs/项目组架构与集成.md': 'ecosystem.md',
  'docs/设计文档.md': 'design.md',
  'docs/后端需求规格.md': 'api-spec.md',
  'docs/API文档.md': 'api-reference.md',
  'docs/前端技术方案.md': 'frontend-guide.md',
  'docs/运维手册.md': 'ops-guide.md',
  'docs/安全与迁移指南.md': 'security-migration-guide.md',
  'docs/storage-providers.md': 'storage-providers.md',
  'docs/UI设计方案.md': 'ui-design.md',
  'docs/插件开发.md': 'plugin-guide.md',
  'docs/legal/README.md': 'legal-readme.md',
  'server/README.md': 'server-readme.md',
  'web/README.md': 'web-readme.md',
  'website/README.md': 'website-readme.md',
}

/** 把仓库内 md 链接解析为站内文档文件名；未知链接返回 null（不静默跳错页）。 */
function resolveDocLink(href: string): string | null {
  // href 形如 docs/设计文档.md、server/README.md、../web/README.md 或裸 filename.md
  const clean = href.replace(/^\.?\//, '').split('#')[0]
  if (LINK_MAP[clean]) return LINK_MAP[clean]
  // 兼容只写文件名的旧链接：在映射表里按后缀再找一次（仅当唯一时安全）
  const base = clean.split('/').pop() ?? clean
  if (base === clean) return null
  const candidates = Object.entries(LINK_MAP).filter(([key]) => key.endsWith('/' + base))
  return candidates.length === 1 ? candidates[0][1] : null
}

async function loadDoc() {
  loading.value = true
  failed.value = false
  html.value = ''

  // 文档实体放在 content/docs 下：/docs/xxx.md 留给前端路由，
  // 否则静态服务器会直接返回 md 原文，刷新即"裸 Markdown"
  const file = activeFile.value
  const enPath = `content/docs/en/${file}`
  const zhPath = `content/docs/${file}`
  const path = lang.value === 'en' ? enPath : zhPath

  try {
    const res = await fetch(`${import.meta.env.BASE_URL}${path}`)
    if (!res.ok) {
      // 英文缺失时回退中文
      if (lang.value === 'en') {
        const fallback = await fetch(`${import.meta.env.BASE_URL}${zhPath}`)
        if (!fallback.ok) throw new Error(String(fallback.status))
        html.value = await renderMd(await fallback.text(), true)
      } else {
        throw new Error(String(res.status))
      }
    } else {
      html.value = await renderMd(await res.text(), false)
    }
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

async function renderMd(md: string, fallbackNote: boolean) {
  // 大文档（changelog ~98KB）解析是长任务：先让出主线程，再按需加载解析器。
  await whenIdle()
  const marked = await loadMarked()
  // 文档自身的 h1 与页面标题重复，去掉首个 h1（正文从 h2 开始层级也正确）
  const body = (marked.parse(md, { async: false }) as string)
    .replace(/^\s*<h1[^>]*>[\s\S]*?<\/h1>/, '')
    // 表格套一层滚动容器：宽表（环境变量全表）在窄屏上横向滚动，而不是把页面撑宽
    .replace(/<table>/g, '<div class="md-table"><table>')
    .replace(/<\/table>/g, '</table></div>')
  // 仓库内 md 链接改写为站内路由（点击由 onBodyClick 走前端路由）。
  // 未收录的文档不改写：留在原地可看出是外部/相对链接，而不是静默跳回「项目总览」。
  const rewritten = body.replace(/href="([^"]+\.md)(#[^"]*)?"/g, (_m, file: string, hash = '') => {
    const mapped = resolveDocLink(file)
    return mapped ? `href="/docs/${mapped}${hash ?? ''}"` : `href="${file}${hash ?? ''}"`
  })
  // 净化：marked 不净化输出 HTML，用 DOMPurify 去除 <script>、javascript: 链接、
  // 事件处理器等危险内容，防止文档被篡改后的存储型 XSS
  const safe = DOMPurify.sanitize(rewritten, { USE_PROFILES: { html: true } })
  return fallbackNote ? `<p class="docs-fallback">${t('docs.enMissing')}</p>${safe}` : safe
}

/** 文档内跳转走前端路由，避免整页刷新（静态托管下会 404） */
function onBodyClick(e: MouseEvent) {
  const link = (e.target as HTMLElement | null)?.closest?.('a')
  if (!link) return
  const href = link.getAttribute('href') ?? ''
  if (/^\/docs\/[\w.-]+\.md(#.*)?$/.test(href)) {
    e.preventDefault()
    void router.push(href)
  }
}

watch([activeFile, lang], loadDoc, { immediate: true })

// 兼容旧的 #/docs/xxx.md 锚点写法
watch(
  () => route.hash,
  (hash) => {
    const m = hash.match(/^#\/docs\/(.+\.md)$/)
    if (m) void router.push({ path: `/docs/${m[1]}` })
  },
)
</script>

<template>
  <main class="container doc">
    <aside class="doc__side">
      <nav class="doc__nav" aria-label="Docs">
        <template v-for="(item, i) in DOCS" :key="i">
          <div v-if="item.group" class="doc__group">{{ label(item) }}</div>
          <RouterLink v-else :to="`/docs/${item.file}`" class="doc__link" :class="{ 'is-on': item.file === activeFile }">
            {{ label(item) }}
          </RouterLink>
        </template>
      </nav>
      <p class="doc__side-note">{{ t('docs.zhNote') }}</p>
    </aside>

    <article class="doc__main">
      <p class="doc__crumb">{{ t('docs.crumb') }}</p>
      <h1 class="doc__title">{{ label(items[activeIndex] ?? items[0]) }}</h1>
      <p class="doc__desc">{{ descOf(items[activeIndex] ?? items[0]) }}</p>

      <div class="md" @click="onBodyClick">
        <p v-if="loading" class="doc__loading">{{ t('docs.loading') }}</p>
        <p v-else-if="failed" class="doc__loading">{{ t('docs.failed') }}</p>
        <!-- eslint-disable-next-line vue/no-v-html -->
        <div v-else v-html="html" />
      </div>

      <nav class="doc__pager">
        <button
          v-if="prevItem"
          class="btn btn--ghost btn--sm"
          type="button"
          @click="router.push(`/docs/${prevItem.file}`)"
        >
          ← {{ label(prevItem) }}
        </button>
        <span v-else />
        <button
          v-if="nextItem"
          class="btn btn--ghost btn--sm"
          type="button"
          @click="router.push(`/docs/${nextItem.file}`)"
        >
          {{ label(nextItem) }} →
        </button>
      </nav>
    </article>
  </main>
</template>
