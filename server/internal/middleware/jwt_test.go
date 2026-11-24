package middleware

import (
	"testing"
)

func TestDeviceSIDStablePerDevice(t *testing.T) {
	const secret = "test-secret-not-for-production"
	sid := DeviceSID(42, secret, "web", "Chrome · Windows", "device-a")
	if sid == "" || len(sid) != 64 {
		t.Fatalf("sid 应为 64 hex，got %q", sid)
	}
	if again := DeviceSID(42, secret, "web", "Chrome · Windows", "device-a"); again != sid {
		t.Fatal("同一设备应生成相同 sid")
	}
	if other := DeviceSID(42, secret, "web", "Chrome · Windows", "device-b"); other == sid {
		t.Fatal("设备 ID 不同必须生成不同 sid")
	}
	if other := DeviceSID(42, secret, "mobile", "Chrome · Windows", ""); other == sid {
		t.Fatal("平台不同必须生成不同 sid")
	}
	if other := DeviceSID(43, secret, "web", "Chrome · Windows", ""); other == sid {
		t.Fatal("用户不同必须生成不同 sid")
	}
}

func TestIssueTokenWithSID(t *testing.T) {
	const secret = "test-secret-not-for-production"
	const sid = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	token, err := IssueTokenWithSID(7, 3, secret, sid)
	if err != nil {
		t.Fatalf("IssueTokenWithSID: %v", err)
	}
	claims, err := ParseToken(token, secret)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if claims.SID != sid {
		t.Fatalf("sid 未写入 JWT: got %q", claims.SID)
	}
	if claims.UID != 7 || claims.TV != 3 {
		t.Fatalf("claims 错误: uid=%d tv=%d", claims.UID, claims.TV)
	}
}
