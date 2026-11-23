package service

import (
	"strings"
)

// S3 兼容服务商预设。只提供「端点模板 / 默认区域 / 寻址风格」三类信息，
// 不引入任何新依赖——所有厂商都靠 minio-go 的 SigV4 走同一套 S3 协议。
//
// 为什么需要这张表：minio-go 默认按虚拟主机风格（bucket.endpoint）发请求，
// 而 MinIO 自建服务通常只有 path 风格（endpoint/bucket）。选错时服务端返回
// 403 SignatureDoesNotMatch 或 301 PermanentRedirect，错误信息完全看不出
// 是寻址风格的问题——这是各家云商接入失败的最常见原因。
type S3Provider struct {
	// ID 后台下拉值，同时作为"未显式填 endpoint/region 时"的模板键
	ID string
	// Label 展示名（i18n 由前端负责，这里只存稳定的英文标识）
	Label string
	// EndpointTemplate 形如 "oss-{region}.aliyuncs.com"；region 为空时给默认
	EndpointTemplate string
	// DefaultRegion 用户没填 region 时用这个（S3 签名必需，多数云商校验）
	DefaultRegion string
	// PathStyle true=path 风格；false=虚拟主机风格
	PathStyle bool
}

// S3Providers 服务商预设表。顺序即后台下拉顺序。
var S3Providers = []S3Provider{
	{ID: "aws", Label: "AWS S3", EndpointTemplate: "s3.{region}.amazonaws.com", DefaultRegion: "us-east-1"},
	{ID: "minio", Label: "MinIO (self-hosted)", EndpointTemplate: "", DefaultRegion: "us-east-1", PathStyle: true},
	{ID: "oss", Label: "Alibaba Cloud OSS", EndpointTemplate: "oss-{region}.aliyuncs.com", DefaultRegion: "cn-hangzhou"},
	{ID: "cos", Label: "Tencent Cloud COS", EndpointTemplate: "cos.{region}.myqcloud.com", DefaultRegion: "ap-guangzhou"},
	{ID: "obs", Label: "Huawei Cloud OBS", EndpointTemplate: "obs.{region}.myhuaweicloud.com", DefaultRegion: "cn-north-4"},
	{ID: "bos", Label: "Baidu Cloud BOS", EndpointTemplate: "bj.bcebos.com", DefaultRegion: "bj"},
	{ID: "tos", Label: "Volcano Engine TOS", EndpointTemplate: "tos-{region}.volces.com", DefaultRegion: "cn-beijing"},
	{ID: "ks3", Label: "Kingsoft KS3", EndpointTemplate: "ks3-{region}.ksyun.com", DefaultRegion: "cn-beijing-6"},
	{ID: "ucloud", Label: "UCloud US3", EndpointTemplate: "ufile-{region}.ucloud.cn", DefaultRegion: "cn-bj2"},
	// custom 默认 path-style：自定义端点常见于自建 MinIO / 内网 IP，
	// 这些场景几乎没有泛域名解析，虚拟主机风格（bucket.endpoint）会 DNS 失败。
	{ID: "custom", Label: "Custom (S3-compatible)", EndpointTemplate: "", DefaultRegion: "us-east-1", PathStyle: true},
}

// FindS3Provider 按 ID 查预设；未知 ID 回退到 custom（保持向后兼容：
// 老配置没有 provider 字段时也能用裸参数继续跑）。
func FindS3Provider(id string) S3Provider {
	for _, p := range S3Providers {
		if p.ID == id {
			return p
		}
	}
	return S3Providers[len(S3Providers)-1] // custom
}

// ResolveEndpoint 按 provider 模板拼出最终 endpoint。
// 用户手填的 endpoint 优先；模板里的 {region} 用实际 region 替换。
func ResolveEndpoint(provider, endpoint, region string) string {
	if endpoint = strings.TrimSpace(endpoint); endpoint != "" {
		return strings.TrimSuffix(endpoint, "/")
	}
	p := FindS3Provider(provider)
	if p.EndpointTemplate == "" {
		return ""
	}
	return strings.ReplaceAll(p.EndpointTemplate, "{region}", region)
}

// ResolveRegion 用户没填 region 时用 provider 默认值。
func ResolveRegion(provider, region string) string {
	if region = strings.TrimSpace(region); region != "" {
		return region
	}
	return FindS3Provider(provider).DefaultRegion
}
