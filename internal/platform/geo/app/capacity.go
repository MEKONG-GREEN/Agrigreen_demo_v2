package app

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Sức chứa kho/Hub (GIS US-WH-01, BRD FN-06, BR-52, BR-57/58) và widget dung
// lượng Hub (GIS US-HUB-01, FN-15, BR-50).
//
// Quy tắc BR-52: số Admin nhập (manual, import) KHÔNG bị lần đồng bộ sau (valve)
// ghi đè. Bảng chiếu giữ ảnh chụp mới nhất theo TỪNG nguồn; số hiệu lực là số
// Admin mới nhất chưa được nhả, nếu không có thì số của van. Khi van có số mới hơn
// và khác số Admin, widget/Admin thấy sync_differs = true (cảnh báo) cho tới khi
// Admin "nhả" (release, kèm lý do) để áp dụng số đồng bộ.

// CapacityView là sức chứa hiệu lực của một cơ sở/hub.
type CapacityView struct {
	Capacity   *decimal.Decimal
	Used       *decimal.Decimal
	Unit       string
	Source     string // manual | import | valve | "" (chưa có số)
	AsOf       *time.Time
	SnapshotID uuid.UUID
	FillPct    *decimal.Decimal
	Class      string // available | near_full | full | unknown
	// SyncDiffers: van có số mới hơn, khác số Admin đang áp dụng (BR-52 — chờ Admin xác nhận).
	SyncDiffers bool
	Pending     *store.CapacityLatest // số của van đang chờ xác nhận
	Latest      []store.CapacityLatest
}

// effectiveCapacity chọn số hiệu lực theo BR-52 và xếp lớp màu theo ngưỡng.
func effectiveCapacity(latest []store.CapacityLatest, warn, full decimal.Decimal) CapacityView {
	v := CapacityView{Class: domain.CapacityUnknown, Latest: latest}
	var admin, valve *store.CapacityLatest
	for i := range latest {
		l := &latest[i]
		switch {
		case domain.AdminEntered(l.Source) && !l.Released:
			if admin == nil || l.AsOf.After(admin.AsOf) {
				admin = l
			}
		case l.Source == domain.CapacityValve:
			valve = l
		}
	}
	eff := admin
	if eff == nil {
		eff = valve
	}
	if eff == nil {
		return v
	}
	c, u, at := eff.Capacity, eff.Used, eff.AsOf
	pct := domain.FillPct(u, c)
	v.Capacity, v.Used, v.Unit, v.Source, v.AsOf, v.SnapshotID, v.FillPct = &c, &u, eff.Unit, eff.Source, &at, eff.SnapshotID, &pct
	v.Class = domain.CapacityClass(pct, warn, full)
	if admin != nil && valve != nil && valve.AsOf.After(admin.AsOf) &&
		(!valve.Capacity.Equal(admin.Capacity) || !valve.Used.Equal(admin.Used)) {
		v.SyncDiffers, v.Pending = true, valve
	}
	return v
}

// CapacityInput là một ảnh chụp sức chứa Admin nhập (form) cho cơ sở hoặc hub MKG.
type CapacityInput struct {
	SubjectKind string // facility | hub
	SubjectRef  uuid.UUID
	Capacity    *decimal.Decimal
	CapacityBad bool
	Used        *decimal.Decimal
	Unit        string
	AsOf        time.Time
	// Sửa một ảnh chụp đã nhập: trỏ dòng sổ cũ kèm lý do (bản ghi bù).
	SupersedesID uuid.UUID
	Reason       string
	IdemKey      string
	source       string
}

// CapacityResult là kết quả ghi.
type CapacityResult struct {
	Snapshot store.CapacitySnapshot
	Capacity CapacityView
	Replayed bool
}

// subjectUnit kiểm đối tượng có sức chứa tồn tại và trả đơn vị mặc định.
func subjectUnit(ctx context.Context, tx pgx.Tx, kind string, ref uuid.UUID) (string, error) {
	switch kind {
	case domain.SubjectFacility:
		f, err := store.FacilityByRef(ctx, tx, ref, false)
		if err != nil {
			return "", err
		}
		if f == nil {
			return "", ErrFacilityNotFound
		}
		if f.Status == domain.StatusRetired {
			return "", ErrFeatureState.WithDetail("kho/Hub đã ngừng dùng")
		}
		return f.Unit, nil
	case domain.SubjectHub:
		h, err := store.HubByRef(ctx, tx, ref)
		if err != nil {
			return "", err
		}
		if h == nil {
			return "", ErrHubNotFound
		}
		return h.Unit, nil
	}
	return "", problem.Validation.WithField("subject_kind", "phải là facility hoặc hub")
}

