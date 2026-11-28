<script setup lang="ts">
import UiCheckbox from '@/components/ui/UiCheckbox.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from '@/components/ui/feedback'
import { Form as TForm, FormItem as TFormItem } from 'tdesign-vue-next'
import type { FormInstanceFunctions, FormProps } from 'tdesign-vue-next'
import { LockOnIcon, MailIcon, UserIcon } from 'tdesign-icons-vue-next'
import QRCode from 'qrcode'

import { authApi } from '@/core/network/endpoints'
import AuthScene from '@/components/common/AuthScene.vue'
import QuickPrefs from '@/components/common/QuickPrefs.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { useAuthStore } from '@/stores/auth'
import { useSiteStore } from '@/stores/site'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const site = useSiteStore()
const { t, te } = useAppI18n()

/** 站点名：后台改了这里跟着变；接口没回来时用语言包兜底 */
const siteName = computed(() => (site.loaded ? site.config.name : t('app.name')))

/** public/ 资源部署在 /app/ 下，必须带 BASE_URL 前缀 */
const faviconUrl = `${import.meta.env.BASE_URL}favicon.ico`

const mode = ref<'login' | 'register'>('login')
const loading = ref(false)
const form = ref<FormInstanceFunctions>()

const loginForm = reactive({ username: '', password: '' })
const registerForm = reactive({
  username: '',
  nickname: '',
  password: '',
  confirm: '',
  inviteCode: '',
  email: '',
  emailCode: '',
})

/** 站点要求填邮箱（首账号也必填）；配置没拉回来之前不渲染，避免白填 */
const needEmail = computed(() => site.loaded && site.config.requireEmail)
/** 是否还需要验证码（首账号豁免、未配 SMTP 也不需要） */
const needCode = computed(() => site.loaded && site.config.requireEmailVerify)

// ===== 邮箱验证码 =====

const sendingCode = ref(false)
const codeCooldown = ref(0)
let codeTimer: number | undefined

function stopCodeTimer() {
  if (codeTimer !== undefined) {
    window.clearInterval(codeTimer)
    codeTimer = undefined
  }
}

function startCooldown(seconds: number) {
  codeCooldown.value = Math.min(600, Math.max(1, Math.floor(seconds) || 60))
  stopCodeTimer()
  codeTimer = window.setInterval(() => {
    codeCooldown.value -= 1
    if (codeCooldown.value <= 0) stopCodeTimer()
  }, 1000)
}

onBeforeUnmount(stopCodeTimer)

async function sendEmailCode() {
  if (sendingCode.value || codeCooldown.value > 0) return
  const email = registerForm.email.trim()
  // 前端先挡一次明显错误的邮箱：省一次请求，也避免「验证码发到不存在的地址」
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    void MessagePlugin.warning(t('auth.validation.emailRequired'))
    return
  }
  sendingCode.value = true
  try {
    startCooldown(await auth.sendRegisterCode(email))
    void MessagePlugin.success(t('auth.codeSent'))
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.codeSendFailed'))
  } finally {
    sendingCode.value = false
  }
}

/**
 * 协议同意状态：
 * - 登录态默认勾选（已有账号视为此前已同意，避免每次重复勾选）；
 * - 注册态优先沿用本机记录，否则要求用户主动勾选；
 * - 勾选结果写入 localStorage，后续访问自动沿用。
 */
const AGREED_KEY = 'mc_terms_agreed'
const storedAgreed = () => localStorage.getItem(AGREED_KEY) === '1'
const agreed = ref(mode.value === 'login' || storedAgreed())

watch(agreed, (v) => {
  if (v) localStorage.setItem(AGREED_KEY, '1')
  else localStorage.removeItem(AGREED_KEY)
})

watch(mode, (m) => {
  agreed.value = m === 'login' || storedAgreed()
})

/** 当前模式的表单数据（模板与校验共用） */
const data = computed(() => (mode.value === 'login' ? loginForm : registerForm))

