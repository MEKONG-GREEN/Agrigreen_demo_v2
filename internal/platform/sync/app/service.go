// Package app: use case của pf/sync — thiết bị, pull, push, tệp minh chứng, xung
// đột. Giao dịch mở ở đây; dữ liệu nghiệp vụ đi qua Source/Sink của module sở hữu.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi của module (docs/errors.md).
var (
	ErrDeviceUnregistered   = problem.New(http.StatusForbidden, "sync.device_unregistered", "Thiết bị chưa đăng ký với tài khoản này")
	ErrDeviceRevoked        = problem.New(http.StatusForbidden, "sync.device_revoked", "Thiết bị đã bị thu hồi — xóa dữ liệu cục bộ")
	ErrDeviceTaken          = problem.New(http.StatusConflict, "sync.device_taken", "Mã thiết bị đã được tài khoản khác đăng ký")
	ErrDeviceNotFound       = problem.New(http.StatusNotFound, "sync.device_not_found", "Không tìm thấy thiết bị")
	ErrDeviceAlreadyRevoked = problem.New(http.StatusConflict, "sync.device_already_revoked", "Thiết bị đã bị thu hồi trước đó")
	ErrCursorInvalid        = problem.New(http.StatusBadRequest, "sync.cursor_invalid", "Con trỏ đồng bộ không hợp lệ — pull lại từ đầu")
	ErrConflictNotFound     = problem.New(http.StatusNotFound, "sync.conflict_not_found", "Không tìm thấy xung đột")
	ErrConflictState        = problem.New(http.StatusConflict, "sync.conflict_state", "Xung đột đã được xử lý")
	ErrFileChecksum         = problem.New(http.StatusUnprocessableEntity, "sync.file_checksum_mismatch", "SHA-256 bản mã không khớp sha256_ciphertext")
	ErrFileRefTaken         = problem.New(http.StatusConflict, "sync.file_ref_taken", "file_ref đã dùng cho tệp có nội dung khác")
	ErrFileNotFound         = problem.New(http.StatusNotFound, "sync.file_not_found", "Không tìm thấy tệp minh chứng")
	ErrPurposeRequired      = problem.New(http.StatusBadRequest, "sync.purpose_required", "Thiếu mục đích xử lý (header X-Purpose)")
	ErrPurposeNotAllowed    = problem.New(http.StatusForbidden, "sync.purpose_not_allowed", "Mục đích không được phép xem tệp minh chứng")
	ErrConsentMissing       = problem.New(http.StatusForbidden, "sync.consent_missing", "Chủ thể chưa đồng ý cho mục đích này")
	ErrDependency           = problem.New(http.StatusServiceUnavailable, "sync.dependency_unavailable", "Chưa nối phụ thuộc của module đồng bộ")
)

// Deps là phụ thuộc ngoài module, do gốc lắp ráp truyền vào.
type Deps struct {
	Scope partyport.ScopeResolver
	// Farmers (tùy chọn): nông hộ liên kết với tài khoản nông dân (pf/accounts). Thiếu →
	// thiết bị của nông dân không nhận gì (mặc định chặn), như trước G2 đợt 3.
	Farmers   accport.FarmerHouseholds
	Consent   partyport.ConsentChecker // tùy chọn: kiểm đồng ý trước khi cấp tệp minh chứng
	Params    mdport.Params            // tùy chọn: sync.conflict_sla_hours
	Vault     client.Client            // khóa công khai (pubkey) và mở DEK xem tệp
	Store     objstore.Store           // kho đối tượng chỉ chứa bản mã (R-40)
	CursorKey []byte                   // khóa HMAC của con trỏ pull (AG_SYNC_CURSOR_KEY)
	Log       *slog.Logger
}

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai sync_app
	Clock clock.Clock
	Deps
	// Integration: phụ thuộc tùy chọn của màn giám sát đồng bộ tích hợp (GIS US-SYNC-01).
	Integration IntegrationDeps

	pagers []pager
	sinks  map[string]port.Sink
}

