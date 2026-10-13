package closedfig

// Bộ điều hợp port.KHTCSourceCrosswalk (W11.62 — Kỳ KHTC) trên port KHTCSources của PFPUB (ADR-0053 §6, §7): mở kỳ KHTC
// lấy danh sách kỳ nguồn quy vào kỳ (Sources), màn tiến độ "Đủ dữ liệu" lấy cờ chốt từng kỳ nguồn (States). Tenant lấy
// từ người gọi trong ctx (ngữ cảnh phiên KN — Kỳ KHTC đã kiểm vai).

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	rcdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// KHTCCrosswalk cài port.KHTCSourceCrosswalk.
type KHTCCrosswalk struct {
	R Reader
	// RuleTag: lịch KHTC (Calendar.Rule.Tag()); rỗng → v2 mặc định Thứ Tư – Thứ Ba (ke_hoach_tai_chinh:v2).
	RuleTag func(ctx context.Context, day time.Time) string
}

var _ port.KHTCSourceCrosswalk = KHTCCrosswalk{}

func tenantOf(ctx context.Context) (uuid.UUID, error) {
	p, ok := authz.FromContext(ctx)
	if !ok || p.TenantID == uuid.Nil {
		return uuid.Nil, problem.Unauthenticated
	}
	return p.TenantID, nil
}

// SourceCode là mã nguồn của tham chiếu: "<module nguồn>:<mã biểu>" (vd "pf/forms:BM-04", "pf/price:PRICE-BOARD").
func SourceCode(module, form string) string { return module + ":" + form }

func calVer(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

// Sources thực hiện port.KHTCSourceCrosswalk: mọi kỳ nguồn (mỗi biểu) của các phòng quy vào kỳ KHTC (kind, key) theo
// ngày cuối (crosswalk v1). Không có nguồn → rỗng.
func (c KHTCCrosswalk) Sources(ctx context.Context, kind, key string, from, to time.Time, departments []string) ([]port.KHTCSourceRef, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	t := port.CrosswalkTarget{PeriodType: kind}
	if c.RuleTag != nil {
		t.RuleTag = c.RuleTag(ctx, from)
	}
	ps, err := c.R.Periods(ctx, tid, port.KHTCQuery{From: from, To: to, Departments: departments, Target: t})
	if err != nil {
		return nil, err
	}
	var out []port.KHTCSourceRef
	seen := map[string]bool{}
	for _, p := range ps {
		if key != "" && p.TargetKey != key {
			continue
		}
		for _, d := range p.Departments {
			if d.Fallback {
				continue // ngày duyệt trước kỳ (D22) không phải kỳ nguồn CỦA kỳ này
			}
			for _, f := range d.Forms {
				for _, s := range f.Sources {
					r := port.KHTCSourceRef{Department: d.DepartmentCode, Source: SourceCode(s.SourceModule, s.FormCode), CalendarID: s.CalendarID,
						CalendarVersion: calVer(s.CalendarVersion), NativeKey: s.NativeKey, Label: s.NativeKey}
					k := r.Department + "|" + r.Source + "|" + r.CalendarID + "|" + r.CalendarVersion + "|" + r.NativeKey
					if !seen[k] {
						seen[k] = true
						out = append(out, r)
					}
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Department != out[j].Department {
			return out[i].Department < out[j].Department
		}
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].NativeKey < out[j].NativeKey
	})
	return out, nil
}

// States thực hiện port.KHTCSourceCrosswalk: cờ chốt (biểu của kỳ nguồn đã chốt VÀ có số) và lần chốt gần nhất của
// từng tham chiếu, cùng thứ tự. Tham chiếu không còn khớp (kỳ thu hồi/hủy, nguồn chưa công bố) → chưa chốt.
func (c KHTCCrosswalk) States(ctx context.Context, refs []port.KHTCSourceRef) ([]port.KHTCSourceState, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]port.KHTCSourceState, len(refs))
	byDept := map[string][]string{}
	var priceFrom, priceTo time.Time
	for _, r := range refs {
		if strings.HasPrefix(r.Source, "pf/price:") {
			span, err := priceSpan(r)
			if err != nil {
				continue
			}
			if priceFrom.IsZero() || span.From.Before(priceFrom) {
				priceFrom = span.From
			}
			if span.To.After(priceTo) {
				priceTo = span.To
			}
			continue
		}
		byDept[r.Department] = append(byDept[r.Department], r.NativeKey)
	}
	var figs []port.ClosedFigure
	for dept, keys := range byDept {
		fs, err := c.R.ClosedFigures(ctx, tid, port.ClosedFiguresQuery{Department: dept, PeriodKeys: keys})
		if err != nil {
			return nil, err
		}
		figs = append(figs, fs...)
	}
	if !priceTo.IsZero() {
		ps, err := c.R.Periods(ctx, tid, port.KHTCQuery{From: priceFrom, To: priceTo, Departments: []string{PriceDepartment},
			Target: port.CrosswalkTarget{PeriodType: rcdomain.PeriodDay}})
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			for _, d := range p.Departments {
				if d.Fallback {
					continue
				}
				for _, f := range d.Forms {
					for _, s := range f.Sources {
						figs = append(figs, port.ClosedFigure{DepartmentCode: d.DepartmentCode, CalendarID: s.CalendarID, CalendarVersion: s.CalendarVersion,
							PeriodKey: s.NativeKey, SourceModule: s.SourceModule, FormCode: s.FormCode, FormClosed: s.Closed, ClosedAt: s.ClosedAt,
							Rows: []byte("[]")})
					}
				}
			}
		}
	}
	for i, r := range refs {
		st := port.KHTCSourceState{}
		found := false
		for _, f := range figs {
			if f.DepartmentCode != r.Department || SourceCode(f.SourceModule, f.FormCode) != r.Source || f.CalendarID != r.CalendarID ||
				calVer(f.CalendarVersion) != r.CalendarVersion || f.PeriodKey != r.NativeKey {
				continue
			}
			closed := f.FormClosed && f.Rows != nil
			if !found {
				st.Closed, found = closed, true
			} else {
				st.Closed = st.Closed && closed
			}
			if f.ClosedAt != nil && (st.ClosedAt == nil || f.ClosedAt.After(*st.ClosedAt)) {
				at := *f.ClosedAt
				st.ClosedAt = &at
			}
		}
		if !st.Closed {
			st.ClosedAt = nil
		}
		out[i] = st
	}
	return out, nil
}

// priceSpan: khoảng ngày của tham chiếu Báo giá (ngày bảng giá hoặc tuần báo giá — lịch v2 mặc định).
func priceSpan(r port.KHTCSourceRef) (rcdomain.Span, error) {
	if strings.HasSuffix(r.Source, ":"+port.PriceFormBoard) {
		return rcdomain.SpanOfKey(rcdomain.DefaultRule, rcdomain.PeriodDay, r.NativeKey, nil)
	}
	return rcdomain.SpanOfKey(rcdomain.DefaultRule, rcdomain.PeriodWeek, r.NativeKey, nil)
}
