#!/usr/bin/env node
/**
 * 插件市场单一真源同步：plugins/examples/ → 官网 public/content/plugins/。
 *
 * - 校验每个示例插件的 manifest（源码首个块注释里的 JSON，规则与
 *   server/internal/plugin/manifest.go 一致：name 1-30、version x.y.z、
 *   permissions 必须非空、description ≤255）
 * - 拷贝源码副本到 website/public/content/plugins/（.plugin.json 是服务端
 *   插件示例，走管理员安装，不进市场，跳过）
 * - 生成 index.json：机器可读的市场索引，官网「一键安装」与版本展示
 *   均以此为准（替换原先手写在 PluginsView 里的版本号）
 *
 * 用法：
 *   node scripts/sync-plugins.mjs          # 同步副本 + 生成 index.json
 *   node scripts/sync-plugins.mjs --check  # CI 校验：不写盘，发现漂移退出 1
 *
 * 新增示例插件：源码写好后跑一次本脚本，再在 website PluginsView.vue 的
 * MKT_TEXT 表补中英文案（名称 / 版本 / 权限自动取自 manifest）。
 */
import { readdirSync, readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { join, resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const SRC_DIR = join(ROOT, 'plugins', 'examples')
const OUT_DIR = join(ROOT, 'website', 'public', 'content', 'plugins')
const INDEX_FILE = join(OUT_DIR, 'index.json')
const CHECK = process.argv.includes('--check')

/** 提取源码首个块注释里的 manifest JSON（容忍前置的 /// reference 注释）。 */
function extractManifest(src, file) {
  const m = src.match(/\/\*\s*(\{[\s\S]*?\})\s*\*\//)
  if (!m) throw new Error(`${file}: 找不到 manifest 块注释（/* { ... } */）`)
  let json
  try {
    json = JSON.parse(m[1])
  } catch (e) {
    throw new Error(`${file}: manifest 不是合法 JSON（${e.message}）`)
  }
  if (typeof json.name !== 'string' || json.name.length < 1 || json.name.length > 30) {
    throw new Error(`${file}: name 须为 1-30 字符`)
  }
  if (typeof json.version !== 'string' || !/^\d+\.\d+\.\d+$/.test(json.version)) {
    throw new Error(`${file}: version 须为 x.y.z`)
  }
  if (
    !Array.isArray(json.permissions) ||
    json.permissions.length === 0 ||
    !json.permissions.every((p) => typeof p === 'string')
  ) {
    throw new Error(`${file}: permissions 须为非空字符串数组`)
  }
  if (!Array.isArray(json.allowedHosts ?? [])) throw new Error(`${file}: allowedHosts 须为数组`)
  if (typeof json.description !== 'string' || json.description.length > 255) {
    throw new Error(`${file}: description 缺失或超过 255 字符`)
  }
  // 分发契约：engines / dependencies 会随 index.json 一起发布，市场页据此展示
  // 「需要 SDK x」「依赖谁」。形状不对就直接拒绝，别把坏数据带进市场。
  if (json.engines != null && (typeof json.engines !== 'object' || Array.isArray(json.engines))) {
    throw new Error(`${file}: engines 须为对象`)
  }
  const deps = json.dependencies ?? []
  if (!Array.isArray(deps) || !deps.every((d) => typeof d === 'string')) {
    throw new Error(`${file}: dependencies 须为字符串数组`)
  }
  if (deps.length > 10) throw new Error(`${file}: dependencies 最多 10 项`)
  return json
}

const files = readdirSync(SRC_DIR)
  .filter((f) => /\.plugin\.(js|ts)$/.test(f))
  .sort()

const entries = files.map((file) => {
  const src = readFileSync(join(SRC_DIR, file), 'utf8')
  const m = extractManifest(src, file)
  return {
    file,
    name: m.name,
    version: m.version,
    description: m.description,
    permissions: m.permissions,
    allowedHosts: m.allowedHosts ?? [],
    // 缺省也写出来：市场前端不用判 undefined，索引形状固定
    engines: m.engines ?? {},
    dependencies: m.dependencies ?? [],
    lang: file.endsWith('.ts') ? 'ts' : 'js',
  }
})

// 按 name 排序输出，保证 index.json 内容确定性（CI --check 可靠比对）
entries.sort((a, b) => a.name.localeCompare(b.name))
const indexJson = `${JSON.stringify({ plugins: entries }, null, 2)}\n`

const drifted = []

// 副本：与源码逐字节比对
for (const file of files) {
  const src = readFileSync(join(SRC_DIR, file))
  let dst = null
  try {
    dst = readFileSync(join(OUT_DIR, file))
  } catch {
    /* 缺文件视为漂移 */
  }
  if (!dst || !src.equals(dst)) drifted.push(`website/public/content/plugins/${file} 与 plugins/examples/${file} 不一致`)
}

// index.json：字符串比对
let current = null
try {
  current = readFileSync(INDEX_FILE, 'utf8')
} catch {
  drifted.push('website/public/content/plugins/index.json 缺失')
}
if (current !== null && current !== indexJson) drifted.push('index.json 与 plugins/examples 的 manifest 不一致')

if (CHECK) {
  if (drifted.length) {
    console.error('插件市场副本漂移，请运行 `node scripts/sync-plugins.mjs` 后提交：')
    for (const d of drifted) console.error(`  - ${d}`)
    process.exit(1)
  }
  console.log(`插件市场同步校验通过（${entries.length} 个插件）`)
  process.exit(0)
}

mkdirSync(OUT_DIR, { recursive: true })
for (const file of files) {
  writeFileSync(join(OUT_DIR, file), readFileSync(join(SRC_DIR, file)))
}
writeFileSync(INDEX_FILE, indexJson)
console.log(`已同步 ${files.length} 个插件副本并生成 index.json`)
