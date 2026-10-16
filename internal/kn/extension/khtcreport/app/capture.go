package app

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"sort"
	"strings"

	kd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
	ks "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/store"
	form "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/domain"
	fs "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/store"
	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	sd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	ss "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) submitted(ctx context.Context, c caller, p ps.Period) ([]domain.Submitted, error) {
	if s.Forms == nil {
		return nil, ErrDependency.WithDetail("Chưa nối cổng phiếu đã gửi")
	}
	var periods []ps.Period
	var events map[uuid.UUID][]ps.Event
	if err := s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		var e error
		periods, e = ps.Periods(ctx, tx, p.Year, 10000)
		if e != nil {
			return e
		}
		ids := make([]uuid.UUID, 0, len(periods))
		for _, period := range periods {
			ids = append(ids, period.ID)
		}
		events, e = ps.Events(ctx, tx, ids)
		return e
	}); err != nil {
		return nil, err
	}
	out := []domain.Submitted{}
	for _, period := range periods {
		if terminal(events[period.ID]) == pd.PeriodCancelled || period.To.After(p.To) || (period.Type != pd.TypeWeek && period.ID != p.ID) {
			continue
		}
		for _, department := range period.Departments {
			submissions, err := s.Forms.Submissions(ctx, pd.ProfileKHTC, period.FormsPeriodRef, department)
			if err != nil {
				return nil, err
			}
			if len(submissions) == 0 {
				continue
			}
			// The port promises descending order, but select by version explicitly to protect the reporting contract.
			latest := submissions[0]
			for _, candidate := range submissions {
				if candidate.Version > latest.Version {
					latest = candidate
				}
			}
			if latest.Unit != department {
				return nil, ErrDependency.WithDetail("Phiếu đã gửi sai phòng")
			}
			sheetID, err := uuid.Parse(latest.Values[form.FieldSheet])
			if err != nil {
				// Legacy text-only submissions remain visible, but cannot masquerade as complete numeric inputs.
				out = append(out, domain.Submitted{PeriodID: period.ID, Department: department, Type: period.Type, Year: period.Year, From: period.From, To: period.To,
					SubmissionID: latest.ID, Attempt: latest.Version, Decision: latest.Decision, Sheet: form.Sheet{Cells: map[string]form.CellInput{}, Texts: latest.Values, Adjustments: []form.Adjustment{}}})
				continue
			}
			var sealed *fs.Sheet
			if err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error { var e error; sealed, e = fs.ByID(ctx, tx, sheetID); return e }); err != nil {
				return nil, err
			}
			if sealed == nil || sealed.Kind != fs.KindSealed || sealed.PeriodID != period.ID || sealed.FormsPeriodRef != period.FormsPeriodRef || sealed.Department != department {
				return nil, ErrDependency.WithDetail("Không có bảng niêm phong đúng phiếu đã gửi")
			}
			_, hash, err := sealed.Content.Hash()
			if err != nil {
				return nil, err
			}
			if hex.EncodeToString(hash) != hex.EncodeToString(sealed.ContentHash) {
				return nil, ErrDependency.WithDetail("Băm bảng số liệu không khớp")
			}
			out = append(out, domain.Submitted{PeriodID: period.ID, Department: department, Type: period.Type, Year: period.Year, From: period.From, To: period.To,
				SubmissionID: latest.ID, Attempt: latest.Version, Decision: latest.Decision, SheetID: sealed.ID, Hash: hex.EncodeToString(hash), Sheet: sealed.Content})
		}
	}
	return out, nil
}

