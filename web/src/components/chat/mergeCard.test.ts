import { describe, expect, it } from 'vitest'

import type { Message } from '@/types/proto'
import { buildMergeCard, CHATCARD_TYPE, MERGE_MAX_ITEMS, parseMergeCard } from './mergeCard'

function msg(id: number, content: string, extra: Partial<Message> = {}): Message {
  return {
    id,
    conversationId: 1,
    senderId: 7,
    clientMsgId: `c${id}`,
    msgType: 'text',
    content,
    isRecalled: false,
    createdAt: '2026-11-23T10:00:00.000Z',
    ...extra,
  }
}

const opts = {
  title: '公共聊天室',
  senderName: (m: Message) => (m.senderId === 7 ? '我' : `用户${m.senderId}`),
  labels: {
    image: '[图片]',
    audio: '[语音]',
    sticker: '[表情]',
    video: '[视频]',
    card: '[聊天记录]',
    unknown: '[{type} 消息]',
  },
}

describe('buildMergeCard', () => {
  it('按选中顺序构造快照，昵称/时间/类型/正文一一对应', () => {
    const card = buildMergeCard([msg(1, '你好'), msg(2, '收到', { senderId: 8 })], opts)
    expect(card).not.toBeNull()
    expect(card!.v).toBe(1)
    expect(card!.title).toBe('公共聊天室')
    expect(card!.items).toEqual([
      { n: '我', t: '2026-11-23T10:00:00.000Z', m: 'text', c: '你好' },
      { n: '用户8', t: '2026-11-23T10:00:00.000Z', m: 'text', c: '收到' },
    ])
  })

  it('过滤已撤回与未落库的本地消息', () => {
    const card = buildMergeCard(
      [msg(1, '好的'), msg(2, '撤回的', { isRecalled: true }), msg(-3, 'pending')],
      opts,
    )
    expect(card!.items).toHaveLength(1)
    expect(card!.items[0]!.c).toBe('好的')
  })

  it('媒体保留 URL 供详情查看，未知类型给占位，嵌套卡片不展开', () => {
    const card = buildMergeCard(
      [
        msg(1, '/uploads/a.webp', { msgType: 'image' }),
        msg(2, 'xyz', { msgType: 'weather' }),
        msg(3, '{"v":1,"items":[]}', { msgType: CHATCARD_TYPE }),
      ],
      opts,
    )
    expect(card!.items.map((i) => i.c)).toEqual(['/uploads/a.webp', '[weather 消息]', '[聊天记录]'])
  })

  it('最多 20 条，多余的直接丢弃', () => {
    const many = Array.from({ length: 25 }, (_, i) => msg(i + 1, `m${i}`))
    const card = buildMergeCard(many, opts)
    expect(card!.items).toHaveLength(MERGE_MAX_ITEMS)
    expect(card!.items[19]!.c).toBe('m19')
  })

  it('超长文本按预算截断而不是超帧', () => {
    const long = '长'.repeat(20000)
    const card = buildMergeCard([msg(1, long)], opts)
    expect(card).not.toBeNull()
    expect(card!.items[0]!.c.length).toBeLessThanOrEqual(400)
    expect(JSON.stringify(card).length).toBeLessThan(12000)
  })

  it('没有可用消息返回 null', () => {
    expect(buildMergeCard([], opts)).toBeNull()
    expect(buildMergeCard([msg(1, '', { isRecalled: true })], opts)).toBeNull()
  })
})

describe('parseMergeCard', () => {
  it('正常往返', () => {
    const card = buildMergeCard([msg(1, '往返测试')], opts)!
    expect(parseMergeCard(JSON.stringify(card))).toEqual(card)
  })

  it('畸形 JSON / 错误版本 / 缺字段一律返回 null 或收敛为安全值', () => {
    expect(parseMergeCard('not json')).toBeNull()
    expect(parseMergeCard('{}')).toBeNull()
    expect(parseMergeCard('{"v":2,"items":[]}')).toBeNull()
    expect(parseMergeCard('{"v":1,"items":[{"n":"x"}]}')!.items).toHaveLength(0)
  })

  it('限制条目数、昵称长度与正文长度，防止恶意 payload 撑爆渲染', () => {
    const raw = JSON.stringify({
      v: 1,
      title: 't'.repeat(500),
      items: Array.from({ length: 100 }, () => ({
        n: 'n'.repeat(100),
        t: '2026-11-23T10:00:00.000Z',
        m: 'text',
        c: 'c'.repeat(2000),
      })),
    })
    const card = parseMergeCard(raw)!
    expect(card.items).toHaveLength(MERGE_MAX_ITEMS)
    expect(card.title).toHaveLength(80)
    expect(card.items[0]!.n).toHaveLength(24)
    expect(card.items[0]!.c).toHaveLength(1000)
    expect(card.items[99]).toBeUndefined()
  })
})
