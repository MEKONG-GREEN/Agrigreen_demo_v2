package migrator

import (
	"fmt"
	"sort"
)

// Row là một dòng sổ migration (đọc từ mọi <schema>.schema_migrations của database).
type Row struct {
	Dir      string
	File     string
	Version  int64
	Module   string
	Checksum string
	Method   string // apply | import
	Schema   string // schema chứa sổ
}

// Key như File.Key.
func (r Row) Key() string { return r.Dir + "/" + r.File }

// V1State là trạng thái bảng golang-migrate (schema_migrations_<thư mục>) của bộ chạy cũ.
type V1State struct {
	Exists  bool
	Version int64 // 0 nếu bảng rỗng
	Dirty   bool
}

// Reconciliation là kết quả đối chiếu kế hoạch với sổ.
type Reconciliation struct {
	// Import: tệp bộ chạy cũ ĐÃ áp (số trong (L, V] với L = số lớn nhất của thư mục trong sổ, V = phiên bản
	// golang-migrate) nhưng chưa có trong sổ — ghi sổ, KHÔNG chạy lại.
	Import []File
	// Apply: tệp vắng trong sổ còn lại, theo thứ tự kế hoạch — kể cả số nhỏ hơn số đã áp (golang-migrate bỏ qua).
	Apply []File
	// Drift: tệp đã ghi sổ mà tổng kiểm khác nội dung hiện tại (R-24: tệp đã áp bất biến).
	Drift []string
	// Unknown: dòng sổ không còn tệp tương ứng (tệp bị xóa/đổi tên).
	Unknown []string
	// Duplicate: tệp ghi ở hơn một sổ.
	Duplicate []string
	// Problems: lỗi chặn (golang-migrate dirty …).
	Problems []string
}

// Blocking cho biết có lỗi phải dừng trước khi áp.
func (r Reconciliation) Blocking() bool {
	return len(r.Drift) > 0 || len(r.Unknown) > 0 || len(r.Duplicate) > 0 || len(r.Problems) > 0
}

// Err trả lỗi tổng hợp khi Blocking.
func (r Reconciliation) Err() error {
	if !r.Blocking() {
		return nil
	}
	return fmt.Errorf("migrator: sổ migration không khớp — lệch tổng kiểm %v; dòng sổ không có tệp %v; ghi trùng %v; %v (xem docs/runbooks/migrate-runner-v2.md)",
		r.Drift, r.Unknown, r.Duplicate, r.Problems)
}

// Reconcile đối chiếu kế hoạch với sổ (rows) và trạng thái golang-migrate (v1, theo thư mục). Hàm thuần.
func Reconcile(plan []File, rows []Row, v1 map[string]V1State) Reconciliation {
	var r Reconciliation
	ledger := map[string]Row{}
	maxV := map[string]int64{}
	for _, row := range rows {
		k := row.Key()
		if prev, dup := ledger[k]; dup {
			r.Duplicate = append(r.Duplicate, fmt.Sprintf("%s (%s, %s)", k, prev.Schema, row.Schema))
			continue
		}
		ledger[k] = row
		maxV[row.Dir] = max(maxV[row.Dir], row.Version)
	}
	inPlan := map[string]bool{}
	dirs := map[string]bool{}
	for _, f := range plan {
		inPlan[f.Key()] = true
		dirs[f.Dir] = true
	}
	for k := range ledger {
		if !inPlan[k] {
			r.Unknown = append(r.Unknown, k)
		}
	}
	sort.Strings(r.Unknown)
	for d := range dirs {
		if s := v1[d]; s.Exists && s.Dirty {
			r.Problems = append(r.Problems, fmt.Sprintf("golang-migrate schema_migrations_%s bẩn ở phiên bản %d — sửa tay trước (runbook)", d, s.Version))
		}
	}
	sort.Strings(r.Problems)
	for _, f := range plan {
		row, ok := ledger[f.Key()]
		switch {
		case ok:
			if row.Checksum != f.Checksum {
				r.Drift = append(r.Drift, f.Key())
			}
		case f.Version > maxV[f.Dir] && f.Version <= v1[f.Dir].Version:
			r.Import = append(r.Import, f)
		default:
			r.Apply = append(r.Apply, f)
		}
	}
	return r
}

// SkippedByV1 trả các tệp sẽ được v2 áp mà bộ chạy cũ (golang-migrate) bỏ qua im lặng: số ≤ phiên bản v1.
func SkippedByV1(apply []File, v1 map[string]V1State) []File {
	var out []File
	for _, f := range apply {
		if f.Version <= v1[f.Dir].Version {
			out = append(out, f)
		}
	}
	return out
}