func (s *Service) defaults(ctx context.Context, c caller, template string) (map[string]string, error) {
	defaults := map[string]string{"report_date": s.now().In(clock.Zone()).Format("2006-01-02"), "place": "An Giang", "document_no": "", "recipient": "", "basis": "", "recipients": "", "authority": "", "signer_title": "", "signer": ""}
	err := s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		rows, err := ks.CatalogCurrent(ctx, tx, "")
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.Status != kd.StatusActive || !slices.Contains(row.DefaultFor, template) {
				continue
			}
			switch row.Kind {
			case kd.KindSigner:
				defaults["signer"], defaults["signer_title"], defaults["authority"] = row.Name, row.Attrs["title"], row.Attrs["authority"]
			case kd.KindRecipient:
				if defaults["recipients"] != "" {
					defaults["recipients"] += "; "
				}
				defaults["recipients"] += row.Name
				if defaults["recipient"] == "" {
					defaults["recipient"] = row.Name
				}
			case kd.KindBasis:
				if defaults["basis"] != "" {
					defaults["basis"] += "; "
				}
				defaults["basis"] += row.Name
			}
		}
		return nil
	})
	return defaults, err
}

func (s *Service) capture(ctx context.Context, c caller, p ps.Period) (domain.Data, map[string]string, error) {
	plan := port.KHTCPlan{Year: p.Year, Entries: []port.KHTCPlanEntry{}}
	if s.Plans != nil {
		var err error
		plan, err = s.Plans.PlanAt(ctx, p.Year, p.Cutoff)
		if err != nil && !errors.Is(err, port.ErrKHTCPlanNotFound) {
			return domain.Data{}, nil, err
		}
	}
	sheets, err := s.submitted(ctx, c, p)
	if err != nil {
		return domain.Data{}, nil, err
	}
	var snapshot *domain.SourceRef
	metrics := []domain.SourceMetric{}
	names := map[string]port.KHTCMetricName{}
	if s.Sources != nil {
		if reader, ok := s.Sources.Sources.(port.KHTCMetricNames); ok {
			var err error
			names, err = reader.MetricNames(ctx, c.principal.TenantID, p.Cutoff)
			if err != nil {
				return domain.Data{}, nil, ErrDependency.WithCause(err)
			}
		}
	}
	if s.Sources != nil {
		state, e := s.Sources.Status(ctx, p.ID)
		if e != nil {
			return domain.Data{}, nil, e
		}
		if state.Snapshot != nil {
			snap := state.Snapshot
			var traceRows []sd.Row
			if e := s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error { var e error; traceRows, e = ss.Rows(ctx, tx, snap.ID); return e }); e != nil {
				return domain.Data{}, nil, e
			}
			if snap.PlanVersionID != nil && *snap.PlanVersionID != plan.VersionID {
				versions, ok := s.Plans.(port.KHTCPlanVersions)
				if !ok {
					return domain.Data{}, nil, ErrDependency.WithDetail("Chưa nối cổng phiên bản kế hoạch đã chốt")
				}
				plan, e = versions.PlanVersion(ctx, p.Year, *snap.PlanVersionID, p.Cutoff)
				if e != nil {
					return domain.Data{}, nil, e
				}
			}
			snapshot = &domain.SourceRef{ID: snap.ID, Version: snap.No, PlanVersionID: snap.PlanVersionID, FiguresHash: snap.FiguresHash, StatusHash: snap.StatusHash, Closed: snap.Closed, At: snap.EventTime}
			for _, value := range state.Values {
				key, e := sd.ParseSourceKey(value.SourceKey)
				if e != nil {
					continue
				}
				label, unit := metricLabel(key)
				if key.Mode == sd.ModePrice {
					if name, ok := names[key.Cell]; ok {
						label, unit = name.Label, name.Unit
					}
				}
				trace := []domain.SourceTrace{}
				for _, row := range traceRows {
					if row.HasSource() && row.Department == key.Department && strings.EqualFold(row.FormCode, key.Form) {
						trace = append(trace, domain.SourceTrace{TargetKey: row.TargetKey, Module: row.SourceModule, Calendar: row.CalendarID, NativeKey: row.NativeKey, From: row.From, To: row.To, Closed: row.Closed, FiguresHash: row.FiguresHash})
					}
				}
				metrics = append(metrics, domain.SourceMetric{Trace: trace, Key: value.SourceKey, Department: key.Department, Form: key.Form, Label: label, Unit: unit,
					Stock: strings.HasPrefix(key.Cell, "tong_dan_") || strings.Contains(key.Cell, "nha_yen"), Value: value.V})
			}
		}
	}
	metrics = domain.WithYields(metrics)
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].Key < metrics[j].Key })
	data, err := domain.Compose(domain.Period{ID: p.ID, No: p.No, Scope: p.Scope, Type: p.Type, Year: p.Year, Name: p.Name, From: p.From, To: p.To, Cutoff: p.Cutoff, Departments: append([]string{}, p.Departments...)}, plan, sheets, snapshot, metrics, s.now())
	if err != nil {
		return domain.Data{}, nil, err
	}
	for _, sheet := range data.Submissions {
		if sheet.SheetID == uuid.Nil {
			data.Provisional = append(data.Provisional, "Phiếu cũ chưa có bảng số liệu niêm phong: "+sheet.Department)
		}
	}
	sort.Strings(data.Provisional)
	defaults, err := s.defaults(ctx, c, data.Template)
	return data, defaults, err
}

