import { describe, expect, it } from 'vitest'
import type { ServerFrame } from '@/types/proto'
import {
  endedPresentation, formatDuration, isCallFrame, normalizeMediaType, safeParseJson, shouldHandleFrame,
  type FrameFilterContext,
} from './logic'

const ctx = (over: Partial<FrameFilterContext> = {}): FrameFilterContext => ({
  callId: 'c1', state: 'connecting', isCaller: false, selfId: 7, hasPeerConnection: true,
  ...over,
})

const frame = (type: string, extra: Record<string, unknown> = {}) =>
  ({ type, ...extra }) as unknown as ServerFrame

describe('isCallFrame', () => {
  it('只识别 call.* 帧', () => {
    expect(isCallFrame(frame('call.incoming'))).toBe(true)
    expect(isCallFrame(frame('message'))).toBe(false)
  })
})

describe('shouldHandleFrame', () => {
  it('来电：空闲且非自己发起才处理', () => {
    expect(shouldHandleFrame(frame('call.incoming', { fromUserId: 9 }), ctx({ state: 'idle', callId: '' }))).toBe(true)
    expect(shouldHandleFrame(frame('call.incoming', { fromUserId: 7 }), ctx({ state: 'idle', callId: '' }))).toBe(false)
    // 占线时仍返回 true：由 handleFrame 回绝来电（原行为）
    expect(shouldHandleFrame(frame('call.incoming', { fromUserId: 9 }), ctx({ state: 'active' }))).toBe(true)
  })

  it('非来电帧必须匹配当前 callId', () => {
    expect(shouldHandleFrame(frame('call.ended', { callId: 'c1' }), ctx())).toBe(true)
    expect(shouldHandleFrame(frame('call.ended', { callId: 'other' }), ctx())).toBe(false)
    expect(shouldHandleFrame(frame('call.ended', { callId: 'c1' }), ctx({ callId: '' }))).toBe(false)
  })

  it('accepted 只发主叫；offer 只发被叫且须 connecting 且已有 PC', () => {
    expect(shouldHandleFrame(frame('call.accepted', { callId: 'c1' }), ctx({ isCaller: true }))).toBe(true)
    expect(shouldHandleFrame(frame('call.accepted', { callId: 'c1' }), ctx({ isCaller: false }))).toBe(false)
    expect(shouldHandleFrame(frame('call.offer', { callId: 'c1' }), ctx())).toBe(true)
    expect(shouldHandleFrame(frame('call.offer', { callId: 'c1' }), ctx({ isCaller: true }))).toBe(false)
    expect(shouldHandleFrame(frame('call.offer', { callId: 'c1' }), ctx({ state: 'idle' }))).toBe(false)
    expect(shouldHandleFrame(frame('call.offer', { callId: 'c1' }), ctx({ hasPeerConnection: false }))).toBe(false)
  })

  it('answer 只发主叫；ice 需要 PC', () => {
    expect(shouldHandleFrame(frame('call.answer', { callId: 'c1' }), ctx({ isCaller: true }))).toBe(true)
    expect(shouldHandleFrame(frame('call.answer', { callId: 'c1' }), ctx())).toBe(false)
    expect(shouldHandleFrame(frame('call.ice', { callId: 'c1' }), ctx({ hasPeerConnection: false }))).toBe(false)
  })
})

describe('formatDuration / normalizeMediaType', () => {
  it('mm:ss 格式化', () => {
    expect(formatDuration(0)).toBe('00:00')
    expect(formatDuration(65)).toBe('01:05')
    expect(formatDuration(600)).toBe('10:00')
  })
  it('未知媒体类型归一为 audio', () => {
    expect(normalizeMediaType('video')).toBe('video')
    expect(normalizeMediaType('audio')).toBe('audio')
    expect(normalizeMediaType('evil')).toBe('audio')
    expect(normalizeMediaType(undefined)).toBe('audio')
  })
})

describe('safeParseJson', () => {
  it('合法 JSON 原样解析；坏包与空值返回 null（不抛异常）', () => {
    expect(safeParseJson<{ type: string }>('{"type":"offer"}')).toEqual({ type: 'offer' })
    expect(safeParseJson('not-json{')).toBe(null)
    expect(safeParseJson(undefined)).toBe(null)
    expect(safeParseJson('')).toBe(null)
  })
})

describe('endedPresentation', () => {
  it('挂断/断线：只展示结束面板，不弹错误提示（与主动挂断一致）', () => {
    expect(endedPresentation('hangup')).toEqual({ panel: true, toast: null })
    expect(endedPresentation('disconnect')).toEqual({ panel: true, toast: null })
  })
  it('accepted-elsewhere：只给专属提示，不进结束面板', () => {
    expect(endedPresentation('accepted-elsewhere')).toEqual({ panel: false, toast: 'call.accepted-elsewhere' })
  })
  it('其它原因：面板 + 具体提示；无 reason 时只给面板', () => {
    expect(endedPresentation('timeout')).toEqual({ panel: true, toast: 'call.timeout' })
    expect(endedPresentation('error')).toEqual({ panel: true, toast: 'call.error' })
    expect(endedPresentation(undefined)).toEqual({ panel: true, toast: null })
  })
})
