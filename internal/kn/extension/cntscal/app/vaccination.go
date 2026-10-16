package app

// Lịch tiêm phòng (BM-07, UR-TINH-11, FN-15 BR-02, AC-03): lịch của MỘT lứa sinh từ quy trình tiêm của loại mô hình/
// giống (giá trị có hiệu lực tại ngày nhận giống — đổi quy trình chỉ áp dụng lứa mới). Chủ nhiệm Cấp Tỉnh lập, sửa
// ngày dự kiến từng hộ, hủy; Cấp Xã ghi ngày tiêm thực tế trong phạm vi. Mỗi thay đổi là một phiên bản.

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/store"
	exdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// BatchView là lứa (bản hiện hành) cho API.
type BatchView struct {
	store.Batch
	Status       string
	Next         *domain.Dose
	OverdueCount int
	Versions     []store.Batch // chỉ chi tiết
	CanPlan      bool          // sửa ngày dự kiến, hủy
	CanRecord    bool          // ghi ngày thực tế
	Names        map[uuid.UUID]string
	Labels       map[string]string // "<danh mục>:<mã>" → tên
	Replayed     bool
}

func (c caller) canPlanVaccination() bool {
	return c.admin() || (c.k.Role == authz.KNTinh && !c.k.ReadOnly() &&
		(c.k.ProvinceGroup == "" || c.k.ProvinceGroup == accport.GroupChuNhiem))
}

// seesBatch: Admin, Cấp Tỉnh (phạm vi tỉnh), Cấp Xã (xã phụ trách + giới hạn hộ W11.07).
func (c caller) seesBatch(b store.Batch) bool {
	if !c.sees(b.CommuneCode) {
		return false
	}
	if c.k.Role == authz.KNXa && b.HouseholdRef != nil && !c.k.AllowsHousehold(*b.HouseholdRef) {
		return false
	}
	return true
}

func (c caller) canRecord(b store.Batch) bool {
	if c.k.ReadOnly() || b.Event == domain.BatchCancelled {
		return false
	}
	return c.canPlanVaccination() || (c.k.Role == authz.KNXa && c.seesBatch(b))
}

func (s *Service) view(c caller, b store.Batch) BatchView {
	today := s.today()
	v := BatchView{Batch: b, Status: domain.StatusOf(b.Event, b.Doses, today), Next: domain.NextDose(b.Doses),
		CanPlan: c.canPlanVaccination() && b.Event != domain.BatchCancelled, CanRecord: c.canRecord(b)}
	for _, d := range b.Doses {
		if d.Overdue(today) {
			v.OverdueCount++
		}
	}
	return v
}

func (s *Service) labels(ctx context.Context) map[string]string {
	return s.catalogNames(ctx, mdport.CatalogCNTSVaccine, mdport.CatalogCNTSModelType, mdport.CatalogCNTSBreed,
		mdport.CatalogCNTSVaccinationSchedule)
}

// BatchQuery lọc danh sách lứa.
type BatchQuery struct {
	Commune   string
	Household *uuid.UUID
	Status    string // open | overdue | done | cancelled | "" (mọi)
	From, To  *time.Time
	Limit     int
}

// ListBatches liệt kê lứa trong phạm vi (bản hiện hành, trạng thái suy diễn).
func (s *Service) ListBatches(ctx context.Context, q BatchQuery) ([]BatchView, map[string]string, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, nil, err
	}
	switch q.Status {
	case "", domain.StatusOpen, domain.StatusOverdue, domain.StatusDone, domain.StatusCancelled:
	default:
		return nil, nil, problem.Validation.WithField("status", "open, overdue, done hoặc cancelled")
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 200
	}
	f := store.BatchFilter{All: c.admin() || c.k.Scope.All, Communes: c.k.Scope.Units, Commune: strings.TrimSpace(q.Commune),
		Household: q.Household, From: q.From, To: q.To, Limit: q.Limit}
	if c.k.Role == authz.KNTinh && !c.k.Scope.All {
		// Cấp Tỉnh: Scope là Nút tỉnh — mọi xã của tỉnh; lọc theo phạm vi ở tầng ứng dụng.
		f.All = true
	}
	var list []store.Batch
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		list, e = store.ListBatches(ctx, tx, f)
		return e
	}); err != nil {
		return nil, nil, err
	}
	out := []BatchView{}
	for _, b := range list {
		if !c.seesBatch(b) {
			continue
		}
		v := s.view(c, b)
		if q.Status != "" && v.Status != q.Status {
			continue
		}
		out = append(out, v)
	}
	return out, s.labels(ctx), nil
}

