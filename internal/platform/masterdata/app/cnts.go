package app

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Danh mục Chăn nuôi – Thủy sản (W11.41, BRD CNTS v3.7 FN-06; pf 000146): tham chiếu giữa các danh mục, mã mộc
// dấu không trùng, facility_ref cố định, sổ hiệu lực theo ngày và cổng CNTSCatalogs.

// SeedValidFrom là ngày hiệu lực của mục CNTS nạp sẵn (áp cho mọi kỳ).
var SeedValidFrom = domain.Day(2000, 1, 1)

// Mã lỗi CNTS (docs/errors.md).
var (
	ErrStampTaken = problem.New(http.StatusConflict, "masterdata.cnts_stamp_taken",
		"Mã số khắc mộc dấu đã thuộc cơ sở giết mổ khác")
	ErrCommuneInvalid = problem.New(http.StatusUnprocessableEntity, "masterdata.cnts_commune_invalid",
		"Cơ sở giết mổ phải thuộc một xã/phường/đặc khu có hiệu lực tại ngày áp dụng")
)

// cntsRefs: thuộc tính tham chiếu mục của danh mục khác (danh sách mã hoặc một mã).
var cntsRefs = map[string]map[string]string{
	port.CatalogCNTSBreed:               {port.AttrSpecies: port.CatalogCNTSSpecies},
	port.CatalogCNTSDisease:             {port.AttrSpecies: port.CatalogCNTSSpecies},
	port.CatalogCNTSVaccine:             {port.AttrSpecies: port.CatalogCNTSSpecies, "diseases": port.CatalogCNTSDisease},
	port.CatalogCNTSSlaughterFacility:   {port.AttrSpecies: port.CatalogCNTSSpecies},
	port.CatalogCNTSModelType:           {port.AttrSpecies: port.CatalogCNTSSpecies},
	port.CatalogCNTSVaccinationSchedule: {"model_type": port.CatalogCNTSModelType, "breed": port.CatalogCNTSBreed},
}

// checkCNTSRefs kiểm tham chiếu của mục CNTS (mục được trỏ phải đang dùng), xã của cơ sở giết mổ có hiệu lực tại
// ngày áp dụng, mã mộc dấu không thuộc cơ sở khác, vaccine trong quy trình tiêm đang dùng.
func checkCNTSRefs(ctx context.Context, tx pgx.Tx, it domain.Item, on time.Time) error {
	refs := cntsRefs[it.Catalog]
	keys := make([]string, 0, len(refs))
	for k := range refs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, attr := range keys {
		for _, c := range domain.CodeList(it.Attrs[attr]) {
			if err := activeRef(ctx, tx, refs[attr], c, "attrs."+attr); err != nil {
				return err
			}
		}
	}
	switch it.Catalog {
	case port.CatalogCNTSVaccinationSchedule:
		ds, err := domain.ParseDoses(it.Attrs[port.AttrDoses])
		if err != nil {
			return asProblem(domain.FieldError{Field: "attrs.doses", Err: err})
		}
		seen := map[string]bool{}
		for _, d := range ds {
			if seen[d.Vaccine] {
				continue
			}
			seen[d.Vaccine] = true
			if err := activeRef(ctx, tx, port.CatalogCNTSVaccine, d.Vaccine, "attrs.doses"); err != nil {
				return err
			}
		}
	case port.CatalogCNTSSlaughterFacility:
		u, err := store.UnitAt(ctx, tx, it.Attrs[port.AttrCommuneCode], on)
		if err != nil {
			return err
		}
		if u == nil || u.Level != partyport.LevelCommune {
			return ErrCommuneInvalid.WithField("attrs.commune_code", it.Attrs[port.AttrCommuneCode])
		}
		owners, err := store.StampOwners(ctx, tx, it.Stamps(), it.Code)
		if err != nil {
			return err
		}
		for _, s := range it.Stamps() {
			if o, taken := owners[s]; taken {
				return ErrStampTaken.WithField("attrs.stamp_codes", s+" → "+o)
			}
		}
	}
	return nil
}

