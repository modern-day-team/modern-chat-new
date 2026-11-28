#!/usr/bin/env node
/**
 * 插件开发闭环 CLI：check / push / watch。
 *
 * 为什么需要它：在此之前，改一行插件代码就要回到网页里「复制 → 粘贴 → 安装 → 启用」，
 * 一次迭代四次操作，而且清单错误要等安装才暴露。这个脚本把「校验」和「安装」搬到
 * 终端，配合 --watch 就是一个最简热更新循环。
 *
 * 校验规则的两层：
 *   - 离线（读文件 + 纯 JS 规则）：不依赖服务端，能跑在 CI 里；
 *   - 在线（POST /api/plugins/validate）：与安装共用服务端那份 Go 规则，
 *     所以「本地通过 → 安装 400」这种漂移不会发生。
 * 离线规则是 Go 侧的镜像，不是真源；两边不一致时以在线校验为准（脚本会提示）。
 *
 * 用法：
 *   node scripts/dev-plugin.mjs check  plugins/examples/dice.plugin.js
 *   node scripts/dev-plugin.mjs check  plugins/examples/dice.plugin.js --url http://localhost:8080 --token $TOKEN
 *   node scripts/dev-plugin.mjs push   my.plugin.js --url http://localhost:8080 --user admin --pass secret
 *   node scripts/dev-plugin.mjs watch  my.plugin.js --url http://localhost:8080 --token $TOKEN
 *
 * 认证优先级：--token > 环境变量 MC_TOKEN > --user/--pass（调 /api/auth/login）。
 * 开启两步验证的账号无法在此登录，请改用一个带 token 的方式。
 */
import { readFileSync, watchFile, statSync } from 'node:fs'
import { extname } from 'node:path'

const PERMISSIONS = new Set([
  'message:read', 'message:send', 'message:render', 'ui:panel', 'storage', 'net',
  'ui:theme', 'conv:read', 'ui:actions', 'ui:host', 'ui:page', 'data', 'ai',
  'conv:write', 'file:upload',
])

const MAX_SIZE = 1 << 20 // 与 PLUGIN_MAX_SIZE 默认值一致
// 与 web/src/composables/plugin/sandbox.ts 的 api.version、Go 侧 plugin.SDKLevel 同源；
// 只用于离线提示，真正的兼容性判定在服务端。
const SDK_LEVEL = 5
const VERSION_RE = /^\d{1,6}(\.\d{1,6}){0,2}$/
const DEP_NAME_RE = /^[A-Za-z0-9_.-]{1,30}$/
const HOST_RE = /^(\*\.)?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]*[a-z0-9])?$/

// ===== 参数 =====

function parseArgs(argv) {
  const [command, ...rest] = argv
  const opts = { command, file: '', url: '', token: '', user: '', pass: '', enable: true }
  for (let i = 0; i < rest.length; i += 1) {
    const arg = rest[i]
    if (arg === '--url') opts.url = rest[++i] ?? ''
    else if (arg === '--token') opts.token = rest[++i] ?? ''
    else if (arg === '--user') opts.user = rest[++i] ?? ''
    else if (arg === '--pass') opts.pass = rest[++i] ?? ''
    else if (arg === '--no-enable') opts.enable = false
    else if (arg.startsWith('--')) fail(`未知参数 ${arg}`)
    else if (!opts.file) opts.file = arg
  }
  return opts
}

function fail(message) {
  console.error(`✗ ${message}`)
  process.exit(2)
}

// ===== 清单校验（Go 规则镜像） =====