// GetBatch đọc chi tiết lứa kèm các phiên bản.
func (s *Service) GetBatch(ctx context.Context, ref uuid.UUID) (BatchView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return BatchView{}, err
	}
	var vs []store.Batch
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		vs, e = store.BatchVersions(ctx, tx, ref)
		return e
	}); err != nil {
		return BatchView{}, err
	}
	if len(vs) == 0 || !c.seesBatch(vs[len(vs)-1]) {
		return BatchView{}, ErrBatchNotFound
	}
	v := s.view(c, vs[len(vs)-1])
	v.Versions = vs
	refs := []uuid.UUID{}
	for _, b := range vs {
		refs = append(refs, b.ActorRef)
	}
	v.Names, v.Labels = s.names(ctx, uniq(refs)), s.labels(ctx)
	return v, nil
}

// CreateBatchInput là thân "Tạo lịch theo quy trình của mô hình" (FN-15 B4).
type CreateBatchInput struct {
	Ref          *uuid.UUID // UUIDv7 do máy khách sinh (tùy chọn)
	CommuneCode  string
	ModelType    string
	Breed        string
	Schedule     string // rỗng → chọn quy trình theo loại mô hình/giống
	HouseholdRef *uuid.UUID
	Locality     string
	Received     time.Time
	AgeAtReceipt int
	HeadCount    *int
	Note         string
	IdemKey      string
}

func idemBatch(k string) string { return "vbatch:" + k }

func (s *Service) itemAt(ctx context.Context, field, catalog, code string, on time.Time) (mdport.CatalogItem, error) {
	it, err := s.Catalogs.ItemAt(ctx, catalog, code, on)
	switch {
	case errors.Is(err, mdport.ErrCatalogItemNotFound), errors.Is(err, mdport.ErrNotEffective):
		return it, ErrBatchInvalid.WithField(field, "không có hoặc không dùng tại ngày "+on.Format("02/01/2006")+" trong danh mục CNTS")
	case err != nil:
		return it, err
	case !it.Active:
		return it, ErrBatchInvalid.WithField(field, "mục đã ngừng dùng")
	}
	return it, nil
}

// pickSchedule chọn quy trình tiêm có hiệu lực tại ngày nhận: ưu tiên quy trình gắn đúng giống, rồi gắn loại mô hình
// (không gắn giống).
func (s *Service) pickSchedule(ctx context.Context, in CreateBatchInput) (mdport.VaccinationSchedule, error) {
	code := strings.TrimSpace(in.Schedule)
	if code == "" {
		items, err := s.Catalogs.ListAt(ctx, mdport.CatalogCNTSVaccinationSchedule, in.Received)
		if err != nil {
			return mdport.VaccinationSchedule{}, err
		}
		best := -1
		for _, it := range items {
			mt, br := it.Attrs["model_type"], it.Attrs["breed"]
			score := -1
			switch {
			case in.Breed != "" && br == in.Breed && (mt == "" || mt == in.ModelType):
				score = 2
			case br == "" && mt == in.ModelType:
				score = 1
			}
			if score > best {
				best, code = score, it.Code
			}
		}
		if code == "" {
			return mdport.VaccinationSchedule{}, ErrNoSchedule
		}
	}
	sc, err := s.Catalogs.ScheduleAt(ctx, code, in.Received)
	if errors.Is(err, mdport.ErrCatalogItemNotFound) || errors.Is(err, mdport.ErrNotEffective) {
		return sc, ErrBatchInvalid.WithField("schedule_code", "quy trình tiêm không có hoặc không dùng tại ngày nhận giống")
	}
	if err != nil {
		return sc, err
	}
	if sc.ModelType != "" && sc.ModelType != in.ModelType {
		return sc, ErrBatchInvalid.WithField("schedule_code", "quy trình tiêm không thuộc loại mô hình đã chọn")
	}
	if len(sc.Doses) == 0 {
		return sc, ErrNoSchedule
	}
	return sc, nil
}

