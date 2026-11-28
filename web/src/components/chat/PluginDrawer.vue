<script setup lang="ts">
import UiSwitch from '@/components/ui/UiSwitch.vue'
import UiDrawer from '@/components/ui/UiDrawer.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import UiTag from '@/components/ui/UiTag.vue'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { DialogPlugin, MessagePlugin } from '@/components/ui/feedback'

import { useAppI18n } from '@/composables/useAppI18n'
import { pluginApi, serverPluginApi, type ServerPluginPage } from '@/core/network/endpoints'
import { usePluginHost } from '@/composables/usePluginHost'
import { useAuthStore } from '@/stores/auth'
import { usePluginsStore } from '@/stores/plugins'
import type { PageDesc, Plugin } from '@/types/proto'
import * as market from './pluginMarket'
import { sdkRequirement, unmetDeps } from './pluginDeps'

const visible = defineModel<boolean>({ default: false })
/** 市场一键安装深链预填：URL 永久生效，名称只做提示横幅 */
const props = defineProps<{ initialUrl?: string; initialName?: string }>()
const emit = defineEmits<{ 'preset-done': [] }>()
const router = useRouter()
const auth = useAuthStore()
const pluginsStore = usePluginsStore()
const host = usePluginHost()
const { t, te } = useAppI18n()

const urlInput = ref('')
const installing = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

// 深链预填：URL 一到就填进输入框（横幅提示来源，安装仍需用户手点）
watch(
  () => props.initialUrl,
  (u) => {
    if (u) urlInput.value = u
  },
  { immediate: true },
)

/**
 * 用按钮主动触发隐藏的文件选择框。
 * 不能把按钮放进 <label> 里：button 属于交互内容，浏览器会抑制 label 的
 * 默认激活行为，点击「上传」不会打开文件选择框（点击外层空白才有效）。
 */
function pickFile() {
  if (installing.value) return
  fileInput.value?.click()
}

/**
 * 抽屉打开时拉全量清单（含已停用）：宿主的 active 列表只有启用项，
 * 若沿用它，停用后的插件将从抽屉里消失、再也无法重新启用。
 * 同时刷新服务端插件页面清单。
 */
const serverPages = ref<ServerPluginPage[]>([])

watch(
  visible,
  (v) => {
    if (!v) return
    pluginsStore.load().catch((e) => {
      void MessagePlugin.error(te(e))
    })
    serverPluginApi.pages()
      .then((list) => {
        serverPages.value = Array.isArray(list) ? list : []
      })
      .catch((e) => {
        // 页面清单加载失败不影响插件管理本身，只让分组显示空态
        serverPages.value = []
        void MessagePlugin.error(te(e))
      })
  },
  { immediate: true },
)

/** 用户插件注册的页面；admin 页仅站长可见 */
const pluginPages = computed<Array<{ pluginId: number; pluginName: string; page: PageDesc }>>(() => {
  const out: Array<{ pluginId: number; pluginName: string; page: PageDesc }> = []
  for (const p of pluginsStore.plugins) {
    for (const page of host.state.pages[p.id] ?? []) {
      if (page.scope === 'admin' && auth.user?.role !== 'admin') continue
      // host.state 是深层 readonly（body/children 都是只读数组），深拷贝成可变 PageDesc
      out.push({ pluginId: p.id, pluginName: p.name, page: JSON.parse(JSON.stringify(page)) as PageDesc })
    }
  }
  return out
})

/** 站点页面：scope=user 人人可见，admin 页仅站长可见 */
const visibleServerPages = computed(() =>
  serverPages.value.filter((p) => p.scope !== 'admin' || auth.user?.role === 'admin'),
)

function openUserPage(pluginId: number, pageId: string) {
  visible.value = false
  void router.push(`/plugin/${pluginId}/${pageId}`)
}

function openServerPage(pluginId: string, pageId: string) {
  visible.value = false
  void router.push(`/plugin-server/${pluginId}/${pageId}`)
}

