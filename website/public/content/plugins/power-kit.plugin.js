/* {
  "name": "power-kit",
  "version": "1.0.0",
  "description": "示例（SDK v5）：会话/消息写操作 + 宿主文件上传 + tabs/steps/empty/loading/upload",
  "permissions": ["ui:panel", "ui:page", "conv:read", "conv:write", "message:send", "file:upload"],
  "allowedHosts": [],
  "engines": { "sdk": ">=5" }
} */

/**
 * 演示 SDK v5 新增能力：
 *   1. conv.createGroup / conv.rename —— 建群并改名（需 conv:write）
 *   2. file.upload —— 宿主 FileReader 读出的 dataUrl 交给宿主上传（需 file:upload）
 *   3. message.history —— 分页读取当前会话历史（需 conv:read，撤回正文为空串）
 *   4. tabs / steps / empty / loading / upload —— 新描述式 UI 组件
 *
 * 安装：聊天页 → 插件 → 上传本文件 → 确认权限 → 启用。
 */

var state = {
  tab: 0,
  step: 0,
  groupName: 'SDK v5 体验群',
  hint: '',
  busy: false,
  history: [],
  hasMore: false,
  uploadResult: null,
  lastConvId: 0,
}

function esc(value) {
  return String(value ?? '')
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/\n/g, ' ')
}

function historyRows(messages) {
  var rows = []
  for (var i = 0; i < messages.length && i < 8; i++) {
    var m = messages[i]
    var content = m.recalled ? '（已撤回）' : String(m.content || '')
    rows.push('["' + esc(m.senderId) + '","' + esc(m.msgType) + '","' + esc(content) + '"]')
  }
  return '[' + rows.join(',') + ']'
}

function toolsBody() {
  var body = [
    { component: 'steps', props: { items: '["输入群名","创建群聊","改名完成"]', active: state.step } },
    { component: 'input', props: { placeholder: '输入新群名（≤60 字）', value: state.groupName }, action: 'groupName' },
    { component: 'button', props: { text: '创建群聊', theme: 'primary' }, action: 'createGroup' },
    { component: 'button', props: { text: '给当前会话改名' }, action: 'rename' },
    { component: 'upload', props: { accept: '*', maxSize: 262144 }, action: 'pickFile' },
  ]
  body.push(
    state.uploadResult
      ? { component: 'text', props: { content: '已上传：' + state.uploadResult.url } }
      : { component: 'text', props: { content: '选择文件后由宿主读取并上传（≤256KB）。' } },
  )
  return body
}

function historyBody() {
  if (state.busy) return [{ component: 'loading', props: { text: '正在读取历史…' } }]
  if (!state.history.length) {
    return [{ component: 'empty', props: { icon: '空', text: '还没有读取历史，点下面的按钮试试。' } }]
  }
  return [
    { component: 'table', props: { headers: '发送者,类型,内容', rows: historyRows(state.history) } },
    {
      component: 'text',
      props: { content: state.hasMore ? '还有更早的消息（本次只取 5 条）。' : '已到最早一条。' },
    },
  ]
}

function aboutBody() {
  return [
    { component: 'text', props: { content: '本示例使用 SDK v' + state.sdkVersion + '。' } },
    {
      component: 'text',
      props: {
        content:
          '权限：ui:panel / ui:page / conv:read / conv:write / message:send / file:upload。文件选择由宿主 FileReader 读取，插件拿不到 DOM 与 JWT。',
      },
    },
  ]
}

function panelBody() {
  return [
    { component: 'badge', props: { text: 'SDK ' + state.sdkVersion, tone: 'success' } },
    {
      component: 'tabs',
      props: { labels: '["工具","历史","说明"]', active: state.tab },
      action: 'tab',
      children: [
        { component: 'card', props: {}, children: toolsBody() },
        { component: 'card', props: {}, children: historyBody() },
        { component: 'card', props: {}, children: aboutBody() },
      ],
    },
    {
      component: 'button',
      props: { text: state.busy ? '读取中…' : '读取当前会话历史', theme: 'primary' },
      action: 'loadHistory',
    },
    { component: 'text', props: { content: state.hint } },
  ]
}

function pageBody() {
  return [
    { component: 'badge', props: { text: 'Power Kit · SDK ' + state.sdkVersion, tone: 'brand' } },
    {
      component: 'tabs',
      props: { labels: '["工具","历史","说明"]', active: state.tab },
      action: 'tab',
      children: [
        { component: 'card', props: {}, children: toolsBody() },
        { component: 'card', props: {}, children: historyBody() },
        { component: 'card', props: {}, children: aboutBody() },
      ],
    },
    { component: 'text', props: { content: state.hint } },
  ]
}

async function render(api) {
  state.sdkVersion = api.version
  await api.addSidePanel({ title: 'Power Kit · SDK ' + api.version, body: panelBody() })
  await api.addPage({
    id: 'power-kit',
    title: 'Power Kit 示例',
    icon: '力',
    scope: 'user',
    body: pageBody(),
  })
}

function __mcActivate(api) {
  state.sdkVersion = api.version

  api.on('ui:action', async function (p) {
    var action = p && p.action
    var value = p && p.value
    try {
      if (action === 'tab') {
        state.tab = Number(value) || 0
      } else if (action === 'groupName') {
        state.groupName = String(value ?? '').slice(0, 60)
      } else if (action === 'createGroup') {
        var conv = await api.conv.createGroup(state.groupName || 'SDK v5 体验群')
        state.lastConvId = conv.id
        state.step = 1
        state.hint = '已创建群聊 #' + conv.id + '（' + conv.name + '）。'
      } else if (action === 'rename') {
        var current = await api.conv.current()
        var id = state.lastConvId || (current && current.id) || 0
        if (!id) {
          state.hint = '请先打开一个会话，或先创建一个群聊。'
        } else {
          await api.conv.rename(id, state.groupName || 'SDK v5 体验群')
          state.step = 2
          state.hint = '已将 #' + id + ' 改名为「' + state.groupName + '」。'
        }
      } else if (action === 'pickFile') {
        var file = value && typeof value === 'object' ? value : null
        if (!file || typeof file.dataUrl !== 'string') return
        var type = String(file.type || '')
        var kind =
          type.indexOf('image/') === 0
            ? 'image'
            : type.indexOf('audio/') === 0
              ? 'audio'
              : type.indexOf('video/') === 0
                ? 'video'
                : 'file'
        state.busy = true
        await render(api)
        var result = await api.file.upload(kind, file.dataUrl, String(file.name || 'file'))
        state.uploadResult = result
        state.hint = '已上传 ' + file.name + '（' + result.size + ' 字节）。'
      } else if (action === 'pickFile:error') {
        state.hint = '文件读取失败：' + ((value && value.message) || '未知错误')
      } else if (action === 'loadHistory') {
        var convNow = await api.conv.current()
        if (!convNow) {
          state.hint = '请先打开一个会话。'
        } else {
          state.busy = true
          state.tab = 1
          await render(api)
          var page = await api.message.history(convNow.id, { limit: 5 })
          state.history = page.messages
          state.hasMore = page.hasMore === true
          state.hint = '已读取 ' + page.messages.length + ' 条历史' + (state.hasMore ? '（还有更早消息）' : '') + '。'
        }
      } else {
        return
      }
    } catch (e) {
      state.hint = '操作失败：' + ((e && e.message) || e)
    } finally {
      state.busy = false
      await render(api)
    }
  })

  void render(api).catch(function () { /* 面板/页面注册失败：忽略 */ })
}
