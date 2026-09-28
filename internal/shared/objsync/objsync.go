// Package objsync sao chép gia tăng kho đối tượng sang kho sao lưu kèm manifest
// checksum (mục 22: "kiểm kê + sao chép gia tăng theo phiên bản và checksum,
// sang vùng thứ hai trong lãnh thổ"), phục hồi ngược lại và kiểm checksum sau
// phục hồi. Dùng bởi cmd/objsync và cmd/drill.
//
// Nguyên tắc:
//
//   - Băm luôn tính lại từ NỘI DUNG khi sao chép (không tin siêu dữ liệu); lệch
//     với băm khai báo của nguồn là dấu hiệu hỏng dữ liệu → dừng, báo lỗi.
//   - Kho đích ghi một lần (objstore): đối tượng đã có cùng băm → bỏ qua (gia
//     tăng); khác băm → lỗi objstore.ErrExists, KHÔNG ghi đè.
//   - Manifest ghi vào kho sao lưu dưới manifests/objects/<UTC>.json — cũng ghi
//     một lần; bản mới nhất là khóa lớn nhất theo thứ tự từ điển.
//   - Không chứa dữ liệu cá nhân: chỉ khóa đối tượng, kích thước, băm (R-40:
//     nội dung trong kho đã là bản mã).
package objsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

// ManifestFormat là định danh định dạng manifest kiểm kê.
const ManifestFormat = "ag.objects-manifest.v1"

// ManifestPrefix là tiền tố khóa của manifest trong kho sao lưu.
const ManifestPrefix = "manifests/objects/"