function extractManifest(source) {
  const m = source.match(/\/\*\s*(\{[\s\S]*?\})\s*\*\//)
  if (!m) return { error: '缺少 manifest 块注释（/* { ... } */）' }
  try {
    return { manifest: JSON.parse(m[1]) }
  } catch (e) {
    return { error: `manifest 不是合法 JSON：${e.message}` }
  }
}

function parseVersion(raw) {
  const s = String(raw).trim().replace(/^v/, '')
  if (!VERSION_RE.test(s)) return null
  const p = s.split('.').map((x) => Number(x))
  return [p[0] ?? 0, p[1] ?? 0, p[2] ?? 0]
}

function compare(a, b) {
  for (let i = 0; i < 3; i += 1) if (a[i] !== b[i]) return a[i] < b[i] ? -1 : 1
  return 0
}

function parseRequirement(raw) {
  const s = String(raw ?? '').trim()
  if (!s || s === '*' || s.toLowerCase() === 'x') return { op: '*', ver: [0, 0, 0] }
  let op = '='
  let body = s
  for (const candidate of ['>=', '<=', '^', '~', '>', '<', '=']) {
    if (s.startsWith(candidate)) {
      op = candidate
      body = s.slice(candidate.length).trim()
      break
    }
  }
  const ver = parseVersion(body)
  return ver ? { op, ver } : null
}

function satisfies(version, requirement) {
  const v = parseVersion(version)
  const r = parseRequirement(requirement)
  if (!v || !r) return false
  const c = compare(v, r.ver)
  switch (r.op) {
    case '*': return true
    case '=': return c === 0
    case '>=': return c >= 0
    case '<=': return c <= 0
    case '>': return c > 0
    case '<': return c < 0
    case '~': return c >= 0 && compare(v, [r.ver[0], r.ver[1] + 1, 0]) < 0
    case '^': {
      const upper = r.ver[0] > 0 ? [r.ver[0] + 1, 0, 0] : r.ver[1] > 0 ? [0, r.ver[1] + 1, 0] : [0, 0, r.ver[2] + 1]
      return c >= 0 && compare(v, upper) < 0
    }
    default: return false
  }
}

/** 返回 { errors, warnings, manifest }。 */
function validateOffline(source, sdkLevel) {
  const errors = []
  const warnings = []
  const { manifest, error } = extractManifest(source)
  if (error) return { errors: [error], warnings, manifest: null }

  const name = manifest.name
  if (typeof name !== 'string' || !name.length || name.length > 30) errors.push('name 需为 1-30 个字符')
  if (typeof manifest.version !== 'string' || !/^\d+\.\d+\.\d+$/.test(manifest.version)) {
    errors.push('version 需为 x.y.z 形式')
  }
  if (manifest.description != null && String(manifest.description).length > 255) {
    errors.push('description 不能超过 255 字')
  }
  if (!Array.isArray(manifest.permissions) || manifest.permissions.length === 0) {
    errors.push('permissions 必须是非空数组')
  } else {
    for (const p of manifest.permissions) {
      if (!PERMISSIONS.has(p)) errors.push(`未知权限 ${JSON.stringify(p)}`)
    }
  }
  const hosts = manifest.allowedHosts ?? []
  if (!Array.isArray(hosts)) errors.push('allowedHosts 必须是数组')
  else {
    if (hosts.length > 20) errors.push('allowedHosts 最多 20 项')
    for (const h of hosts) {
      if (typeof h !== 'string' || !HOST_RE.test(h.trim().toLowerCase())) {
        errors.push(`allowedHosts 含非法域名 ${JSON.stringify(h)}`)
      }
    }
  }

  // engines：未知键一律报错（Go 侧同规则），拼错的键会让你以为兼容性检查生效了
  const engines = manifest.engines
  if (engines != null) {
    if (typeof engines !== 'object' || Array.isArray(engines)) errors.push('engines 必须是对象')
    else {
      for (const key of Object.keys(engines)) {
        if (key !== 'sdk') errors.push(`engines 只支持 "sdk"，未知键 ${JSON.stringify(key)}`)
      }
      if ('sdk' in engines) {
        if (!String(engines.sdk).trim()) errors.push('engines.sdk 不能为空')
        else if (!parseRequirement(engines.sdk)) errors.push('engines.sdk 版本约束非法')
        else if (sdkLevel && !satisfies(String(sdkLevel), engines.sdk)) {
          errors.push(`插件要求 SDK ${engines.sdk}，当前宿主 SDK 为 ${sdkLevel}（服务端安装会拒绝）`)
        }
      }
    }
  }

  const deps = manifest.dependencies
  if (deps != null) {
    if (!Array.isArray(deps)) errors.push('dependencies 必须是数组')
    else {
      if (deps.length > 10) errors.push('dependencies 最多 10 项')
      const seen = new Set()
      for (const raw of deps) {
        if (typeof raw !== 'string') {
          errors.push(`依赖项必须是字符串：${JSON.stringify(raw)}`)
          continue
        }
        const at = raw.indexOf('@')
        const depName = (at < 0 ? raw : raw.slice(0, at)).trim()
        const spec = at < 0 ? '*' : raw.slice(at + 1).trim()
        if (!DEP_NAME_RE.test(depName)) errors.push(`依赖名 ${JSON.stringify(depName)} 只能用字母数字与 _ . -`)
        if (depName === name) errors.push('不能依赖自己')
        if (seen.has(depName)) errors.push(`依赖 ${depName} 重复声明`)
        seen.add(depName)
        if (!parseRequirement(spec)) errors.push(`依赖 ${raw} 的版本约束非法`)
      }
    }
  }

  if (source.length > MAX_SIZE) errors.push('插件文件超过 1MB')
  if (source.length > MAX_SIZE * 0.9) warnings.push('文件接近 1MB 上限')
  return { errors, warnings, manifest }
}

// ===== HTTP =====

function apiBase(opts) {
  if (!opts.url) fail('需要 --url（例如 http://localhost:8080）')
  const base = opts.url.replace(/\/+$/, '')
  return /\/api$/.test(base) ? base : `${base}/api`
}

async function request(url, init) {
  let res
  try {
    res = await fetch(url, init)
  } catch (e) {
    fail(`连不上 ${url}：${e.message}（服务端起了吗？）`)
  }
  const text = await res.text()
  let body = text
  try {
    body = JSON.parse(text)
  } catch {
    // 非 JSON 响应（例如纯文本错误）保留原文
  }
  return { ok: res.ok, status: res.status, body }
}

async function resolveToken(opts) {
  if (opts.token) return opts.token
  if (process.env.MC_TOKEN) return process.env.MC_TOKEN
  if (!opts.user || !opts.pass) return ''
  const { ok, body } = await request(`${apiBase(opts)}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: opts.user, password: opts.pass }),
  })
  if (!ok) fail(`登录失败：${body?.error ?? JSON.stringify(body)}`)
  if (body?.twofa) fail('该账号启用了两步验证，CLI 无法登录；请用 --token / MC_TOKEN')
  return body?.token ?? ''
}

function langOf(file) {
  return ['.ts', '.mts'].includes(extname(file).toLowerCase()) ? 'ts' : 'js'
}

// ===== 子命令 =====

function report(offline, remote) {
  let bad = false
  for (const e of offline.errors) {
    bad = true
    console.error(`✗ ${e}`)
  }
  for (const w of offline.warnings) console.warn(`! ${w}`)
  if (remote) {
    for (const e of remote.errors ?? []) {
      bad = true
      console.error(`✗ ${e}`)
    }
    for (const n of remote.notes ?? []) console.warn(`! ${n}`)
  }
  if (!bad) {
    const m = offline.manifest
    console.log(`✓ 清单合法：${m.name}@${m.version}（${m.permissions.length} 个权限）`)
  }
  return !bad
}

async function check(opts, { quiet = false } = {}) {
  if (!opts.file) fail('用法：node scripts/dev-plugin.mjs check <插件文件> [--url ...]')
  let source
  try {
    source = readFileSync(opts.file, 'utf8')
  } catch (e) {
    fail(`读不到 ${opts.file}：${e.message}`)
  }
  const offline = validateOffline(source, SDK_LEVEL)
  let remote = null
  if (opts.url) {
    const token = await resolveToken(opts)
    if (!token) fail('在线校验需要 --token / MC_TOKEN，或 --user/--pass')
    const { body } = await request(`${apiBase(opts)}/plugins/validate?lang=${langOf(opts.file)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/octet-stream', Authorization: `Bearer ${token}` },
      body: source,
    })
    remote = body
  }
  if (quiet) return offline.errors.length === 0
  return report(offline, remote)
}

