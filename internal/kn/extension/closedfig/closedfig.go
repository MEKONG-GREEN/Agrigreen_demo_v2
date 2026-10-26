// Package closedfig cài port.ClosedFigures (ADR-0053, gói PFPUB): bộ đọc "số liệu đã chốt" thống nhất cho KHTC trên họ
// hợp đồng closed_figures/v1 — hai view công bố cùng cột: pf_extension_pub.v1_closed_figures (pf 20261013150110, vòng đời báo
// cáo cũ) và pf_forms_pub.v1_closed_figures (pf 20261013150108, kỳ native cap/forms). Giao dịch chỉ đọc của tenant trên pool
// của module (db.ForModule → SET LOCAL ROLE r_extension; quyền đọc hai view qua thành viên r_extension_pub, r_forms_pub
// do bootstrap cấp — khai báo readpubview của module). Không đọc bảng nội bộ của nguồn nào.
package closedfig

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Reader đọc họ hợp đồng closed_figures/v1.
type Reader struct {
	DB tenant.Beginner // pool của module (đã bọc db.ForModule)
	// HTX: cổng trạng thái xác nhận HTX (D2); nil → dòng HTX là "Chưa xác nhận" (htx.go).
	HTX port.HTXConfirmations
}

var (
	_ port.ClosedFigures = Reader{}
	_ port.KHTCSources   = Reader{}
)

// cols: cột của họ hợp đồng theo đúng thứ tự (contracts/pubview/*.closed_figures.v1.json).
const cols = `department_code, province_code, calendar_id, calendar_version, period_type, period_key, week_year, period_rule, period_from, period_to,
  period_label, source, source_module, source_period_id, form_code, form_version_id, form_closed, period_closed, key_closed,
  closed_at, snapshot_id, snapshot_no, data_at, provisional, units_total, units_with_data, missing_units, rows, totals,
  confirmation_status, figures_hash, status_hash`

// Nhánh kỳ native bỏ kỳ `adhoc` (W11.50 — đợt điều tra CNTS, FN-21 BR-02): số liệu đợt không vào tổng/lũy kế quy đổi
// của KHTC; kỳ định kỳ có loại day/week/ksgm_week/month/… (lịch D8).
//
// filter chỉ trên cột chia cửa sổ key_closed của view (phòng, tỉnh, loại kỳ, khóa kỳ) → cờ của từng nguồn vẫn tính trên
// đủ khóa; lọc biểu làm ở Go sau khi tính KeyClosedAll.
const filter = `tenant_id = $1 AND ($2 = '' OR department_code = $2) AND ($3 = '' OR province_code = $3)
  AND ($4 = '' OR period_type = $4) AND (cardinality($5::text[]) = 0 OR period_key = ANY ($5::text[]))`

const query = `WITH u AS (
    SELECT ` + cols + ` FROM pf_extension_pub.v1_closed_figures WHERE ` + filter + `
    UNION ALL
    SELECT ` + cols + ` FROM pf_forms_pub.v1_closed_figures WHERE ` + filter + ` AND period_type <> 'adhoc')
  SELECT ` + cols + `,
         bool_and(key_closed) OVER (PARTITION BY department_code, province_code, period_type, period_key) AS key_closed_all
    FROM u
   ORDER BY department_code, province_code, period_type, period_key, form_code, source, source_period_id`

// ClosedFigures thực hiện port.ClosedFigures.
func (r Reader) ClosedFigures(ctx context.Context, tid uuid.UUID, q port.ClosedFiguresQuery) ([]port.ClosedFigure, error) {
	var out []port.ClosedFigure
	err := tenant.WithTenantTxOptions(ctx, r.DB, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		out, err = readFamily(ctx, tx, tid, q)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, applyHTX(ctx, r.HTX, out)
}

// readFamily đọc họ closed_figures/v1 (hai view) trong giao dịch tx của tenant.
func readFamily(ctx context.Context, tx pgx.Tx, tid uuid.UUID, q port.ClosedFiguresQuery) ([]port.ClosedFigure, error) {
	keys := q.PeriodKeys
	if keys == nil {
		keys = []string{}
	}
	rows, err := tx.Query(ctx, query, tid, q.Department, q.Province, q.PeriodType, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []port.ClosedFigure
	for rows.Next() {
		var f port.ClosedFigure
		if err := rows.Scan(&f.DepartmentCode, &f.ProvinceCode, &f.CalendarID, &f.CalendarVersion, &f.PeriodType, &f.PeriodKey, &f.WeekYear, &f.PeriodRule,
			&f.PeriodFrom, &f.PeriodTo, &f.PeriodLabel, &f.Source, &f.SourceModule, &f.SourcePeriodID, &f.FormCode, &f.FormVersionID,
			&f.FormClosed, &f.PeriodClosed, &f.KeyClosed, &f.ClosedAt, &f.SnapshotID, &f.SnapshotNo, &f.DataAt, &f.Provisional,
			&f.UnitsTotal, &f.UnitsWithData, &f.MissingUnits, &f.Rows, &f.Totals, &f.ConfirmationStatus, &f.FiguresHash,
			&f.StatusHash, &f.KeyClosedAll); err != nil {
			return nil, err
		}
		if len(q.FormCodes) > 0 && !slices.Contains(q.FormCodes, f.FormCode) || !inWindow(f.PeriodTo, q.From, q.To) {
			continue
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// inWindow: ngày cuối kỳ nguồn trong [from, to] (nil = không giới hạn; kỳ không có ngày cuối chỉ qua khi không lọc).
func inWindow(end, from, to *time.Time) bool {
	if from == nil && to == nil {
		return true
	}
	if end == nil {
		return false
	}
	d := end.Format(time.DateOnly)
	return (from == nil || d >= from.Format(time.DateOnly)) && (to == nil || d <= to.Format(time.DateOnly))
}

// Converted thực hiện port.ClosedFigures: đọc kỳ nguồn rồi quy đổi bằng Convert (hàm thuần).
func (r Reader) Converted(ctx context.Context, tid uuid.UUID, q port.ClosedFiguresQuery, t port.CrosswalkTarget) ([]port.TargetFigure, error) {
	figs, err := r.ClosedFigures(ctx, tid, q)
	if err != nil {
		return nil, err
	}
	return Convert(figs, t)
}
