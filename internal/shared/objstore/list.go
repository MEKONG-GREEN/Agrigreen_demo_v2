package objstore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/minio/minio-go/v7"
)

// Lister liệt kê đối tượng theo tiền tố — CHỈ cho công cụ vận hành (sao lưu
// gia tăng cmd/objsync, diễn tập phục hồi cmd/drill, mục 22). Ứng dụng nghiệp
// vụ không cần liệt kê nên Store không có phương thức này; công cụ kiểm bằng
// type assertion.
type Lister interface {
	// List gọi fn cho mọi đối tượng có khóa bắt đầu bằng prefix, theo thứ tự
	// khóa tăng dần. Info có Key và Size; SHA256 lấy từ siêu dữ liệu (có thể
	// rỗng với đối tượng không do objstore ghi) — người cần chắc chắn phải đọc
	// nội dung và tự băm. fn trả lỗi → dừng và trả lỗi đó.
	List(ctx context.Context, prefix string, fn func(Info) error) error
}

var (
	_ Lister = (*Local)(nil)
	_ Lister = (*S3)(nil)
)

// List của kho cục bộ: duyệt <dir>/objects.
func (l *Local) List(ctx context.Context, prefix string, fn func(Info) error) error {
	root := filepath.Join(l.dir, "objects")
	var keys []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if strings.HasPrefix(key, prefix) && ValidateKey(key) == nil {
			keys = append(keys, key)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("objstore: liệt kê: %w", err)
	}
	sort.Strings(keys)
	for _, k := range keys {
		info, err := l.Head(ctx, k)
		if err != nil {
			return err
		}
		if err := fn(info); err != nil {
			return err
		}
	}
	return nil
}

// List của S3: ListObjectsV2 đệ quy, rồi HEAD từng đối tượng để lấy băm
// x-amz-meta-ag-sha256 (không phải nhà cung cấp nào cũng trả metadata khi liệt kê).
func (s *S3) List(ctx context.Context, prefix string, fn func(Info) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for obj := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return fmt.Errorf("objstore: liệt kê: %w", obj.Err)
		}
		if strings.HasSuffix(obj.Key, "/") || ValidateKey(obj.Key) != nil {
			continue // "thư mục" giả hoặc khóa không do objstore ghi
		}
		info, err := s.Head(ctx, obj.Key)
		if err != nil {
			return err
		}
		if err := fn(info); err != nil {
			return err
		}
	}
	return nil
}

// FromEnvPrefix như FromEnv nhưng đọc biến có tiền tố p thay cho "AG_", vd
// p = "AG_BACKUP_" → AG_BACKUP_S3_ENDPOINT, AG_BACKUP_S3_BUCKET, …, hoặc
// AG_BACKUP_OBJSTORE_DIR. Dùng cho kho sao lưu (vùng thứ hai, credential riêng — mục 22).
func FromEnvPrefix(ctx context.Context, p string) (Store, error) {
	if p == "AG_" {
		return FromEnv(ctx)
	}
	get := func(k string) string { return os.Getenv(p + k) }
	if get("S3_ENDPOINT") != "" {
		c := S3Config{
			Endpoint: get("S3_ENDPOINT"), Bucket: get("S3_BUCKET"), AccessKey: get("S3_ACCESS_KEY"),
			SecretKey: get("S3_SECRET_KEY"), UseTLS: true, Region: get("S3_REGION"),
		}
		if v := get("S3_USE_TLS"); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("objstore: %sS3_USE_TLS không hợp lệ: %w", p, err)
			}
			c.UseTLS = b
		}
		if c.Bucket == "" || c.AccessKey == "" || c.SecretKey == "" {
			return nil, fmt.Errorf("objstore: thiếu %sS3_BUCKET, %sS3_ACCESS_KEY hoặc %sS3_SECRET_KEY", p, p, p)
		}
		return NewS3(ctx, c)
	}
	if dir := get("OBJSTORE_DIR"); dir != "" {
		return NewLocal(dir, LocalOptions{})
	}
	return nil, errors.New("objstore: chưa cấu hình " + p + "S3_ENDPOINT hoặc " + p + "OBJSTORE_DIR")
}
