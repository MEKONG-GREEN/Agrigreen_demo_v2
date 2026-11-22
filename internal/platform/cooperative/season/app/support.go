package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Yêu cầu hỗ trợ của nông dân App HTX (HTX US-SUPPORT-01) — PA1-HTX-SUPPORT-GPSLOG (#313): chuyển từ pf/extension về
// module chủ HTX. Nông dân gửi (REST hoặc pf/sync entity support_request) và xem yêu cầu của hộ mình; quản lý HTX xem
// yêu cầu của HTX mình; quản trị xem toàn tenant. Log GPS qua consumer port.GpsLogConsumer (HandleGpsLog).

// Mã lỗi yêu cầu hỗ trợ (docs/errors.md).
var (
	ErrNoHousehold     = problem.New(http.StatusForbidden, "cropcycle.farmer_household_required", "Tài khoản chưa liên kết nông hộ nào")
	ErrHouseholdChoice = problem.New(http.StatusUnprocessableEntity, "cropcycle.household_invalid", "Nông hộ không thuộc tài khoản của bạn")
	ErrNoCooperative   = problem.New(http.StatusUnprocessableEntity, "cropcycle.household_no_cooperative", "Nông hộ chưa là xã viên HTX đang hoạt động")
)

// HouseholdMemberships tra HTX và xã hiện tại của một nông hộ (xã viên — phần member của pf/cooperative).
type HouseholdMemberships interface {
	Membership(ctx context.Context, ref uuid.UUID) (port.HouseholdMembership, error)
}

// SupportPayload là payload của bản ghi đồng bộ support_request (chiều lên, op create) và thân POST /v1/support-requests
// (docs/dev/sync-contract.md). Toạ độ và độ chính xác nhận chuỗi decimal hoặc số JSON.
type SupportPayload struct {
	HouseholdRef *string         `json:"household_ref"`
	PlotRef      *string         `json:"plot_ref"`
	Category     string          `json:"category"`
	Urgency      string          `json:"urgency"`
	Description  string          `json:"description"`
	PhotoRefs    []string        `json:"photo_refs"`
	Location     *PointPayload   `json:"location"`
	GPSAccuracyM json.RawMessage `json:"gps_accuracy_m"`
}

// PointPayload là toạ độ WGS84 (chuỗi decimal hoặc số JSON — kiểm cú pháp trước khi parse, xem plainDecimal).
type PointPayload struct {
	Lat json.RawMessage `json:"lat"`
	Lon json.RawMessage `json:"lon"`
}

// plainDecRe: số thập phân thường (không mũ, ≤ 10 chữ số phần nguyên, ≤ 10 chữ số phần lẻ) — chặn 1e100000, 1e-100000,
// chuỗi rất dài trước khi parse (CONTEXT §4).
var plainDecRe = regexp.MustCompile(`^-?[0-9]{1,10}(\.[0-9]{1,10})?$`)

// plainDecimal đọc một số JSON hoặc chuỗi decimal; rỗng/null → nil.
func plainDecimal(field string, raw json.RawMessage) (*decimal.Decimal, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil, nil
	}
	if len(s) > 2 && s[0] == '"' {
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return nil, problem.Validation.WithField(field, "phải là số thập phân")
		}
		s = strings.TrimSpace(str)
	}
	if len(s) > 32 || !plainDecRe.MatchString(s) {
		return nil, problem.Validation.WithField(field, "phải là số thập phân thường (≤ 10 chữ số trước và sau dấu chấm)")
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return nil, problem.Validation.WithField(field, "phải là số thập phân")
	}
	return &d, nil
}

// SupportInput là một yêu cầu hỗ trợ nông dân gửi.
type SupportInput struct {
	ID           *uuid.UUID // UUIDv7 sinh trên máy (ngoại tuyến, AC-2); nil → server sinh
	HouseholdRef uuid.UUID  // Nil → nông hộ duy nhất của tài khoản
	PlotRef      uuid.UUID
	Category     string
	Urgency      string
	Description  string
	PhotoRefs    []uuid.UUID
	Location     *domain.Point
	GPSAccuracyM *decimal.Decimal
	EventTime    *time.Time // lúc bấm Gửi trên máy; nil = lúc nhận
	DeviceID     *string
	IdemKey      string
}

