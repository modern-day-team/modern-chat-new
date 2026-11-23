<script setup lang="ts">
import { ref } from 'vue'

/**
 * 视频按需加载：未点击前不设置 src（零请求），用固定 16:9 占位 + 播放按钮；
 * 点击后才绑定 src 并自动播放。浏览器随后按 HTTP Range 流式拉取，
 * 拖动进度条只请求缺失片段，不重下整文件。
 */
const props = defineProps<{ src: string }>()
const videoEl = ref<HTMLVideoElement | null>(null)
const loaded = ref(false)
const failed = ref(false)

async function start() {
  if (loaded.value) return
  loaded.value = true
  // 下一帧再 play：等 src 绑定完成
  requestAnimationFrame(() => {
    void videoEl.value?.play().catch(() => undefined)
  })
}

function onError() {
  failed.value = true
}
</script>

<template>
  <div class="mc-lazy-video" :class="{ 'is-failed': failed }">
    <video
      ref="videoEl"
      :src="loaded ? props.src : undefined"
      controls
      playsinline
      preload="none"
      @error="onError"
    />
    <button
      v-if="!loaded"
      type="button"
      class="mc-lazy-video__cover"
      aria-label="加载并播放视频"
      @click="start"
    >
      <span class="mc-lazy-video__play" aria-hidden="true">▶</span>
      <span class="mc-lazy-video__hint">点击加载视频</span>
    </button>
    <span v-if="failed" class="mc-lazy-video__failed">视频加载失败</span>
  </div>
</template>

<style scoped>
.mc-lazy-video {
  position: relative;
  width: 100%;
  aspect-ratio: 16 / 9;
  border-radius: var(--mc-radius-md);
  overflow: hidden;
  background: var(--mc-bg-raised, #f2f3f5);
}

.mc-lazy-video video {
  width: 100%;
  height: 100%;
  object-fit: contain;
  display: block;
}

.mc-lazy-video__cover {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border: none;
  background: transparent;
  color: var(--mc-text-muted, #6b7280);
  cursor: pointer;
}

.mc-lazy-video__play {
  display: grid;
  place-items: center;
  width: 48px;
  height: 48px;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  font-size: 18px;
  padding-left: 3px;
}

.mc-lazy-video__hint {
  font-size: var(--mc-fs-sm, 12px);
}

.mc-lazy-video__failed {
  position: absolute;
  left: 12px;
  bottom: 12px;
  font-size: var(--mc-fs-sm, 12px);
  color: var(--mc-danger, #d64949);
}
</style>