const rules = computed<FormProps['rules']>(() => ({
  username: [{ required: true, message: t('auth.validation.usernameRequired'), trigger: 'blur' }],
  password: [
    { required: true, message: t('auth.validation.passwordRequired'), trigger: 'blur' },
    { min: 6, message: t('auth.validation.passwordMin'), trigger: 'blur' },
  ],
  nickname: [{ required: true, message: t('auth.validation.nicknameRequired'), trigger: 'blur' }],
  confirm: [
    {
      validator: (val: string) => val === registerForm.password,
      message: t('auth.validation.confirmMismatch'),
      trigger: 'blur',
    },
  ],
  // 邮箱相关校验只在字段真的渲染时生效（否则会拦住不显示字段的提交）
  ...(needEmail.value
    ? { email: [{ required: true, message: t('auth.validation.emailRequired'), trigger: 'blur' }] }
    : {}),
  ...(needCode.value
    ? {
        emailCode: [{ required: true, message: t('auth.validation.codeRequired'), trigger: 'blur' }],
      }
    : {}),
}))

async function doSubmit() {
  if (!agreed.value) {
    void MessagePlugin.warning(t('legal.agreeRequired'))
    return
  }
  loading.value = true
  try {
    if (mode.value === 'login') {
      const result = await auth.login(loginForm.username, loginForm.password)
      // 已开启两步验证：不落登录态，切到第二因子表单（票据绑定本次登录）
      if ('twofa' in result) {
        startTwofa(result.ticket, result.method)
        return
      }
    } else {
      await auth.register(
        registerForm.username,
        registerForm.password,
        registerForm.nickname,
        site.config.requireInviteCode ? registerForm.inviteCode.trim() : undefined,
        needEmail.value ? registerForm.email.trim() : undefined,
        needCode.value ? registerForm.emailCode.trim() : undefined,
      )
      void MessagePlugin.success(t('auth.registerOk'))
    }
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/chat'
    await router.push(redirect)
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.opFailed'))
  } finally {
    loading.value = false
  }
}

/** TForm 的 submit：Enter 与按钮统一走校验→提交 */
function onSubmit({ validateResult }: { validateResult: boolean | Record<string, unknown> }) {
  if (validateResult === true) void doSubmit()
}

function switchMode(m: 'login' | 'register') {
  mode.value = m
}

// ===== 旧站迁移自动登录（/app/login?token=..&key=..&migrated=1） =====

/** 正在用一次性票据换本站登录态 */
const migrating = ref(false)
/** 票据失效（或缺失）：提示用原密码登录，并留在登录页 */
const migrateFailed = ref(false)

/**
 * 旧站迁移：URL 带一次性 token & key 时自动换本站登录态。
 * - 成功：applyAuth 后 replace('/chat')，并弹一次性提示
 *   （MessagePlugin 渲染在 body 上，路由切换/组件卸载后依然可见）；
 * - 失败：留在登录页展示失效提示，用户可继续用密码登录；
 * - 成败都会把一次性票据从地址栏清掉（replace 到 /login 或 /chat），
 *   避免刷新、复制链接导致票据被重复消费。
 */
async function tryMigrateLogin() {
  const token = typeof route.query.token === 'string' ? route.query.token : ''
  const key = typeof route.query.key === 'string' ? route.query.key : ''
  if (!token || !key) {
    // 旧站跳转带了 migrated=1 却没有票据：同样按失效处理，给用户明确提示
    if (route.query.migrated === '1') migrateFailed.value = true
    return
  }
  migrating.value = true
  try {
    const payload = await authApi.migrateLogin({ token, key })
    // 契约返回 {token, user}；若后端只回 token，再拉一次 /me 补齐用户信息
    auth.applyAuth(payload)
    if (!payload.user) await auth.fetchMe()
    void MessagePlugin.info({ content: t('auth.migrateDone'), duration: 12_000 })
    await router.replace('/chat')
  } catch {
    migrateFailed.value = true
    await router.replace({ name: 'login' })
  } finally {
    migrating.value = false
  }
}

// ===== 统一登录（聚合登录中心，handler/sso.go） =====

/** 后端配置了 SSO_* 才展示入口；接口失败按未启用处理，不影响密码登录 */
const ssoEnabled = ref(false)

/** 发起统一登录：302 到聚合中心授权页，回调后经 /sso/callback 换票据登录 */
function startSso() {
  const redirect =
    typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/')
      ? route.query.redirect
      : '/chat'
  location.href = `/api/auth/sso/start?redirect=${encodeURIComponent(redirect)}`
}

// ===== 两步验证（登录第二因子，handler/auth_2fa.go，默认关闭） =====