// Entry là một đối tượng trong manifest.
type Entry struct {
	Key    string `json:"key"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest là kiểm kê kho nguồn tại lúc sao lưu.
type Manifest struct {
	Format     string    `json:"format"`
	CreatedAt  time.Time `json:"created_at"`
	Label      string    `json:"label,omitempty"`      // vd tên cụm/bucket nguồn — không có bí mật
	DstPrefix  string    `json:"dst_prefix,omitempty"` // tiền tố của bản sao trong kho sao lưu
	Count      int       `json:"count"`
	TotalBytes int64     `json:"total_bytes"`
	Entries    []Entry   `json:"entries"`
}

// Options điều chỉnh Backup.
type Options struct {
	SrcPrefix string    // chỉ sao lưu khóa có tiền tố này ("" = tất cả)
	DstPrefix string    // tiền tố trong kho sao lưu, vd "backups/" khi dùng chung bucket
	Label     string    // ghi vào manifest
	Now       time.Time // rỗng → time.Now().UTC()
	SpoolDir  string    // thư mục tệp tạm ("" = os.TempDir())
}

// Result là kết quả một lần sao chép.
type Result struct {
	Copied      int
	Skipped     int
	Bytes       int64
	Manifest    Manifest
	ManifestKey string
}

// Problem là một sai lệch khi kiểm.
type Problem struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// ErrSourceCorrupt: nội dung nguồn không khớp băm khai báo.
var ErrSourceCorrupt = errors.New("objsync: nội dung nguồn không khớp băm khai báo")

// Backup sao chép gia tăng src → dst và ghi manifest vào dst.
func Backup(ctx context.Context, src, dst objstore.Store, o Options) (Result, error) {
	l, ok := src.(objstore.Lister)
	if !ok {
		return Result{}, errors.New("objsync: kho nguồn không liệt kê được (cần objstore.Lister)")
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	res := Result{Manifest: Manifest{Format: ManifestFormat, CreatedAt: now.UTC(), Label: o.Label, DstPrefix: o.DstPrefix, Entries: []Entry{}}}
	err := l.List(ctx, o.SrcPrefix, func(info objstore.Info) error {
		// Dùng chung bucket: không sao lưu chính bản sao lưu và manifest của nó.
		if (o.DstPrefix != "" && strings.HasPrefix(info.Key, o.DstPrefix)) || strings.HasPrefix(info.Key, ManifestPrefix) {
			return nil
		}
		got, copied, err := copyOne(ctx, src, dst, info.Key, o.DstPrefix+info.Key, info.SHA256, o.SpoolDir)
		if err != nil {
			return fmt.Errorf("%s: %w", info.Key, err)
		}
		if copied {
			res.Copied++
			res.Bytes += got.Size
		} else {
			res.Skipped++
		}
		res.Manifest.Entries = append(res.Manifest.Entries, Entry{Key: info.Key, Size: got.Size, SHA256: got.SHA256})
		res.Manifest.TotalBytes += got.Size
		return nil
	})
	if err != nil {
		return res, err
	}
	sort.Slice(res.Manifest.Entries, func(i, j int) bool { return res.Manifest.Entries[i].Key < res.Manifest.Entries[j].Key })
	res.Manifest.Count = len(res.Manifest.Entries)
	body, err := json.MarshalIndent(res.Manifest, "", "  ")
	if err != nil {
		return res, err
	}
	res.ManifestKey = ManifestPrefix + now.UTC().Format("20060102T150405.000000000Z") + ".json"
	if _, err := dst.Put(ctx, res.ManifestKey, bytes.NewReader(body), int64(len(body)), objstore.SHA256Hex(body), "application/json"); err != nil {
		return res, fmt.Errorf("objsync: ghi manifest: %w", err)
	}
	return res, nil
}

// Restore chép mọi đối tượng trong m từ kho sao lưu backup về dst, kiểm băm
// từng đối tượng theo manifest.
func Restore(ctx context.Context, backup, dst objstore.Store, m Manifest, spoolDir string) (Result, error) {
	res := Result{Manifest: m}
	for _, e := range m.Entries {
		got, copied, err := copyOne(ctx, backup, dst, m.DstPrefix+e.Key, e.Key, e.SHA256, spoolDir)
		if err != nil {
			return res, fmt.Errorf("%s: %w", e.Key, err)
		}
		if copied {
			res.Copied++
			res.Bytes += got.Size
		} else {
			res.Skipped++
		}
	}
	return res, nil
}

// Verify đọc lại NỘI DUNG từng đối tượng của m trong s (khóa gốc, không tiền
// tố) và so kích thước + SHA-256. Trả danh sách sai lệch; lỗi chỉ khi không đọc được kho.
func Verify(ctx context.Context, s objstore.Store, m Manifest) ([]Problem, error) {
	var out []Problem
	for _, e := range m.Entries {
		rc, _, err := s.Get(ctx, e.Key)
		if errors.Is(err, objstore.ErrNotFound) {
			out = append(out, Problem{Key: e.Key, Reason: "thiếu đối tượng"})
			continue
		}
		if err != nil {
			return out, err
		}
		h := sha256.New()
		n, err := io.Copy(h, rc)
		_ = rc.Close()
		if err != nil {
			return out, fmt.Errorf("objsync: đọc %s: %w", e.Key, err)
		}
		switch sum := hex.EncodeToString(h.Sum(nil)); {
		case n != e.Size:
			out = append(out, Problem{Key: e.Key, Reason: fmt.Sprintf("kích thước %d khác manifest %d", n, e.Size)})
		case sum != e.SHA256:
			out = append(out, Problem{Key: e.Key, Reason: "SHA-256 nội dung khác manifest"})
		}
	}
	return out, nil
}

// ParseManifest đọc và kiểm định dạng manifest.
func ParseManifest(b []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("objsync: manifest hỏng: %w", err)
	}
	if m.Format != ManifestFormat || m.Count != len(m.Entries) {
		return m, errors.New("objsync: manifest sai định dạng")
	}
	for _, e := range m.Entries {
		if objstore.ValidateKey(e.Key) != nil || len(e.SHA256) != 64 || e.Size < 0 {
			return m, fmt.Errorf("objsync: mục manifest không hợp lệ: %q", e.Key)
		}
	}
	return m, nil
}

// ReadManifest đọc manifest theo khóa trong s; key rỗng hoặc "latest" → bản mới nhất.
func ReadManifest(ctx context.Context, s objstore.Store, key string) (Manifest, string, error) {
	if key == "" || key == "latest" {
		key = ""
		l, ok := s.(objstore.Lister)
		if !ok {
			return Manifest{}, "", errors.New("objsync: kho không liệt kê được để tìm manifest mới nhất")
		}
		if err := l.List(ctx, ManifestPrefix, func(i objstore.Info) error {
			if strings.HasSuffix(i.Key, ".json") && i.Key > key {
				key = i.Key
			}
			return nil
		}); err != nil {
			return Manifest{}, "", err
		}
		if key == "" {
			return Manifest{}, "", fmt.Errorf("objsync: không có manifest dưới %s", ManifestPrefix)
		}
	}
	rc, _, err := s.Get(ctx, key)
	if err != nil {
		return Manifest{}, key, err
	}
	defer rc.Close() //nolint:errcheck // chỉ đọc
	b, err := io.ReadAll(io.LimitReader(rc, 256<<20))
	if err != nil {
		return Manifest{}, key, err
	}
	m, err := ParseManifest(b)
	return m, key, err
}

// copyOne chép srcKey → dstKey qua tệp tạm, tính băm từ nội dung. want là băm
// khai báo (manifest hoặc siêu dữ liệu nguồn), có thể rỗng. Trả Info của bản ở
// đích và cho biết đã chép (false = đích đã có cùng nội dung).
func copyOne(ctx context.Context, src, dst objstore.Store, srcKey, dstKey, want, spoolDir string) (objstore.Info, bool, error) {
	cur, err := dst.Head(ctx, dstKey)
	switch {
	case err == nil && want != "" && cur.SHA256 == want:
		return cur, false, nil
	case err != nil && !errors.Is(err, objstore.ErrNotFound):
		return objstore.Info{}, false, err
	}
	exists := err == nil

	rc, info, err := src.Get(ctx, srcKey)
	if err != nil {
		return objstore.Info{}, false, err
	}
	defer rc.Close() //nolint:errcheck // chỉ đọc
	f, err := os.CreateTemp(spoolDir, "objsync-*")
	if err != nil {
		return objstore.Info{}, false, err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), rc)
	if err != nil {
		return objstore.Info{}, false, fmt.Errorf("objsync: đọc nguồn: %w", err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if want != "" && sum != want {
		return objstore.Info{}, false, ErrSourceCorrupt
	}
	if exists {
		if cur.SHA256 == sum {
			return cur, false, nil
		}
		return objstore.Info{}, false, objstore.ErrExists
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return objstore.Info{}, false, err
	}
	got, err := dst.Put(ctx, dstKey, f, n, sum, info.ContentType)
	if err != nil {
		return objstore.Info{}, false, err
	}
	return got, true, nil
}
