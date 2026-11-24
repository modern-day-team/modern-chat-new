import { createI18n } from 'vue-i18n'

export type Lang = 'zh-CN' | 'en-US'

const LANG_KEY = 'mc_lang'

/** 默认语言：已保存 → 浏览器语言（zh 开头为中文，其余英文）。 */
export function detectLang(): Lang {
  const saved = localStorage.getItem(LANG_KEY)
  if (saved === 'zh-CN' || saved === 'en-US') return saved
  return navigator.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en-US'
}

const loaders = {
  'zh-CN': () => import('./zh-CN'),
  'en-US': () => import('./en-US'),
} as const

const initialLang = detectLang()
const loaded = new Set<Lang>()

const i18n = createI18n({
  legacy: false,
  locale: initialLang,
  fallbackLocale: initialLang,
  messages: {},
})

/** 只加载当前语言；切换时先加载目标语言再切换，避免界面闪出 key。 */
async function ensureLocale(lang: Lang) {
  if (loaded.has(lang)) return
  const mod = await loaders[lang]()
  i18n.global.setLocaleMessage(lang, mod.default)
  loaded.add(lang)
}

await ensureLocale(initialLang)

/** 切换语言：加载词条 → 更新 i18n、持久化、同步 <html lang> 与页面标题。 */
export async function setLang(lang: Lang) {
  await ensureLocale(lang)
  i18n.global.locale.value = lang
  localStorage.setItem(LANG_KEY, lang)
  document.documentElement.setAttribute('lang', lang === 'zh-CN' ? 'zh-CN' : 'en')
  document.title = i18n.global.t('app.name')
}

export function currentLang(): Lang {
  return i18n.global.locale.value as Lang
}

export { i18n }
export default i18n
