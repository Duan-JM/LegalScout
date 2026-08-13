// Package capture turns browser PNGs into named, watermarked deliverables.
package capture

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Duan-JM/LegalScout/internal/domain"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

//go:embed assets/NotoSansSC-Regular.otf
var embeddedFont []byte

type Request struct {
	ProjectPath string
	Sequence    int
	Subject     string
	Source      string
	Status      domain.CheckStatus
	PNG         []byte
	Now         time.Time
}

func statusName(status domain.CheckStatus) string {
	switch status {
	case domain.NotFound:
		return "无记录"
	case domain.Found:
		return "发现记录"
	default:
		return string(status)
	}
}

var unsafeName = regexp.MustCompile(`[\\/:*?"<>|]+`)

func filesystemName(value string) string {
	value = unsafeName.ReplaceAllString(strings.TrimSpace(value), "_")
	value = strings.Trim(value, ". ")
	if value == "" {
		return "未命名"
	}
	return value
}

func Filename(source string, status domain.CheckStatus, now time.Time) string {
	return fmt.Sprintf("%s-%s-%s.png", filesystemName(source), statusName(status), now.Format("20060102-150405"))
}

func relativePath(sequence int, subject, source string, status domain.CheckStatus, now time.Time) string {
	return filepath.Join("截图", fmt.Sprintf("%03d-%s", sequence, filesystemName(subject)), Filename(source, status, now))
}

func Save(request Request) (string, error) {
	if !request.Status.IsConfirmed() {
		return "", fmt.Errorf("only confirmed results may enter the deliverable directory")
	}
	if request.Now.IsZero() {
		request.Now = time.Now()
	}
	relative := relativePath(request.Sequence, request.Subject, request.Source, request.Status, request.Now)
	path := filepath.Join(request.ProjectPath, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	watermark := fmt.Sprintf("%s | %s | %s", request.Subject, request.Source, request.Now.Format("2006-01-02 15:04:05.000 MST"))
	processed, err := Watermark(request.PNG, watermark)
	if err != nil {
		return "", err
	}
	path, err = writeAtomic(path, processed)
	if err != nil {
		return "", err
	}
	relative, err = filepath.Rel(request.ProjectPath, path)
	if err != nil {
		return "", err
	}
	return relative, nil
}

func SaveDiagnostic(projectPath, source, subject string, png []byte, now time.Time) (string, error) {
	if len(png) == 0 {
		return "", nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	name := fmt.Sprintf("%s-%s-%s.png", filesystemName(source), filesystemName(subject), now.Format("20060102-150405"))
	path := filepath.Join(projectPath, "_legalscout", "diagnostics", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	return writeAtomic(path, png)
}

// writeAtomic writes next to the final file, syncs and closes it before an
// atomic rename. A stable numeric suffix prevents a second
// confirmation in the same second from replacing the earlier deliverable.
func writeAtomic(path string, contents []byte) (string, error) {
	directory := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	extension := filepath.Ext(path)
	for suffix := 1; ; suffix++ {
		final := path
		if suffix > 1 {
			final = filepath.Join(directory, fmt.Sprintf("%s-%d%s", base, suffix, extension))
		}
		temp, err := os.CreateTemp(directory, "."+base+".tmp-*")
		if err != nil {
			return "", err
		}
		tempName := temp.Name()
		cleanup := func() {
			_ = temp.Close()
			_ = os.Remove(tempName)
		}
		if err := temp.Chmod(0o600); err != nil {
			cleanup()
			return "", err
		}
		if _, err := temp.Write(contents); err != nil {
			cleanup()
			return "", err
		}
		if err := temp.Sync(); err != nil {
			cleanup()
			return "", err
		}
		if err := temp.Close(); err != nil {
			_ = os.Remove(tempName)
			return "", err
		}
		if _, err := os.Lstat(final); errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(tempName, final); err != nil {
				_ = os.Remove(tempName)
				return "", fmt.Errorf("atomically rename screenshot: %w", err)
			}
			return final, nil
		} else if err == nil {
			_ = os.Remove(tempName)
			continue
		} else {
			_ = os.Remove(tempName)
			return "", fmt.Errorf("inspect screenshot collision: %w", err)
		}
	}
}

func Watermark(pngBytes []byte, text string) ([]byte, error) {
	original, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, fmt.Errorf("decode browser PNG: %w", err)
	}
	bounds := original.Bounds()
	canvas := image.NewRGBA(bounds)
	draw.Draw(canvas, bounds, original, bounds.Min, draw.Src)
	face, err := fontFace(16)
	if err != nil {
		return nil, err
	}
	defer face.Close()
	// A light opaque strip preserves legibility on dark and light sites.
	height := 30
	strip := image.Rect(bounds.Min.X, bounds.Max.Y-height, bounds.Max.X, bounds.Max.Y)
	draw.Draw(canvas, strip, &image.Uniform{C: color.RGBA{255, 255, 255, 210}}, image.Point{}, draw.Over)
	drawer := &font.Drawer{Dst: canvas, Src: image.NewUniform(color.RGBA{20, 20, 20, 255}), Face: face,
		Dot: fixed.P(bounds.Min.X+10, bounds.Max.Y-10)}
	drawer.DrawString(text)
	output := bytes.NewBuffer(nil)
	if err := png.Encode(output, canvas); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

type closeableFace interface {
	font.Face
	Close() error
}

var (
	parsedFont    *opentype.Font
	parsedFontErr error
	loadFontOnce  sync.Once
)

func fontFace(size float64) (closeableFace, error) {
	// The embedded OFL Noto Sans SC font makes Chinese watermarks portable.
	// A local CJK font is still preferred where the operator has one.
	loadFontOnce.Do(func() {
		parsedFont, parsedFontErr = parseFont(embeddedFont)
		for _, candidate := range cjkFontCandidates() {
			data, err := os.ReadFile(candidate)
			if err != nil {
				continue
			}
			if candidateFont, err := parseFont(data); err == nil {
				parsedFont, parsedFontErr = candidateFont, nil
				return
			}
		}
	})
	if parsedFontErr != nil {
		return nil, parsedFontErr
	}
	face, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	return face, nil
}

func parseFont(data []byte) (*opentype.Font, error) {
	if parsed, err := opentype.Parse(data); err == nil {
		return parsed, nil
	}
	collection, err := sfnt.ParseCollection(data)
	if err != nil {
		return nil, err
	}
	return collection.Font(0)
}

func cjkFontCandidates() []string {
	return []string{
		"/Library/Fonts/NotoSansCJKsc-Regular.ttf",
		"/Library/Fonts/Arial Unicode.ttf",
		"/System/Library/Fonts/PingFang.ttc",
		"/System/Library/Fonts/Hiragino Sans GB.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttf",
		filepath.Join(os.Getenv("WINDIR"), "Fonts", "msyh.ttf"),
		filepath.Join(os.Getenv("WINDIR"), "Fonts", "msyh.ttc"),
	}
}