const twofaStep = ref(false)
const twofaTicket = ref('')
const twofaMethod = ref<'' | 'app' | 'email'>('')
const twofaUsingRecovery = ref(false)
const twofaCode = ref('')
const twofaVerifying = ref(false)
const twofaEmailCooldown = ref(0)
let twofaEmailTimer: ReturnType<typeof setInterval> | null = null

function startTwofa(ticket: string, method: '' | 'app' | 'email') {
  twofaTicket.value = ticket
  twofaMethod.value = method
  twofaUsingRecovery.value = false
  twofaCode.value = ''
  twofaStep.value = true
}

function useRecovery() {
  twofaUsingRecovery.value = true
  twofaCode.value = ''
}

function backToPassword() {
  stopTwofaEmailTimer()
  twofaStep.value = false
  twofaTicket.value = ''
  twofaCode.value = ''
  twofaUsingRecovery.value = false
}

function stopTwofaEmailTimer() {
  if (twofaEmailTimer) {
    clearInterval(twofaEmailTimer)
    twofaEmailTimer = null
  }
}

/** 登录期向本人邮箱发第二因子验证码（email 方式） */
async function sendTwofaEmail() {
  try {
    const r = await authApi.twofaEmailCodeForLogin(twofaTicket.value)
    twofaEmailCooldown.value = r.cooldownSeconds ?? 60
    void MessagePlugin.success(t('auth.codeSent'))
    stopTwofaEmailTimer()
    twofaEmailTimer = setInterval(() => {
      if (twofaEmailCooldown.value > 0) twofaEmailCooldown.value--
      else stopTwofaEmailTimer()
    }, 1000)
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.sendFailed'))
  }
}

/** 校验第二因子（TOTP / 邮箱码 / 恢复码）并落登录态 */
async function verifyTwofa() {
  if (!twofaCode.value.trim()) return
  twofaVerifying.value = true
  try {
    await auth.verifyTwofa(twofaTicket.value, twofaCode.value.trim())
    stopTwofaEmailTimer()
    void MessagePlugin.success(t('auth.loginOk'))
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/chat'
    await router.push(redirect)
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.verifyFailed'))
  } finally {
    twofaVerifying.value = false
  }
}

// ===== 扫码登录（桌面端展示二维码，手机端扫码确认，handler/auth_qr.go） =====

const qrOpen = ref(false)
const qrImg = ref('')
const qrState = ref<'pending' | 'expired'>('pending')
const qrBusy = ref(false)
let qrToken = ''
let qrTimer: ReturnType<typeof setInterval> | null = null

function stopQrPolling() {
  if (qrTimer) {
    clearInterval(qrTimer)
    qrTimer = null
  }
}

async function openQrLogin() {
  stopQrPolling()
  qrOpen.value = true
  qrBusy.value = true
  qrState.value = 'pending'
  qrImg.value = ''
  try {
    const s = await authApi.qrStart()
    qrToken = s.token
    // 二维码内容 = 本站确认页地址：手机扫码后在该页用已登录会话确认
    qrImg.value = await QRCode.toDataURL(`${location.origin}${import.meta.env.BASE_URL}qr/${s.token}`, {
      width: 220,
      margin: 1,
    })
    qrTimer = setInterval(pollQr, 2500)
    void pollQr()
  } catch {
    qrState.value = 'expired'
  } finally {
    qrBusy.value = false
  }
}

function closeQrLogin() {
  stopQrPolling()
  qrToken = ''
  qrOpen.value = false
}

async function pollQr() {
  if (!qrToken) return
  try {
    const s = await authApi.qrStatus(qrToken)
    if (s.status === 'confirmed' && s.token && s.user) {
      stopQrPolling()
      auth.applyAuth({ token: s.token, user: s.user })
      // qr/status 不写 Cookie，而 WS 已改为凭 HttpOnly Cookie 握手：
      // 先打一次带 Authorization 的 /me 让后端补发 mc_auth，再进聊天页。
      try {
        await auth.fetchMe()
      } catch {
        /* 网络故障时保留登录态，聊天页会自行重试 */
      }
      void MessagePlugin.success(t('auth.qrLoginOk'))
      await router.replace('/chat')
    } else if (s.status === 'expired') {
      stopQrPolling()
      qrState.value = 'expired'
    }
    // pending：继续轮询
  } catch {
    /* 网络抖动：等下一轮 */
  }
}

