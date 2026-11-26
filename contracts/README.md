# contracts/ — 跨端契约金样本

本目录是 ModernChat 跨端契约的**机器可读唯一真源**，配套 `docs/项目组架构与集成.md` §3。客户端（desktop / mobile）测试直接消费这里的金样本，断言「解析口径与主仓库一致」。

| 文件 | 内容 |
|---|---|
| `ws-frames.json` | WS 帧 `ClientFrame` / `ServerFrame` 金样本（真源 `web/src/types/proto.ts`） |
| `rest-samples.json` | 关键 REST 响应金样本（真源 `docs/后端需求规格.md` §3） |

## 同步规则

1. **只改主仓库**：修改契约先改《后端需求规格》→ 再改金样本与 `proto.ts` → 最后复制到两端副本；
2. **两端副本与指纹**：
   - desktop：复制 `ws-frames.json` → `Modernchat-desktop/contracts/`，指纹写入同目录 `ws-frames.sha256`；
   - mobile：复制 `ws-frames.json` → `Modernchat-mobile/test/fixtures/contracts/ws_frames.json`，指纹写入同目录 `ws_frames.sha256`；
   - 指纹生成（Windows PowerShell）：`Get-FileHash -Algorithm SHA256 <文件>`，写入 `<hash>  <文件名>` 两空格格式；
3. **CI 校验**：两端 CI 各跑一步 `sha256sum -c`，副本被本地手改而指纹未更新会直接挂 CI——防止契约悄悄漂移；
4. **破坏性变更**：金样本只增不改语义；变更已有字段须走版本协商（`GET /api/client/version` 提高最低版本）并在两端 CHANGELOG 同步记录。

## 消费方

- desktop：`src/core/network/ws.test.ts`（Vitest 读取 `../../contracts/ws-frames.json`）
- mobile：`test/contract_frames_test.dart`（dart:io 读取 `test/fixtures/contracts/ws_frames.json`）
