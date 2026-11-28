# 插件开发指南

> 面向**写插件**的人：怎么打包、有哪些能力、权限怎么申明、UI 怎么描述。
> 架构取舍见 [设计文档.md](设计文档.md) §8，接口契约见 [后端需求规格.md](后端需求规格.md)。

ModernChat 有两层插件：

| 层级 | 安装者 | 作用范围 | 形态 |
|---|---|---|---|
| 用户插件 | 每个用户自己 | 只影响该用户 | 单文件 JS / TS，运行在浏览器沙箱 |
| 服务端插件 | 管理员 | 全站生效 | JSON manifest + 可选 WASM 二进制，运行在服务端沙箱 |

用户插件适合“我的页面 / 我的工具 / 消息增强”；服务端插件适合“全站页面 / API / 事件 / 定时任务 / 扫码登录”。两者都使用同一套描述式 UI 白名单，插件不能注入 HTML 或任意 CSS。服务端插件默认使用静态 manifest；需要自定义逻辑时切换 `runtime: "wasm"`，WASM 只能通过宿主允许的 KV、受控 HTTP、AI 与扫码 API 访问外部世界。

---

# 第一部分：用户插件

## 1. 插件是什么

一个插件 = **一个文件**。文件顶部是一段 `/* {...} */` JSON 注释（manifest），下面是你自己的代码。

- 语言：**JavaScript 或 TypeScript 均可**。上传 `.ts` 时服务端用 esbuild 去掉类型后入库，**不需要本地构建**（仍是单文件模型，不解析 `import`）
- 体积：≤ 1 MB（`PLUGIN_MAX_SIZE`）
- 入口：定义全局函数 `function __mcActivate(api) { ... }`，主站注入 `api` 后调用
- 运行：跑在 `sandbox="allow-scripts"` 的 iframe 里，**碰不到主站 DOM、window、JWT**，所有能力经 `postMessage` 桥接

```ts
/// <reference path="../modernchat-sdk.d.ts" />
/* {
  "name": "hello",
  "version": "1.0.0",
  "description": "打个招呼",
  "permissions": ["ui:panel", "ui:host"],
  "allowedHosts": []
} */

function __mcActivate(api: McApi): void {
  void api.addSidePanel({
    title: '你好',
    body: [{ component: 'text', props: { content: '这是面板内容' } }],
  })
  void api.toast('插件已启用', 'success')
}
```

## 2. manifest 字段

| 字段 | 要求 |
|---|---|
| `name` | 1–30 字符，**同名再次安装 = 升级**（复用记录、保留启停与下架状态） |
| `version` | `x.y.z` |
| `description` | ≤255 字符 |
| `permissions` | 非空，取值见 §3 |
| `allowedHosts` | ≤20 项，`example.com` 或 `*.example.com`；`api.request` 只放行这里列出的域名，同时会写进沙箱 CSP 的 `connect-src`（未声明时插件完全不能发起网络请求）。**媒体播放同源同表**：写进 CSP `media-src`，插件可用 `<audio>` / `<video>` 播放这些域名下的媒体（见 §3.1） |
| `engines` | 可选，宿主能力要求。当前只支持 `{"sdk": ">=5"}`：**安装时**校验，宿主 SDK 不满足直接 400。未知键报错（拼错的键会让作者误以为检查生效） |
| `dependencies` | 可选，≤10 项，写法 `"name"` 或 `"name@^1.0.0"`。依赖名的字符集是 `[A-Za-z0-9_.-]`，不能自依赖或重复。**安装不检查，启用才检查**（见下） |

