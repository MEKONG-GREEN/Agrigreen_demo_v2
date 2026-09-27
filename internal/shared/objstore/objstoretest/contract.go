// Package objstoretest là bộ kiểm thử hợp đồng dùng chung cho mọi hiện thực
// objstore.Store: hiện thực mới phải qua Contract.
package objstoretest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

// Contract chạy bộ kiểm thử trên s. prefix tách khóa giữa các lần chạy (bucket dùng chung).
func Contract(t *testing.T, s objstore.Store, prefix string) {
	t.Helper()
	ctx := context.Background()
	body := []byte("bản mã giả lập — kho chỉ nhận ciphertext (R-40)")
	sum := objstore.SHA256Hex(body)
	key := prefix + "/evidence/a.bin"

	t.Run("put_get_head", func(t *testing.T) {
		info, err := s.Put(ctx, key, bytes.NewReader(body), int64(len(body)), sum, "application/octet-stream")
		if err != nil {
			t.Fatal(err)
		}
		if info.SHA256 != sum || info.Size != int64(len(body)) {
			t.Fatalf("info sai: %+v", info)
		}
		h, err := s.Head(ctx, key)
		if err != nil || h.SHA256 != sum || h.Size != int64(len(body)) {
			t.Fatalf("head: %+v %v", h, err)
		}
		rc, _, err := s.Get(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("get trả nội dung khác: %v", err)
		}
	})

	t.Run("put_same_content_is_idempotent", func(t *testing.T) {
		info, err := s.Put(ctx, key, bytes.NewReader(body), int64(len(body)), strings.ToUpper(sum), "")
		if err != nil || info.SHA256 != sum {
			t.Fatalf("ghi lặp cùng nội dung phải thành công: %+v %v", info, err)
		}
	})

	t.Run("no_overwrite", func(t *testing.T) {
		other := []byte("nội dung khác")
		_, err := s.Put(ctx, key, bytes.NewReader(other), int64(len(other)), objstore.SHA256Hex(other), "")
		if !errors.Is(err, objstore.ErrExists) {
			t.Fatalf("ghi đè phải bị từ chối bằng ErrExists, nhận %v", err)
		}
		h, _ := s.Head(ctx, key)
		if h.SHA256 != sum {
			t.Fatal("đối tượng cũ bị thay đổi")
		}
	})

	t.Run("checksum_mismatch_creates_nothing", func(t *testing.T) {
		k := prefix + "/evidence/bad.bin"
		_, err := s.Put(ctx, k, bytes.NewReader(body), int64(len(body)), objstore.SHA256Hex([]byte("x")), "")
		if !errors.Is(err, objstore.ErrChecksumMismatch) {
			t.Fatalf("muốn ErrChecksumMismatch, nhận %v", err)
		}
		if _, err := s.Head(ctx, k); !errors.Is(err, objstore.ErrNotFound) {
			t.Fatalf("không được có đối tượng sau khi sai băm, nhận %v", err)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		if _, err := s.Head(ctx, prefix+"/none"); !errors.Is(err, objstore.ErrNotFound) {
			t.Fatalf("muốn ErrNotFound, nhận %v", err)
		}
		if _, _, err := s.Get(ctx, prefix+"/none"); !errors.Is(err, objstore.ErrNotFound) {
			t.Fatalf("muốn ErrNotFound, nhận %v", err)
		}
	})

	t.Run("invalid_keys", func(t *testing.T) {
		for _, k := range []string{"", "/abs", "a/../b", "a//b", "a/./b", `a\b`, "a b"} {
			if _, err := s.Put(ctx, k, bytes.NewReader(body), int64(len(body)), sum, ""); !errors.Is(err, objstore.ErrInvalidKey) {
				t.Errorf("khóa %q phải bị từ chối, nhận %v", k, err)
			}
		}
	})

	t.Run("presign_ttl", func(t *testing.T) {
		if _, err := s.PresignGet(ctx, key, time.Hour); !errors.Is(err, objstore.ErrInvalidTTL) {
			t.Fatalf("TTL quá dài phải bị từ chối, nhận %v", err)
		}
		if _, err := s.PresignGet(ctx, key, 0); !errors.Is(err, objstore.ErrInvalidTTL) {
			t.Fatalf("TTL 0 phải bị từ chối, nhận %v", err)
		}
		u, err := s.PresignGet(ctx, key, 5*time.Minute)
		if err != nil || u == "" {
			t.Fatalf("presign: %q %v", u, err)
		}
	})

	t.Run("concurrent_same_key", func(t *testing.T) {
		k := prefix + "/evidence/race.bin"
		var wg sync.WaitGroup
		errs := make([]error, 8)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = s.Put(ctx, k, bytes.NewReader(body), int64(len(body)), sum, "")
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("ghi song song cùng nội dung phải đều thành công: %v", err)
			}
		}
	})

	// Không có Delete trong interface: kiểm ở mức kiểu.
	t.Run("no_delete_method", func(t *testing.T) {
		if _, ok := s.(interface {
			Delete(context.Context, string) error
		}); ok {
			t.Fatal("Store không được có Delete — minh chứng không bị xóa qua ứng dụng")
		}
	})
}
