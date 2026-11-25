# qr-kit 服务端 WASM 示例

演示管理员安装的 WASM 插件如何调用宿主扫码能力：

- `POST /api/ext-public/qr-kit/qr/start` → 生成一次性扫码票据与 `/qr/<token>` 确认路径
- `POST /api/ext/qr-kit/qr/confirm` → 登录用户确认票据（实际产品可改为第三方回调校验）

## 构建

```powershell
./build.ps1
```

或手动：

```bash
GOOS=wasip1 GOARCH=wasm go build -o qr-kit.wasm .
```

`qr-kit.wasm` 不提交仓库。

## 安装

1. 管理员打开 `/admin` → 「服务端插件」。
2. 粘贴 `manifest.json`，选择 `qr-kit.wasm`，点击安装。
3. 在列表中启用插件。
4. 调用：

```bash
curl -X POST http://127.0.0.1:8080/api/ext-public/qr-kit/qr/start \
  -H 'Content-Type: application/json' -d '{}'
```

返回 `{ token, expiresAt, confirmPath }`。前端把 `confirmPath` 渲染成二维码，
手机用已登录会话打开确认页即可；桌面可继续用 `/api/auth/qr/status` 取走登录态。