> **为什么依赖只在启用时拦**：一次导入多个插件时，使用者可能先落库、被依赖者后装；安装阶段就拦会让「批量导入」没法做。启用时则要求依赖**已安装 + 版本符合 + 已启用**三者齐备——装了但停用的依赖同样提供不了能力，放过去只会在运行时炸。服务端返回 403 并列出具体原因，前端抽屉在点开关前就会标出「依赖未就绪」。
>
> `engines` 与 `dependencies` 在用户插件和服务端插件里**同一套语法**（共用 `plugin.ParseDependencies` / `ParseRequirement`），服务端插件的依赖指向其它服务端插件。版本约束只实现 `*` `=` `>` `>=` `<` `<=` `~` `^` 七种，语义与 npm 一致（`^0.x` 锁次版本）；不支持预发布版本与 `||`。

安装后插件**默认停用**，前端会弹权限清单，用户确认后才开始启用运行。升级（同名覆盖）时如果**权限或 `allowedHosts` 与已授权版本不同**，服务端会带 `X-Plugin-Perms-Changed: true`，前端强制重新确认：确认后新版本生效，拒绝则插件立即停用（新代码不会在旧授权下自动运行）。站长可全局下架（对所有用户立即生效）。

> 注意：`allowedHosts` 只控制“能去哪些域名”，不等于授权。真正发请求还需要 `net` 权限；审计时两者都要看。

## 3. 权限

| 权限 | 放行的能力 |
|---|---|
| `message:read` | `api.on(...)` 订阅事件 |
| `message:send` | `api.send()` 发消息；`api.message.recall/edit/react/favorite/report/delete` 修改消息 |
| `message:render` | `api.registerMessageRenderer()` 自定义消息类型渲染 |
| `ui:panel` | `api.addSidePanel()` 侧边栏面板 |
| `ui:actions` | `api.registerMessageAction()` 消息右键菜单项 |
| `conv:read` | `api.me()` / `api.conv.*` / `api.messages.recent/history()` 只读数据 |
| `conv:write` | `api.conv.createSingle/createGroup/rename/invite/...` 会话与群管理 |
| `ui:host` | `api.nav.*` / `api.toast()` / `api.users.block*` 宿主侧动作 |
| `file:upload` | `api.file.upload()`，把描述式 `upload` 控件读到的文件传到站内存储 |
| `ui:page` | `api.addPage()` / `api.setPage()` / `api.removePage()` 注册完整插件页面 |
| `data` | `api.data.get/set/remove/keys`，跟随账号的用户级服务端数据（单值 ≤64KB） |
| `ai` | `api.ai.status/chat`，走服务端统一 AI 代理（密钥只在服务端） |
| `storage` | `api.storage.get/set`（每插件独立命名空间，单值 ≤64KB、总量 ≤256KB） |
| `net` | `api.request()`（还受 `allowedHosts` 限制） |
| `ui:theme` | `api.theme.apply()`（仅 `--td-*` 令牌，防任意 CSS 注入） |

权限不只是“能调哪个方法”，还会过滤事件：

- `message:new` / `message:recall` / `presence` 的 payload 含消息正文或用户列表，**只投递给声明 `message:read` 的插件**；
- `ui:actions` 的右键菜单回调如果没有 `message:read`，只拿到消息元信息（`id/convId/senderId/msgType/createdAt/isRecalled`），拿不到正文；
- `message:action` 只投递给注册了该动作的插件，不广播。

> **依赖裸 `fetch` 的插件已失效，必须改用 `mc.request`。**
> 仓库注入的 SDK 对象名是 `api`，所以代码里写 `api.request(url, opts)`（`McApi` 类型）；`mc.request` 指的就是这个方法。
> 原因：沙箱 iframe 没有 `allow-same-origin`，origin 是 opaque（`Origin: null`），目标站点的 CORS 通常不放行；同时 CSP `connect-src` 只允许 `allowedHosts`，未声明时是 `'none'`。

### 3.1 媒体能力（内核通用，无业务语义）

插件可以在自己的沙箱文档里创建 `<audio>` / `<video>` 播放媒体——内核不提供任何音乐/播放器业务 API，
「音乐播放器」这类功能完全由插件自己实现（曲库、搜索 UI、播放列表都走插件，内核只保证"沙箱能出声"）：

