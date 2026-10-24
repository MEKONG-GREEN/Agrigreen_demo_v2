package objstore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config là cấu hình kho S3 tương thích (MinIO, S3 trong lãnh thổ — RB-7).
type S3Config struct {
	Endpoint string // host:port, không có scheme
	// PublicEndpoint chỉ dùng để KÝ URL tải; I/O và kiểm bucket vẫn qua Endpoint nội bộ.
	PublicEndpoint string
	PublicUseTLS   bool
	Bucket         string
	AccessKey      string
	SecretKey      string
	UseTLS         bool
	Region         string
}

// S3ConfigFromEnv đọc AG_S3_ENDPOINT, AG_S3_BUCKET, AG_S3_ACCESS_KEY,
// AG_S3_SECRET_KEY, AG_S3_USE_TLS (mặc định true), AG_S3_REGION (tùy chọn),
// AG_S3_PUBLIC_ENDPOINT và AG_S3_PUBLIC_USE_TLS (mặc định true).
func S3ConfigFromEnv() (S3Config, error) {
	c := S3Config{
		Endpoint:       os.Getenv("AG_S3_ENDPOINT"),
		PublicEndpoint: os.Getenv("AG_S3_PUBLIC_ENDPOINT"),
		PublicUseTLS:   true,
		Bucket:         os.Getenv("AG_S3_BUCKET"),
		AccessKey:      os.Getenv("AG_S3_ACCESS_KEY"),
		SecretKey:      os.Getenv("AG_S3_SECRET_KEY"),
		UseTLS:         true,
		Region:         os.Getenv("AG_S3_REGION"),
	}
	if v := os.Getenv("AG_S3_USE_TLS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("objstore: AG_S3_USE_TLS không hợp lệ: %w", err)
		}
		c.UseTLS = b
	}
	if v := os.Getenv("AG_S3_PUBLIC_USE_TLS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("objstore: AG_S3_PUBLIC_USE_TLS không hợp lệ: %w", err)
		}
		c.PublicUseTLS = b
	}
	if c.Endpoint == "" || c.Bucket == "" || c.AccessKey == "" || c.SecretKey == "" {
		return c, errors.New("objstore: thiếu AG_S3_ENDPOINT, AG_S3_BUCKET, AG_S3_ACCESS_KEY hoặc AG_S3_SECRET_KEY")
	}
	return c, nil
}

// S3 là Store trên S3 tương thích.
//
// Bucket production phải bật versioning + object lock (khóa đối tượng bất
// biến, mục 22) và nằm ở nhà cung cấp trong lãnh thổ (RB-7). Credential của
// ứng dụng chỉ có s3:PutObject, s3:GetObject — không có s3:DeleteObject.
type S3 struct {
	c      *minio.Client
	signer *minio.Client
	bucket string
}

var _ Store = (*S3)(nil)

// metaSHA là khóa user-metadata lưu băm hex (x-amz-meta-ag-sha256).
const metaSHA = "Ag-Sha256"

// NewS3 kết nối và kiểm bucket tồn tại.
func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseTLS,
		Region: cfg.Region,
		// Bắt buộc để gửi x-amz-checksum-sha256 do người gọi tính.
		TrailingHeaders: true,
	})
	if err != nil {
		return nil, fmt.Errorf("objstore: tạo client S3: %w", err)
	}
	ok, err := c.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("objstore: kiểm bucket: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("objstore: bucket %q không tồn tại", cfg.Bucket)
	}
	signer := c
	if cfg.PublicEndpoint != "" {
		// Region is resolved through the internal client, never through the public
		// endpoint. The latter may be unreachable from the container network.
		region := cfg.Region
		if region == "" {
			region, err = c.GetBucketLocation(ctx, cfg.Bucket)
			if err != nil {
				return nil, fmt.Errorf("objstore: đọc vùng bucket để ký URL: %w", err)
			}
		}
		signer, err = minio.New(cfg.PublicEndpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
			Secure: cfg.PublicUseTLS, Region: region, BucketLookup: minio.BucketLookupPath,
		})
		if err != nil {
			return nil, fmt.Errorf("objstore: tạo client ký URL công khai: %w", err)
		}
	}
	return &S3{c: c, signer: signer, bucket: cfg.Bucket}, nil
}

