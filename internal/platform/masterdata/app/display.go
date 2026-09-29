package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/seeddata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// ParamDefault là giá trị mặc định của một khóa (seeddata/params.json).
type ParamDefault struct {
	Key       string
	Value     *decimal.Decimal
	Text      *string
	Unit      string
	SourceDoc string
}

var (
	defaultsOnce sync.Once
	defaults     map[string]ParamDefault
	defaultsErr  error
)

// Defaults trả bảng giá trị mặc định nhúng theo khóa (đọc một lần).
func Defaults() (map[string]ParamDefault, error) {
	defaultsOnce.Do(func() {
		var pf paramFile
		if err := decodeStrict(seeddata.Params, &pf); err != nil {
			defaultsErr = fmt.Errorf("masterdata: dữ liệu nhúng params lỗi: %w", err)
			return
		}
		defaults = make(map[string]ParamDefault, len(pf.Params))
		for _, p := range pf.Params {
			d := ParamDefault{Key: p.Key, Text: p.Text, Unit: p.Unit, SourceDoc: p.SourceDoc}
			if p.Value != nil {
				v, err := decimal.NewFromString(*p.Value)
				if err != nil {
					defaultsErr = fmt.Errorf("masterdata: tham số nhúng %s: %w", p.Key, err)
					return
				}
				d.Value = &v
			}
			defaults[p.Key] = d
		}
	})
	return defaults, defaultsErr
}

// RestoreDefaultInput là dữ liệu vào của RestoreDefault.
type RestoreDefaultInput struct {
	Key     string
	Reason  string // lý do khôi phục (≥ 10 ký tự) — ghi vào correction_reason khi cần bản ghi bù
	IdemKey string
}

// RestoreDefault là "Khôi phục mặc định" / "Xóa cấu hình" của GIS US-CFG-02 AC-2: KHÔNG xóa
// phiên bản nào (RB-1, ADR-0011) mà tạo PHIÊN BẢN NHÁP mới mang giá trị mặc định nhúng,
// hiệu lực từ lúc đề xuất. Phiên bản đi qua quy trình gửi duyệt → duyệt như mọi tham số
// (R-56): giá trị mặc định chỉ áp dụng khi được người khác người đặt phê duyệt. Giá trị cũ
// vẫn nằm trong sổ param_approved (tra cứu, kiểm toán). Vai param_setter.
func (s *Service) RestoreDefault(ctx context.Context, in RestoreDefaultInput) (domain.ParamVersion, bool, error) {
	if _, err := authz.Require(ctx, port.RoleParamSetter); err != nil {
		return domain.ParamVersion{}, false, err
	}
	if err := domain.ValidateParamKey(in.Key); err != nil {
		return domain.ParamVersion{}, false, asProblem(err)
	}
	ds, err := Defaults()
	if err != nil {
		return domain.ParamVersion{}, false, err
	}
	d, ok := ds[in.Key]
	if !ok {
		return domain.ParamVersion{}, false, ErrNoDefault
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "Khôi phục giá trị mặc định (GIS US-CFG-02)"
	}
	pi := ProposeInput{
		Key: in.Key, Value: d.Value, Text: d.Text, Unit: d.Unit, EffectiveFrom: s.Clock.Now(),
		SourceDoc: truncateRunes("Khôi phục mặc định — "+d.SourceDoc, 300), IdemKey: in.IdemKey,
		CorrectionReason: &reason,
	}
	return s.Propose(ctx, pi)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// DisplayValue là một giá trị của quy chuẩn hiển thị kèm nguồn.
type DisplayValue struct {
	Key     string
	Value   string // mã màu HEX hoặc số thập phân dạng chuỗi
	Default string
	Version int  // 0 = chưa có phiên bản duyệt (dùng mặc định)
	FromDB  bool // true = phiên bản đã duyệt; false = mặc định nhúng
}

// DisplayConfig là quy chuẩn hiển thị dùng chung của bản đồ GIS tại một thời điểm.
type DisplayConfig struct {
	AsOf             time.Time
	StageColors      map[string]DisplayValue
	CapacityColors   map[string]DisplayValue
	RipeningOutline  DisplayValue
	HubWarnPct       DisplayValue
	HubFullPct       DisplayValue
	PlotLayerMinZoom DisplayValue
}

// DisplayConfig trả quy chuẩn hiển thị GIS đang hiệu lực (GIS US-CFG-01/02): mọi người dùng
// đã xác thực đọc được (legend web). Khóa chưa có phiên bản đã duyệt → giá trị mặc định nhúng.
func (s *Service) DisplayConfig(ctx context.Context) (DisplayConfig, error) {
	p, err := reader(ctx)
	if err != nil {
		return DisplayConfig{}, err
	}
	ds, err := Defaults()
	if err != nil {
		return DisplayConfig{}, err
	}
	now := s.Clock.Now()
	out := DisplayConfig{AsOf: now, StageColors: map[string]DisplayValue{}, CapacityColors: map[string]DisplayValue{}}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		get := func(key string) (DisplayValue, error) {
			dv := DisplayValue{Key: key}
			if d, ok := ds[key]; ok {
				dv.Default = defaultString(d)
				dv.Value = dv.Default
			}
			a, err := store.ApprovedAt(ctx, tx, key, now)
			if err != nil || a == nil {
				return dv, err
			}
			dv.Version, dv.FromDB = a.Version, true
			switch {
			case a.Text != nil:
				dv.Value = *a.Text
			case a.Value != nil:
				dv.Value = a.Value.String()
			}
			return dv, nil
		}
		for _, c := range port.StageCodes {
			v, err := get(port.ParamGISStageColorPrefix + ":" + c)
			if err != nil {
				return err
			}
			out.StageColors[c] = v
		}
		for _, c := range port.CapacityClasses {
			v, err := get(port.ParamGISCapacityColorPrefix + ":" + c)
			if err != nil {
				return err
			}
			out.CapacityColors[c] = v
		}
		var e error
		if out.RipeningOutline, e = get(port.ParamGISRipeningOutline); e != nil {
			return e
		}
		if out.HubWarnPct, e = get(port.ParamHubCapacityWarn); e != nil {
			return e
		}
		if out.HubFullPct, e = get(port.ParamHubCapacityFull); e != nil {
			return e
		}
		out.PlotLayerMinZoom, e = get(port.ParamPlotLayerMinZoom)
		return e
	})
	return out, err
}

func defaultString(d ParamDefault) string {
	switch {
	case d.Text != nil:
		return *d.Text
	case d.Value != nil:
		return d.Value.String()
	}
	return ""
}
