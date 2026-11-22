/**
 * 合并转发卡片：payload 构造与解析（纯函数，便于回归测试）。
 *
 * 设计取舍：卡片是一条**独立消息**，快照只存精简字段（昵称/时间/类型/正文摘要），
 * 不引用原消息 id——原消息被撤回或删除后卡片仍然可读，与单条转发语义一致。
 * 数据放在既有 content 字段（JSON）+ msgType='chatcard'，不加数据库列、不改帧契约。
 */
import type { Message } from '@/types/proto'

export const CHATCARD_TYPE = 'chatcard'
/** 与 MessagePane 多选上限一致 */
export const MERGE_MAX_ITEMS = 20
/** 卡片正文预算：与后端 ws.maxCardContentChars 对齐（12000），留余量给 JSON 转义 */
const CONTENT_BUDGET_CHARS = 11000
const CONTENT_BUDGET_BYTES = 48000

export interface MergeCardItem {
  /** 原发送者昵称（已截断） */
  n: string
  /** 原消息时间（ISO8601） */
  t: string
  /** 原消息类型 */
  m: string
  /** 正文摘要或媒体占位文案 */
  c: string
}

export interface MergeCardPayload {
  /** payload 版本，后续结构演进用 */
  v: 1
  title: string
  items: MergeCardItem[]
}

export interface BuildMergeCardOptions {
  /** 卡片标题（如「群聊的聊天记录」） */
  title: string
  senderName(m: Message): string
  /** 各类型的占位文案，键为 msgType，另含 unknown 兜底 */
  labels: Record<string, string>
}

/** 保留媒体 URL（详情里要看图/听语音）；嵌套卡片不保留，避免无限展开 */
function itemBody(m: Message, labels: BuildMergeCardOptions['labels']): string {
  if (m.msgType === CHATCARD_TYPE) return labels.card ?? ''
  if (m.msgType === 'text' || m.msgType === 'image' || m.msgType === 'audio' ||
      m.msgType === 'sticker' || m.msgType === 'video') {
    return m.content
  }
  return (labels.unknown ?? '').replace('{type}', m.msgType)
}

function truncate(s: string, max: number): string {
  return [...s].slice(0, max).join('')
}

function withinBudget(json: string): boolean {
  return (
    [...json].length <= CONTENT_BUDGET_CHARS &&
    new TextEncoder().encode(json).length <= CONTENT_BUDGET_BYTES
  )
}

/**
 * 用选中的消息构造卡片 payload。
 *
 * 原消息可能已被撤回（不能转发正文）、可能还在本地 pending（id<=0，未落库），
 * 两类都在这里过滤。文本按预算动态截断：20 条长文本也要塞进 64KB 帧限制内。
 * 返回 null 表示没有可转发的消息或即使截断到最短仍超限（调用方提示减少选中）。
 */
export function buildMergeCard(
  msgs: Message[],
  opts: BuildMergeCardOptions,
): MergeCardPayload | null {
  const usable = msgs.filter((m) => !m.isRecalled && m.id > 0).slice(0, MERGE_MAX_ITEMS)
  if (!usable.length) return null
  const base = usable.map((m) => ({
    n: truncate(opts.senderName(m), 24),
    t: m.createdAt,
    m: m.msgType,
    full: itemBody(m, opts.labels),
    // 媒体 c 是资源地址，截短就打不开了：豁免逐档收紧，只受 1000 字硬上限
    media: m.msgType === 'image' || m.msgType === 'audio' || m.msgType === 'sticker' || m.msgType === 'video',
  }))
  // 逐档收紧每条正文长度，直到整份 JSON 落在预算内；
  // 下限 40 字——再短就没有阅读价值，此时宁可让用户少选几条
  for (let limit = 400; limit >= 40; limit -= 40) {
    const items: MergeCardItem[] = base.map((b) => ({
      n: b.n,
      t: b.t,
      m: b.m,
      c: truncate(b.full, b.media ? 1000 : limit),
    }))
    const payload: MergeCardPayload = { v: 1, title: opts.title, items }
    if (withinBudget(JSON.stringify(payload))) return payload
  }
  return null
}

/** 把 content 里的 JSON 安全解析为卡片；任何畸形数据都返回 null（渲染层给出友好占位） */
export function parseMergeCard(content: string): MergeCardPayload | null {
  try {
    const raw: unknown = JSON.parse(content)
    if (!raw || typeof raw !== 'object') return null
    const obj = raw as Record<string, unknown>
    if (obj.v !== 1 || !Array.isArray(obj.items)) return null
    const items: MergeCardItem[] = []
    for (const it of obj.items.slice(0, MERGE_MAX_ITEMS)) {
      if (!it || typeof it !== 'object') continue
      const row = it as Record<string, unknown>
      if (typeof row.n !== 'string' || typeof row.t !== 'string' || typeof row.m !== 'string') continue
      items.push({
        n: truncate(row.n, 24),
        t: row.t,
        m: truncate(row.m, 32),
        c: truncate(typeof row.c === 'string' ? row.c : '', 1000),
      })
    }
    return { v: 1, title: truncate(typeof obj.title === 'string' ? obj.title : '', 80), items }
  } catch {
    return null
  }
}