// Put gửi kèm x-amz-checksum-sha256 để máy chủ tự từ chối nội dung sai băm
// trước khi đối tượng xuất hiện, và If-None-Match: * để không ghi đè.
func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, sha256Hex, contentType string) (Info, error) {
	if err := ValidateKey(key); err != nil {
		return Info{}, err
	}
	want, err := normalizeSHA(sha256Hex)
	if err != nil {
		return Info{}, err
	}
	if size < 0 {
		return Info{}, errors.New("objstore: size âm")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if cur, err := s.Head(ctx, key); err == nil {
		return sameOrExists(cur, want)
	} else if !errors.Is(err, ErrNotFound) {
		return Info{}, err
	}
	raw, _ := hex.DecodeString(want)
	opts := minio.PutObjectOptions{
		ContentType: contentType,
		UserMetadata: map[string]string{
			"x-amz-checksum-sha256": base64.StdEncoding.EncodeToString(raw),
			metaSHA:                 want,
		},
		DisableMultipart:     true, // checksum toàn đối tượng; minh chứng không vượt 5 GiB
		DisableContentSha256: true,
	}
	opts.SetMatchETagExcept("*")
	h := sha256.New()
	_, err = s.c.PutObject(ctx, s.bucket, key, io.TeeReader(r, h), size, opts)
	if err != nil {
		resp := minio.ToErrorResponse(err)
		switch {
		case resp.StatusCode == http.StatusPreconditionFailed:
			cur, herr := s.Head(ctx, key)
			if herr != nil {
				return Info{}, herr
			}
			return sameOrExists(cur, want)
		case resp.Code == "BadDigest" || resp.Code == "XAmzContentChecksumMismatch" || resp.Code == "InvalidDigest":
			return Info{}, ErrChecksumMismatch
		}
		if hex.EncodeToString(h.Sum(nil)) != want {
			return Info{}, ErrChecksumMismatch
		}
		return Info{}, fmt.Errorf("objstore: put: %w", err)
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		// Máy chủ không kiểm checksum (không đạt yêu cầu nhà cung cấp): đối tượng
		// đã ghi nhưng bị coi là hỏng. Không xóa được qua ứng dụng — báo lỗi để vận hành xử lý.
		return Info{}, fmt.Errorf("%w (máy chủ đã nhận đối tượng %s: nhà cung cấp không kiểm x-amz-checksum-sha256)", ErrChecksumMismatch, key)
	}
	return Info{Key: key, Size: size, SHA256: want, ContentType: contentType}, nil
}

// Head đọc siêu dữ liệu.
func (s *S3) Head(ctx context.Context, key string) (Info, error) {
	if err := ValidateKey(key); err != nil {
		return Info{}, err
	}
	st, err := s.c.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
			return Info{}, ErrNotFound
		}
		return Info{}, fmt.Errorf("objstore: head: %w", err)
	}
	return statInfo(key, st), nil
}

func statInfo(key string, st minio.ObjectInfo) Info {
	sha := st.UserMetadata[metaSHA]
	if sha == "" {
		for k, v := range st.UserMetadata {
			if strings.EqualFold(k, metaSHA) {
				sha = v
			}
		}
	}
	if sha == "" && st.ChecksumSHA256 != "" {
		if b, err := base64.StdEncoding.DecodeString(st.ChecksumSHA256); err == nil {
			sha = hex.EncodeToString(b)
		}
	}
	return Info{Key: key, Size: st.Size, SHA256: strings.ToLower(sha), ContentType: st.ContentType}
}

// Get mở đối tượng.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	info, err := s.Head(ctx, key)
	if err != nil {
		return nil, Info{}, err
	}
	obj, err := s.c.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, Info{}, fmt.Errorf("objstore: get: %w", err)
	}
	return obj, info, nil
}

// PresignGet cấp URL GET có chữ ký SigV4, hạn tối đa MaxPresignTTL.
func (s *S3) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if err := validateTTL(ttl); err != nil {
		return "", err
	}
	if _, err := s.Head(ctx, key); err != nil {
		return "", err
	}
	u, err := s.signer.PresignedGetObject(ctx, s.bucket, key, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("objstore: presign: %w", err)
	}
	return u.String(), nil
}
