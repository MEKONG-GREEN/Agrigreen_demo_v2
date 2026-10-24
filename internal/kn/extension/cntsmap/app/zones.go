package app

// Vẽ, sửa, ngưng, khôi phục vùng (FN-18 B3–B4); khoanh vùng từ ổ dịch BM-10 theo bán kính (UR-TINH-17 "vẽ vùng theo bán
// kính"); đọc vùng theo ngày (BR-01, AC-03).

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/store"
	exdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ZoneView là một phiên bản vùng cho API.
type ZoneView struct {
	store.Zone
	Status   string
	CanEdit  bool
	Warnings []string
	Replayed bool
}

// ZoneDetail là vùng kèm lịch sử phiên bản (mới nhất trước).
type ZoneDetail struct {
	ZoneView
	Versions []store.Zone
	Names    map[uuid.UUID]string
}

// seesZone: Admin; người gọi thấy một xã của vùng (Cấp Tỉnh: phạm vi gồm các xã của tỉnh).
func (c caller) seesZone(z store.Zone) bool { return c.admin() || c.seesAny(z.Communes) }

// canEditZone: người vẽ được, vùng trong phạm vi (mọi xã của vùng thuộc phạm vi người gọi).
func (c caller) canEditZone(z store.Zone) bool {
	if !c.drawer() {
		return false
	}
	if c.admin() {
		return true
	}
	for _, x := range z.Communes {
		if !c.sees(x) {
			return false
		}
	}
	return true
}

func (c caller) view(z store.Zone) ZoneView {
	return ZoneView{Zone: z, Status: domain.Status(z.Event), CanEdit: c.canEditZone(z)}
}

// ZoneInput là thân tạo/sửa vùng.
type ZoneInput struct {
	Ref         *uuid.UUID // tạo: UUIDv7 tùy chọn; sửa: mã vùng
	BaseVersion *int64     // sửa
	Reason      string     // sửa: bắt buộc
	Province    string     // tạo (Admin bắt buộc; Cấp Tỉnh suy từ phạm vi)
	OutbreakRef *uuid.UUID
	ParentRef   *uuid.UUID
	domain.Zone
	IdemKey string
}

func idemZone(k string) string { return "czone:" + k }

// checkZone kiểm vùng (trường, danh mục, xã, ổ dịch, vùng cha, ngày, hình học).
func (s *Service) checkZone(ctx context.Context, tx pgx.Tx, c caller, province string, in *ZoneInput, cur *store.Zone) error {
	if es := in.Normalize(); len(es) > 0 {
		return fieldsProblem(ErrZoneInvalid, es)
	}
	for f, v := range map[string]string{"name": in.Name, "note": in.Note} {
		if exdomain.ContainsPII(v) {
			return ErrZoneInvalid.WithField(f, exdomain.ErrPIIInText.Error())
		}
	}
	if err := s.communesIn(ctx, c, in.Communes, province); err != nil {
		return err
	}
	prevSpecies, prevDisease := "", ""
	if cur != nil {
		prevSpecies, prevDisease = cur.Species, cur.Disease
	}
	if err := s.checkCatalog(ctx, knport.CatalogCNTSSpecies, "species_code", in.Species, prevSpecies); err != nil {
		return err
	}
	if err := s.checkCatalog(ctx, knport.CatalogCNTSDisease, "disease_code", in.Disease, prevDisease); err != nil {
		return err
	}
	if in.OutbreakRef != nil {
		if !domain.EpidemicKind(in.Kind) {
			return ErrZoneInvalid.WithField("outbreak_ref", "chỉ vùng dịch, vùng bị uy hiếp, vùng đệm, hướng di chuyển gắn ổ dịch")
		}
		o, err := store.LatestOutbreak(ctx, tx, *in.OutbreakRef)
		if err != nil {
			return err
		}
		if o == nil || !c.sees(o.Commune) {
			return ErrZoneInvalid.WithField("outbreak_ref", "không có ổ dịch này trong phạm vi")
		}
		if p, err := s.provinceOf(ctx, o.Commune); err != nil {
			return err
		} else if p != province {
			return ErrZoneInvalid.WithField("outbreak_ref", "ổ dịch thuộc tỉnh khác")
		}
	}
	if in.ParentRef != nil {
		if cur != nil && *in.ParentRef == cur.Ref {
			return ErrZoneInvalid.WithField("parent_zone_ref", "không trỏ về chính vùng này")
		}
		p, err := store.LatestZone(ctx, tx, *in.ParentRef)
		if err != nil {
			return err
		}
		if p == nil || !c.seesZone(*p) || p.Province != province {
			return ErrZoneInvalid.WithField("parent_zone_ref", "không có vùng này trong tỉnh")
		}
		if p.Kind != domain.KindOutbreak || in.Kind == domain.KindOutbreak {
			return ErrZoneInvalid.WithField("parent_zone_ref", "chỉ vùng uy hiếp/vùng đệm/hướng di chuyển gắn vào một vùng dịch")
		}
	}
	today := s.today()
	if in.EffectiveOn == nil {
		in.EffectiveOn = &today
	}
	switch {
	case in.EffectiveOn.After(today):
		return ErrZoneInvalid.WithField("effective_on", "không sau hôm nay")
	case cur != nil && in.EffectiveOn.Before(cur.EffectiveOn):
		return ErrZoneInvalid.WithField("effective_on", "không sớm hơn ngày áp dụng của phiên bản trước ("+
			cur.EffectiveOn.Format(time.DateOnly)+")")
	}
	if in.Mode != domain.ModeRadius {
		reason, err := store.GeometryProblem(ctx, tx, in.Geometry)
		if err != nil {
			return err
		}
		if reason != "" {
			return ErrZoneInvalid.WithField("geometry", "hình không hợp lệ: "+reason)
		}
	}
	return nil
}

