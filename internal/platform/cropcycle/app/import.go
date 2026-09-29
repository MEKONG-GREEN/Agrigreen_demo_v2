package app

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// MaxImportRows giới hạn số dòng xử lý đồng bộ trong một request (R-52: dưới 2 giây).
const MaxImportRows = 200

// MaxImportBytes giới hạn dung lượng tệp nhập sản lượng (CSV hoặc .xlsx).
const MaxImportBytes = 2 << 20

// Trạng thái từng dòng nhập (HTX US-YIELD-02 AC-1).
const (
	RowAccepted  = "accepted"
	RowDuplicate = "duplicate"
	RowRejected  = "rejected"
)

// HarvestRow là một dòng nhập sản lượng (bảng nhập nhanh hoặc tệp Excel).
type HarvestRow struct {
	Line        int
	SeasonRef   string
	PlotCode    string
	HarvestDate string // YYYY-MM-DD, rỗng = hôm nay
	MassKg      string // kg, hoặc để trống nếu dùng Tonnes
	Tonnes      string
	MoisturePct string
	Confirm     bool
}

// HarvestRowResult là kết quả một dòng. Reason không chứa dữ liệu cá nhân.
type HarvestRowResult struct {
	Line         int
	Status       string
	Code         string
	Reason       string
	NeedsConfirm bool
	SeasonRef    uuid.UUID
	HarvestID    uuid.UUID
}

// BulkHarvestResult là kết quả cả lượt.
type BulkHarvestResult struct {
	Accepted, Duplicate, Rejected int
	Rows                          []HarvestRowResult
}

// BulkHarvest lưu nhiều dòng sản lượng trong một lần bấm (HTX US-YIELD-02 AC-2),
// bỏ qua dòng lỗi và chỉ ghi dòng hợp lệ (AC-1). coop: HTX tra mã lô (quản lý
// HTX: bỏ trống = HTX của mình).
func (s *Service) BulkHarvest(ctx context.Context, coop uuid.UUID, rows []HarvestRow, source, idemKey string) (BulkHarvestResult, error) {
	a, err := s.access(ctx, managerRoles...)
	if err != nil {
		return BulkHarvestResult{}, err
	}
	if idemKey == "" {
		return BulkHarvestResult{}, problem.IdempotencyRequired
	}
	if len(rows) == 0 || len(rows) > MaxImportRows {
		return BulkHarvestResult{}, problem.Validation.WithField("rows", "1–200 dòng")
	}
	if a.kind == accCoop {
		if coop != uuid.Nil && coop != a.p.OrgRef {
			return BulkHarvestResult{}, ErrOutOfScope
		}
		coop = a.p.OrgRef
	}
	var res BulkHarvestResult
	for i, r := range rows {
		if r.Line == 0 {
			r.Line = i + 1
		}
		out := s.harvestRow(ctx, a, coop, r, source, idemKey)
		switch out.Status {
		case RowAccepted:
			res.Accepted++
		case RowDuplicate:
			res.Duplicate++
		case "":
			return BulkHarvestResult{}, errors.New("cropcycle: lỗi hệ thống khi ghi dòng " + strconv.Itoa(r.Line))
		default:
			res.Rejected++
		}
		res.Rows = append(res.Rows, out)
	}
	return res, nil
}

func (s *Service) harvestRow(ctx context.Context, a access, coop uuid.UUID, r HarvestRow, source, idemKey string) HarvestRowResult {
	out := HarvestRowResult{Line: r.Line}
	reject := func(code, reason string) HarvestRowResult {
		out.Status, out.Code, out.Reason = RowRejected, code, reason
		return out
	}
	var m mass.Kg
	var err error
	switch {
	case strings.TrimSpace(r.MassKg) != "":
		m, err = mass.ParseKg(strings.ReplaceAll(strings.TrimSpace(r.MassKg), ",", "."))
	case strings.TrimSpace(r.Tonnes) != "":
		m, err = mass.FromTonnes(strings.ReplaceAll(strings.TrimSpace(r.Tonnes), ",", "."))
	default:
		return reject(problem.Validation.Code, "Thiếu sản lượng")
	}
	if err != nil {
		return reject(problem.Validation.Code, "Sai định dạng số sản lượng")
	}
	in := HarvestInput{MassKg: m, ConfirmAnomaly: r.Confirm, GPS: domain.GPS{Source: domain.GPSManualPick}, Source: source,
		IdemKey: idemKey + ":" + strconv.Itoa(r.Line)}
	if s := strings.TrimSpace(r.HarvestDate); s != "" {
		d, err := domain.ParseDay(s)
		if err != nil {
			return reject(problem.Validation.Code, "Ngày thu hoạch phải có dạng YYYY-MM-DD")
		}
		in.HarvestDate = d
	}
	if s := strings.TrimSpace(r.MoisturePct); s != "" {
		d, err := decimal.NewFromString(strings.ReplaceAll(s, ",", "."))
		if err != nil {
			return reject(problem.Validation.Code, "Sai định dạng số độ ẩm")
		}
		in.MoisturePct = &d
	}
	switch {
	case strings.TrimSpace(r.SeasonRef) != "":
		ref, err := uuid.Parse(strings.TrimSpace(r.SeasonRef))
		if err != nil {
			return reject(problem.Validation.Code, "Mã vụ không hợp lệ")
		}
		in.SeasonRef = ref
	case strings.TrimSpace(r.PlotCode) != "":
		if coop == uuid.Nil {
			return reject(problem.Validation.Code, "Cần cooperative_ref để tra mã lô")
		}
		var sn *store.Season
		if err := s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
			var e error
			sn, e = store.OpenSeasonByPlotCode(ctx, tx, coop, strings.TrimSpace(r.PlotCode))
			return e
		}); err != nil {
			return out // lỗi hệ thống: Status rỗng
		}
		if sn == nil {
			return reject(ErrSeasonNotOpen.Code, "Lô "+strings.TrimSpace(r.PlotCode)+" không có vụ đang mở")
		}
		in.SeasonRef = sn.SeasonRef
	default:
		return reject(problem.Validation.Code, "Thiếu mã lô hoặc mã vụ")
	}
	h, err := s.declareHarvest(ctx, a, in)
	if err != nil {
		pr := problem.From(err)
		if pr.Status >= 500 {
			return out
		}
		out = reject(pr.Code, rowReason(pr))
		out.NeedsConfirm = pr.Code == ErrYieldAnomaly.Code
		out.SeasonRef = in.SeasonRef
		return out
	}
	out.Status, out.SeasonRef, out.HarvestID = RowAccepted, h.Season.SeasonRef, h.Harvest.ID
	if h.Replayed {
		out.Status = RowDuplicate
	}
	return out
}

