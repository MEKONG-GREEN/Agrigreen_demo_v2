package app

// Sản phẩm dữ liệu collection_volume v1 cho bên thứ ba (G5, ADR-0022; g5-conventions §6;
// proto SUB-DATA — nhà máy/bên mua rơm).
//
// pf/apiclient (cmd/api-public) đã kiểm cấp quyền dp_collection_volume và đọc k từ
// publicdata.k_min TRƯỚC khi gọi. Ở đây, trên tenant bên cấp (pool vai cropcycle_app,
// tenant.WithTenantTx — R-05, không BYPASSRLS):
//   - nguồn: bảng kê thu gom HTX nhận qua van htx_collection_statement (bảng chiếu = revision cao
//     nhất) + vụ (pf/cooperative Seasons.Codes — W3.3, vụ ở pf_coop);
//   - thửa → xã, nông hộ, diện tích: pf/cooperative (cooperativeport.Plots.Facts — chủ quan hệ thửa ↔ nông hộ; W3.2 đọc qua pf/geo);
//     xã → tỉnh: pf/masterdata (AdminUnits);
//   - ô (tỉnh × xã × vụ × tháng giờ VN), ẩn theo domain.KAnonRule (ẩn sơ cấp < k + ẩn bổ sung, tính
//     trên TOÀN BỘ dữ liệu bên cấp rồi mới lọc) — ô bị ẩn không bao giờ trả, chỉ đếm Suppressed;
//   - chỉ điền trường của DTO CollectionVolumeV1: không thửa, nông hộ, HTX, khối lượng cân, giá.
//
// Bảng kê bị bỏ khỏi mọi ô (không đếm vào Suppressed): chưa gắn vụ / vụ không có ở pf/cooperative;
// thửa không có ở pf/cooperative; xã không tra được tỉnh hoặc mã sai dạng hợp đồng.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var _ apiclientport.CollectionVolumeSource = (*Service)(nil)

// Giới hạn trang của collection_volume.
const (
	DefaultVolumeLimit = 100
	MaxVolumeLimit     = 1000
)

var (
	monthRe    = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)
	communeRe  = regexp.MustCompile(`^[0-9]{5}$`)
	provinceRe = regexp.MustCompile(`^[0-9]{2}$`)
	seasonRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)
)

type volumeAgg struct {
	cell  domain.VolumeCell
	hh    map[uuid.UUID]struct{}
	plots map[uuid.UUID]decimal.Decimal
	asOf  time.Time
}

func validateVolumeQuery(q apiclientport.CollectionQuery) error {
	switch {
	case q.DataTenant == uuid.Nil:
		return tenant.ErrMissing
	case q.K < 2:
		return problem.Validation.WithField("k", "ngưỡng k phải ≥ 2 (publicdata.k_min)")
	case q.SeasonCode != "" && !seasonRe.MatchString(q.SeasonCode):
		return problem.Validation.WithField("season_code", "mã vụ không hợp lệ")
	case q.ProvinceCode != "" && !provinceRe.MatchString(q.ProvinceCode):
		return problem.Validation.WithField("province_code", "mã tỉnh 2 chữ số")
	case q.FromMonth != "" && !monthRe.MatchString(q.FromMonth):
		return problem.Validation.WithField("from_month", "YYYY-MM")
	case q.ToMonth != "" && !monthRe.MatchString(q.ToMonth):
		return problem.Validation.WithField("to_month", "YYYY-MM")
	case q.FromMonth != "" && q.ToMonth != "" && q.ToMonth < q.FromMonth:
		return problem.Validation.WithField("to_month", "không trước from_month")
	}
	return nil
}

// CollectionCells cài đặt apiclientport.CollectionVolumeSource.
func (s *Service) CollectionCells(ctx context.Context, q apiclientport.CollectionQuery) (apiclientport.CollectionPage, error) {
	if err := validateVolumeQuery(q); err != nil {
		return apiclientport.CollectionPage{}, err
	}
	if s.PlotFacts == nil || s.Seasons == nil || s.Units == nil {
		return apiclientport.CollectionPage{}, ErrDependency.WithDetail("chưa nối pf/cooperative Plots/Seasons hoặc pf/masterdata AdminUnits")
	}
	after := ""
	if q.After != "" {
		b, err := base64.RawURLEncoding.DecodeString(q.After)
		if err != nil {
			return apiclientport.CollectionPage{}, problem.Validation.WithField("cursor", "con trỏ không hợp lệ")
		}
		after = string(b)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultVolumeLimit
	}
	limit = min(limit, MaxVolumeLimit)

	keys, aggs, err := s.volumeAggs(ctx, q.DataTenant, q.AsOf)
	if err != nil {
		return apiclientport.CollectionPage{}, err
	}
	cells := make([]domain.VolumeCell, len(keys))
	for i, k := range keys {
		cells[i] = aggs[k].cell
	}
	domain.Suppress(cells, q.K)

	var page apiclientport.CollectionPage
	page.Items = []apiclientport.CollectionVolumeV1{}
	more := false
	for i, c := range cells {
		if !volumeMatches(c, q) {
			continue
		}
		if c.Suppressed {
			page.Suppressed++ // đếm trên cả bộ lọc, không chỉ trang
			continue
		}
		if keys[i] <= after {
			continue
		}
		if len(page.Items) == limit {
			more = true
			continue
		}
		a := aggs[keys[i]]
		area := decimal.Zero
		for _, v := range a.plots {
			area = area.Add(v)
		}
		page.Items = append(page.Items, apiclientport.CollectionVolumeV1{
			ProvinceCode: c.ProvinceCode, CommuneCode: c.CommuneCode, SeasonCode: c.SeasonCode, PeriodMonth: c.PeriodMonth,
			RollsCollected: c.Rolls, AreaHaCollected: area.StringFixed(2), KApplied: q.K, AsOf: a.asOf.UTC().Format(time.RFC3339),
		})
		if len(page.Items) == limit {
			page.Next = base64.RawURLEncoding.EncodeToString([]byte(keys[i]))
		}
	}
	if !more {
		page.Next = ""
	}
	return page, nil
}