- **来源白名单**：CSP `media-src` 与 `connect-src` 同源，都是插件 `allowedHosts`；未声明 `allowedHosts` 的插件不能播放任何媒体。`blob:` 也在放行之列（插件可先 `api.request` 拉数据再 `URL.createObjectURL` 边下边播）；
- **自动播放委托**：宿主挂载沙箱时带 `allow="autoplay"`——播放按钮在宿主渲染的声明式面板里（用户手势在宿主文档），跨文档没有这个委托，浏览器会拦下沙箱帧的播放；
- **能力边界**：沙箱无 same-origin，读不到跨域媒体字节——**不可缓存、不可解码波形/精确 seek**，只能顺序播放与自带 UI；需要这些高级能力时用 `api.request`（受 CORS 约束）自行实现；
- 播放状态归插件自己管理（插件 JS 直接持有 `<audio>` 元素）；切会话/关抽屉不影响播放，离开聊天页插件运行时卸载、播放停止。

### 3.2 内核加固（宿主↔沙箱通道）

- 沙箱 → 宿主的 `postMessage` 目标 origin 收紧为宿主真实 origin（此前 `*`）：插件自导航到外部站点后，旧文档的消息不再送达宿主；
- iframe 带 `referrerpolicy="no-referrer"`：第三方媒体服务器拿不到站点 URL；
- `api.request` 响应体上限 2MB（超限报错中止），`registerMessageAction` 每插件最多 10 个，`storage` 每插件总量 256KB——三条都是防单插件拖垮宿主的上限。

示例：在面板按钮的 action 里播放

```js
const audio = new Audio()
api.addSidePanel({ /* … 声明式 UI，按钮 action: 'play' */ })
api.on('ui:action', ({ action }) => {
  if (action === 'play') {
    audio.src = 'https://media.example.com/song.mp3' // 必须在 allowedHosts 里
    audio.play()
  }
})
```

## 4. SDK 一览

SDK 当前为 **v5**：`api.version === '5'` 时可用完整页面、账号数据、AI 代理、会话/消息管理、用户关系和文件上传；v1–v4 API 全部保留，旧插件无需修改。

