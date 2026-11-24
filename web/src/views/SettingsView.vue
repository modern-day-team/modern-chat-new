<script setup lang="ts">
import UiSlider from '@/components/ui/UiSlider.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import UiSwitch from '@/components/ui/UiSwitch.vue'
import UiTag from '@/components/ui/UiTag.vue'
import SettingsRow from '@/components/ui/SettingsRow.vue'
import SettingsSegmented from '@/components/ui/SettingsSegmented.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { DialogPlugin, MessagePlugin } from '@/components/ui/feedback'
import {
  ArrowLeftIcon,
  CheckIcon,
  ChevronRightIcon,
  CopyIcon,
  DataIcon,
  DesktopIcon,
  ExtensionIcon,
  InfoCircleIcon,
  LockOnIcon,
  MobileIcon,
  NotificationIcon,
  PaletteIcon,
  RefreshIcon,
  SettingIcon,
  TabletIcon,
  UserIcon,
} from 'tdesign-icons-vue-next'

import AppBackgroundPicker from '@/components/common/AppBackgroundPicker.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import {
  notificationsEnabled,
  notificationsSupported,
  pushEnabled,
  pushSupported,
  setNotificationsEnabled,
  setPushEnabled,
} from '@/composables/notifications'
import {
  clearStaticCache,
  formatBytes,
  measureLocalCache,
  type LocalCacheUsage,
} from '@/composables/localCache'
import QRCode from 'qrcode'

import { authApi, systemApi, type TwofaStatus, type VersionInfo } from '@/core/network/endpoints'
import type { UserSession } from '@/types/proto'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFriendsStore } from '@/stores/friends'
import { PANE_DEFAULTS, PANE_LIMITS, useLayoutStore, type PaneKey } from '@/stores/layout'
import { usePrefsStore, type Theme } from '@/stores/prefs'
import { useWsStore } from '@/stores/ws'

const router = useRouter()
const prefs = usePrefsStore()
const auth = useAuthStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const layout = useLayoutStore()
const ws = useWsStore()
const { t, te } = useAppI18n()

// ===== 锚点导航（≥1200px 双栏，窄屏隐藏）=====
const settingsSections = [
  'appearance',
  'notifications',
  'account-profile',
  'sessions',
  'twofa',
  'layout',
  'cache',
  'extensions',
  'about',
  'danger',
] as const
type SettingsSectionId = (typeof settingsSections)[number]
const activeSection = ref<SettingsSectionId>('appearance')
let scrollFrame = 0

/** 顶部阈值以下最后一个区块即当前区块；比 IntersectionObserver 更稳定。 */
function syncActiveSection() {
  // 最后一节在页面底部可能无法滚到顶部阈值，触底时直接选中它
  if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 8) {
    activeSection.value = settingsSections.at(-1) ?? settingsSections[0]
    return
  }
  let current: SettingsSectionId = settingsSections[0]
  for (const id of settingsSections) {
    const el = document.getElementById(id)
    if (el && el.getBoundingClientRect().top <= 96) current = id
    else break
  }
  activeSection.value = current
}

function onSettingsScroll() {
  if (scrollFrame) return
  scrollFrame = window.requestAnimationFrame(() => {
    syncActiveSection()
    scrollFrame = 0
  })
}

function observeSections() {
  window.addEventListener('scroll', onSettingsScroll, { passive: true })
  syncActiveSection()
}

