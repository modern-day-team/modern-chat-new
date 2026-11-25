/**
 * ModernChat 插件 SDK 类型声明（供 TypeScript 写插件时使用）。
 *
 * 用法：在你的 .ts 插件第一行加
 *   /// <reference path="../modernchat-sdk.d.ts" />
 * 然后照常写 `function __mcActivate(api: McApi) { ... }`。
 * 插件以单文件上传，服务端用 esbuild 去掉类型后入库，不需要本地构建。
 */

/** 会话里的一条消息（只读摘要） */
interface McMessageLite {
  id: number
  senderId: number
  msgType: string
  content: string
  createdAt: string
  recalled: boolean
}

interface McConvInfo {
  id: number
  type: 1 | 2 | 3
  name: string
  peerNickname: string
  memberCount: number
}

/** SDK v5：群文件目录项（conv.files / conv.addFile 返回） */
interface McConversationFile {
  id: number
  name: string
  url: string
  size: number
  uploaderId: number
  uploaderName: string
  createdAt: string
}

/** SDK v5：宿主文件上传结果（file.upload 返回） */
interface McUploadResult {
  url: string
  size: number
  kind: 'image' | 'audio' | 'sticker' | 'file' | 'video'
}

/** SDK v5：分页消息历史（message.history 返回；撤回消息 content 为空串） */
interface McHistoryPage {
  messages: McMessageLite[]
  hasMore: boolean
}

/** 消息搜索命中（messages.search 结果） */
interface McSearchHit {
  messageId: number
  conversationId: number
  convType: 1 | 2 | 3
  convName: string
  senderId: number
  senderNickname: string
  msgType: string
  content: string
  createdAt: string
}

interface McMemberLite {
  userId: number
  nickname: string
  role: string
}

/** 站内用户目录条目（users.search 结果；绝不含 email） */
interface McUserLite {
  id: number
  username: string
  nickname: string
  avatarColor: string
}

/** 右键菜单里被点击的那条消息 */
interface McActionMessage {
  id: number
  conversationId: number
  senderId: number
  msgType: string
  content: string
  createdAt: string
}

type McPrimitive = string | number | boolean | null

interface McUIDescNode {
  /** text | image | button | input | textarea | checkbox | switch | segmented | select | card | list | divider | link | table | progress | badge | tabs | steps | empty | loading | upload */
  component: string
  props: Record<string, McPrimitive>
  /** 交互控件触发时回传给 on('ui:action') 的动作名 */
  action?: string
  children?: McUIDescNode[]
}

interface McPanelDesc {
  title?: string
  body: McUIDescNode[]
}

/** SDK v3：插件注册的全站页面（需 ui:page；同一 id 再次注册即覆盖） */
interface McPageDesc {
  /** 路由片段，^[a-z0-9_-]{1,40}$，同一插件内唯一 */
  id: string
  /** 1–40 字符 */
  title: string
  icon?: string
  /** user=所有登录用户（默认），admin=仅站长可见 */
  scope?: 'user' | 'admin'
  /** 白名单描述式 UI 节点，最多 100 个 */
  body: McUIDescNode[]
}

interface McRequestOptions {
  method?: string
  headers?: Record<string, string>
  body?: string
}

interface McResponse {
  status: number
  ok: boolean
  /** JSON 可解析时是对象，否则是原文 */
  body: unknown
}

/** 插件可监听的事件 */
type McEvent =
  | 'message:new'
  | 'message:recall'
  | 'presence'
  | 'conv:open'
  | 'ui:action'
  | 'message:action'

interface McAIMessage {
  role: 'system' | 'user' | 'assistant'
  content: string
}

