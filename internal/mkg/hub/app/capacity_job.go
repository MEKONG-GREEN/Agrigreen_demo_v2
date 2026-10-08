package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// TenantLister trả danh sách tenant cần chạy job.
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// ActiveHubTenants tìm tenant có cơ sở đang dùng từ pool vai ag_ops_jobs (xuyên
// tenant, chỉ đọc hai cột — migration mkg 000005).
func ActiveHubTenants(pool *pgxpool.Pool) TenantLister {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, "SELECT DISTINCT tenant_id FROM mkg_hub.facility_current WHERE status = 'active'")
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
}

// DailyCapacityKey là khóa chống trùng của ảnh chụp sức chứa định kỳ: mỗi cơ sở MỘT lần mỗi
// ngày nghiệp vụ (giờ Việt Nam).
func DailyCapacityKey(facility uuid.UUID, day clock.Date) string {
	return "capacity-daily:" + facility.String() + ":" + day.String()
}

// DailyResult tổng kết một lượt công bố định kỳ.
type DailyResult struct {
	Published, Skipped int
}

// PublishDailyCapacity chụp và công bố sức chứa của MỌI cơ sở đang dùng qua van hub_capacity
// (GIS FN-06 / GIS US-WH-01 AC-2, Q-MKG-02): "ERP cập nhật sức chứa → đến chu kỳ đồng bộ
// theo ngày, GIS tự cập nhật". Chạy ở api-internal (leader, vùng mkg — R-03: không nối DB
// pf, dữ liệu sang nền tảng chỉ qua van). Idempotent theo (cơ sở, ngày): chạy lại cùng ngày
// không xuất lần hai (sổ valve_export có idem_key = DailyCapacityKey). Tác nhân là
// port.SystemActorRef với vai hub_operator.
func (s *Service) PublishDailyCapacity(ctx context.Context, tenants TenantLister) (DailyResult, error) {
	var res DailyResult
	tids, err := tenants(ctx)
	if err != nil {
		return res, err
	}
	now := s.Clock.Now()
	day := clock.BusinessDate(now)
	for _, tid := range tids {
		p := authz.Principal{TenantID: tid, ActorRef: port.SystemActorRef, Roles: []string{RoleHubOperator}}
		var facs []domain.Facility
		err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
			after := uuid.Nil
			for {
				page, err := store.ListCurrent(ctx, tx, "", after, 500)
				if err != nil {
					return err
				}
				for _, f := range page {
					if f.Spec.Status == domain.StatusActive {
						facs = append(facs, f)
					}
				}
				if len(page) < 500 {
					return nil
				}
				after = page[len(page)-1].ID
			}
		})
		if err != nil {
			return res, err
		}
		for _, f := range facs {
			key := DailyCapacityKey(f.ID, day)
			replayed, err := s.write(ctx, tid, func(tx pgx.Tx) error {
				cur, err := store.Current(ctx, tx, f.ID, false)
				if err != nil || cur == nil || cur.Spec.Status != domain.StatusActive {
					return err
				}
				_, err = s.publishCapacity(ctx, tx, p, *cur, now, key)
				return err
			}, func(tx pgx.Tx) (bool, error) {
				prev, err := store.ValveByIdem(ctx, tx, key)
				return prev != nil, err
			})
			if err != nil {
				return res, err
			}
			if replayed {
				res.Skipped++
			} else {
				res.Published++
			}
		}
	}
	return res, nil
}

// DailyCapacityHour / Minute: giờ chạy job (giờ Việt Nam), sau khi ERP chốt số liệu ngày.
const (
	DailyCapacityHour   = 1
	DailyCapacityMinute = 15
)