func (s *Service) capacityReplay(ctx context.Context, key string, warn, full decimal.Decimal) func(pgx.Tx) (*CapacityResult, error) {
	return func(tx pgx.Tx) (*CapacityResult, error) {
		snap, err := store.CapacityByIdemKey(ctx, tx, key)
		if err != nil || snap == nil {
			return nil, err
		}
		v, err := viewOf(ctx, tx, snap.SubjectKind, snap.SubjectRef, warn, full)
		if err != nil {
			return nil, err
		}
		return &CapacityResult{Snapshot: *snap, Capacity: v, Replayed: true}, nil
	}
}

func viewOf(ctx context.Context, tx pgx.Tx, kind string, ref uuid.UUID, warn, full decimal.Decimal) (CapacityView, error) {
	l, err := store.LatestFor(ctx, tx, kind, []uuid.UUID{ref})
	if err != nil {
		return CapacityView{}, err
	}
	return effectiveCapacity(l[store.SubjectKey{Kind: kind, Ref: ref}], warn, full), nil
}

// RecordCapacity ghi ảnh chụp sức chứa Admin nhập (GIS US-WH-01; BR-52: chỉ Admin
// nhập tay). Số này có hiệu lực ngay và không bị lần đồng bộ sau ghi đè.
func (s *Service) RecordCapacity(ctx context.Context, in CapacityInput) (CapacityResult, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return CapacityResult{}, err
	}
	in.source = domain.CapacityManual
	return s.recordCapacity(ctx, a, in)
}

func (s *Service) recordCapacity(ctx context.Context, a access, in CapacityInput) (CapacityResult, error) {
	if in.IdemKey == "" {
		return CapacityResult{}, problem.IdempotencyRequired
	}
	if in.CapacityBad || !positive(in.Capacity) {
		return CapacityResult{}, problem.Validation.WithDetail(domain.MsgCapacityPositive).WithField("capacity", domain.MsgCapacityPositive)
	}
	if in.Used == nil || in.Used.IsNegative() {
		return CapacityResult{}, problem.Validation.WithField("used", "bắt buộc, không âm (dung lượng đã sử dụng)")
	}
	if in.Unit != "" && !domain.KnownUnit(in.Unit) {
		return CapacityResult{}, problem.Validation.WithField("unit", "phải là tan, m3 hoặc ke")
	}
	var why *string
	var sup *uuid.UUID
	if in.SupersedesID != uuid.Nil {
		r, err := reason("correction_reason", in.Reason)
		if err != nil {
			return CapacityResult{}, err
		}
		why, sup = &r, &in.SupersedesID
	}
	asOf, err := s.eventTime(in.AsOf)
	if err != nil {
		return CapacityResult{}, problem.Validation.WithField("as_of", "không được ở tương lai")
	}
	warn, full, err := s.hubThresholds(ctx, s.now())
	if err != nil {
		return CapacityResult{}, err
	}
	res, replayed, err := writeOnce(ctx, s, a.p.TenantID, s.capacityReplay(ctx, in.IdemKey, warn, full), func(tx pgx.Tx) (CapacityResult, error) {
		unit, err := subjectUnit(ctx, tx, in.SubjectKind, in.SubjectRef)
		if err != nil {
			return CapacityResult{}, err
		}
		if in.Unit != "" {
			unit = in.Unit
		}
		if sup != nil {
			old, err := store.CapacityByID(ctx, tx, *sup)
			if err != nil {
				return CapacityResult{}, err
			}
			if old == nil || old.SubjectKind != in.SubjectKind || old.SubjectRef != in.SubjectRef || old.Action != domain.CapacitySnapshot {
				return CapacityResult{}, problem.Validation.WithField("supersedes_id", "không phải ảnh chụp sức chứa của đối tượng này")
			}
		}
		snap := store.CapacitySnapshot{
			ID: ids.New(), TenantID: a.p.TenantID, SubjectKind: in.SubjectKind, SubjectRef: in.SubjectRef,
			Action: domain.CapacitySnapshot, Source: in.source, AsOf: asOf, Capacity: in.Capacity, Used: in.Used, Unit: unit,
			EventTime: asOf, SupersedesID: sup, CorrectionReason: why, ActorRef: a.p.ActorRef, IdemKey: in.IdemKey,
		}
		if err := store.InsertCapacity(ctx, tx, &snap); err != nil {
			return CapacityResult{}, err
		}
		if err := store.ApplyLatest(ctx, tx, snap); err != nil {
			return CapacityResult{}, err
		}
		v, err := viewOf(ctx, tx, in.SubjectKind, in.SubjectRef, warn, full)
		return CapacityResult{Snapshot: snap, Capacity: v}, err
	})
	res.Replayed = replayed
	return res, err
}