func activeRef(ctx context.Context, tx pgx.Tx, catalog, code, field string) error {
	ref, err := store.Item(ctx, tx, catalog, code, false)
	if err != nil {
		return err
	}
	if ref == nil || !ref.Active {
		return ErrCatalogRef.WithField(field, code)
	}
	return nil
}

// withAttr trả bản sao của attrs có thêm k = v (không sửa map của bên gọi).
func withAttr(attrs map[string]string, k, v string) map[string]string {
	out := make(map[string]string, len(attrs)+1)
	for a, x := range attrs {
		out[a] = x
	}
	out[k] = v
	return out
}

// today là ngày nghiệp vụ hiện tại (giờ Việt Nam).
func (s *Service) today() time.Time { return domain.DayOf(s.Clock.Now()) }

// effectiveFrom: ngày áp dụng của lần ghi (zero → hôm nay).
func (s *Service) effectiveFrom(it domain.Item) time.Time {
	if it.ValidFrom.IsZero() {
		return s.today()
	}
	return it.ValidFrom
}

// writeVersion ghi dòng sổ hiệu lực cùng giao dịch với dòng catalog_change (chỉ danh mục CNTS).
func (s *Service) writeVersion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c *domain.Change, it domain.Item) error {
	if !s.catalogEffective(it.Catalog) {
		return nil
	}
	v := &domain.Version{ID: ids.New(), Catalog: it.Catalog, Code: it.Code, ChangeID: c.ID, ValidFrom: s.effectiveFrom(it),
		After: domain.Snapshot{Name: it.Name, Attrs: it.Attrs, Active: it.Active}, EventTime: c.EventTime, ActorRef: c.ActorRef,
		IdemKey: c.IdemKey}
	return store.InsertEffective(ctx, tx, tid, v)
}

// itemAt đọc mục với giá trị có hiệu lực tại ngày d (danh mục CNTS); mục chưa có sổ hiệu lực → bảng hiện trạng.
func (s *Service) itemAt(ctx context.Context, tx pgx.Tx, catalog, code string, d time.Time) (*domain.Item, error) {
	code = domain.CanonicalCode(catalog, code)
	if s.catalogEffective(catalog) {
		v, err := store.EffectiveAt(ctx, tx, catalog, code, d)
		if err != nil {
			return nil, err
		}
		if v != nil {
			it := v.Item()
			return &it, nil
		}
		if has, err := store.HasEffective(ctx, tx, catalog, code); err != nil || has {
			return nil, err // có sổ nhưng chưa hiệu lực tại d
		}
	}
	return store.Item(ctx, tx, catalog, code, false)
}

// ItemAt đọc một mục theo ngày (GET …/{catalog}/{code}?at=). Danh mục không theo hiệu lực → hiện trạng.
func (s *Service) ItemAt(ctx context.Context, catalog, code string, at time.Time) (domain.Item, error) {
	p, err := reader(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	if err := s.knownCatalog(catalog); err != nil {
		return domain.Item{}, err
	}
	var it *domain.Item
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		it, e = s.itemAt(ctx, tx, catalog, code, domain.DayOf(at))
		return e
	})
	if err != nil {
		return domain.Item{}, err
	}
	if it == nil {
		return domain.Item{}, ErrItemNotFound
	}
	return *it, nil
}

// ListItemsAt liệt kê mục của danh mục CNTS với giá trị có hiệu lực tại ngày at (không phân trang — danh mục vài
// trăm mục; mục ngừng dùng tại ngày đó chỉ khi includeInactive).
func (s *Service) ListItemsAt(ctx context.Context, catalog string, at time.Time, includeInactive bool) ([]domain.Item, error) {
	p, err := reader(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.knownCatalog(catalog); err != nil {
		return nil, err
	}
	return s.listAt(ctx, p.TenantID, catalog, domain.DayOf(at), includeInactive)
}

func (s *Service) listAt(ctx context.Context, tid uuid.UUID, catalog string, d time.Time, includeInactive bool) ([]domain.Item, error) {
	var items []domain.Item
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		items, e = store.ItemsAt(ctx, tx, catalog, d)
		return e
	})
	if err != nil || includeInactive {
		return items, err
	}
	return slices.DeleteFunc(items, func(it domain.Item) bool { return !it.Active }), nil
}

