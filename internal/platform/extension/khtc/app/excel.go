package app

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtc/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtc/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// ProductPlanExport là tên sản phẩm của lần xuất Kế hoạch năm trong sổ egress_event (pf/metering).
const ProductPlanExport = "kn_khtc_annual_plan"

// Cột của tệp Excel Kế hoạch năm — xuất và nhập dùng cùng bố cục (nhập lại tệp đã xuất giữ nguyên định danh dòng).
var planHeader = []string{"Tab", "Cấp", "Mã dòng", "Mã dòng cha", "Mã CT/DA", "Số La Mã", "Thứ tự", "Mã", "Tên", "ĐVT",
	"Khối lượng KH", "Quy mô", "Kinh phí KH", "Nguồn kinh phí", "Lĩnh vực", "Phòng phụ trách", "Xã/điểm", "Số điểm",
	"Nguồn số liệu", "Khóa nguồn module", "Theo dõi", "Ngừng từ", "QĐ ngừng", "Ghi chú"}

var planAliases = map[string]string{}

func init() {
	keys := []string{"tab", "kind", "ref", "parent", "program", "roman", "sort", "code", "name", "unit", "qty", "scale", "budget",
		"funding", "field", "dept", "communes", "points", "source", "source_key", "tracking", "stopped_from", "stop_decision", "note"}
	for i, h := range planHeader {
		planAliases[tabular.NormalizeHeader(h)] = keys[i]
		planAliases[keys[i]] = keys[i]
	}
}

var kindLabel = map[string]string{domain.EntryProgram: "CT/DA", domain.EntryGroup: "Nhóm", domain.EntryItem: "Hạng mục"}
var sourceLabel = map[string]string{domain.SourceForm: "Phiếu", domain.SourceModule: "Module"}
var trackingLabel = map[string]string{domain.TrackingOn: "Đang theo dõi", domain.TrackingStopped: "Ngừng theo dõi"}

func reverse(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[tabular.NormalizeHeader(v)] = k
		out[k] = k
	}
	return out
}

// Export là tệp xuất.
type Export struct {
	Body        []byte
	ContentType string
	FileName    string
}

func optDec(d *decimal.Decimal) string {
	if d == nil {
		return ""
	}
	return d.String()
}

// ExportPlan xuất một phiên bản kế hoạch năm ra Excel (một trang tính, cả 6 tab, cây theo thứ tự lưu).
func (s *Service) ExportPlan(ctx context.Context, year, version int) (Export, error) {
	pv, err := s.GetPlan(ctx, year, version, "")
	if err != nil {
		return Export{}, err
	}
	rows := make([][]string, 0, len(pv.Entries))
	for _, e := range pv.Entries {
		parent, stopped, points := "", "", ""
		if e.ParentRef != nil {
			parent = e.ParentRef.String()
		}
		if e.StoppedFrom != nil {
			stopped = e.StoppedFrom.Format("2006-01-02")
		}
		if e.PointsCount != nil {
			points = strconv.Itoa(*e.PointsCount)
		}
		rows = append(rows, []string{domain.TabLabel[e.Tab], kindLabel[e.Kind], e.Ref.String(), parent, e.ProgramCode, e.Roman,
			strconv.Itoa(e.SortOrder), e.Code, e.Name, e.UnitCode, optDec(e.PlannedQty), e.ScaleNote, optDec(e.Budget), e.FundingCode,
			e.FieldCode, e.Department, strings.Join(e.Communes, ", "), points, sourceLabel[e.DataSource], e.SourceKey,
			trackingLabel[e.Tracking], stopped, e.StopDecision, e.Note})
	}
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, "Kế hoạch năm "+strconv.Itoa(year), planHeader, rows); err != nil {
		return Export{}, err
	}
	s.egress(ctx, len(rows), buf.Len())
	name := "ke-hoach-nam-" + strconv.Itoa(year) + "-v" + strconv.FormatInt(pv.Version.Seq, 10) + ".xlsx"
	return Export{Body: buf.Bytes(), ContentType: tabular.ContentTypeXLSX, FileName: name}, nil
}

// egress đưa lần xuất qua điểm kiểm soát dùng chung (ADR-0024, A5): kênh report_export, bên nhận = chính cán bộ xuất,
// nội bộ → observe (không bao giờ từ chối). Chưa nối checkpoint → bỏ qua.
func (s *Service) egress(ctx context.Context, records, size int) {
	p, ok := authz.FromContext(ctx)
	if !ok || s.Egress == nil {
		return
	}
	_ = meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{
		SourceID: ids.New(), Channel: meter.ChannelReportExport, DataTenantID: p.TenantID,
		CounterpartyKind: meter.CounterpartyActor, CounterpartyRef: p.ActorRef, Product: ProductPlanExport,
		Records: int64(records), Bytes: int64(size), OccurredAt: s.now(), ActorRef: p.ActorRef, TraceID: telemetry.TraceID(ctx),
		Internal: true,
	}, s.Log)
}