function goToSection(id: SettingsSectionId) {
  activeSection.value = id
  document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

type SettingsNavItem = { id: SettingsSectionId; label: string }
const navGroups = computed<Array<{ title: string; items: SettingsNavItem[] }>>(() => [
  {
    title: t('settings.navPersonalization'),
    items: [
      { id: 'appearance', label: t('settings.navAppearance') },
      { id: 'notifications', label: t('settings.navNotifications') },
      { id: 'layout', label: t('settings.navLayout') },
    ],
  },
  {
    title: t('settings.navAccountSecurity'),
    items: [
      { id: 'account-profile', label: t('settings.navProfile') },
      { id: 'sessions', label: t('settings.navSessions') },
      { id: 'twofa', label: t('settings.navTwofa') },
      { id: 'danger', label: t('settings.navDanger') },
    ],
  },
  {
    title: t('settings.navDataExtensions'),
    items: [
      { id: 'cache', label: t('settings.navCache') },
      { id: 'extensions', label: t('settings.navPlugins') },
    ],
  },
  {
    title: t('settings.navAbout'),
    items: [{ id: 'about', label: t('settings.navAbout') }],
  },
])

// ===== 桌面通知 =====
const notifyOn = ref(notificationsEnabled())
const notifySupported = notificationsSupported()

// ===== 本地缓存 =====
const cacheUsage = ref<LocalCacheUsage | null>(null)
const cacheBusy = ref(false)

async function refreshCacheUsage() {
  try {
    cacheUsage.value = await measureLocalCache()
  } catch {
    cacheUsage.value = null
  }
}

function confirmClearCache() {
  const dialog = DialogPlugin.confirm({
    header: t('settings.cacheClearConfirmTitle'),
    body: t('settings.cacheClearConfirmBody'),
    theme: 'warning',
    onConfirm: async () => {
      dialog.destroy()
      cacheBusy.value = true
      try {
        await clearStaticCache()
        await refreshCacheUsage()
        void MessagePlugin.success(t('settings.cacheCleared'))
      } catch {
        void MessagePlugin.error(t('settings.cacheClearFailed'))
      } finally {
        cacheBusy.value = false
      }
    },
  })
}

// ===== 离线推送（Web Push）=====
const pushOn = ref(pushEnabled())
const pushIsSupported = pushSupported()

async function onPushToggle(v: unknown) {
  const on = v === true
  if (on && !pushIsSupported) {
    void MessagePlugin.warning(t('settings.pushUnsupported'))
    return
  }
  try {
    const ok = await setPushEnabled(on)
    pushOn.value = ok
    if (on && !ok) {
      void MessagePlugin.error(t('settings.pushUnavailable'))
    } else {
      void MessagePlugin.success(on ? t('settings.pushOn') : t('settings.pushOff'))
    }
  } catch {
    pushOn.value = false
    void MessagePlugin.error(t('settings.pushUnavailable'))
  }
}

async function onNotifyToggle(v: unknown) {
  const on = v === true
  if (on && !notifySupported) {
    void MessagePlugin.warning(t('settings.notifyUnsupported'))
    return
  }
  try {
    const ok = await setNotificationsEnabled(on)
    notifyOn.value = ok
    if (on && !ok) {
      void MessagePlugin.error(t('settings.notifyDenied'))
    } else {
      void MessagePlugin.success(on ? t('settings.notifyOn') : t('settings.notifyOff'))
    }
  } catch {
    // requestPermission 在部分浏览器/隐私模式下会 reject：给出明确失败提示
    notifyOn.value = false
    void MessagePlugin.error(t('settings.notifyDenied'))
  }
}

// 直接打开/刷新设置页时不会经过聊天页的初始化，auth.user 为空 →
// 账号卡片会误显示「登录后可修改昵称与邮箱」。这里自行补一次资料拉取。
onMounted(async () => {
  void refreshCacheUsage()
  void loadTwofa()
  void loadSessions()
  await nextTick()
  observeSections()
  if (!auth.token || auth.user) return
  try {
    await auth.fetchMe()
  } catch {
    /* 令牌失效由请求拦截器统一处理 */
  }
})

// ===== 登录会话（L3 设置页：低频、仅本人）=====

const sessions = ref<UserSession[]>([])
const sessionsLoading = ref(false)

async function loadSessions() {
  if (!auth.token) return
  sessionsLoading.value = true
  try {
    const r = await authApi.sessions()
    sessions.value = r.sessions ?? []
  } catch {
    sessions.value = []
  } finally {
    sessionsLoading.value = false
  }
}

const sessionPlatformLabel: Record<UserSession['platform'], string> = {
  web: '网页',
  desktop: '桌面端',
  mobile: '手机',
  tablet: '平板',
  other: '其他',
}

function sessionLabel(s: UserSession): string {
  const part = sessionPlatformLabel[s.platform] ?? '其他'
  const client = s.clientType?.replace(/\s*[·•]\s*/g, ' · ').trim() ?? ''
  // 旧会话可能存成 "Chrome Windows"，补回分隔符但保持原始值可读
  const normalized = /^[A-Za-z]+\s+[A-Za-z]+$/.test(client) ? client.replace(' ', ' · ') : client
  return normalized && normalized !== 'browser' ? `${part} · ${normalized}` : part
}

function sessionIcon(s: UserSession) {
  if (s.platform === 'mobile') return MobileIcon
  if (s.platform === 'tablet') return TabletIcon
  if (s.platform === 'desktop') return DesktopIcon
  return PaletteIcon
}

function sessionIp(s: UserSession): string {
  if (!s.ip || s.ip === '::1' || s.ip === '127.0.0.1') return t('settings.localMachine')
  return s.ip
}

/** 会话时间：今天/昨天显示相对日期，更早显示本地日期，原始 ISO 放 title。 */
function sessionTime(iso: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const now = new Date()
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const start = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  const dayDiff = Math.round((startOfToday - start) / 86_400_000)
  const time = d.toLocaleTimeString(prefs.lang === 'zh-CN' ? 'zh-CN' : 'en-US', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
  if (dayDiff === 0) return t('settings.timeToday', { time })
  if (dayDiff === 1) return t('settings.timeYesterday', { time })
  return d.toLocaleString(prefs.lang === 'zh-CN' ? 'zh-CN' : 'en-US', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

function confirmRevokeSession(s: UserSession) {
  const dialog = DialogPlugin.confirm({
    header: t('settings.sessionRevoke'),
    body: t('settings.sessionRevokeConfirm'),
    confirmBtn: { content: t('settings.sessionRevoke'), theme: 'danger' },
    onConfirm: async () => {
      try {
        await authApi.revokeSession(s.id)
        await loadSessions()
        // 撤销当前设备：按服务端口径立即清本地登录态
        if (s.current) {
          auth.logout()
          ws.disconnect()
          convStore.reset()
        }
        void MessagePlugin.success(t('settings.sessionRevoked'))
      } catch (e) {
        void MessagePlugin.error(te(e) || t('auth.opFailed'))
      }
      dialog.destroy()
    },
  })
}

// ===== 两步验证（默认关闭，设置页开启 / 关闭）=====

const twofa = ref<TwofaStatus>({ enabled: false, method: '', email: false })
/** '' = 未在流程中；choose = 选方式；app / email = 各自开启流程；recovery = 展示恢复码 */
const twofaSetup = ref<'' | 'choose' | 'app' | 'email' | 'recovery'>('')
const twofaSecret = ref('')
const twofaOtpauthImg = ref('')
const twofaSetupCode = ref('')
const twofaEnabling = ref(false)
const twofaRecovery = ref<string[]>([])
const twofaEmailCd = ref(0)
const twofaOffPassword = ref('')
const twofaDisabling = ref(false)
let twofaCdTimer: ReturnType<typeof setInterval> | null = null

async function loadTwofa() {
  if (!auth.token) return
  try {
    twofa.value = await authApi.twofaStatus()
  } catch {
    /* 状态拉取失败按未开启展示 */
  }
}

function stopTwofaCd() {
  if (twofaCdTimer) {
    clearInterval(twofaCdTimer)
    twofaCdTimer = null
  }
}
onBeforeUnmount(() => {
  stopTwofaCd()
  window.removeEventListener('scroll', onSettingsScroll)
  if (scrollFrame) window.cancelAnimationFrame(scrollFrame)
})

async function startTwofaSetup(kind: 'app' | 'email') {
  twofaSetup.value = kind
  twofaSetupCode.value = ''
  if (kind !== 'app') return
  try {
    const s = await authApi.twofaTotpSetup()
    twofaSecret.value = s.secret
    twofaOtpauthImg.value = await QRCode.toDataURL(s.otpauthUrl, { width: 180, margin: 1 })
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.setupFailed'))
    twofaSetup.value = ''
  }
}

function cancelTwofaSetup() {
  twofaSetup.value = ''
  twofaSecret.value = ''
  twofaOtpauthImg.value = ''
  twofaSetupCode.value = ''
  twofaRecovery.value = []
}

async function enableTotp() {
  twofaEnabling.value = true
  try {
    const r = await authApi.twofaTotpEnable(twofaSetupCode.value.trim())
    twofaRecovery.value = r.recoveryCodes
    twofaSetup.value = 'recovery'
    await loadTwofa()
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.verifyFailed'))
  } finally {
    twofaEnabling.value = false
  }
}

async function sendTwofaSetupCode() {
  try {
    const r = await authApi.twofaEmailCode()
    twofaEmailCd.value = r.cooldownSeconds ?? 60
    void MessagePlugin.success(t('auth.codeSent'))
    stopTwofaCd()
    twofaCdTimer = setInterval(() => {
      if (twofaEmailCd.value > 0) twofaEmailCd.value--
      else stopTwofaCd()
    }, 1000)
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.sendFailed'))
  }
}

async function enableEmail() {
  twofaEnabling.value = true
  try {
    const r = await authApi.twofaEmailEnable(twofaSetupCode.value.trim())
    twofaRecovery.value = r.recoveryCodes
    twofaSetup.value = 'recovery'
    await loadTwofa()
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.verifyFailed'))
  } finally {
    twofaEnabling.value = false
  }
}

/** 恢复码以文本文件下载（仅展示一次，用户自行保存） */
function downloadRecovery() {
  const blob = new Blob([twofaRecovery.value.join('\\n')], { type: 'text/plain;charset=utf-8' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = 'modernchat-recovery-codes.txt'
  a.click()
  URL.revokeObjectURL(a.href)
}

/** 启用流程收尾：隐藏恢复码展示 */
function finishTwofaSetup() {
  cancelTwofaSetup()
}

async function disableTwofa() {
  twofaDisabling.value = true
  try {
    await authApi.twofaDisable(twofaOffPassword.value)
    twofaOffPassword.value = ''
    twofaSetup.value = ''
    await loadTwofa()
    void MessagePlugin.success(t('settings.twofaDisabled'))
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.opFailed'))
  } finally {
    twofaDisabling.value = false
  }
}

// ===== 布局（宽度可调 / 区域可改抽屉形态）=====

const layoutPanes = computed(() => [
  { key: 'group' as PaneKey, label: t('settings.layoutGroup') },
  { key: 'conv' as PaneKey, label: t('settings.layoutConv') },
  { key: 'detail' as PaneKey, label: t('settings.layoutDetail') },
])

/** TSlider 单值模式回调 number，区间模式是 number[]，这里统一处理 */
function onWidthChange(key: PaneKey, v: unknown) {
  const n = Array.isArray(v) ? Number(v[0]) : Number(v)
  if (Number.isFinite(n)) layout.setWidth(key, n)
}

function resetPane(key: PaneKey) {
  layout.setWidth(key, PANE_DEFAULTS[key].width)
  layout.setDrawer(key, PANE_DEFAULTS[key].drawer)
}

// ===== 插件 / 站长（低频入口，按信息架构 L3 收在这里）=====

/** 插件面板属于聊天页，带参数回去由 ChatLayout 自动展开 */
function openPlugins() {
  void router.push({ path: '/chat', query: { panel: 'plugins' } })
}

function openAdmin() {
  void router.push('/admin')
}

// ===== 外观 =====

const themeSegments = computed(() => [
  { value: 'auto', label: t('prefs.themeAuto') },
  { value: 'light', label: t('prefs.themeLight') },
  { value: 'dark', label: t('prefs.themeDark') },
])
const languageOptions = computed(() => [
  { value: 'zh-CN', label: t('settings.languageZh') },
  { value: 'en-US', label: t('settings.languageEn') },
])
const dockOptions = computed(() => [
  { value: 'docked', label: t('settings.docked') },
  { value: 'drawer', label: t('settings.drawer') },
])

function onThemeChange(v: string) {
  prefs.setTheme(v as Theme)
}

function onLangChange(v: string) {
  void prefs.setLang(v === 'zh-CN' ? 'zh-CN' : 'en-US')
}

const backgroundPicker = ref<InstanceType<typeof AppBackgroundPicker> | null>(null)
function openBackgroundPicker() {
  backgroundPicker.value?.open()
}

// ===== 账号 =====

const profile = reactive({ nickname: '', email: '', emailCode: '' })
const saving = ref(false)
const profileError = ref('')
const profileState = computed(() => {
  if (profileError.value) return { text: profileError.value, tone: 'danger' as const }
  return profileDirty.value
    ? { text: t('settings.unsavedChanges'), tone: 'warning' as const }
    : { text: t('settings.allChangesSaved'), tone: 'muted' as const }
})

watch(
  () => auth.user,
  (u) => {
    profile.nickname = u?.nickname ?? ''
    profile.email = u?.email ?? ''
    profile.emailCode = ''
    profileError.value = ''
  },
  { immediate: true },
)

/** 邮箱被改动 = 换绑：需要先给新邮箱发验证码 */
const emailChanged = computed(() => {
  const current = auth.user?.email ?? ''
  const next = profile.email.trim()
  return next !== '' && next !== current
})

// ===== 换绑邮箱验证码 =====
const codeSending = ref(false)
const codeCooldown = ref(0)
let codeTimer: number | undefined

function stopCodeTimer() {
  if (codeTimer !== undefined) {
    window.clearInterval(codeTimer)
    codeTimer = undefined
  }
}

function startCooldown(sec: number) {
  codeCooldown.value = Math.min(600, Math.max(1, Math.floor(sec) || 60))
  stopCodeTimer()
  codeTimer = window.setInterval(() => {
    codeCooldown.value -= 1
    if (codeCooldown.value <= 0) stopCodeTimer()
  }, 1000)
}

onBeforeUnmount(stopCodeTimer)

async function sendChangeCode() {
  if (codeSending.value || codeCooldown.value > 0) return
  const email = profile.email.trim()
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    void MessagePlugin.warning(t('auth.validation.emailRequired'))
    return
  }
  codeSending.value = true
  try {
    startCooldown(await auth.sendEmailChangeCode(email))
    void MessagePlugin.success(t('auth.codeSent'))
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.codeSendFailed'))
  } finally {
    codeSending.value = false
  }
}

/** 资料是否有未保存的修改（用于禁用保存按钮，避免无意义的提交） */
const profileDirty = computed(() => {
  const u = auth.user
  if (!u) return false
  return profile.nickname.trim() !== u.nickname || profile.email.trim() !== (u.email ?? '')
})

async function saveProfile() {
  const nickname = profile.nickname.trim()
  if (!nickname) {
    void MessagePlugin.warning(t('settings.nicknameRequired'))
    return
  }
  const u = auth.user
  const email = profile.email.trim()
  // 换绑邮箱必须带验证码：服务端会核销通过才落库（防止占用别人的邮箱）
  if (emailChanged.value && !profile.emailCode.trim()) {
    void MessagePlugin.warning(t('settings.emailCodeRequired'))
    return
  }
  // 只提交真正变化的字段：「只改昵称」不该顺手动邮箱与验证状态
  const patch: { nickname?: string; email?: string; emailCode?: string } = {}
  if (!u || nickname !== u.nickname) patch.nickname = nickname
  if (email !== (u?.email ?? '')) {
    patch.email = email
    if (email) patch.emailCode = profile.emailCode.trim()
  }
  if (!u || (!patch.nickname && patch.email === undefined)) {
    void MessagePlugin.info(t('settings.nothingChanged'))
    return
  }
  saving.value = true
  try {
    await auth.updateMe(patch)
    profile.emailCode = ''
    profileError.value = ''
    void MessagePlugin.success(t('settings.savedOk'))
  } catch (e) {
    profileError.value = te(e) || t('settings.profileSaveFailed')
    void MessagePlugin.error(profileError.value)
  } finally {
    saving.value = false
  }
}

function confirmLogout() {
  const dialog = DialogPlugin.confirm({
    header: t('settings.logoutConfirmTitle'),
    body: t('settings.logoutConfirmBody'),
    confirmBtn: { content: t('settings.logout'), theme: 'danger' },
    onConfirm: () => {
      auth.logout()
      ws.disconnect()
      convStore.reset()
      friendsStore.reset()
      void router.push('/login')
      dialog.destroy()
    },
  })
}

/** 退出所有设备：让后端 token_version+1，所有旧令牌失效；当前端也会退出 */
function confirmLogoutOthers() {
  const dialog = DialogPlugin.confirm({
    header: t('settings.logoutOthers'),
    body: t('settings.logoutOthersConfirm'),
    theme: 'warning',
    onConfirm: async () => {
      try {
        await authApi.logoutOthers()
      } catch (e) {
        // 令牌已经失效时继续走本地登出，避免停在半登录状态
        void MessagePlugin.error(te(e))
      }
      auth.logout()
      ws.disconnect()
      convStore.reset()
      friendsStore.reset()
      void router.push('/login')
      dialog.destroy()
    },
  })
}

// ===== 关于 =====

const build = ref<VersionInfo | null>(null)
const webVersion = `${__WEB_VERSION__} · ${__WEB_COMMIT__}`
const versionText = computed(() => {
  const server = build.value ? `${build.value.version} · ${build.value.commit}` : '—'
  return `${t('settings.webVersion')}: ${webVersion}\n${t('settings.serverVersion')}: ${server}`
})

systemApi
  .version()
  .then((v) => {
    build.value = v
  })
  .catch(() => undefined)

async function copyVersion() {
  try {
    await navigator.clipboard.writeText(versionText.value)
    void MessagePlugin.success(t('settings.versionCopied'))
  } catch {
    void MessagePlugin.error(t('settings.versionCopyFailed'))
  }
}

function openChangelog() {
  window.open('https://github.com/modern-day-team/modern-chat-new/blob/main/CHANGELOG.md', '_blank', 'noopener')
}

function openLegal(path: '/terms' | '/privacy') {
  void router.push(path)
}

function goBack() {
  if (window.history.length > 1) router.back()
  else void router.push(auth.token ? '/chat' : '/login')
}
</script>

<template>
  <div class="settings-page">
    <header class="settings-top">
      <UiButton variant="text" size="small" @click="goBack">
        <template #icon><ArrowLeftIcon /></template>
        {{ t('settings.back') }}
      </UiButton>
      <h1 class="settings-top__title">{{ t('settings.title') }}</h1>
      <span class="settings-top__spacer" />
    </header>

    <div class="settings-layout">
      <aside class="settings-anchors" aria-label="Settings sections">
        <div v-for="group in navGroups" :key="group.title" class="settings-anchors__group">
          <div class="settings-anchors__title">{{ group.title }}</div>
          <button
            v-for="item in group.items"
            :key="item.id"
            type="button"
            class="settings-anchors__link"
            :class="{ 'is-active': activeSection === item.id }"
            @click="goToSection(item.id)"
          >
            {{ item.label }}
          </button>
        </div>
      </aside>

      <div class="settings-body">
        <!-- 外观 -->
        <section id="appearance" class="settings-card settings-section">
          <h2 class="settings-card__title">
            <PaletteIcon class="settings-card__icon" />
            {{ t('settings.appearance') }}
          </h2>

          <SettingsRow :title="t('prefs.language')">
            <SettingsSegmented
              :model-value="prefs.lang"
              :options="languageOptions"
              :aria-label="t('prefs.language')"
              @update:model-value="onLangChange"
            />
          </SettingsRow>

          <SettingsRow :title="t('prefs.theme')">
            <SettingsSegmented
              :model-value="prefs.theme"
              :options="themeSegments"
              :aria-label="t('prefs.theme')"
              @update:model-value="onThemeChange"
            />
          </SettingsRow>

          <SettingsRow
            :title="t('settings.appBackground')"
            :description="t('settings.appBackgroundHint')"
            link
            last
            @click="openBackgroundPicker"
          >
            <span class="settings-link-hint" @click.stop>
              <AppBackgroundPicker ref="backgroundPicker" />
              <ChevronRightIcon class="settings-link-chevron" />
            </span>
          </SettingsRow>
        </section>

        <!-- 桌面通知 -->
        <section id="notifications" class="settings-card settings-section">
          <h2 class="settings-card__title">
            <NotificationIcon class="settings-card__icon" />
            {{ t('settings.notify') }}
          </h2>
          <SettingsRow
            :title="t('settings.notifyLabel')"
            :description="t('settings.notifyHint')"
          >
            <UiSwitch :value="notifyOn" @change="onNotifyToggle" />
          </SettingsRow>
          <!-- 离线推送：应用完全关闭时也能收到消息提醒（需 HTTPS 与浏览器授权） -->
          <SettingsRow
            v-if="auth.token"
            :title="t('settings.pushLabel')"
            :description="t('settings.pushHint')"
            last
          >
            <UiSwitch :value="pushOn" @change="onPushToggle" />
          </SettingsRow>
        </section>

      <!-- 账号资料 -->
      <section id="account-profile" class="settings-card settings-section">
        <h2 class="settings-card__title">
          <UserIcon class="settings-card__icon" />
          {{ t('settings.profileTitle') }}
        </h2>
        <p class="settings-card__hint">{{ t('settings.profileHint') }}</p>

        <template v-if="auth.user">
          <SettingsRow :title="t('settings.nickname')">
            <UiInput
              id="st-nickname"
              v-model="profile.nickname"
              class="settings-input"
              :placeholder="t('settings.nicknamePlaceholder')"
              :maxlength="20"
              clearable
            />
          </SettingsRow>

          <SettingsRow :title="t('settings.email')" :description="t('settings.emailHint')">
            <UiInput
              id="st-email"
              v-model="profile.email"
              class="settings-input"
              :placeholder="t('settings.email')"
              clearable
            />
            <template #footer>
              <!-- 换绑邮箱：必须验证新邮箱归属 -->
              <template v-if="emailChanged">
                <div class="settings-code-row">
                  <UiInput
                    v-model="profile.emailCode"
                    :maxlength="6"
                    :placeholder="t('auth.emailCode')"
                  />
                  <UiButton
                    variant="outline"
                    :loading="codeSending"
                    :disabled="codeCooldown > 0"
                    @click="sendChangeCode"
                  >
                    {{ codeCooldown > 0 ? t('auth.resendIn', { n: codeCooldown }) : t('auth.sendCode') }}
                  </UiButton>
                </div>
                <p class="settings-field__hint">{{ t('settings.emailChangeHint') }}</p>
              </template>
              <div class="settings-actions">
                <span class="settings-save-state" :class="`is-${profileState.tone}`">
                  <CheckIcon v-if="profileState.tone === 'muted'" class="settings-save-state__icon" />
                  {{ profileState.text }}
                </span>
                <UiButton
                  theme="primary"
                  :loading="saving"
                  :disabled="!profileDirty"
                  @click="saveProfile"
                >
                  {{ t('settings.save') }}
                </UiButton>
              </div>
            </template>
          </SettingsRow>
        </template>

        <p v-else class="settings-placeholder">
          {{ t('settings.loginRequired') }}
          <RouterLink to="/login">{{ t('auth.actionLogin') }}</RouterLink>
        </p>
      </section>

      <!-- 登录设备 -->
      <section id="sessions" class="settings-card settings-section">
        <div class="settings-card__head">
          <h2 class="settings-card__title">
            <DesktopIcon class="settings-card__icon" />
            {{ t('settings.sessions') }}
          </h2>
          <UiButton
            variant="text"
            size="small"
            shape="square"
            :aria-label="t('common.refresh')"
            :loading="sessionsLoading"
            @click="loadSessions"
          >
            <template #icon><RefreshIcon /></template>
          </UiButton>
        </div>
        <p class="settings-card__hint">{{ t('settings.sessionsHint') }}</p>

        <div v-if="sessions.length" class="session-list">
          <div v-for="s in sessions" :key="s.id" class="session-item">
            <component :is="sessionIcon(s)" class="session-item__icon" />
            <div class="session-item__text">
              <span class="session-item__title">
                {{ sessionLabel(s) }}
                <UiTag v-if="s.current" theme="primary" variant="light">
                  {{ t('settings.currentDeviceTag') }}
                </UiTag>
              </span>
              <span class="session-item__meta" :title="`${s.appVersion || '-'} · ${s.ip || '-'} · ${s.lastSeenAt}`">
                {{ s.appVersion || '-' }} · {{ sessionIp(s) }} · {{ sessionTime(s.lastSeenAt) }}
              </span>
            </div>
            <UiButton
              variant="text"
              theme="danger"
              size="small"
              :disabled="s.current"
              @click="confirmRevokeSession(s)"
            >
              {{ t('settings.sessionRevoke') }}
            </UiButton>
          </div>
        </div>
        <p v-else-if="!sessionsLoading" class="settings-card__hint">{{ t('settings.sessionsEmpty') }}</p>
      </section>

      <!-- 两步验证（默认关闭，可开可关） -->
      <section id="twofa" class="settings-card settings-section">
        <h2 class="settings-card__title">
          <LockOnIcon class="settings-card__icon" />
          {{ t('settings.twofaTitle') }}
        </h2>
        <p class="settings-card__hint">{{ t('settings.twofaHint') }}</p>

        <template v-if="auth.user">
          <!-- 状态行：卡片标题已说明模块，这里只展示状态 -->
          <SettingsRow :title="t('settings.status')">
            <UiTag :theme="twofa.enabled ? 'success' : 'default'" variant="light">
              {{ twofa.enabled ? t('settings.twofaEnabledShort') : t('settings.twofaOff') }}
            </UiTag>
            <span class="settings-row__value">
              {{
                twofa.enabled
                  ? t('settings.twofaOn', {
                      method: twofa.method === 'app' ? t('settings.twofaMethodApp') : t('settings.twofaMethodEmail'),
                    })
                  : ''
              }}
            </span>
            <UiButton
              v-if="!twofa.enabled && twofaSetup === ''"
              variant="outline"
              theme="primary"
              size="small"
              @click="twofaSetup = 'choose'"
            >
              {{ t('settings.twofaEnable') }}
            </UiButton>
          </SettingsRow>

          <!-- 开启：选方式 -->
          <template v-if="!twofa.enabled && twofaSetup === 'choose'">
            <SettingsRow
              :title="t('auth.twofaMethodApp')"
              :description="t('auth.twofaMethodAppHint')"
            >
              <UiButton variant="outline" size="small" @click="startTwofaSetup('app')">
                {{ t('settings.twofaChoose') }}
              </UiButton>
            </SettingsRow>
            <SettingsRow
              :title="t('auth.twofaMethodEmail')"
              :description="t('auth.twofaMethodEmailHint')"
              last
            >
              <UiButton
                variant="outline"
                size="small"
                :disabled="!twofa.email"
                @click="startTwofaSetup('email')"
              >
                {{ t('settings.twofaChoose') }}
              </UiButton>
              <template #footer>
                <p v-if="!twofa.email" class="settings-field__hint">{{ t('settings.twofaNeedEmail') }}</p>
              </template>
            </SettingsRow>
          </template>

          <!-- 开启：验证器应用（扫码 / 手动录入 + 6 位码） -->
          <template v-if="!twofa.enabled && twofaSetup === 'app'">
            <p class="settings-field__hint">{{ t('auth.scanDesc', { issuer: 'ModernChat' }) }}</p>
            <img v-if="twofaOtpauthImg" :src="twofaOtpauthImg" alt="" width="180" height="180" />
            <p class="settings-field__hint">
              {{ t('auth.manualCode') }}：<span class="num">{{ twofaSecret }}</span>
            </p>
            <div class="settings-field">
              <UiInput v-model="twofaSetupCode" :maxlength="6" :placeholder="t('auth.enter6')" clearable />
            </div>
            <div class="settings-actions">
              <UiButton theme="primary" :loading="twofaEnabling" @click="enableTotp">
                {{ t('settings.twofaEnable') }}
              </UiButton>
              <UiButton variant="outline" @click="cancelTwofaSetup">{{ t('common.cancel') }}</UiButton>
            </div>
          </template>

          <!-- 开启：邮箱验证码 -->
          <template v-if="!twofa.enabled && twofaSetup === 'email'">
            <p class="settings-field__hint">{{ t('auth.emailDesc', { email: profile.email || '' }) }}</p>
            <div class="settings-code-row">
              <UiInput v-model="twofaSetupCode" :maxlength="6" :placeholder="t('auth.enter6')" />
              <UiButton variant="outline" :disabled="twofaEmailCd > 0" @click="sendTwofaSetupCode">
                {{ twofaEmailCd > 0 ? t('auth.twofaResendIn', { n: twofaEmailCd }) : t('auth.twofaSendCode') }}
              </UiButton>
            </div>
            <div class="settings-actions">
              <UiButton theme="primary" :loading="twofaEnabling" :disabled="!twofaSetupCode" @click="enableEmail">
                {{ t('settings.twofaEnable') }}
              </UiButton>
              <UiButton variant="outline" @click="cancelTwofaSetup">{{ t('common.cancel') }}</UiButton>
            </div>
          </template>

          <!-- 启用成功：一次性展示恢复码 -->
          <template v-if="twofaSetup === 'recovery'">
            <p class="settings-field__hint">{{ t('auth.setupDoneDesc') }}</p>
            <p class="settings-field__hint">{{ t('auth.recoveryWarn') }}</p>
            <pre class="settings-twofa-codes num">{{ twofaRecovery.join('\\n') }}</pre>
            <div class="settings-actions">
              <UiButton variant="outline" size="small" @click="downloadRecovery">
                {{ t('auth.downloadRecovery') }}
              </UiButton>
              <UiButton theme="primary" size="small" @click="finishTwofaSetup">
                {{ t('auth.setupDone') }}
              </UiButton>
            </div>
          </template>

          <!-- 关闭：密码再认证 -->
          <template v-if="twofa.enabled && twofaSetup !== 'recovery'">
            <SettingsRow
              :title="t('settings.twofaDisableLabel')"
              :description="t('settings.twofaDisableHint')"
              last
            >
              <span />
            </SettingsRow>
            <div class="settings-code-row">
              <UiInput v-model="twofaOffPassword" type="password" :placeholder="t('auth.password')" clearable />
              <UiButton
                variant="outline"
                theme="danger"
                size="small"
                :loading="twofaDisabling"
                :disabled="!twofaOffPassword"
                @click="disableTwofa"
              >
                {{ t('settings.twofaDisableBtn') }}
              </UiButton>
            </div>
          </template>
        </template>
      </section>

      <!-- 布局 -->
      <section id="layout" class="settings-card settings-section">
        <div class="settings-card__head">
          <h2 class="settings-card__title">
            <SettingIcon class="settings-card__icon" />
            {{ t('settings.layout') }}
          </h2>
          <UiButton variant="outline" size="small" @click="layout.reset()">
            <template #icon><RefreshIcon /></template>
            {{ t('settings.resetLayout') }}
          </UiButton>
        </div>
        <p class="settings-card__hint">{{ t('settings.layoutHint') }}</p>
        <div v-for="p in layoutPanes" :key="p.key" class="settings-layout">
          <div class="settings-layout__head">
            <span class="settings-layout__name">{{ p.label }}</span>
            <div class="settings-layout__tools">
              <span class="settings-layout__value num">{{ layout.panes[p.key].width }}px</span>
              <UiButton
                variant="text"
                size="small"
                shape="square"
                :aria-label="t('settings.resetPane', { name: p.label })"
                :title="t('settings.resetPane', { name: p.label })"
                @click="resetPane(p.key)"
              >
                <template #icon><RefreshIcon /></template>
              </UiButton>
            </div>
          </div>
          <UiSlider
            :model-value="layout.panes[p.key].width"
            :min="PANE_LIMITS[p.key].min"
            :max="PANE_LIMITS[p.key].max"
            @change="onWidthChange(p.key, $event)"
          />
          <SettingsSegmented
            class="settings-layout__seg"
            :model-value="layout.panes[p.key].drawer ? 'drawer' : 'docked'"
            :options="dockOptions"
            :aria-label="p.label"
            @update:model-value="layout.setDrawer(p.key, $event === 'drawer')"
          />
        </div>
      </section>

      <!-- 本地缓存 -->
      <section id="cache" class="settings-card settings-section">
        <h2 class="settings-card__title">
          <DataIcon class="settings-card__icon" />
          {{ t('settings.cache') }}
        </h2>
        <p class="settings-card__hint">{{ t('settings.cacheHint') }}</p>
        <SettingsRow
          :title="t('settings.cacheUsage')"
          :description="t('settings.cacheUsageHint')"
        >
          <span class="settings-row__value num">
            {{ cacheUsage ? formatBytes(cacheUsage.cacheBytes) : '—' }}
          </span>
        </SettingsRow>
        <SettingsRow
          :title="t('settings.cachePrefs')"
          :description="t('settings.cachePrefsHint')"
        >
          <span class="settings-row__value num">
            {{ cacheUsage ? formatBytes(cacheUsage.localStorageBytes) : '—' }}
          </span>
        </SettingsRow>
        <SettingsRow
          :title="t('settings.cacheClear')"
          :description="t('settings.cacheClearHint')"
          last
        >
          <UiButton variant="outline" size="small" :loading="cacheBusy" @click="confirmClearCache">
            {{ t('settings.cacheClear') }}
          </UiButton>
        </SettingsRow>
      </section>

      <!-- 数据与扩展 -->
      <section id="extensions" class="settings-card settings-section">
        <h2 class="settings-card__title">
          <ExtensionIcon class="settings-card__icon" />
          {{ t('settings.navDataExtensions') }}
        </h2>
        <SettingsRow
          :title="t('settings.pluginsLabel')"
          :description="t('settings.pluginsHint')"
          link
          @click="openPlugins"
        >
          <ChevronRightIcon class="settings-link-chevron" />
        </SettingsRow>
        <SettingsRow
          v-if="auth.isAdmin"
          :title="t('settings.adminLabel')"
          :description="t('settings.adminHint')"
          link
          last
          @click="openAdmin"
        >
          <ChevronRightIcon class="settings-link-chevron" />
        </SettingsRow>
      </section>

      <!-- 关于 -->
      <section id="about" class="settings-card settings-section">
        <h2 class="settings-card__title">
          <InfoCircleIcon class="settings-card__icon" />
          {{ t('settings.about') }}
        </h2>
        <SettingsRow :title="t('settings.webVersion')">
          <span class="settings-row__value num">{{ webVersion }}</span>
        </SettingsRow>
        <SettingsRow :title="t('settings.serverVersion')">
          <span class="settings-row__value num">
            {{ build ? `${build.version} · ${build.commit}` : '—' }}
          </span>
        </SettingsRow>
        <SettingsRow :title="t('settings.copyVersion')" link @click="copyVersion">
          <CopyIcon class="settings-link-chevron" />
        </SettingsRow>
        <SettingsRow :title="t('settings.changelog')" link @click="openChangelog">
          <ChevronRightIcon class="settings-link-chevron" />
        </SettingsRow>
        <SettingsRow :title="t('settings.terms')" link @click="openLegal('/terms')">
          <ChevronRightIcon class="settings-link-chevron" />
        </SettingsRow>
        <SettingsRow :title="t('settings.privacy')" link last @click="openLegal('/privacy')">
          <ChevronRightIcon class="settings-link-chevron" />
        </SettingsRow>
      </section>

      <!-- 危险操作区：账号安全分组的页尾 -->
      <section id="danger" class="settings-card settings-card--danger settings-section">
        <h2 class="settings-card__title">
          <LockOnIcon class="settings-card__icon" />
          {{ t('settings.dangerZone') }}
        </h2>
        <p class="settings-card__hint">{{ t('settings.dangerZoneHint') }}</p>
        <SettingsRow
          :title="t('settings.logoutOthers')"
          :description="t('settings.logoutOthersHint')"
        >
          <UiButton variant="outline" theme="danger" size="small" @click="confirmLogoutOthers">
            {{ t('settings.logoutOthers') }}
          </UiButton>
        </SettingsRow>
        <SettingsRow :title="t('settings.logout')" last>
          <UiButton theme="danger" size="small" @click="confirmLogout">
            {{ t('settings.logout') }}
          </UiButton>
        </SettingsRow>
      </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.settings-page {
  min-height: 100%;
  background: transparent;
  padding-bottom: 48px;
}

.settings-top {
  position: sticky;
  top: 0;
  z-index: 5;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  background: var(--td-bg-color-container);
  border-bottom: 1px solid var(--td-component-border);
}

.settings-top__title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}

.settings-top__spacer {
  flex: 1;
}

/* ≥1200px：左侧锚点 + 右侧内容；窄屏自动回到单列 */
.settings-layout {
  max-width: 1240px;
  margin: 0 auto;
  padding: 28px 20px 0;
  display: grid;
  grid-template-columns: 184px minmax(0, 1fr);
  gap: 24px;
  align-items: start;
}

.settings-anchors {
  position: sticky;
  top: 64px;
  display: flex;
  flex-direction: column;
  gap: 18px;
  max-height: calc(100vh - 96px);
  overflow-y: auto;
  padding-right: 4px;
}

.settings-anchors__group {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.settings-anchors__title {
  padding: 0 10px 6px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.08em;
  color: var(--td-text-color-secondary);
  text-transform: uppercase;
}

.settings-anchors__link {
  width: 100%;
  padding: 7px 10px;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  color: var(--td-text-color-secondary);
  font: inherit;
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  transition:
    background-color var(--mc-dur-instant) var(--mc-ease-out),
    color var(--mc-dur-instant) var(--mc-ease-out);
}

.settings-anchors__link:hover {
  background: var(--td-bg-color-container-hover);
  color: var(--td-text-color-primary);
}

.settings-anchors__link.is-active {
  background: var(--td-brand-color-light);
  color: var(--td-brand-color);
  font-weight: 600;
  box-shadow: inset 2px 0 0 var(--td-brand-color);
}

.settings-anchors__link:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: -2px;
}

.settings-body {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.settings-section {
  scroll-margin-top: 72px;
}

.settings-card {
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  border-radius: var(--mc-radius-lg);
  padding: 20px 20px 10px;
  box-shadow: var(--mc-shadow-1);
  transition: border-color var(--mc-dur-instant) var(--mc-ease-out);
}

.settings-card:hover {
  border-color: var(--td-brand-color-light);
}

:root[theme-mode='dark'] .settings-card {
  border-color: rgba(255, 255, 255, 0.06);
}

.settings-card--danger {
  border-color: var(--danger-line);
}

.settings-card--danger .settings-card__title {
  color: var(--td-error-color);
}

.settings-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 4px;
}

.settings-card__title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 4px;
  font-size: 16px;
  font-weight: 600;
  letter-spacing: -0.01em;
}

.settings-card__icon {
  width: 16px;
  height: 16px;
  color: var(--td-brand-color);
  flex-shrink: 0;
}

.settings-card__hint {
  margin: 0 0 8px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--td-text-color-secondary);
}

.settings-row__value {
  font-size: 13px;
  color: var(--td-text-color-secondary);
}

.settings-input {
  width: 280px;
  max-width: 100%;
}

.settings-link-hint {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--td-text-color-secondary);
  font-size: 13px;
}

.settings-link-chevron {
  width: 16px;
  height: 16px;
  color: var(--td-text-color-placeholder);
}

.settings-save-state {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin-right: auto;
  font-size: 12px;
}

.settings-save-state.is-muted {
  color: var(--td-success-color);
}

.settings-save-state.is-warning {
  color: var(--td-warning-color);
}

.settings-save-state.is-danger {
  color: var(--td-error-color);
}

.settings-save-state__icon {
  width: 14px;
  height: 14px;
}

/* ===== 登录设备 ===== */
.session-list {
  display: flex;
  flex-direction: column;
}

.session-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 8px;
  border-bottom: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-sm);
  transition: background-color var(--mc-dur-instant) var(--mc-ease-out);
}