func (s *Service) zoneRow(c caller, in ZoneInput, ref uuid.UUID, event, province string) store.Zone {
	z := store.Zone{Meta: c.meta(s.now(), idemZone(in.IdemKey)), Ref: ref, Event: event, Province: province, Communes: in.Communes,
		Kind: in.Kind, Name: in.Name, Mode: in.Mode, GeoJSON: in.Geometry, Center: in.Center, RadiusM: in.RadiusM,
		OutbreakRef: in.OutbreakRef, ParentRef: in.ParentRef, Species: in.Species, Disease: in.Disease, EffectiveOn: *in.EffectiveOn,
		Note: in.Note, Reason: strings.TrimSpace(in.Reason)}
	if event == domain.EventCreated {
		z.Reason = ""
	}
	return z
}

// CreateZone vẽ một vùng mới.
func (s *Service) CreateZone(ctx context.Context, in ZoneInput) (ZoneView, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return ZoneView{}, err
	}
	ref := uuid.Nil
	if in.Ref != nil {
		ref = *in.Ref
	}
	var out store.Zone
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ZoneByIdem(ctx, tx, idemZone(in.IdemKey)); err != nil || prev != nil {
			if err != nil {
				return err
			}
			if err := s.replayable(ctx, tx, c, *prev, nil, domain.EventCreated); err != nil {
				return err
			}
			out, replayed = *prev, true
			return nil
		}
		province, err := s.callerProvince(ctx, c, in.Province)
		if err != nil {
			return err
		}
		if err := s.checkZone(ctx, tx, c, province, &in, nil); err != nil {
			return err
		}
		if ref == uuid.Nil {
			ref = newRef()
		} else if cur, err := store.LatestZone(ctx, tx, ref); err != nil {
			return err
		} else if cur != nil {
			return ErrZoneInvalid.WithField("id", "mã vùng đã dùng")
		}
		out = s.zoneRow(c, in, ref, domain.EventCreated, province)
		return store.InsertZone(ctx, tx, c.p.TenantID, &out, nil)
	})
	if dup(err) {
		return s.CreateZone(ctx, in)
	}
	if err != nil {
		return ZoneView{}, err
	}
	v := c.view(out)
	v.Replayed = replayed
	if !replayed {
		v.Warnings = s.warnings(ctx, out)
	}
	return v, nil
}

// lockZone khóa và đọc bản hiện hành của vùng (kiểm phạm vi xem).
func (s *Service) lockZone(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID) (*store.Zone, error) {
	if err := store.Lock(ctx, tx, "zone:"+ref.String()); err != nil {
		return nil, err
	}
	cur, err := store.LatestZone(ctx, tx, ref)
	if err != nil {
		return nil, err
	}
	if cur == nil || !c.seesZone(*cur) {
		return nil, ErrZoneNotFound
	}
	if !c.canEditZone(*cur) {
		return nil, ErrOutOfScope.WithDetail("vùng có xã ngoài phạm vi phụ trách")
	}
	return cur, nil
}

