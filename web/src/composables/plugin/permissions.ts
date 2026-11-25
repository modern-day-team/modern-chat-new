/**
 * 插件 API / 事件的权限映射表（§8.3）。
 *
 * 沙箱每条出站调用都按这里的表查权限：插件 manifest 里没声明对应权限就拒答。
 * 表是纯数据——新增 API 时在此登记，判定逻辑（宿主 frame 分发）不感知具体条目。
 */

/** API 名 -> 所需权限 */
export const API_PERMISSION: Record<string, string> = {
  // 注意：api.on 不走 RPC（沙箱内纯本地注册），不在此表——事件门槛见 EVENT_PERMISSION
  send: 'message:send',
  registerMessageRenderer: 'message:render',
  addSidePanel: 'ui:panel',
  removeSidePanel: 'ui:panel',
  'storage.get': 'storage',
  'storage.set': 'storage',
  request: 'net',
  'theme.apply': 'ui:theme',
  // 扩展点（§8.3）
  registerMessageAction: 'ui:actions',
  me: 'conv:read',
  'conv.current': 'conv:read',
  'conv.members': 'conv:read',
  'messages.recent': 'conv:read',
  // 全局搜索会跨会话返回正文，按正文门槛收敛
  'messages.search': 'message:read',
  'conv.list': 'conv:read',
  'users.search': 'conv:read',
  // 草稿与剪贴板都是宿主 UI 动作，复用 ui:host，不引入新权限名
  'draft.get': 'ui:host',
  'draft.set': 'ui:host',
  'clipboard.writeText': 'ui:host',
  'nav.openConversation': 'ui:host',
  'nav.jumpToMessage': 'ui:host',
  toast: 'ui:host',
  'storage.remove': 'storage',
  'storage.keys': 'storage',
  // SDK v3：全站页面（需 ui:page）与服务端用户数据（需 data）
  addPage: 'ui:page',
  removePage: 'ui:page',
  setPage: 'ui:page',
  'data.get': 'data',
  'data.set': 'data',
  'data.remove': 'data',
  'data.keys': 'data',
  'nav.openPluginPage': 'ui:host',
  // SDK v4：服务端统一 AI 代理（密钥不下发，插件只拿到回答）
  'ai.status': 'ai',
  'ai.chat': 'ai',
  // SDK v5：会话写操作。conv.files 虽是读取，按任务约定归 conv:write；
  // 消息历史是只读正文，门槛仍是 conv:read
  'conv.createSingle': 'conv:write',
  'conv.createGroup': 'conv:write',
  'conv.rename': 'conv:write',
  'conv.invite': 'conv:write',
  'conv.leave': 'conv:write',
  'conv.join': 'conv:write',
  'conv.setPrefs': 'conv:write',
  'conv.remove': 'conv:write',
  'conv.clear': 'conv:write',
  'conv.files': 'conv:write',
  'conv.addFile': 'conv:write',
  'conv.removeFile': 'conv:write',
  'conv.setAnnouncement': 'conv:write',
  'conv.kick': 'conv:write',
  'conv.setAdmin': 'conv:write',
  'conv.mute': 'conv:write',
  'conv.transfer': 'conv:write',
  'conv.createInviteLink': 'conv:write',
  'message.history': 'conv:read',
  // 消息写动作统一按发送门槛收敛（含「仅自己删除」的 REST 端点）
  'message.delete': 'message:send',
  'message.recall': 'message:send',
  'message.edit': 'message:send',
  'message.react': 'message:send',
  'message.favorite': 'message:send',
  'message.report': 'message:send',
  // 账号关系动作复用 ui:host，不新增权限名
  'users.blockStatus': 'ui:host',
  'users.block': 'ui:host',
  'users.unblock': 'ui:host',
  'file.upload': 'file:upload',
}

/**
 * 事件 -> 接收方必须持有的权限（订阅与投递共用同一张表）。
 * message:new/recall 的 payload 是整条消息（含正文），只发给声明 message:read 的插件；
 * presence 同样带 userId 列表，按同一权限收敛。
 *
 * api.on 在沙箱内是纯本地注册、无权限门槛，真正把关的是这里的投递过滤——
 * 没登记的事件要么本身定向投递（只发给该插件），要么 payload 无敏感数据。
 */
export const EVENT_PERMISSION: Record<string, string> = {
  'message:new': 'message:read',
  'message:recall': 'message:read',
  presence: 'message:read',
  // ui:action 由 panelAction 定向投递给「拥有该面板」的插件；
  // 登记进表固化语义：能创建面板的插件才可能收到自己的面板动作
  'ui:action': 'ui:panel',
  // message:action 由 runMessageAction 定向投递，投递点已校验 ui:actions，
  // 无 message:read 时 payload 降级为消息元信息（见 usePluginHost.messageMeta）
  'message:action': 'ui:actions',
  // conv:open 故意不设门槛：payload 只有 convId、不含内容；设门槛（如 conv:read）
  // 会逼插件为收会话切换信号申请更宽的权限——与最小权限相反
}