| API | 作用 | 权限 |
|---|---|---|
| `api.on(event, fn)` | 监听事件（门槛按事件而定，见 §5） | 按事件（§5） |
| `api.send(convId, payload, msgType?)` | 发消息，`payload` 非字符串时自动 JSON 序列化 | `message:send` |
| `api.registerMessageRenderer(type, fn)` | 注册自定义消息渲染，`fn` 返回 UI 描述 | `message:render` |
| `api.addSidePanel(desc)` | 侧边栏面板（详情面板底部），再次调用即更新内容 | `ui:panel` |
| `api.removeSidePanel()` | 收起自己的面板（重新展示用 addSidePanel） | `ui:panel` |
| `api.version` | 宿主 API 级别（常量，新增 SDK 能力时递增），插件据此做特性检测 | 免权限 |
| `api.registerMessageAction({ id, label })` | 消息右键菜单项，`label` ≤16 字 | `ui:actions` |
| `api.me()` | 当前用户 `{ id, nickname }` | `conv:read` |
| `api.conv.current()` | 当前会话 `{ id, type, name, peerNickname, memberCount }` | `conv:read` |
| `api.conv.members()` | 成员 `[{ userId, nickname, role }]` | `conv:read` |
| `api.conv.list()` | 我的会话列表摘要（≤50 条；不含未读 / 消息预览 / 头像等隐私字段） | `conv:read` |
| `api.messages.recent(n?)` | 最近 n 条（1–50，默认 20）消息摘要 | `conv:read` |
| `api.messages.search(kw, opts?)` | 全局消息搜索；`kw` ≤50 字，`limit` 1–50（默认 20），`opts.convId` 可限定会话；命中含会话名 / 发送者 / 正文 / 时间 | `message:read` |
| `api.users.search(kw)` | 站内用户目录搜索（≤20 条，只含 id/username/nickname/avatarColor，**不含 email**） | `conv:read` |
| `api.draft.get()` / `api.draft.set(text)` | 读 / 写当前会话输入框草稿，≤8192 字；写入后输入区立即同步；正在编辑消息时写入被拒绝 | `ui:host` |
| `api.clipboard.writeText(text)` | 写剪贴板，≤8192 字；不开放读取，需 HTTPS / localhost 等安全上下文 | `ui:host` |
| `api.nav.openConversation(id)` | 切换到指定会话 | `ui:host` |
| `api.nav.jumpToMessage(id)` | 在当前会话定位并高亮某条消息，返回是否找到 | `ui:host` |
| `api.toast(text, kind?)` | 轻提示，`kind`: info/success/warning/error | `ui:host` |
| `api.storage.get/set` | 独立命名空间 KV（单值 ≤64KB） | `storage` |
| `api.storage.remove(key)` / `api.storage.keys()` | 删除自己的存储键 / 列出自己的键（剥离命名空间前缀，≤500 个） | `storage` |
| `api.addPage(desc)` / `setPage(desc)` / `removePage(id)` | 注册完整插件页面（标题、图标、描述式 UI 页面体），同名 id 覆盖 | `ui:page` |
| `api.nav.openPluginPage(pluginId, pageId)` | 打开用户插件页面路由 | `ui:host` |
| `api.data.get(key)` / `set(key, value)` / `remove(key)` / `keys()` | 跟随账号的服务端 KV，换设备/清浏览器缓存仍保留 | `data` |
| `api.ai.status()` / `api.ai.chat(messages)` | 服务端统一 AI 代理；非流式，messages 1–40 条、上下文 ≤96KB | `ai` |
| `api.conv.createSingle(userId)` / `createGroup(name)` | 创建单聊 / 群聊 | `conv:write` |
| `api.conv.rename/invite/leave/join/setPrefs/remove/clear` | 会话与成员管理 | `conv:write` |
| `api.conv.files/addFile/removeFile/setAnnouncement/kick/setAdmin/mute/transfer/createInviteLink` | 群文件、公告与群管理 | `conv:write` |
| `api.message.history(convId, opts?)` | 分页读取消息历史（≤100 条） | `conv:read` |
| `api.message.delete(convId, messageId)` / `recall(id)` / `edit(id, text)` / `react(id, emoji, remove?)` | 删除 / 撤回 / 编辑 / reaction | `message:send` |
| `api.message.favorite(id, favorite?)` / `report(id, reason)` | 收藏 / 举报消息 | `message:send` |
| `api.users.blockStatus/block/unblock(userId)` | 查询与维护屏蔽关系 | `ui:host` |
| `api.file.upload(kind, dataUrl, filename)` | 把 `upload` 控件读到的数据上传到站内存储，返回 `{url,size,kind}`；≤1MB | `file:upload` |
| `api.request(url, opts)` | 主站代发请求（浏览器侧执行，受 CORS 与 `allowedHosts` 双重约束，响应体上限 2MB）。**这是唯一可用的网络方式，裸 `fetch` 已失效** | `net` |
| `api.theme.apply(vars)` | 改 `--td-*` 主题变量 | `ui:theme` |

## 5. 事件

| 事件 | 时机 | payload | 门槛（投递所需权限） |
|---|---|---|---|
| `message:new` | 收到新消息 | WS `message` 帧 | `message:read` |
| `message:recall` | 消息被撤回 | WS `recall` 帧 | `message:read` |
| `presence` | 好友上下线 | `{ userId, online }` | `message:read` |
| `conv:open` | 切换会话 | `{ convId }` | 无（payload 只有会话 id，刻意不设门槛） |
| `ui:action` | 面板里 button/input 被点击 | `{ action, value }` | `ui:panel`（只投递给拥有该面板的插件） |
| `message:action` | 右键菜单里**你注册的**菜单项被点击 | `{ actionId, message }` | `ui:actions`（只投递给注册者，不广播） |

