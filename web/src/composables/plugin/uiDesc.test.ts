/**
 * 声明式 UI 校验的回归测试：这是宿主渲染前唯一的安全闸门，
 * 白名单 / 属性注入 / 链接协议的每一个拒绝路径都不能被悄悄放宽。
 */
import { describe, expect, it } from 'vitest'

import { validatePage, validatePanel, validateRenderDesc, validateUIDesc } from './uiDesc'

describe('validateUIDesc 白名单', () => {
  it('白名单内组件且 props 合法时通过', () => {
    const node = validateUIDesc({ component: 'text', props: { content: 'hi' } })
    expect(node?.component).toBe('text')
    expect(node?.props.content).toBe('hi')
  })

  it('SDK v2 交互控件进入白名单，且布尔 props 保留', () => {
    for (const component of ['textarea', 'checkbox', 'switch', 'segmented']) {
      const node = validateUIDesc({ component, props: { value: true, checked: true, options: '["a","b"]' }, action: 'change' })
      expect(node?.component).toBe(component)
      expect(node?.props.checked).toBe(true)
    }
  })

  it('SDK v5 UI 面组件进入白名单；upload 的 accept/maxSize 保留原始值，对象 props 仍被丢弃', () => {
    for (const component of ['tabs', 'steps', 'empty', 'loading', 'upload']) {
      const node = validateUIDesc({
        component,
        props: { labels: '["A","B"]', items: '["A"]', text: 'x', accept: 'image/*', maxSize: 262144 },
        action: 'pick',
      })
      expect(node?.component).toBe(component)
    }
    const upload = validateUIDesc({
      component: 'upload',
      props: { accept: 'image/*', maxSize: 262144, evil: { a: 1 } },
    })
    expect(upload?.props.accept).toBe('image/*')
    expect(upload?.props.maxSize).toBe(262144)
    expect(upload?.props).not.toHaveProperty('evil')
  })

  it('白名单外组件（如 script）拒绝', () => {
    expect(validateUIDesc({ component: 'script', props: { content: 'x' } })).toBeNull()
    expect(validateUIDesc({ component: 'iframe', props: {} })).toBeNull()
  })

  it('link 仅放行 http(s)，javascript: 一律拒绝', () => {
    expect(validateUIDesc({ component: 'link', props: { href: 'javascript:alert(1)' } })).toBeNull()
    expect(validateUIDesc({ component: 'link', props: { href: 'data:text/html,x' } })).toBeNull()
    expect(validateUIDesc({ component: 'link', props: { href: 'https://a.example/x' } })?.props.href).toBe(
      'https://a.example/x',
    )
  })

  it('image src 仅放行 http(s)', () => {
    expect(validateUIDesc({ component: 'image', props: { src: 'file:///etc/passwd' } })).toBeNull()
    expect(validateUIDesc({ component: 'image', props: { src: 'https://a.example/i.png' } })).not.toBeNull()
  })

  it('props 含对象/数组时被过滤丢弃（杜绝注入面，不影响其余合法字段）', () => {
    const node = validateUIDesc({
      component: 'text',
      props: { content: 'ok', evil: { a: 1 }, evil2: [1, 2] },
    })
    expect(node?.props).toEqual({ content: 'ok' })
    expect(validateUIDesc({ component: 'text', props: { content: { a: 1 } } })?.props).toEqual({})
  })

  it('props 缺省时放行（list / divider 纯结构节点）', () => {
    expect(validateUIDesc({ component: 'divider' })).not.toBeNull()
    expect(validateUIDesc({ component: 'list', children: [{ component: 'divider' }] })).not.toBeNull()
  })

  it('嵌套深度超过 8 层被截断（渲染有界，防栈耗尽）', () => {
    let node: Record<string, unknown> = { component: 'divider' }
    for (let i = 0; i < 12; i++) node = { component: 'list', children: [node] }
    let depth = 0
    let cur: { children?: unknown[] } | null = validateUIDesc(node)
    while (cur && (cur.children as unknown[] | undefined)?.length) {
      depth++
      cur = cur.children![0] as never
    }
    expect(depth).toBeLessThanOrEqual(8)
  })
})

describe('validatePanel / validateRenderDesc', () => {
  it('body 非数组或含非法节点时整体拒绝', () => {
    expect(validatePanel({ title: 't', body: 'x' })).toBeNull()
    expect(validatePanel({ body: [{ component: 'script' }] })).toBeNull()
    expect(validatePanel({ body: [{ component: 'text', props: { content: 'ok' } }] })?.title).toBe('')
  })

  it('单节点渲染结果包成面板；面板形状直接走 validatePanel', () => {
    const single = validateRenderDesc({ component: 'text', props: { content: 'hi' } })
    expect(single?.body).toHaveLength(1)
    const panel = validateRenderDesc({ title: 'T', body: [{ component: 'divider' }] })
    expect(panel?.title).toBe('T')
  })
})

describe('validatePage（SDK v3）', () => {
  const valid = {
    id: 'my_page-1',
    title: '页面工具箱',
    icon: '页',
    scope: 'user',
    body: [{ component: 'text', props: { content: 'hi' } }],
  }

  it('合法页面通过；scope 缺省时归一为 user', () => {
    const page = validatePage(valid)
    expect(page?.id).toBe('my_page-1')
    expect(page?.title).toBe('页面工具箱')
    expect(page?.scope).toBe('user')
    expect(page?.body).toHaveLength(1)
    expect(validatePage({ id: 'x', title: 't', body: [] })?.scope).toBe('user')
  })

  it('id 格式、标题长度、scope 取值均严格校验', () => {
    expect(validatePage({ ...valid, id: 'Bad ID!' })).toBeNull()
    expect(validatePage({ ...valid, id: '' })).toBeNull()
    expect(validatePage({ ...valid, title: '' })).toBeNull()
    expect(validatePage({ ...valid, title: 'x'.repeat(41) })).toBeNull()
    expect(validatePage({ ...valid, scope: 'superuser' })).toBeNull()
    expect(validatePage({ ...valid, scope: 'admin' })?.scope).toBe('admin')
  })

  it('body 复用面板白名单：非法节点整体拒绝', () => {
    expect(validatePage({ ...valid, body: [{ component: 'script', props: {} }] })).toBeNull()
    expect(validatePage({ ...valid, body: 'x' })).toBeNull()
  })

  it('节点总量上限 100（含嵌套子节点）', () => {
    const body = Array.from({ length: 101 }, () => ({ component: 'divider', props: {} }))
    expect(validatePage({ ...valid, body })).toBeNull()
    expect(validatePage({ ...valid, body: body.slice(0, 100) })).not.toBeNull()
    const nested = [{ component: 'list', props: {}, children: body.slice(0, 100) }]
    expect(validatePage({ ...valid, body: nested })).toBeNull()
  })
})
