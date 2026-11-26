<script setup lang="ts">
/**
 * 描述式 UI 的 select 节点懒加载包装。
 * TDesign 的 Select 组件体积较大（约 140KB），而插件消息里的 select 节点并不常见。
 * 若在 DeclarativeNodes 里静态引入，会被聊天主屏关键路径（MessageRow）一并拉入首屏。
 * 这里改为挂载时按需动态加载，仅在插件确实渲染 select 节点时才加载该 chunk，
 * 加载完成前显示轻量占位，避免阻塞首屏。
 */
import { ref, shallowRef, onMounted } from 'vue'

const props = defineProps<{
  value: string
  options: { label: string; value: string }[]
  placeholder: string
}>()

const emit = defineEmits<{ (e: 'change', v: unknown): void }>()

const SelectComp = shallowRef<unknown>(null)

onMounted(async () => {
  try {
    const { Select } = await import('tdesign-vue-next/es/select')
    SelectComp.value = Select
  } catch {
    // 加载失败时保持占位，不阻塞整个面板渲染
  }
})
</script>

<template>
  <component
    :is="SelectComp"
    v-if="SelectComp"
    size="small"
    clearable
    :placeholder="placeholder"
    :value="value"
    :options="options"
    @change="emit('change', $event)"
  />
  <div v-else class="mc-dloading" role="status">
    <span class="mc-dloading__spinner" aria-hidden="true" />
  </div>
</template>
