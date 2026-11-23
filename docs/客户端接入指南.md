# 客户端接入指南

> 适用：Web（`web/`）、桌面端（`Modernchat-desktop`，Tauri 2）、移动端（`Modernchat-mobile`，Flutter）、以及 SSO / Hub 等需要调用主站接口的组件。
> 接口契约以 `docs/后端需求规格.md` 为准，本文只讲**跨端接入的共同约定与踩坑**。

## 1. 服务端地址与来源登记

所有端统一访问同一后端，REST 前缀 `/api`，WebSocket `/ws`，附件 `/uploads`。

- 浏览器内核的端（Web、Tauri WebView）会带 `Origin`，必须在服务端 `FRONTEND_URL`（逗号分隔）里登记自己的来源，否则 **WS 握手被拒、REST 跨源请求被浏览器拦截**。典型值：
  - Web dev `http://localhost:5173`；Web 生产站点同源或 `https://chat.example.com`
  - Tauri 开发 `http://localhost:1420`（`tauri.conf.json` 的 `devUrl`）
  - Tauri 生产 `tauri://localhost` / `https://tauri.localhost`（Windows 用后者）
  - Flutter Web 调试 `http://localhost:5173` 之类
- 无 `Origin` 的原生客户端（Flutter Android/iOS 的 Dio、Go 程序）不受 Origin 白名单约束，但仍必须携带合法 JWT。
- `FRONTEND_URL` 同时是找回密码链接前缀，**不要只填一个地址导致其他端邮件链接跳错站**。

## 2. 登录与令牌

- 密码登录 `POST /api/auth/login {username,password}` → `{token,user}`；开两步验证时先返回 `{twofa:true,ticket,method}`，再 `POST /api/auth/2fa/verify {ticket,code}` 换 token。
- 桌面端推荐扫码登录：`POST /api/auth/qr/start` 渲染二维码 → `POST /api/auth/qr/status` 轮询 → confirmed 时拿到 token；手机端 `POST /api/auth/qr/confirm` 授权。票据一次性消费，轮询请用 POST body 避免进访问日志。
- SSO 场景：`GET /api/auth/sso/start?redirect=` 跳聚合中心 → `callback` 回跳 → `POST /api/auth/sso/exchange {ticket}` 换 token。
- 令牌存储：
  - **必须用 `Authorization: Bearer <token>` 调 REST**。REST 中间件只认 Bearer，不认 cookie（避免 CSRF）；cookie `mc_auth` 仅用于浏览器加载 `/uploads` 图片/视频与 WS 握手回退。
  - Web 端存 `localStorage`（键 `mc_token`）；桌面/移动端存平台安全存储（Tauri 用 keychain 插件或本地加密文件，Flutter 用 `flutter_secure_storage`），**不要写进日志、崩溃报告或 URL**。
  - JWT 有效期 7 天；改密 / 重置 / 封禁 / `logout-others` 会让 `token_version` 失效，401 时清本地 token 并回登录页。
- 退出：`POST /api/auth/logout` 只清 cookie，不吊销 JWT；主动全端下线用 `POST /api/me/logout-others`（当前设备也需重新登录）。单设备下线用 `DELETE /api/me/sessions/:id`。

## 3. 客户端标识与会话管理

- 已登录请求可带以下请求头（均可选、≤32 字符，非法值忽略）：
  - `X-Client-Platform`：`web` | `desktop` | `mobile` | `tablet` | `other`
  - `X-Client-Type`：细分类型，如 `tauri` / `flutter-android` / `flutter-ios` / `browser`
  - `X-Client-Version`：应用版本，如 `0.1.0`
- 每个 JWT 含 128 位会话 ID `sid`。首次请求时服务端按请求头落库，之后只刷新活跃时间。`GET /api/me/sessions` 列出全部会话，`DELETE /api/me/sessions/:id` 撤销单条（该 token 立即失效并踢掉其 WS）。
- WS 握手也支持 `?platform=&clientType=&appVersion=`，用于扫码登录等"先拿 token 后报标识"的场景。
- 设置页里应提供会话列表入口（L3，低频、仅本人）：展示平台、版本、IP、最近活跃时间，支持"退出此设备"。

## 4. WebSocket 实时链路

- 连接 `GET /ws?token=<JWT>`；cookie 回退仅用于浏览器。浏览器来源必须在 `FRONTEND_URL` 白名单内。
- 进入主界面后建立连接；25s 发 `{"type":"ping"}`，服务端回 `pong`（读超时 60s）。
- 帧格式见 `web/src/types/proto.ts` 的 `ClientFrame` / `ServerFrame`——**这是前后端唯一口径**，各端类型定义须逐字段对齐，不要凭猜测构造。
- 幂等：发送文本消息必须带 `clientMsgId`（UUID）；重连后用游标 `/api/conversations/:id/messages?afterId=` 补齐增量。
- 收到 `kicked` 帧表示被强制下线（封禁 / 重置密码 / 会话撤销），清 token 回登录页。
- 后台刷新保留旧数据，不得切回 loading 空态；WS 事件优先局部更新。

## 5. 版本协商

`GET /api/client/version?platform=&clientType=&appVersion=`（免鉴权）返回 `{version,minVersion,upgradeRequired,message?}`。各端启动时调用：当 `upgradeRequired=true` 时提示用户升级（不强制阻断，避免旧端在离线发布期集体不可用）。`minVersion` 来自服务端 `MIN_CLIENT_VERSION`，未配置时永远为 false。

## 6. 上传与附件

- 上传走 `POST /api/upload?kind=image|audio|video|file|sticker`，multipart 字段名 `file`；不要设 `Content-Type`（让运行时补 boundary）。
- 附件读取 `/uploads/...` 必须登录：浏览器 `<img>/<video>` 依赖同源 HttpOnly cookie，原生端用 Bearer。**消息正文里的图片禁止懒加载**（虚拟列表测量依赖真实高度）。
- 服务端按文件头判真实类型；各端压缩后再上传可省带宽（Web 端图片最大边 1600 / webp）。

## 7. 多端一致性检查清单

1. REST 形状与 `docs/后端需求规格.md` §3.2 对齐，错误体统一 `{"error":"中文"}`，401 统一清 token。
2. WS 帧与 `proto.ts` 逐字段对齐；`clientMsgId` 幂等；重连增量补齐。
3. i18n 中英双语文案同步；服务端中文错误由 `errors.ts` 映射。
4. 界面入口遵循信息架构三层（L1 主界面 / L2 详情面板 / L3 设置页），新入口先判层再放。
5. 版本更新后跑一遍：登录 → 会话列表 → 发消息 → 刷新 → 断线重连 → 附件加载。
