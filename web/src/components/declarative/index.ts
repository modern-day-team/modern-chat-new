/**
 * 描述式 UI 白名单渲染器（§8.2）。
 * 白名单组件：text / image / button / input / textarea / checkbox / switch / segmented
 * / card / list / divider / link + 数据展示类 table / progress / badge；
 * SDK v5 UI 面追加 tabs / steps / empty / loading / upload；
 * props 仅接受原始值；链接/图片仅 http(s)（双保险，桥接层已校验过）。
 * upload 是唯一回传对象给插件的组件：对象由宿主 FileReader 产出，不是插件 DOM。
 */
import { defineComponent, h, type PropType, type VNode } from 'vue'
import {
  Button as TButton,
  Checkbox as TCheckbox,
  Divider as TDivider,
  Input as TInput,
  Switch as TSwitch,
  Textarea as TTextarea,
} from 'tdesign-vue-next'

import i18n from '@/i18n'
import type { UIDescNode } from '@/types/proto'
import DeclarativeSelect from './DeclarativeSelect.vue'

function safeProps(raw: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(raw)) {
    if (['string', 'number', 'boolean'].includes(typeof v) || v === null) out[k] = v
  }
  return out
}

function httpUrl(v: unknown, fallback: string): string {
  const s = typeof v === 'string' ? v : ''
  return /^https?:\/\//i.test(s) ? s : fallback
}

type ActionFn = (action: string, value?: unknown) => void

