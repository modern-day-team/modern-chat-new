<script setup lang="ts">
/**
 * 单条消息行（从 MessagePane 抽出）。
 *
 * 抽取目的：宿主面板的响应式依赖极多（flash / 菜单 / 多选 / 贴底状态 /
 * 表情选择器……），原先任意一项变化都会重跑整个大模板、重建全部可见气泡。
 * 现在每行是独立组件——props 只传消息对象引用与少量标量，消息对象在
 * store 里原地修改时本行的渲染作用域照样跟踪；其余行因 props 未变被
 * Vue 跳过，消息突发的渲染成本从「整面板重渲」降为「仅受影响的行」。
 * 宿主的成员昵称缓存 / 已读回执等经 inject 的稳定函数读取（见
 * messageRowContext.ts，依赖跟踪发生在本组件作用域）。
 */
import { computed, inject } from 'vue'

import AvatarBadge from './AvatarBadge.vue'
import VoiceMessage from './VoiceMessage.vue'
import MergeForwardCard from './MergeForwardCard.vue'
import DeclarativeNodes from '@/components/declarative'
import { FEATURES } from '@/config/features'
import { useAppI18n } from '@/composables/useAppI18n'
import { useAuthStore } from '@/stores/auth'
import { useSiteStore } from '@/stores/site'
import { useConversationsStore } from '@/stores/conversations'
import type { Message, ReplyBrief } from '@/types/proto'
import { isRenderedOk, renderedNodes } from './pluginRenders'
import { messageRowKey } from './messageRowContext'

const props = defineProps<{
  msg: Message
  /** jumpToMessage 定位高亮 */
  flash: boolean
  /** 多选模式选中态（仅 selectMode 下有意义） */
  selected: boolean
  selectMode: boolean
  /** 表情选择器是否在本行打开 */
  pickerOpen: boolean
  /** 1=单聊（发送状态文案），2=群聊（头像下显示成员名） */
  convType: number
  /** 对方已读游标（单聊已读状态） */
  peerReadUpTo: number
}>()

const ctx = inject(messageRowKey)
if (!ctx) throw new Error('MessageRow 必须在 MessagePane 内使用（缺 messageRowCtx）')

const convStore = useConversationsStore()
const auth = useAuthStore()
const siteStore = useSiteStore()
const { t } = useAppI18n()

const mine = computed(() => props.msg.senderId === auth.user?.id)

/** 撤回时限（本地提示用，服务端仍会再校验一次） */
function canRecall(m: Message): boolean {
  if (m.isRecalled || m.senderId !== auth.user?.id || m.id <= 0) return false
  const minutes = siteStore.config.recallMinutes
  if (minutes <= 0) return false
  return Date.now() - new Date(m.createdAt).getTime() < minutes * 60 * 1000
}

/** 引用块里显示的文本：媒体与已撤回要给占位，不能直接甩 URL */
function quoteText(r: ReplyBrief): string {
  if (r.isRecalled) return t('message.quoteRecalled')
  if (r.msgType === 'image') return t('message.quoteImage')
  if (r.msgType === 'audio') return t('message.quoteAudio')
  if (r.msgType === 'sticker') return t('message.quoteSticker')
  if (r.msgType === 'video') return t('message.quoteVideo')
  if (r.msgType === 'chatcard') return t('message.quoteCard')
  return r.content
}

/** 撤回提示文案：自己显示「你」，别人显示其昵称 */
function recallLabel(m: Message): string {
  return mine.value ? t('message.recalledByYou') : t('message.recalledBy', { name: ctx!.senderName(m) })
}

// ===== @ 高亮 =====

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/**
 * 把正文按「@昵称」切段，高亮其中的 @ 部分。
 * 只在消息确实带 mentions 时才做，普通文本走原样渲染，不额外开销。
 */
function textParts(m: Message): { t: string; at: boolean }[] {
  if (m.msgType !== 'text' || !m.mentions?.length) return [{ t: m.content, at: false }]
  const names = m.mentions.map((id) => ctx!.memberName(id)).filter(Boolean)
  if (!names.length) return [{ t: m.content, at: false }]
  const re = new RegExp(`(@(?:${names.map(escapeRe).join('|')}))`, 'g')
  return m.content
    .split(re)
    .filter((s) => s !== '')
    .map((s) => ({ t: s, at: names.some((n) => s === `@${n}`) }))
}