.session-item:hover {
  background: var(--td-bg-color-container-hover);
}

.session-item:last-child {
  border-bottom: none;
}

.session-item__icon {
  width: 18px;
  height: 18px;
  color: var(--td-text-color-secondary);
  flex-shrink: 0;
}

.session-item__text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1;
}

.session-item__title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 14px;
  font-weight: 500;
}

.session-item__meta {
  overflow: hidden;
  font-size: 12px;
  color: var(--td-text-color-secondary);
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ===== 两步验证恢复码 / 表单 ===== */
.settings-twofa-codes {
  margin: 0;
  padding: 12px 14px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: var(--mc-radius-md);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  line-height: 1.8;
  white-space: pre-wrap;
}

.settings-code-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}

.settings-code-row > :first-child {
  flex: 1;
  min-width: 0;
}

.settings-code-row > :last-child {
  flex-shrink: 0;
  white-space: nowrap;
}

.settings-field {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 10px;
}

.settings-field > div {
  background: var(--td-bg-color-page);
  border-color: var(--td-component-border);
}

.settings-field__hint {
  margin: 0;
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.settings-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  padding: 12px 0 0;
}

.settings-placeholder {
  padding: 16px 0 20px;
  font-size: 13px;
  color: var(--td-text-color-secondary);
}