// ToInput chuyển payload thành SupportInput (chưa có ID, khóa idem, thời điểm).
func (b SupportPayload) ToInput() (SupportInput, error) {
	in := SupportInput{Category: b.Category, Urgency: b.Urgency, Description: b.Description}
	var err error
	if in.GPSAccuracyM, err = plainDecimal("gps_accuracy_m", b.GPSAccuracyM); err != nil {
		return in, err
	}
	if b.HouseholdRef != nil {
		if in.HouseholdRef, err = optRef("household_ref", *b.HouseholdRef); err != nil {
			return in, err
		}
	}
	if b.PlotRef != nil {
		if in.PlotRef, err = optRef("plot_ref", *b.PlotRef); err != nil {
			return in, err
		}
	}
	if len(b.PhotoRefs) > domain.MaxSupportPhotos {
		return in, problem.Validation.WithField("photo_refs", domain.MsgSupportPhotos)
	}
	for _, s := range b.PhotoRefs {
		u, err := uuid.Parse(s)
		if err != nil {
			return in, problem.Validation.WithField("photo_refs", "mỗi phần tử phải là mã tệp (UUID)")
		}
		in.PhotoRefs = append(in.PhotoRefs, u)
	}
	if b.Location != nil {
		lat, err := plainDecimal("location.lat", b.Location.Lat)
		if err != nil {
			return in, err
		}
		lon, err := plainDecimal("location.lon", b.Location.Lon)
		if err != nil {
			return in, err
		}
		if lat == nil || lon == nil {
			return in, problem.Validation.WithField("location", "cần cả lat và lon")
		}
		in.Location = &domain.Point{Lat: *lat, Lon: *lon}
	}
	return in, nil
}

// SupportResult là kết quả gửi yêu cầu.
type SupportResult struct {
	Request  domain.SupportRequest
	Replayed bool
}

// farmerHouseholds trả nông hộ liên kết với nông dân gọi.
func (s *Service) farmerHouseholds(ctx context.Context) (authz.Principal, []uuid.UUID, error) {
	p, err := authz.Require(ctx, port.RoleFarmer)
	if err != nil {
		return p, nil, err
	}
	if s.Farmers == nil {
		return p, nil, ErrDependency.WithDetail("chưa nối liên kết tài khoản ↔ nông hộ (pf/accounts)")
	}
	hh, err := s.Farmers.HouseholdsOf(ctx, p.ActorRef)
	if err != nil {
		return p, nil, ErrDependency.WithDetail("không tra được nông hộ của tài khoản").WithCause(err)
	}
	if len(hh) == 0 {
		return p, nil, ErrNoHousehold
	}
	return p, hh, nil
}

// supportPlace tra HTX, xã (xã viên hiện tại) và tỉnh của nông hộ.
func (s *Service) supportPlace(ctx context.Context, household uuid.UUID) (coop uuid.UUID, commune, province string, err error) {
	if s.Members == nil || s.Units == nil {
		return uuid.Nil, "", "", ErrDependency.WithDetail("chưa nối xã viên HTX hoặc danh mục đơn vị hành chính")
	}
	m, err := s.Members.Membership(ctx, household)
	if errors.Is(err, port.ErrHouseholdNotFound) || (err == nil && (!m.Active || m.CooperativeRef == uuid.Nil)) {
		return uuid.Nil, "", "", ErrNoCooperative.WithField("household_ref", "nông hộ chưa thuộc HTX đang hoạt động")
	}
	if err != nil {
		return uuid.Nil, "", "", err
	}
	u, err := s.Units.Get(ctx, m.CommuneCode, s.now())
	if errors.Is(err, partyport.ErrUnitNotFound) || (err == nil && u.Level != partyport.LevelCommune) {
		return uuid.Nil, "", "", problem.Validation.WithField("commune_code", "nông hộ phải gắn với một xã còn hiệu lực")
	}
	if err != nil {
		return uuid.Nil, "", "", err
	}
	return m.CooperativeRef, m.CommuneCode, u.ParentCode, nil
}

