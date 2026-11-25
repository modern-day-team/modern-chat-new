/**
 * 权限表不变量：SDK 里每一个 call("...") 都必须在 API_PERMISSION 登记。
 * 漏登记 = 该 API 对任何插件都返回 permission denied（功能性回归）；
 * 这类回归在宿主分发逻辑里不可见，只能在表上验。
 */
import { describe, expect, it } from 'vitest'

import { bootHtml } from './sandbox'
import { API_PERMISSION, EVENT_PERMISSION } from './permissions'

describe('API_PERMISSION 完整性', () => {
  it('bootHtml SDK 中每个 call("...") 都已登记权限', () => {
    const html = bootHtml('', [], 'tk', 'https://app.example')
    // SDK v3 的 api 名含大写（nav.openPluginPage），字符类必须允许大写
    const calls = [...html.matchAll(/call\("([a-zA-Z.]+)"/g)].map((m) => m[1] ?? '')
    expect(calls.length).toBeGreaterThan(10) // SDK 面非空，防止正则失效导致测试空转
    const missing = [...new Set(calls)].filter((api) => !(api in API_PERMISSION))
    expect(missing).toEqual([])
  })

  it('权限值均为已知权限名（防手滑写出不存在的权限）', () => {
    const known = new Set([
      'message:read',
      'message:send',
      'message:render',
      'ui:panel',
      'ui:actions',
      'ui:host',
      'conv:read',
      'storage',
      'net',
      'ui:theme',
      'ui:page',
      'data',
      'ai',
      // SDK v5
      'conv:write',
      'file:upload',
    ])
    const bad = Object.entries(API_PERMISSION)
      .concat(Object.entries(EVENT_PERMISSION))
      .filter(([, perm]) => !known.has(perm))
      .map(([api, perm]) => `${api} -> ${perm}`)
    expect(bad).toEqual([])
  })

  it('SDK v2 新增 API 权限门槛符合数据敏感度', () => {
    expect(API_PERMISSION['conv.list']).toBe('conv:read')
    expect(API_PERMISSION['messages.search']).toBe('message:read')
    expect(API_PERMISSION['draft.get']).toBe('ui:host')
    expect(API_PERMISSION['draft.set']).toBe('ui:host')
    expect(API_PERMISSION['clipboard.writeText']).toBe('ui:host')
    expect(API_PERMISSION['storage.remove']).toBe('storage')
    expect(API_PERMISSION['storage.keys']).toBe('storage')
  })

  it('SDK v3 新增 API 权限门槛：页面注册需 ui:page，用户数据需 data', () => {
    expect(API_PERMISSION['addPage']).toBe('ui:page')
    expect(API_PERMISSION['removePage']).toBe('ui:page')
    expect(API_PERMISSION['setPage']).toBe('ui:page')
    expect(API_PERMISSION['data.get']).toBe('data')
    expect(API_PERMISSION['data.set']).toBe('data')
    expect(API_PERMISSION['data.remove']).toBe('data')
    expect(API_PERMISSION['data.keys']).toBe('data')
    // 跳页面是宿主 UI 动作，沿用 ui:host，不新造权限名
    expect(API_PERMISSION['nav.openPluginPage']).toBe('ui:host')
  })

  it('SDK v4 AI 代理需要 ai 权限', () => {
    expect(API_PERMISSION['ai.status']).toBe('ai')
    expect(API_PERMISSION['ai.chat']).toBe('ai')
  })

  it('SDK v5：会话写操作需 conv:write，消息历史仍需 conv:read', () => {
    for (const api of [
      'conv.createSingle',
      'conv.createGroup',
      'conv.rename',
      'conv.invite',
      'conv.leave',
      'conv.join',
      'conv.setPrefs',
      'conv.remove',
      'conv.clear',
      'conv.files',
      'conv.addFile',
      'conv.removeFile',
      'conv.setAnnouncement',
      'conv.kick',
      'conv.setAdmin',
      'conv.mute',
      'conv.transfer',
      'conv.createInviteLink',
    ]) {
      expect(API_PERMISSION[api]).toBe('conv:write')
    }
    expect(API_PERMISSION['message.history']).toBe('conv:read')
  })

  it('SDK v5：消息写动作按 message:send 收敛，账号关系复用 ui:host，上传独立权限', () => {
    for (const api of [
      'message.delete',
      'message.recall',
      'message.edit',
      'message.react',
      'message.favorite',
      'message.report',
    ]) {
      expect(API_PERMISSION[api]).toBe('message:send')
    }
    for (const api of ['users.blockStatus', 'users.block', 'users.unblock']) {
      expect(API_PERMISSION[api]).toBe('ui:host')
    }
    expect(API_PERMISSION['file.upload']).toBe('file:upload')
  })

  it('on 不进 API_PERMISSION：api.on 是沙箱内本地注册，不走 RPC', () => {
    expect('on' in API_PERMISSION).toBe(false)
  })
})

describe('EVENT_PERMISSION 门槛', () => {
  it('含正文/用户列表的事件必须 message:read', () => {
    expect(EVENT_PERMISSION['message:new']).toBe('message:read')
    expect(EVENT_PERMISSION['message:recall']).toBe('message:read')
    expect(EVENT_PERMISSION['presence']).toBe('message:read')
  })

  it('面板/右键动作按对应 UI 权限收敛，不要求 message:read', () => {
    // 纯面板插件（只有 ui:panel）必须能收到自己的面板动作——
    // 这是不引入 message:read 的前提；右键动作同理归 ui:actions
    expect(EVENT_PERMISSION['ui:action']).toBe('ui:panel')
    expect(EVENT_PERMISSION['message:action']).toBe('ui:actions')
  })

  it('conv:open 故意不设门槛（payload 只有 convId）', () => {
    expect('conv:open' in EVENT_PERMISSION).toBe(false)
  })
})