function fmtTime(ts: string): string {
  const d = new Date(ts)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

/** 失败消息重发：复用 store 的 resend（重置 pending 并重新发帧） */
function retrySend(m: Message) {
  if (!m.failed) return
  convStore.resend(m)
}

const REACTION_EMOJIS = ['👍', '❤️', '😂', '😮', '😢', '🎉']
</script>

<template>
  <div
    class="mc-msg"
    :class="{
      mine,
      'is-recalled': msg.isRecalled,
      'is-flash': flash,
      'is-selected': selectMode && selected,
      'is-selecting': selectMode,
    }"
    @click="selectMode && ctx!.toggleSelect(msg)"
    @contextmenu.prevent="ctx!.openMenu($event, msg)"
  >
    <!-- 多选标记 -->
    <span
      v-if="selectMode && !msg.isRecalled"
      class="mc-msg__check"
      :class="{ 'is-on': selected }"
      aria-hidden="true"
    >✓</span>
    <!-- 头像：自己的消息也显示（.mine 的 row-reverse 会把它排到最右）。
         撤回提示是系统文案，居中显示，不带头像。 -->
    <AvatarBadge
      v-if="!msg.isRecalled"
      :name="ctx!.senderName(msg)"
      :src="ctx!.senderAvatar(msg)"
      :color="ctx!.senderAvatarColor(msg)"
      :size="32"
    />
    <div class="mc-msg__body">
      <div
        v-if="convType === 2 && !mine && !msg.isRecalled"
        class="mc-msg__sender text-placeholder"
      >
        {{ ctx!.memberName(msg.senderId) }}
      </div>

      <!-- 转发标注：转发不保留原作者，只标来源性质 -->
      <div v-if="msg.forwarded && !msg.isRecalled" class="mc-msg__forward">
        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <path
            d="M4 8h11a5 5 0 0 1 0 10H8M4 8l4-4M4 8l4 4"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
        {{ t('message.forwarded') }}
      </div>

      <!-- 引用：点一下跳到原消息 -->
      <div
        v-if="msg.replyTo && !msg.isRecalled"
        class="mc-msg__quote"
        role="button"
        tabindex="0"
        @click="ctx!.jumpToMessage(msg.replyTo.id)"
        @keydown.enter="ctx!.jumpToMessage(msg.replyTo.id)"
      >
        <span class="mc-msg__quote-name">{{ msg.replyTo.senderNickname || '…' }}</span>
        <span class="mc-msg__quote-text">{{ quoteText(msg.replyTo) }}</span>
      </div>

      <!-- 撤回：满 3 分钟后整条从列表中过滤掉 -->
      <div v-if="msg.isRecalled" class="mc-msg__bubble recalled">
        {{ recallLabel(msg) }}
      </div>

      <!-- 图片：点击放大 -->
      <div v-else-if="msg.msgType === 'image'" class="mc-msg__media">
        <!-- 不能用 loading="lazy"：DynamicScroller 依赖测量 item 高度，
             图片未加载时高度为 0 → 永远进不了视口 → 永远不会加载（死循环）。
             虚拟滚动本身已经保证只渲染视口附近的消息，无需再 lazy。 -->
        <img
          :src="msg.content"
          alt=""
          decoding="async"
          @click="ctx!.onMediaClick(msg, $event)"
          @contextmenu.prevent.stop="ctx!.openImageMenu($event, msg)"
        />
      </div>

      <!-- 语音：自定义气泡（宽度随时长、进度条、同时只播一条） -->
      <VoiceMessage
        v-else-if="msg.msgType === 'audio'"
        :src="msg.content"
        :mine="mine"
      />

      <!-- 视频：原生控件（播放/进度/全屏交给浏览器）。
           固定 16:9 占位：DynamicScroller 依赖测量高度，
           元数据到达前高度为 0 会让列表跳动。 -->
      <div v-else-if="msg.msgType === 'video'" class="mc-msg__video">
        <video
          :src="msg.content"
          controls
          playsinline
          preload="metadata"
          @contextmenu.prevent.stop="ctx!.openMenu($event, msg)"
        />
      </div>

      <!-- 表情：透明底、无气泡（和图片区分：表情是"贴"在对话里的） -->
      <div v-else-if="msg.msgType === 'sticker'" class="mc-msg__sticker">
        <img
          :src="msg.content"
          alt=""
          decoding="async"
          @click="ctx!.onMediaClick(msg, $event)"
          @contextmenu.prevent.stop="ctx!.openImageMenu($event, msg)"
        />
      </div>

      <!-- 合并转发卡片：固定高度摘要，详情走抽屉 -->
      <MergeForwardCard v-else-if="msg.msgType === 'chatcard'" :msg="msg" />

      <!-- 自定义类型：插件白名单渲染 -->
      <div v-else-if="msg.msgType !== 'text'" class="mc-msg__bubble custom">
        <DeclarativeNodes v-if="isRenderedOk(msg)" :nodes="renderedNodes(msg)" />
        <span v-else class="text-placeholder">
          {{ t('message.unknownType', { type: msg.msgType }) }}
        </span>
      </div>

      <!-- 普通文本 -->
      <div v-else class="mc-msg__bubble" :class="{ pending: msg.pending, failed: msg.failed }">
        <span class="mc-msg__text">
          <template v-for="(p, pi) in textParts(msg)" :key="pi">
            <span v-if="p.at" class="mc-msg__at">{{ p.t }}</span>
            <template v-else>{{ p.t }}</template>
          </template>
        </span>
        <span
          v-if="canRecall(msg)"
          class="mc-msg__recall"
          role="button"
          tabindex="0"
          :aria-label="t('message.recallAria')"
          @click.stop="ctx!.recall(msg)"
          @keydown.enter="ctx!.recall(msg)"
        >{{ t('message.recall') }}</span>
        <span v-if="msg.failed" class="mc-msg__resend" @click.stop="convStore.resend(msg)">
          {{ t('message.resend') }}
        </span>
      </div>

      <!-- 表情回应：已有点击切换，+ 展开预设表情。
           暂未开放（FEATURES.reactions=false 隐藏，逻辑保留），开关在 src/config/features.ts -->
      <div
        v-if="FEATURES.reactions && !msg.isRecalled && msg.id > 0"
        class="mc-msg__reactions"
      >
        <button
          v-for="r in msg.reactions ?? []"
          :key="r.emoji"
          type="button"
          class="mc-msg__reaction"
          :class="{ 'is-mine': r.mine }"
          @click="ctx!.react(msg, r.emoji)"
        >
          {{ r.emoji }}<span class="num">{{ r.count }}</span>
        </button>
        <button
          type="button"
          class="mc-msg__reaction-add"
          :aria-label="t('message.addReaction')"
          :title="t('message.addReaction')"
          @click="ctx!.toggleReactionPicker(msg.id)"
        >
          +
        </button>
        <div v-if="pickerOpen" class="mc-msg__reaction-picker">
          <button
            v-for="e in REACTION_EMOJIS"
            :key="e"
            type="button"
            @click="ctx!.react(msg, e)"
          >
            {{ e }}
          </button>
        </div>
      </div>

      <div class="mc-msg__meta text-placeholder num">
        <!-- 发送失败：所有会话类型都给重试入口（原来只有单聊显示了文字，群里连提示都没有） -->
        <button
          v-if="msg.failed"
          type="button"
          class="mc-msg__retry"
          :aria-label="t('message.resend')"
          @click.stop="retrySend(msg)"
        >
          {{ t('message.sendFailed') }} · {{ t('message.resend') }}
        </button>
        <template v-else-if="mine && convType === 1">
          {{ ctx!.messageStatus(msg) }} ·
        </template>
        {{ fmtTime(msg.createdAt) }}
        <span v-if="msg.editedAt"> · {{ t('message.edited') }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.mc-msg {
  display: flex;
  gap: 10px;
  /* 气泡之间的呼吸感：上下各 11px。
     用 padding 而非 margin：DynamicScroller 按 wrapper 高度测量 item，
     margin 不计入会导致相邻消息的 meta 行互相贴边、视觉上被裁。 */
  padding: 11px 0;
}

.mc-msg.mine {
  flex-direction: row-reverse;
}

.mc-msg__body {
  max-width: 64%;
  display: flex;
  flex-direction: column;
}

.mc-msg.mine .mc-msg__body {
  align-items: flex-end;
}

.mc-msg__sender {
  font-size: 12px;
  margin-bottom: 2px;
}

/* ===== 引用块 ===== */
.mc-msg__quote {
  display: flex;
  flex-direction: column;
  gap: 1px;
  max-width: 100%;
  margin-bottom: 4px;
  padding: 5px 9px;
  border-left: 2px solid var(--td-brand-color);
  border-radius: 4px;
  background: var(--td-bg-color-secondarycontainer);
  cursor: pointer;
  transition: background-color var(--mc-dur-instant) var(--mc-ease-out);
}

.mc-msg__quote:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-msg__quote-name {
  font-size: var(--mc-fs-caption);
  color: var(--td-brand-color);
}

.mc-msg__quote-text {
  font-size: var(--mc-fs-sm);
  color: var(--td-text-color-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

@keyframes mc-msg-flash {
  0%,
  40% {
    background: color-mix(in srgb, var(--td-brand-color) 14%, transparent);
    border-radius: var(--mc-radius-md);
  }
  100% {
    background: transparent;
  }
}

/* ===== 图片 / 语音消息 ===== */
.mc-msg__media {
  max-width: 240px;
  /* 加载完成前先占一块高度，避免图片到达时消息列表整体跳动 */
  min-height: 80px;
  border-radius: var(--mc-radius-lg);
  overflow: hidden;
  background: var(--td-bg-color-container);
  line-height: 0;
}

.mc-msg__media img {
  display: block;
  width: 100%;
  max-height: 280px;
  object-fit: cover;
  cursor: zoom-in;
}

/* 视频：固定 16:9 占位，竖屏视频以黑边居中（object-fit: contain） */
.mc-msg__video {
  width: min(260px, 62vw);
  aspect-ratio: 16 / 9;
  border-radius: var(--mc-radius-lg);
  overflow: hidden;
  background: #000;
  line-height: 0;
}

.mc-msg__video video {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
}

/* 发送失败的重发入口：低调但可点，不用另起一行 */
.mc-msg__retry {
  padding: 0;
  border: none;
  background: transparent;
  color: var(--td-error-color);
  font: inherit;
  cursor: pointer;
  text-decoration: underline;
  text-underline-offset: 2px;
}

.mc-msg__retry:hover {
  color: var(--td-error-color);
  text-decoration-thickness: 2px;
}

/* 表情：不套气泡背景，透明贴图 */
.mc-msg__sticker img {
  display: block;
  width: 120px;
  height: 120px;
  object-fit: contain;
  cursor: zoom-in;
}

.mc-msg__voice audio {
  height: 34px;
  max-width: 240px;
  border-radius: var(--mc-radius-md);
}

.mc-msg__bubble {
  position: relative;
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  padding: 8px 12px;
  word-break: break-word;
  white-space: pre-wrap;
}

/* 气泡朝头像方向收一个角，弱化方向感，不额外画尾巴 */
.mc-msg:not(.mine) .mc-msg__bubble {
  border-top-left-radius: 4px;
}

.mc-msg.mine .mc-msg__bubble {
  background: var(--td-brand-color);
  border-color: transparent;
  border-top-right-radius: 4px;
  color: #fff;
}

.mc-msg__bubble.pending {
  opacity: 0.55;
}

.mc-msg__bubble.failed {
  outline: 1px solid var(--td-error-color);
}

.mc-msg__bubble.recalled {
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  background: transparent;
  border-color: transparent;
  box-shadow: none;
  padding: 2px 0;
}

.mc-msg.is-selecting {
  padding-left: 6px;
  padding-right: 6px;
  border-radius: var(--mc-radius-sm);
  cursor: pointer;
}

.mc-msg.is-selected {
  background: color-mix(in srgb, var(--td-brand-color) 10%, transparent);
}

.mc-msg__check {
  flex-shrink: 0;
  align-self: center;
  width: 18px;
  height: 18px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--td-component-border);
  border-radius: 50%;
  background: var(--td-bg-color-container);
  color: transparent;
  font-size: 12px;
  line-height: 1;
}

.mc-msg__check.is-on {
  border-color: var(--td-brand-color);
  background: var(--td-brand-color);
  color: #fff;
}

/* ===== 转发：气泡上的来源标注 ===== */
.mc-msg__forward {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  margin-bottom: 2px;
  font-size: var(--mc-fs-caption);
  color: var(--td-text-color-placeholder);
}

/* 必须盖过 .mc-msg.mine .mc-msg__bubble（3 类选择器）：
   否则自己撤回的提示会被染成品牌蓝底白字，看起来像一条真消息。 */
.mc-msg.is-recalled .mc-msg__bubble.recalled {
  background: transparent;
  color: var(--td-text-color-placeholder);
  border-color: transparent;
  box-shadow: none;
}

.mc-msg.is-recalled .mc-msg__body {
  max-width: 100%;
  align-items: center;
}

.mc-msg.is-recalled .mc-msg__meta {
  display: none;
}

.mc-msg__bubble.custom {
  white-space: normal;
}

.mc-msg__text {
  user-select: text;
}

/* 被 @ 的昵称：微信里是蓝色，这里跟随气泡前景色加粗，亮暗都可读 */
.mc-msg__at {
  font-weight: 600;
  color: var(--td-brand-color);
}

.mc-msg.mine .mc-msg__at {
  color: #fff;
  text-decoration: underline;
  text-underline-offset: 2px;
}

.mc-msg__recall,
.mc-msg__resend {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s;
  user-select: none;
  white-space: nowrap;
}

.mc-msg__recall {
  right: -46px;
}

.mc-msg.mine .mc-msg__recall {
  left: -46px;
  right: auto;
}

.mc-msg:hover .mc-msg__recall,
.mc-msg:focus-within .mc-msg__recall {
  opacity: 1;
}

.mc-msg__recall:focus-visible {
  opacity: 1;
  outline: 1px solid var(--td-brand-color);
  border-radius: 2px;
}

.mc-msg__resend {
  right: -52px;
  color: var(--td-error-color);
  opacity: 1;
}

/* ===== 表情回应 ===== */
.mc-msg__reactions {
  position: relative;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  margin-top: 4px;
}

.mc-msg__reaction,
.mc-msg__reaction-add {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 1px 7px;
  border: 1px solid var(--mc-hairline);
  border-radius: 999px;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-primary);
  font: inherit;
  font-size: 12px;
  line-height: 18px;
  cursor: pointer;
  transition:
    border-color var(--mc-dur-fast) var(--mc-ease-out),
    background-color var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-msg__reaction:hover,
.mc-msg__reaction.is-mine {
  border-color: var(--td-brand-color);
}

.mc-msg__reaction.is-mine {
  background: var(--td-brand-color-1, rgba(0, 82, 217, 0.08));
  color: var(--td-brand-color);
}

.mc-msg__reaction .num {
  font-size: 11px;
  color: var(--td-text-color-secondary);
}

.mc-msg__reaction.is-mine .num {
  color: var(--td-brand-color);
}

.mc-msg__reaction-add {
  width: 22px;
  height: 22px;
  padding: 0;
  justify-content: center;
  border-style: dashed;
  color: var(--td-text-color-placeholder);
  opacity: 0;
}

.mc-msg:hover .mc-msg__reaction-add,
.mc-msg:focus-within .mc-msg__reaction-add,
.mc-msg__reaction-add:focus-visible {
  opacity: 1;
}

.mc-msg__reaction-picker {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 0;
  z-index: 20;
  display: flex;
  gap: 2px;
  padding: 4px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  background: var(--td-bg-color-container);
  box-shadow: var(--mc-shadow-2);
}

.mc-msg.mine .mc-msg__reaction-picker {
  left: auto;
  right: 0;
}

.mc-msg__reaction-picker button {
  width: 30px;
  height: 30px;
  padding: 0;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
}

.mc-msg__reaction-picker button:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-msg__meta {
  font-size: var(--mc-fs-sm);
  /* 明确行高：12px 字体在默认行高下中文字形下缘容易被裁 */
  line-height: 18px;
  margin-top: 4px;
  user-select: none;
  letter-spacing: 0.01em;
}
</style>
