package app

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Người đọc sổ xuất / cảnh báo: metering_viewer hoặc platform_admin của tenant CHỦ DỮ LIỆU (RLS).
var egressReaders = []string{port.RoleMeteringViewer, authz.RolePlatformAdmin}

// AdminUsage (Admin API, tenant đơn vị vận hành) trả tổng theo ngày × sản phẩm của một khách hàng.
func (s *Service) AdminUsage(ctx context.Context, client uuid.UUID, from, to time.Time) ([]meter.UsageDay, error) {
	p, err := authz.Require(ctx, statementReaders...)
	if err != nil {
		return nil, err
	}
	if to.Sub(from) > 366*24*time.Hour {
		return nil, problem.Validation.WithField("to", "tối đa 366 ngày")
	}
	return s.UsageOf(ctx, p.TenantID, client, from, to)
}

// UsageEvents liệt kê dòng sổ sử dụng của một khách hàng (Admin API, cursor).
func (s *Service) UsageEvents(ctx context.Context, client, after uuid.UUID, limit int) (Page[store.UsageRow], error) {
	p, err := authz.Require(ctx, statementReaders...)
	if err != nil {
		return Page[store.UsageRow]{}, err
	}
	limit = clampLimit(limit)
	var rows []store.UsageRow
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.UsageEvents(ctx, tx, client, after, limit+1)
		return e
	})
	return page(rows, limit, func(r store.UsageRow) uuid.UUID { return r.ID }), err
}

// EgressEvents liệt kê sổ xuất của tenant người gọi (chủ dữ liệu).
func (s *Service) EgressEvents(ctx context.Context, f store.EgressFilter) (Page[store.EgressRow], error) {
	p, err := authz.Require(ctx, egressReaders...)
	if err != nil {
		return Page[store.EgressRow]{}, err
	}
	if f.Channel != "" && !domain.ValidChannel(meter.Channel(f.Channel)) {
		return Page[store.EgressRow]{}, problem.Validation.WithField("channel", "kênh không hợp lệ")
	}
	limit := clampLimit(f.Limit)
	f.Limit = limit + 1
	var rows []store.EgressRow
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.EgressEvents(ctx, tx, f)
		return e
	})
	return page(rows, limit, func(r store.EgressRow) uuid.UUID { return r.ID }), err
}

// Anomalies liệt kê cảnh báo khối lượng xuất bất thường của tenant người gọi.
func (s *Service) Anomalies(ctx context.Context, channel string, after uuid.UUID, limit int) (Page[store.AnomalyRow], error) {
	p, err := authz.Require(ctx, egressReaders...)
	if err != nil {
		return Page[store.AnomalyRow]{}, err
	}
	if channel != "" && !domain.ValidChannel(meter.Channel(channel)) {
		return Page[store.AnomalyRow]{}, problem.Validation.WithField("channel", "kênh không hợp lệ")
	}
	limit = clampLimit(limit)
	var rows []store.AnomalyRow
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.Anomalies(ctx, tx, channel, after, limit+1)
		return e
	})
	return page(rows, limit, func(r store.AnomalyRow) uuid.UUID { return r.ID }), err
}

// ParamView là một tham số hiệu lực của pf/metering tại thời điểm tra.
type ParamView struct {
	Key    string
	Value  *string // decimal dạng chuỗi; nil = chưa đặt (giá tùy chọn)
	Source string  // "default" | "v<N>" (phiên bản đã duyệt ở pf/masterdata) | "unset"
}

// EffectiveParams (Admin API) liệt kê tham số DLP theo kênh và tham số giá theo sản phẩm hiệu lực tại
// at cho tenant người gọi. Đề xuất/duyệt tham số qua /v1/masterdata/params (setter ≠ approver, R-56).
func (s *Service) EffectiveParams(ctx context.Context, at time.Time) ([]ParamView, error) {
	p, err := authz.Require(ctx, port.RoleMeteringViewer, port.RoleBillingOfficer, port.RoleBillingApprover, authz.RolePlatformAdmin)
	if err != nil {
		return nil, err
	}
	var out []ParamView
	for _, ch := range meter.Channels {
		ep, err := s.EgressParams(ctx, p.TenantID, ch, at)
		if err != nil {
			return nil, err
		}
		q, f, r := fmt.Sprint(ep.DailyQuota), ep.AnomalyFactor.String(), fmt.Sprint(ep.AnomalyRecords)
		for _, it := range []struct {
			format string
			v      *string
		}{{port.ParamEgressDailyQuotaFmt, &q}, {port.ParamEgressAnomalyFactorFmt, &f}, {port.ParamEgressAnomalyRecordsFmt, &r}} {
			key := port.EgressParam(it.format, ch)
			out = append(out, ParamView{Key: key, Value: it.v, Source: ep.Source[key]})
		}
	}
	keys := []string{fmt.Sprintf(port.ParamPriceSubscriptionFmt, domain.PricePlanDefault)}
	if prods, err := dataproduct.All(); err == nil {
		for _, pr := range prods {
			keys = append(keys, fmt.Sprintf(port.ParamPricePerUnitFmt, domain.PricePlanDefault, pr.Name))
		}
	}
	sort.Strings(keys[1:])
	for _, key := range keys {
		v, ok, err := s.param(ctx, p.TenantID, key, at)
		if err != nil {
			return nil, err
		}
		pv := ParamView{Key: key, Source: "unset"}
		if ok {
			str := v.Value.String()
			pv.Value, pv.Source = &str, fmt.Sprintf("v%d", v.Version)
		}
		out = append(out, pv)
	}
	return out, nil
}

// RebuildTenant dựng lại bảng chiếu (bộ đếm sử dụng, bộ đếm xuất, bảng kê hiện hành) của tenant tid chỉ
// từ sổ (R-22). Chạy bằng vai metering_app; RLS giới hạn vào tenant đó.
func (s *Service) RebuildTenant(ctx context.Context, tid uuid.UUID) (store.Rebuilt, error) {
	var r store.Rebuilt
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		r, e = store.RebuildProjections(ctx, tx)
		return e
	})
	return r, err
}
