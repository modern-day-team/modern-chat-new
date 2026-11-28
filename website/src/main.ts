import { createApp } from 'vue'

import App from './App.vue'
import { i18n, setLang } from './i18n'
import { router } from './router'
import { applyTheme, readMode } from './theme'

// 首屏样式：令牌 → 基础层 → 站点外壳。
// docs.css / market.css / home.css 由各自页面按需 import，不进入首屏 CSS。
import './styles/tokens.css'
// 栅格系统（与主仓库 web/src/assets/grid.css 逐字节相同）
import './styles/grid.css'
import './styles/base.css'
import './styles/site.css'

const app = createApp(App)

app.use(router)
app.use(i18n)

// 首屏前应用语言（避免 <html lang> 与文案跳变）
setLang(i18n.global.locale.value as 'zh' | 'en')

// 主题：index.html 已做首屏预设，这里补上「跟随系统」的实时响应
applyTheme()
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
  if (readMode() === 'auto') applyTheme()
})

app.mount('#app')
