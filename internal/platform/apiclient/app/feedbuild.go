package app

// Job dựng tập dữ liệu (W5.3, port.JobFeedBuild):
//   - harvest_progress_by_commune, collection_volume_by_commune: ô thô từ module chủ (port.AggregateSource —
//     pf/cropcycle) của MỌI tenant đóng góp (port.Contributors), gộp theo xã (đơn vị đếm phân biệt xuyên tenant),
//     áp quy tắc k MỘT lần (domain.ApplyK — ADR-0035 §3) rồi ghi một phiên bản;
//   - admin_units: bảng chiếu lõi k_ref.admin_unit của tenant đơn vị vận hành (W2.5, mọi r_* đọc được), kế thừa
//     từ successor_codes với basis_status pending_c9 (căn cứ chính thức nối khi pf/masterdata có port công bố —
//     ADR-0032, C-9).
// k = tham số aggregation.k_param (publicdata.k_min) tại tenant đơn vị vận hành; chưa đặt → k_default của hợp
// đồng; nhỏ hơn k_default → TỪ CHỐI dựng (port.ErrKBelowFloor). k_used ghi vào mỗi phiên bản.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// BuiltFeeds là các tập có nguồn ở W5.3, theo thứ tự dựng.
var BuiltFeeds = []string{port.FeedAdminUnits, port.FeedHarvestProgress, port.FeedCollectionVolume}

// BuildAll dựng mọi tập có nguồn; lỗi một tập không chặn tập khác (trả lỗi gộp).
func (s *Service) BuildAll(ctx context.Context) ([]BuildResult, error) {
	var out []BuildResult
	var errs []error
	for _, f := range BuiltFeeds {
		var (
			r   BuildResult
			err error
		)
		if f == port.FeedAdminUnits {
			r, err = s.BuildAdminUnits(ctx)
		} else {
			r, err = s.BuildAggregate(ctx, f)
		}
		if err != nil {
			s.log().ErrorContext(ctx, "feed: dựng tập dữ liệu lỗi", "event", "integration.dataset_build_failed", "dataset", f, "outcome", "error", "error", err.Error())
			errs = append(errs, fmt.Errorf("%s: %w", f, err))
			continue
		}
		out = append(out, r)
	}
	// W5.5: feed lớp partner theo bên cấp — chỉ khi gốc lắp ráp đã nối nguồn + danh sách bên cấp.
	for _, f := range PartnerFeeds {
		if s.Grantors == nil || s.PartnerSources[f] == nil {
			continue
		}
		rs, err := s.BuildPartner(ctx, f)
		out = append(out, rs...)
		if err != nil {
			s.log().ErrorContext(ctx, "feed: dựng tập dữ liệu lỗi", "event", "integration.dataset_build_failed", "dataset", f, "outcome", "error", "error", err.Error())
			errs = append(errs, fmt.Errorf("%s: %w", f, err))
		}
	}
	// W11.81: feed tham chiếu của dữ liệu đã công bố (price_board_daily) — chỉ khi gốc lắp ráp đã nối nguồn + đơn vị công bố.
	for _, f := range PublishedFeeds {
		if s.PublishedSources[f] == nil || len(s.Publishers[f]) == 0 {
			continue
		}
		r, err := s.BuildPublished(ctx, f)
		if err != nil {
			s.log().ErrorContext(ctx, "feed: dựng tập dữ liệu lỗi", "event", "integration.dataset_build_failed", "dataset", f, "outcome", "error", "error", err.Error())
			errs = append(errs, fmt.Errorf("%s: %w", f, err))
			continue
		}
		out = append(out, r)
	}
	// W5.6: feed của cặp song phương — chép bản ghi mới của sổ song phương (chỉ khi gốc lắp ráp đã nối sổ).
	if s.Bilateral != nil {
		rs, err := s.BuildBilateral(ctx)
		out = append(out, rs...)
		if err != nil {
			s.log().ErrorContext(ctx, "feed: dựng tập dữ liệu lỗi", "event", "integration.dataset_build_failed", "dataset", port.FeedBilateral, "outcome", "error", "error", err.Error())
			errs = append(errs, fmt.Errorf("%s: %w", port.FeedBilateral, err))
		}
	}
	return out, errors.Join(errs...)
}

// kFor đọc k của tập tổng hợp tại tenant đơn vị vận hành.
func (s *Service) kFor(ctx context.Context, a *integration.Aggregation, at time.Time) (int, error) {
	k := a.KDefault
	if s.Params != nil {
		v, err := s.Params.At(tenant.WithID(ctx, s.OperatorTenant), a.KParam, at)
		switch {
		case err == nil:
			k = int(v.Value.IntPart())
		case !errors.Is(err, params.ErrParamNotFound):
			return 0, ErrDependencyUnavailable.WithDetail("không đọc được tham số " + a.KParam).WithCause(err)
		}
	}
	if k < a.KDefault || k < 2 {
		return 0, port.ErrKBelowFloor.WithDetail(fmt.Sprintf("%s = %d < %d", a.KParam, k, a.KDefault))
	}
	return k, nil
}

