<script setup lang="ts">
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from '@/components/ui/feedback'
import { Form as TForm, FormItem as TFormItem } from 'tdesign-vue-next'
import type { FormInstanceFunctions, FormProps } from 'tdesign-vue-next'
import { KeyIcon, LockOnIcon, MailIcon } from 'tdesign-icons-vue-next'

import AuthScene from '@/components/common/AuthScene.vue'
import QuickPrefs from '@/components/common/QuickPrefs.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { authApi } from '@/core/network/endpoints'

const router = useRouter()
const route = useRoute()
const { t, te } = useAppI18n()

/** public/ 资源部署在 /app/ 下，必须带 BASE_URL 前缀 */
const faviconUrl = `${import.meta.env.BASE_URL}favicon.ico`

/** 1 = 填写邮箱获取链接；2 = 用邮件中的令牌设置新密码 */
const step = ref<1 | 2>(1)
const loading = ref(false)
const form = ref<FormInstanceFunctions>()

const emailForm = reactive({ email: '' })
const resetForm = reactive({ token: '', newPassword: '', confirm: '' })

const title = computed(() => (step.value === 1 ? t('forgot.title') : t('forgot.titleStep2')))
const sub = computed(() => (step.value === 1 ? t('forgot.sub') : t('forgot.subStep2')))

const emailRules = computed<FormProps['rules']>(() => ({
  email: [{ required: true, message: t('forgot.emailRequired'), trigger: 'blur' }],
}))

const resetRules = computed<FormProps['rules']>(() => ({
  token: [{ required: true, message: t('forgot.tokenRequired'), trigger: 'blur' }],
  newPassword: [
    { required: true, message: t('forgot.newPasswordRequired'), trigger: 'blur' },
    { min: 6, message: t('auth.validation.passwordMin'), trigger: 'blur' },
  ],
  confirm: [
    {
      validator: (val: string) => val === resetForm.newPassword,
      message: t('auth.validation.confirmMismatch'),
      trigger: 'blur',
    },
  ],
}))

async function submitEmail() {
  const valid = await form.value?.validate()
  if (valid !== true) return
  loading.value = true
  try {
    await authApi.forgot({ email: emailForm.email.trim() })
    void MessagePlugin.success(t('forgot.sent'))
    step.value = 2
  } catch (e) {
    void MessagePlugin.error(te(e) || t('forgot.sendFailed'))
  } finally {
    loading.value = false
  }
}

async function submitReset() {
  const valid = await form.value?.validate()
  if (valid !== true) return
  loading.value = true
  try {
    await authApi.reset({ token: resetForm.token.trim(), newPassword: resetForm.newPassword })
    void MessagePlugin.success(t('forgot.resetOk'))
    // 令牌与新密码都是敏感值：成功后立刻从内存里清掉
    Object.assign(resetForm, { token: '', newPassword: '', confirm: '' })
    await router.push('/login')
  } catch (e) {
    void MessagePlugin.error(te(e) || t('forgot.resetFailed'))
  } finally {
    loading.value = false
  }
}

/** 返回第一步：保留已填邮箱，但清掉令牌与新密码（换账号时不能带着上一个账号的令牌） */
function backToStep1() {
  Object.assign(resetForm, { token: '', newPassword: '', confirm: '' })
  step.value = 1
}

/**
 * 邮件里发的是链接 `{前端地址}/forgot?token=xxx`，点开必须直接可用：
 * - 带 token 进来 → 直接进第二步并自动填入（否则用户会在第一步迷路，
 *   而邮件正文里并没有可复制的令牌）
 * - 填好后顺手 clean 掉地址栏里的 token（避免截图/分享时泄漏）
 */
onMounted(() => {
  const raw = route.query.token
  const token = Array.isArray(raw) ? raw[0] : raw
  if (typeof token === 'string' && token.trim()) {
    resetForm.token = token.trim()
    step.value = 2
    void router.replace({ path: '/forgot' })
  }
})
</script>

