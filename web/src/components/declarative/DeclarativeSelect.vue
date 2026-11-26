<script setup lang="ts">
/**
 * 描述式 UI 的 select 节点懒加载包装。
 * TDesign 的 Select 组件体积较大（约 140KB），而插件消息里的 select 节点并不常见。
 * 若在 DeclarativeNodes 里静态引入，会被聊天主屏关键路径（MessageRow）一并拉入首屏。
 * 这里改为挂载时按需动态加载，仅在插件确实渲染 select 节点时才加载该 chunk，
 * 加载完成前显示轻量占位，避免阻塞首屏。
 * 加载失败降级为原生 select：功能可用性优先于样式统一，不让加载态永久挂起。
 */
import { shallowRef, ref, onMounted } from 'vue'

defineProps<{
  value: string
  options: { label: string; value: string }[]
  placeholder: string
}>()

const emit = defineEmits<{ (e: 'change', v: unknown): void }>()

const SelectComp = shallowRef<unknown>(null)
const loadFailed = ref(false)

onMounted(async () => {
  try {
    const { Select } = await import('tdesign-vue-next/es/select')
    SelectComp.value = Select
  } catch {
    loadFailed.value = true
  }
})

/** 原生 select 降级的 change 透传（模板表达式不做 TS 断言，收进脚本）。 */
function onNativeChange(ev: Event) {
  emit('change', (ev.target as HTMLSelectElement).value)
}
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
  <select v-else-if="loadFailed" class="mc-dselect" :value="value" @change="onNativeChange">
    <option v-if="!value" value="" disabled>{{ placeholder }}</option>
    <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
  </select>
  <div v-else class="mc-dloading" role="status">
    <span class="mc-dloading__spinner" aria-hidden="true" />
  </div>
</template>
