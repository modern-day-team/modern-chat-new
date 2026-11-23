# Object Storage Provider Guide

> For site owners: how to pick a provider, what each field means, and what common
> misconfigurations look like. Implementation: `server/internal/service/storage_provider.go`
> (provider presets) and `storage.go` (local + S3).

---

## 1. Current scope

The kernel supports **local disk** and **one S3-compatible implementation** (minio-go).
"Multiple providers" here means they all speak S3; what differs is the **endpoint**,
**region** and **addressing style**.

> Proprietary protocols (Qiniu Kodo / Uppyun USS) are **not implemented**. An earlier
> doc described them and 13 presets, but no code ever shipped; it has been rewritten
> to reflect the actual capabilities.

The admin "System settings → Object storage" page has a Provider dropdown:

| ID | Label | Endpoint template | Default region | Addressing |
|---|---|---|---|---|
| `aws` | AWS S3 | `s3.{region}.amazonaws.com` | `us-east-1` | virtual-hosted |
| `minio` | MinIO (self-hosted) | manual | `us-east-1` | path |
| `oss` | Alibaba Cloud OSS | `oss-{region}.aliyuncs.com` | `cn-hangzhou` | virtual-hosted |
| `cos` | Tencent Cloud COS | `cos.{region}.myqcloud.com` | `ap-guangzhou` | virtual-hosted |
| `obs` | Huawei Cloud OBS | `obs.{region}.myhuaweicloud.com` | `cn-north-4` | virtual-hosted |
| `bos` | Baidu Cloud BOS | `bj.bcebos.com` | `bj` | virtual-hosted |
| `tos` | Volcano Engine TOS | `tos-{region}.volces.com` | `cn-beijing` | virtual-hosted |
| `ks3` | Kingsoft KS3 | `ks3-{region}.ksyun.com` | `cn-beijing-6` | virtual-hosted |
| `ucloud` | UCloud US3 | `ufile-{region}.ucloud.cn` | `cn-bj2` | virtual-hosted |
| `custom` | Custom | manual | `us-east-1` | path |

---

## 2. Fields

- **Provider**: pre-fills endpoint / region templates (still editable).
- **Endpoint**: built from template + region when empty; can be an internal endpoint.
- **Region**: falls back to the provider default. **S3 signatures bind the region** —
  a wrong value gives `403 SignatureDoesNotMatch`.
- **Bucket**: required; when empty, uploads go to local disk.
- **AccessKey / SecretKey**: static credentials. SecretKey is encrypted at rest and
  never echoed; leaving it blank keeps the stored value.
- **PublicBase**: public URL prefix (CDN). Empty → `scheme://endpoint/bucket`.

### Why addressing style matters

- Virtual-hosted: `bucket.endpoint/key` (AWS and most Chinese clouds)
- Path: `endpoint/bucket/key` (self-hosted MinIO / custom endpoints)

A mismatch shows up as `403 SignatureDoesNotMatch`, `301 PermanentRedirect` or a DNS
failure, with no hint about the root cause. Presets pick the right style; verify it
when using a hand-filled endpoint.

> Configs without `provider` are treated as `custom` (path-style). If you previously
> used AWS/OSS with only a hand-filled endpoint, select the matching provider after
> upgrading so the addressing style does not change silently.

---

## 3. Connection test

After saving, click **Test connection**. The server rebuilds a client from the just-saved
values and issues `HEAD Bucket` (read-only — no object is written or overwritten).
Success returns the resolved endpoint.

Storage is assembled at startup, so a restart is required before uploads switch to the
new configuration. The test lets you catch mistakes before restarting.

---

## 4. Known limits

- Static AK/SK only; no STS / AssumeRole.
- No server-side signed URLs for private buckets. S3-mode files return the absolute
  `publicBase` URL; access control is delegated to the bucket / CDN. Only legacy local
  files under `/uploads` go through the login-gated static handler.
- Switching storage never migrates old files; use `mc` / `rclone` and rewrite
  `messages.content` yourself.
- S3 orphan objects are not covered by "clean orphan attachments".
- No multi-backend routing / per-kind splitting.

Email transports (SMTP / HTTP API) are configured separately — see the Ops Manual.