// ReleaseInput là lệnh Admin xác nhận áp dụng số đồng bộ thay số nhập tay (BR-52).
type ReleaseInput struct {
	SubjectKind string
	SubjectRef  uuid.UUID
	Reason      string
	IdemKey     string
}

// ReleaseCapacity nhả số Admin nhập của đối tượng: số của van (nếu có) có hiệu
// lực từ đây. Ghi một dòng sổ release kèm lý do; số cũ vẫn nằm trong sổ.
func (s *Service) ReleaseCapacity(ctx context.Context, in ReleaseInput) (CapacityResult, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return CapacityResult{}, err
	}
	if in.IdemKey == "" {
		return CapacityResult{}, problem.IdempotencyRequired
	}
	why, err := reason("reason", in.Reason)
	if err != nil {
		return CapacityResult{}, err
	}
	warn, full, err := s.hubThresholds(ctx, s.now())
	if err != nil {
		return CapacityResult{}, err
	}
	now := s.now()
	res, replayed, err := writeOnce(ctx, s, a.p.TenantID, s.capacityReplay(ctx, in.IdemKey, warn, full), func(tx pgx.Tx) (CapacityResult, error) {
		if _, err := subjectUnit(ctx, tx, in.SubjectKind, in.SubjectRef); err != nil && !errors.Is(err, ErrFeatureState) {
			return CapacityResult{}, err
		}
		// Dòng release trỏ ảnh chụp Admin đang áp dụng (mới nhất) mà nó nhả.
		cur, err := viewOf(ctx, tx, in.SubjectKind, in.SubjectRef, warn, full)
		if err != nil {
			return CapacityResult{}, err
		}
		if !domain.AdminEntered(cur.Source) {
			return CapacityResult{}, ErrNothingToRelease
		}
		if _, err := store.ReleaseAdmin(ctx, tx, in.SubjectKind, in.SubjectRef, now); err != nil {
			return CapacityResult{}, err
		}
		sup := cur.SnapshotID
		snap := store.CapacitySnapshot{
			ID: ids.New(), TenantID: a.p.TenantID, SubjectKind: in.SubjectKind, SubjectRef: in.SubjectRef,
			Action: domain.CapacityRelease, Source: cur.Source, AsOf: now, EventTime: now, SupersedesID: &sup,
			CorrectionReason: &why, ActorRef: a.p.ActorRef, IdemKey: in.IdemKey,
		}
		if err := store.InsertCapacity(ctx, tx, &snap); err != nil {
			return CapacityResult{}, err
		}
		v, err := viewOf(ctx, tx, in.SubjectKind, in.SubjectRef, warn, full)
		return CapacityResult{Snapshot: snap, Capacity: v}, err
	})
	res.Replayed = replayed
	return res, err
}

// CapacityImportItem là một dòng tệp sức chứa (FN-06 "nhập file hàng loạt theo mẫu").
type CapacityImportItem struct {
	FacilityCode string
	HubRef       uuid.UUID
	Capacity     *decimal.Decimal
	CapacityBad  bool
	Used         *decimal.Decimal
	Unit         string
	AsOf         time.Time
}

// CapacityImportRow là kết quả một dòng.
type CapacityImportRow struct {
	Index      int
	Status     string
	Code       string
	Reason     string
	SnapshotID uuid.UUID
}

// MaxCapacityImport giới hạn số dòng mỗi lần nhập (đồng bộ dưới 2 giây, R-52).
const MaxCapacityImport = 500

