package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	contentport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

// Vai nhận thông báo chủ đề.
var (
	// officerRoles: cán bộ khuyến nông mọi cấp — người nhận chỉ đạo khi không có danh sách.
	officerRoles = []string{partyport.RoleKNOfficer, partyport.RoleKNProvinceAdmin, partyport.RoleKNNationalAdmin}
	// orgWatchRoles: người theo dõi tổ chức trong địa bàn.
	orgWatchRoles = []string{partyport.RoleKNOfficer, partyport.RoleKNProvinceAdmin, partyport.RoleKNNationalAdmin, partyport.RoleRegistryAdmin}
)

// HandledEvents là mọi sự kiện consumer của module xử lý (trừ pf.notify.push.requested).
var HandledEvents = []string{
	extport.EventDirectiveIssued, extport.EventTaskEscalated, contentport.EventAlertPublished,
	contentport.EventDocumentPublished, syncport.EventConflictOpened, syncport.EventDeviceRevoked,
	partyport.EventOrgDeactivated,
}

// HandleEvent là inbox.Handler cho các sự kiện nguồn: dựng thông báo, xếp lần gửi
// đẩy, trong CÙNG giao dịch với dòng shared.inbox (R-61). Giao lặp → inbox bỏ qua;
// kể cả gọi thẳng hai lần, chỉ mục duy nhất (sự kiện, người nhận) chặn trùng.
func (s *Service) HandleEvent(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	switch env.EventType {
	case extport.EventDirectiveIssued:
		var p extport.DirectiveIssuedV1
		if err := decode(env, &p); err != nil {
			return err
		}
		return s.onDirective(ctx, tx, env, p)
	case extport.EventTaskEscalated:
		var p extport.TaskEscalatedV1
		if err := decode(env, &p); err != nil {
			return err
		}
		return s.onTaskEscalated(ctx, tx, env, p)
	case contentport.EventAlertPublished:
		var p contentport.AlertPublishedV1
		if err := decode(env, &p); err != nil {
			return err
		}
		a, err := s.contentAudience(ctx, p.AudienceLevel, p.AudienceUnits, env.OccurredAt)
		if err != nil {
			return err
		}
		return s.deliver(ctx, tx, env, []domain.Notification{domain.AlertTemplate(p, env.OccurredAt).ForAudience(a)})
	case contentport.EventDocumentPublished:
		var p contentport.DocumentPublishedV1
		if err := decode(env, &p); err != nil {
			return err
		}
		if p.Kind == "alert" {
			return nil // cảnh báo đã có pf.content.alert.published (cùng giao dịch) — không thông báo hai lần
		}
		a, err := s.contentAudience(ctx, p.AudienceLevel, p.AudienceUnits, env.OccurredAt)
		if err != nil {
			return err
		}
		return s.deliver(ctx, tx, env, []domain.Notification{domain.DocumentTemplate(p, env.OccurredAt).ForAudience(a)})
	case syncport.EventConflictOpened:
		var p syncport.ConflictOpenedV1
		if err := decode(env, &p); err != nil {
			return err
		}
		a := domain.Audience{OrgRef: p.OwnerOrgRef, Roles: []string{syncport.RoleConflictResolver}}
		if p.OwnerOrgRef == nil {
			a = domain.Audience{National: true, Roles: []string{syncport.RoleConflictResolver}}
		}
		return s.deliver(ctx, tx, env, []domain.Notification{domain.ConflictTemplate(p, env.OccurredAt).ForAudience(a)})
	case syncport.EventDeviceRevoked:
		var p syncport.DeviceRevokedV1
		if err := decode(env, &p); err != nil {
			return err
		}
		_, err := store.DisableDevice(ctx, tx, p.DeviceID, domain.TokenDisabledDeviceRevoked, s.now())
		return err
	case partyport.EventOrgDeactivated:
		var p partyport.OrgDeactivatedV1
		if err := decode(env, &p); err != nil {
			return err
		}
		if _, err := store.DisableOrg(ctx, tx, p.OrgRef, s.now()); err != nil {
			return err
		}
		a := domain.Audience{Units: []string{p.AdminUnitCode}, Roles: orgWatchRoles}
		if p.AdminUnitCode == "" {
			a = domain.Audience{National: true, Roles: orgWatchRoles}
		}
		return s.deliver(ctx, tx, env, []domain.Notification{domain.OrgDeactivatedTemplate(p, env.OccurredAt).ForAudience(a)})
	}
	return nil // sự kiện không thuộc module: bỏ qua (binding thừa không làm hỏng hàng đợi)
}

