/**
 * MessageRow 的宿主上下文（MessagePane provide，MessageRow inject）。
 *
 * 行组件只接「稳定的函数引用 + 少量标量 props」：函数内部读取宿主的
 * 响应式状态（成员昵称缓存、已读回执等），依赖跟踪发生在行组件自己的
 * 渲染作用域里——宿主任意状态变化只重渲真正读到它的行，其余行因 props
 * 未变被 Vue 跳过，这正是抽行组件要的逐行更新语义。
 */
import type { InjectionKey } from 'vue'

import type { Message } from '@/types/proto'

export interface MessageRowCtx {
  /** 头像下的名字：自己用昵称，单聊用对方昵称，群聊查成员表 */
  senderName(m: Message): string
  senderAvatar(m: Message): string
  senderAvatarColor(m: Message): string
  /** 群成员昵称（未加载时回退「用户N」） */
  memberName(userId: number): string
  /** 单聊已读状态文案（发送中/已发送/已送达/已读） */
  messageStatus(m: Message): string
  /** 撤回自己的消息 */
  recall(m: Message): void
  /** 表情回应（含收起选择器） */
  react(m: Message, emoji: string): void
  toggleReactionPicker(messageId: number): void
  /** 引用块点击跳转并高亮 */
  jumpToMessage(id: number): void
  /** 图片 / 表情点击放大（多选模式下 = 选中） */
  onMediaClick(m: Message, e?: MouseEvent): void
  /** 消息右键菜单 */
  openMenu(e: MouseEvent, m: Message): void
  /** 图片 / 表情右键菜单 */
  openImageMenu(e: MouseEvent, m: Message): void
  /** 多选模式下点击消息行 = 勾选/取消（无副作用时内部自行忽略） */
  toggleSelect(m: Message): void
}

export const messageRowKey: InjectionKey<MessageRowCtx> = Symbol('messageRowCtx')