func metricLabel(key sd.SourceKey) (string, string) {
	for _, form := range rc.CNTSForms {
		if strings.EqualFold(form.Code, key.Form) || strings.EqualFold(form.BM, key.Form) {
			for _, field := range form.Fields {
				if strings.EqualFold(field.Key, key.Cell) {
					return field.Label, field.Unit
				}
			}
		}
	}
	if key.Form == "cnts-bm18" || key.Form == "bm-18" {
		cell := strings.TrimSuffix(key.Cell, "_luy_ke_nam")
		labels := map[string]string{"khai_thac_ca": "Khai thác cá", "khai_thac_tom": "Khai thác tôm", "khai_thac_muc": "Khai thác mực", "khai_thac_khac": "Khai thác thủy sản khác", "khai_thac_tong": "Tổng sản lượng khai thác", "nuoi_trong_dien_tich": "Diện tích nuôi trồng", "nuoi_trong_so_long": "Số lồng nuôi", "nuoi_trong_san_luong": "Sản lượng nuôi trồng", "nuoi_trong_ngoc_trai_nghin_vien": "Ngọc trai", "tong_san_luong": "Tổng sản lượng thủy sản"}
		unit := "tấn"
		if cell == "nuoi_trong_dien_tich" {
			unit = "ha"
		}
		if cell == "nuoi_trong_so_long" {
			unit = "lồng"
		}
		if cell == "nuoi_trong_ngoc_trai_nghin_vien" {
			unit = "nghìn viên"
		}
		if label := labels[cell]; label != "" {
			return label, unit
		}
	}
	if key.Department == "trong_trot" {
		parts := strings.Split(key.Cell, "/")
		field := parts[len(parts)-1]
		unit := "ha"
		labels := map[string]string{"luy_ke": "Diện tích xuống giống lũy kế", "dt_xg": "Diện tích xuống giống", "dt_th": "Diện tích thu hoạch", "dt_th_luy_ke": "Diện tích thu hoạch lũy kế", "san_luong": "Sản lượng", "san_luong_luy_ke": "Sản lượng lũy kế"}
		if strings.HasPrefix(field, "san_luong") {
			unit = "tấn"
		}
		if label := labels[field]; label != "" {
			if len(parts) > 1 {
				label += " — " + strings.ReplaceAll(parts[0], "_", " ")
			}
			return label, unit
		}
	}
	if key.Mode == sd.ModePrice {
		return "Mặt hàng " + strings.ToUpper(key.Cell), ""
	}
	if key.Cell == "tickets_collected" {
		return "Số phiếu giá cả đã thu thập", "phiếu"
	}
	return strings.ReplaceAll(key.Cell, "_", " "), ""
}