// ImportCapacities nhập ảnh chụp sức chứa hàng loạt (nguồn import — Admin nhập, BR-52).
func (s *Service) ImportCapacities(ctx context.Context, items []CapacityImportItem, idemKey string) ([]CapacityImportRow, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return nil, err
	}
	if idemKey == "" {
		return nil, problem.IdempotencyRequired
	}
	if len(items) == 0 || len(items) > MaxCapacityImport {
		return nil, problem.Validation.WithField("items", "từ 1 đến "+strconv.Itoa(MaxCapacityImport)+" dòng")
	}
	out := make([]CapacityImportRow, 0, len(items))
	for i, it := range items {
		row := CapacityImportRow{Index: i + 1}
		in := CapacityInput{
			SubjectKind: domain.SubjectHub, SubjectRef: it.HubRef, Capacity: it.Capacity, CapacityBad: it.CapacityBad, Used: it.Used,
			Unit: it.Unit, AsOf: it.AsOf, IdemKey: idemKey + ":" + strconv.Itoa(i+1), source: domain.CapacityImport,
		}
		var err error
		if code := strings.TrimSpace(it.FacilityCode); code != "" {
			in.SubjectKind = domain.SubjectFacility
			err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
				f, e := store.FacilityByCode(ctx, tx, code)
				if e == nil && f == nil {
					return ErrFacilityNotFound.WithDetail("Không có kho/Hub mã " + code)
				}
				if f != nil {
					in.SubjectRef = f.Ref
				}
				return e
			})
		} else if it.HubRef == uuid.Nil {
			err = problem.Validation.WithField("facility_code", "bắt buộc: facility_code hoặc hub_ref")
		}
		var res CapacityResult
		if err == nil {
			res, err = s.recordCapacity(ctx, a, in)
		}
		if err != nil {
			var pr *problem.Problem
			if !errors.As(err, &pr) || pr.Status >= 500 {
				return nil, err
			}
			row.Status = FeatureRejected
			row.Code, row.Reason = rejectReason(err)
		} else {
			row.Status, row.SnapshotID = FeatureAccepted, res.Snapshot.ID
			if res.Replayed {
				row.Status = FeatureDuplicate
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// SubjectCapacity đọc sức chứa hiệu lực và lịch sử của cơ sở/hub.
func (s *Service) SubjectCapacity(ctx context.Context, kind string, ref uuid.UUID) (CapacityView, []store.CapacitySnapshot, error) {
	a, err := s.access(ctx, layerReadRoles...)
	if err != nil {
		return CapacityView{}, nil, err
	}
	warn, full, err := s.hubThresholds(ctx, s.now())
	if err != nil {
		return CapacityView{}, nil, err
	}
	var v CapacityView
	var hist []store.CapacitySnapshot
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if _, err := subjectUnit(ctx, tx, kind, ref); err != nil && !errors.Is(err, ErrFeatureState) {
			return err
		}
		var err error
		if v, err = viewOf(ctx, tx, kind, ref, warn, full); err != nil {
			return err
		}
		hist, err = store.CapacityHistory(ctx, tx, kind, ref, 100)
		return err
	})
	return v, hist, err
}

// ---------------------------------------------------------------------------
// Widget dung lượng Hub (GIS US-HUB-01)
// ---------------------------------------------------------------------------

// CapacityWidgets trả marker kèm % và lớp màu cho mọi cơ sở/hub trong khung nhìn.
func (s *Service) CapacityWidgets(ctx context.Context, in FacilityListInput) (FacilityPage, error) {
	return s.FacilitiesInBBox(ctx, in)
}

// ---------------------------------------------------------------------------
// port.HubSink — dữ liệu hub MKG qua van công bố (consumer nối ở đợt 2)
// ---------------------------------------------------------------------------

var _ port.HubSink = (*Service)(nil)

