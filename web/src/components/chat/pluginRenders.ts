/**
 * 插件自定义消息类型的渲染管线（原 MessagePane 内联状态，抽成模块级单例：
 * MessagePane 与 MessageRow 两处都要读 renderedMap，消息 id 全局唯一，
 * 单例 Map 与每面板 Map 等价）。
 */
import { reactive } from 'vue'

import type { Message } from '@/types/proto'
import { usePluginHost } from '@/composables/usePluginHost'
import type { UIDescNode } from '@/types/proto'

export interface RenderedDesc {
  nodes: UIDescNode[]
}

export const renderedMap = reactive<Record<number, RenderedDesc | 'failed' | undefined>>({})

export function ensureRendered(m: Message) {
  if (m.msgType === 'text' || m.isRecalled) return
  if (renderedMap[m.id] !== undefined) return
  const host = usePluginHost()
  if (!host.hasRenderer(m.msgType)) {
    renderedMap[m.id] = 'failed'
    return
  }
  let payload: unknown = m.content
  try {
    payload = JSON.parse(m.content)
  } catch {
    /* 非 JSON 原样传给插件 */
  }
  host
    .requestRender(m.msgType, payload)
    .then((panel) => {
      renderedMap[m.id] = panel ? { nodes: panel.body } : 'failed'
    })
    .catch(() => {
      renderedMap[m.id] = 'failed'
    })
}

export function isRenderedOk(m: Message): boolean {
  const r = renderedMap[m.id]
  return !!r && r !== 'failed'
}

export function renderedNodes(m: Message): RenderedDesc['nodes'] {
  const r = renderedMap[m.id]
  return r && r !== 'failed' ? r.nodes : []
}
