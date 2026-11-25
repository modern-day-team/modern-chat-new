package middleware

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// 不同用途的限流器使用不同键前缀，避免同 IP/UID 在两个接口间串配额。
func TestRedisLimiterNamespaceIsolation(t *testing.T) {
	auth := NewRedisLimiter(nil, 10, "auth")
	migrate := NewRedisLimiter(nil, 120, "migrate")
	if auth.prefix == migrate.prefix {
		t.Fatalf("namespace should isolate prefixes, both %q", auth.prefix)
	}
	if !strings.HasPrefix(auth.prefix, "mc:rl:auth") {
		t.Fatalf("unexpected auth prefix %q", auth.prefix)
	}
	if NewRedisLimiter(nil, 10).prefix != "mc:rl" {
		t.Fatal("no-namespace callers should keep legacy prefix")
	}
}

// 内存工厂按用途返回相互独立的实例：打满一个不影响另一个。
func TestMemoryFactoryIsolatesPurposes(t *testing.T) {
	factory := MemoryLimiterFactory
	auth := factory("auth", 10, 2)
	upload := factory("upload", 10, 2)
	for i := 0; i < 2; i++ {
		if !auth.Allow("ip:1.2.3.4") {
			t.Fatalf("auth request %d should pass", i+1)
		}
		if !upload.Allow("ip:1.2.3.4") {
			t.Fatalf("upload request %d should pass", i+1)
		}
	}
	if auth.Allow("ip:1.2.3.4") {
		t.Fatal("auth should be exhausted")
	}
	if upload.Allow("ip:1.2.3.4") {
		t.Fatal("upload should be exhausted independently")
	}
}

// Redis 限流器的窗口键包含用途前缀与分钟桶，格式稳定供运维排查。
func TestRedisLimiterKeyShape(t *testing.T) {
	l := NewRedisLimiter(nil, 10, "qr")
	bucket := time.Now().Unix() / 60
	key := l.key("ip:127.0.0.1")
	want := "mc:rl:qr:ip:127.0.0.1:" + strconv.FormatInt(bucket, 10)
	if key != want {
		t.Fatalf("key = %q, want %q", key, want)
	}
}
