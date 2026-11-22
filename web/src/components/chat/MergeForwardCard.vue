<script setup lang="ts">
/**
 * 合并转发卡片：气泡里只放固定行数的摘要（虚拟列表依赖稳定高度），
 * 点开后走 Teleport 抽屉展示完整快照，不在消息行内撑高。
 * 详情里的媒体 URL 来自服务端落库的快照，服务端已校验归属。
 */
import { computed, ref } from 'vue'

import UiDrawer from '@/components/ui/UiDrawer.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import type { Message } from '@/types/proto'
import { parseMergeCard, type MergeCardItem } from './mergeCard'

const props = defineProps<{ msg: Message }>()
const { t } = useAppI18n()

const open = ref(false)
const card = computed(() => parseMergeCard(props.msg.content))
const preview = computed(() => card.value?.items.slice(0, 3) ?? [])
const count = computed(() => card.value?.items.length ?? 0)

function bodyText(item: MergeCardItem): string {
  if (item.m === 'image') return t('message.quoteImage')
  if (item.m === 'audio') return t('message.quoteAudio')
  if (item.m === 'sticker') return t('message.quoteSticker')
  if (item.m === 'video') return t('message.quoteVideo')
  if (item.m === 'chatcard') return t('message.quoteCard')
  return item.c
}

function itemTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function mediaClass(item: MergeCardItem): string {
  return item.m === 'sticker' ? 'mc-card__sticker' : 'mc-card__media'
}
</script>

<template>
  <button
    v-if="card"
    type="button"
    class="mc-card"
    :aria-label="t('message.cardOpen')"
    @click.stop="open = true"
  >
    <div class="mc-card__head">
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden="true">
        <rect x="4" y="3" width="16" height="18" rx="2" stroke="currentColor" stroke-width="1.5" />
        <path d="M8 8h8M8 12h8M8 16h5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
      </svg>
      <span class="mc-card__title">{{ card.title || t('message.mergeForward') }}</span>
    </div>
    <div class="mc-card__lines">
      <div v-for="(item, i) in preview" :key="i" class="mc-card__line">
        <span class="mc-card__who">{{ item.n }}</span>
        <span class="mc-card__sep">:</span>
        <span class="mc-card__body">{{ bodyText(item) }}</span>
      </div>
    </div>
    <div class="mc-card__foot">
      <span>{{ t('message.cardCount', { n: count }) }}</span>
      <span class="mc-card__more">{{ t('message.cardOpen') }} ›</span>
    </div>
  </button>
  <div v-else class="mc-card__broken text-placeholder">{{ t('message.cardBroken') }}</div>

  <UiDrawer v-model:visible="open" :header="card?.title || t('message.mergeForward')" :size="560">
    <div v-if="card" class="mc-card__detail">
      <p class="mc-card__detail-head text-placeholder">
        {{ t('message.cardCount', { n: count }) }} · {{ t('message.cardSnapshot') }}
      </p>
      <div v-for="(item, i) in card.items" :key="i" class="mc-card__entry">
        <div class="mc-card__entry-meta text-placeholder">
          <span class="mc-card__entry-name">{{ item.n }}</span>
          <span class="num">{{ itemTime(item.t) }}</span>
        </div>
        <div v-if="item.m === 'text'" class="mc-card__entry-text">{{ item.c }}</div>
        <img
          v-else-if="item.m === 'image'"
          :src="item.c"
          alt=""
          decoding="async"
          :class="mediaClass(item)"
        />
        <img
          v-else-if="item.m === 'sticker'"
          :src="item.c"
          alt=""
          decoding="async"
          :class="mediaClass(item)"
        />
        <audio v-else-if="item.m === 'audio'" :src="item.c" controls preload="metadata" />
        <video v-else-if="item.m === 'video'" :src="item.c" controls playsinline preload="metadata" />
        <div v-else class="mc-card__entry-text text-placeholder">{{ bodyText(item) }}</div>
      </div>
    </div>
  </UiDrawer>
</template>

<style scoped>
.mc-card {
  display: block;
  width: min(320px, 68vw);
  padding: 11px 13px 9px;
  text-align: left;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  background: var(--mc-material);
  color: var(--td-text-color-primary);
  cursor: pointer;
  transition: background var(--mc-dur-fast) var(--mc-ease-in-out);
}
.mc-card:hover {
  background: var(--td-bg-color-container-hover);
}
.mc-card__head {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--td-brand-color);
}
.mc-card__title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--mc-fs-body);
  font-weight: 600;
}
.mc-card__lines {
  margin-top: 7px;
  padding-left: 4px;
  border-left: 2px solid var(--mc-hairline);
}
.mc-card__line {
  display: flex;
  gap: 3px;
  min-width: 0;
  font-size: var(--mc-fs-caption);
  line-height: 1.5;
  color: var(--td-text-color-secondary);
}
.mc-card__who {
  flex-shrink: 0;
  max-width: 88px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mc-card__sep {
  flex-shrink: 0;
}
.mc-card__body {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mc-card__foot {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  margin-top: 8px;
  font-size: var(--mc-fs-caption);
  color: var(--td-text-color-placeholder);
}
.mc-card__more {
  color: var(--td-brand-color);
}

.mc-card__detail {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.mc-card__detail-head {
  margin: 0;
  font-size: var(--mc-fs-caption);
}
.mc-card__entry {
  padding: 10px 12px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  background: var(--td-bg-color-container);
}
.mc-card__broken {
  padding: 8px 12px;
  border-radius: var(--mc-radius-md);
  background: var(--mc-material);
}
.mc-card__entry-meta {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  font-size: var(--mc-fs-caption);
}
.mc-card__entry-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mc-card__entry-text {
  margin-top: 5px;
  font-size: var(--mc-fs-body);
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.mc-card__media {
  display: block;
  max-width: 100%;
  max-height: 260px;
  margin-top: 6px;
  border-radius: var(--mc-radius-sm);
}
.mc-card__sticker {
  display: block;
  max-width: 140px;
  max-height: 140px;
  margin-top: 6px;
}
.mc-card__entry audio,
.mc-card__entry video {
  display: block;
  max-width: 100%;
  margin-top: 6px;
}
</style>