> `api.on` 在沙箱内是本地注册、本身无权限门槛，真正把关的是投递过滤（上表"门槛"列）。
> `ui:action` 只投递给拥有该面板的插件，`message:action` 只投递给注册了该动作的插件——纯面板插件（只有 `ui:panel`）即可收面板动作，不需要申请 `message:read`。
> 没有 `message:read` 但有 `ui:actions` 的插件，右键回调里只拿得到消息元信息，拿不到正文。

## 6. 描述式 UI

插件不写 HTML，只发 UI 描述，主站用白名单组件渲染：

```json
{
  "title": "天气",
  "body": [
    { "component": "badge", "props": { "text": "杭州", "tone": "brand" } },
    { "component": "text", "props": { "content": "晴 22℃" } },
    { "component": "progress", "props": { "label": "湿度", "value": 62 } },
    { "component": "table", "props": { "headers": "时段,温度", "rows": "[[\"上午\",22],[\"下午\",26]]" } },
    { "component": "button", "props": { "text": "刷新", "theme": "primary" }, "action": "refresh" }
  ]
}
```

| 组件 | props |
|---|---|
| `text` | `content`（支持换行） |
| `image` | `src`（仅 http(s)）、`alt` |
| `button` | `text`、`theme`（`primary` 或默认）；点击回传 `action` |
| `input` | `placeholder`、`value`；变更回传 `action` |
| `textarea` | `placeholder`、`value`、`rows`（2–8，默认 3）、`maxlength`（≤4000）；变更回传 `action` |
| `checkbox` | `text`、`checked`（布尔）；变更回传 `action` |
| `switch` | `text`、`value`（布尔）；变更回传 `action` |
| `segmented` | `options`（JSON 字符串数组，≤8 项）、`value`；点击回传 `action` 与选中值 |
| `select` | `options`（JSON 字符串数组，≤50 项）、`value`、`placeholder`；选中回传 `action` |
| `card` / `list` | 用 `children` 放子节点 |
| `divider` | — |
| `link` | `href`（仅 http(s)）、`text` |
| `badge` | `text`、`tone`（brand/success/warning/danger） |
| `progress` | `label`、`value`（0–100） |
| `table` | `headers`（逗号分隔）、`rows`（JSON 字符串，二维数组，≤50 行 × 6 列） |
| `tabs` | `labels`（JSON 字符串数组，≤8 项）、`active`（索引）；用 `children` 放对应页内容；切换回传索引 |
| `steps` | `items`（JSON 字符串数组，≤8 项）、`active`（索引）；步骤条 |
| `empty` | `text`、`icon`；空态 |
| `loading` | `text`；加载态 |
| `upload` | `accept`、`maxSize`（字节，默认 256KB、上限 1MB）；选择后由**宿主**读取文件，回传 `{name,size,type,dataUrl}` 或 `action:error` |

约束：嵌套深度 ≤8（超出部分被截断）；`props` 只接受字符串/数字/布尔/null，非原始值会被丢弃；出现白名单外组件整段拒绝渲染。

## 7. 调试与进阶

- **开发循环（推荐）**：`scripts/dev-plugin.mjs` 把「每次改动都去网页里复制粘贴安装」换成命令行：
  ```bash
  # 保存即推送：服务端重新安装并启用，刷新页面即可加载新代码
  node scripts/dev-plugin.mjs watch my.plugin.js --url http://localhost:8080 --token "$JWT"
  # 只校验。带 --url 时走服务端同一套规则；不带则用离线镜像规则（CI 可用）
  node scripts/dev-plugin.mjs check my.plugin.js [--url ... --token ...]
  ```
  `push` / `watch` 会登录、上传、按需启用；依赖未就绪时**只安装不启用**并打印原因。在线校验走 `POST /api/plugins/validate`（干跑，不落库、不改状态），与安装共用服务端的 Go 校验，因此不会出现「本地过了、安装 400」。认证优先级 `--token` > `MC_TOKEN` > `--user/--pass`（开启两步验证的账号请直接用 token）。