onMounted(() => {
  void tryMigrateLogin()
  void authApi
    .ssoStatus()
    .then((s) => {
      ssoEnabled.value = s.enabled
    })
    .catch(() => {
      /* 探测失败按未启用处理 */
    })
})

onBeforeUnmount(stopQrPolling)
</script>

<template>
  <div class="auth-page">
    <!-- 背景层：两段纯色（天 / 水）+ 地平线；放置 web/public/login-bg.jpg 可覆盖为自定义图片 -->
    <AuthScene />

    <div class="auth-top">
      <QuickPrefs />
    </div>

    <main class="mc-container mc-container--narrow">
    <div class="auth-card">
        <div class="auth-card__brand">
          <img class="auth-card__mark" :src="faviconUrl" alt="" width="32" height="32" />
          <span class="auth-card__product">{{ siteName }}</span>
        </div>

        <!-- 两步验证：密码正确后的第二因子表单（默认关闭，开启后登录出现） -->
        <template v-if="twofaStep">
          <h1 class="auth-card__title">{{ t('auth.twofaLoginTitle') }}</h1>
          <p class="auth-card__notice text-placeholder">{{ t('auth.twofaLoginDesc') }}</p>

          <template v-if="!twofaUsingRecovery">
            <p class="auth-card__notice text-placeholder">
              {{
                twofaMethod === 'app'
                  ? t('auth.twofaMethodApp')
                  : twofaMethod === 'email'
                    ? t('auth.twofaMethodEmail')
                    : t('auth.twofaNotConfigured')
              }}
            </p>
            <UiInput
              v-model="twofaCode"
              size="large"
              :maxlength="twofaUsingRecovery ? 20 : 6"
              :placeholder="twofaUsingRecovery ? t('auth.twofaRecoveryCode') : t('auth.enterCode')"
              clearable
              @enter="verifyTwofa"
            >
            </UiInput>
            <UiButton
              class="auth-card__submit"
              theme="primary"
              block
              size="large"
              :loading="twofaVerifying"
              @click="verifyTwofa"
            >
              {{ t('auth.twofaVerify') }}
            </UiButton>
            <p v-if="twofaMethod === 'email'" class="auth-card__notice text-placeholder">
              <button
                type="button"
                class="auth-card__link-btn"
                :disabled="twofaEmailCooldown > 0"
                @click="sendTwofaEmail"
              >
                {{ twofaEmailCooldown > 0 ? t('auth.twofaResendIn', { n: twofaEmailCooldown }) : t('auth.twofaSendCode') }}
              </button>
            </p>
            <p class="auth-card__notice text-placeholder">
              <button type="button" class="auth-card__link-btn" @click="useRecovery">
                {{ t('auth.twofaUseRecovery') }}
              </button>
            </p>
          </template>
          <template v-else>
            <p class="auth-card__notice text-placeholder">{{ t('auth.twofaRecoveryDesc') }}</p>
            <UiInput
              v-model="twofaCode"
              size="large"
              :maxlength="20"
              :placeholder="t('auth.twofaRecoveryCode')"
              clearable
              @enter="verifyTwofa"
            >
            </UiInput>
            <UiButton
              class="auth-card__submit"
              theme="primary"
              block
              size="large"
              :loading="twofaVerifying"
              @click="verifyTwofa"
            >
              {{ t('auth.twofaVerify') }}
            </UiButton>
            <p class="auth-card__notice text-placeholder">
              <button
                type="button"
                class="auth-card__link-btn"
                @click="twofaUsingRecovery = false; twofaCode = ''"
              >
                {{ t('auth.twofaBackToCode') }}
              </button>
            </p>
          </template>
          <div class="auth-card__links">
            <button type="button" class="auth-card__link-btn" @click="backToPassword">
              {{ t('auth.twofaBackToLogin') }}
            </button>
          </div>
        </template>
        <template v-else-if="qrOpen">
          <h1 class="auth-card__title">{{ t('auth.qrLogin') }}</h1>
          <div class="auth-card__qr">
            <img v-if="qrImg" :src="qrImg" alt="" width="200" height="200" />
            <p v-if="qrState === 'pending'" class="auth-card__notice text-placeholder">
              {{ t('auth.qrPending') }}
            </p>
            <template v-else>
              <p class="auth-card__notice is-error">{{ t('auth.qrExpired') }}</p>
              <UiButton theme="primary" :loading="qrBusy" @click="openQrLogin">
                {{ t('auth.qrRefresh') }}
              </UiButton>
            </template>
          </div>
          <p class="auth-card__notice text-placeholder">{{ t('auth.qrHint') }}</p>
          <div class="auth-card__links">
            <button type="button" class="auth-card__link-btn" @click="closeQrLogin">
              {{ t('auth.qrBack') }}
            </button>
          </div>
        </template>
        <template v-else>
        <h1 class="auth-card__title">
          {{ mode === 'login' ? t('auth.loginTitle') : t('auth.registerTitle') }}
        </h1>

        <!-- 旧站迁移自动登录：处理中 / 票据失效时才出现（成功会直接跳聊天页） -->
        <div v-if="migrating" class="auth-card__notice">{{ t('auth.migrateChecking') }}</div>
        <div v-else-if="migrateFailed" class="auth-card__notice is-error">
          <span>{{ t('auth.migrateFailed') }}</span>
          <UiButton size="small" variant="text" @click="migrateFailed = false">
            {{ t('auth.migrateManualLogin') }}
          </UiButton>
        </div>

        <TForm
          ref="form"
          :key="mode"
          class="auth-card__form"
          :data="data"
          :rules="rules"
          label-width="0"
          @submit="onSubmit"
        >
          <TFormItem name="username">
            <UiInput
              v-model="data.username"
              size="large"
              :placeholder="t('auth.username')"
              clearable
              @enter="form?.submit()"
            >
              <template #prefix-icon><UserIcon /></template>
            </UiInput>
          </TFormItem>

          <TFormItem v-if="mode === 'register'" name="nickname">
            <UiInput
              v-model="registerForm.nickname"
              size="large"
              :placeholder="t('auth.nickname')"
              clearable
              @enter="form?.submit()"
            >
              <template #prefix-icon><UserIcon /></template>
            </UiInput>
          </TFormItem>

          <!-- 站点开启「注册需邮箱验证」时才出现：邮箱 + 验证码 -->
          <TFormItem v-if="mode === 'register' && needEmail" name="email">
            <UiInput
              v-model="registerForm.email"
              size="large"
              type="email"
              :placeholder="t('auth.email')"
              clearable
              @enter="needCode ? sendEmailCode() : form?.submit()"
            >
              <template #prefix-icon><MailIcon /></template>
            </UiInput>
          </TFormItem>

          <TFormItem v-if="mode === 'register' && needCode" name="emailCode">
            <div class="auth-card__code-row">
              <UiInput
                v-model="registerForm.emailCode"
                size="large"
                :maxlength="6"
                :placeholder="t('auth.emailCode')"
                @enter="form?.submit()"
              />
              <UiButton
                variant="outline"
                size="large"
                :loading="sendingCode"
                :disabled="codeCooldown > 0"
                @click="sendEmailCode"
              >
                {{ codeCooldown > 0 ? t('auth.resendIn', { n: codeCooldown }) : t('auth.sendCode') }}
              </UiButton>
            </div>
          </TFormItem>

          <!-- 站点开启邀请码注册时才出现 -->
          <TFormItem v-if="mode === 'register' && site.config.requireInviteCode" name="inviteCode">
            <UiInput
              v-model="registerForm.inviteCode"
              size="large"
              :placeholder="t('auth.inviteCode')"
              clearable
            >
              <template #prefix-icon><LockOnIcon /></template>
            </UiInput>
          </TFormItem>

          <TFormItem name="password" :help="mode === 'register' ? t('auth.passwordHelp') : undefined">
            <UiInput
              v-model="data.password"
              size="large"
              type="password"
              :placeholder="t('auth.password')"
              @enter="form?.submit()"
            >
              <template #prefix-icon><LockOnIcon /></template>
            </UiInput>
          </TFormItem>

          <TFormItem v-if="mode === 'register'" name="confirm">
            <UiInput
              v-model="registerForm.confirm"
              size="large"
              type="password"
              :placeholder="t('auth.confirmPassword')"
              @enter="form?.submit()"
            >
              <template #prefix-icon><LockOnIcon /></template>
            </UiInput>
          </TFormItem>

          <!-- 协议同意：紧邻密码与提交按钮之间 -->
          <div class="auth-card__agree">
            <UiCheckbox v-model="agreed" size="small">
              <span class="auth-card__agree-text">
                {{ t('legal.agreePrefix') }}
                <!-- 新标签打开，避免打断登录流程 -->
                <RouterLink :to="'/terms'" target="_blank" @click.stop>{{ t('legal.termsLink') }}</RouterLink>
                {{ t('legal.and') }}
                <RouterLink :to="'/privacy'" target="_blank" @click.stop>{{ t('legal.privacyLink') }}</RouterLink>
              </span>
            </UiCheckbox>
          </div>

          <UiButton
            class="auth-card__submit"
            theme="primary"
            type="submit"
            block
            size="large"
            :loading="loading"
            :disabled="migrating"
          >
            {{ mode === 'login' ? t('auth.actionLogin') : t('auth.actionRegister') }}
          </UiButton>
        </TForm>

        <!-- 统一登录：状态异步探测，先占住按钮位置（visibility 切换而非插入），避免推低下方链接 -->
        <UiButton
          v-if="mode === 'login'"
          class="auth-card__sso"
          :class="{ 'is-placeholder': !ssoEnabled }"
          variant="outline"
          size="large"
          block
          :disabled="migrating"
          @click="startSso"
        >
          {{ t('auth.ssoLogin') }}
        </UiButton>

        <div class="auth-card__links">
          <template v-if="mode === 'login'">
            <RouterLink to="/forgot">{{ t('auth.forgot') }}</RouterLink>
            <!-- 站点关闭注册时不再露出入口；配置没拉回来之前也不露出（默认值已改为 false） -->
            <button
              v-if="site.loaded && site.config.allowRegister"
              type="button"
              class="auth-card__link-btn"
              @click="switchMode('register')"
            >
              {{ t('auth.signupLink') }}
            </button>
            <!-- 扫码登录入口：手机端已登录时扫桌面二维码即可登录 -->
            <button type="button" class="auth-card__link-btn" @click="openQrLogin">
              {{ t('auth.qrLogin') }}
            </button>
          </template>
          <template v-else>
            <span class="text-placeholder">{{ t('auth.haveAccount') }}</span>
            <button type="button" class="auth-card__link-btn" @click="switchMode('login')">
              {{ t('auth.loginLink') }}
            </button>
          </template>
        </div>
        </template>
    </div>
    </main>
  </div>
