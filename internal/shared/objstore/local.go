package objstore

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LocalOptions điều chỉnh kho cục bộ.
type LocalOptions struct {
	// PresignSecret ký URL tải về. Rỗng → sinh ngẫu nhiên, URL chỉ dùng được
	// trong vòng đời tiến trình.
	PresignSecret []byte
	// BaseURL là tiền tố URL của Handler, vd http://localhost:8080/_objstore.
	// Rỗng → "local://objstore".
	BaseURL string
	// Now là nguồn thời gian (kiểm thử). Rỗng → time.Now.
	Now func() time.Time
}

// Local là kho đối tượng trên hệ tệp — CHỈ cho Local/Dev và kiểm thử.
//
// Bố cục: <dir>/objects/<key>, <dir>/meta/<key>.json, <dir>/tmp/. Ghi một lần
// bằng os.Link (thất bại nếu đích đã có), nên hai tiến trình ghi cùng khóa
// không đè nhau.
type Local struct {
	dir    string
	secret []byte
	base   string
	now    func() time.Time
}

var _ Store = (*Local)(nil)

type localMeta struct {
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type"`
}

// NewLocal tạo kho cục bộ tại dir.
func NewLocal(dir string, o LocalOptions) (*Local, error) {
	if dir == "" {
		return nil, errors.New("objstore: thiếu thư mục")
	}
	for _, sub := range []string{"objects", "meta", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o750); err != nil { //nolint:gosec // dir là cấu hình vận hành, không phải đầu vào người dùng
			return nil, fmt.Errorf("objstore: tạo thư mục: %w", err)
		}
	}
	l := &Local{dir: dir, secret: o.PresignSecret, base: strings.TrimRight(o.BaseURL, "/"), now: o.Now}
	if len(l.secret) == 0 {
		l.secret = make([]byte, 32)
		if _, err := rand.Read(l.secret); err != nil {
			return nil, err
		}
	}
	if l.base == "" {
		l.base = "local://objstore"
	}
	if l.now == nil {
		l.now = time.Now
	}
	return l, nil
}

func (l *Local) objPath(key string) string {
	return filepath.Join(l.dir, "objects", filepath.FromSlash(key))
}

func (l *Local) metaPath(key string) string {
	return filepath.Join(l.dir, "meta", filepath.FromSlash(key)+".json")
}

// Put ghi một lần, kiểm SHA-256 trước khi đối tượng xuất hiện.
func (l *Local) Put(ctx context.Context, key string, r io.Reader, size int64, sha256Hex, contentType string) (Info, error) {
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
	if cur, err := l.Head(ctx, key); err == nil {
		return sameOrExists(cur, want)
	} else if !errors.Is(err, ErrNotFound) {
		return Info{}, err
	}

	tmp, err := os.CreateTemp(filepath.Join(l.dir, "tmp"), "put-*")
	if err != nil {
		return Info{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck // tệp tạm; đã link thì xóa tên tạm là đúng

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(ctxReader{ctx, r}, size+1))
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Info{}, fmt.Errorf("objstore: ghi tạm: %w", err)
	}
	if n != size {
		return Info{}, fmt.Errorf("objstore: kích thước khai báo %d, nhận %d byte", size, n)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return Info{}, ErrChecksumMismatch
	}

	dst := l.objPath(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return Info{}, err
	}
	if err := os.Link(tmpName, dst); err != nil {
		if errors.Is(err, fs.ErrExist) {
			cur, herr := l.Head(ctx, key)
			if herr != nil {
				return Info{}, herr
			}
			return sameOrExists(cur, want)
		}
		return Info{}, fmt.Errorf("objstore: link: %w", err)
	}
	info := Info{Key: key, Size: size, SHA256: want, ContentType: contentType}
	if err := l.writeMeta(key, info); err != nil {
		return Info{}, err
	}
	return info, nil
}

func sameOrExists(cur Info, want string) (Info, error) {
	if cur.SHA256 == want {
		return cur, nil
	}
	return Info{}, ErrExists
}

func (l *Local) writeMeta(key string, info Info) error {
	b, err := json.Marshal(localMeta{Size: info.Size, SHA256: info.SHA256, ContentType: info.ContentType})
	if err != nil {
		return err
	}
	mp := l.metaPath(key)
	if err := os.MkdirAll(filepath.Dir(mp), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Join(l.dir, "tmp"), "meta-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Meta cũng ghi một lần: mọi người ghi đều mô tả cùng một nội dung đã cố
	// định (đối tượng đã link), nên người đến sau bỏ qua là đúng.
	if err := os.Link(tmp.Name(), mp); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return nil
}

// Head đọc siêu dữ liệu; thiếu tệp meta (tiến trình chết giữa chừng) → tính lại từ nội dung.
func (l *Local) Head(_ context.Context, key string) (Info, error) {
	if err := ValidateKey(key); err != nil {
		return Info{}, err
	}
	st, err := os.Stat(l.objPath(key))
	if errors.Is(err, fs.ErrNotExist) {
		return Info{}, ErrNotFound
	}
	if err != nil {
		return Info{}, err
	}
	if b, err := os.ReadFile(l.metaPath(key)); err == nil {
		var m localMeta
		if err := json.Unmarshal(b, &m); err == nil && m.SHA256 != "" {
			return Info{Key: key, Size: m.Size, SHA256: m.SHA256, ContentType: m.ContentType}, nil
		}
	}
	f, err := os.Open(l.objPath(key))
	if err != nil {
		return Info{}, err
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return Info{}, err
	}
	info := Info{Key: key, Size: st.Size(), SHA256: hex.EncodeToString(h.Sum(nil)), ContentType: "application/octet-stream"}
	return info, l.writeMeta(key, info)
}

// Get mở đối tượng.
func (l *Local) Get(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	info, err := l.Head(ctx, key)
	if err != nil {
		return nil, Info{}, err
	}
	f, err := os.Open(l.objPath(key))
	if err != nil {
		return nil, Info{}, err
	}
	return f, info, nil
}

func (l *Local) sign(key string, exp int64) string {
	m := hmac.New(sha256.New, l.secret)
	m.Write([]byte(key + "\n" + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(m.Sum(nil))
}

// PresignGet cấp URL có chữ ký HMAC, phục vụ bởi Handler.
func (l *Local) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if err := validateTTL(ttl); err != nil {
		return "", err
	}
	if _, err := l.Head(ctx, key); err != nil {
		return "", err
	}
	exp := l.now().Add(ttl).Unix()
	q := url.Values{"exp": {strconv.FormatInt(exp, 10)}, "sig": {l.sign(key, exp)}}
	return l.base + "/" + key + "?" + q.Encode(), nil
}

// Handler phục vụ URL do PresignGet cấp (gắn tại BaseURL, bỏ tiền tố trước khi gọi).
// Chữ ký sai hoặc hết hạn → 403.
func (l *Local) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/")
		exp, err := strconv.ParseInt(r.URL.Query().Get("exp"), 10, 64)
		sig := r.URL.Query().Get("sig")
		if err != nil || ValidateKey(key) != nil || l.now().Unix() > exp ||
			!hmac.Equal([]byte(sig), []byte(l.sign(key, exp))) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		rc, info, err := l.Get(r.Context(), key)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer rc.Close() //nolint:errcheck
		w.Header().Set("Content-Type", info.ContentType)
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
		w.Header().Set("Cache-Control", "private, no-store")
		_, _ = io.Copy(w, rc)
	})
}

// ctxReader dừng đọc khi ctx bị hủy.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