// volumeAggs gộp bảng kê hiện hành tới asOf của tenant thành ô (tỉnh × xã × vụ × tháng) CHƯA ẩn, kèm tập nông hộ
// phân biệt và diện tích thửa; keys đã sắp. Dùng chung cho sản phẩm collection_volume (ẩn theo KAnonRule ở đây)
// và nguồn ô thô của feed collection_volume_by_commune (ẩn ở pf/apiclient — ADR-0035).
func (s *Service) volumeAggs(ctx context.Context, dataTenant uuid.UUID, asOf time.Time) ([]string, map[string]*volumeAgg, error) {
	var rows []store.VolumeRow
	if err := s.inTx(ctx, dataTenant, func(tx pgx.Tx) error {
		var e error
		rows, e = store.VolumeRows(ctx, tx, asOf)
		return e
	}); err != nil {
		return nil, nil, err
	}
	tctx := tenant.WithID(ctx, dataTenant)
	refs := make([]uuid.UUID, 0, len(rows))
	seasonRefs := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		refs = append(refs, r.PlotRef)
		if r.SeasonRef != nil {
			seasonRefs = append(seasonRefs, *r.SeasonRef)
		}
	}
	slices.SortFunc(refs, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	refs = slices.Compact(refs)
	plots, err := s.PlotFacts.Facts(tctx, refs)
	if err != nil {
		return nil, nil, ErrDependency.WithDetail("không đọc được thửa (pf/cooperative)").WithCause(err)
	}
	slices.SortFunc(seasonRefs, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	seasons, err := s.Seasons.Codes(tctx, slices.Compact(seasonRefs))
	if err != nil {
		return nil, nil, ErrDependency.WithDetail("không đọc được vụ (pf/cooperative)").WithCause(err)
	}
	at := asOf
	if at.IsZero() {
		at = s.now()
	}
	provinces := map[string]string{}
	aggs := map[string]*volumeAgg{}
	for _, r := range rows {
		if r.SeasonRef == nil {
			continue
		}
		sk, known := seasons[*r.SeasonRef]
		if !known {
			continue
		}
		p, ok := plots[r.PlotRef]
		if !ok || !communeRe.MatchString(p.CommuneCode) {
			continue
		}
		prov, ok := provinces[p.CommuneCode]
		if !ok {
			if prov, err = provinceOf(tctx, s.Units, p.CommuneCode, at, r.CollectedOn); err != nil {
				return nil, nil, err
			}
			provinces[p.CommuneCode] = prov
		}
		if !provinceRe.MatchString(prov) {
			continue
		}
		season := fmt.Sprintf("%d-%s", sk.SeasonYear, sk.SeasonCode)
		if !seasonRe.MatchString(season) {
			continue
		}
		c := domain.VolumeCell{ProvinceCode: prov, CommuneCode: p.CommuneCode, SeasonCode: season, PeriodMonth: r.CollectedOn.Format("2006-01")}
		a := aggs[c.Key()]
		if a == nil {
			a = &volumeAgg{cell: c, hh: map[uuid.UUID]struct{}{}, plots: map[uuid.UUID]decimal.Decimal{}}
			aggs[c.Key()] = a
		}
		a.cell.Rolls += r.Rolls
		if p.HouseholdRef != uuid.Nil {
			a.hh[p.HouseholdRef] = struct{}{}
		}
		a.plots[p.Ref] = p.AreaHa // mỗi thửa tính diện tích một lần trong ô
		if r.ReceivedAt.After(a.asOf) {
			a.asOf = r.ReceivedAt
		}
	}
	keys := make([]string, 0, len(aggs))
	for k, a := range aggs {
		a.cell.Households = int64(len(a.hh))
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys, aggs, nil
}

func volumeMatches(c domain.VolumeCell, q apiclientport.CollectionQuery) bool {
	return (q.SeasonCode == "" || c.SeasonCode == q.SeasonCode) && (q.ProvinceCode == "" || c.ProvinceCode == q.ProvinceCode) &&
		(q.FromMonth == "" || c.PeriodMonth >= q.FromMonth) && (q.ToMonth == "" || c.PeriodMonth <= q.ToMonth)
}

// provinceOf tra tỉnh của xã: tại at, không có thì tại ngày gom (xã đã sáp nhập sau đó); đi lên qua
// cấp huyện của dữ liệu lịch sử trước 01/07/2025. Không tra được → "" (bảng kê bị bỏ).
func provinceOf(ctx context.Context, units partyport.AdminUnits, commune string, at, collectedOn time.Time) (string, error) {
	for _, t := range []time.Time{at, collectedOn} {
		code := commune
		for range 3 {
			u, err := units.Get(ctx, code, t)
			if errors.Is(err, partyport.ErrUnitNotFound) {
				break
			}
			if err != nil {
				return "", ErrDependency.WithDetail("không đọc được đơn vị hành chính (pf/masterdata)").WithCause(err)
			}
			if u.Level == partyport.LevelProvince {
				return u.Code, nil
			}
			if u.ParentCode == "" {
				break
			}
			code = u.ParentCode
		}
	}
	return "", nil
}
