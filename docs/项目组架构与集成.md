# 项目组架构与集成

> 本文是 Modernchat 项目组跨仓库协同的单一入口：组件定位与关系、登录拓扑（含身份空间约束）、契约真源与金样本机制、部署组合、版本与发版协同。各仓库内部设计见其自身文档；主仓库内部细节见《设计文档》《后端需求规格》。

## §1 项目组定位：一个核心，四个组件

Modernchat-new 是项目组核心（网页聊天平台）；其余四个仓库是它的组件。每个组件都具备**双重属性**：既可独立出去单独部署运行，也可与其他组件联合协同工作——不存在让某个组件无法独立部署的硬耦合，联合时只通过标准化契约（REST / WS / JWT / OAuth）对接。

| 仓库 | GitHub | 形态 | 定位 | 开发默认端口（web / 后端 / DB） | 最新发布 |
|---|---|---|---|---|---|
| **Modernchat-new（核心）** | modern-chat-new | Go 1.26 + Vue 3 monorepo | 聊天平台本体：REST / WS / 插件 / 官网 / 站长后台 | 5173、官网 5174 / 8080 / 3306 | 0.12.0 |
| Modernchat-Hub | Modernchat-Hub | Go 1.26 + Vue 3 | 项目组统一管理面板：服务实例注册与探测、更新编排、统一管理后台、**OAuth Provider（统一登录中心）** | 5176 / 8091 / 3308 | 0.5.0 |
| Modernchat-sso | Modernchat-sso | Go 1.26 + Vue 3 | 独立 SSO 身份服务：注册登录 / 邮箱验证码 / 站长账号管理，可脱离 Hub 单独给主仓库做身份源 | 5175 / 8090 / 3307 | 0.1.0 |
| Modernchat-desktop | Modernchat-desktop | Tauri 2 + Vue 3 | 桌面客户端（扫码登录 + 最小聊天闭环，token 存系统钥匙串） | 1420 / — / — | 0.1.0 |
| Modernchat-mobile | Modernchat-mobile | Flutter | 移动客户端（Android / iOS，auth / chat / contacts / settings 四特性闭环） | — / — / — | 1.0.0+1 |

> 端口是开发默认值，仅用于本地联调对齐；生产部署端口随机化更安全（见《运维手册》），生产环境以部署配置为准。

## §2 登录与身份拓扑

主仓库支持三种身份来源。**同一主仓库实例同一时间只应接入一个身份源**（约束见下方警示）：

| 拓扑 | 身份主数据 | 接入方式 | 适用场景 |
|---|---|---|---|
| A. 主仓库自带账号 | 主仓库 users 表 | 默认，无需配置 | 最小部署（不装 Hub / sso） |
| B. Hub 统一登录中心 | Hub users 表 | 主仓库设 `SSO_HUB_URL` 指向 Hub 并填 `SSO_CLIENT_ID / SSO_CLIENT_SECRET`（Hub 管理员经「统一登录中心」注册 OAuth client）；主仓库 SSO 客户端零改动 | 标准部署（全家桶）、多站点统一账号 |
| C. sso 独立身份服务 | sso users 表 | 与 B 同一套 OAuth / SSO 客户端契约，把 `SSO_HUB_URL` 指向 sso 即可 | 不部署 Hub、但要把登录注册独立出去的站点 |

**⚠️ uid 空间约束（跨仓库身份集成的第一注意事项）**：项目组 JWT 口径统一（HS256、claims 仅 `uid` + `tv`），各服务共享同一 `JWT_SECRET` 即互认——但前提是**两端 uid 指向同一个用户库**。主仓库自带账号（uid 来自主仓库 users 表）与拓扑 B / C（uid 来自 Hub / sso 的 users 表）是**两个不同的 uid 空间**，混用会导致「同一 token 在两个服务里解析成不同用户」的串号。因此：

- 每个主仓库实例只配置一个身份源；`SSO_HUB_URL` 配置后，权限判断一律以该身份源签发的 token 为准，不再混用本地注册用户的 uid 语义；
- 换身份源是**迁移**：停写旧库 → 迁移用户数据（保持 uid 或做映射表）→ 切换，不做双身份源并存；
- `JWT_SECRET` 一旦被多服务共享就不可随意轮换——主仓库还用它派生 SecretBox（后台凭据加密）与邮箱验证码 HMAC，轮换需按《安全与迁移指南》全链路同步。

## §3 契约真源与金样本

### 3.1 真源索引

| 契约 | 真源 | 消费方 |
|---|---|---|
| REST 形状 | `docs/后端需求规格.md` §3 ↔ `web/src/api/index.ts` | 全部客户端 |
| WS 帧 | `web/src/types/proto.ts`（`ClientFrame` / `ServerFrame`） | desktop / mobile / 插件 |
| 第三方客户端接入 | `docs/客户端接入指南.md` | desktop / mobile / 未来端 |
| JWT / OAuth | Hub `/oauth/authorize` + `/api/oauth/{token,userinfo}`（与主仓库 SSO 客户端契约对齐） | 主仓库 / sso |

