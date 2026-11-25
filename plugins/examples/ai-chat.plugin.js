/* {
  "name": "ai-chat",
  "version": "1.0.0",
  "description": "示例：服务端统一 AI 代理的全站对话页面",
  "permissions": ["ui:page", "ai"],
  "allowedHosts": []
} */

/**
 * SDK v4 示例：插件页面 + 服务端 AI 代理。
 * API Key 只在服务端环境变量里，插件只收发消息，不接触密钥。
 * 安装：插件抽屉上传 → 启用 → 打开“AI 对话”页面。
 */

var state = {
  draft: '',
  busy: false,
  enabled: false,
  model: '',
  messages: [],
  hint: '输入问题后发送。回答由本站服务端代理请求模型。',
}

function transcript() {
  if (!state.messages.length) return '还没有对话。'
  return state.messages
    .map(function (m) {
      return (m.role === 'user' ? '你：' : 'AI：') + m.content
    })
    .join('\n\n')
}

async function render(api) {
  var status = await api.ai.status()
  state.enabled = !!(status && status.enabled)
  state.model = (status && status.model) || ''
  if (!state.enabled) state.hint = '管理员尚未配置服务端 AI 代理（AI_BASE_URL / AI_API_KEY / AI_MODEL）。'

  await api.setPage({
    id: 'ai-chat',
    title: 'AI 对话',
    icon: 'AI',
    scope: 'user',
    body: [
      {
        component: 'badge',
        props: {
          text: state.enabled ? '服务端代理 · ' + state.model : 'AI 未启用',
          tone: state.enabled ? 'success' : 'warning',
        },
      },
      { component: 'text', props: { content: state.hint } },
      { component: 'text', props: { content: transcript() } },
      {
        component: 'textarea',
        props: { placeholder: '输入问题…', value: state.draft, rows: 3, maxlength: 4000 },
        action: 'draft',
      },
      {
        component: 'button',
        props: { text: state.busy ? '生成中…' : '发送', theme: 'primary' },
        action: 'send',
      },
      { component: 'button', props: { text: '清空对话' }, action: 'clear' },
    ],
  })
}

function __mcActivate(api) {
  api.on('ui:action', async function (p) {
    var action = p && p.action
    var value = p && p.value
    try {
      if (action === 'draft') {
        state.draft = String(value == null ? '' : value)
        return
      }
      if (action === 'clear') {
        state.messages = []
        state.hint = '对话已清空。'
        await render(api)
        return
      }
      if (action !== 'send' || state.busy || !state.enabled) return
      var text = String(state.draft || '').trim()
      if (!text) {
        state.hint = '请先输入问题。'
        await render(api)
        return
      }
      state.messages.push({ role: 'user', content: text })
      state.draft = ''
      state.busy = true
      state.hint = '正在等待服务端代理…'
      await render(api)

      var result = await api.ai.chat(
        state.messages.slice(-20).map(function (m) {
          return { role: m.role, content: m.content }
        }),
      )
      state.messages.push({ role: 'assistant', content: String((result && result.content) || '') })
      state.hint = '回答完成。'
    } catch (e) {
      state.hint = '请求失败：' + ((e && e.message) || e)
    } finally {
      state.busy = false
      await render(api).catch(function () {})
    }
  })

  void render(api).catch(function () {})
}
