package service

import (
	"bytes"
	"image"
	"image/draw"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	webp "github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// 服务端图片统一转 WebP：覆盖所有上传来源（前端、移动端、SSO 客户端），
// 前端已有的浏览器端压缩是"提前减重"，这里是兜底统一，二者不冲突。
//
// 保守策略（任何一步失败都保留原图，绝不因转换失败阻断上传）：
//   - 只转静态 JPEG/PNG/WebP；GIF 多帧动画跳过（转 WebP 会丢帧）
//   - 最长边 1600px（与前端 MessageComposer 一致）；更小的图不放大
//   - 编码质量 82；转换后反而更大就保留原图
//   - 解码前检查像素上限，防解压炸弹（沿用 thumbnail.go 的口径）
const (
	webpMaxEdge   = 1600
	webpQuality   = 82
	webpMaxPixels = 40_000_000
)

// convertToWebP 就地转换 tmpPath 指向的图片文件。
// 成功时返回新的扩展名（".webp"）并已把文件内容替换为 WebP 字节；
// 不需要或无法转换时返回原 ext，文件保持不变。
func convertToWebP(tmpPath, kind, ext string) string {
	if kind != "image" && kind != "sticker" {
		return ext
	}
	lower := strings.ToLower(ext)
	if lower == ".gif" || lower == ".webp" {
		// GIF 是动画；已是 WebP 无需二次编码
		return ext
	}
	if lower != ".jpg" && lower != ".jpeg" && lower != ".png" {
		return ext
	}

	defer func() {
		if r := recover(); r != nil {
			slog.Warn("webp conversion panic, keeping original", "file", filepath.Base(tmpPath), "panic", r)
		}
	}()

	f, err := os.Open(tmpPath)
	if err != nil {
		return ext
	}
	cfg, _, err := image.DecodeConfig(f)
	_ = f.Close()
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return ext
	}
	if int64(cfg.Width)*int64(cfg.Height) > webpMaxPixels {
		return ext // 解压炸弹防御
	}

	f, err = os.Open(tmpPath)
	if err != nil {
		return ext
	}
	img, _, err := image.Decode(f)
	_ = f.Close()
	if err != nil {
		return ext
	}

	// 等比缩小到最长边 1600；透明通道（PNG）保留 —— WebP 支持 alpha
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w > webpMaxEdge || h > webpMaxEdge {
		scale := float64(webpMaxEdge) / float64(max(w, h))
		nw, nh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
		img = dst
	}

	var buf bytes.Buffer
	if err := webp.Encode(&buf, img, webp.Options{Quality: webpQuality, Method: 4}); err != nil {
		return ext
	}

	// 原图本身已经很小（前端压过）时，WebP 重编码可能反而变大
	origStat, err := os.Stat(tmpPath)
	if err != nil || buf.Len() >= int(origStat.Size()) {
		return ext
	}
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0o600); err != nil {
		return ext
	}
	slog.Info("image converted to webp", "file", filepath.Base(tmpPath), "from", origStat.Size(), "to", buf.Len())
	return ".webp"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