// ===== 安装 / 升级 =====

async function onPickFile(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  await installConfirm(() => pluginApi.upload(file))
  input.value = ''
}

async function installFromUrl() {
  const url = urlInput.value.trim()
  if (!url) return
  const ok = await installConfirm(async () => {
    const r = await pluginApi.fromUrl(url)
    urlInput.value = ''
    return r
  })
  // 市场深链的安装落定（成功与否）：横幅使命结束
  if (ok) emit('preset-done')
}

/** 语义化版本比较：old 大于 new 时为降级安装（实现在 pluginMarket.ts，纯函数可测） */
const isDowngrade = market.isDowngrade

/** 安装 / 升级 → 弹权限清单，用户确认后才生效（§8.4）。
 *  返回是否走完弹窗（供市场深链判断是否收横幅）。 */
async function installConfirm(action: () => Promise<{ plugin: Plugin; updated: boolean; permsChanged: boolean; oldVersion?: string }>): Promise<boolean> {
  installing.value = true
  let plugin: Plugin
  let updated = false
  let permsChanged = false
  let oldVersion = ''
  try {
    const r = await action()
    plugin = r.plugin
    updated = r.updated
    permsChanged = r.permsChanged
    oldVersion = r.oldVersion ?? ''
  } catch (e) {
    void MessagePlugin.error(te(e) || t('plugin.installFailed'))
    installing.value = false
    return false
  }
  installing.value = false
  // 安装已经成功：即便清单刷新失败，也要继续弹权限确认，否则用户以为没装上
  try {
    await pluginsStore.load()
  } catch (e) {
    void MessagePlugin.error(te(e))
  }

  const permText = plugin.permissions.length
    ? plugin.permissions.map((p) => permLabel(p)).join(', ')
    : t('plugin.noPerm')
  const hostText = plugin.allowedHosts.length ? plugin.allowedHosts.join(', ') : t('plugin.noPerm')
  // 危险升级：插件正启用，而新代码的权限 / 域名白名单与用户上次授权不同。
  // 新版服务端在写入时已直接停用（见 handler/plugin.go），这里的 needsReconfirm
  // 只兼容旧服务端；取消普通安装 / 同权限升级时不得误停用已启用插件。
  const needsReconfirm = updated && permsChanged && plugin.enabled
  let confirmed = false
  let declined = false
  async function declineReconfirm() {
    if (declined || confirmed || !plugin || !needsReconfirm) return
    declined = true
    try {
      await pluginsStore.setEnabled(plugin.id, false)
      await host.reloadPlugin({ ...plugin, enabled: false })
      void MessagePlugin.warning(t('plugin.declinedDisabled', { name: plugin.name }))
    } catch (e) {
      void MessagePlugin.error(te(e))
    }
  }
  // 依赖提示：装上不等于能启用——依赖缺失/版本不符/未启用时，服务端会拒绝启用。
  const unmet = unmetDeps(plugin, pluginsStore.plugins)
  const depLine = plugin.dependencies?.length
    ? t('plugin.depLine') +
      plugin.dependencies.join(', ') +
      (unmet.length ? `\n${t('plugin.depBlocked', { list: depListText(plugin) })}` : '')
    : ''
  // 版本过渡提示：升级展示 old → new；市场旧包覆盖更高版本（降级）要额外警告
  const verLine =
    updated && oldVersion
      ? isDowngrade(oldVersion, plugin.version)
        ? t('plugin.downgradeWarn', { old: oldVersion, new: plugin.version })
        : t('plugin.updateFromTo', { old: oldVersion, new: plugin.version })
      : ''
  // 服务端遇到权限变化会直接停用新代码（不再等前端补一次停用），因此这里
  // 不能再用 plugin.enabled 判断要不要展示「权限已变化」：停用只是执行结果，
  // 用户仍需要看到新旧权限的差异。
  const baseBody = updated && permsChanged
    ? t('plugin.updatePermChangedBody', {
        perms: permText,
        hosts: hostText,
        desc: plugin.description || '',
      })
    : t('plugin.permBody', {
        perms: permText,
        hosts: hostText,
        desc: plugin.description || '',
      })
  const dialog = DialogPlugin.confirm({
    header: updated
      ? t('plugin.updateTitle', { name: plugin.name, version: plugin.version })
      : t('plugin.installTitle', { name: plugin.name, version: plugin.version }),
    body: [verLine, depLine, baseBody].filter(Boolean).join('\n\n'),
    confirmBtn: updated ? t('plugin.confirmUpdate') : t('plugin.confirmEnable'),
    cancelBtn: needsReconfirm ? t('plugin.declineDisable') : updated ? t('plugin.laterReload') : t('plugin.laterEnable'),
    onConfirm: async () => {
      confirmed = true
      try {
        if (updated) {
          // 升级：保持原启停状态；已启用则立即重载新代码
          if (plugin.enabled) {
            await host.reloadPlugin(plugin)
            void MessagePlugin.success(t('plugin.updatedReloadOk'))
          } else {
            void MessagePlugin.success(t('plugin.updatedSavedOk'))
          }
        } else {
          // 依赖未就绪时不给启用：服务端也会 403，这里直接说清原因并保持停用
          if (unmet.length) {
            void MessagePlugin.warning(t('plugin.depInstalledLater', { list: depListText(plugin) }))
          } else {
            await pluginsStore.setEnabled(plugin.id, true)
            await host.reloadPlugin({ ...plugin, enabled: true })
            void MessagePlugin.success(t('plugin.enabledOk'))
          }
        }
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
    onCancel: declineReconfirm,
    onClose: declineReconfirm,
  })
  // 弹窗已弹出（无论用户确认与否）：市场横幅使命结束
  return true
}

/** 权限标签（未知权限原样显示） */
function permLabel(perm: string): string {
  const key = `plugin.perms.${perm}`
  const label = t(key)
  return label === key ? perm : label
}

// ===== 依赖与兼容性 =====
// 服务端在启用时用同一套规则把关（403 + 原因）；这里只提前把「还差什么」摆出来，
// 免得用户点一次开关才看到错误。

/** 未就绪依赖的可读描述，例如「word-count 未启用」。 */
function depListText(plugin: Plugin): string {
  return unmetDeps(plugin, pluginsStore.plugins)
    .map((i) => {
      if (i.reason === 'missing') return t('plugin.depMissing', { name: i.name })
      if (i.reason === 'version') return t('plugin.depVersion', { name: i.name })
      return t('plugin.depDisabled', { name: i.name })
    })
    .join(t('plugin.depSep'))
}

function hasUnmetDeps(plugin: Plugin): boolean {
  return unmetDeps(plugin, pluginsStore.plugins).length > 0
}

// ===== 启停 / 卸载 =====

async function toggle(plugin: Plugin, enabled: boolean) {
  // 依赖未就绪时直接拦下：服务端也会 403，但先给原因能少一次往返与一次报错
  if (enabled && hasUnmetDeps(plugin)) {
    void MessagePlugin.warning(t('plugin.depBlocked', { list: depListText(plugin) }))
    return
  }
  try {
    await pluginsStore.setEnabled(plugin.id, enabled)
    await host.reloadPlugin({ ...plugin, enabled })
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

async function uninstall(plugin: Plugin) {
  const dialog = DialogPlugin.confirm({
    header: t('plugin.uninstallTitle'),
    body: t('plugin.uninstallBody', { name: plugin.name }),
    confirmBtn: { content: t('plugin.uninstallBtn'), theme: 'danger' },
    // DialogPlugin 未传 cancelBtn 时不渲染取消按钮，卸载这种不可逆操作只剩
    // 一个危险按钮；这里显式补上取消入口（默认值在 feedback.ts，属公共文件）。
    cancelBtn: t('common.cancel'),
    onConfirm: async () => {
      try {
        await pluginsStore.remove(plugin.id)
        await host.reloadPlugin({ ...plugin, enabled: false })
        void MessagePlugin.success(t('plugin.uninstallOk'))
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}
</script>

<template>
  <!-- TDesign 的 Drawer 只有 visible prop（没有 modelValue），必须写全 v-model:visible，
       否则值会被当作 HTML 属性落到 DOM 上，抽屉永远停在屏幕外 -->
  <UiDrawer v-model:visible="visible" :header="t('plugin.title')" size="420px">
    <div class="mc-plugins">
      <!-- 安装 -->
      <div class="mc-plugins__install">
        <div v-if="initialName" class="mc-plugins__market">
          {{ t('plugin.marketPreset', { name: initialName }) }}
        </div>
        <div class="mc-plugins__upload">
          <input
            ref="fileInput"
            type="file"
            accept=".js,.ts,.mts,application/javascript,text/javascript"
            @change="onPickFile"
          />
          <UiButton variant="outline" size="small" :loading="installing" @click="pickFile">
            {{ t('plugin.upload') }}
          </UiButton>
        </div>
        <div class="mc-plugins__url">
          <UiInput v-model="urlInput" :placeholder="t('plugin.urlPlaceholder')" clearable @enter="installFromUrl" />
          <UiButton variant="outline" size="small" :loading="installing" @click="installFromUrl">
            {{ t('plugin.urlInstall') }}
          </UiButton>
        </div>
      </div>

      <!-- 用户插件页面（SDK v3 addPage / setPage） -->
      <div class="mc-plugins__group">
        <div class="mc-plugins__group-title">{{ t('plugin.userPages') }}</div>
        <button
          v-for="item in pluginPages"
          :key="`u-${item.pluginId}-${item.page.id}`"
          type="button"
          class="mc-plugins__page"
          @click="openUserPage(item.pluginId, item.page.id)"
        >
          <span class="mc-plugins__page-name">{{ item.page.title }}</span>
          <span class="mc-plugins__page-sub text-placeholder">{{ item.pluginName }}</span>
        </button>
        <div v-if="!pluginPages.length" class="text-placeholder mc-plugins__pages-empty">
          {{ t('plugin.noUserPages') }}
        </div>
      </div>

      <!-- 站点页面（管理员安装的服务端插件） -->
      <div class="mc-plugins__group">
        <div class="mc-plugins__group-title">{{ t('plugin.sitePages') }}</div>
        <button
          v-for="p in visibleServerPages"
          :key="`s-${p.pluginId}-${p.id}`"
          type="button"
          class="mc-plugins__page"
          @click="openServerPage(String(p.pluginId), p.id)"
        >
          <span class="mc-plugins__page-name">{{ p.title }}</span>
          <span class="mc-plugins__page-sub text-placeholder">{{ p.pluginId }}</span>
        </button>
        <div v-if="!visibleServerPages.length" class="text-placeholder mc-plugins__pages-empty">
          {{ t('plugin.noSitePages') }}
        </div>
      </div>

      <!-- 列表 -->
      <div v-for="p in pluginsStore.plugins" :key="p.id" class="mc-plugins__item">
        <div class="mc-plugins__head">
          <span class="mc-plugins__name">
            {{ p.name }}
            <span class="text-placeholder num">v{{ p.version }}</span>
          </span>
          <UiSwitch
            :value="p.enabled && !p.globalDisabled"
            :disabled="p.globalDisabled || (!p.enabled && hasUnmetDeps(p))"
            @change="(v: unknown) => toggle(p, v as boolean)"
          />
        </div>
        <div class="mc-plugins__desc text-secondary">{{ p.description || t('plugin.noDesc') }}</div>
        <div class="mc-plugins__tags">
          <UiTag v-for="perm in p.permissions" :key="perm" size="small" theme="primary" variant="light">
            {{ permLabel(perm) }}
          </UiTag>
          <UiTag v-for="h in p.allowedHosts" :key="h" size="small" variant="outline">{{ h }}</UiTag>
          <UiTag v-for="d in p.dependencies" :key="`dep-${d}`" size="small" variant="outline">
            ⟵ {{ d }}
          </UiTag>
          <UiTag v-if="sdkRequirement(p)" size="small" variant="outline">
            SDK {{ sdkRequirement(p) }}
          </UiTag>
          <UiTag v-if="p.globalDisabled" size="small" theme="danger" variant="light">
            {{ t('plugin.globalDisabled') }}
          </UiTag>
        </div>
        <!-- 依赖未就绪：开关禁用并在原地说明原因，比点一下再弹错更好理解 -->
        <div v-if="!p.enabled && hasUnmetDeps(p)" class="mc-plugins__dep-warn">
          {{ t('plugin.depBlocked', { list: depListText(p) }) }}
        </div>
        <div class="mc-plugins__ops">
          <a class="mc-plugins__remove" @click="uninstall(p)">{{ t('plugin.uninstall') }}</a>
        </div>
      </div>

      <div v-if="!pluginsStore.plugins.length" class="text-placeholder mc-plugins__empty">
        {{ t('plugin.empty') }}
      </div>
    </div>
  </UiDrawer>
</template>

<style scoped>
.mc-plugins {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.mc-plugins__install {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 16px;
  border: 1px dashed var(--td-component-border);
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-secondarycontainer);
}

.mc-plugins__upload input {
  display: none;
}

.mc-plugins__market {
  padding: 8px 10px;
  border-radius: var(--mc-radius-md);
  background: var(--td-brand-color-1, rgba(0, 82, 217, 0.1));
  color: var(--td-text-color-primary);
  font-size: var(--mc-fs-sm);
  line-height: 1.6;
}

.mc-plugins__url {
  display: flex;
  gap: 8px;
}

.mc-plugins__group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.mc-plugins__group-title {
  font-size: var(--mc-fs-sm);
  font-weight: 600;
  color: var(--td-text-color-secondary);
}

.mc-plugins__page {
  border: none;
  background: var(--td-bg-color-container);
  padding: 8px 10px;
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
  text-align: left;
  font: inherit;
  cursor: pointer;
  border-radius: var(--mc-radius-md);
  transition: background var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-plugins__page:hover {
  background: var(--td-bg-color-secondarycontainer);
}

.mc-plugins__page-name {
  font-size: var(--mc-fs-body);
  color: var(--td-text-color-primary);
}

.mc-plugins__page-sub {
  font-size: var(--mc-fs-caption);
  flex-shrink: 0;
}

.mc-plugins__pages-empty {
  font-size: var(--mc-fs-caption);
  padding: 2px 0;
}

.mc-plugins__item {
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  padding: 16px;
  transition:
    border-color var(--mc-dur-fast) var(--mc-ease-out),
    box-shadow var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-plugins__item:hover {
  border-color: var(--td-component-border);
  box-shadow: var(--mc-shadow-1);
}

.mc-plugins__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.mc-plugins__name {
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: 6px;
  font-size: 14px;
  font-weight: 600;
}

.mc-plugins__desc {
  font-size: var(--mc-fs-sm);
  line-height: 1.6;
  margin: 8px 0;
  white-space: pre-wrap;
}

.mc-plugins__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.mc-plugins__dep-warn {
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.5;
  /* warning token：提示但不抢过 danger 的视觉权重 */
  color: var(--td-warning-color, #ed9f11);
}

.mc-plugins__ops {
  margin-top: 10px;
  text-align: right;
}

.mc-plugins__remove {
  font-size: 12px;
  color: var(--td-error-color);
  cursor: pointer;
}

.mc-plugins__remove:hover {
  text-decoration: underline;
}

.mc-plugins__empty {
  text-align: center;
  padding: 48px 0;
  font-size: 13px;
}
</style>
