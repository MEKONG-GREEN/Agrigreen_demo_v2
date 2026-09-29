package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ReasonStale: bản đồng bộ cũ hơn bản App HTX đã áp (sự kiện đến không theo thứ tự).
const ReasonStale = "bản đồng bộ cũ hơn dữ liệu App HTX đã áp"

// AreaSyncKey là khóa chống trùng của một sự kiện diện tích vụ App HTX (CGH FN-07).
func AreaSyncKey(eventID uuid.UUID) string { return "cropcycle:" + eventID.String() }

// HandleCoopSeasonAreaChanged là inbox.Handler của consumer port.AreaSyncConsumer:
// áp pf.cropcycle.coop_season_area.changed vào diện tích vụ theo QT-03 (CGH FN-07)
// TRONG giao dịch của inbox (vai mechanization_app) — dòng inbox và dòng sổ cùng
// commit. Khóa chống trùng là event_id; bản ghi Admin đã khóa giữ nguyên
// (ResolveArea); sự kiện cũ hơn bản App HTX đã áp bị bỏ qua (giao không theo thứ tự).
// Tác nhân trong sổ là port.SyncActorRef với vai cgh_sync, phạm vi toàn tenant.
func (s *Service) HandleCoopSeasonAreaChanged(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != ccport.EventCoopSeasonAreaChanged {
		return nil
	}
	var p ccport.CoopSeasonAreaChangedV1
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("mechanization: payload %s hỏng: %w", env.EventType, err)
	}
	if s.Orgs == nil || s.Catalogs == nil || s.AdminUnits == nil {
		return ErrDependency.WithDetail("chưa nối pf/party hoặc pf/masterdata")
	}
	ctx = tenant.WithID(ctx, env.TenantID)
	area, err := decimal.NewFromString(strings.TrimSpace(p.AreaHa))
	if err != nil || area.IsNegative() {
		return fmt.Errorf("mechanization: area_ha %q không hợp lệ", p.AreaHa)
	}
	sn := domain.Season{Code: p.SeasonCode, Year: p.SeasonYear}
	if _, err := s.season(ctx, sn); err != nil {
		return err
	}
	at, err := time.Parse(time.RFC3339Nano, p.SourceAt)
	if err != nil {
		at = env.OccurredAt
	}
	at = at.UTC()
	key := AreaSyncKey(env.EventID)
	pr := authz.Principal{TenantID: env.TenantID, ActorRef: port.SyncActorRef, Roles: []string{port.RoleCGHSync}}
	ci, err := s.activeCoop(ctx, authz.Scope{All: true}, p.CooperativeRef, "cooperative_ref")
	if err != nil {
		return err
	}
	if prev, err := store.AreaEventByIdem(ctx, tx, key); err != nil || prev != nil {
		return err
	}
	cur, err := store.AreaByKey(ctx, tx, ci.Ref, sn, true)
	if err != nil {
		return err
	}
	if ok, _ := domain.ResolveArea(cur, domain.AreaFromAppHTX); !ok {
		return nil // QT-03: Admin đã khóa — giữ giá trị nhập tay, không ghi sổ
	}
	if cur != nil && cur.Source == domain.AreaFromAppHTX && at.Before(cur.SourceAt) {
		return nil // ReasonStale
	}
	a := domain.SeasonArea{ID: ids.New(), CooperativeRef: ci.Ref, Season: sn}
	if cur != nil {
		a = *cur
	}
	a.CommuneCode, a.AreaHa, a.Source, a.SourceAt, a.Locked = ci.CommuneCode, area, domain.AreaFromAppHTX, at, false
	_, err = writeAreaTx(ctx, tx, pr, a, domain.AreaSynced, "", s.now(), key)
	return classify(err)
}
