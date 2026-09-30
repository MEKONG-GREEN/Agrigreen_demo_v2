// Package adapter: hiện thực port.LedgerAdapter của mkg/acctbridge. Nhà cung cấp phần mềm kế toán
// CHƯA CHỐT (MISA/FAST/Bravo… — chờ khách): G4 có bộ xuất tệp CSV + XML tất định (kho đối tượng trong
// lãnh thổ, RB-7), bộ giả cho kiểm thử, và bộ "chưa cấu hình" (luôn tạm không nhận → thử lại + cảnh
// báo). Hợp đồng cho bộ điều hợp của nhà cung cấp thật: docs/dev/acctbridge-adapter.md.
package adapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

// Tên bộ điều hợp (tên + phiên bản định dạng — ghi vào push_attempt, sự kiện journal.pushed).
const (
	NameFileExport   = "file_export.v1"
	NameFake         = "fake.v1"
	NameUnconfigured = "unconfigured"
)

// validate là kiểm chung trước khi đẩy: bút toán phải cân và có khóa idempotency.
func validate(j port.SummaryJournal) error {
	if j.IdempotencyKey == "" {
		return fmt.Errorf("%w: thiếu IdempotencyKey", port.ErrAdapterRejected)
	}
	if err := domain.CheckBalanced(j.Lines); err != nil {
		return fmt.Errorf("%w: %w", port.ErrAdapterRejected, err)
	}
	if !j.TotalDebit.Equal(j.TotalCredit) {
		return fmt.Errorf("%w: %w", port.ErrAdapterRejected, port.ErrUnbalanced)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Bộ xuất tệp
// ---------------------------------------------------------------------------

// FileExport ghi bút toán thành ba đối tượng ghi-một-lần ở kho đối tượng trong lãnh thổ:
//
//	<prefix>/<tenant_id>/<period_code>_<journal_id>.csv
//	<prefix>/<tenant_id>/<period_code>_<journal_id>.xml
//	<prefix>/<tenant_id>/<period_code>_<journal_id>.sha256   (tệp ký, định dạng sha256sum)
//
// Nội dung tất định theo SummaryJournal nên đẩy lại cùng bút toán ghi đúng các byte cũ (objstore.Put
// cùng băm → trả Info cũ): idempotent, cùng ExternalRef. Kế toán tải tệp về nhập vào phần mềm (thủ công,
// tới khi có bộ điều hợp của nhà cung cấp).
type FileExport struct {
	Store  objstore.Store
	Prefix string // mặc định "acctbridge"
	Clock  clock.Clock
}

// Name trả tên + phiên bản bộ điều hợp.
func (f *FileExport) Name() string { return NameFileExport }

// Push ghi tệp; lỗi kho → ErrAdapterUnavailable (thử lại), nội dung khác dưới cùng khóa → ErrAdapterRejected.
func (f *FileExport) Push(ctx context.Context, j port.SummaryJournal) (port.ExternalRef, error) {
	if err := validate(j); err != nil {
		return port.ExternalRef{}, err
	}
	if f.Store == nil {
		return port.ExternalRef{}, fmt.Errorf("%w: chưa cấu hình kho đối tượng", port.ErrAdapterUnavailable)
	}
	prefix := strings.Trim(f.Prefix, "/")
	if prefix == "" {
		prefix = "acctbridge"
	}
	stem := domain.FileStem(j)
	key := prefix + "/" + j.TenantID.String() + "/" + stem
	files := map[string][]byte{stem + ".csv": domain.RenderCSV(j), stem + ".xml": domain.RenderXML(j)}
	manifest := domain.Manifest(files)
	put := func(name string, body []byte, ct string) error {
		_, err := f.Store.Put(ctx, prefix+"/"+j.TenantID.String()+"/"+name, bytes.NewReader(body), int64(len(body)), domain.SHA256Hex(body), ct)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, objstore.ErrExists), errors.Is(err, objstore.ErrInvalidKey):
			return fmt.Errorf("%w: %s: %w", port.ErrAdapterRejected, name, err)
		default:
			return fmt.Errorf("%w: %s: %w", port.ErrAdapterUnavailable, name, err)
		}
	}
	if err := put(stem+".csv", files[stem+".csv"], "text/csv; charset=utf-8"); err != nil {
		return port.ExternalRef{}, err
	}
	if err := put(stem+".xml", files[stem+".xml"], "application/xml; charset=utf-8"); err != nil {
		return port.ExternalRef{}, err
	}
	// Tệp ký ghi CUỐI: có tệp ký = đủ hai tệp dữ liệu.
	if err := put(stem+".sha256", manifest, "text/plain; charset=utf-8"); err != nil {
		return port.ExternalRef{}, err
	}
	return port.ExternalRef{Adapter: NameFileExport, Ref: key, ArtifactSHA256: domain.SHA256Hex(manifest), AcceptedAt: now(f.Clock)}, nil
}