<template>
  <div class="auth-page">
    <AuthScene />

    <div class="auth-top">
      <QuickPrefs />
    </div>

    <main class="mc-container mc-container--narrow">
    <div class="auth-card">
        <div class="auth-card__brand">
          <img class="auth-card__mark" :src="faviconUrl" alt="" width="32" height="32" />
          <span class="auth-card__product">{{ t('app.name') }}</span>
        </div>

        <h1 class="auth-card__title">{{ title }}</h1>
        <p class="auth-card__sub">{{ sub }}</p>

        <!-- 步骤指示：当前步实底，完成步打勾 -->
        <ol class="auth-steps">
          <li class="auth-steps__item" :class="{ 'is-active': step === 1, 'is-done': step === 2 }">
            <span class="auth-steps__num">{{ step === 2 ? '✓' : '1' }}</span>
            {{ t('forgot.step1Label') }}
          </li>
          <li class="auth-steps__item" :class="{ 'is-active': step === 2 }">
            <span class="auth-steps__num">2</span>
            {{ t('forgot.step2Label') }}
          </li>
        </ol>

        <!-- 第一步：邮箱 -->
        <TForm
          v-if="step === 1"
          ref="form"
          class="auth-card__form"
          :data="emailForm"
          :rules="emailRules"
          label-width="0"
          @submit.prevent
        >
          <TFormItem name="email">
            <UiInput v-model="emailForm.email" size="large" :placeholder="t('forgot.email')" clearable @enter="submitEmail">
              <template #prefix-icon><MailIcon /></template>
            </UiInput>
          </TFormItem>

          <UiButton class="auth-card__submit" theme="primary" block size="large" :loading="loading" @click="submitEmail">
            {{ t('forgot.sendLink') }}
          </UiButton>
        </TForm>

        <!-- 第二步：令牌 + 新密码 -->
        <TForm
          v-else
          ref="form"
          class="auth-card__form"
          :data="resetForm"
          :rules="resetRules"
          label-width="0"
          @submit.prevent
        >
          <TFormItem name="token">
            <UiInput v-model="resetForm.token" size="large" :placeholder="t('forgot.token')" clearable>
              <template #prefix-icon><KeyIcon /></template>
            </UiInput>
          </TFormItem>

          <TFormItem name="newPassword">
            <UiInput v-model="resetForm.newPassword" size="large" type="password" :placeholder="t('forgot.newPassword')">
              <template #prefix-icon><LockOnIcon /></template>
            </UiInput>
          </TFormItem>

          <TFormItem name="confirm">
            <UiInput v-model="resetForm.confirm" size="large" type="password" :placeholder="t('forgot.confirm')">
              <template #prefix-icon><LockOnIcon /></template>
            </UiInput>
          </TFormItem>

          <UiButton class="auth-card__submit" theme="primary" block size="large" :loading="loading" @click="submitReset">
            {{ t('forgot.reset') }}
          </UiButton>
        </TForm>

        <div class="auth-card__links">
          <button v-if="step === 2" type="button" class="auth-card__link-btn" @click="backToStep1">
            {{ t('forgot.backStep') }}
          </button>
          <RouterLink to="/login">{{ t('forgot.backToLogin') }}</RouterLink>
        </div>
    </div>
    </main>
  </div>
</template>

<style scoped>
/* 与登录页保持同一套骨架：外框 mc-container--narrow，窄屏左右留白 = --mc-gutter */
.auth-page {
  position: relative;
  min-height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
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

.auth-card {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
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
    box-shadow: var(--mc-elev-2);
  }
}

/* 与登录页一致：品牌块与标题、表单同轴居中 */
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

.auth-card__sub {
  /* 副标题紧贴标题：收掉卡片 gap 的一半 */
  margin: calc(-1 * var(--mc-sp-2)) 0 0;
  font-size: var(--mc-fs-sm);
  line-height: var(--mc-lh-normal);
  color: var(--mc-text-faint);
  text-align: center;
}

/* ===== 步骤条 ===== */
.auth-steps {
  display: flex;
  gap: var(--mc-sp-2);
  width: 100%;
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: var(--mc-fs-sm);
}

.auth-steps__item {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--mc-sp-1);
  padding: var(--mc-sp-2);
  border-radius: var(--mc-radius-sm);
  background: var(--td-bg-color-secondarycontainer);
  color: var(--mc-text-faint);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  transition:
    background-color var(--mc-dur-fast) var(--mc-ease-out),
    color var(--mc-dur-fast) var(--mc-ease-out);
}

.auth-steps__num {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: var(--mc-fs-caption);
  font-variant-numeric: tabular-nums;
  background: var(--td-bg-color-container);
  color: var(--mc-text-faint);
}

.auth-steps__item.is-active {
  background: var(--td-brand-color);
  color: var(--td-text-color-anti);
}

.auth-steps__item.is-active .auth-steps__num {
  background: rgba(255, 255, 255, 0.22);
  color: var(--td-text-color-anti);
}

.auth-steps__item.is-done {
  color: var(--td-success-color);
}

.auth-steps__item.is-done .auth-steps__num {
  color: var(--td-success-color);
}

/* ===== 表单 =====
 * 字段间距用 flex gap 统一控制：TDesign 的 FormItem margin 在本布局下不生效，
 * 会导致「最后一个字段紧贴按钮」。gap 对 FormItem 与按钮同样有效。 */
.auth-card__form {
  display: flex;
  flex-direction: column;
  gap: var(--mc-sp-4);
  width: 100%;
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

.auth-card__submit {
  margin-top: 0;
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

.auth-card__link-btn {
  padding: 0;
  border: none;
  background: none;
  font: inherit;
  color: var(--mc-link);
  cursor: pointer;
}

.auth-card__link-btn:hover,
.auth-card__links a:hover {
  text-decoration: underline;
}

.auth-card__link-btn:focus-visible,
.auth-card__links a:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
  border-radius: var(--mc-radius-sm);
}
</style>