// SubmitSupport nhận yêu cầu hỗ trợ của nông dân (HTX US-SUPPORT-01): ghi yêu cầu và phát
// pf.cooperative.support_request.submitted trong cùng giao dịch; gửi lặp cùng khóa idem trả bản đã ghi.
func (s *Service) SubmitSupport(ctx context.Context, in SupportInput) (SupportResult, error) {
	received := s.now()
	p, households, err := s.farmerHouseholds(ctx)
	if err != nil {
		return SupportResult{}, err
	}
	if in.IdemKey == "" {
		return SupportResult{}, problem.IdempotencyRequired
	}
	household := households[0]
	if in.HouseholdRef != uuid.Nil {
		if !slices.Contains(households, in.HouseholdRef) {
			return SupportResult{}, ErrHouseholdChoice.WithField("household_ref", "không thuộc tài khoản")
		}
		household = in.HouseholdRef
	} else if len(households) > 1 {
		return SupportResult{}, ErrHouseholdChoice.WithField("household_ref", "tài khoản có nhiều nông hộ — chọn một")
	}
	r := domain.SupportRequest{
		ID: ids.New(), HouseholdRef: household, PlotRef: in.PlotRef, Category: in.Category, Urgency: in.Urgency,
		Description: in.Description, PhotoRefs: in.PhotoRefs, Location: in.Location, GPSAccuracyM: in.GPSAccuracyM,
		EventTime: received, ReceivedAt: received, SubmittedBy: p.ActorRef, DeviceID: in.DeviceID, IdemKey: in.IdemKey,
	}
	if in.ID != nil {
		r.ID = *in.ID
	}
	if in.EventTime != nil && !in.EventTime.IsZero() && !in.EventTime.After(received) {
		r.EventTime = in.EventTime.UTC()
	}
	if err := r.Validate(); err != nil {
		return SupportResult{}, fieldProblem(err)
	}
	if r.CooperativeRef, r.CommuneCode, r.ProvinceCode, err = s.supportPlace(ctx, household); err != nil {
		return SupportResult{}, err
	}
	var res SupportResult
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.SupportByIdemKey(ctx, tx, in.IdemKey); err != nil || prev != nil {
			if prev != nil {
				res = SupportResult{Request: *prev, Replayed: true}
			}
			return err
		}
		if prev, err := store.SupportByID(ctx, tx, r.ID); err != nil {
			return err
		} else if prev != nil {
			return ErrIDReused
		}
		if err := store.InsertSupportRequest(ctx, tx, p.TenantID, r); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, p.TenantID, port.EventSupportRequestSubmitted, r.ReceivedAt, port.SupportRequestSubmittedV1{
			SupportRequestID: r.ID, HouseholdRef: r.HouseholdRef, CooperativeRef: r.CooperativeRef, CommuneCode: r.CommuneCode,
			ProvinceCode: r.ProvinceCode, Category: r.Category, Urgency: r.Urgency, PhotoCount: len(r.PhotoRefs),
			GPSMissing: r.GPSMissing(), EventTime: r.EventTime.UTC().Format(time.RFC3339),
			ReceivedAt: r.ReceivedAt.UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
		cur, err := store.SupportByID(ctx, tx, r.ID)
		if err != nil || cur == nil {
			return errors.Join(errors.New("cooperative: không đọc lại được yêu cầu hỗ trợ vừa ghi"), err)
		}
		res = SupportResult{Request: *cur}
		return nil
	})
	if isUnique(err, "support_request_tenant_id_idem_key_key") {
		// Hai lần gửi cùng khóa chạy song song: bên thua đọc lại bản đã ghi.
		var prev *domain.SupportRequest
		if rerr := s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = store.SupportByIdemKey(ctx, tx, in.IdemKey)
			return e
		}); rerr == nil && prev != nil {
			return SupportResult{Request: *prev, Replayed: true}, nil
		}
	}
	if isUnique(err, "support_request_pkey") {
		return SupportResult{}, ErrIDReused
	}
	if err != nil {
		return SupportResult{}, err
	}
	return res, nil
}

// SupportPage là một trang yêu cầu hỗ trợ.
type SupportPage struct {
	Items []domain.SupportRequest
	Next  *uuid.UUID
}

func page(items []domain.SupportRequest, limit int) SupportPage {
	pg := SupportPage{Items: items}
	if len(items) > limit {
		pg.Items = items[:limit]
		last := pg.Items[limit-1].ID
		pg.Next = &last
	}
	return pg
}

func supportLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}

// ListMySupport liệt kê yêu cầu hỗ trợ của (các) nông hộ của nông dân, mới nhất trước.
func (s *Service) ListMySupport(ctx context.Context, before uuid.UUID, limit int) (SupportPage, error) {
	p, households, err := s.farmerHouseholds(ctx)
	if err != nil {
		return SupportPage{}, err
	}
	limit = supportLimit(limit)
	var items []domain.SupportRequest
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.SupportForHouseholds(ctx, tx, households, before, limit+1)
		return e
	})
	if err != nil {
		return SupportPage{}, err
	}
	return page(items, limit), nil
}

// SupportQuery là tham số danh sách yêu cầu hỗ trợ cho quản lý HTX / quản trị.
type SupportQuery struct {
	Cooperative uuid.UUID
	Category    string
	Urgency     string
	CommuneCode string
	GPSMissing  *bool
	Before      uuid.UUID
	Limit       int
}

// ListSupportForManagers liệt kê yêu cầu hỗ trợ của nông dân cho quản lý HTX (chỉ HTX của mình — Principal.OrgRef) và
// quản trị (toàn tenant, lọc HTX được) — HTX US-SUPPORT-01 "quản lý HTX biết để hỗ trợ kịp thời". Mới nhất trước.
func (s *Service) ListSupportForManagers(ctx context.Context, q SupportQuery) (SupportPage, error) {
	a, err := s.access(ctx, managerRoles...)
	if err != nil {
		return SupportPage{}, err
	}
	switch {
	case q.Category != "" && !domain.ValidSupportCategory(q.Category):
		return SupportPage{}, problem.Validation.WithField("category", "không hợp lệ")
	case q.Urgency != "" && !domain.ValidSupportUrgency(q.Urgency):
		return SupportPage{}, problem.Validation.WithField("urgency", "phải là binh_thuong hoặc khan")
	}
	limit := supportLimit(q.Limit)
	f := store.SupportFilter{Scope: a.filter(), Cooperative: q.Cooperative, Category: q.Category, Urgency: q.Urgency,
		CommuneCode: q.CommuneCode, GPSMissing: q.GPSMissing, Before: q.Before, Limit: limit + 1}
	if a.kind == accCoop {
		if q.Cooperative != uuid.Nil && q.Cooperative != a.p.OrgRef {
			return SupportPage{}, ErrOutOfScope.WithDetail("quản lý HTX chỉ xem yêu cầu hỗ trợ của HTX mình")
		}
		if a.p.OrgRef == uuid.Nil {
			return SupportPage{Items: []domain.SupportRequest{}}, nil
		}
	}
	var items []domain.SupportRequest
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.SupportForManagers(ctx, tx, f)
		return e
	})
	if err != nil {
		return SupportPage{}, err
	}
	return page(items, limit), nil
}

// supportData là dữ liệu một yêu cầu gửi xuống máy nông dân và trả qua REST (dùng chung).
type supportData struct {
	ID           string   `json:"id"`
	HouseholdRef string   `json:"household_ref"`
	PlotRef      *string  `json:"plot_ref"`
	Category     string   `json:"category"`
	Urgency      string   `json:"urgency"`
	Description  string   `json:"description"`
	PhotoRefs    []string `json:"photo_refs"`
	GPSMissing   bool     `json:"gps_missing"`
	Labels       []string `json:"labels"`
	EventTime    string   `json:"event_time"`
	ReceivedAt   string   `json:"received_at"`
	TaskStatus   *string  `json:"task_status"` // chỉ yêu cầu chép từ pf/extension: trạng thái nhiệm vụ KN lúc chép
}

// SupportData dựng dữ liệu hiển thị của một yêu cầu cho nông dân.
func SupportData(r domain.SupportRequest) json.RawMessage {
	photos := uuidStrings(r.PhotoRefs)
	labels := []string{}
	if r.GPSMissing() {
		labels = append(labels, domain.LabelGPSMissing)
	}
	b, _ := json.Marshal(supportData{
		ID: r.ID.String(), HouseholdRef: r.HouseholdRef.String(), PlotRef: refStr(r.PlotRef), Category: r.Category,
		Urgency: r.Urgency, Description: r.Description, PhotoRefs: photos, GPSMissing: r.GPSMissing(), Labels: labels,
		EventTime: r.EventTime.UTC().Format(time.RFC3339), ReceivedAt: r.ReceivedAt.UTC().Format(time.RFC3339),
		TaskStatus: strPtr(r.LegacyTaskStatus),
	})
	return b
}

