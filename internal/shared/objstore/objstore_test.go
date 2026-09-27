package objstore_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore/objstoretest"
)

func TestLocalContract(t *testing.T) {
	s, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	objstoretest.Contract(t, s, "c")
}

// Chạy với MinIO/S3 thật: đặt AG_TEST_S3_ENDPOINT (+ AG_TEST_S3_BUCKET,
// AG_TEST_S3_ACCESS_KEY, AG_TEST_S3_SECRET_KEY, AG_TEST_S3_USE_TLS).
func TestS3Contract(t *testing.T) {
	ep := os.Getenv("AG_TEST_S3_ENDPOINT")
	if ep == "" {
		t.Skip("chưa đặt AG_TEST_S3_ENDPOINT — bỏ qua kiểm thử S3")
	}
	cfg := objstore.S3Config{
		Endpoint:  ep,
		Bucket:    os.Getenv("AG_TEST_S3_BUCKET"),
		AccessKey: os.Getenv("AG_TEST_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("AG_TEST_S3_SECRET_KEY"),
		UseTLS:    os.Getenv("AG_TEST_S3_USE_TLS") == "true",
	}
	s, err := objstore.NewS3(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	objstoretest.Contract(t, s, "test/"+ids.New().String())
}

func TestLocalPresignedHandler(t *testing.T) {
	// Đồng hồ đọc từ goroutine của máy chủ HTTP: dùng atomic cho -race.
	var now atomic.Int64
	now.Store(time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC).Unix())
	clock := func() time.Time { return time.Unix(now.Load(), 0) }
	s, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{PresignSecret: []byte("k"), BaseURL: "http://x/_obj", Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	body := "ciphertext"
	if _, err := s.Put(ctx, "a/b.bin", strings.NewReader(body), int64(len(body)), objstore.SHA256Hex([]byte(body)), "application/x-ag-envelope"); err != nil {
		t.Fatal(err)
	}
	raw, err := s.PresignGet(ctx, "a/b.bin", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	srv := httptest.NewServer(http.StripPrefix("/_obj", s.Handler()))
	defer srv.Close()

	get := func(path, rawQuery string) (int, string) {
		resp, err := http.Get(srv.URL + path + "?" + rawQuery)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close() //nolint:errcheck
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, b := get(u.Path, u.RawQuery); code != 200 || b != body {
		t.Fatalf("URL hợp lệ: %d %q", code, b)
	}
	if code, _ := get("/_obj/a/other.bin", u.RawQuery); code != http.StatusForbidden {
		t.Fatalf("chữ ký của khóa khác phải bị từ chối: %d", code)
	}
	now.Add(int64((2 * time.Minute).Seconds()))
	if code, _ := get(u.Path, u.RawQuery); code != http.StatusForbidden {
		t.Fatalf("URL hết hạn phải bị từ chối: %d", code)
	}
}
