package capture

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Duan-JM/LegalScout/internal/domain"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	image := image.NewRGBA(image.Rect(0, 0, 100, 50))
	for x := 0; x < 100; x++ {
		for y := 0; y < 50; y++ {
			image.Set(x, y, color.White)
		}
	}
	buffer := bytes.NewBuffer(nil)
	if err := png.Encode(buffer, image); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestFilenameAndWatermarkedDeliverable(t *testing.T) {
	now := time.Date(2026, 8, 13, 9, 30, 0, 0, time.Local)
	name := Filename("证监会政府信息公开", domain.NotFound, now)
	if !strings.Contains(name, "无记录") || !strings.Contains(name, "20260813-093000") {
		t.Fatalf("filename = %q", name)
	}
	root := filepath.Join(".", ".test-capture-"+now.Format("20060102150405"))
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	path, err := Save(Request{ProjectPath: root, Sequence: 1, Subject: "张三", Source: "证监会", Status: domain.NotFound, PNG: tinyPNG(t), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, "截图"+string(os.PathSeparator)+"001-张三") {
		t.Fatalf("deliverable path = %q", path)
	}
	file, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(file)); err != nil {
		t.Fatalf("saved screenshot is not PNG: %v", err)
	}
	if bytes.Equal(file, tinyPNG(t)) {
		t.Fatal("watermark did not alter the screenshot")
	}
}

func TestDiagnosticsNeverWriteToDeliverables(t *testing.T) {
	root := filepath.Join(".", ".test-diagnostic")
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	path, err := SaveDiagnostic(root, "csrc", "张三", tinyPNG(t), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "_legalscout"+string(os.PathSeparator)+"diagnostics") {
		t.Fatalf("diagnostic path = %q", path)
	}
}

func TestSaveDoesNotOverwriteSameSecondAndEmbeddedFontHasChineseGlyph(t *testing.T) {
	now := time.Date(2026, 8, 13, 9, 30, 0, 0, time.Local)
	root := filepath.Join(".", ".test-collision")
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	first, err := Save(Request{ProjectPath: root, Sequence: 1, Subject: "张三", Source: "证监会", Status: domain.Found, PNG: tinyPNG(t), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Save(Request{ProjectPath: root, Sequence: 1, Subject: "张三", Source: "证监会", Status: domain.Found, PNG: tinyPNG(t), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.Contains(second, "-2.png") {
		t.Fatalf("same-second paths = %q / %q", first, second)
	}
	face, err := fontFace(16)
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	advance, ok := face.GlyphAdvance('中')
	if !ok || advance <= 0 {
		t.Fatalf("embedded font lacks Chinese glyph: advance=%v ok=%v", advance, ok)
	}
	watermarked, err := Watermark(tinyPNG(t), "张三 | 证监会")
	if err != nil || bytes.Equal(watermarked, tinyPNG(t)) {
		t.Fatalf("Chinese watermark = %v, changed=%v", err, !bytes.Equal(watermarked, tinyPNG(t)))
	}
}