// UpsertHubFacility thực hiện port.HubSink: ghi hub MKG nếu phiên bản mới hơn.
func (s *Service) UpsertHubFacility(ctx context.Context, h port.HubFacility) (bool, error) {
	if h.HubRef == uuid.Nil || h.Version < 1 {
		return false, problem.Validation.WithField("hub_ref", "bắt buộc hub_ref và version ≥ 1")
	}
	h.Code, h.Name = strings.TrimSpace(h.Code), strings.TrimSpace(h.Name)
	if h.Code == "" || h.Name == "" || len([]rune(h.Code)) > 40 || len([]rune(h.Name)) > 200 {
		return false, problem.Validation.WithField("code", "mã (≤ 40) và tên (≤ 200) bắt buộc")
	}
	if h.Kind == "" {
		h.Kind = domain.FacilityHub
	}
	if !domain.KnownFacilityKind(h.Kind) {
		return false, problem.Validation.WithField("kind", "phải là kho, hub hoặc nha_may")
	}
	if h.Status == "" {
		h.Status = domain.StatusActive
	}
	if h.Status != domain.StatusActive && h.Status != domain.StatusRetired {
		return false, problem.Validation.WithField("status", "phải là active hoặc retired")
	}
	if h.CapacityTons != nil && !h.CapacityTons.IsPositive() {
		return false, problem.Validation.WithField("capacity_tons", domain.MsgCapacityPositive)
	}
	pos, err := domain.NewPosition(h.Lon, h.Lat)
	if err != nil {
		return false, violationProblem(err)
	}
	polyGJ := ""
	if len(h.Polygon) > 0 && string(h.Polygon) != "null" {
		mp, _, err := domain.ParseGeometry(h.Polygon)
		if err != nil {
			return false, violationProblem(err)
		}
		polyGJ = mp.GeoJSON()
	}
	if h.PublishedAt.IsZero() {
		h.PublishedAt = s.now()
	}
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return false, err
	}
	var applied bool
	err = s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		applied, e = store.UpsertHub(ctx, tx, tid, store.Hub{
			HubRef: h.HubRef, Code: h.Code, Name: h.Name, Kind: h.Kind, ProvinceCode: h.ProvinceCode, CommuneCode: h.CommuneCode,
			Capacity: h.CapacityTons, Unit: domain.UnitTon, Status: h.Status, Version: h.Version, PublishedAt: h.PublishedAt,
		}, domain.PointGeoJSON(pos), polyGJ)
		return e
	})
	return applied, err
}

// AppendHubCapacity thực hiện port.HubSink: thêm ảnh chụp sức chứa nguồn valve,
// idempotent theo (hub_ref, as_of). Không ghi đè số Admin nhập (BR-52).
func (s *Service) AppendHubCapacity(ctx context.Context, c port.HubCapacity) (bool, error) {
	if c.HubRef == uuid.Nil || c.AsOf.IsZero() {
		return false, problem.Validation.WithField("hub_ref", "bắt buộc hub_ref và as_of")
	}
	if !c.CapacityTons.IsPositive() {
		return false, problem.Validation.WithField("capacity_tons", domain.MsgCapacityPositive)
	}
	if c.UsedTons.IsNegative() {
		return false, problem.Validation.WithField("used_tons", "không âm")
	}
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return false, err
	}
	key := "valve:hub_capacity:" + c.HubRef.String() + ":" + c.AsOf.UTC().Format(time.RFC3339Nano)
	applied := false
	err = s.inTx(ctx, tid, func(tx pgx.Tx) error {
		if prev, err := store.CapacityByIdemKey(ctx, tx, key); err != nil || prev != nil {
			return err
		}
		capacity, used := c.CapacityTons, c.UsedTons
		snap := store.CapacitySnapshot{
			ID: ids.New(), TenantID: tid, SubjectKind: domain.SubjectHub, SubjectRef: c.HubRef, Action: domain.CapacitySnapshot,
			Source: domain.CapacityValve, AsOf: c.AsOf, Capacity: &capacity, Used: &used, Unit: domain.UnitTon, EventTime: c.AsOf,
			ActorRef: port.ValveActorRef, IdemKey: key,
		}
		if err := store.InsertCapacity(ctx, tx, &snap); err != nil {
			return err
		}
		applied = true
		return store.ApplyLatest(ctx, tx, snap)
	})
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
		return false, nil
	}
	return applied, err
}

// ---------------------------------------------------------------------------
// tiện ích đọc số từ JSON (sức chứa, tải trọng — thông báo AC-2 khi sai định dạng)
// ---------------------------------------------------------------------------

// ParseDecimalJSON đọc số decimal từ JSON (số hoặc chuỗi). Rỗng/null → nil, bad = false;
// sai định dạng → bad = true.
func ParseDecimalJSON(raw json.RawMessage) (d *decimal.Decimal, bad bool) {
	t := strings.TrimSpace(string(raw))
	if t == "" || t == "null" {
		return nil, false
	}
	t = strings.Trim(t, `"`)
	v, err := decimal.NewFromString(strings.TrimSpace(t))
	if err != nil {
		return nil, true
	}
	return &v, false
}