// replayable kiểm bản đã ghi với Idempotency-Key trước khi trả lại (phát lại): khóa chỉ dùng lại được cho cùng thao tác
// (event), cùng người ghi, cùng vùng (ref khác nil — sửa/ngưng/khôi phục), và người gọi theo phiên HIỆN HÀNH vẫn sửa được
// vùng đó — Cấp Tỉnh: tỉnh của vùng và mọi xã của vùng còn trong phạm vi; sửa/ngưng/khôi phục: kiểm thêm bản hiện hành
// của vùng (lockZone). Khóa đã dùng ở thao tác/phạm vi khác → idempotency_key_reused (422); vùng hiện hành ngoài phạm vi
// → 404/403 như yêu cầu mới. Không bao giờ trả vùng ngoài phạm vi (hết cache HTTP, sổ idem còn lâu hơn).
func (s *Service) replayable(ctx context.Context, tx pgx.Tx, c caller, prev store.Zone, ref *uuid.UUID, event string) error {
	if prev.Event != event || prev.ActorRef != c.p.ActorRef || (ref != nil && prev.Ref != *ref) || !c.canEditZone(prev) {
		return problem.IdempotencyConflict
	}
	if _, err := s.callerProvince(ctx, c, prev.Province); err != nil {
		if pb := (*problem.Problem)(nil); errors.As(err, &pb) {
			return problem.IdempotencyConflict
		}
		return err
	}
	if ref != nil {
		if _, err := s.lockZone(ctx, tx, c, *ref); err != nil {
			return err
		}
	}
	return nil
}

// UpdateZone ghi phiên bản mới của vùng (hình hoặc thuộc tính) — lý do và base_version bắt buộc.
func (s *Service) UpdateZone(ctx context.Context, in ZoneInput) (ZoneView, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return ZoneView{}, err
	}
	if in.Ref == nil {
		return ZoneView{}, ErrZoneNotFound
	}
	if in.BaseVersion == nil {
		return ZoneView{}, errBaseVersion
	}
	if !domain.ValidReason(in.Reason) {
		return ZoneView{}, errReason
	}
	var out store.Zone
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ZoneByIdem(ctx, tx, idemZone(in.IdemKey)); err != nil || prev != nil {
			if err != nil {
				return err
			}
			if err := s.replayable(ctx, tx, c, *prev, in.Ref, domain.EventUpdated); err != nil {
				return err
			}
			out, replayed = *prev, true
			return nil
		}
		cur, err := s.lockZone(ctx, tx, c, *in.Ref)
		if err != nil {
			return err
		}
		if cur.Event == domain.EventRetired {
			return ErrZoneRetired
		}
		if !versionOK(in.BaseVersion, cur.Seq) {
			return ErrZoneConflict
		}
		if err := s.checkZone(ctx, tx, c, cur.Province, &in, cur); err != nil {
			return err
		}
		out = s.zoneRow(c, in, cur.Ref, domain.EventUpdated, cur.Province)
		return store.InsertZone(ctx, tx, c.p.TenantID, &out, nil)
	})
	if dup(err) {
		return s.UpdateZone(ctx, in)
	}
	if err != nil {
		return ZoneView{}, err
	}
	v := c.view(out)
	v.Replayed = replayed
	if !replayed {
		v.Warnings = s.warnings(ctx, out)
	}
	return v, nil
}

// SetZoneStatus ngưng hiển thị (retire) hoặc khôi phục vùng từ ngày effective (mặc định hôm nay) — lý do bắt buộc. Hình
// giữ nguyên (chép từ phiên bản hiện hành); không xóa dòng nào.
func (s *Service) SetZoneStatus(ctx context.Context, ref uuid.UUID, retire bool, base *int64, reason string, effective *time.Time,
	idem string) (ZoneView, error) {
	c, err := s.writer(ctx, idem)
	if err != nil {
		return ZoneView{}, err
	}
	if base == nil {
		return ZoneView{}, errBaseVersion
	}
	if !domain.ValidReason(reason) {
		return ZoneView{}, errReason
	}
	var out store.Zone
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ZoneByIdem(ctx, tx, idemZone(idem)); err != nil || prev != nil {
			if err != nil {
				return err
			}
			ev := domain.EventRestored
			if retire {
				ev = domain.EventRetired
			}
			if err := s.replayable(ctx, tx, c, *prev, &ref, ev); err != nil {
				return err
			}
			out, replayed = *prev, true
			return nil
		}
		cur, err := s.lockZone(ctx, tx, c, ref)
		if err != nil {
			return err
		}
		if !versionOK(base, cur.Seq) {
			return ErrZoneConflict
		}
		if (cur.Event == domain.EventRetired) == retire {
			return ErrZoneConflict.WithDetail("vùng đã ở trạng thái này")
		}
		on := s.today()
		if effective != nil {
			on = *effective
		}
		if on.After(s.today()) || on.Before(cur.EffectiveOn) {
			return ErrZoneInvalid.WithField("effective_on", "từ "+cur.EffectiveOn.Format(time.DateOnly)+" đến hôm nay")
		}
		ev := domain.EventRestored
		if retire {
			ev = domain.EventRetired
		}
		out = *cur
		out.Meta, out.Event, out.Reason, out.EffectiveOn = c.meta(s.now(), idemZone(idem)), ev, strings.TrimSpace(reason), on
		prevID := cur.ID
		return store.InsertZone(ctx, tx, c.p.TenantID, &out, &prevID)
	})
	if dup(err) {
		return s.SetZoneStatus(ctx, ref, retire, base, reason, effective, idem)
	}
	if err != nil {
		return ZoneView{}, err
	}
	v := c.view(out)
	v.Replayed = replayed
	return v, nil
}