- **调试**：插件 iframe 默认隐藏，打开 DevTools 的 frame 列表可选中它看 `console`；`__mcActivate` 抛错时控制台会有 `[plugin:名称] boot warning`
- **自定义消息类型**：`api.registerMessageRenderer('weather', p => desc)` 注册，`api.send(convId, data, 'weather')` 发送；类型名需匹配 `^[a-z0-9_-]+$`
- **存储**：`api.storage` 只存字符串，对象自己 `JSON.stringify`；v2 可用 `keys()` / `remove()` 清理本插件数据，仍受每插件 256KB 总配额约束
- **草稿**：`api.draft.set()` 只作用于当前会话，适合「把搜索词/模板填进输入框」这类工作流；不会替用户发送消息，编辑消息时写入会被拒绝
- **剪贴板**：`api.clipboard.writeText()` 只写不读，且受浏览器安全上下文与用户手势约束；失败时插件会收到明确错误
- **网络**：服务端不出站，`api.request` 在浏览器执行，因此受目标站点 CORS 限制；需要跨域时用支持 CORS 的接口或自己的代理。**不要直接 `fetch`**：沙箱是 opaque origin，且 CSP 只放行 `allowedHosts`，裸 `fetch` 基本都会失败；必须用 `mc.request` / `api.request`
- **示例**：
  - `plugins/examples/weather.plugin.js` —— 侧边栏面板 + 自定义消息类型（JS）
  - `plugins/examples/conv-stats.plugin.ts` —— 会话统计，演示 `conv:read`、右键菜单动作、`table/progress/badge`（TypeScript）
  - `plugins/examples/dice.plugin.js` —— 掷骰子：面板输入 + `message:send` 发送
  - `plugins/examples/word-count.plugin.js` —— 消息字数统计，演示 `message:new` 与 `storage`
  - `plugins/examples/search-kit.plugin.js` —— 搜索工具箱，演示 SDK v2 的会话列表 / 消息搜索 / 草稿 / 剪贴板 / 存储管理与新控件
  - `plugins/examples/page-kit.plugin.js` —— SDK v3 完整页面示例，演示 `addPage` + 用户级服务端 `data` KV
  - `plugins/examples/ai-chat.plugin.js` —— SDK v4 AI 对话页面，演示 `api.ai.*` 服务端代理
  - `plugins/examples/power-kit.plugin.js` —— SDK v5 综合能力示例，演示会话/消息/文件 API 与新控件

---

# 第二部分：服务端插件

服务端插件由**管理员**在后台安装，可以为全站提供页面、API、事件订阅和定时任务。

- `runtime: "declarative"`（默认）：只写 JSON manifest，由服务端运行时解释静态响应 / 代理 / 事件 / 定时任务。
- `runtime: "wasm"`：额外上传一个 WASI 模块，用自定义逻辑处理路由；代码运行在 `wazero` 沙箱里，只能通过宿主 `mc_call` 访问 KV、受控 HTTP、AI 与扫码能力。

## S1. 能做什么