export const DeclarativeNodes = defineComponent({
  name: 'DeclarativeNodes',
  props: {
    nodes: { type: Array as PropType<readonly UIDescNode[]>, required: true },
    onAction: { type: Function as PropType<ActionFn>, default: undefined },
    // 嵌套深度：描述树由插件生成，card/list/tabs 无上限嵌套会把渲染递归打爆
    depth: { type: Number, default: 0 },
  },
  setup(props) {
    // 容器类节点的最大嵌套层数；合法 UI 远用不到，超限渲染占位
    const MAX_DEPTH = 8

    function renderNode(node: UIDescNode, depth: number): VNode {
      if (depth > MAX_DEPTH) {
        return h('span', { class: 'text-placeholder' }, i18n.global.t('message.rejectedComponent'))
      }
      const p = safeProps(node.props ?? {})
      const str = (k: string, d = '') => String(p[k] ?? d)

      switch (node.component) {
        case 'text':
          return h('span', { class: 'mc-dtext' }, str('content'))
        case 'image':
          return h('img', {
            class: 'mc-dimage',
            src: httpUrl(p.src, ''),
            alt: str('alt'),
          })
        case 'button':
          return h(
            TButton,
            {
              size: 'small',
              variant: 'outline',
              theme: p.theme === 'primary' ? 'primary' : 'default',
              onClick: () => props.onAction?.(String(node.action ?? '')),
            },
            { default: () => str('text', '按钮') },
          )
        case 'input':
          return h(TInput, {
            size: 'small',
            clearable: true,
            placeholder: str('placeholder'),
            value: str('value'),
            onChange: (v: unknown) => props.onAction?.(String(node.action ?? 'input'), v),
          })
        case 'select': {
          // 选项用字符串传（与 table 同款约定）：options='["甲","乙"]'，仅接受字符串数组，
          // 上限 50 项防插件塞巨列表顶爆面板
          let options: string[] = []
          try {
            const parsed: unknown = JSON.parse(str('options', '[]'))
            if (Array.isArray(parsed)) {
              options = parsed.slice(0, 50).map((o) => String(o ?? ''))
            }
          } catch {
            options = []
          }
          return h(DeclarativeSelect, {
            placeholder: str('placeholder'),
            value: str('value'),
            options: options.map((o) => ({ label: o, value: o })),
            onChange: (v: unknown) => props.onAction?.(String(node.action ?? 'select'), v === null ? '' : String(v)),
          })
        }
        case 'textarea': {
          const rows = Math.max(2, Math.min(8, Number(p.rows ?? 3) || 3))
          return h(TTextarea, {
            placeholder: str('placeholder'),
            value: str('value'),
            maxlength: Math.max(1, Math.min(4000, Number(p.maxlength ?? 1000) || 1000)),
            autosize: { minRows: rows, maxRows: rows },
            onChange: (v: unknown) => props.onAction?.(String(node.action ?? 'textarea'), v),
          })
        }
        case 'checkbox': {
          const checked = p.checked === true || p.checked === 'true'
          return h(
            TCheckbox,
            {
              checked,
              onChange: (v: unknown) => props.onAction?.(String(node.action ?? 'checkbox'), v === true),
            },
            { default: () => str('text', '选项') },
          )
        }
        case 'switch': {
          const value = p.value === true || p.value === 'true'
          return h(
            TSwitch,
            {
              value,
              onChange: (v: unknown) => props.onAction?.(String(node.action ?? 'switch'), v === true),
            },
            str('text') ? { label: () => str('text') } : undefined,
          )
        }
        case 'segmented': {
          // options='["甲","乙"]'；最多 8 项，避免插件把面板挤爆
          let options: string[] = []
          try {
            const parsed: unknown = JSON.parse(str('options', '[]'))
            if (Array.isArray(parsed)) options = parsed.slice(0, 8).map((o) => String(o ?? ''))
          } catch {
            options = []
          }
          const current = str('value')
          return h(
            'div',
            { class: 'mc-dsegmented', role: 'group' },
            options.map((o) =>
              h(
                'button',
                {
                  type: 'button',
                  class: ['mc-dsegmented__item', o === current ? 'is-active' : ''],
                  onClick: () => props.onAction?.(String(node.action ?? 'segmented'), o),
                },
                o,
              ),
            ),
          )
        }
        case 'card':
          return h(
            'div',
            { class: 'mc-dcard' },
            h(DeclarativeNodes, { nodes: node.children ?? [], onAction: props.onAction, depth: depth + 1 }),
          )
        case 'list':
          return h(
            'ul',
            { class: 'mc-dlist' },
            (node.children ?? []).map((c, i) => h('li', { key: i }, [renderNode(c, depth + 1)])),
          )
        case 'divider':
          return h(TDivider, { style: { margin: '6px 0' } })
        case 'link':
          return h(
            'a',
            {
              href: httpUrl(p.href, '#'),
              target: '_blank',
              rel: 'noopener noreferrer',
            },
            str('text', str('href', '链接')),
          )
        case 'badge': {
          const tone = ['brand', 'success', 'warning', 'danger'].includes(str('tone')) ? str('tone') : 'brand'
          return h('span', { class: `mc-dbadge mc-dbadge--${tone}` }, str('text'))
        }
        case 'progress': {
          const raw = Number(p.value)
          const value = Number.isFinite(raw) ? Math.min(100, Math.max(0, raw)) : 0
          return h('div', { class: 'mc-dprogress' }, [
            h('div', { class: 'mc-dprogress__head' }, [
              h('span', { class: 'mc-dprogress__label' }, str('label')),
              h('span', { class: 'mc-dprogress__value num' }, `${value}%`),
            ]),
            h('div', { class: 'mc-dprogress__track' }, [
              h('div', { class: 'mc-dprogress__bar', style: { width: `${value}%` } }),
            ]),
          ])
        }
        case 'table': {
          // 表格数据用字符串传：headers="A,B"、rows='[["a","1"],["b","2"]]'
          // 只接受原始值，行/列数量设上限，避免插件塞一张巨表把面板顶爆
          const headers = str('headers')
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean)
            .slice(0, 6)
          let rows: string[][] = []
          try {
            const parsed: unknown = JSON.parse(str('rows', '[]'))
            if (Array.isArray(parsed)) {
              rows = parsed
                .slice(0, 50)
                .map((r) =>
                  Array.isArray(r)
                    ? r.slice(0, headers.length || 6).map((c) => String(c ?? ''))
                    : [String(r ?? '')],
                )
            }
          } catch {
            rows = []
          }
          return h('table', { class: 'mc-dtable' }, [
            headers.length
              ? h('thead', {}, [
                  h('tr', {}, headers.map((c, i) => h('th', { key: i }, c))),
                ])
              : null,
            h(
              'tbody',
              {},
              rows.map((r, ri) => h('tr', { key: ri }, r.map((c, ci) => h('td', { key: ci }, c)))),
            ),
          ])
        }
        case 'tabs': {
          // labels='["A","B"]'（≤8 项），active 为索引；只渲染当前子节点的描述树。
          // 动作回传索引，插件据此更新 active 后重发面板/页面描述。
          let labels: string[] = []
          try {
            const parsed: unknown = JSON.parse(str('labels', '[]'))
            if (Array.isArray(parsed)) labels = parsed.slice(0, 8).map((l) => String(l ?? ''))
          } catch {
            labels = []
          }
          const rawActive = Number(p.active ?? 0)
          const active = Number.isFinite(rawActive)
            ? Math.max(0, Math.min(labels.length - 1, Math.trunc(rawActive)))
            : 0
          const children = node.children ?? []
          const action = String(node.action ?? 'tabs')
          return h('div', { class: 'mc-dtabs' }, [
            h(
              'div',
              { class: 'mc-dtabs__bar', role: 'tablist' },
              labels.map((label, i) =>
                h(
                  'button',
                  {
                    key: i,
                    type: 'button',
                    role: 'tab',
                    'aria-selected': i === active,
                    class: ['mc-dtabs__item', i === active ? 'is-active' : ''],
                    onClick: () => props.onAction?.(action, i),
                  },
                  label,
                ),
              ),
            ),
            h('div', { class: 'mc-dtabs__body' }, children[active] ? [renderNode(children[active]!, depth + 1)] : []),
          ])
        }
        case 'steps': {
          // items='["A","B"]'（≤8 项），active 为当前步骤序号；展示型组件，不回传动作。
          let items: string[] = []
          try {
            const parsed: unknown = JSON.parse(str('items', '[]'))
            if (Array.isArray(parsed)) items = parsed.slice(0, 8).map((i) => String(i ?? ''))
          } catch {
            items = []
          }
          const rawActive = Number(p.active ?? 0)
          const active = Number.isFinite(rawActive)
            ? Math.max(0, Math.min(items.length - 1, Math.trunc(rawActive)))
            : 0
          return h(
            'ol',
            { class: 'mc-dsteps' },
            items.map((label, i) =>
              h(
                'li',
                { key: i, class: ['mc-dsteps__item', i === active ? 'is-active' : '', i < active ? 'is-done' : ''] },
                [h('span', { class: 'mc-dsteps__dot num' }, String(i + 1)), h('span', { class: 'mc-dsteps__label' }, label)],
              ),
            ),
          )
        }
        case 'empty':
          return h('div', { class: 'mc-dempty' }, [
            str('icon') ? h('span', { class: 'mc-dempty__icon' }, str('icon')) : null,
            h('span', { class: 'mc-dempty__text' }, str('text', '暂无内容')),
          ])
        case 'loading':
          return h('div', { class: 'mc-dloading', role: 'status' }, [
            h('span', { class: 'mc-dloading__spinner', 'aria-hidden': 'true' }),
            h('span', { class: 'mc-dloading__text' }, str('text', '加载中…')),
          ])
        case 'upload': {
          // 唯一允许回传对象的组件：文件字节由宿主 FileReader 读取，插件拿不到 DOM 节点。
          // accept 仅为原生 input 属性；maxSize 默认 256KB、硬上限 1MB，超限走 action:error。
          const accept = String(p.accept ?? '*').slice(0, 200)
          const rawMax = Number(p.maxSize ?? 262144)
          const maxSize = Number.isFinite(rawMax) && rawMax > 0 ? Math.min(1048576, Math.trunc(rawMax)) : 262144
          const action = String(node.action ?? 'upload')
          return h('div', { class: 'mc-dupload' }, [
            h('input', {
              type: 'file',
              class: 'mc-dupload__input',
              accept,
              onChange: (ev: Event) => {
                const input = ev.target as HTMLInputElement
                const file = input.files?.[0]
                // 清空 value：同一文件再次选择也要触发 change
                input.value = ''
                if (!file) return
                if (file.size > maxSize) {
                  props.onAction?.(`${action}:error`, { message: `文件超过 ${Math.round(maxSize / 1024)} KB` })
                  return
                }
                const reader = new FileReader()
                reader.onload = () => {
                  props.onAction?.(action, {
                    name: file.name,
                    size: file.size,
                    type: file.type,
                    dataUrl: String(reader.result ?? ''),
                  })
                }
                reader.onerror = () => props.onAction?.(`${action}:error`, { message: '文件读取失败' })
                reader.readAsDataURL(file)
              },
            }),
            h('span', { class: 'mc-dupload__hint num' }, `≤ ${Math.round(maxSize / 1024)} KB`),
          ])
        }
        default:
          // 白名单外：拒绝渲染
          return h('span', { class: 'text-placeholder' }, i18n.global.t('message.rejectedComponent'))
      }
    }

    return () => h('div', { class: 'mc-declarative' }, props.nodes.map((n) => renderNode(n, props.depth)))
  },
})

export default DeclarativeNodes