// ItemVersions liệt kê các giá trị có hiệu lực từ ngày của một mục CNTS (Admin, FN-06 "danh mục có lịch sử").
func (s *Service) ItemVersions(ctx context.Context, catalog, code string) ([]domain.Version, error) {
	p, err := authz.Require(ctx, port.RoleMasterdataAdmin)
	if err != nil {
		return nil, err
	}
	if err := s.knownCatalog(catalog); err != nil {
		return nil, err
	}
	var vs []domain.Version
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if it, e := store.Item(ctx, tx, catalog, code, false); e != nil {
			return e
		} else if it == nil {
			return ErrItemNotFound
		}
		var e error
		vs, e = store.EffectiveVersions(ctx, tx, catalog, domain.CanonicalCode(catalog, code))
		return e
	})
	return vs, err
}

// CNTSPort thực hiện port.CNTSCatalogs.
type CNTSPort struct{ Svc *Service }

var _ port.CNTSCatalogs = CNTSPort{}

func toPortItem(it domain.Item) port.CatalogItem {
	return port.CatalogItem{Catalog: it.Catalog, Code: it.Code, Name: it.Name, Attrs: it.Attrs, Active: it.Active}
}

func (p CNTSPort) itemAt(ctx context.Context, catalog, code string, on time.Time) (domain.Item, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	if !p.Svc.catalogKnown(catalog) {
		return domain.Item{}, ErrCatalogNotFound
	}
	var it *domain.Item
	err = p.Svc.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		it, e = p.Svc.itemAt(ctx, tx, catalog, code, domain.DayOf(on))
		return e
	})
	if err != nil {
		return domain.Item{}, err
	}
	if it == nil {
		return domain.Item{}, ErrItemNotFound.WithCause(port.ErrNotEffective)
	}
	return *it, nil
}

// ItemAt thực hiện port.CNTSCatalogs.
func (p CNTSPort) ItemAt(ctx context.Context, catalog, code string, on time.Time) (port.CatalogItem, error) {
	it, err := p.itemAt(ctx, catalog, code, on)
	if err != nil {
		return port.CatalogItem{}, err
	}
	return toPortItem(it), nil
}

func (p CNTSPort) listAt(ctx context.Context, catalog string, on time.Time) ([]domain.Item, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	if !p.Svc.catalogKnown(catalog) {
		return nil, ErrCatalogNotFound
	}
	return p.Svc.listAt(ctx, tid, catalog, domain.DayOf(on), false)
}

// ListAt thực hiện port.CNTSCatalogs.
func (p CNTSPort) ListAt(ctx context.Context, catalog string, on time.Time) ([]port.CatalogItem, error) {
	items, err := p.listAt(ctx, catalog, on)
	if err != nil {
		return nil, err
	}
	out := make([]port.CatalogItem, len(items))
	for i, it := range items {
		out[i] = toPortItem(it)
	}
	return out, nil
}

// FacilitiesAt thực hiện port.CNTSCatalogs.
func (p CNTSPort) FacilitiesAt(ctx context.Context, on time.Time, communeCodes []string) ([]port.SlaughterFacility, error) {
	items, err := p.listAt(ctx, port.CatalogCNTSSlaughterFacility, on)
	if err != nil {
		return nil, err
	}
	out := []port.SlaughterFacility{}
	for _, it := range items {
		if len(communeCodes) == 0 || slices.Contains(communeCodes, it.Attrs[port.AttrCommuneCode]) {
			out = append(out, domain.ToFacility(it))
		}
	}
	return out, nil
}

