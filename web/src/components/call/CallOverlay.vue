<script setup lang="ts">
/**
 * 一对一视频通话（WebRTC 信令 UI）。
 * 媒体端到端（RTCPeerConnection），服务端只中继 call.* 帧（后端需求规格 §3.3.1）。
 * 本组件自包含 Teleport，父组件只需把 ServerFrame 喂给 handleFrame()、
 * 把 startCall() 接到会话侧入口，并通过 send 事件上行。
 */
import { computed, onBeforeUnmount, ref } from 'vue'
import {
  CallIcon, CallOffIcon, MicrophoneIcon, SoundMuteIcon, CameraIcon, CameraOffIcon,
} from 'tdesign-icons-vue-next'
import UiButton from '@/components/ui/UiButton.vue'
import AvatarBadge from '@/components/common/AvatarBadge.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import type { ClientFrame, ServerFrame } from '@/types/proto'

const props = defineProps<{
  /** 上行出口：返回 false 表示 WS 未连接 */
  send: (frame: ClientFrame) => boolean
  /** 当前登录用户 id（用于判断来电是否自己发起） */
  selfId: number
}>()
const emit = defineEmits<{ error: [message: string] }>()
const { t } = useAppI18n()

type State = 'idle' | 'incoming' | 'connecting' | 'active' | 'ended'
type MediaType = 'audio' | 'video'

interface IceServer { urls: string; username?: string; credential?: string }

const state = ref<State>('idle')
const callId = ref('')
const peerId = ref(0)
const peerName = ref('')
const peerAvatar = ref('')
const mediaType = ref<MediaType>('video')
const isCaller = ref(false)
// 对端设备 sid 由服务端从 callId 推导，前端不再需要保存/发送 toSid（修复被叫无法回 answer）
const localStream = ref<MediaStream | null>(null)
const localVideo = ref<HTMLVideoElement | null>(null)
const remoteVideo = ref<HTMLVideoElement | null>(null)
// 语音通话用隐藏 audio 元素播放远端声音（video 元素在语音模式下不渲染）
const remoteAudio = ref<HTMLAudioElement | null>(null)
const micOn = ref(true)
const camOn = ref(true)
const elapsed = ref(0)
const iceServers = ref<IceServer[]>([])
let pc: RTCPeerConnection | null = null
let elapsedTimer: ReturnType<typeof setInterval> | null = null
let incomingTimer: ReturnType<typeof setTimeout> | null = null
let connectingTimer: ReturnType<typeof setTimeout> | null = null
const pendingIce: RTCIceCandidateInit[] = []

const statusText = computed(() => {
  if (state.value === 'incoming') {
    return mediaType.value === 'video' ? t('call.incomingVideo') : t('call.incomingAudio')
  }
  if (state.value === 'connecting') return t('call.connecting')
  if (state.value === 'active') return `${t('call.connected')} · ${formatDuration(elapsed.value)}`
  return ''
})

