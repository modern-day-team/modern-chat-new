#!/usr/bin/env node
/**
 * 构建产物预压缩：为 dist 下的文本资源生成 .gz 与 .br 同名文件。
 * 配合 nginx `gzip_static on` 使用：直接发送预压缩文件，零运行时 CPU；
 * 没有预压缩文件时回落到 gzip 实时压缩。Node 内置 zlib，无额外依赖。
 */
import fs from 'node:fs'
import path from 'node:path'
import zlib from 'node:zlib'

const DIST = path.resolve(process.argv[2] || 'dist')
const EXT = new Set(['.js', '.mjs', '.css', '.html', '.json', '.svg', '.txt', '.xml', '.map'])
const MIN_BYTES = 1024

function walk(dir, out = []) {
  for (const name of fs.readdirSync(dir)) {
    const p = path.join(dir, name)
    const st = fs.statSync(p)
    if (st.isDirectory()) walk(p, out)
    else if (EXT.has(path.extname(p).toLowerCase()) && st.size >= MIN_BYTES) out.push({ p, size: st.size })
  }
  return out
}

if (!fs.existsSync(DIST)) {
  console.error('dist 不存在，先跑 npm run build')
  process.exit(1)
}

const files = walk(DIST)
let raw = 0
let gzTotal = 0
let brTotal = 0

for (const { p, size } of files) {
  const buf = fs.readFileSync(p)
  const gz = zlib.gzipSync(buf, { level: 9 })
  const br = zlib.brotliCompressSync(buf, {
    params: {
      [zlib.constants.BROTLI_PARAM_QUALITY]: 11,
      [zlib.constants.BROTLI_PARAM_SIZE_HINT]: buf.length,
    },
  })
  fs.writeFileSync(`${p}.gz`, gz)
  fs.writeFileSync(`${p}.br`, br)
  raw += size
  gzTotal += gz.length
  brTotal += br.length
}

const mb = (n) => `${(n / 1024).toFixed(1)} KB`
console.log(`已压缩 ${files.length} 个文件`)
console.log(`  原始   ${mb(raw)}`)
console.log(`  gzip   ${mb(gzTotal)}  (${((gzTotal / raw) * 100).toFixed(0)}%)`)
console.log(`  brotli ${mb(brTotal)}  (${((brTotal / raw) * 100).toFixed(0)}%)`)