func checkText(field, v string, maxLen int) error {
	if utf8.RuneCountInString(v) > maxLen {
		return ErrBatchInvalid.WithField(field, "quá dài")
	}
	if exdomain.ContainsPII(v) {
		return ErrBatchInvalid.WithField(field, exdomain.ErrPIIInText.Error())
	}
	return nil
}

// CreateBatch lập lịch tiêm cho một lứa (chủ nhiệm Cấp Tỉnh, Admin).
func (s *Service) CreateBatch(ctx context.Context, in CreateBatchInput) (BatchView, error) {
	c, err := s.planner(ctx, "chỉ chủ nhiệm chương trình Cấp Tỉnh (hoặc Admin) lập lịch tiêm phòng", accport.GroupChuNhiem)
	if err != nil {
		return BatchView{}, err
	}
	if s.Units == nil {
		return BatchView{}, ErrDependency
	}
	if strings.TrimSpace(in.IdemKey) == "" {
		return BatchView{}, problem.Validation.WithField("Idempotency-Key", "bắt buộc")
	}
	in.CommuneCode, in.ModelType, in.Breed = strings.TrimSpace(in.CommuneCode), strings.TrimSpace(in.ModelType), strings.TrimSpace(in.Breed)
	in.Locality, in.Note = strings.TrimSpace(in.Locality), strings.TrimSpace(in.Note)
	today := s.today()
	switch {
	case in.HouseholdRef == nil && in.Locality == "":
		return BatchView{}, ErrBatchInvalid.WithField("household_ref", "chọn hộ mô hình hoặc ghi địa bàn")
	case in.AgeAtReceipt < 0 || in.AgeAtReceipt > domain.MaxAgeDays:
		return BatchView{}, ErrBatchInvalid.WithField("age_at_receipt", "0–3650 ngày")
	case in.HeadCount != nil && (*in.HeadCount < 1 || *in.HeadCount > domain.MaxHeadCount):
		return BatchView{}, ErrBatchInvalid.WithField("head_count", "từ 1")
	case in.Received.After(today.AddDate(0, 3, 0)) || in.Received.Before(today.AddDate(-3, 0, 0)):
		return BatchView{}, ErrBatchInvalid.WithField("received_date", "trong 3 năm trước đến 3 tháng tới")
	}
	if err := checkText("locality", in.Locality, domain.MaxLocality); err != nil {
		return BatchView{}, err
	}
	if err := checkText("note", in.Note, domain.MaxNoteLen); err != nil {
		return BatchView{}, err
	}
	if !c.sees(in.CommuneCode) {
		return BatchView{}, ErrOutOfScope.WithField("commune_code", "xã ngoài phạm vi")
	}
	u, err := s.Units.Get(ctx, in.CommuneCode, s.now())
	if errors.Is(err, partyport.ErrUnitNotFound) || (err == nil && u.Level != partyport.LevelCommune) {
		return BatchView{}, ErrBatchInvalid.WithField("commune_code", "không phải xã/phường có hiệu lực")
	}
	if err != nil {
		return BatchView{}, err
	}
	if _, err := s.itemAt(ctx, "model_type_code", mdport.CatalogCNTSModelType, in.ModelType, in.Received); err != nil {
		return BatchView{}, err
	}
	if in.Breed != "" {
		if _, err := s.itemAt(ctx, "breed_code", mdport.CatalogCNTSBreed, in.Breed, in.Received); err != nil {
			return BatchView{}, err
		}
	}
	sc, err := s.pickSchedule(ctx, in)
	if err != nil {
		return BatchView{}, err
	}
	plan := make([]domain.PlanDose, 0, len(sc.Doses))
	for _, d := range sc.Doses {
		plan = append(plan, domain.PlanDose{Vaccine: d.Vaccine, Shot: d.Shot, AgeDays: d.AgeDays})
	}
	doses := domain.GenerateDoses(plan, in.Received, in.AgeAtReceipt)
	if len(doses) == 0 {
		return BatchView{}, ErrBatchInvalid.WithField("age_at_receipt", "tuổi lúc nhận đã qua mọi mũi của quy trình")
	}
	ref := ids.New()
	if in.Ref != nil {
		ref = *in.Ref
	}
	now := s.now()
	b := store.Batch{Meta: c.meta(now, idemBatch(in.IdemKey)), Ref: ref, Event: domain.BatchCreated, CommuneCode: in.CommuneCode,
		ModelType: in.ModelType, Breed: in.Breed, Schedule: sc.Code, ScheduleValidFrom: sc.ValidFrom, HouseholdRef: in.HouseholdRef,
		Locality: in.Locality, Received: in.Received, AgeAtReceipt: in.AgeAtReceipt, HeadCount: in.HeadCount, Doses: doses, Note: in.Note}
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.BatchByIdem(ctx, tx, b.IdemKey); err != nil || prev != nil {
			if prev != nil {
				b, replayed = *prev, true
			}
			return err
		}
		if cur, err := store.LatestBatch(ctx, tx, ref); err != nil {
			return err
		} else if cur != nil {
			return ErrBatchInvalid.WithField("id", "mã lứa đã dùng")
		}
		return store.InsertBatch(ctx, tx, c.p.TenantID, &b)
	})
	if dup(err) {
		return s.CreateBatch(ctx, in)
	}
	if err != nil {
		return BatchView{}, err
	}
	v := s.view(c, b)
	v.Replayed, v.Versions, v.Labels, v.Names = replayed, []store.Batch{b}, s.labels(ctx), s.names(ctx, []uuid.UUID{b.ActorRef})
	return v, nil
}