function formatDuration(sec: number) {
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

/** 返回是否发送成功；关键帧发送失败由调用方回滚状态（避免前后端状态分叉）。 */
function post(frame: ClientFrame): boolean {
  const ok = props.send(frame)
  if (!ok) emit('error', t('chat.reconnecting'))
  return ok
}

async function loadIceConfig() {
  if (iceServers.value.length) return
  try {
    const res = await fetch('/api/call/config', { headers: { Accept: 'application/json' } })
    if (res.ok) {
      const data = (await res.json()) as { iceServers?: IceServer[] }
      iceServers.value = data.iceServers ?? []
    }
  } catch {
    // 失败时退回空配置：STUN 仍可由浏览器默认行为处理，不阻断通话
  }
}

function createPC(): boolean {
  if (pc) return true
  if (typeof RTCPeerConnection === 'undefined') {
    emit('error', t('call.unsupported'))
    return false
  }
  pc = new RTCPeerConnection({ iceServers: iceServers.value })
  pc.onicecandidate = (e) => {
    if (e.candidate && state.value !== 'idle') {
      post({ type: 'call.ice', callId: callId.value, candidate: JSON.stringify(e.candidate) })
    }
  }
  pc.ontrack = (e) => {
    const stream = e.streams[0] ?? new MediaStream([e.track])
    if (e.track.kind === 'audio') {
      // 语音通话：远端音轨绑到隐藏 audio；视频通话时 video 元素自带声音，不重复绑定
      if (mediaType.value === 'audio' && remoteAudio.value) remoteAudio.value.srcObject = stream
      return
    }
    if (remoteVideo.value) remoteVideo.value.srcObject = stream
  }
  pc.onconnectionstatechange = () => {
    if (pc?.connectionState === 'failed' || pc?.connectionState === 'disconnected') {
      finish('error')
    } else if (pc?.connectionState === 'connected' && state.value === 'connecting') {
      if (connectingTimer) { clearTimeout(connectingTimer); connectingTimer = null }
      state.value = 'active'
      elapsed.value = 0
      elapsedTimer = setInterval(() => { elapsed.value += 1 }, 1000)
    }
  }
  return true
}

async function getMedia(): Promise<boolean> {
  try {
    localStream.value = await navigator.mediaDevices.getUserMedia({
      audio: true,
      video: mediaType.value === 'video',
    })
    if (localVideo.value) localVideo.value.srcObject = localStream.value
    for (const track of localStream.value.getTracks()) {
      pc?.addTrack(track, localStream.value)
    }
    return true
  } catch {
    emit('error', t('call.micDenied'))
    finish('error')
    return false
  }
}

async function flushPendingIce() {
  if (!pc || !pc.remoteDescription) return
  while (pendingIce.length) {
    try { await pc.addIceCandidate(pendingIce.shift()!) } catch { /* 过期 candidate 忽略 */ }
  }
}

function cleanup() {
  if (elapsedTimer) { clearInterval(elapsedTimer); elapsedTimer = null }
  if (incomingTimer) { clearTimeout(incomingTimer); incomingTimer = null }
  if (connectingTimer) { clearTimeout(connectingTimer); connectingTimer = null }
  localStream.value?.getTracks().forEach((track) => track.stop())
  localStream.value = null
  if (localVideo.value) localVideo.value.srcObject = null
  if (remoteVideo.value) remoteVideo.value.srcObject = null
  if (remoteAudio.value) remoteAudio.value.srcObject = null
  pc?.close()
  pc = null
  pendingIce.length = 0
  peerId.value = 0
  peerName.value = ''
  peerAvatar.value = ''
  isCaller.value = false
  micOn.value = true
  camOn.value = true
}

/** connecting 兜底：30s 未建立连接则自动结束（后端 accept 后会停 60s timer）。 */
function armConnectingTimer() {
  if (connectingTimer) clearTimeout(connectingTimer)
  connectingTimer = setTimeout(() => {
    if (state.value === 'connecting') finish('error')
  }, 30_000)
}

function finish(reason: string) {
  if (callId.value) post({ type: 'call.hangup', callId: callId.value })
  cleanup()
  state.value = 'ended'
  callId.value = ''
  // 让结束态短暂展示后回到 idle
  setTimeout(() => { if (state.value === 'ended') state.value = 'idle' }, 1800)
  if (reason && reason !== 'hangup') emit('error', t(`call.${reason}` as 'call.rejected'))
}

/** 主叫发起通话（由会话侧「视频通话」按钮调用）。 */
async function startCall(toUserId: number, type: MediaType = 'video', convId = 0) {
  if (state.value !== 'idle') return
  await loadIceConfig()
  callId.value = crypto.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  peerId.value = toUserId
  peerName.value = ''
  mediaType.value = type
  isCaller.value = true
  state.value = 'connecting'
  armConnectingTimer()
  const frame: ClientFrame = { type: 'call.invite', callId: callId.value, toUserId, mediaType: type }
  if (convId > 0) frame.convId = convId
  if (!post(frame)) { cleanup(); state.value = 'idle'; callId.value = '' }
}

/** 被叫接听。 */
async function accept() {
  if (state.value !== 'incoming') return
  await loadIceConfig()
  if (!createPC() || !(await getMedia())) return
  state.value = 'connecting'
  armConnectingTimer()
  if (!post({ type: 'call.accept', callId: callId.value })) {
    cleanup()
    state.value = 'idle'
    callId.value = ''
  }
}

function reject() {
  if (state.value !== 'incoming') return
  post({ type: 'call.reject', callId: callId.value })
  cleanup()
  state.value = 'idle'
  callId.value = ''
}

function toggleMic() {
  micOn.value = !micOn.value
  localStream.value?.getAudioTracks().forEach((track) => { track.enabled = micOn.value })
}

function toggleCam() {
  camOn.value = !camOn.value
  localStream.value?.getVideoTracks().forEach((track) => { track.enabled = camOn.value })
}

/** 接收服务端下发的 call.* 帧（由 ChatLayout 的 ws.onFrame 旁路驱动）。 */
async function handleFrame(frame: ServerFrame) {
  if (!('type' in frame) || !frame.type.startsWith('call.')) return
  switch (frame.type) {
    case 'call.incoming': {
      // 忽略自己发起的来电回环（selfId 仅用于此防御性校验）
      if (props.selfId && frame.fromUserId === props.selfId) break
      if (state.value !== 'idle') {
        post({ type: 'call.reject', callId: frame.callId })
        return
      }
      callId.value = frame.callId
      peerId.value = frame.fromUserId
      peerName.value = frame.nickname ?? ''
      peerAvatar.value = frame.avatar ?? ''
      mediaType.value = frame.mediaType
      isCaller.value = false
      state.value = 'incoming'
      incomingTimer = setTimeout(() => finish('timeout'), 60_000)
      break
    }
    case 'call.busy':
      if (frame.callId === callId.value) { cleanup(); state.value = 'idle'; emit('error', t('call.busy')) }
      break
    case 'call.rejected':
      if (frame.callId === callId.value) { cleanup(); state.value = 'idle'; emit('error', t('call.rejected')) }
      break
    case 'call.accepted': {
      if (frame.callId !== callId.value || !isCaller.value) break
      if (incomingTimer) { clearTimeout(incomingTimer); incomingTimer = null }
      if (!createPC() || !(await getMedia())) break
      state.value = 'connecting'
      armConnectingTimer()
      const offer = await pc!.createOffer()
      await pc!.setLocalDescription(offer)
      // 目标设备由服务端按 callId 推导，不再传 toSid
      if (!post({ type: 'call.offer', callId: callId.value, sdp: JSON.stringify(offer) })) finish('error')
      break
    }
    case 'call.offer': {
      // offer 只应发给被叫；主叫端忽略
      if (frame.callId !== callId.value || isCaller.value || state.value !== 'connecting') break
      if (!pc) break
      await pc.setRemoteDescription(JSON.parse(frame.sdp) as RTCSessionDescriptionInit)
      await flushPendingIce()
      const answer = await pc.createAnswer()
      await pc.setLocalDescription(answer)
      if (!post({ type: 'call.answer', callId: callId.value, sdp: JSON.stringify(answer) })) finish('error')
      break
    }
    case 'call.answer': {
      // answer 只应发给主叫
      if (frame.callId !== callId.value || !isCaller.value || !pc) break
      await pc.setRemoteDescription(JSON.parse(frame.sdp) as RTCSessionDescriptionInit)
      await flushPendingIce()
      break
    }
    case 'call.ice': {
      if (frame.callId !== callId.value || !pc) break
      const candidate = JSON.parse(frame.candidate) as RTCIceCandidateInit
      if (pc.remoteDescription) await pc.addIceCandidate(candidate)
      else pendingIce.push(candidate)
      break
    }
    case 'call.ended':
      if (frame.callId === callId.value) {
        cleanup()
        state.value = 'idle'
        callId.value = ''
        if (frame.reason && frame.reason !== 'hangup' && frame.reason !== 'disconnect') {
          emit('error', t(`call.${frame.reason}` as 'call.rejected'))
        }
      }
      break
  }
}

function onWsDisconnect() {
  if (state.value === 'idle') return
  cleanup()
  state.value = 'idle'
  callId.value = ''
}

onBeforeUnmount(cleanup)
defineExpose({ handleFrame, startCall, onWsDisconnect })
</script>

<template>
  <Teleport to="body">
    <!-- 来电弹窗 -->
    <div v-if="state === 'incoming'" class="call-overlay" role="dialog" aria-modal="true" :aria-label="t('call.incoming')">
      <div class="call-card mc-elev-3">
        <div class="call-card__icon">
          <CameraIcon v-if="mediaType === 'video'" size="32px" />
          <CallIcon v-else size="32px" />
        </div>
        <p class="call-card__label">{{ statusText }}</p>
        <h3 class="call-card__name">{{ peerName || `用户 ${peerId}` }}</h3>
        <div class="call-card__actions">
          <UiButton theme="danger" shape="circle" size="large" :aria-label="t('call.reject')" @click="reject">
            <template #icon><CallOffIcon size="20px" /></template>
          </UiButton>
          <UiButton theme="success" shape="circle" size="large" :aria-label="t('call.accept')" @click="accept">
            <template #icon><CameraIcon v-if="mediaType === 'video'" size="20px" /><CallIcon v-else size="20px" /></template>
          </UiButton>
        </div>
      </div>
    </div>

    <!-- 通话中悬浮窗 -->
    <div v-else-if="state === 'connecting' || state === 'active'" class="call-float mc-elev-3">
      <!-- 视频通话：远端 + 本地预览 -->
      <div v-if="mediaType === 'video'" class="call-float__videos">
        <video ref="remoteVideo" class="call-float__remote" autoplay playsinline />
        <video ref="localVideo" class="call-float__local" autoplay playsinline muted />
      </div>
      <!-- 语音通话：无视频画面，展示对方头像 + 状态 -->
      <div v-else class="call-float__voice">
        <AvatarBadge :name="peerName || `用户 ${peerId}`" :src="peerAvatar" :color="'var(--td-brand-color)'" :size="64" />
        <span class="call-float__voice-label">{{ t('call.incomingAudio') }}</span>
      </div>
      <!-- 语音模式下播放远端声音 -->
      <audio v-if="mediaType === 'audio'" ref="remoteAudio" autoplay />
      <div class="call-float__bar">
        <span class="call-float__status">{{ statusText }}</span>
        <UiButton variant="text" shape="circle" size="small" :aria-label="micOn ? t('call.micOff') : t('call.micOn')" @click="toggleMic">
          <template #icon><MicrophoneIcon v-if="micOn" size="18px" /><SoundMuteIcon v-else size="18px" /></template>
        </UiButton>
        <UiButton v-if="mediaType === 'video'" variant="text" shape="circle" size="small" :aria-label="camOn ? t('call.camOff') : t('call.camOn')" @click="toggleCam">
          <template #icon><CameraIcon v-if="camOn" size="18px" /><CameraOffIcon v-else size="18px" /></template>
        </UiButton>
        <UiButton theme="danger" shape="circle" size="small" :aria-label="t('call.hangup')" @click="finish('hangup')">
          <template #icon><CallOffIcon size="18px" /></template>
        </UiButton>
      </div>
    </div>

    <!-- 结束提示 -->
    <div v-else-if="state === 'ended'" class="call-toast" role="status">{{ t('call.ended') }}</div>
  </Teleport>
</template>

<style scoped>
.call-overlay {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.42);
  backdrop-filter: var(--mc-blur);
  animation: mc-fade-in var(--mc-dur-base) var(--mc-ease-out);
}
.call-card {
  width: min(360px, calc(100vw - 32px));
  padding: 32px 24px 24px;
  border-radius: var(--mc-radius-xl);
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  text-align: center;
}
.call-card__icon {
  width: 64px;
  height: 64px;
  margin: 0 auto 12px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: var(--td-brand-color);
  background: var(--td-brand-color-1, rgba(0, 82, 217, 0.1));
  animation: mc-pop-in var(--mc-dur-slow) var(--mc-ease-spring);
}
.call-card__label { margin: 0; font-size: var(--mc-fs-sm); color: var(--td-text-color-secondary); }
.call-card__name { margin: 6px 0 24px; font-size: var(--mc-fs-title); color: var(--td-text-color-primary); }
.call-card__actions { display: flex; justify-content: center; gap: 24px; }
.call-float {
  position: fixed;
  right: 24px;
  bottom: 24px;
  z-index: 95;
  width: 320px;
  border-radius: var(--mc-radius-lg);
  overflow: hidden;
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  animation: mc-fade-up var(--mc-dur-base) var(--mc-ease-out);
}
.call-float__videos { position: relative; width: 100%; aspect-ratio: 4 / 3; background: #101216; }
.call-float__voice {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 26px 16px;
  background: var(--td-bg-color-container);
}
.call-float__voice-label {
  font-size: var(--mc-fs-sm);
  color: var(--td-text-color-secondary);
  font-variant-numeric: tabular-nums;
}
.call-float__remote { width: 100%; height: 100%; object-fit: cover; }
.call-float__local {
  position: absolute;
  right: 10px;
  bottom: 10px;
  width: 84px;
  height: 62px;
  border-radius: var(--mc-radius-sm);
  object-fit: cover;
  border: 1px solid var(--mc-hairline-strong);
}
.call-float__bar { display: flex; align-items: center; gap: 8px; padding: 8px 12px; }
.call-float__status { flex: 1; font-size: var(--mc-fs-sm); color: var(--td-text-color-secondary); font-variant-numeric: tabular-nums; }
.call-toast {
  position: fixed;
  left: 50%;
  bottom: 32px;
  z-index: 95;
  transform: translateX(-50%);
  padding: 10px 18px;
  border-radius: var(--mc-radius-pill);
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  color: var(--td-text-color-primary);
  font-size: var(--mc-fs-body);
  animation: mc-fade-up var(--mc-dur-base) var(--mc-ease-out);
}
</style>
