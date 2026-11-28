<script setup lang="ts">
/**
 * 统一登录回跳页（/sso/callback，站内 SPA 路由）。
 *
 * 聚合登录中心授权完成后，后端 /api/auth/sso/callback 换到一次性票据，
 * 302 回本页并带 ?ticket=..&redirect=..。这里用票据换本站 JWT：
 *   - 成功：先展示待登录账号并要求用户确认，确认后才 applyAuth 并跳 redirect
 *     （站内相对路径，后端已校验防开放重定向）；
 *   - 中心返回 error=..（用户拒绝授权）：提示后回登录页；
 *   - 票据缺失 / 无效 / 已用过：提示后回登录页重新发起。
 * 无论成败都 replace 清掉地址栏里的票据，防止刷新/复制链接重复消费。
 */
import UiButton from '@/components/ui/UiButton.vue'
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from '@/components/ui/feedback'

import { authApi } from '@/core/network/endpoints'
import { useAppI18n } from '@/composables/useAppI18n'
import { useAuthStore } from '@/stores/auth'
import type { AuthPayload } from '@/types/proto'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const { t } = useAppI18n()

/** 正在用一次性票据换本站登录态 */
const exchanging = ref(true)
/** 票据失效 / 授权被拒：提示后回登录页 */
const failed = ref(false)
/**
 * 票据已核销、等待用户确认的登录结果。
 * 不直接 applyAuth：票据不绑定发起浏览器，自动换号会让「把带 ?ticket= 的链接
 * 发给已登录受害者」直接变成登录 CSRF / 会话固定。确认前还要让用户看清账号。
 */
const pendingAuth = ref<AuthPayload | null>(null)
let redirectTo = '/chat'

/** 当前会话与待登录账号不同名时，明确展示将切换到的账号 */
const currentName = computed(() => auth.user?.nickname || auth.user?.username || '')
const nextName = computed(
  () => pendingAuth.value?.user?.nickname || pendingAuth.value?.user?.username || t('common.none'),
)

onMounted(async () => {
  const error = typeof route.query.error === 'string' ? route.query.error : ''
  const ticket = typeof route.query.ticket === 'string' ? route.query.ticket : ''
  // 后端 ssoSafeNext 只放行站内相对路径；空值按默认落地页处理
  const redirect =
    typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/')
      ? route.query.redirect
      : '/chat'
  redirectTo = redirect
  if (!ticket || error) {
    failed.value = true
    exchanging.value = false
    return
  }
  try {
    const payload = await authApi.ssoExchange({ ticket })
    // 先摘掉地址栏里的票据：刷新/复制不应再次消费，也不该把凭据留在历史记录里
    await router.replace({ query: { redirect } })
    pendingAuth.value = payload
  } catch {
    failed.value = true
    await router.replace({ name: 'login', query: { redirect } })
  } finally {
    exchanging.value = false
  }
})

/** 用户明确同意后才写入登录态（已登录时这一步会切换账号） */
async function confirmLogin() {
  const payload = pendingAuth.value
  if (!payload) return
  pendingAuth.value = null
  auth.applyAuth(payload)
  // WS 已改为凭 HttpOnly Cookie 握手（不再把 token 放 URL），而 ssoExchange
  // 响应本身不写 cookie：先打一次带 Authorization 的 /me，让后端补发新账号的
  // mc_auth，避免带着上一个账号的旧 cookie 去连 WS。
  try {
    await auth.fetchMe()
  } catch {
    /* 网络故障时保留登录态，聊天页会自行重试 */
  }
  void MessagePlugin.info({ content: t('auth.ssoLoginOk'), duration: 8_000 })
  await router.replace(redirectTo)
}

/** 取消：票据已核销但绝不使用；已有会话保持不变 */
function cancelLogin() {
  pendingAuth.value = null
  if (auth.token) void router.replace('/chat')
  else void router.replace({ name: 'login' })
}
</script>

<template>
  <div class="mc-sso">
    <div v-if="exchanging" class="mc-sso__card">
      <p>{{ t('auth.ssoExchanging') }}</p>
    </div>
    <!-- 票据换到的是账号凭据：必须由用户确认后才写入，避免打开链接即被换号 -->
    <div v-else-if="pendingAuth" class="mc-sso__card">
      <h1 class="mc-sso__title">{{ t('auth.ssoLogin') }}</h1>
      <p v-if="currentName" class="mc-sso__switch">
        {{ currentName }} → {{ nextName }}
      </p>
      <p v-else class="mc-sso__account">{{ nextName }}</p>
      <div class="mc-sso__actions">
        <UiButton variant="outline" @click="cancelLogin">{{ t('common.cancel') }}</UiButton>
        <UiButton theme="primary" @click="confirmLogin">{{ t('common.confirm') }}</UiButton>
      </div>
    </div>
    <div v-else class="mc-sso__card is-error">
      <p>{{ failed ? t('auth.ssoFailed') : t('auth.ssoFailed') }}</p>
      <UiButton theme="primary" @click="router.replace({ name: 'login' })">
        {{ t('auth.ssoBackLogin') }}
      </UiButton>
    </div>
  </div>
</template>

<style scoped>
.mc-sso {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.mc-sso__card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  padding: 32px 40px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
}
.mc-sso__card.is-error p {
  color: var(--td-error-color);
}
.mc-sso__title {
  font-size: var(--mc-fs-title);
  font-weight: 600;
}
.mc-sso__account,
.mc-sso__switch {
  font-size: var(--mc-fs-title);
  font-weight: 600;
  color: var(--td-text-color-primary);
}
.mc-sso__actions {
  display: flex;
  gap: 8px;
}
</style>