// DoseChangeInput là thân "Cập nhật mũi tiêm" (ngày dự kiến, ngày thực tế, ghi chú).
type DoseChangeInput struct {
	Ref         uuid.UUID
	BaseVersion *int64
	Updates     []domain.DoseUpdate
	CapturedAt  *time.Time
	IdemKey     string
}

// UpdateDoses ghi thay đổi mũi tiêm: Cấp Xã (phạm vi) ghi ngày tiêm thực tế, ghi chú; chủ nhiệm Cấp Tỉnh/Admin còn
// sửa ngày dự kiến từng hộ. Bản ghi mới mang sự việc dose_recorded (chỉ ngày thực tế/ghi chú) hoặc updated.
func (s *Service) UpdateDoses(ctx context.Context, in DoseChangeInput) (BatchView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return BatchView{}, err
	}
	if err := c.checkWrite(); err != nil {
		return BatchView{}, err
	}
	if strings.TrimSpace(in.IdemKey) == "" {
		return BatchView{}, problem.Validation.WithField("Idempotency-Key", "bắt buộc")
	}
	if in.BaseVersion == nil {
		return BatchView{}, errBaseVersionNeed
	}
	if len(in.Updates) == 0 || len(in.Updates) > domain.MaxDoses {
		return BatchView{}, ErrBatchInvalid.WithField("doses", "1–60 mũi")
	}
	return s.mutate(ctx, c, in.Ref, *in.BaseVersion, in.IdemKey, func(cur store.Batch) (store.Batch, error) {
		if !c.canRecord(cur) {
			if cur.Event == domain.BatchCancelled {
				return cur, ErrBatchCancelled
			}
			return cur, problem.Forbidden.WithDetail("chỉ Cấp Xã phụ trách hoặc chủ nhiệm Cấp Tỉnh ghi mũi tiêm")
		}
		plan := c.canPlanVaccination()
		doses, changed, es := domain.ApplyDoseUpdates(cur.Doses, in.Updates, plan, cur.Received, s.today())
		if len(es) > 0 {
			return cur, fieldsProblem(ErrBatchInvalid, es)
		}
		if !changed {
			return cur, ErrBatchInvalid.WithField("doses", "không có thay đổi")
		}
		ev := domain.BatchDoseRecorded
		for _, u := range in.Updates {
			if u.Expected != nil {
				ev = domain.BatchUpdated
			}
		}
		cur.Event, cur.Doses, cur.StatusReason, cur.CapturedAt = ev, doses, "", in.CapturedAt
		return cur, nil
	})
}

