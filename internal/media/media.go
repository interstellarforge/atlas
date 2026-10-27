// Package media validates, stores and thumbnails uploaded images. It knows nothing about work items.
package media

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // registers the PNG decoder for image.DecodeConfig and image.Decode
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers the WebP decoder
)

var (
	ErrType     = errors.New("only PNG, JPEG and WebP images are accepted")
	ErrTooLarge = errors.New("image is too large")
	ErrCorrupt  = errors.New("file is not a valid image")
)

// ThumbSize is the longest edge of a thumbnail in pixels.
const ThumbSize = 320

type Limits struct {
	MaxBytes  int64
	MaxPixels int64
}

type Store struct {
	dir    string
	limits Limits
	// thumbMu allows one thumbnail to be generated at a time: decoding a large image costs far more
	// memory than the file size suggests, and concurrent decodes would stack those peaks.
	thumbMu sync.Mutex
}

func NewStore(dir string, l Limits) *Store { return &Store{dir: dir, limits: l} }

type Saved struct {
	Path          string // relative to the store, generated
	Mime          string
	Width, Height int
	Size          int64
}

var extMime = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp"}

// Save copies r to a temporary file while counting bytes, checks size, extension, magic bytes and pixel
// count (reading only the image header), then moves it to a generated name. The upload is never held
// in memory, and nothing is decoded before the limits pass.
func (s *Store) Save(r io.Reader, filename string) (Saved, error) {
	mime, ok := extMime[strings.ToLower(filepath.Ext(filename))]
	if !ok {
		return Saved{}, ErrType
	}
	if err := os.MkdirAll(filepath.Join(s.dir, "tmp"), 0o750); err != nil {
		return Saved{}, err
	}
	tmp, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "upload-*")
	if err != nil {
		return Saved{}, err
	}
	defer os.Remove(tmp.Name()) // a no-op after the rename below
	defer tmp.Close()

	n, err := io.Copy(tmp, io.LimitReader(r, s.limits.MaxBytes+1))
	if err != nil {
		return Saved{}, err
	}
	if n > s.limits.MaxBytes {
		return Saved{}, fmt.Errorf("%w: more than %d bytes", ErrTooLarge, s.limits.MaxBytes)
	}

	head := make([]byte, 512)
	m, _ := tmp.ReadAt(head, 0)
	if http.DetectContentType(head[:m]) != mime {
		return Saved{}, ErrType
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return Saved{}, err
	}
	cfg, _, err := image.DecodeConfig(tmp)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return Saved{}, ErrCorrupt
	}
	if int64(cfg.Width)*int64(cfg.Height) > s.limits.MaxPixels {
		return Saved{}, fmt.Errorf("%w: %d x %d pixels", ErrTooLarge, cfg.Width, cfg.Height)
	}

	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Saved{}, err
	}
	name := hex.EncodeToString(id[:])
	rel := filepath.Join(name[:2], name+extFor(mime))
	if err := os.MkdirAll(filepath.Dir(s.abs(rel)), 0o750); err != nil {
		return Saved{}, err
	}
	if err := os.Rename(tmp.Name(), s.abs(rel)); err != nil {
		return Saved{}, err
	}
	if err := os.Chmod(s.abs(rel), 0o640); err != nil {
		return Saved{}, err
	}
	return Saved{Path: rel, Mime: mime, Width: cfg.Width, Height: cfg.Height, Size: n}, nil
}

func extFor(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	}
	return ".jpg"
}

// abs maps a stored relative path to a file inside the store; ".." cannot escape it.
func (s *Store) abs(rel string) string {
	return filepath.Join(s.dir, filepath.Clean(string(filepath.Separator)+rel))
}

func (s *Store) Open(rel string) (*os.File, error) { return os.Open(s.abs(rel)) }

// Remove deletes a stored file; used to clean up when the database write that follows an upload fails.
func (s *Store) Remove(rel string) { _ = os.Remove(s.abs(rel)) }

// RemoveAll deletes a stored file and its cached thumbnail.
func (s *Store) RemoveAll(rel string) {
	s.Remove(rel)
	_ = os.Remove(filepath.Join(s.dir, "thumbs", strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))+".jpg"))
}

// Thumb returns the path of a JPEG thumbnail (longest edge 320 px), generating and caching it on first use.
func (s *Store) Thumb(rel string) (string, error) {
	out := filepath.Join(s.dir, "thumbs", strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))+".jpg")
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	s.thumbMu.Lock()
	defer s.thumbMu.Unlock()
	if _, err := os.Stat(out); err == nil { // another request finished it while this one waited
		return out, nil
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return "", err
	}
	src, err := s.Open(rel)
	if err != nil {
		return "", err
	}
	defer src.Close()
	img, _, err := image.Decode(src)
	if err != nil {
		return "", ErrCorrupt
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if long := max(w, h); long > ThumbSize {
		w, h = max(1, w*ThumbSize/long), max(1, h*ThumbSize/long)
	}
	// JPEG has no alpha: flatten transparent PNG/WebP onto white.
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)

	tmp, err := os.CreateTemp(filepath.Dir(out), "thumb-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if err := jpeg.Encode(tmp, dst, &jpeg.Options{Quality: 80}); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return out, os.Rename(tmp.Name(), out)
}
