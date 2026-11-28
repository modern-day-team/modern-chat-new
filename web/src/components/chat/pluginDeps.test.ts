import { describe, expect, it } from 'vitest'

import type { Plugin } from '@/types/proto'
import { parseVersion, satisfies, splitDependency, sdkRequirement, unmetDeps } from './pluginDeps'

function plugin(partial: Partial<Plugin> & { name: string }): Plugin {
  return {
    id: 1,
    version: '1.0.0',
    description: '',
    permissions: [],
    allowedHosts: [],
    engines: {},
    dependencies: [],
    enabled: true,
    globalDisabled: false,
    installedBy: 1,
    createdAt: '',
    ...partial,
  }
}

describe('parseVersion', () => {
  it('补零并可带 v 前缀', () => {
    expect(parseVersion('5')).toEqual([5, 0, 0])
    expect(parseVersion('v1.2')).toEqual([1, 2, 0])
    expect(parseVersion('1.2.3')).toEqual([1, 2, 3])
  })

  it('拒绝非法写法', () => {
    expect(parseVersion('1.2.3.4')).toBeNull()
    expect(parseVersion('1.x')).toBeNull()
    expect(parseVersion('')).toBeNull()
  })
})

describe('satisfies', () => {
  it('与 Go 侧语义一致', () => {
    expect(satisfies('5.0.0', '>=5')).toBe(true)
    expect(satisfies('4.9.9', '>=5')).toBe(false)
    expect(satisfies('1.2.9', '~1.2.3')).toBe(true)
    expect(satisfies('1.3.0', '~1.2.3')).toBe(false)
    expect(satisfies('1.9.9', '^1.2.3')).toBe(true)
    expect(satisfies('2.0.0', '^1.2.3')).toBe(false)
    expect(satisfies('0.3.0', '^0.2.3')).toBe(false)
    expect(satisfies('1.2.3', '*')).toBe(true)
    expect(satisfies('1.2.3', '')).toBe(true)
  })
})

describe('splitDependency', () => {
  it('拆出名字与约束', () => {
    expect(splitDependency('word-count@^1.0.0')).toEqual({ name: 'word-count', requirement: '^1.0.0' })
    expect(splitDependency('word-count')).toEqual({ name: 'word-count', requirement: '*' })
  })
})

describe('unmetDeps', () => {
  const demo = plugin({ name: 'deps-demo', dependencies: ['word-count@^1.0.0'] })

  it('未安装', () => {
    expect(unmetDeps(demo, [])).toEqual([{ name: 'word-count', raw: 'word-count@^1.0.0', reason: 'missing' }])
  })

  it('版本不符', () => {
    const installed = [plugin({ name: 'word-count', version: '2.0.0' })]
    expect(unmetDeps(demo, installed)[0]?.reason).toBe('version')
  })

  it('已安装但停用也算未满足', () => {
    const installed = [plugin({ name: 'word-count', version: '1.1.0', enabled: false })]
    expect(unmetDeps(demo, installed)[0]?.reason).toBe('disabled')
  })

  it('满足时为空', () => {
    const installed = [plugin({ name: 'word-count', version: '1.1.0' })]
    expect(unmetDeps(demo, installed)).toEqual([])
  })
})

describe('sdkRequirement', () => {
  it('只在声明时返回值', () => {
    expect(sdkRequirement(plugin({ name: 'a' }))).toBe('')
    expect(sdkRequirement(plugin({ name: 'a', engines: { sdk: '>=5' } }))).toBe('>=5')
  })
})