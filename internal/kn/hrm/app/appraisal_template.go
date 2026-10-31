package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type appraisalActor struct {
	ref, tenant, unitRef uuid.UUID
	group, unit          string
	manager, readOnly    bool
	scope                domain.Scope
}

func (c appraisalActor) center() bool {
	return c.group == domain.GroupAdmin || c.group == domain.GroupHCTH
}
func (c appraisalActor) needCenter(write bool) error {
	if !c.center() || c.scope != domain.ScopeAll || (write && c.readOnly) {
		return problem.Forbidden.WithDetail("đánh giá: chỉ Phòng HCTH/Admin, ngữ cảnh chỉ xem không ghi")
	}
	return nil
}

// appraisalCaller chỉ dùng phiên server-bound. Không suy chủ thể bằng tên/email hoặc nhận role/unit từ request.
func (s *Service) appraisalCaller(ctx context.Context) (appraisalActor, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return appraisalActor{}, problem.Unauthenticated
	}
	if p.ActorRef == uuid.Nil || p.TenantID == uuid.Nil {
		return appraisalActor{}, problem.Unauthenticated
	}
	k, err := s.recordSession(ctx)
	if err != nil {
		return appraisalActor{}, err
	}
	c := appraisalActor{ref: p.ActorRef, tenant: p.TenantID, unitRef: k.UnitRef, unit: k.InternalUnit,
		group: s.group(k), manager: k.Manager, readOnly: k.ReadOnly || k.ViewOnly}
	if c.group == domain.GroupTW || k.Role == "kn_tw" || k.ViewOnly {
		return c, problem.Forbidden.WithDetail("Cấp TW không tham gia hoặc truy cập phiếu đánh giá")
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		m, e := s.matrixOf(ctx, tx, c.group, c.ref)
		c.scope = m[domain.PermAppraisal]
		return e
	})
	if err != nil {
		return c, err
	}
	if c.scope == "" || c.scope == domain.ScopeNone {
		return c, problem.Forbidden.WithDetail("không có quyền đánh giá nhân sự (FN-20)")
	}
	return c, nil
}

func (c appraisalActor) canRead(m domain.AppraisalMember) bool {
	self := m.AccountRef != nil && *m.AccountRef == c.ref
	unit := c.unit != "" && (m.Unit == c.unit || m.ManagementUnit == c.unit)
	return c.scope.Allows(self, unit) && domain.CanReadAppraisal(appraisalAuthority(c), m)
}

func appraisalValidation(err error) error {
	var field domain.AppraisalInputError
	if errors.As(err, &field) {
		return problem.Validation.WithField(field.Field, field.Message)
	}
	return err
}

func appraisalHash(actor uuid.UUID, operation string, body any) (string, error) {
	raw, err := json.Marshal(struct {
		Actor     uuid.UUID `json:"actor"`
		Operation string    `json:"operation"`
		Body      any       `json:"body"`
	}{actor, operation, body})
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]), nil
}

func appraisalJSON(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(raw) > 2<<20 {
		return nil, problem.PayloadTooLarge.WithDetail("bản chụp đánh giá tối đa 2 MiB")
	}
	return raw, nil
}

func appraisalKey(idem string) error {
	if len(idem) < 8 || len(idem) > 200 {
		return problem.Validation.WithField("Idempotency-Key", "thao tác ghi cần khóa từ 8 đến 200 ký tự")
	}
	for _, ch := range idem {
		if ch < 0x21 || ch > 0x7e {
			return problem.Validation.WithField("Idempotency-Key", "khóa chỉ gồm ký tự ASCII in được, không khoảng trắng")
		}
	}
	return nil
}

// Lock request trước entity, cùng một thứ tự cho mọi thao tác: không có hai route chạy cùng khóa vào hai stream.
func appraisalReplay(ctx context.Context, tx pgx.Tx, c appraisalActor, idem, hash string) (store.AppraisalEvent, bool, error) {
	if err := store.LockAppraisal(ctx, tx, c.tenant, "request:"+idem); err != nil {
		return store.AppraisalEvent{}, false, err
	}
	old, ok, err := store.AppraisalReplay(ctx, tx, idem)
	if err != nil || !ok {
		return old, ok, err
	}
	if old.Actor != c.ref || old.RequestHash != hash {
		return store.AppraisalEvent{}, false, port.ErrAppraisalConflict.WithDetail("Idempotency-Key đã dùng cho thao tác hoặc dữ liệu khác")
	}
	return old, true, nil
}

type AppraisalTemplateChange struct {
	Status  string                           `json:"status"`
	Version int64                            `json:"version"`
	Summary *domain.AppraisalSummaryTemplate `json:"summary,omitempty"`
	Form    *domain.AppraisalForm            `json:"form,omitempty"`
}

type AppraisalTemplate struct {
	ID         uuid.UUID                        `json:"id"`
	Kind       string                           `json:"kind"`
	Code       string                           `json:"code"`
	Status     string                           `json:"status"`
	Version    int64                            `json:"version"`
	RecordedAt time.Time                        `json:"recorded_at"`
	Actor      uuid.UUID                        `json:"actor_ref"`
	Summary    *domain.AppraisalSummaryTemplate `json:"summary,omitempty"`
	Form       *domain.AppraisalForm            `json:"form,omitempty"`
}

