/**
 * 插件依赖与兼容性的前端提示（纯函数）。
 *
 * 权威判定在服务端：启用时 server/internal/plugin 会用同一套规则返回 403 并带原因。
 * 这里只负责在用户点开关之前就把「还差什么」摆出来，避免点一次弹一次错误。
 * 规则镜像 server/internal/plugin/requirement.go；两边不一致时以服务端为准。
 */
import type { Plugin } from '@/types/proto'

export type DepIssue = {
  name: string
  raw: string
  reason: 'missing' | 'version' | 'disabled'
}

const VERSION_RE = /^\d{1,6}(\.\d{1,6}){0,2}$/

/** "5" / "1.2" / "1.2.3" → 三段数字；不合法返回 null。 */
export function parseVersion(raw: string): [number, number, number] | null {
  const s = raw.trim().replace(/^v/, '')
  if (!VERSION_RE.test(s)) return null
  const parts = s.split('.').map((p) => Number(p))
  return [parts[0] ?? 0, parts[1] ?? 0, parts[2] ?? 0]
}

function compare(a: [number, number, number], b: [number, number, number]): number {
  for (let i = 0; i < 3; i += 1) {
    const x = a[i] ?? 0
    const y = b[i] ?? 0
    if (x !== y) return x < y ? -1 : 1
  }
  return 0
}

/**
 * 判断版本是否满足约束。^/~ 沿用 npm 语义（^0.x 锁次版本），
 * 与 Go 侧 Satisfies 逐条对应。
 */
export function satisfies(version: string, requirement: string): boolean {
  const v = parseVersion(version)
  const spec = requirement.trim()
  if (!v) return false
  if (!spec || spec === '*' || spec.toLowerCase() === 'x') return true
  let op = '='
  let rest = spec
  for (const candidate of ['>=', '<=', '^', '~', '>', '<', '=']) {
    if (spec.startsWith(candidate)) {
      op = candidate
      rest = spec.slice(candidate.length).trim()
      break
    }
  }
  const target = parseVersion(rest)
  if (!target) return false
  const cmp = compare(v, target)
  switch (op) {
    case '=':
      return cmp === 0
    case '>=':
      return cmp >= 0
    case '<=':
      return cmp <= 0
    case '>':
      return cmp > 0
    case '<':
      return cmp < 0
    case '~':
      return cmp >= 0 && compare(v, [target[0], target[1] + 1, 0]) < 0
    case '^': {
      const upper: [number, number, number] =
        target[0] > 0
          ? [target[0] + 1, 0, 0]
          : target[1] > 0
            ? [0, target[1] + 1, 0]
            : [0, 0, target[2] + 1]
      return cmp >= 0 && compare(v, upper) < 0
    }
    default:
      return false
  }
}

/** "name@^1.0.0" → 名字与约束；不带 @ 视为任意版本。 */
export function splitDependency(raw: string): { name: string; requirement: string } {
  const at = raw.indexOf('@')
  if (at < 0) return { name: raw.trim(), requirement: '*' }
  return { name: raw.slice(0, at).trim(), requirement: raw.slice(at + 1).trim() }
}

/**
 * 列出未满足的依赖。依赖必须同时满足「已安装」「版本符合」「已启用」：
 * 装了但停用的依赖一样无法提供能力，让使用者跑起来只会在运行时炸。
 */
export function unmetDeps(plugin: Pick<Plugin, 'dependencies'>, installed: readonly Plugin[]): DepIssue[] {
  const byName = new Map(installed.map((p) => [p.name, p]))
  const issues: DepIssue[] = []
  for (const raw of plugin.dependencies ?? []) {
    const { name, requirement } = splitDependency(raw)
    const dep = byName.get(name)
    if (!dep) {
      issues.push({ name, raw, reason: 'missing' })
    } else if (!satisfies(dep.version, requirement)) {
      issues.push({ name, raw, reason: 'version' })
    } else if (!dep.enabled || dep.globalDisabled) {
      issues.push({ name, raw, reason: 'disabled' })
    }
  }
  return issues
}

/** 插件声明的 SDK 要求（没有则空串），用于标签展示。 */
export function sdkRequirement(plugin: Pick<Plugin, 'engines'>): string {
  return plugin.engines?.sdk?.trim() ?? ''
}