// NewService gom Source/Sink. Hai Source/Sink khai cùng một entity, hoặc Source
// khai entity "pubkey" (của pf/sync), là lỗi lắp ráp.
func NewService(db tenant.Beginner, clk clock.Clock, d Deps, sources []port.Source, sinks []port.Sink) (*Service, error) {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if len(d.CursorKey) < 32 {
		return nil, fmt.Errorf("sync: khóa con trỏ phải có ít nhất 32 byte")
	}
	s := &Service{DB: db, Clock: clk, Deps: d, sinks: map[string]port.Sink{}}
	seen := map[string]bool{port.EntityPubKey: true}
	for _, src := range sources {
		if fs, ok := src.(port.FarmerSource); ok {
			for _, e := range fs.FarmerEntities() {
				if !slices.Contains(src.Entities(), e) {
					return nil, fmt.Errorf("sync: entity nông dân %q không thuộc Entities() của Source", e)
				}
			}
		}
		for _, e := range src.Entities() {
			if seen[e] {
				return nil, fmt.Errorf("sync: entity chiều xuống %q được khai hai lần", e)
			}
			seen[e] = true
			pg := sourcePager{src: src, ent: e}
			if fs, ok := src.(port.FarmerSource); ok {
				pg.farmer = slices.Contains(fs.FarmerEntities(), e)
			}
			s.pagers = append(s.pagers, pg)
		}
	}
	slices.SortFunc(s.pagers, func(a, b pager) int { return strings.Compare(a.entity(), b.entity()) })
	s.pagers = append(s.pagers, pubkeyPager{svc: s})
	for _, sk := range sinks {
		for _, e := range sk.Entities() {
			if _, dup := s.sinks[e]; dup {
				return nil, fmt.Errorf("sync: entity chiều lên %q được khai hai lần", e)
			}
			s.sinks[e] = sk
		}
	}
	return s, nil
}

// PullEntities liệt kê entity chiều xuống (đã sắp).
func (s *Service) PullEntities() []string {
	out := make([]string, 0, len(s.pagers))
	for _, p := range s.pagers {
		out = append(out, p.entity())
	}
	return out
}

func (s *Service) scope(ctx context.Context) (authz.Scope, error) {
	if s.Scope == nil {
		return authz.Scope{}, nil // chưa nối party → mặc định chặn
	}
	return s.Scope.Scope(ctx)
}

// checkDevice kiểm header X-Device-Id: thiết bị phải đăng ký bởi CHÍNH người gọi
// và chưa bị thu hồi. Ghi lần thấy cuối.
func (s *Service) checkDevice(ctx context.Context, p authz.Principal, header string) (domain.Device, error) {
	id, err := uuid.Parse(strings.TrimSpace(header))
	if header == "" || err != nil {
		return domain.Device{}, ErrDeviceUnregistered.WithDetail("Thiếu hoặc sai header " + port.HeaderDeviceID)
	}
	var d *domain.Device
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if d, e = store.DeviceByID(ctx, tx, id, false); e != nil || d == nil || d.ActorRef != p.ActorRef || d.Revoked() {
			return e
		}
		return store.TouchDevice(ctx, tx, id, s.Clock.Now())
	})
	switch {
	case err != nil:
		return domain.Device{}, err
	case d == nil || d.ActorRef != p.ActorRef:
		return domain.Device{}, ErrDeviceUnregistered
	case d.Revoked():
		return domain.Device{}, ErrDeviceRevoked
	}
	return *d, nil
}

func principal(ctx context.Context) (authz.Principal, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Principal{}, problem.Unauthenticated
	}
	return p, nil
}

func validation(errs []problem.FieldError) error {
	v := problem.Validation
	for _, e := range errs {
		v = v.WithField(e.Field, e.Reason)
	}
	return v
}

func fieldErr(field, reason string) problem.FieldError {
	return problem.FieldError{Field: field, Reason: reason}
}

func orgPtr(p authz.Principal) *uuid.UUID {
	if p.OrgRef == uuid.Nil {
		return nil
	}
	o := p.OrgRef
	return &o
}