func decode(env outbox.Envelope, v any) error {
	if err := json.Unmarshal(env.Payload, v); err != nil {
		// Payload hỏng không bao giờ tự lành: trả lỗi để đi DLQ sau giới hạn giao lại (R-62).
		return fmt.Errorf("notify: payload %s hỏng: %w", env.EventType, err)
	}
	return nil
}

// onDirective: người nhận là target_refs (danh bạ chụp lúc ban hành). Không có →
// tra danh bạ hiện tại theo vùng; vẫn không có → thông báo chủ đề cho cán bộ của vùng.
// Chỉ đạo khẩn còn được xếp vào email tóm tắt.
func (s *Service) onDirective(ctx context.Context, tx pgx.Tx, env outbox.Envelope, p extport.DirectiveIssuedV1) error {
	tpl := domain.DirectiveTemplate(p, env.OccurredAt)
	targets := domain.DedupRefs(p.TargetRefs)
	units := domain.DedupUnits(p.AudienceUnits)
	if len(targets) == 0 && s.Staff != nil {
		staff, err := s.Staff.ActiveStaff(ctx, env.TenantID, units)
		if err != nil {
			return err
		}
		for _, st := range staff {
			targets = append(targets, st.ActorRef)
		}
		targets = domain.DedupRefs(targets)
	}
	var ns []domain.Notification
	for _, r := range targets {
		ns = append(ns, tpl.For(r))
	}
	if len(ns) == 0 && len(units) > 0 {
		exp, err := s.expandUnits(ctx, units, env.OccurredAt)
		if err != nil {
			return err
		}
		ns = append(ns, tpl.ForAudience(domain.Audience{Units: exp, Roles: officerRoles}))
	}
	if p.Severity == port.SeverityUrgent {
		if err := store.InsertDigestItem(ctx, tx, env.TenantID, domain.DigestItem{DirectiveID: p.DirectiveID,
			AudienceUnits: units, IssuedAt: tpl.CreatedAt}, env.EventID, s.now()); err != nil {
			return err
		}
	}
	return s.deliver(ctx, tx, env, ns)
}

// onTaskEscalated: người được giao (nếu có) + cán bộ cấp tỉnh trong danh bạ của tỉnh
// nhận leo thang. Tỉnh chưa có cán bộ trong danh bạ → thông báo chủ đề cho vai
// kn_province_admin của tỉnh.
func (s *Service) onTaskEscalated(ctx context.Context, tx pgx.Tx, env outbox.Envelope, p extport.TaskEscalatedV1) error {
	tpl := domain.TaskEscalatedTemplate(p, env.OccurredAt)
	var recipients []uuid.UUID
	if p.AssigneeRef != nil {
		recipients = append(recipients, *p.AssigneeRef)
	}
	admins := 0
	if s.Staff != nil && p.ProvinceCode != "" {
		staff, err := s.Staff.ActiveStaff(ctx, env.TenantID, []string{p.ProvinceCode})
		if err != nil {
			return err
		}
		for _, st := range staff {
			if st.ProvinceLevel() && st.ProvinceCode == p.ProvinceCode {
				recipients = append(recipients, st.ActorRef)
				admins++
			}
		}
	}
	var ns []domain.Notification
	for _, r := range domain.DedupRefs(recipients) {
		ns = append(ns, tpl.For(r))
	}
	if admins == 0 && p.ProvinceCode != "" {
		ns = append(ns, tpl.ForAudience(domain.Audience{Units: []string{p.ProvinceCode}, Roles: []string{partyport.RoleKNProvinceAdmin}}))
	}
	return s.deliver(ctx, tx, env, ns)
}