</template>

<style scoped>
/* ===== 页面骨架：外框走统一容器的 narrow 档（--mc-container-narrow），
   窄屏左右留白 = --mc-gutter；卡片本身不再写死宽度 ===== */
.auth-page {
  position: relative;
  min-height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  /* 窄屏内容靠上（顶部有固定的偏好切换），≥480 再垂直居中 */
  justify-content: flex-start;
  padding: 64px var(--mc-gutter) 32px;
  overflow-y: auto;
}

@media (min-width: 480px) {
  .auth-page {
    justify-content: center;
    padding-block: 32px 48px;
  }
}

/* ===== 顶部偏好（语言 / 主题） ===== */
.auth-top {
  position: fixed;
  top: var(--mc-sp-4);
  right: var(--mc-gutter);
  z-index: 2;
  display: flex;
  align-items: center;
  padding: var(--mc-sp-1) var(--mc-sp-2);
  border-radius: var(--mc-radius-sm);
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  box-shadow: var(--mc-shadow-1);
}

/* ===== 卡片 ===== */
.auth-card {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  gap: var(--mc-sp-4);
  width: 100%;
  padding: var(--mc-sp-6) var(--mc-sp-5) var(--mc-sp-4);
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  border-radius: var(--mc-radius-lg);
}

@media (min-width: 480px) {
  .auth-card {
    padding: var(--mc-sp-8) var(--mc-sp-6) var(--mc-sp-6);
    /* 浮于场景之上，允许一层柔和阴影 */
    box-shadow: var(--mc-elev-2);
  }
}

