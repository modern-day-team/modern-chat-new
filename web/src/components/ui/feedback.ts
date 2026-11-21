/**
 * 自研全局轻提示（toast）与确认弹窗——替代 TDesign 的 MessagePlugin / DialogPlugin。
 *
 * 动机：TDesign 的 Dialog/Message 是路由共享块里最大的组成部分（每个异步路由
 * 都要背一份），而它们的 API 只是「队列里塞一条提示 / 弹一个确认框」，自实现
 * 几十行就能替掉。API 形状与 TDesign 对齐（confirm 返回 { destroy }、按钮支持
 * 字符串或 { content, theme }、onConfirm 里由调用方自行 destroy），存量调用点
 * 只需换导入路径。挂载点：App.vue 里的 <UiMessageHost /> 与 <UiDialogHost />。
 */
import { reactive } from 'vue'

// ===== 轻提示 =====

export type MessageKind = 'info' | 'success' | 'warning' | 'error'

export interface McMessage {
  id: number
  kind: MessageKind
  text: string
}

/** UiMessageHost 渲染源（reactive 数组，host 组件负责 DOM） */
export const mcMessages = reactive<McMessage[]>([])

let msgSeq = 0
const MESSAGE_TTL = 3200
const MESSAGE_MAX = 6

function pushMessage(kind: MessageKind, raw: string | { content: string; duration?: number }) {
  const text = typeof raw === 'string' ? raw : raw.content
  if (!text) return
  const item: McMessage = { id: ++msgSeq, kind, text: String(text).slice(0, 200) }
  mcMessages.push(item)
  // 上限保护：极端连点不让 DOM 无限堆积
  if (mcMessages.length > MESSAGE_MAX) mcMessages.splice(0, mcMessages.length - MESSAGE_MAX)
  const ttl = typeof raw === 'object' && raw.duration && raw.duration > 0 ? raw.duration : MESSAGE_TTL
  window.setTimeout(() => {
    const i = mcMessages.indexOf(item)
    if (i >= 0) mcMessages.splice(i, 1)
  }, ttl)
}

type MessageInput = string | { content: string; duration?: number }

export const MessagePlugin = {
  info: (text: MessageInput) => pushMessage('info', text),
  success: (text: MessageInput) => pushMessage('success', text),
  warning: (text: MessageInput) => pushMessage('warning', text),
  error: (text: MessageInput) => pushMessage('error', text),
}

// ===== 确认弹窗 =====

export interface DialogButtonSpec {
  content: string
  theme?: 'default' | 'primary' | 'danger'
}

export interface DialogOptions {
  header: string
  body?: string
  /** danger / warning：确认按钮标红（危险操作确认，UI 层不区分深浅） */
  theme?: 'default' | 'danger' | 'warning'
  confirmBtn?: string | DialogButtonSpec
  cancelBtn?: string | DialogButtonSpec
  onConfirm?: () => void | Promise<void>
  /** 点击取消按钮（TDesign 语义：cancel 后还会触发 onClose） */
  onCancel?: () => void | Promise<void>
  /** 取消按钮 / ESC / 遮罩关闭都会触发；confirm + destroy 不触发 */
  onClose?: () => void | Promise<void>
}

export interface DialogHandle {
  destroy(): void
  /** destroy 的别名（TDesign 返回值同时带 hide/destroy） */
  hide(): void
}

/** UiDialogHost 渲染源（reactive，host 组件负责 DOM 与键盘焦点） */
export const mcDialog = reactive({
  visible: false,
  header: '',
  body: '',
  danger: false,
  confirmText: '',
  cancelText: '',
})

interface ActiveDialog {
  options: DialogOptions
  confirmed: boolean
}

let active: ActiveDialog | null = null
const queue: DialogOptions[] = []

function btnText(b: string | DialogButtonSpec | undefined): string {
  if (!b) return ''
  return typeof b === 'string' ? b : b.content
}

function btnTheme(b: string | DialogButtonSpec | undefined): string | undefined {
  return typeof b === 'object' ? b.theme : undefined
}

function showNext() {
  const next = queue.shift()
  if (!next) {
    active = null
    mcDialog.visible = false
    return
  }
  active = { options: next, confirmed: false }
  mcDialog.header = next.header
  mcDialog.body = next.body ?? ''
  mcDialog.danger =
    next.theme === 'danger' || next.theme === 'warning' || btnTheme(next.confirmBtn) === 'danger'
  mcDialog.confirmText = btnText(next.confirmBtn)
  mcDialog.cancelText = btnText(next.cancelBtn)
  mcDialog.visible = true
}

function open(options: DialogOptions, withCancel: boolean) {
  queue.push(withCancel ? options : { ...options, cancelBtn: undefined, onCancel: undefined })
  // 同一时刻只显示一个弹窗：后到的排队（TDesign 是叠层，这里串行更可控）
  if (!mcDialog.visible || !active) showNext()
}

function closeActive() {
  active = null
  mcDialog.visible = false
}

export const DialogPlugin = {
  confirm(options: DialogOptions): DialogHandle {
    open(options, true)
    return { destroy, hide: destroy }
  },
  /** 仅一个确认按钮（如「重置后的新密码」展示框） */
  alert(options: DialogOptions): DialogHandle {
    open(options, false)
    return { destroy, hide: destroy }
  },
}

/** 确认按钮：调用点在 onConfirm 里自行 destroy()（与 TDesign 一致，异步确认中不闪退） */
function onConfirmClick() {
  const a = active
  if (!a) return
  a.confirmed = true
  void a.options.onConfirm?.()
}

/** 取消按钮：先关再回调（onCancel → onClose，TDesign 语义） */
function onCancelClick() {
  const a = active
  if (!a) return
  const { options } = a
  closeActive()
  void options.onCancel?.()
  void options.onClose?.()
  showNext()
}

/** ESC / 遮罩关闭：只触发 onClose */
function onDismiss() {
  const a = active
  if (!a) return
  const { options } = a
  closeActive()
  void options.onClose?.()
  showNext()
}

/** 程序化关闭（调用点持有 handle）：不触发任何回调 */
function destroy() {
  if (!active) return
  closeActive()
  showNext()
}

// 供 UiDialogHost 内部事件绑定（不进公开 API 面）
export const dialogInternals = { onConfirmClick, onCancelClick, onDismiss }
