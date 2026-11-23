package service

import "testing"

func TestResolveEndpoint(t *testing.T) {
	cases := []struct {
		provider, endpoint, region, want string
	}{
		{"oss", "", "cn-shanghai", "oss-cn-shanghai.aliyuncs.com"},
		{"oss", "oss-cn-shanghai-internal.aliyuncs.com", "cn-shanghai", "oss-cn-shanghai-internal.aliyuncs.com"},
		{"cos", "", "ap-beijing", "cos.ap-beijing.myqcloud.com"},
		{"aws", "", "eu-west-1", "s3.eu-west-1.amazonaws.com"},
		{"minio", "http://127.0.0.1:9000", "us-east-1", "http://127.0.0.1:9000"},
		{"", "", "us-east-1", ""}, // custom 无模板，必须手填
	}
	for _, c := range cases {
		if got := ResolveEndpoint(c.provider, c.endpoint, c.region); got != c.want {
			t.Errorf("ResolveEndpoint(%q,%q,%q) = %q, want %q", c.provider, c.endpoint, c.region, got, c.want)
		}
	}
}

func TestResolveRegion(t *testing.T) {
	if got := ResolveRegion("oss", ""); got != "cn-hangzhou" {
		t.Errorf("oss default region = %q", got)
	}
	if got := ResolveRegion("oss", "cn-shanghai"); got != "cn-shanghai" {
		t.Errorf("explicit region overridden: %q", got)
	}
}

func TestProviderPathStyle(t *testing.T) {
	if !FindS3Provider("minio").PathStyle {
		t.Error("minio must use path-style")
	}
	if FindS3Provider("aws").PathStyle {
		t.Error("aws must use virtual-hosted style")
	}
	if !FindS3Provider("unknown").PathStyle {
		t.Error("unknown provider falls back to custom, which must be path-style for self-hosted endpoints")
	}
}
