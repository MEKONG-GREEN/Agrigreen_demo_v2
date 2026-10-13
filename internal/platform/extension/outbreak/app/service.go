// Package app: use case Báo cáo dịch bệnh BM-10 và báo nghi dịch (W11.49, D4 CN9, BRD CNTS v3.7 FN-11, UR-XA-10).
//
// Quyền theo ngữ cảnh phiên KN (authz.RequireKN), phòng Chăn nuôi – Thủy sản (ma trận FN-11: Admin CRU, Cấp Tỉnh R,
// Cấp Xã CRU, Cấp TW không có): Cấp Xã lập/cập nhật ổ dịch, ghi nhận/xác nhận/bác bỏ báo nghi dịch trong các xã phụ
// trách; Cấp Tỉnh xem toàn phạm vi và xác nhận kết thúc / mở lại ổ dịch (vòng đời BM-10); nhóm chỉ xem (Ban Giám đốc,
// lãnh đạo xem phòng khác) chỉ đọc.
package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/outbreak/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/outbreak/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi (docs/errors/pf-extension-cnts-outbreak.md).
var (
	ErrOutbreakNotFound   = problem.New(http.StatusNotFound, "extension.cnts_outbreak_not_found", "Không có ổ dịch")
	ErrOutbreakInvalid    = problem.New(http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "Báo cáo dịch bệnh không hợp lệ")
	ErrOutbreakConflict   = problem.New(http.StatusConflict, "extension.cnts_outbreak_version_conflict", "Ổ dịch đã có người cập nhật — tải lại bản mới")
	ErrOutbreakClosed     = problem.New(http.StatusConflict, "extension.cnts_outbreak_closed", "Ổ dịch đã kết thúc")
	ErrOutbreakNotClosed  = problem.New(http.StatusConflict, "extension.cnts_outbreak_not_closed", "Ổ dịch chưa kết thúc")
	ErrSuspectNotFound    = problem.New(http.StatusNotFound, "extension.cnts_suspect_not_found", "Không có tin báo nghi dịch")
	ErrSuspectInvalid     = problem.New(http.StatusUnprocessableEntity, "extension.cnts_suspect_invalid", "Tin báo nghi dịch không hợp lệ")
	ErrSuspectConflict    = problem.New(http.StatusConflict, "extension.cnts_suspect_version_conflict", "Tin báo đã có người cập nhật — tải lại bản mới")
	ErrSuspectResolved    = problem.New(http.StatusConflict, "extension.cnts_suspect_resolved", "Tin báo nghi dịch đã được xác nhận hoặc bác bỏ")
	ErrReadOnly           = problem.New(http.StatusForbidden, "extension.read_only", "Ngữ cảnh chỉ xem")
	ErrSupportReason      = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency         = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Chưa nối danh mục CNTS hoặc danh mục đơn vị hành chính")
	ErrOutOfScope         = problem.New(http.StatusForbidden, "extension.out_of_scope", "Ngoài phạm vi phụ trách")
	errBaseVersionMissing = problem.Validation.WithField("base_version", "bắt buộc (khóa lạc quan)")
)

// DeptCNTS là phòng ban sở hữu nghiệp vụ.
const DeptCNTS = partyport.DeptChanNuoiThuySan

// Service là use case dịch bệnh CNTS.
type Service struct {
	DB    tenant.Beginner // pool vai extension_app (db.ForModule)
	Clock clock.Clock
	// Units: danh mục đơn vị hành chính (xã của ổ dịch phải là cấp xã có hiệu lực). nil → ghi trả 503.
	Units partyport.AdminUnits
	// Catalogs: danh mục CNTS theo ngày (bệnh, vật nuôi — W11.41). nil → ghi trả 503.
	Catalogs mdport.CNTSCatalogs
	// Names (tùy chọn): tên người thao tác cho lịch sử.
	Names func() partyport.ActorNames
}

func newID() uuid.UUID { return ids.New() }

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// day là ngày (giờ Việt Nam) của t, dạng UTC 00:00.
func day(t time.Time) time.Time {
	v := t.In(clock.Zone())
	return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) today() time.Time { return day(s.now()) }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

type caller struct {
	p authz.Principal
	k authz.KNContext
}

