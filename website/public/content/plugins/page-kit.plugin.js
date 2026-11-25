/* {
  "name": "page-kit",
  "version": "1.0.0",
  "description": "示例（SDK v3）：全站页面 + 服务端用户数据 KV + 面板入口",
  "permissions": ["ui:panel", "ui:page", "ui:host", "data"],
  "allowedHosts": []
} */

/**
 * 演示 SDK v3 新增能力：
 *   1. addPage / setPage —— 注册全站页面，抽屉「插件页面」里可打开
 *   2. data.get / set / keys / remove —— 服务端按「用户 + 插件 + key」隔离的 KV
 *   3. nav.openPluginPage —— 从侧栏面板跳到自己的页面
 *
 * 安装：聊天页 → 插件 → 上传本文件 → 确认权限 → 启用。
 * 注意：SDK 不暴露插件自身 id；面板里的跳转按钮需要把下面的 PLUGIN_ID 改成
 * 「插件管理」里看到的数字 id（仅用于演示 nav.openPluginPage，页面本身不依赖它）。
 */

var PLUGIN_ID = 0

var state = {
  draft: '',
  hint: '页面按钮写入的内容会保存在服务端，刷新后仍在。',
}

function escapeCell(value) {
  return String(value ?? '')
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/\n/g, ' ')
}

function rowsJson(keys) {
  var rows = []
  for (var i = 0; i < keys.length && i < 10; i++) {
    rows.push('["' + escapeCell(keys[i]) + '","' + escapeCell(state.saved[keys[i]]) + '"]')
  }
  return '[' + rows.join(',') + ']'
}

state.saved = {}

async function renderPanel(api) {
  await api.addSidePanel({
    title: '页面工具箱',
    body: [
      { component: 'badge', props: { text: 'SDK ' + api.version, tone: api.version === '3' ? 'success' : 'brand' } },
      { component: 'text', props: { content: '演示全站页面与服务端数据 KV。' } },
      { component: 'button', props: { text: '写入一条数据', theme: 'primary' }, action: 'panelWrite' },
      { component: 'button', props: { text: '打开我的页面' }, action: 'openPage' },
      { component: 'text', props: { content: state.hint } },
    ],
  })
}

async function renderPage(api) {
  var keys = await api.data.keys()
  var saved = {}
  for (var i = 0; i < keys.length && i < 10; i++) saved[keys[i]] = await api.data.get(keys[i])
  state.saved = saved

  await api.addPage({
    id: 'page-kit',
    title: '页面工具箱示例',
    icon: '页',
    scope: 'user',
    body: [
      { component: 'badge', props: { text: 'SDK v3 · 用户数据', tone: 'success' } },
      { component: 'text', props: { content: state.hint } },
      {
        component: 'input',
        props: { placeholder: '输入要保存的内容…', value: state.draft },
        action: 'draft',
      },
      { component: 'button', props: { text: '保存到 data', theme: 'primary' }, action: 'save' },
      { component: 'button', props: { text: '刷新数据' }, action: 'refresh' },
      keys.length
        ? { component: 'table', props: { headers: '键,值', rows: rowsJson(keys) } }
        : { component: 'text', props: { content: '还没有数据，点「保存到 data」试试。' } },
    ],
  })
}

function __mcActivate(api) {
  api.on('ui:action', async function (p) {
    var action = p && p.action
    var value = p && p.value
    try {
      if (action === 'draft') state.draft = String(value ?? '')
      else if (action === 'save') {
        if (!state.draft) {
          state.hint = '请先输入内容。'
        } else {
          await api.data.set('note', state.draft)
          state.hint = '已保存：「' + state.draft + '」'
          state.draft = ''
        }
      } else if (action === 'refresh') state.hint = '已刷新。'
      else if (action === 'panelWrite') {
        await api.data.set('panel_note', new Date().toISOString())
        state.saved.panel_note = await api.data.get('panel_note')
        state.hint = '已从面板写入 panel_note。'
      } else if (action === 'openPage') {
        if (PLUGIN_ID > 0) await api.nav.openPluginPage(PLUGIN_ID, 'page-kit')
        else await api.toast('请先在示例里填写 PLUGIN_ID（插件列表中的数字 id）', 'warning')
      } else return

      await renderPage(api)
      await renderPanel(api)
    } catch (e) {
      state.hint = '操作失败：' + ((e && e.message) || e)
      await renderPage(api)
      await renderPanel(api)
    }
  })

  void renderPage(api).catch(function () { /* 页面注册失败：不影响面板 */ })
  void renderPanel(api).catch(function () { /* 面板注册失败：忽略 */ })
}