func dateUTC(d clock.Date) time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

type mergedCell struct {
	port.AggregateCell
	units map[string]struct{}
}

func cellKey(dataset string, c port.AggregateCell, area string) string {
	if dataset == port.FeedCollectionVolume {
		return area + integration.EntityKeySep + c.SeasonCode + integration.EntityKeySep + c.PeriodMonth
	}
	return area + integration.EntityKeySep + c.SeasonCode
}

// BuildAggregate dựng một phiên bản của tập tổng hợp dataset.
func (s *Service) BuildAggregate(ctx context.Context, dataset string) (BuildResult, error) {
	c, err := FeedContract(dataset, 0)
	if err != nil {
		return BuildResult{}, err
	}
	a := c.Aggregation
	if a == nil {
		return BuildResult{}, port.ErrUnknownFeed.WithDetail(dataset + " không phải tập tổng hợp")
	}
	if s.Aggregates == nil || s.Contributors == nil {
		return BuildResult{}, ErrDependencyUnavailable.WithDetail("chưa nối nguồn tập tổng hợp hoặc danh sách đóng góp")
	}
	now := s.Clock.Now()
	k, err := s.kFor(ctx, a, now)
	if err != nil {
		return BuildResult{}, err
	}
	tenants, err := s.Contributors.Contributors(ctx, dataset, now)
	if err != nil {
		return BuildResult{}, err
	}
	merged := map[string]*mergedCell{}
	for _, t := range tenants {
		cells, err := s.Aggregates.AggregateCells(ctx, dataset, t, now)
		if err != nil {
			return BuildResult{}, fmt.Errorf("nguồn %s của tenant %s: %w", dataset, t, err)
		}
		for _, cell := range cells {
			key := cellKey(dataset, cell, cell.CommuneCode)
			m := merged[key]
			if m == nil {
				m = &mergedCell{AggregateCell: cell, units: map[string]struct{}{}}
				m.Measures = map[string]decimal.Decimal{}
				m.Units = nil
				merged[key] = m
			} else {
				m.DateFrom, m.DateTo = minDate(m.DateFrom, cell.DateFrom), maxDate(m.DateTo, cell.DateTo)
			}
			for n, v := range cell.Measures {
				m.Measures[n] = m.Measures[n].Add(v)
			}
			for _, u := range cell.Units {
				m.units[t.String()+":"+u] = struct{}{} // phân biệt xuyên tenant
			}
		}
	}
	kcells := make([]domain.KCell, 0, len(merged))
	for key, m := range merged {
		group := cellKey(dataset, m.AggregateCell, m.ProvinceCode)
		kcells = append(kcells, domain.KCell{Key: key, Group: group, WithheldKey: group, Units: m.units})
	}
	hyst := a.Hysteresis
	contributors := len(tenants)
	adminAsOf := dateUTC(clock.BusinessDate(now))
	return s.publish(ctx, publishInput{contract: c, asOf: now, adminUnitAsOf: &adminAsOf, kUsed: &k, hysteresis: &hyst, contributors: &contributors,
		shape: func(_ pgx.Tx, prev map[string]bool) (shaped, error) {
			kr := domain.ApplyK(kcells, domain.KRule{K: k, Hysteresis: hyst}, prev)
			recs := make([]domain.KeyedRecord, 0, len(kr.Published)+len(kr.Withheld))
			for _, key := range kr.Published {
				r, err := aggregateRecord(dataset, merged[key].AggregateCell, false)
				if err != nil {
					return shaped{}, err
				}
				recs = append(recs, domain.KeyedRecord{EntityKey: key, Record: r})
			}
			for _, w := range kr.Withheld {
				sum := mergedCell{AggregateCell: merged[w.Members[0]].AggregateCell}
				sum.Measures = map[string]decimal.Decimal{}
				sum.DateFrom, sum.DateTo = "", ""
				for _, mk := range w.Members {
					for n, v := range merged[mk].Measures {
						sum.Measures[n] = sum.Measures[n].Add(v)
					}
				}
				r, err := aggregateRecord(dataset, sum.AggregateCell, true)
				if err != nil {
					return shaped{}, err
				}
				recs = append(recs, domain.KeyedRecord{EntityKey: w.Key, Record: r})
			}
			n := kr.Suppressed
			return shaped{records: recs, marks: kr.Marks, suppressed: &n}, nil
		}})
}

func minDate(a, b string) string {
	if a == "" || (b != "" && b < a) {
		return b
	}
	return a
}