// Facility thực hiện port.CNTSCatalogs.
func (p CNTSPort) Facility(ctx context.Context, ref uuid.UUID, on time.Time) (port.SlaughterFacility, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return port.SlaughterFacility{}, err
	}
	var code string
	if err := p.Svc.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		code, e = store.FacilityCodeByRef(ctx, tx, ref)
		return e
	}); err != nil {
		return port.SlaughterFacility{}, err
	}
	if code == "" {
		return port.SlaughterFacility{}, port.ErrFacilityNotFound
	}
	it, err := p.itemAt(ctx, port.CatalogCNTSSlaughterFacility, code, on)
	if err != nil {
		return port.SlaughterFacility{}, err
	}
	return domain.ToFacility(it), nil
}

// UnknownFacilities thực hiện port.CNTSCatalogs: ref không phải cơ sở giết mổ đang dùng hôm nay.
func (p CNTSPort) UnknownFacilities(ctx context.Context, refs []uuid.UUID) ([]uuid.UUID, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	fs, err := p.FacilitiesAt(ctx, p.Svc.Clock.Now(), nil)
	if err != nil {
		return nil, err
	}
	known := make(map[uuid.UUID]bool, len(fs))
	for _, f := range fs {
		known[f.Ref] = true
	}
	var out []uuid.UUID
	for _, r := range refs {
		if !known[r] {
			out = append(out, r)
		}
	}
	return out, nil
}

// FacilityCommunes thực hiện port.CNTSCatalogs.
func (p CNTSPort) FacilityCommunes(ctx context.Context, refs []uuid.UUID, on time.Time) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(refs) == 0 {
		return out, nil
	}
	fs, err := p.FacilitiesAt(ctx, on, nil)
	if err != nil {
		return nil, err
	}
	for _, f := range fs {
		if slices.Contains(refs, f.Ref) {
			out[f.Ref] = f.CommuneCode
		}
	}
	return out, nil
}

// ScheduleAt thực hiện port.CNTSCatalogs.
func (p CNTSPort) ScheduleAt(ctx context.Context, code string, on time.Time) (port.VaccinationSchedule, error) {
	it, err := p.itemAt(ctx, port.CatalogCNTSVaccinationSchedule, code, on)
	if err != nil {
		return port.VaccinationSchedule{}, err
	}
	ds, err := domain.ParseDoses(it.Attrs[port.AttrDoses])
	if err != nil {
		return port.VaccinationSchedule{}, err
	}
	return port.VaccinationSchedule{Code: it.Code, Name: it.Name, ModelType: it.Attrs["model_type"], Breed: it.Attrs["breed"],
		Doses: ds, ValidFrom: it.ValidFrom}, nil
}

// VaccineSpecies thực hiện port.CNTSCatalogs.
func (p CNTSPort) VaccineSpecies(ctx context.Context, on time.Time) (map[string][]string, error) {
	items, err := p.listAt(ctx, port.CatalogCNTSVaccine, on)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(items))
	for _, it := range items {
		out[it.Code] = domain.CodeList(it.Attrs[port.AttrSpecies])
	}
	return out, nil
}

// prepareFacilityRef gắn facility_ref cho cơ sở giết mổ trước khi kiểm: tạo mới → UUID mới (bỏ giá trị bên gọi
// gửi); sửa → giá trị tạm, thay bằng ref hiện có trong giao dịch (keepFacilityRef).
func prepareFacilityRef(in domain.Item) domain.Item {
	if in.Catalog == port.CatalogCNTSSlaughterFacility {
		in.Attrs = withAttr(in.Attrs, port.AttrFacilityRef, ids.New().String())
	}
	return in
}

// keepFacilityRef giữ facility_ref của mục hiện có (ref cố định qua mọi lần sửa).
func keepFacilityRef(upd domain.Item, cur domain.Item) domain.Item {
	if upd.Catalog == port.CatalogCNTSSlaughterFacility && strings.TrimSpace(cur.Attrs[port.AttrFacilityRef]) != "" {
		upd.Attrs = withAttr(upd.Attrs, port.AttrFacilityRef, cur.Attrs[port.AttrFacilityRef])
	}
	return upd
}