interface McApi {
  /** 宿主 API 级别：'5' = 会话/消息写操作 + 宿主文件上传（'4' 为 AI 代理，'3' 为全站页面 + 用户数据） */
  version: string
  /** 监听事件。门槛按事件而定：message:new/recall、presence 需 message:read；ui:action / message:action 只投递给注册者本身（见 docs/插件开发.md §5） */
  on(event: McEvent, fn: (payload: unknown) => void): void
  /** 发消息，msgType 可自定义（需 message:send） */
  send(convId: number, payload: unknown, msgType?: string): Promise<void>
  /** 注册自定义消息类型的渲染器（需 message:render） */
  registerMessageRenderer(type: string, fn: (payload: unknown) => McPanelDesc | McUIDescNode): Promise<void>
  /** 侧边栏面板（需 ui:panel）；再次调用即更新内容 */
  addSidePanel(desc: McPanelDesc): Promise<void>
  /** 收起自己的面板（需 ui:panel）；重新展示用 addSidePanel */
  removeSidePanel(): Promise<void>
  /** SDK v3：注册/覆盖全站页面（需 ui:page），页面按钮动作通过 on('ui:action') 回传 */
  addPage(desc: McPageDesc): Promise<void>
  /** SDK v3：覆盖指定 id 的页面（需 ui:page） */
  setPage(desc: McPageDesc): Promise<void>
  /** SDK v3：移除指定 id 的页面（需 ui:page） */
  removePage(id: string): Promise<void>
  /** 注册消息右键菜单项（需 ui:actions）；点击后收到 on('message:action', { actionId, message }) */
  registerMessageAction(desc: { id: string; label: string }): Promise<void>

  /** 当前用户身份：只含 id / 昵称（需 conv:read） */
  me(): Promise<{ id: number; nickname: string } | null>

  /** 当前会话只读数据（需 conv:read）；SDK v5 写操作需 conv:write */
  conv: {
    current(): Promise<McConvInfo | null>
    members(): Promise<McMemberLite[]>
    /** 我的会话列表摘要（≤50 条；不含未读 / 消息预览 / 头像，需 conv:read） */
    list(): Promise<McConvInfo[]>
    /** SDK v5：创建单聊 / 群聊，返回新会话摘要 */
    createSingle(userId: number): Promise<McConvInfo>
    createGroup(name: string): Promise<McConvInfo>
    rename(id: number, name: string): Promise<void>
    /** 拉人入群（只能邀请好友；userIds 最多 50 个正整数） */
    invite(id: number, userIds: number[]): Promise<{ added: number }>
    leave(id: number): Promise<void>
    join(id: number): Promise<void>
    /** 会话个人设置：只接受 pinned/muted/archived 三个布尔字段 */
    setPrefs(id: number, prefs: { pinned?: boolean; muted?: boolean; archived?: boolean }): Promise<{
      pinned: boolean
      muted: boolean
      archived: boolean
    }>
    /** 从自己的会话列表移除（不退群、不删消息） */
    remove(id: number): Promise<void>
    /** 清空我在该会话的记录；upToId 为可选游标 */
    clear(id: number, upToId?: number): Promise<{ clearedBeforeId: number }>
    /** 群文件目录（需 conv:write，≤100 条） */
    files(id: number): Promise<McConversationFile[]>
    addFile(id: number, file: { name: string; url: string; size: number }): Promise<McConversationFile>
    removeFile(fileId: number): Promise<void>
    /** 群公告（仅群主）；空串撤销 */
    setAnnouncement(id: number, content: string): Promise<void>
    kick(id: number, userId: number): Promise<void>
    setAdmin(id: number, userId: number, admin: boolean): Promise<void>
    /** minutes=0 解除禁言；上限 30 天 */
    mute(id: number, userId: number, minutes: number): Promise<{ mutedUntil: string | null }>
    transfer(id: number, userId: number): Promise<void>
    /** expiresDays=0 表示永久；省略默认 7 天 */
    createInviteLink(id: number, expiresDays?: number): Promise<{ token: string; path: string; expiresAt: string | null }>
  }
  messages: {
    /** 最近 n 条（1–50），按时间正序 */
    recent(n?: number): Promise<McMessageLite[]>
    /** 全局消息搜索（需 message:read）；kw ≤50 字，limit 1–50，convId 可限定会话 */
    search(kw: string, opts?: { convId?: number; limit?: number }): Promise<McSearchHit[]>
  }
  /** SDK v5：消息历史（需 conv:read）与写操作（需 message:send） */
  message: {
    /** 分页历史；afterId 优先（断线补齐），否则按 beforeId 向上翻；limit 1–50 */
    history(convId: number, opts?: { beforeId?: number; afterId?: number; limit?: number }): Promise<McHistoryPage>
    /** 仅自己删除（对方仍可见） */
    delete(convId: number, messageId: number): Promise<void>
    /** 撤回自己的消息（服务端复核时限） */
    recall(messageId: number): Promise<void>
    /** 编辑自己的文本消息；返回 false 表示本地校验未通过 */
    edit(messageId: number, content: string): Promise<boolean>
    /** emoji ≤16 字；remove=true 取消回应 */
    react(messageId: number, emoji: string, remove?: boolean): Promise<boolean>
    /** favorite=false 取消收藏 */
    favorite(messageId: number, favorite?: boolean): Promise<void>
    /** reason ≤200 字；重复举报返回 duplicate=true */
    report(messageId: number, reason: string): Promise<{ ok: true; duplicate: boolean }>
  }