func maxDate(a, b string) string {
	if b > a {
		return b
	}
	return a
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// aggregateRecord dựng bản ghi theo hợp đồng của tập (withheld: dòng province_withheld của tỉnh).
func aggregateRecord(dataset string, c port.AggregateCell, withheld bool) (json.RawMessage, error) {
	area, level, validFrom := c.CommuneCode, "commune", c.AdminUnitValidFrom
	if withheld {
		area, level, validFrom = c.ProvinceCode, "province_withheld", c.ProvinceValidFrom
	}
	rec := map[string]any{"area_code": area, "area_level": level, "province_code": c.ProvinceCode, "admin_unit_valid_from": validFrom,
		"season_code": c.SeasonCode}
	switch dataset {
	case port.FeedHarvestProgress:
		sown, harvested := c.Measures[port.MeasureAreaSown], c.Measures[port.MeasureAreaHarvested]
		share := decimal.Zero
		if sown.IsPositive() {
			share = decimal.Min(harvested.Div(sown), decimal.NewFromInt(1))
		}
		rec["season_year"] = c.SeasonYear
		rec["area_ha_sown"] = sown.StringFixed(2)
		rec["area_ha_harvested"] = harvested.StringFixed(2)
		rec["harvested_share"] = share.StringFixed(4)
		rec["expected_harvest_from"] = nullable(c.DateFrom)
		rec["expected_harvest_to"] = nullable(c.DateTo)
	case port.FeedCollectionVolume:
		rec["period_month"] = c.PeriodMonth
		rec["rolls_collected"] = c.Measures[port.MeasureRolls].IntPart()
		rec["area_ha_collected"] = c.Measures[port.MeasureAreaCollected].StringFixed(2)
	default:
		return nil, port.ErrUnknownFeed.WithDetail(dataset)
	}
	return json.Marshal(rec)
}

// ---------------------------------------------------------------------------
// admin_units (tham chiếu, từ k_ref.admin_unit)
// ---------------------------------------------------------------------------

// BuildAdminUnits dựng một phiên bản của feed admin_units.
func (s *Service) BuildAdminUnits(ctx context.Context) (BuildResult, error) {
	c, err := FeedContract(port.FeedAdminUnits, 0)
	if err != nil {
		return BuildResult{}, err
	}
	now := s.Clock.Now()
	adminAsOf := dateUTC(clock.BusinessDate(now))
	return s.publish(ctx, publishInput{contract: c, asOf: now, adminUnitAsOf: &adminAsOf, shape: func(tx pgx.Tx, _ map[string]bool) (shaped, error) {
		units, err := store.AdminUnitRows(ctx, tx)
		if err != nil {
			return shaped{}, err
		}
		recs, err := adminUnitRecords(units)
		return shaped{records: recs}, err
	}})
}

const legalBasisPending = "k_ref.admin_unit.successor_codes — chưa có căn cứ chính thức (C-9, ADR-0032)"

// adminUnitRecords dựng bản ghi admin_units: kế thừa lấy phiên bản của đơn vị kế thừa có hiệu lực ngày đóng.
func adminUnitRecords(units []store.AdminUnitRow) ([]domain.KeyedRecord, error) {
	byCode := map[string][]store.AdminUnitRow{}
	for _, u := range units {
		byCode[u.Code] = append(byCode[u.Code], u)
	}
	out := make([]domain.KeyedRecord, 0, len(units))
	for _, u := range units {
		succ := []any{}
		if u.ValidTo != nil {
			codes := slices.Clone(u.SuccessorCodes)
			slices.Sort(codes)
			for _, sc := range slices.Compact(codes) {
				vf := *u.ValidTo
				for _, v := range byCode[sc] {
					if !v.ValidFrom.After(*u.ValidTo) && (v.ValidTo == nil || v.ValidTo.After(*u.ValidTo)) {
						vf = v.ValidFrom
					}
				}
				succ = append(succ, map[string]any{"code": sc, "valid_from": vf.Format(time.DateOnly), "effective_on": u.ValidTo.Format(time.DateOnly),
					"area_share": nil, "basis_status": "pending_c9", "legal_basis": legalBasisPending})
			}
		}
		var validTo, parent any
		if u.ValidTo != nil {
			validTo = u.ValidTo.Format(time.DateOnly)
		}
		if u.ParentCode != nil && *u.ParentCode != "" {
			parent = *u.ParentCode
		}
		rec, err := json.Marshal(map[string]any{"code": u.Code, "valid_from": u.ValidFrom.Format(time.DateOnly), "valid_to": validTo, "name": u.Name,
			"level": u.Level, "parent_code": parent, "successors": succ})
		if err != nil {
			return nil, err
		}
		out = append(out, domain.KeyedRecord{EntityKey: u.Code + integration.EntityKeySep + u.ValidFrom.Format(time.DateOnly), Record: rec})
	}
	slices.SortFunc(out, func(a, b domain.KeyedRecord) int { return strings.Compare(a.EntityKey, b.EntityKey) })
	return out, nil
}