- **全站页面**：`pages` 数组声明页面 id / 标题 / 图标 / 可见范围 / 描述式 UI 页面体；`scope: user` 所有登录用户可见，`scope: admin` 仅管理员可见。
- **静态 API**：`routes` 声明 method + path，直接返回固定 JSON；适合配置查询、状态页数据源。
- **代理 API**：`routes.proxy` 指向外部 HTTPS 服务；服务端做 SSRF 防护后转发，不把用户 JWT 转给第三方。
- **WASM API**：`runtime: "wasm"` 时，`routes[].handler: true` 把请求交给 WASM；设置 `public: true` 可免登录（扫码首屏 / 第三方回调）。
- **事件订阅**：`events` 目前支持 `message:new`，服务端以 HMAC 签名 POST JSON 到你的 HTTPS endpoint。
- **定时任务**：`jobs` 按 `everySeconds` 触发 HTTPS endpoint，适合报表、清理、外部同步。
- **宿主能力**：WASM 可调用 `kv.*`、`http.request`、`ai.chat`、`auth.qr.*`、`message.send`、`conv.*`、`user.*`、`notification.push`；需要 `permissions` 显式声明 `ai:chat` / `auth:qr` / `server:message` / `server:conv` / `server:user`。
- **统一启停**：管理员可启用 / 停用 / 卸载，所有变更写审计；WASM 上传单独限制 8MB。

## S2. manifest 示例

完整示例见 `plugins/examples/server-hello.plugin.json`：

```json
{
  "name": "server-hello",
  "version": "1.0.0",
  "description": "管理员服务端插件示例",
  "pages": [
    {
      "id": "hello",
      "title": "服务端示例页",
      "icon": "服务器",
      "scope": "user",
      "body": [
        { "component": "text", "props": { "content": "所有登录用户都能看到这个页面。" } }
      ]
    }
  ],
  "routes": [
    { "method": "POST", "path": "/hello", "response": { "status": 200, "body": { "ok": true, "message": "Hello" } } }
  ],
  "events": [],
  "jobs": [],
  "allowedHosts": []
}
```

`dependencies` 可选，语法与用户插件一致（`["other-plugin@^1.0.0"]`），指向**其它服务端插件**；安装时只校验语法，管理员启用时才检查对方是否已安装并启用。`engines` 不适用于服务端插件（它的兼容性取决于宿主版本而不是插件 SDK，而宿主版本在本地开发时是 `dev`，强校验会让本地装不上）。

## S2b. WASM manifest 与宿主 ABI

WASM 示例见 `server-examples/qr-kit/`（源码 + manifest + 构建脚本）。目录内执行：

```bash
GOOS=wasip1 GOARCH=wasm go build -o qr-kit.wasm .
```

然后后台粘贴 `manifest.json`、选择 `qr-kit.wasm` 上传并启用。WASM 入口是标准 WASI `_start`：

- **stdin**：JSON 请求信封 `{kind,plugin,method,path,query,headers,body,user,event,payload}`。
- **stdout**：JSON 响应 `{status,headers,body}`。
- **宿主调用**：导入 `env.mc_call(opPtr, opLen, reqPtr, reqLen, outPtr, outCap) int32`；返回写入 `outPtr` 的字节数，负数表示失败。

可用 `op`：

| op | 作用 |
|---|---|
| `kv.get` / `kv.set` / `kv.delete` / `kv.keys` | 插件专属持久 KV |
| `http.request` | 受 `allowedHosts` + SSRF 防护的出站请求 |
| `ai.chat` | 服务端统一 AI 代理（需 `ai:chat`） |
| `auth.qr.start` | 生成一次性扫码票据（需 `auth:qr`） |
| `auth.qr.confirm` / `auth.qr.invalidate` | 确认 / 作废扫码票据（需 `auth:qr`） |
| `message.send` | 以当前用户身份向可见会话发消息（需 `server:message`） |
| `conv.list` / `conv.members` | 读取当前用户可见会话与成员（需 `server:conv`） |
| `user.get` / `user.search` | 查询用户摘要（需 `server:user`） |
| `notification.push` | 推送通知；未配置 PushService 时返回明确错误 |

扫码票据与本站手机确认页共用同一状态机：桌面端扫描 `/qr/<token>` 后，手机确认即可由 `/api/auth/qr/status` 取走登录态。

## S3. 安全边界

