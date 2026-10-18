package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// KPI là bốn chỉ số cốt lõi của một đơn vị (US-DASH-01).
type KPI struct {
	SownAreaHa         decimal.Decimal  // diện tích đã sạ báo cáo trong kỳ (ha)
	Cooperatives       int64            // số HTX có báo cáo gieo sạ trong kỳ
	OpenTasks          int64            // nhiệm vụ hỗ trợ đang mở
	OverdueTasks       int64            // trong đó quá SLA
	DirectiveTargets   int64            // lượt nhận chỉ đạo ban hành trong kỳ
	DirectiveAcks      int64            // lượt đã xác nhận đọc
	DirectiveAckRate   *decimal.Decimal // acks / targets; nil khi không có chỉ đạo
	ExpectedCommunes   int64            // xã có cán bộ phụ trách
	ReportedCommunes   int64            // trong đó đã có báo cáo của kỳ
	MissingCommunes    []string         // xã chưa báo cáo → "tạm tính"
	Provisional        bool             // US-DASH-02 AC-1: "Số liệu tạm tính"
	PeriodClosed       bool             // kỳ đã chốt (mọi tỉnh trong đơn vị)
	ClosedProvinceList []string
}

func (k *KPI) add(c store.Cell, commune string) {
	k.SownAreaHa = k.SownAreaHa.Add(c.SownHa)
	k.Cooperatives += c.Cooperatives
	k.OpenTasks += c.OpenTasks
	k.OverdueTasks += c.Overdue
	k.DirectiveTargets += c.Targets
	k.DirectiveAcks += c.Acks
	if c.Expected {
		k.ExpectedCommunes++
		if c.Reported {
			k.ReportedCommunes++
		} else {
			k.MissingCommunes = append(k.MissingCommunes, commune)
		}
	}
}

func (k *KPI) sum(o KPI) {
	k.SownAreaHa = k.SownAreaHa.Add(o.SownAreaHa)
	k.Cooperatives += o.Cooperatives
	k.OpenTasks += o.OpenTasks
	k.OverdueTasks += o.OverdueTasks
	k.DirectiveTargets += o.DirectiveTargets
	k.DirectiveAcks += o.DirectiveAcks
	k.ExpectedCommunes += o.ExpectedCommunes
	k.ReportedCommunes += o.ReportedCommunes
	k.MissingCommunes = append(k.MissingCommunes, o.MissingCommunes...)
}

func (k *KPI) finish() {
	k.DirectiveAckRate = domain.AckRate(k.DirectiveAcks, k.DirectiveTargets)
	sort.Strings(k.MissingCommunes)
	if k.MissingCommunes == nil {
		k.MissingCommunes = []string{}
	}
	k.Provisional = len(k.MissingCommunes) > 0
}

// Dashboard là số liệu của một đơn vị kèm các đơn vị con. Tổng = tổng các con
// (US-DASH-01 AC-2: toàn quốc bằng đúng tổng các tỉnh).
type Dashboard struct {
	Unit     string // rỗng = toàn quốc
	Level    string
	Period   string
	AsOf     time.Time
	Total    KPI
	Children []Child
}

// Child là một đơn vị con (tỉnh của toàn quốc, xã của tỉnh).
type Child struct {
	Unit string
	KPI  KPI
}

// GetDashboard tính 4 KPI theo phạm vi của người gọi. unit rỗng: toàn quốc nếu
// người gọi xem được toàn quốc, nếu không là tỉnh/xã phụ trách.
func (s *Service) GetDashboard(ctx context.Context, unit, period string) (Dashboard, error) {
	p, err := authz.Require(ctx, knReaders...)
	if err != nil {
		return Dashboard{}, err
	}
	now := s.now()
	if period == "" {
		period = domain.PeriodOf(now, clock.Zone())
	}
	if !domain.ValidPeriod(period) {
		return Dashboard{}, problem.Validation.WithField("period", "định dạng YYYY-MM")
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return Dashboard{}, err
	}
	if sc.Empty() {
		return Dashboard{}, ErrOutOfScope.WithDetail("tài khoản chưa gắn đơn vị công tác")
	}
	unit = strings.TrimSpace(unit)
	level := store.LevelNational
	if unit == "" && !sc.All {
		if unit, err = s.defaultUnit(ctx, sc); err != nil {
			return Dashboard{}, err
		}
	}
	if unit != "" {
		if !sc.Allows(unit) {
			return Dashboard{}, ErrOutOfScope
		}
		_, lv, err := s.provinceOf(ctx, unit, "unit")
		if err != nil {
			return Dashboard{}, err
		}
		level = store.LevelCommune
		if lv == partyport.LevelProvince {
			level = store.LevelProvince
		}
	}
	from, to, err := periodBounds(period)
	if err != nil {
		return Dashboard{}, problem.Validation.WithField("period", "định dạng YYYY-MM")
	}
	var cells map[store.CellKey]*store.Cell
	var closed map[string]bool
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if cells, e = store.Cells(ctx, tx, store.DashQuery{Level: level, Unit: unit, Period: period, From: from, To: to, Now: now}); e != nil {
			return e
		}
		closed, e = store.ClosedProvinces(ctx, tx, period)
		return e
	})
	if err != nil {
		return Dashboard{}, err
	}

	// Ô (tỉnh, xã) → con: tỉnh với toàn quốc, xã với tỉnh/xã.
	children := map[string]*KPI{}
	provincesSeen := map[string]bool{}
	for k, c := range cells {
		child := k.Commune
		if level == store.LevelNational {
			child = k.Province
		}
		if children[child] == nil {
			children[child] = &KPI{SownAreaHa: decimal.Zero}
		}
		children[child].add(*c, k.Commune)
		provincesSeen[k.Province] = true
	}
	d := Dashboard{Unit: unit, Level: level, Period: period, AsOf: now, Total: KPI{SownAreaHa: decimal.Zero}}
	keys := make([]string, 0, len(children))
	for k := range children {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		kp := *children[k]
		kp.finish()
		d.Total.sum(kp)
		d.Children = append(d.Children, Child{Unit: k, KPI: kp})
	}
	d.Total.finish()
	d.Total.PeriodClosed = len(provincesSeen) > 0
	for pc := range provincesSeen {
		if closed[pc] {
			d.Total.ClosedProvinceList = append(d.Total.ClosedProvinceList, pc)
		} else {
			d.Total.PeriodClosed = false
		}
	}
	sort.Strings(d.Total.ClosedProvinceList)
	if d.Total.ClosedProvinceList == nil {
		d.Total.ClosedProvinceList = []string{}
	}
	if d.Children == nil {
		d.Children = []Child{}
	}
	return d, nil
}

// defaultUnit chọn đơn vị mặc định trong phạm vi: tỉnh nếu có, nếu không là xã đầu tiên.
func (s *Service) defaultUnit(ctx context.Context, sc authz.Scope) (string, error) {
	if len(sc.Units) == 1 {
		return sc.Units[0], nil
	}
	for _, u := range sc.Units {
		prov, lv, err := s.provinceOf(ctx, u, "unit")
		if err != nil {
			return "", err
		}
		if lv == partyport.LevelProvince && prov == u {
			return u, nil
		}
	}
	return sc.Units[0], nil
}

// periodBounds trả [đầu tháng, đầu tháng sau) theo giờ Việt Nam.
func periodBounds(period string) (time.Time, time.Time, error) {
	t, err := time.ParseInLocation("2006-01", period, clock.Zone())
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return t.UTC(), t.AddDate(0, 1, 0).UTC(), nil
}