// ImportDraft đọc tệp Excel/CSV theo bố cục của ExportPlan thành bản nháp (KHÔNG ghi). Mã dòng / Mã dòng cha là UUID
// (tệp đã xuất) hoặc nhãn tùy ý trong tệp; Mã CT/DA tra danh sách chủ. Dòng lỗi trả kèm lý do; người dùng sửa ở màn
// hình rồi "Lưu" như bình thường (kiểm đầy đủ lúc lưu).
func (s *Service) ImportDraft(ctx context.Context, year int, data []byte) (Draft, error) {
	c, err := s.planWriter(ctx)
	if err != nil {
		return Draft{}, err
	}
	if err := validYear(year); err != nil {
		return Draft{}, err
	}
	lim := tabular.Limits{MaxBytes: 5 << 20, MaxRows: domain.MaxEntries, MaxCols: 64}
	t, err := tabular.ReadBytes(data, lim)
	if err != nil {
		return Draft{}, ErrImport.WithDetail(tabular.Message(err, lim))
	}
	col := t.Columns(planAliases)
	if miss := tabular.Missing(col, "tab", "kind", "name"); len(miss) > 0 {
		return Draft{}, ErrImport.WithDetail("thiếu cột: " + strings.Join(miss, ", "))
	}
	var progs []store.ProgramRow
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		progs, e = store.ProgramsCurrent(ctx, tx)
		return e
	}); err != nil {
		return Draft{}, err
	}
	byCode := map[string]store.ProgramRow{}
	for _, p := range progs {
		byCode[strings.ToLower(p.Code)] = p
	}
	tabs, kinds, sources, tracks := reverse(domain.TabLabel), reverse(kindLabel), reverse(sourceLabel), reverse(trackingLabel)
	refOf := map[string]uuid.UUID{}
	resolve := func(key string) uuid.UUID {
		if u, err := uuid.Parse(key); err == nil {
			return u
		}
		if u, ok := refOf[key]; ok {
			return u
		}
		u := ids.New()
		refOf[key] = u
		return u
	}
	d := Draft{}
	var es []domain.Entry
	var lineOf []int
	for _, r := range t.Rows {
		get := func(k string) string { return strings.TrimSpace(r.Get(col, k)) }
		bad := func(f, why string) { d.Problems = append(d.Problems, RowProblem{Row: r.Line, Field: f, Reason: why}) }
		e := domain.Entry{Name: get("name"), Code: get("code"), UnitCode: get("unit"), ScaleNote: get("scale"), FundingCode: get("funding"),
			FieldCode: get("field"), Department: get("dept"), SourceKey: get("source_key"), StopDecision: get("stop_decision"), Note: get("note")}
		e.Tab = tabs[tabular.NormalizeHeader(get("tab"))]
		e.Kind = kinds[tabular.NormalizeHeader(get("kind"))]
		if e.Tab == "" {
			bad("tab", "tab không hợp lệ")
			continue
		}
		if e.Kind == "" {
			bad("kind", "cấp phải là CT/DA, Nhóm hoặc Hạng mục")
			continue
		}
		if k := get("ref"); k != "" {
			e.Ref = resolve(k)
		} else {
			e.Ref = ids.New()
		}
		if k := get("parent"); k != "" {
			p := resolve(k)
			e.ParentRef = &p
		}
		if k := get("program"); k != "" && e.Kind == domain.EntryProgram {
			p, ok := byCode[strings.ToLower(k)]
			if !ok {
				bad("program", "mã CT/DA không có trong danh sách: "+k)
				continue
			}
			ref := p.Ref
			e.ProgramRef = &ref
		}
		if v := get("sort"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				bad("sort", "thứ tự phải là số nguyên")
				continue
			}
			e.SortOrder = n
		}
		ok := true
		for _, x := range []struct {
			k   string
			dst **decimal.Decimal
		}{{"qty", &e.PlannedQty}, {"budget", &e.Budget}} {
			v := strings.ReplaceAll(get(x.k), " ", "")
			if v == "" {
				continue
			}
			dv, err := decimal.NewFromString(v)
			if err != nil {
				bad(x.k, "không phải số (dấu thập phân là dấu chấm)")
				ok = false
				continue
			}
			*x.dst = &dv
		}
		if v := get("points"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				bad("points", "số điểm phải là số nguyên")
				ok = false
			} else {
				e.PointsCount = &n
			}
		}
		if v := get("communes"); v != "" {
			for _, c := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
				e.Communes = append(e.Communes, c)
			}
		}
		if v := get("source"); v != "" {
			e.DataSource = sources[tabular.NormalizeHeader(v)]
			if e.DataSource == "" {
				bad("source", "nguồn số liệu là Phiếu hoặc Module")
				ok = false
			}
		}
		e.Tracking = domain.TrackingOn
		if v := get("tracking"); v != "" {
			e.Tracking = tracks[tabular.NormalizeHeader(v)]
			if e.Tracking == "" {
				bad("tracking", "Đang theo dõi hoặc Ngừng theo dõi")
				ok = false
			}
		}
		if v := get("stopped_from"); v != "" {
			t, err := domain.ParseDate(v)
			if err != nil {
				bad("stopped_from", "ngày YYYY-MM-DD")
				ok = false
			} else {
				e.StoppedFrom = &t
			}
		}
		if ok {
			es = append(es, e)
			lineOf = append(lineOf, r.Line)
		}
	}
	// Kiểm cây (không tra danh mục — kiểm đủ lúc lưu); lỗi trả theo dòng.
	for _, fe := range domain.NormalizePlan(year, es, domain.Refs{}) {
		rp := RowProblem{Field: fe.Field, Reason: fe.Reason}
		// "entries.<i>.<trường>" → dòng trên trang tính.
		if parts := strings.SplitN(fe.Field, ".", 3); len(parts) == 3 {
			if i, err := strconv.Atoi(parts[1]); err == nil && i < len(lineOf) {
				rp.Row, rp.Field = lineOf[i], parts[2]
			}
		}
		d.Problems = append(d.Problems, rp)
	}
	d.Entries = entryViews(es, programMap(progs), nil)
	if len(es) == 0 {
		return d, ErrImport.WithDetail("không có dòng hợp lệ")
	}
	return d, nil
}