func rowReason(pr *problem.Problem) string {
	if pr.Detail != "" {
		return pr.Detail
	}
	if len(pr.Errors) > 0 {
		return pr.Errors[0].Field + ": " + pr.Errors[0].Reason
	}
	return pr.Title
}

// Cột của tệp mẫu nhập sản lượng (tiêu đề đã chuẩn hóa bằng tabular.NormalizeHeader).
var importAliases = map[string]string{
	"season_ref": "season_ref", "ma_vu": "season_ref",
	"plot_code": "plot_code", "ma_lo": "plot_code", "ma_thua": "plot_code",
	"harvest_date": "harvest_date", "ngay_thu_hoach": "harvest_date",
	"mass_kg": "mass_kg", "san_luong_kg": "mass_kg",
	"tonnes": "tonnes", "san_luong_tan": "tonnes", "tan": "tonnes",
	"moisture_pct": "moisture_pct", "do_am": "moisture_pct", "do_am_pct": "moisture_pct",
	"confirm_anomaly": "confirm_anomaly", "xac_nhan": "confirm_anomaly", "xac_nhan_bat_thuong": "confirm_anomaly",
}

// ImportHarvests nhập sản lượng từ CSV (UTF-8) hoặc Excel .xlsx (HTX US-YIELD-02):
// tệp sai mẫu bị từ chối toàn bộ; dòng lỗi (thiếu sản lượng, sai định dạng số...)
// bị bỏ qua kèm lý do, chỉ dòng hợp lệ được ghi.
func (s *Service) ImportHarvests(ctx context.Context, coop uuid.UUID, src io.Reader, idemKey string) (BulkHarvestResult, error) {
	if _, err := s.access(ctx, managerRoles...); err != nil {
		return BulkHarvestResult{}, err
	}
	lim := tabular.Limits{MaxBytes: MaxImportBytes, MaxRows: MaxImportRows}
	tb, err := tabular.Read(src, lim)
	if err != nil {
		var fe *tabular.FormulaError
		if errors.As(err, &fe) || errors.Is(err, tabular.ErrTooManyRows) || errors.Is(err, tabular.ErrTooLarge) ||
			errors.Is(err, tabular.ErrTooManyColumns) || errors.Is(err, tabular.ErrEncoding) || errors.Is(err, tabular.ErrLegacyExcel) ||
			errors.Is(err, tabular.ErrEmpty) || errors.Is(err, tabular.ErrFormat) {
			return BulkHarvestResult{}, ErrImportInvalid.WithDetail(tabular.Message(err, lim))
		}
		return BulkHarvestResult{}, err
	}
	col := tb.Columns(importAliases)
	_, hasSeason := col["season_ref"]
	_, hasPlot := col["plot_code"]
	_, hasKg := col["mass_kg"]
	_, hasT := col["tonnes"]
	if !hasSeason && !hasPlot {
		return BulkHarvestResult{}, ErrImportInvalid.WithDetail("Thiếu cột Mã lô (plot_code) hoặc Mã vụ (season_ref)")
	}
	if !hasKg && !hasT {
		return BulkHarvestResult{}, ErrImportInvalid.WithDetail("Thiếu cột Sản lượng kg (mass_kg) hoặc Sản lượng tấn (tonnes)")
	}
	if len(tb.Rows) == 0 {
		return BulkHarvestResult{}, ErrImportInvalid.WithDetail("Tệp không có dòng dữ liệu")
	}
	rows := make([]HarvestRow, 0, len(tb.Rows))
	for _, r := range tb.Rows {
		c := strings.ToLower(r.Get(col, "confirm_anomaly"))
		rows = append(rows, HarvestRow{Line: r.Line, SeasonRef: r.Get(col, "season_ref"), PlotCode: r.Get(col, "plot_code"),
			HarvestDate: r.Get(col, "harvest_date"), MassKg: r.Get(col, "mass_kg"), Tonnes: r.Get(col, "tonnes"),
			MoisturePct: r.Get(col, "moisture_pct"), Confirm: c == "1" || c == "true" || c == "x" || c == "co" || c == "có"})
	}
	return s.BulkHarvest(ctx, coop, rows, SourceExcel, idemKey)
}