// OutbreakZonesInput: khoanh vùng theo bán kính quanh vị trí ổ dịch BM-10.
type OutbreakZonesInput struct {
	OutbreakRef uuid.UUID
	Radii       map[string]float64 // loại vùng → bán kính (m); rỗng = mặc định domain.DefaultRadii
	EffectiveOn *time.Time
	Note        string
	IdemKey     string
}

// ZonesFromOutbreak tạo vùng dịch (tâm = vị trí ổ dịch) và các vùng bị uy hiếp, vùng đệm gắn vào vùng dịch đó, trong
// một giao dịch. Gọi lặp cùng Idempotency-Key trả lại các vùng đã tạo.
func (s *Service) ZonesFromOutbreak(ctx context.Context, in OutbreakZonesInput) ([]ZoneView, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return nil, err
	}
	radii := in.Radii
	if len(radii) == 0 {
		radii = domain.DefaultRadii
	}
	kinds := []string{}
	for _, k := range []string{domain.KindOutbreak, domain.KindThreatened, domain.KindBuffer} {
		if _, ok := radii[k]; ok {
			kinds = append(kinds, k)
		}
	}
	for k := range radii {
		if !slices.Contains(kinds, k) {
			return nil, ErrZoneInvalid.WithField("radii", "chỉ vung_dich, vung_uy_hiep, vung_dem")
		}
	}
	if !slices.Contains(kinds, domain.KindOutbreak) {
		return nil, ErrZoneInvalid.WithField("radii", "cần bán kính vùng dịch (vung_dich)")
	}
	for i := 1; i < len(kinds); i++ {
		if radii[kinds[i]] <= radii[kinds[i-1]] {
			return nil, ErrZoneInvalid.WithField("radii", "bán kính tăng dần: vùng dịch < vùng bị uy hiếp < vùng đệm")
		}
	}
	var out []store.Zone
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ZoneByIdem(ctx, tx, idemZone(in.IdemKey+":"+domain.KindOutbreak)); err != nil || prev != nil {
			if prev == nil {
				return err
			}
			replayed = true
			for _, k := range kinds {
				z, err := store.ZoneByIdem(ctx, tx, idemZone(in.IdemKey+":"+k))
				if err != nil {
					return err
				}
				if z == nil {
					continue
				}
				if z.OutbreakRef == nil || *z.OutbreakRef != in.OutbreakRef {
					return problem.IdempotencyConflict
				}
				if err := s.replayable(ctx, tx, c, *z, nil, domain.EventCreated); err != nil {
					return err
				}
				out = append(out, *z)
			}
			return nil
		}
		if err := store.Lock(ctx, tx, "outbreak:"+in.OutbreakRef.String()); err != nil {
			return err
		}
		o, err := store.LatestOutbreak(ctx, tx, in.OutbreakRef)
		if err != nil {
			return err
		}
		if o == nil || !c.sees(o.Commune) {
			return ErrZoneInvalid.WithField("outbreak_ref", "không có ổ dịch này trong phạm vi")
		}
		province, err := s.provinceOf(ctx, o.Commune)
		if err != nil {
			return err
		}
		if province == "" {
			return ErrZoneInvalid.WithField("outbreak_ref", "xã của ổ dịch không còn hiệu lực")
		}
		var parent *uuid.UUID
		for _, k := range kinds {
			r := radii[k]
			zin := ZoneInput{OutbreakRef: &in.OutbreakRef, ParentRef: parent, IdemKey: in.IdemKey + ":" + k, Zone: domain.Zone{
				Kind: k, Name: domain.KindLabel[k] + " " + o.Disease + " – " + o.Commune, Communes: []string{o.Commune},
				Mode: domain.ModeRadius, Center: &domain.Point{Lat: o.Location.Lat, Lng: o.Location.Lng}, RadiusM: &r,
				EffectiveOn: in.EffectiveOn, Note: in.Note}}
			if err := s.checkZone(ctx, tx, c, province, &zin, nil); err != nil {
				return err
			}
			// Bệnh, loài chép từ ổ dịch (đã kiểm danh mục ở BM-10).
			zin.Disease, zin.Species = o.Disease, o.Species
			z := s.zoneRow(c, zin, newRef(), domain.EventCreated, province)
			if err := store.InsertZone(ctx, tx, c.p.TenantID, &z, nil); err != nil {
				return err
			}
			out = append(out, z)
			if k == domain.KindOutbreak {
				ref := z.Ref
				parent = &ref
			}
		}
		return nil
	})
	if dup(err) {
		return s.ZonesFromOutbreak(ctx, in)
	}
	if err != nil {
		return nil, err
	}
	views := make([]ZoneView, 0, len(out))
	for _, z := range out {
		v := c.view(z)
		v.Replayed = replayed
		views = append(views, v)
	}
	return views, nil
}