// CancelBatch hủy lịch tiêm của lứa (lập nhầm, lứa không nuôi) — chủ nhiệm Cấp Tỉnh, Admin; bắt buộc lý do.
func (s *Service) CancelBatch(ctx context.Context, ref uuid.UUID, base *int64, reason, key string) (BatchView, error) {
	c, err := s.planner(ctx, "chỉ chủ nhiệm chương trình Cấp Tỉnh (hoặc Admin) hủy lịch tiêm", accport.GroupChuNhiem)
	if err != nil {
		return BatchView{}, err
	}
	if strings.TrimSpace(key) == "" {
		return BatchView{}, problem.Validation.WithField("Idempotency-Key", "bắt buộc")
	}
	if base == nil {
		return BatchView{}, errBaseVersionNeed
	}
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 5 || n > 1000 {
		return BatchView{}, ErrBatchInvalid.WithField("reason", "bắt buộc, 5–1000 ký tự")
	}
	if err := checkText("reason", reason, 1000); err != nil {
		return BatchView{}, err
	}
	return s.mutate(ctx, c, ref, *base, key, func(cur store.Batch) (store.Batch, error) {
		if cur.Event == domain.BatchCancelled {
			return cur, ErrBatchCancelled
		}
		cur.Event, cur.StatusReason, cur.CapturedAt = domain.BatchCancelled, reason, nil
		return cur, nil
	})
}

// mutate ghi một phiên bản mới từ bản hiện hành (khóa lạc quan base = seq hiện hành; gọi lặp cùng khóa trả bản đã ghi).
func (s *Service) mutate(ctx context.Context, c caller, ref uuid.UUID, base int64, key string,
	fn func(store.Batch) (store.Batch, error)) (BatchView, error) {
	now := s.now()
	var out store.Batch
	replayed := false
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.BatchByIdem(ctx, tx, idemBatch(key)); err != nil || prev != nil {
			if prev != nil {
				out, replayed = *prev, true
			}
			return err
		}
		if err := store.Lock(ctx, tx, "vbatch:"+ref.String()); err != nil {
			return err
		}
		cur, err := store.LatestBatch(ctx, tx, ref)
		if err != nil {
			return err
		}
		if cur == nil || !c.seesBatch(*cur) {
			return ErrBatchNotFound
		}
		if cur.Seq != base {
			return ErrBatchConflict.With("current_version", cur.Seq).With("updated_at", cur.EventTime.UTC().Format(time.RFC3339))
		}
		next, err := fn(*cur)
		if err != nil {
			return err
		}
		next.Meta = c.meta(now, idemBatch(key))
		if err := store.InsertBatch(ctx, tx, c.p.TenantID, &next); err != nil {
			return err
		}
		out = next
		return nil
	})
	if dup(err) {
		return s.mutate(ctx, c, ref, base, key, fn)
	}
	if err != nil {
		return BatchView{}, err
	}
	if replayed && !c.seesBatch(out) {
		return BatchView{}, ErrBatchNotFound
	}
	v, err := s.GetBatch(ctx, ref)
	v.Replayed = replayed
	return v, err
}