func appraisalTemplate(row store.AppraisalEvent) (AppraisalTemplate, error) {
	v := AppraisalTemplate{ID: row.ID, Kind: row.Kind, Code: row.Key, Status: row.Action, Version: row.Seq,
		RecordedAt: row.RecordedAt, Actor: row.Actor}
	switch row.Kind {
	case "summary":
		v.Summary = &domain.AppraisalSummaryTemplate{}
		if err := json.Unmarshal(row.Payload, v.Summary); err != nil {
			return v, err
		}
	case "form":
		v.Form = &domain.AppraisalForm{}
		if err := json.Unmarshal(row.Payload, v.Form); err != nil {
			return v, err
		}
	default:
		return v, port.ErrAppraisalNotFound
	}
	if v.Status == "publish" {
		v.Status = "published"
	}
	if v.Status == "retire" {
		v.Status = "retired"
	}
	return v, nil
}

func (s *Service) AppraisalTemplates(ctx context.Context, kind, code string, history bool) ([]AppraisalTemplate, error) {
	c, err := s.appraisalCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.needCenter(false); err != nil {
		return nil, err
	}
	if kind != "summary" && kind != "form" {
		return nil, port.ErrAppraisalNotFound
	}
	out := []AppraisalTemplate{}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var rows []store.AppraisalEvent
		var e error
		if history {
			rows, e = store.AppraisalHistory(ctx, tx, kind, code)
		} else {
			rows, e = store.AppraisalCurrent(ctx, tx, kind, nil)
		}
		if e != nil {
			return e
		}
		for _, row := range rows {
			if code != "" && row.Key != code {
				continue
			}
			v, e := appraisalTemplate(row)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return nil
	})
	return out, classify(err)
}

func (s *Service) SaveAppraisalTemplate(ctx context.Context, kind, code, idem string, in AppraisalTemplateChange) (AppraisalTemplate, error) {
	c, err := s.appraisalCaller(ctx)
	if err != nil {
		return AppraisalTemplate{}, err
	}
	if err := c.needCenter(true); err != nil {
		return AppraisalTemplate{}, err
	}
	if err := appraisalKey(idem); err != nil {
		return AppraisalTemplate{}, err
	}
	var payload any
	switch kind {
	case "summary":
		if in.Summary == nil || in.Form != nil {
			return AppraisalTemplate{}, problem.Validation.WithField("summary", "cần cấu hình mẫu MS05/MS06")
		}
		v, e := domain.ValidateAppraisalSummary(*in.Summary)
		if e != nil {
			return AppraisalTemplate{}, appraisalValidation(e)
		}
		if v.Code != code {
			return AppraisalTemplate{}, problem.Validation.WithField("code", "mã ở route và thân phải trùng")
		}
		in.Summary = &v
		payload = v
	case "form":
		if in.Form == nil || in.Summary != nil {
			return AppraisalTemplate{}, problem.Validation.WithField("form", "cần cấu hình phiếu đánh giá")
		}
		v, e := domain.ValidateAppraisalForm(*in.Form, in.Status == "published")
		if e != nil {
			return AppraisalTemplate{}, appraisalValidation(e)
		}
		if v.Code != code {
			return AppraisalTemplate{}, problem.Validation.WithField("code", "mã ở route và thân phải trùng")
		}
		in.Form = &v
		payload = v
	default:
		return AppraisalTemplate{}, port.ErrAppraisalNotFound
	}
	action := map[string]string{"draft": "draft", "published": "publish", "retired": "retire"}[in.Status]
	if action == "" || in.Version < 0 {
		return AppraisalTemplate{}, problem.Validation.WithField("status", "chọn nháp, đang dùng hoặc ngừng dùng; phiên bản không âm")
	}
	hash, err := appraisalHash(c.ref, "template:"+kind+":"+code, in)
	if err != nil {
		return AppraisalTemplate{}, err
	}
	raw, err := appraisalJSON(payload)
	if err != nil {
		return AppraisalTemplate{}, err
	}
	var saved store.AppraisalEvent
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		old, replayed, e := appraisalReplay(ctx, tx, c, idem, hash)
		if e != nil {
			return e
		}
		if replayed {
			saved = old
			return nil
		}
		if e := store.LockAppraisal(ctx, tx, c.tenant, "template:"+kind+":"+code); e != nil {
			return e
		}
		previous, ok, e := store.AppraisalLatest(ctx, tx, kind, code)
		if e != nil {
			return e
		}
		if (!ok && in.Version != 0) || (ok && previous.Seq != in.Version) || (!ok && action == "retire") {
			return port.ErrAppraisalConflict
		}
		if ok && kind == "summary" {
			var old domain.AppraisalSummaryTemplate
			if e = json.Unmarshal(previous.Payload, &old); e != nil {
				return e
			}
			if old.Kind != in.Summary.Kind {
				return problem.Validation.WithField("kind", "nhân bản mẫu mới thay vì đổi họ MS05/MS06 của mã đã có")
			}
		}
		saved, e = store.InsertAppraisal(ctx, tx, c.tenant, store.AppraisalEvent{ID: ids.New(), Kind: kind, Key: code, Action: action,
			Payload: raw, RequestHash: hash, IdemKey: idem, Actor: c.ref}, s.Clock.Now())
		return e
	})
	if err != nil {
		return AppraisalTemplate{}, classify(err)
	}
	return appraisalTemplate(saved)
}

// appraisalsReadObject chỉ giải mã dữ liệu của sổ đã kiểm; lỗi hỏng sổ không biến thành trạng thái hợp lệ rỗng.
func appraisalsReadObject[T any](row store.AppraisalEvent) (T, error) {
	var out T
	if len(row.Payload) == 0 || strings.TrimSpace(string(row.Payload)) == "null" {
		return out, fmt.Errorf("appraisal: payload rỗng tại %s", row.ID)
	}
	if err := json.Unmarshal(row.Payload, &out); err != nil {
		return out, fmt.Errorf("appraisal: payload tại %s: %w", row.ID, err)
	}
	return out, nil
}