async function push(opts, { quiet = false } = {}) {
  if (!opts.file) fail('用法：node scripts/dev-plugin.mjs push <插件文件> --url ... [--token ...]')
  const source = readFileSync(opts.file, 'utf8')
  const offline = validateOffline(source, SDK_LEVEL)
  if (offline.errors.length) {
    report(offline, null)
    process.exit(1)
  }
  const token = await resolveToken(opts)
  if (!token) fail('push 需要 --token / MC_TOKEN，或 --user/--pass')

  const { ok, status, body } = await request(`${apiBase(opts)}/plugins/upload?lang=${langOf(opts.file)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/octet-stream', Authorization: `Bearer ${token}` },
    body: source,
  })
  if (!ok) {
    console.error(`✗ 安装失败（${status}）：${body?.error ?? JSON.stringify(body)}`)
    process.exit(1)
  }
  const label = `${body.name}@${body.version}`
  if (!quiet) console.log(`✓ 已安装 ${label}（id=${body.id}）`)

  if (opts.enable) {
    const on = await request(`${apiBase(opts)}/plugins/${body.id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      body: JSON.stringify({ enabled: true }),
    })
    if (!on.ok) {
      // 依赖未就绪是最常见的 403：报到 stderr 但不视为脚本失败，装上本身是成功的
      console.warn(`! 已安装但未能启用：${on.body?.error ?? JSON.stringify(on.body)}`)
    } else if (!quiet) {
      console.log('✓ 已启用（刷新页面即可加载新代码）')
    }
  }
  return true
}

function watch(opts) {
  if (!opts.file) fail('用法：node scripts/dev-plugin.mjs watch <插件文件> --url ...')
  let last = statSync(opts.file).mtimeMs
  console.log(`· 监听 ${opts.file}（保存即推送，Ctrl+C 结束）`)
  void push(opts)
  watchFile(opts.file, { interval: 500 }, (curr) => {
    if (curr.mtimeMs === last) return
    last = curr.mtimeMs
    console.log(`\n· ${new Date().toLocaleTimeString()} 检测到修改`)
    push(opts).catch((e) => console.error(`✗ ${e.message}`))
  })
}

// ===== 入口 =====

const opts = parseArgs(process.argv.slice(2))
if (opts.command === 'check') {
  // 校验失败必须以非 0 退出，否则 CI 里「清单坏了但流水线绿」
  void check(opts).then((ok) => {
    process.exitCode = ok ? 0 : 1
  })
} else if (opts.command === 'push') void push(opts)
else if (opts.command === 'watch') watch(opts)
else {
  console.log(
    [
      '用法：',
      '  node scripts/dev-plugin.mjs check <文件> [--url <服务端>] [--token <jwt>]',
      '  node scripts/dev-plugin.mjs push  <文件> --url <服务端> [--token <jwt> | --user <账号> --pass <密码>] [--no-enable]',
      '  node scripts/dev-plugin.mjs watch <文件> --url <服务端> [--token <jwt>]',
    ].join('\n'),
  )
  process.exit(opts.command ? 2 : 0)
}