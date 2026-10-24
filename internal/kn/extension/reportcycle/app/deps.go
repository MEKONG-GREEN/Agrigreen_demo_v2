package app

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
)

// P0-POOL-KN (#125): không đường nào của vòng đời báo cáo được GIỮ một kết nối DB (giao dịch KN đang mở) trong khi
// CHỜ kết nối khác. Port của module khác (pf/reports: phiên bản mẫu, kỳ đã gửi Chi cục; pf/party: khu vực, tên xã)
// mở giao dịch riêng trên CHUNG pool của gốc lắp ráp (ADR-0028) — gọi chúng bên trong s.tx thì N thao tác song song
// với N = cỡ pool giữ hết kết nối và cùng chờ nhau tới hết hạn (treo toàn API).
//
// extDeps gom các giá trị đó, nạp NGOÀI giao dịch rồi "niêm phong" trong giao dịch (txDeps): thiếu phiên bản mẫu /
// sổ tên tỉnh → ghi nhận và hủy giao dịch (errNeedDeps); thiếu tên xã → ghi nhận, giao dịch hủy ở cuối. txDeps nạp
// phần thiếu ngoài giao dịch rồi chạy lại thân giao dịch từ đầu (đọc lại và kiểm lại mọi thứ trong giao dịch mới —
// ví dụ phiên bản mẫu của công việc), tối đa maxDepsAttempts lần.

// errNeedDeps: giao dịch cần dữ liệu module khác chưa nạp — hủy, nạp ngoài giao dịch, chạy lại.
var errNeedDeps = errors.New("reportcycle: cần dữ liệu module khác — nạp ngoài giao dịch rồi chạy lại")

// maxDepsAttempts: số lần chạy thân giao dịch tối đa (lần đầu thường thiếu, lần hai đủ; dư cho dữ liệu đổi giữa chừng).
const maxDepsAttempts = 5

// extDeps là dữ liệu module khác của MỘT thao tác (không dùng chung giữa goroutine).
type extDeps struct {
	s      *Service
	fc     *formCache
	books  map[string]*nameBook
	sealed bool // đang trong giao dịch: không gọi port, thiếu → ghi nhận

	wantForms    []uuid.UUID
	wantBooks    []string
	wantCommunes map[string][]string // tỉnh (sổ chưa nạp) → mã xã cần tên
}

func (s *Service) newDeps(tid uuid.UUID) *extDeps {
	return &extDeps{s: s, fc: s.forms(tid), books: map[string]*nameBook{}, wantCommunes: map[string][]string{}}
}

// form trả phiên bản mẫu đã phân tích; trong giao dịch chưa nạp → errNeedDeps.
func (d *extDeps) form(ctx context.Context, versionID uuid.UUID) (formInfo, error) {
	if fi, ok := d.fc.m[versionID]; ok {
		return fi, nil
	}
	if d.sealed {
		d.addForm(versionID)
		return formInfo{}, errNeedDeps
	}
	return d.fc.get(ctx, versionID)
}

// book trả sổ khu vực + tên xã của tỉnh; trong giao dịch chưa nạp → errNeedDeps.
func (d *extDeps) book(ctx context.Context, province string) (*nameBook, error) {
	if nb, ok := d.books[province]; ok {
		return nb, nil
	}
	if d.sealed {
		if !slices.Contains(d.wantBooks, province) {
			d.wantBooks = append(d.wantBooks, province)
		}
		return nil, errNeedDeps
	}
	nb, err := d.s.regionNames(ctx, province)
	if err != nil {
		return nil, err
	}
	nb.deps = d
	d.books[province] = nb
	return nb, nil
}

// want khai báo trước các giá trị thao tác sẽ cần (phiên bản mẫu, sổ tên tỉnh, tên xã của tỉnh). Ngoài giao dịch:
// nạp ngay. Trong giao dịch: thiếu → errNeedDeps (thân giao dịch trả lỗi này để hủy sớm, trước khi ghi).
func (d *extDeps) want(ctx context.Context, versions []uuid.UUID, province string, communes ...string) error {
	for _, v := range versions {
		if _, ok := d.fc.m[v]; !ok {
			d.addForm(v)
		}
	}
	if province != "" {
		if nb, ok := d.books[province]; ok {
			for _, c := range communes {
				if _, ok := nb.communes[c]; !ok && !slices.Contains(nb.missing, c) {
					nb.missing = append(nb.missing, c)
				}
			}
		} else {
			if !slices.Contains(d.wantBooks, province) {
				d.wantBooks = append(d.wantBooks, province)
			}
			for _, c := range communes {
				if !slices.Contains(d.wantCommunes[province], c) {
					d.wantCommunes[province] = append(d.wantCommunes[province], c)
				}
			}
		}
	}
	if !d.pending() {
		return nil
	}
	if d.sealed {
		return errNeedDeps
	}
	return d.fill(ctx)
}

