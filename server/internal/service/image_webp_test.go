package service

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestConvertToWebP(t *testing.T) {
	// 用一张大面积纯色图：PNG 体积极小，WebP 编码后可能更大，
	// 此时策略是保留原图——这正是我们要验证的"转换失败/变大不破坏原文件"分支。
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 128, A: 255})
		}
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "src.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()
	before, _ := os.ReadFile(p)

	ext := convertToWebP(p, "image", ".png")
	after, _ := os.ReadFile(p)

	if ext != ".png" && ext != ".webp" {
		t.Fatalf("扩展名不合法: %q", ext)
	}
	if len(after) == 0 {
		t.Fatal("转换后文件不应为空")
	}
	if ext == ".png" && string(before) != string(after) {
		t.Fatal("保留原图时文件内容不应被改动")
	}
	if ext == ".webp" && string(after[:4]) != "RIFF" {
		t.Fatalf("转为 WebP 后文件头应为 RIFF: %q", after[:4])
	}

	// GIF 动图必须跳过
	gifPath := filepath.Join(dir, "src.gif")
	if err := os.WriteFile(gifPath, []byte("GIF89a..."), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := convertToWebP(gifPath, "image", ".gif"); got != ".gif" {
		t.Fatalf("GIF 不应转换: %q", got)
	}

	// 非图片 kind 必须跳过
	if got := convertToWebP(p, "file", ".png"); got != ".png" {
		t.Fatalf("非图片 kind 不应转换: %q", got)
	}
}
