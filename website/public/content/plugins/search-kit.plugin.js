/* {
  "name": "search-kit",
  "version": "1.0.0",
  "description": "示例（SDK v2）：会话列表 / 全局消息搜索 / 草稿 / 剪贴板 / 存储管理 + 新描述式控件",
  "permissions": ["ui:panel", "conv:read", "message:read", "ui:host", "storage"],
  "allowedHosts": []
} */

/**
 * 演示 SDK v2 新增能力：
 *   conv.list / messages.search / draft.get / draft.set / clipboard.writeText
 *   storage.keys / storage.remove
 *   textarea / checkbox / switch / segmented 四种描述式控件
 * 安装：聊天页 → 插件 → 上传本文件；启用后在详情面板操作。
 */

let state = {
  scope: 'all',
  keyword: '',
  copyFirst: false,
  includeDraft: true,
  rows: [],
  hint: '输入关键词后点「搜索」。',
}

function escapeCell(value) {
  return String(value ?? '')
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/\n/g, ' ')
}

function render(api) {
  const rows = state.rows.slice(0, 20).map((h) => {
    const where = h.convName || `会话 ${h.conversationId}`
    return `["${escapeCell(where)}","${escapeCell(h.senderNickname)}","${escapeCell(h.content)}"]`
  })

  return api.addSidePanel({
    title: '搜索工具箱',
    body: [
      { component: 'badge', props: { text: `SDK ${api.version}`, tone: api.version === '2' ? 'success' : 'brand' } },
      { component: 'text', props: { content: state.hint } },
      {
        component: 'textarea',
        props: { placeholder: '输入要搜索的消息内容…', value: state.keyword, rows: 2, maxlength: 200 },
        action: 'keyword',
      },
      {
        component: 'segmented',
        props: { options: '["全部会话","仅当前会话"]', value: state.scope === 'all' ? '全部会话' : '仅当前会话' },
        action: 'scope',
      },
      {
        component: 'checkbox',
        props: { text: '复制第一条结果', checked: state.copyFirst },
        action: 'copyFirst',
      },
      {
        component: 'switch',
        props: { text: '把搜索词填入输入框', value: state.includeDraft },
        action: 'includeDraft',
      },
      { component: 'button', props: { text: '搜索消息', theme: 'primary' }, action: 'search' },
      { component: 'divider', props: {} },
      { component: 'button', props: { text: '查看我的会话' }, action: 'convs' },
      { component: 'button', props: { text: '存储键清理' }, action: 'storage' },
      ...(rows.length
        ? [
            { component: 'text', props: { content: `命中 ${state.rows.length} 条，最多展示 20 条` } },
            { component: 'table', props: { headers: '会话,发送者,内容', rows: `[${rows.join(',')}]` } },
          ]
        : []),
    ],
  })
}

function refresh(api) {
  return render(api)
}

function __mcActivate(api) {
  api.on('ui:action', async ({ action, value }) => {
    try {
      if (action === 'keyword') state.keyword = String(value ?? '')
      else if (action === 'scope') state.scope = value === '仅当前会话' ? 'current' : 'all'
      else if (action === 'copyFirst') state.copyFirst = value === true
      else if (action === 'includeDraft') state.includeDraft = value === true
      else if (action === 'search') {
        const kw = state.keyword.trim()
        if (!kw) {
          state.hint = '请先输入关键词。'
          state.rows = []
          return refresh(api)
        }
        const opts = { limit: 50 }
        if (state.scope === 'current') {
          const conv = await api.conv.current()
          if (conv) opts.convId = conv.id
        }
        state.rows = (await api.messages.search(kw, opts)) || []
        state.hint = state.rows.length ? `找到 ${state.rows.length} 条结果。` : '没有匹配的消息。'
        if (state.copyFirst && state.rows[0]) {
          await api.clipboard.writeText(state.rows[0].content)
          state.hint += ' 已复制第一条。'
        }
        if (state.includeDraft) await api.draft.set(kw)
      } else if (action === 'convs') {
        const convs = (await api.conv.list()) || []
        state.rows = convs.map((c) => ({
          conversationId: c.id,
          convName: c.name || c.peerNickname || `会话 ${c.id}`,
          senderNickname: c.type === 2 ? `群聊 · ${c.memberCount} 人` : '单聊',
          content: `ID ${c.id}`,
        }))
        state.hint = `你有 ${convs.length} 个会话。`
      } else if (action === 'storage') {
        const keys = (await api.storage.keys()) || []
        for (const key of keys.slice(5)) await api.storage.remove(key)
        state.hint = `本插件有 ${keys.length} 个存储键，已保留前 5 个。`
        state.rows = []
      }
      await refresh(api)
    } catch (e) {
      state.hint = `操作失败：${(e && e.message) || e}`
      await refresh(api)
    }
  })

  void refresh(api)
}
