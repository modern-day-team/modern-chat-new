# host-kit 服务端 WASM 宿主能力示例

演示管理员安装的 WASM 插件如何调用宿主业务能力：

- `GET /api/ext/host-kit/summary`：列出当前用户自己的会话摘要（`conv.list`）。
- `GET /api/ext/host-kit/members?convId=1`：列出当前用户所在会话的成员（`conv.members`）。
- `POST /api/ext/host-kit/users` body `{ "kw": "zhang", "limit": 20 }`：搜索用户摘要（`user.search`）。
- `POST /api/ext/host-kit/send` body `{ "convId": 1, "content": "来自插件" }`：以当前用户身份发消息（`message.send`）。
- `POST /api/ext/host-kit/push` body `{ "title": "提醒", "body": "内容" }`：只给自己推送。
- `POST /api/ext/host-kit/ai` body `{ "messages": [{ "role": "user", "content": "你好" }] }`：服务端统一 AI 代理（`ai.chat`）。

## 构建

```powershell
./build.ps1
```

`host-kit.wasm` 不提交仓库。

## 安装

1. 管理员打开 `/admin` → 「服务端插件」。
2. 粘贴 `manifest.json`，选择 `host-kit.wasm`，点击安装。
3. 在列表中启用插件；AI 路由需服务端配置 `AI_*` 环境变量。

所有路由都需要登录。宿主不会把插件伪装成系统或他人身份：消息发送者、会话可见范围和推送目标都来自当前登录用户。