### 3.2 契约金样本（`contracts/`）

`contracts/` 是跨端契约的**机器可读金样本**：主仓库维护唯一真源，desktop / mobile 各持同步副本，客户端测试直接消费副本，断言「解析口径与主仓库一致」而不是「自己解析自己」。

| 文件 | 内容 |
|---|---|
| `contracts/ws-frames.json` | WS 帧 `ClientFrame` / `ServerFrame` 金样本，与 proto.ts 逐字段对应 |
| `contracts/rest-samples.json` | 关键 REST 响应金样本（版本协商 / 当前用户 / 会话列表 / 消息分页） |

**同步与校验规则**：

1. 修改契约只改主仓库金样本（先改《后端需求规格》，再改金样本与 proto.ts），然后复制到两端副本；
2. 两端副本旁有 `*.sha256` 指纹文件，CI 校验「副本未被本地手改」——直接改副本不改指纹会挂 CI，防止契约悄悄漂移；
3. 客户端测试消费副本金样本：desktop `src/core/network/ws.test.ts`、mobile `test/contract_frames_test.dart` 直接读取 JSON 断言字段口径；
4. 金样本只增不改语义：变更已有字段属于破坏性变更，须走版本协商（§6）并在两端 CHANGELOG 同步记录。

## §4 客户端能力矩阵

各端实现现状（金样本是契约全集，端实现允许滞后）：

| 能力 | web | desktop | mobile |
|---|---|---|---|
| 基础消息闭环（chat / typing / read / recall / edit） | ✅ | ✅ | ✅ |
| reaction / delivered / 群管理 / 搜索收藏 | ✅ | — | — |
| call.* 语音 / 视频通话 | ✅ | — | — |
| 版本协商 `GET /api/client/version` | ✅ | ✅ | ✅ |
| 扫码登录 | 确认端 | 发起端 | 确认端 |
| 客户端标识 `X-Client-*` | ✅ | ✅ | ✅ |

新帧落地流程：主仓库金样本 → 需要接入的端复制副本并实现 → 更新本表。

## §5 部署组合

| 组合 | 包含 | 说明 |
|---|---|---|
| 最小 | 主仓库 | 自带账号体系，`install.sh` 开箱即用 |
| 标准 | 主仓库 + Hub | Hub 提供统一登录中心（OAuth）+ 集中运维：版本矩阵 / 更新编排 / 统一管理后台（主仓库与 sso 的更新编排均已接入 Hub adapter） |
| 全套 | 主仓库 + Hub + sso + desktop + mobile | 身份源在 Hub 与 sso 之间**二选一**（见 §2），客户端走各自 Release 渠道 |

desktop / mobile 不属于服务端部署：版本经应用内协商接口获知，`upgradeRequired=true` 只提示不硬阻断，旧端不会被服务端强制下线。

## §6 版本与发版协同

- **版本协商**：各端启动请求 `GET /api/client/version?platform=&clientType=&appVersion=`（免鉴权），返回 `{version, minVersion, upgradeRequired, message?}`；客户端标识经 `X-Client-*` 头与 WS query 同步上报，服务端据此在会话列表区分设备（口径见《客户端接入指南》）。
- **跨仓库契约变更的发版顺序**：主仓库先行（金样本 + proto.ts + 服务端同一提交）→ 客户端跟进（副本 + 实现）→ Hub adapter 如受影响同步。文档与代码同一提交更新（项目组铁律）。
- **Hub 版本矩阵**：Hub 对已注册服务实例做健康 / 版本探测与更新编排——主仓库 `GET /api/health` + `/api/version`、sso `GET /healthz` + `/api/v1/version`；服务端更新编排主仓库与 sso 均已接入。desktop / mobile 版本走各自 Release，不经 Hub。
- **CHANGELOG**：每个仓库各自维护；涉及跨仓库契约的条目在双方 CHANGELOG 都要出现。

## §7 跨仓库协同流程约定

1. **改接口**：先改 `docs/后端需求规格.md` → 再改后端与 `proto.ts`（金样本同步）→ 最后改前端与各端；
2. **文档与代码同一提交**：功能 / 架构 → 设计文档；接口 → 后端需求规格；环境变量 / 部署 → 运维手册；用户可见功能 → CHANGELOG `[未发布]`；
3. **提交信息**：Conventional Commits + 中文（`feat(adapter): xxx`）；`docs/` 中文文档改完跑 `website/scripts/sync-docs.ps1` 同步官网镜像，英文镜像手动对应更新；
4. **安全红线**：凭据永不入库；服务端日志永不记录密码 / 令牌 / 消息正文；`FRONTEND_URL`（逗号分隔）同时是 WS Origin 白名单——任何客户端 / 管理面板的域名必须登记，否则 WS 握手被拒。