func (c caller) admin() bool { return c.k.Role == authz.KNAdmin }

// sees cho biết người gọi thấy dữ liệu của xã code.
func (c caller) sees(code string) bool { return c.admin() || c.k.Scope.Allows(code) }

// reader: Admin; Cấp Tỉnh, Cấp Xã của phòng Chăn nuôi – Thủy sản (FN-11). Cấp TW không có chức năng.
func (s *Service) reader(ctx context.Context) (caller, error) {
	p, k, err := authz.RequireKN(ctx)
	if err != nil {
		return caller{}, err
	}
	c := caller{p: p, k: k}
	switch {
	case k.Role == authz.KNAdmin:
	case (k.Role == authz.KNTinh || k.Role == authz.KNXa) && k.AllowsDepartment(DeptCNTS):
	default:
		return c, problem.Forbidden.WithDetail("Báo cáo dịch bệnh thuộc phòng Chăn nuôi – Thủy sản (Admin, Cấp Tỉnh, Cấp Xã)")
	}
	return c, nil
}

func (c caller) checkWrite() error {
	switch {
	case c.k.ReadOnly():
		return ErrReadOnly
	case c.k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(c.k.Support.Reason)) < 10:
		return ErrSupportReason
	}
	return nil
}

// reporter: Admin, Cấp Xã CNTS — lập, cập nhật ổ dịch; ghi nhận, xác nhận, bác bỏ báo nghi dịch (FN-11 Cấp Xã CRU).
func (s *Service) reporter(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	if !c.admin() && c.k.Role != authz.KNXa {
		return c, problem.Forbidden.WithDetail("chỉ Cấp Xã lập, cập nhật báo cáo dịch bệnh; Cấp Tỉnh xem và xác nhận kết thúc")
	}
	if err := c.checkWrite(); err != nil {
		return c, err
	}
	if s.Units == nil || s.Catalogs == nil {
		return c, ErrDependency
	}
	return c, nil
}

// closer: Admin, Cấp Tỉnh CNTS — xác nhận kết thúc / mở lại ổ dịch (BRD Mục V vòng đời BM-10).
func (s *Service) closer(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	if !c.admin() && c.k.Role != authz.KNTinh {
		return c, problem.Forbidden.WithDetail("chỉ Cấp Tỉnh xác nhận kết thúc, mở lại ổ dịch")
	}
	return c, c.checkWrite()
}

func (c caller) meta(event string, at time.Time, idem string) store.Meta {
	m := store.Meta{ID: newID(), Event: event, EventTime: at, ActorRef: c.p.ActorRef, IdemKey: idem}
	if c.k.Support != nil {
		r := strings.TrimSpace(c.k.Support.Reason)
		m.SupportReason = &r
		if c.k.Support.OnBehalfOf != uuid.Nil {
			v := c.k.Support.OnBehalfOf
			m.OnBehalfOf = &v
		}
	}
	return m
}

func dup(err error) bool { return errors.Is(ledger.Classify(err), ledger.ErrDuplicate) }

func fieldsProblem(base *problem.Problem, es domain.Errors) *problem.Problem {
	p := base
	for _, e := range es {
		p = p.WithField(e.Field, e.Reason)
	}
	return p
}

// Actor là người thao tác hiển thị ở lịch sử.
type Actor struct {
	Ref     uuid.UUID
	Display *string
}

func (s *Service) actors(ctx context.Context, refs []uuid.UUID) func(uuid.UUID) Actor {
	m := map[uuid.UUID]string{}
	if s.Names != nil && len(refs) > 0 {
		if n := s.Names(); n != nil {
			if got, err := n.DisplayNames(ctx, refs); err == nil {
				m = got
			}
		}
	}
	return func(r uuid.UUID) Actor {
		a := Actor{Ref: r}
		if n, ok := m[r]; ok && n != "" {
			nn := n
			a.Display = &nn
		}
		return a
	}
}

