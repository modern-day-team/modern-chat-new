/**
 * 插件市场一键安装深链的纯逻辑（官网 → 聊天应用）。
 * 深链形状：/chat?plugin-install=<插件URL>&plugin-name=<名称>。
 * 只做解析与判定，不做导航/安装——那些留在 ChatLayout / PluginDrawer。
 */

/** 深链解析结果；plugin-install 缺失或为空时返回 null。 */
export interface PluginInstallPreset {
  url: string
  name: string
}

/**
 * 从路由 query 里读市场深链。
 * vue-router 的 query 值可能是 string 或 string[]（同名参数重复时），
 * 这里只认 string，数组与其它类型一律忽略（防构造畸形链接）。
 */
export function readPluginInstallQuery(q: Record<string, unknown>): PluginInstallPreset | null {
  const u = q['plugin-install']
  if (typeof u !== 'string' || !u.trim()) return null
  const n = q['plugin-name']
  return { url: u, name: typeof n === 'string' ? n : '' }
}

/** 语义化版本比较：old 大于 new 时为降级安装（缺失段按 0 补齐）。 */
export function isDowngrade(oldV: string, newV: string): boolean {
  const a = oldV.split('.').map(Number)
  const b = newV.split('.').map(Number)
  for (let i = 0; i < 3; i++) {
    const x = a[i] ?? 0
    const y = b[i] ?? 0
    if (x !== y) return x > y
  }
  return false
}