func now(c clock.Clock) time.Time {
	if c == nil {
		return clock.System{}.Now()
	}
	return c.Now()
}

// ---------------------------------------------------------------------------
// Bộ giả (kiểm thử, dev)
// ---------------------------------------------------------------------------

// Fake nhớ bút toán đã nhận theo IdempotencyKey (đẩy lại → cùng ExternalRef) và cho kịch bản lỗi.
type Fake struct {
	Clock clock.Clock

	mu    sync.Mutex
	refs  map[string]port.ExternalRef
	fail  []error
	calls int
	got   []port.SummaryJournal
}

// NewFake tạo bộ giả rỗng.
func NewFake(c clock.Clock) *Fake { return &Fake{Clock: c, refs: map[string]port.ExternalRef{}} }

// Name trả tên bộ giả.
func (f *Fake) Name() string { return NameFake }

// FailNext xếp lỗi cho các lần Push kế tiếp (theo thứ tự) — vd port.ErrAdapterUnavailable.
func (f *Fake) FailNext(errs ...error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = append(f.fail, errs...)
}

// Calls là số lần Push được gọi.
func (f *Fake) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Accepted là số bút toán khác nhau đã nhận (theo IdempotencyKey).
func (f *Fake) Accepted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.refs)
}

// Received trả các bút toán đã nhận thành công (theo thứ tự nhận).
func (f *Fake) Received() []port.SummaryJournal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]port.SummaryJournal(nil), f.got...)
}

// Push nhận bút toán (idempotent theo IdempotencyKey).
func (f *Fake) Push(_ context.Context, j port.SummaryJournal) (port.ExternalRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.fail) > 0 {
		err := f.fail[0]
		f.fail = f.fail[1:]
		return port.ExternalRef{}, err
	}
	if err := validate(j); err != nil {
		return port.ExternalRef{}, err
	}
	if r, ok := f.refs[j.IdempotencyKey]; ok {
		return r, nil
	}
	r := port.ExternalRef{Adapter: NameFake, Ref: "FAKE-" + domain.SHA256Hex([]byte(j.IdempotencyKey))[:16], AcceptedAt: now(f.Clock)}
	f.refs[j.IdempotencyKey] = r
	f.got = append(f.got, j)
	return r, nil
}

// ---------------------------------------------------------------------------
// Chưa cấu hình
// ---------------------------------------------------------------------------

// Unconfigured luôn trả ErrAdapterUnavailable: bút toán đã duyệt được giữ, thử lại có giãn cách và cuối
// cùng vào DLQ + cảnh báo — không bao giờ "nhận giả" ở môi trường thật.
type Unconfigured struct{ Reason string }

// Name trả tên.
func (u Unconfigured) Name() string { return NameUnconfigured }

// Push luôn tạm không nhận.
func (u Unconfigured) Push(context.Context, port.SummaryJournal) (port.ExternalRef, error) {
	return port.ExternalRef{}, fmt.Errorf("%w: bộ điều hợp chưa cấu hình (%s)", port.ErrAdapterUnavailable, u.Reason)
}

var (
	_ port.LedgerAdapter = (*FileExport)(nil)
	_ port.LedgerAdapter = (*Fake)(nil)
	_ port.LedgerAdapter = Unconfigured{}
)