func uuidStrings(u []uuid.UUID) []string {
	out := make([]string, 0, len(u))
	for _, x := range u {
		out = append(out, x.String())
	}
	return out
}

// SupportSyncSource là chiều xuống support_request cho máy nông dân (chỉ yêu cầu của nông hộ mình).
type SupportSyncSource struct{ Svc *Service }

// SupportSyncSink nhận support_request gửi ngoại tuyến (HTX US-SUPPORT-01 AC-2).
type SupportSyncSink struct{ Svc *Service }

var (
	_ syncport.FarmerSource = SupportSyncSource{}
	_ syncport.Sink         = SupportSyncSink{}
)

// Entities thực hiện syncport.Source.
func (SupportSyncSource) Entities() []string { return []string{port.EntitySupportRequest} }

// Changes thực hiện syncport.Source: chỉ nông dân nhận yêu cầu hỗ trợ (qua FarmerChanges) — vai khác nhận rỗng.
func (SupportSyncSource) Changes(_ context.Context, _ authz.Scope, _ string, after int64, _ int) ([]syncport.Change, int64, error) {
	return []syncport.Change{}, after, nil
}

// FarmerEntities thực hiện syncport.FarmerSource.
func (SupportSyncSource) FarmerEntities() []string { return []string{port.EntitySupportRequest} }

// FarmerChanges thực hiện syncport.FarmerSource: yêu cầu của các nông hộ liên kết, cursor = sync_seq của yêu cầu.
func (src SupportSyncSource) FarmerChanges(ctx context.Context, fs syncport.FarmerScope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, after, problem.Unauthenticated
	}
	if entity != port.EntitySupportRequest || !p.Has(port.RoleFarmer) || len(fs.Households) == 0 {
		return []syncport.Change{}, after, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var rows []domain.SupportRequest
	err := src.Svc.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.SupportChangesForHouseholds(ctx, tx, fs.Households, after, limit)
		return e
	})
	if err != nil {
		return nil, after, err
	}
	out := make([]syncport.Change, 0, len(rows))
	last := after
	for _, r := range rows {
		out = append(out, syncport.Change{Entity: port.EntitySupportRequest, ID: r.ID, Version: r.SyncSeq, Op: syncport.OpUpsert, Data: SupportData(r)})
		last = r.SyncSeq
	}
	return out, last, nil
}

// Entities thực hiện syncport.Sink.
func (SupportSyncSink) Entities() []string { return []string{port.EntitySupportRequest} }

// Accept thực hiện syncport.Sink: chỉ op create; idempotent theo IdemKey.
func (snk SupportSyncSink) Accept(ctx context.Context, _ authz.Scope, rec syncport.Record) syncport.Result {
	if rec.Op != syncport.OpCreate {
		return rejected(rec.ID, problem.Validation.WithField("op", "support_request chỉ nhận op = create"))
	}
	var body SupportPayload
	if err := json.Unmarshal(rec.Payload, &body); err != nil {
		return rejected(rec.ID, problem.Validation.WithField("payload", "JSON không hợp lệ"))
	}
	in, err := body.ToInput()
	if err != nil {
		return rejected(rec.ID, err)
	}
	id := rec.ID
	in.ID, in.IdemKey = &id, rec.IdemKey
	if rec.DeviceID != uuid.Nil {
		d := rec.DeviceID.String()
		in.DeviceID = &d
	}
	if !rec.EventTime.IsZero() {
		t := rec.EventTime
		in.EventTime = &t
	}
	out, err := snk.Svc.SubmitSupport(ctx, in)
	if err != nil {
		return rejected(rec.ID, err)
	}
	if out.Replayed {
		return syncport.Result{ID: rec.ID, Status: syncport.StatusDuplicate}
	}
	return syncport.Result{ID: rec.ID, Status: syncport.StatusAccepted}
}
