package docfile

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

func TestCheckTypeSizeChecksum(t *testing.T) {
	pdf := []byte("%PDF-1.7\n%%EOF")
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	jpg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}
	for _, c := range []struct {
		data []byte
		ct   string
	}{{pdf, TypePDF}, {png, TypePNG}, {jpg, TypeJPEG}} {
		got, err := Check(c.data, "a", objstore.SHA256Hex(c.data))
		if err != nil || got.ContentType != c.ct || got.Size != int64(len(c.data)) || got.SHA256 != objstore.SHA256Hex(c.data) {
			t.Fatalf("%s: %+v %v", c.ct, got, err)
		}
	}
	if _, err := Check(pdf, "a", strings.ToUpper(objstore.SHA256Hex(pdf))); err != nil {
		t.Fatalf("băm chữ hoa vẫn khớp: %v", err)
	}
	cases := map[string]struct {
		data []byte
		sha  string
		want error
	}{
		"rỗng":      {nil, "", ErrEmpty},
		"loại":      {[]byte("<html>"), objstore.SHA256Hex([]byte("<html>")), ErrType},
		"băm sai":   {pdf, objstore.SHA256Hex([]byte("x")), ErrChecksum},
		"thiếu băm": {pdf, "", ErrChecksum},
		"quá cỡ":    {append([]byte("%PDF-"), make([]byte, MaxBytes)...), "", ErrTooLarge},
	}
	for name, c := range cases {
		if _, err := Check(c.data, "a", c.sha); !errors.Is(err, c.want) {
			t.Errorf("%s: %v (muốn %v)", name, err, c.want)
		}
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		`C:\tmp\biên bản "chênh".pdf`: "biên bản chênh.pdf",
		"../../etc/passwd":            "passwd",
		"a\x00b\x1f.png":              "ab.png",
		"":                            "tep",
		"/":                           "tep",
		strings.Repeat("x", 250):      strings.Repeat("x", 200),
	} {
		if got := CleanName(in); got != want {
			t.Errorf("%q → %q (muốn %q)", in, got, want)
		}
	}
}

func TestReadMultipartAndWriteContent(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	w, _ := mw.CreateFormFile("file", "epod.pdf")
	_, _ = w.Write([]byte("%PDF-1.4"))
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	b, name, err := ReadMultipart(r)
	if err != nil || string(b) != "%PDF-1.4" || name != "epod.pdf" {
		t.Fatalf("đọc multipart: %q %q %v", b, name, err)
	}
	r = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	if _, _, err := ReadMultipart(r); !errors.Is(err, ErrMissing) {
		t.Fatalf("thiếu phần file: %v", err)
	}
	rec := httptest.NewRecorder()
	WriteContent(rec, strings.NewReader("%PDF-1.4"), TypePDF, 8, "abc", "biên bản.pdf")
	if rec.Code != 200 || rec.Body.String() != "%PDF-1.4" || rec.Header().Get("X-Content-SHA256") != "abc" ||
		rec.Header().Get("Cache-Control") != "no-store" || !strings.Contains(rec.Header().Get("Content-Disposition"), "bi%C3%AAn%20b%E1%BA%A3n.pdf") {
		t.Fatalf("tải về: %d %v", rec.Code, rec.Header())
	}
	if ObjectKey("mkg/orders", "t", "s") != "mkg/orders/t/s" {
		t.Fatal("khóa đối tượng")
	}
}