func (d *extDeps) addForm(v uuid.UUID) {
	if !slices.Contains(d.wantForms, v) {
		d.wantForms = append(d.wantForms, v)
	}
}

// pending: còn giá trị đã ghi nhận mà chưa nạp.
func (d *extDeps) pending() bool {
	if len(d.wantForms) > 0 || len(d.wantBooks) > 0 {
		return true
	}
	for _, nb := range d.books {
		if len(nb.missing) > 0 {
			return true
		}
	}
	return false
}

// fill nạp mọi giá trị đã ghi nhận — CHỈ gọi ngoài giao dịch (gọi port của module khác).
func (d *extDeps) fill(ctx context.Context) error {
	if d.sealed {
		return errors.New("reportcycle: extDeps.fill trong giao dịch")
	}
	for _, v := range d.wantForms {
		if _, err := d.fc.get(ctx, v); err != nil {
			return err
		}
	}
	d.wantForms = nil
	for _, p := range d.wantBooks {
		nb, err := d.book(ctx, p)
		if err != nil {
			return err
		}
		for _, c := range d.wantCommunes[p] {
			if !slices.Contains(nb.missing, c) {
				nb.missing = append(nb.missing, c)
			}
		}
		delete(d.wantCommunes, p)
	}
	d.wantBooks = nil
	for _, nb := range d.books {
		for _, c := range nb.missing {
			nb.load(ctx, d.s, c)
		}
		nb.missing = nil
	}
	return nil
}

// txDeps chạy fn trong giao dịch tenant với d niêm phong: fn trả errNeedDeps (hoặc lỗi bất kỳ / thành công nhưng còn
// giá trị thiếu) → hủy giao dịch, nạp phần thiếu NGOÀI giao dịch, chạy lại fn từ đầu. fn phải đặt lại mọi kết quả
// ra ngoài ở đầu mỗi lần chạy.
func (s *Service) txDeps(ctx context.Context, tid uuid.UUID, d *extDeps, fn func(pgx.Tx) error) error {
	if d.pending() {
		if err := d.fill(ctx); err != nil {
			return err
		}
	}
	for attempt := 1; ; attempt++ {
		d.sealed = true
		err := s.tx(ctx, tid, func(tx pgx.Tx) error {
			if err := fn(tx); err != nil {
				return err
			}
			if d.pending() {
				return errNeedDeps
			}
			return nil
		})
		d.sealed = false
		if err == nil || (!errors.Is(err, errNeedDeps) && !d.pending()) {
			return err
		}
		if attempt >= maxDepsAttempts {
			return ErrDependency.WithDetail("dữ liệu mẫu báo cáo/khu vực thay đổi liên tục — thử lại")
		}
		if ferr := d.fill(ctx); ferr != nil {
			return ferr
		}
	}
}

// wantPeriod khai báo dữ liệu module khác mà tổng hợp kỳ p cần: phiên bản mẫu các biểu của đợt (templates nil = mọi
// biểu), sổ khu vực của tỉnh và tên các xã của kỳ. Trong giao dịch, thiếu → errNeedDeps (gọi sớm để hủy trước khi ghi).
func (s *Service) wantPeriod(ctx context.Context, tx pgx.Tx, d *extDeps, p store.Period, b store.Batch, templates []uuid.UUID) error {
	var versions []uuid.UUID
	for _, f := range b.Forms {
		if templates == nil || slices.Contains(templates, f.TemplateID) {
			versions = append(versions, f.VersionID)
		}
	}
	tasks, err := store.TasksOfPeriod(ctx, tx, p.ID, nil, false)
	if err != nil {
		return err
	}
	communes := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if !slices.Contains(communes, t.CommuneCode) {
			communes = append(communes, t.CommuneCode)
		}
	}
	return d.want(ctx, versions, p.ProvinceCode, communes...)
}