// ZoneQuery lọc danh sách vùng tại một ngày.
type ZoneQuery struct {
	Day         string // YYYY-MM-DD, rỗng = hôm nay
	Kind        string
	OutbreakRef *uuid.UUID
	WithRetired bool
}

// ListZones trả các vùng (phiên bản tại ngày) trong phạm vi; canDraw = người gọi vẽ được vùng mới.
func (s *Service) ListZones(ctx context.Context, q ZoneQuery) ([]ZoneView, bool, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, false, err
	}
	d, err := s.parseDay(q.Day)
	if err != nil {
		return nil, false, err
	}
	f := store.ZoneFilter{Day: d, OutbreakRef: q.OutbreakRef, WithRetired: q.WithRetired}
	if k := strings.TrimSpace(q.Kind); k != "" {
		if !slices.Contains(domain.Kinds, k) {
			return nil, false, problem.Validation.WithField("kind", "một trong "+strings.Join(domain.Kinds, ", "))
		}
		f.Kinds = []string{k}
	}
	var zs []store.Zone
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		zs, e = store.ZonesAt(ctx, tx, f)
		return e
	}); err != nil {
		return nil, false, err
	}
	out := []ZoneView{}
	for _, z := range zs {
		if c.seesZone(z) {
			out = append(out, c.view(z))
		}
	}
	return out, c.drawer(), nil
}

// GetZone đọc bản hiện hành của vùng và mọi phiên bản (mới nhất trước).
func (s *Service) GetZone(ctx context.Context, ref uuid.UUID) (ZoneDetail, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return ZoneDetail{}, err
	}
	var vs []store.Zone
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		vs, e = store.ZoneVersions(ctx, tx, ref)
		return e
	}); err != nil {
		return ZoneDetail{}, err
	}
	if len(vs) == 0 || !c.seesZone(vs[len(vs)-1]) {
		return ZoneDetail{}, ErrZoneNotFound
	}
	slices.Reverse(vs)
	refs := make([]uuid.UUID, 0, len(vs))
	for _, v := range vs {
		if !slices.Contains(refs, v.ActorRef) {
			refs = append(refs, v.ActorRef)
		}
	}
	return ZoneDetail{ZoneView: c.view(vs[0]), Versions: vs, Names: s.names(ctx, refs)}, nil
}

// warnings: cảnh báo không chặn sau khi ghi (FN-18 luồng ngoại lệ). Lỗi tra cứu không chặn.
func (s *Service) warnings(ctx context.Context, z store.Zone) []string {
	if s.Geometry == nil || z.Kind == domain.KindDirection || len(z.GeoJSON) == 0 {
		return nil
	}
	all := true
	for _, commune := range z.Communes {
		pct, err := s.Geometry.OutsideCommunePct(ctx, string(z.GeoJSON), commune)
		if err != nil || pct == nil {
			return nil // chưa có ranh giới xã → không kết luận
		}
		if pct.LessThan(decimal.NewFromInt(100)) {
			all = false
		}
	}
	if all {
		return []string{WarnOutsideCommunes}
	}
	return nil
}
