package api_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kees/atlas/internal/auth"
)

// A 1x1 lossless WebP.
const tinyWebP = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, 0, color.RGBA{R: 200, A: 255})
	}
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func upload(e env, key, filename string, data []byte, mut func(*http.Request)) *httptest.ResponseRecorder {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", filename)
	_, _ = part.Write(data)
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/api/v1/items/"+key+"/attachments", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	mut(r)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func TestAttachmentUploadValidationAndAccess(t *testing.T) {
	e := setup(t)
	cookie, csrf := e.login(t)
	asUser := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set(auth.CSRFHeader, csrf) }
	for _, k := range []string{"EXO", "OTH"} {
		e.do("POST", "/api/v1/projects", map[string]any{"key": k, "name": k}, asUser)
		e.do("POST", "/api/v1/items", map[string]any{"project": k, "type": "task", "title": k}, asUser)
	}
	_, read, _ := e.auth.CreateToken(1, "ro", []string{auth.ScopeRead}, nil, nil)
	_, other, _ := e.auth.CreateToken(1, "oth", []string{auth.ScopeRead, auth.ScopeWrite}, []string{"OTH"}, nil)
	as := func(tok string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
	}

	// Accepted formats; a path in the file name is reduced to its base name.
	w := upload(e, "EXO-1", "../../shot.png", pngBytes(t, 1000, 500), asUser)
	if w.Code != 201 {
		t.Fatalf("png: %d %s", w.Code, w.Body)
	}
	att := decodeBody[struct {
		ID            int
		Filename      string
		Width, Height int
	}](t, w)
	if att.Filename != "shot.png" || att.Width != 1000 || att.Height != 500 {
		t.Fatalf("attachment: %+v", att)
	}
	var jb bytes.Buffer
	_ = jpeg.Encode(&jb, image.NewRGBA(image.Rect(0, 0, 10, 10)), nil)
	webp, _ := base64.StdEncoding.DecodeString(tinyWebP)
	for name, data := range map[string][]byte{"a.jpg": jb.Bytes(), "b.webp": webp} {
		if w := upload(e, "EXO-1", name, data, asUser); w.Code != 201 {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body)
		}
	}

	// Rejected: extension, content that does not match the extension, not an image, pixel and size limits.
	big := append(pngBytes(t, 10, 10), bytes.Repeat([]byte{0}, 300<<10)...)
	for name, c := range map[string]struct {
		file string
		data []byte
		want int
	}{
		"gif extension":  {"a.gif", pngBytes(t, 5, 5), 400},
		"png as jpg":     {"a.jpg", pngBytes(t, 5, 5), 400},
		"text as png":    {"a.png", []byte("not an image at all"), 400},
		"too many pixel": {"a.png", pngBytes(t, 1001, 1001), 413},
		"too many bytes": {"a.png", big, 413},
	} {
		if w := upload(e, "EXO-1", c.file, c.data, asUser); w.Code != c.want {
			t.Errorf("%s: got %d want %d (%s)", name, w.Code, c.want, w.Body)
		}
	}
	if w := upload(e, "EXO-1", "a.png", pngBytes(t, 5, 5), as(read)); w.Code != 403 {
		t.Fatalf("read-only token uploaded: %d", w.Code)
	}

	// Download is an authorised stream with nosniff; a token without access sees nothing.
	w = e.do("GET", "/api/v1/attachments/1", nil, asUser)
	if w.Code != 200 || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("download: %d %v", w.Code, w.Header())
	}
	if w := e.do("GET", "/api/v1/attachments/1", nil, as(other)); w.Code != 404 {
		t.Fatalf("attachment of another project: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/attachments/1", nil, as(read)); w.Code != 200 {
		t.Fatalf("read token download: %d", w.Code)
	}

	// Thumbnail: JPEG, longest edge at most 320 px.
	w = e.do("GET", "/api/v1/attachments/1/thumb", nil, asUser)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumb: %d %v", w.Code, w.Header())
	}
	cfg, err := jpeg.DecodeConfig(w.Body)
	if err != nil || cfg.Width != 320 || cfg.Height != 160 {
		t.Fatalf("thumb size: %+v %v", cfg, err)
	}
}
