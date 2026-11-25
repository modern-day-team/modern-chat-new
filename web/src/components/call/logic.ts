/**
 * 通话 UI 的纯逻辑：类型、帧过滤规则、时间格式化。
 * 不依赖 Vue / DOM，便于单测；CallOverlay 只负责把状态映射到这里。
 */
import type { ServerFrame } from '@/types/proto'

export type CallState = 'idle' | 'incoming' | 'connecting' | 'active' | 'ended'
export type MediaType = 'audio' | 'video'

export interface IceServer {
  urls: string
  username?: string
  credential?: string
}

/** 帧过滤所需的本地上下文。 */
export interface FrameFilterContext {
  callId: string
  state: CallState
  isCaller: boolean
  selfId: number
  hasPeerConnection: boolean
}

/** 是否是通话信令帧（其它帧一律忽略）。 */
export function isCallFrame(frame: ServerFrame): boolean {
  return 'type' in frame && frame.type.startsWith('call.')
}

/**
 * 判断一帧 call.* 是否应被当前通话处理。
 * 集中所有「callId + 角色 + 状态」规则，避免各分支漏判导致串话。
 */
export function shouldHandleFrame(frame: ServerFrame, ctx: FrameFilterContext): boolean {
  if (!isCallFrame(frame)) return false
  // 来电不需要当前 callId（空闲时才能收到）；其余帧必须属于当前通话
  if (frame.type !== 'call.incoming') {
    if (!ctx.callId || !('callId' in frame) || frame.callId !== ctx.callId) return false
  }
  switch (frame.type) {
    case 'call.incoming':
      // 只过滤自己的回环帧；空闲/占线状态由 handleFrame 处理（占线时要回绝来电）
      return !ctx.selfId || frame.fromUserId !== ctx.selfId
    case 'call.busy':
    case 'call.rejected':
    case 'call.ended':
      return true
    case 'call.accepted':
      // accepted 只发给主叫
      return ctx.isCaller
    case 'call.offer':
      // offer 只发给被叫，且必须在 connecting（已接听）
      return !ctx.isCaller && ctx.state === 'connecting' && ctx.hasPeerConnection
    case 'call.answer':
      // answer 只发给主叫
      return ctx.isCaller && ctx.hasPeerConnection
    case 'call.ice':
      return ctx.hasPeerConnection
    default:
      return false
  }
}

/** 秒 → mm:ss（通话时长）。 */
export function formatDuration(sec: number): string {
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

/** 未知 mediaType 归一为 audio（与后端白名单一致）。 */
export function normalizeMediaType(value: string | undefined): MediaType {
  return value === 'video' ? 'video' : 'audio'
}
