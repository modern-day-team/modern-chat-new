# 对象存储服务商接入手册

> 面向**站长**：本文讲清对象存储怎么选服务商、字段怎么填、填错会报什么错。
> 代码实现见 `server/internal/service/storage_provider.go`（服务商预设）与
> `storage.go`（本地 + S3 实现）。本文描述的是当前版本实际可用的能力。

---

## 1. 当前支持范围

内核只有 **本地磁盘** 与 **一套 S3 兼容实现**（minio-go）。所谓"多家服务商"
指的是：它们都讲 S3 协议，差别只在 **端点**、**区域** 与 **寻址风格**。

> 七牛 Kodo / 又拍云 USS 这类自有协议**未实现**。曾经有一份文档描述过它们和
> 13 家预设，但代码从未落地；该文档已按实际能力重写，请以本页与后台界面为准。

后台「系统设置 → 对象存储」有一个「服务商」下拉，当前提供：

| 服务商 ID | 显示名 | 端点模板 | 默认区域 | 寻址风格 |
|---|---|---|---|---|
| `aws` | AWS S3 | `s3.{region}.amazonaws.com` | `us-east-1` | 虚拟主机 |
| `minio` | MinIO (self-hosted) | 手填 | `us-east-1` | path |
| `oss` | 阿里云 OSS | `oss-{region}.aliyuncs.com` | `cn-hangzhou` | 虚拟主机 |
| `cos` | 腾讯云 COS | `cos.{region}.myqcloud.com` | `ap-guangzhou` | 虚拟主机 |
| `obs` | 华为云 OBS | `obs.{region}.myhuaweicloud.com` | `cn-north-4` | 虚拟主机 |
| `bos` | 百度智能云 BOS | `bj.bcebos.com` | `bj` | 虚拟主机 |
| `tos` | 火山引擎 TOS | `tos-{region}.volces.com` | `cn-beijing` | 虚拟主机 |
| `ks3` | 金山云 KS3 | `ks3-{region}.ksyun.com` | `cn-beijing-6` | 虚拟主机 |
| `ucloud` | UCloud US3 | `ufile-{region}.ucloud.cn` | `cn-bj2` | 虚拟主机 |
| `custom` | 自定义 | 手填 | `us-east-1` | path |

---

## 2. 字段说明

- **服务商**：选了以后，端点 / 区域会按模板自动填占位（仍可覆盖）。
- **Endpoint**：留空时用模板 + 区域拼出；可填内网端点（省流量费）或私有部署地址。
- **Region**：留空时用服务商默认值。**S3 签名校验区域**，填错会 `403 SignatureDoesNotMatch`。
- **Bucket**：必填；留空视为未启用对象存储，上传走本地磁盘。
- **AccessKey / SecretKey**：静态 AK/SK。SecretKey 加密落库、永不回显；留空表示不修改。
- **PublicBase**：对外访问前缀（CDN 域名）。留空则按 `scheme://endpoint/bucket` 拼。

### 寻址风格为什么重要

- **虚拟主机风格**：`bucket.endpoint/key`（AWS / 国内主流云商）
- **path 风格**：`endpoint/bucket/key`（MinIO 自建 / 自定义端点）

选错的典型症状是 `403 SignatureDoesNotMatch`、`301 PermanentRedirect` 或 DNS 解析失败，
错误信息完全看不出根因。预设已经按服务商设置了正确风格，手填端点时请确认你的服务支持哪种。

> 老配置没有「服务商」字段时按 `custom` 处理（path 风格）。如果你原来用的是 AWS/OSS
> 且只手填了端点，升级后请到后台把服务商选成对应项，避免寻址风格变化。

---

## 3. 连接自检

保存配置后点 **「测试连接」**，服务端会按刚保存的 values 重建客户端并执行
`HEAD Bucket`（只读，不写对象、不覆盖文件）。成功即返回实际使用的端点。

对象存储是**启动时**装配的，改完配置需重启服务才切换上传实现。自检让你在重启前
就能发现配置错误，不必等上传失败才排查。

---

## 4. 已知边界

- 静态 AK/SK，不支持 STS 临时凭证 / AssumeRole。
- 私有桶不生成服务端签名 URL。S3 模式下新文件返回 `publicBase` 绝对地址，
  访问控制由桶策略 / CDN 承担；只有本地存储的历史文件走 `/uploads` 登录鉴权。
- 切换存储不会搬运历史文件，需自行用 `mc` / `rclone` 迁移并批量改 `messages.content`。
- S3 孤儿对象不在「清理孤儿附件」范围内。
- 多后端并存 / 按文件类型分流未实现。

与邮件服务（SMTP / HTTP API）相关的配置见 [运维手册](运维手册.md) §3.3 与本页无关。
