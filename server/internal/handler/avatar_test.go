package handler

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modern-day-team/modern-chat-new/server/internal/service"
)

// PNG 1×1 透明图（合法文件头，Storage.Save 的嗅探会认出 image/png）。
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0xf0,
	0x1f, 0x00, 0x05, 0x00, 0x01, 0xff, 0x9d, 0x7e,
	0x6a, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestStoreAvatarDataURLToLocalStorage(t *testing.T) {
	dir := t.TempDir()
	st, err := service.NewStorage(service.StorageConfig{Kind: "local", DataDir: dir})
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	h := &AuthHandler{storage: st}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(tinyPNG)

	url, err := h.storeAvatarDataURL(dataURL, 512*1024)
	if err != nil {
		t.Fatalf("store avatar: %v", err)
	}
	if !strings.HasPrefix(url, "/uploads/") || !strings.HasSuffix(url, ".png") {
		t.Fatalf("unexpected local URL: %q", url)
	}
	// 文件必须真的落在本地目录（不是留在数据库 data URL）
	rel := strings.TrimPrefix(url, "/uploads/")
	if _, err := os.Stat(filepath.Join(service.UploadsDir(dir), filepath.FromSlash(rel))); err != nil {
		t.Fatalf("file not written: %v", err)
	}
	// Owns 必须认得它，方便后续 PATCH /me 幂等提交 URL
	if !st.Owns(url) {
		t.Fatalf("storage should own %q", url)
	}
}

func TestStoreAvatarDataURLRejectsBadInput(t *testing.T) {
	st, _ := service.NewStorage(service.StorageConfig{Kind: "local", DataDir: t.TempDir()})
	h := &AuthHandler{storage: st}
	if _, err := h.storeAvatarDataURL("not-a-data-url", 512*1024); err == nil {
		t.Error("expected error for malformed data URL")
	}
	if _, err := h.storeAvatarDataURL("data:image/png;base64,@@@", 512*1024); err == nil {
		t.Error("expected error for invalid base64")
	}
	if _, err := (&AuthHandler{}).storeAvatarDataURL("data:image/png;base64,AA==", 512*1024); err == nil {
		t.Error("expected error when storage is nil")
	}
}