// contentAudience: national (hoặc không có đơn vị) → toàn quốc; còn lại → các đơn
// vị, tỉnh được mở rộng ra mọi xã để cán bộ xã của tỉnh cũng nhận.
func (s *Service) contentAudience(ctx context.Context, level string, units []string, at time.Time) (domain.Audience, error) {
	units = domain.DedupUnits(units)
	if level == "national" || len(units) == 0 {
		return domain.Audience{National: true}, nil
	}
	exp, err := s.expandUnits(ctx, units, at)
	return domain.Audience{Units: exp}, err
}

// expandUnits thêm mọi đơn vị con (xã của tỉnh) theo masterdata tại thời điểm sự
// việc. Mã không có trong danh mục được giữ nguyên.
func (s *Service) expandUnits(ctx context.Context, units []string, at time.Time) ([]string, error) {
	if s.Units == nil {
		return units, nil
	}
	out := append([]string(nil), units...)
	for _, u := range units {
		desc, err := s.Units.Descendants(ctx, u, at)
		if errors.Is(err, mdport.ErrUnitNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, desc...)
	}
	return domain.DedupUnits(out), nil
}

// sendSpec là tập thiết bị nhận một thông điệp.
type sendSpec struct {
	tokens         []domain.PushToken
	notificationID *uuid.UUID
	urgent         bool
}

// deliver ghi thông báo (bỏ qua bản trùng), phát pf.notify.notification.created và
// xếp lần gửi đẩy tới thiết bị của người nhận.
func (s *Service) deliver(ctx context.Context, tx pgx.Tx, env outbox.Envelope, ns []domain.Notification) error {
	now := s.now()
	var specs []sendSpec
	kind := ""
	for _, n := range ns {
		n.ID, n.SourceEventID, n.SourceEventType = ids.New(), env.EventID, env.EventType
		if !n.Normalize() {
			return fmt.Errorf("notify: thông báo %s dựng từ %s không hợp lệ", n.Kind, env.EventType)
		}
		ok, err := store.InsertNotification(ctx, tx, env.TenantID, n)
		if err != nil {
			return err
		}
		if !ok {
			continue // đã có từ lần giao trước
		}
		kind = n.Kind
		if err := outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: env.TenantID, Type: port.EventNotificationCreated, SchemaVersion: 1,
			OccurredAt: now, TraceID: env.TraceID, Producer: port.EventProducerName,
			Payload: port.NotificationCreatedV1{NotificationID: n.ID, Kind: n.Kind, Severity: n.Severity, RecipientRef: n.RecipientRef,
				Topic: n.Topic(), SubjectRef: n.SubjectRef, SourceEventID: env.EventID, CreatedAt: n.CreatedAt.Format(time.RFC3339)},
		}); err != nil {
			return err
		}
		var toks []domain.PushToken
		if n.Topic() {
			toks, err = store.ActiveTokensForAudience(ctx, tx, n.Audience)
		} else {
			toks, err = store.ActiveTokensForActors(ctx, tx, []uuid.UUID{*n.RecipientRef})
		}
		if err != nil {
			return err
		}
		id := n.ID
		specs = append(specs, sendSpec{tokens: toks, notificationID: &id, urgent: n.Urgent()})
	}
	return s.queueDeliveries(ctx, tx, env.TenantID, ids.New(), kind, now, specs...)
}

// queueDeliveries ghi lần gửi của lô và phát pf.notify.push.requested (nếu có).
func (s *Service) queueDeliveries(ctx context.Context, tx pgx.Tx, tid, batch uuid.UUID, kind string, now time.Time, specs ...sendSpec) error {
	count := 0
	for _, sp := range specs {
		for _, t := range sp.tokens {
			if err := store.InsertDelivery(ctx, tx, tid, domain.Delivery{ID: ids.New(), BatchID: batch, DeviceID: t.DeviceID,
				NotificationID: sp.notificationID, Kind: kind, Urgent: sp.urgent, CreatedAt: now}); err != nil {
				return err
			}
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: tid, Type: port.EventPushRequested, SchemaVersion: 1, OccurredAt: now,
		Producer: port.EventProducerName, Payload: port.PushRequestedV1{BatchID: batch, Kind: kind, Deliveries: count},
	})
}