.settings-placeholder a {
  margin-left: 8px;
}

/* ===== 布局 ===== */
.settings-layout {
  padding: 14px 0;
  border-bottom: 1px solid var(--mc-hairline);
}

.settings-layout:last-child {
  border-bottom: none;
}

.settings-layout__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
}

.settings-layout__name {
  font-size: 14px;
  font-weight: 500;
}

.settings-layout__tools {
  display: flex;
  align-items: center;
  gap: 4px;
}

.settings-layout__value {
  min-width: 48px;
  font-size: 13px;
  color: var(--td-text-color-secondary);
  text-align: right;
}

.settings-layout__seg {
  display: flex;
  justify-content: flex-end;
  margin-top: 10px;
}

@media (max-width: 1199px) {
  .settings-layout {
    max-width: 880px;
    display: block;
  }

  .settings-anchors {
    display: none;
  }
}

@media (max-width: 560px) {
  .settings-layout {
    padding: 16px 12px 0;
  }

  .settings-card {
    padding: 4px 16px 8px;
  }

  .settings-card__head {
    flex-wrap: wrap;
  }

  .settings-input {
    width: 100%;
  }

  .session-item {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .session-item > :last-child {
    margin-left: auto;
  }

  .settings-actions {
    justify-content: flex-start;
    flex-wrap: wrap;
  }
}
</style>
