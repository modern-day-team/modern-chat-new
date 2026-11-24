// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest'
import { createApp, h, nextTick } from 'vue'

import UiPopup from './UiPopup.vue'

let mountedApp: ReturnType<typeof createApp> | null = null

function mountPopup(props: Record<string, unknown> = {}) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  mountedApp = createApp({
    render: () =>
      h(
        UiPopup,
        props,
        {
          default: () => h('button', { id: 'trigger' }, 'open'),
          content: () => h('div', { class: 'popup-content' }, 'content'),
        },
      ),
  })
  mountedApp.mount(host)
}

afterEach(() => {
  mountedApp?.unmount()
  mountedApp = null
  document.body.innerHTML = ''
})

describe('UiPopup', () => {
  it('非受控模式点击触发元素后打开面板', async () => {
    mountPopup()
    ;(document.querySelector('#trigger') as HTMLElement).dispatchEvent(
      new MouseEvent('click', { bubbles: true }),
    )
    await nextTick()
    expect(document.querySelector('.ui-popup__panel')).not.toBeNull()
    expect(document.querySelector('.popup-content')?.textContent).toBe('content')
  })

  it('受控模式 visible=false 时点击不应打开面板', async () => {
    mountPopup({ visible: false })
    ;(document.querySelector('#trigger') as HTMLElement).dispatchEvent(
      new MouseEvent('click', { bubbles: true }),
    )
    await nextTick()
    expect(document.querySelector('.ui-popup__panel')).toBeNull()
  })
})
