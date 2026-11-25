/**
 * 插件声明式 UI 的形状校验（内核安全边界之一）。
 *
 * 插件沙箱只能产出"描述"（component + 原始值 props），渲染在宿主侧进行；
 * 这里是宿主渲染前唯一的闸门：白名单外组件、javascript: 链接、非原始值
 * props 一律拒绝，杜绝插件把任意 DOM/脚本带进宿主文档。
 */
import type { PageDesc, PanelDesc, UIDescNode } from '@/types/proto'

const ALLOWED_COMPONENTS = new Set([
  'text',
  'image',
  'button',
  'input',
  'select',
  'card',
  'list',
  'divider',
  'link',
  // 数据展示类：表格 / 进度条 / 徽标，让面板能承载统计与状态，而不只是一段文字
  'table',
  'progress',
  'badge',
  // 表单交互控件：让插件面板能收集输入，而不只是一块静态展示
  'textarea',
  'checkbox',
  'switch',
  'segmented',
  // SDK v5 UI 面：结构 / 状态 / 宿主文件选择（props 仍只放行原始值）
  'tabs',
  'steps',
  'empty',
  'loading',
  'upload',
])

/** 校验 UI 描述：白名单外组件、javascript: 链接、非 plain props 一律拒绝 */
export function validateUIDesc(node: unknown, depth = 0): UIDescNode | null {
  if (depth > 8 || !node || typeof node !== 'object') return null
  const n = node as Record<string, unknown>
  if (typeof n.component !== 'string' || !ALLOWED_COMPONENTS.has(n.component)) return null
  // props 是可选的：list / divider 这类纯结构节点在业务上不需要 props，
  // 渲染器也不读它。旧版强制要求 props，会让整个面板被判非法后丢弃。
  const rawProps = n.props
  if (rawProps !== undefined && rawProps !== null && typeof rawProps !== 'object') return null
  const props: Record<string, unknown> = {}
  for (const [k, v] of Object.entries((rawProps ?? {}) as Record<string, unknown>)) {
    // 仅放行原始值，杜绝对象/函数注入面
    if (['string', 'number', 'boolean'].includes(typeof v) || v === null) props[k] = v
  }
  if (n.component === 'link' && typeof props.href === 'string') {
    if (!/^https?:\/\//i.test(props.href)) return null
  }
  if (n.component === 'image' && typeof props.src === 'string') {
    if (!/^https?:\/\//i.test(props.src)) return null
  }
  const children = Array.isArray(n.children)
    ? n.children.map((c) => validateUIDesc(c, depth + 1)).filter((c): c is UIDescNode => !!c)
    : undefined
  return { component: n.component as UIDescNode['component'], props, action: typeof n.action === 'string' ? n.action : undefined, children }
}

/**
 * 校验面板描述：{ title?, body: UIDescNode[] }。
 * 面板对象本身没有 component 字段，不能直接交给 validateUIDesc（会被判 null）；
 * 这里校验外层形状后逐节点校验，任一节点非法则整体拒绝（保持严格的安全边界）。
 */
export function validatePanel(desc: unknown): PanelDesc | null {
  if (!desc || typeof desc !== 'object') return null
  const d = desc as Record<string, unknown>
  if (d.title !== undefined && typeof d.title !== 'string') return null
  if (!Array.isArray(d.body)) return null
  const body: UIDescNode[] = []
  for (const node of d.body) {
    const v = validateUIDesc(node)
    if (!v) return null
    body.push(v)
  }
  return { kind: 'panel', title: typeof d.title === 'string' ? d.title : '', body }
}

/** 渲染结果：面板形状（有 body 数组）走 validatePanel，单节点则包成只含一个节点的面板 */
export function validateRenderDesc(desc: unknown): PanelDesc | null {
  if (!desc || typeof desc !== 'object') return null
  if (Array.isArray((desc as Record<string, unknown>).body)) return validatePanel(desc)
  const node = validateUIDesc(desc)
  return node ? { kind: 'panel', title: '', body: [node] } : null
}

/** 递归统计描述树节点数（含自身），超过上限即提前返回，避免深树反复遍历 */
function countNodes(nodes: readonly UIDescNode[], limit: number): number {
  let total = 0
  for (const node of nodes) {
    total += 1 + countNodes(node.children ?? [], limit)
    if (total > limit) return total
  }
  return total
}

/**
 * 校验插件页面描述：{ id, title, icon?, scope?, body: UIDescNode[] }（SDK v3）。
 * id 是路由片段、title 是页面标题，都按短文本约束；body 完全复用面板渲染白名单规则，
 * 再叠加 100 节点总量上限——页面是全屏渲染，不能被插件用海量节点把主线程顶死。
 */
export function validatePage(desc: unknown): PageDesc | null {
  if (!desc || typeof desc !== 'object') return null
  const d = desc as Record<string, unknown>
  if (typeof d.id !== 'string' || !/^[a-z0-9_-]{1,40}$/.test(d.id)) return null
  if (typeof d.title !== 'string' || d.title.length < 1 || d.title.length > 40) return null
  if (d.icon !== undefined && (typeof d.icon !== 'string' || d.icon.length > 40)) return null
  const scope = (d.scope === undefined ? 'user' : d.scope) as 'user' | 'admin'
  if (scope !== 'user' && scope !== 'admin') return null
  const panel = validateRenderDesc(d)
  if (!panel || countNodes(panel.body, 100) > 100) return null
  return { id: d.id, title: d.title, icon: typeof d.icon === 'string' ? d.icon : undefined, scope, body: panel.body }
}