- 外部地址必须是 **HTTPS**，且主机名出现在 `allowedHosts` 中；动态路由和 WASM `http.request` 同样受限。
- 出站请求全部经过 SSRF 防护，禁止内网 / 回环 / 保留网段。
- Webhook 携带 `X-MC-Plugin` 与 `X-MC-Signature`（HMAC-SHA256），第三方可校验来源。
- 动态 API 默认要求本站登录；只有 `public: true` 的路由可通过 `/api/ext-public` 匿名访问。
- 服务端不会把用户 JWT / Cookie 转发给第三方；WASM 只能拿到当前用户的 id / nickname / role。
- 页面 UI 与用户插件共用白名单组件，不解析 HTML / 任意 CSS。
- `pages` / `routes` / `events` / `jobs` 各最多 20 项，页面体 ≤64KB；WASM ≤8MB。
- AI 密钥只在服务端；本地模型可设 `AI_ALLOW_PRIVATE=true`，默认禁止私网。

## S4. 路由与事件

- 登录插件 API：`/api/ext/{pluginName}/{path}`。
- 公开插件 API：`/api/ext-public/{pluginName}/{path}`，仅 `public: true`。
- 全站页面：`/plugin-server/{pluginId}/{pageId}`。
- `message:new` 事件 payload 仅含消息元数据与正文：`{ id, convId, from, msgType, content, createdAt }`。

## S5. 服务端 AI 代理

配置 `AI_BASE_URL`（完整 OpenAI 兼容端点）、`AI_API_KEY`、`AI_MODEL` 后启用：

- 用户插件：`api.ai.status()` / `api.ai.chat(messages)`，需 `ai` 权限。
- WASM 服务端插件：`mc_call("ai.chat", {messages})`，需 `ai:chat` 权限。
- 当前为非流式，单用户 20 次/分钟；密钥不会下发到浏览器。

## 8. 发布与升级

1. 把单文件放到任意可访问的 URL（或直接在插件面板上传）
2. 用户三种入口：「插件」→ 上传文件 / 粘贴 URL → 确认权限；或官网插件市场「一键装进应用」——深链 `${CHAT_APP}chat?plugin-install=<插件URL>&plugin-name=<名称>` 自动打开抽屉并预填 URL，仍需手点安装并确认权限（确认框是唯一人工闸门，不存在静默安装）
3. **升级**：同名插件再次安装即覆盖（`id` 不变、保留启停），响应头带 `X-Plugin-Updated: true` 与 `X-Plugin-Old-Version: <旧版本>`（前端确认框展示版本过渡，降级安装会额外警告）；**权限 / `allowedHosts` 有变化**时另带 `X-Plugin-Perms-Changed: true`，已启用的插件必须重新确认——确认后重载新代码，拒绝则立即停用（见 §2）
4. 站长在后台可按插件名**全局下架**或**强制卸载**
5. **市场收录**：官方示例插件放在 `plugins/examples/`（manifest 写在源码首个块注释），跑 `node scripts/sync-plugins.mjs` 同步官网副本并生成 `index.json`（市场卡片与一键安装的数据源；CI 用 `--check` 校验副本与索引不漂移），再在官网 `PluginsView.vue` 的 `PLUGINS` 表补中英文案。`index.json` 同时带 `engines` / `dependencies`，市场卡片会显示「SDK ≥x」与依赖标签——**兼容性与依赖信息只有 manifest 一个真源**，不要在官网文案里手写版本要求。

> 安全边界：插件跑在 `sandbox="allow-scripts"` 且**没有** `allow-same-origin` 的 iframe 里，origin 是 opaque，拿不到 JWT、读不到主站存储、碰不到其他插件；引导代码通过 `srcdoc` 注入，`</script` 会被转义；CSP 为 `default-src 'none'`，网络出口只有 `allowedHosts`；服务端不出站；TS 转译在服务端完成，转译失败会直接拒绝安装并提示行号。