// names trả tên mục danh mục CNTS đang dùng hôm nay (bệnh, vật nuôi) để hiển thị; thiếu → dùng mã.
func (s *Service) names(ctx context.Context) map[string]string {
	out := map[string]string{}
	if s.Catalogs == nil {
		return out
	}
	for _, cat := range []string{mdport.CatalogCNTSDisease, mdport.CatalogCNTSSpecies} {
		items, err := s.Catalogs.ListAt(ctx, cat, s.today())
		if err != nil {
			continue
		}
		for _, it := range items {
			out[cat+":"+it.Code] = it.Name
		}
	}
	return out
}

// checkCommune: xã phải là đơn vị cấp xã có hiệu lực hôm nay và thuộc phạm vi người gọi.
func (s *Service) checkCommune(ctx context.Context, base *problem.Problem, c caller, code string) (partyport.AdminUnit, error) {
	if !c.sees(code) {
		return partyport.AdminUnit{}, ErrOutOfScope.WithField("commune_code", "xã ngoài phạm vi được phân công")
	}
	u, err := s.Units.Get(ctx, code, s.now())
	if errors.Is(err, partyport.ErrUnitNotFound) || (err == nil && u.Level != partyport.LevelCommune) {
		return u, base.WithField("commune_code", "không phải xã/phường có hiệu lực")
	}
	return u, err
}

// catalogItem tra mục danh mục CNTS có hiệu lực tại ngày on; không có/không hiệu lực → lỗi trường.
func (s *Service) catalogItem(ctx context.Context, base *problem.Problem, field, catalog, code string, on time.Time) (mdport.CatalogItem, error) {
	it, err := s.Catalogs.ItemAt(ctx, catalog, code, on)
	switch {
	case errors.Is(err, mdport.ErrCatalogItemNotFound), errors.Is(err, mdport.ErrNotEffective):
		return it, base.WithField(field, "không có hoặc không dùng tại ngày "+on.Format("02/01/2006")+" trong danh mục CNTS")
	case err != nil:
		return it, err
	case !it.Active:
		return it, base.WithField(field, "mục đã ngừng dùng")
	}
	return it, nil
}

func codeList(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// checkCatalogs kiểm bệnh, vật nuôi theo ngày diễn biến; bệnh có danh sách loài thì loài phải thuộc danh sách.
// Trả mục bệnh (cờ notifiable).
func (s *Service) checkCatalogs(ctx context.Context, base *problem.Problem, disease, species string, on time.Time) (mdport.CatalogItem, error) {
	d, err := s.catalogItem(ctx, base, "disease_code", mdport.CatalogCNTSDisease, disease, on)
	if err != nil {
		return d, err
	}
	if _, err := s.catalogItem(ctx, base, "species_code", mdport.CatalogCNTSSpecies, species, on); err != nil {
		return d, err
	}
	if sp := codeList(d.Attrs[mdport.AttrSpecies]); len(sp) > 0 && !slices.Contains(sp, species) {
		return d, base.WithField("species_code", "bệnh "+d.Name+" không áp dụng cho vật nuôi này (danh mục bệnh)")
	}
	return d, nil
}

// emitReported ghi sự kiện ổ dịch mới (cảnh báo Khẩn W11.43, lớp bản đồ W11.52).
func emitReported(ctx context.Context, tx pgx.Tx, c caller, r store.OutbreakRow, unit partyport.AdminUnit, disease mdport.CatalogItem) error {
	payload := port.OutbreakReportedV1{
		OutbreakRef: r.Ref, VersionID: r.ID, CommuneCode: r.CommuneCode, DiseaseCode: r.DiseaseCode, SpeciesCode: r.SpeciesCode,
		Notifiable: disease.Attrs["notifiable"] == "true", SuspectRef: r.SuspectRef, ReceivedAt: r.ReceivedAt.UTC().Format(time.RFC3339),
		ReportedAt: r.EventTime.UTC().Format(time.RFC3339), ReporterRef: r.ActorRef, Department: DeptCNTS, HerdSize: r.HerdSize,
		Dead: r.Dead, AsOf: r.AsOf.Format(time.DateOnly), RuleVersion: domain.RuleVersion,
	}
	if unit.ParentCode != "" {
		pc := unit.ParentCode
		payload.ProvinceCode = &pc
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: c.p.TenantID, Type: port.EventOutbreakReported, SchemaVersion: 1, OccurredAt: r.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: port.EventProducerName, Payload: payload,
	})
}