  /** 当前会话输入框草稿（需 ui:host）；插件写入后输入区立即同步，编辑消息时写入会被拒绝 */
  draft: {
    get(): Promise<string>
    set(text: string): Promise<void>
  }
  /** 只写剪贴板（需 ui:host）；不开放读取，保护用户复制内容 */
  clipboard: {
    writeText(text: string): Promise<void>
  }
  users: {
    /** 站内用户目录搜索（需 conv:read；结果不含 email，≤20 条） */
    search(kw: string): Promise<McUserLite[]>
    /** SDK v5：账号关系动作（需 ui:host） */
    blockStatus(userId: number): Promise<{ blocked: boolean }>
    block(userId: number): Promise<void>
    unblock(userId: number): Promise<void>
  }

  /**
   * SDK v5：宿主侧文件上传（需 file:upload）。
   * dataUrl 必须是 `data:<mime>;base64,...`；插件不构造 Blob，由宿主解码并校验 ≤1MB。
   */
  file: {
    upload(kind: McUploadResult['kind'], dataUrl: string, filename?: string): Promise<McUploadResult>
  }

  /** 宿主侧动作（需 ui:host） */
  nav: {
    openConversation(convId: number): Promise<void>
    jumpToMessage(messageId: number): Promise<boolean>
    /** SDK v3：打开本插件注册的全站页面（pluginId 为插件记录的平台 id） */
    openPluginPage(pluginId: number | string, pageId: string): Promise<void>
  }
  toast(text: string, kind?: 'info' | 'success' | 'warning' | 'error'): Promise<void>

  /** 每插件独立命名空间的键值存储（需 storage，值 ≤64KB） */
  storage: {
    get(key: string): Promise<string | null>
    set(key: string, value: string): Promise<void>
    remove(key: string): Promise<void>
    /** 列出本插件已存的键（剥离命名空间前缀，≤500 个） */
    keys(): Promise<string[]>
  }
  /**
   * SDK v3：服务端按「用户 + 插件 + key」隔离的 KV 数据（需 data）。
   * 插件 id 由宿主注入，插件只传 key；key 1–120 字符，value ≤64KB，不存在时 get 返回空串。
   */
  data: {
    get(key: string): Promise<string>
    set(key: string, value: string): Promise<void>
    remove(key: string): Promise<void>
    keys(): Promise<string[]>
  }
  /**
   * SDK v4：服务端统一 AI 代理（需 ai）。API Key 只存在服务端，插件不接触密钥。
   * messages 1–40 条，单条 ≤32KB，上下文 ≤96KB；当前为非流式响应。
   */
  ai: {
    status(): Promise<{ enabled: boolean; model: string }>
    chat(messages: McAIMessage[]): Promise<{ content: string }>
  }
  /** 经主站转发的网络请求（需 net，且域名必须在 manifest.allowedHosts 里） */
  request(url: string, opts?: McRequestOptions): Promise<McResponse>
  /** 改主题变量（需 ui:theme，仅 --td-* 令牌） */
  theme: { apply(vars: Record<string, string>): Promise<void> }
}
