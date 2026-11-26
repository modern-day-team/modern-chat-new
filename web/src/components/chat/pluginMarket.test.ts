import { describe, expect, it } from 'vitest'

import { isDowngrade, readPluginInstallQuery } from './pluginMarket'

describe('readPluginInstallQuery', () => {
  it('正常深链：取 URL 与可选名称', () => {
    expect(readPluginInstallQuery({ 'plugin-install': 'https://x/a.js', 'plugin-name': 'weather' })).toEqual({
      url: 'https://x/a.js',
      name: 'weather',
    })
    expect(readPluginInstallQuery({ 'plugin-install': 'https://x/a.js' })).toEqual({
      url: 'https://x/a.js',
      name: '',
    })
  })

  it('缺失 / 空串 / 数组与其它类型一律忽略', () => {
    expect(readPluginInstallQuery({})).toBe(null)
    expect(readPluginInstallQuery({ 'plugin-install': '' })).toBe(null)
    expect(readPluginInstallQuery({ 'plugin-install': '   ' })).toBe(null)
    // 同名参数重复时 vue-router 给数组：不认，防畸形链接
    expect(readPluginInstallQuery({ 'plugin-install': ['https://x/a.js', 'https://y/b.js'] })).toBe(null)
    expect(readPluginInstallQuery({ 'plugin-install': 42 })).toBe(null)
    // 名称非字符串（数组）时只取 URL
    expect(readPluginInstallQuery({ 'plugin-install': 'https://x/a.js', 'plugin-name': ['a'] })).toEqual({
      url: 'https://x/a.js',
      name: '',
    })
  })
})

describe('isDowngrade', () => {
  it('old 大于 new 为降级；小于与相等都不是', () => {
    expect(isDowngrade('2.0.0', '1.9.9')).toBe(true)
    expect(isDowngrade('1.2.0', '1.1.99')).toBe(true)
    expect(isDowngrade('1.0.1', '1.0.0')).toBe(true)
    expect(isDowngrade('1.0.0', '1.0.1')).toBe(false)
    expect(isDowngrade('1.0.0', '1.0.0')).toBe(false)
  })
  it('缺失段按 0 补齐', () => {
    expect(isDowngrade('1.1', '1.0.9')).toBe(true)
    expect(isDowngrade('1', '1.0.0')).toBe(false)
    expect(isDowngrade('1.0', '1.0.1')).toBe(false)
  })
})
