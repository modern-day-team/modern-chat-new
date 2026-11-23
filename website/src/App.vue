<script setup lang="ts">
import { watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterView } from 'vue-router'

import SiteFooter from '@/components/SiteFooter.vue'
import SiteNav from '@/components/SiteNav.vue'

const { t } = useI18n()

// 标题与描述随语言切换（SEO / 分享卡片）
watchEffect(() => {
  document.title = t('site.title')
  document.querySelector('meta[name="description"]')?.setAttribute('content', t('site.desc'))
})
</script>

<template>
  <SiteNav />
  <!-- 路由组件是异步 chunk：先给主区占满一屏，避免页脚在内容到达前被顶上去（CLS） -->
  <div class="site-main">
    <RouterView />
  </div>
  <SiteFooter />
</template>