/* 品牌块居中，与居中的标题、表单形成同一条轴 */
.auth-card__brand {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--mc-sp-2);
}

/* 产品图标：ico 原图 32px，按 1:1 显示最清晰；1px 描边避免深色卡片上糊成一块 */
.auth-card__mark {
  width: 32px;
  height: 32px;
  border-radius: var(--mc-radius-sm);
  flex-shrink: 0;
  display: block;
  /* 1px 描边走 hairline 令牌：非投影，不用 elev 档 */
  box-shadow: 0 0 0 1px var(--mc-hairline);
}

:root[theme-mode='dark'] .auth-card__mark {
  box-shadow: 0 0 0 1px var(--mc-hairline-strong);
}

.auth-card__product {
  font-size: var(--mc-fs-emphasis);
  font-weight: 600;
  letter-spacing: 0.01em;
  color: var(--td-text-color-secondary);
}

.auth-card__title {
  margin: 0;
  font-size: var(--mc-fs-heading);
  font-weight: 600;
  line-height: var(--mc-lh-tight);
  letter-spacing: -0.02em;
  text-align: center;
}

/* 迁移自动登录状态条 / 说明文字：紧贴标题下方，不抢表单的视觉重心 */
.auth-card__notice {
  display: flex;
  align-items: center;
  gap: var(--mc-sp-1);
  flex-wrap: wrap;
  margin-top: 0;
  padding: var(--mc-sp-2) var(--mc-sp-3);
  border-radius: var(--mc-radius-sm);
  font-size: var(--mc-fs-sm);
  line-height: var(--mc-lh-normal);
  color: var(--td-text-color-secondary);
  background: var(--td-bg-color-secondarycontainer);
  border: 1px solid var(--mc-hairline);
}

