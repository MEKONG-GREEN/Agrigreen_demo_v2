// Package porttest là bản giả trong bộ nhớ của cổng cấp quyền chia sẻ pf/party (ADR-0020) cho
// kiểm thử của module khác (geo, cropcycle, mrv). Hành vi thật (sổ, R-56, bảng chiếu hai phía,
// sổ đọc) kiểm ở kiểm thử tích hợp của pf/party và kịch bản e2e G3.
package porttest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// Sharing cài đặt port.PartnerAccess và port.ValveGrants trong bộ nhớ.
type Sharing struct {
	mu     sync.Mutex
	grants []port.ShareGrant
	// Reads là các lần đọc đã ghi sổ (RecordRead).
	Reads []port.PartnerRead
	// FailAudit: RecordRead trả lỗi (mô phỏng sổ đọc không ghi được).
	FailAudit bool
}

var (
	_ port.PartnerAccess = (*Sharing)(nil)
	_ port.ValveGrants   = (*Sharing)(nil)
)

// New tạo bản giả rỗng (mọi đọc → 403, mọi van → ErrValveGrantMissing).
func New() *Sharing { return &Sharing{} }

// Grant thêm một cấp quyền đang hiệu lực grantor → grantee với các loại kinds.
func (s *Sharing) Grant(grantor, grantee uuid.UUID, kinds ...string) port.ShareGrant {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := port.ShareGrant{ID: uuid.Must(uuid.NewV7()), Grantor: grantor, Grantee: grantee, Kinds: kinds,
		ValidFrom: time.Unix(0, 0).UTC(), ValidTo: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)}
	s.grants = append(s.grants, g)
	return g
}

// Revoke xóa cấp quyền khỏi bản giả.
func (s *Sharing) Revoke(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants = slices.DeleteFunc(s.grants, func(g port.ShareGrant) bool { return g.ID == id })
}

// ReadCount trả số lần đọc đã ghi sổ.
func (s *Sharing) ReadCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Reads)
}

func (s *Sharing) matching(grantee, grantor uuid.UUID, kind string) []port.ShareGrant {
	var out []port.ShareGrant
	for _, g := range s.grants {
		if g.Grantee == grantee && (grantor == uuid.Nil || g.Grantor == grantor) && (kind == "" || g.Covers(kind)) {
			out = append(out, g)
		}
	}
	return out
}

// Authorize cài đặt port.PartnerAccess.
func (s *Sharing) Authorize(ctx context.Context, kind string, grantor uuid.UUID) (port.ShareGrant, error) {
	p, err := authz.Require(ctx, port.RolePartnerSync)
	if err != nil {
		return port.ShareGrant{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	gs := s.matching(p.TenantID, grantor, kind)
	if len(gs) == 0 {
		return port.ShareGrant{}, port.ErrShareGrantMissing
	}
	for _, g := range gs[1:] {
		if g.Grantor != gs[0].Grantor {
			return port.ShareGrant{}, port.ErrShareGrantorAmbiguous
		}
	}
	return gs[0], nil
}

// RecordRead cài đặt port.PartnerAccess.
func (s *Sharing) RecordRead(_ context.Context, _ port.ShareGrant, r port.PartnerRead) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailAudit {
		return port.ErrPartnerAuditUnavailable
	}
	s.Reads = append(s.Reads, r)
	return nil
}

// ValveGrantors cài đặt port.ValveGrants.
func (s *Sharing) ValveGrantors(_ context.Context, grantee uuid.UUID, kind string, _ time.Time) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []uuid.UUID
	for _, g := range s.matching(grantee, uuid.Nil, kind) {
		if !slices.Contains(out, g.Grantor) {
			out = append(out, g.Grantor)
		}
	}
	return out, nil
}