.auth-card__notice.is-error {
  color: var(--td-error-color);
  border-color: var(--td-error-color-3, var(--mc-hairline));
  background: var(--td-error-color-1, var(--td-bg-color-secondarycontainer));
}

/* ===== 扫码登录 ===== */
.auth-card__qr {
  display: flex;
  flex-direction: column;
  gap: var(--mc-sp-2);
}
.auth-card__qr img {
  display: block;
  margin: 0 auto;
  border-radius: var(--mc-radius-md);
  /* 二维码留白（margin:1）自带浅色底，深浅主题下都可扫 */
}

/* 表单字段间距用 flex gap 统一控制
 * （TDesign 的 FormItem margin 在此布局下不生效，会导致字段贴死） */
.auth-card__form {
  display: flex;
  flex-direction: column;
  gap: var(--mc-sp-4);
  animation: auth-form-in var(--mc-dur-base) var(--mc-ease-out);
}

.auth-card__form :deep(.t-form__item) {
  margin-bottom: 0;
}

@keyframes auth-form-in {
  from {
    opacity: 0.4;
    transform: translateY(6px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@media (prefers-reduced-motion: reduce) {
  .auth-card__form {
    animation: none;
  }
}

/* 验证码行：输入框占满剩余宽度，按钮固定不换行 */
.auth-card__code-row {
  display: flex;
  align-items: center;
  gap: var(--mc-sp-2);
}

.auth-card__code-row > :first-child {
  flex: 1;
  min-width: 0;
}

.auth-card__code-row > :last-child {
  flex-shrink: 0;
  white-space: nowrap;
}

/* 协议同意行：位于密码之后、提交按钮之前 */
.auth-card__agree {
  margin: 0;
  font-size: var(--mc-fs-sm);
  line-height: var(--mc-lh-normal);
}

.auth-card__agree-text {
  margin-left: var(--mc-sp-1);
  color: var(--mc-text-faint);
}

.auth-card__agree-text a {
  color: var(--mc-link);
}

/* SSO 探测是异步的：按钮始终占位，只切可见性；下方链接区不会在配置到达时下移 */
.auth-card__sso.is-placeholder {
  visibility: hidden;
  pointer-events: none;
}

.auth-card__links {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: var(--mc-sp-2) var(--mc-sp-4);
  margin-top: 0;
  padding-top: var(--mc-sp-4);
  border-top: 1px solid var(--mc-hairline);
  font-size: var(--mc-fs-body);
  color: var(--td-text-color-secondary);
}

.auth-card__links a {
  color: var(--mc-link);
}

/* 切换模式是动作而非跳转，用按钮保持语义与键盘可达 */
.auth-card__link-btn {
  padding: 0;
  border: none;
  background: none;
  font: inherit;
  color: var(--mc-link);
  cursor: pointer;
}

.auth-card__link-btn:hover,
.auth-card__links a:hover,
.auth-card__agree-text a:hover {
  text-decoration: underline;
}

.auth-card__link-btn:focus-visible,
.auth-card__links a:focus-visible,
.auth-card__agree-text a:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
  border-radius: var(--mc-radius-sm);
}
</style>
