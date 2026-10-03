package app

// Quyết định A4 (03/10/2026, FN-TI-02.06): email Chi cục vượt giới hạn đính kèm mang LIÊN KẾT TẢI do api-platform phục
// vụ (trong lãnh thổ) thay URL ký sẵn 15 phút của kho đối tượng. Mỗi (lần gửi × tệp × người nhận To) một liên kết, hạn
// theo tham số reports.dispatch_link_ttl_hours (mặc định 168 = 7 ngày). Mỗi lần mở liên kết ghi một dòng sổ
// dispatch_link_access (người nhận, thời điểm, IP đã băm, kết quả) và đi qua điểm kiểm soát xuất (export_audit kind
// download + pf/metering kênh report_export, ADR-0024). Hết hạn hoặc Admin thu hồi → 410.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/dispatch"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Mã lỗi liên kết tải (docs/errors.md, mục pf/reports).
var (
	ErrLinkNotFound = problem.New(http.StatusNotFound, "reports.link_not_found", "Liên kết tải không tồn tại")
	ErrLinkExpired  = problem.New(http.StatusGone, "reports.link_expired", "Liên kết tải đã hết hạn — đề nghị đơn vị gửi lại báo cáo")
	ErrLinkRevoked  = problem.New(http.StatusGone, "reports.link_revoked", "Liên kết tải đã bị thu hồi")
)

// linkTTL đọc hạn liên kết (giờ) có hiệu lực tại at, kẹp trong [1, MaxLinkTTLHours].
func (s *Service) linkTTL(ctx context.Context, at time.Time) (int, error) {
	h, err := s.paramInt(ctx, port.ParamDispatchLinkTTLHours, at, decimal.NewFromInt(dispatch.DefaultLinkTTLHours))
	if err != nil {
		return 0, err
	}
	return min(max(h, 1), dispatch.MaxLinkTTLHours), nil
}

// issueLink ghi một liên kết trong tx và trả dòng chữ cho email "tên tệp: URL (hết hạn …)".
func (s *Service) issueLink(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d store.Dispatch, f store.DispatchFile, recipient uuid.UUID,
	ttlHours int, now time.Time) (string, error) {
	id := ids.New()
	tok, hash, err := dispatch.NewLinkToken(tid, id)
	if err != nil {
		return "", err
	}
	exp := now.Add(time.Duration(ttlHours) * time.Hour)
	if err := store.InsertDispatchLink(ctx, tx, tid, store.DispatchLink{ID: id, DispatchID: d.ID, FileID: f.ID, RecipientRef: recipient,
		TokenHash: hash, TTLHours: ttlHours, ExpiresAt: exp, CreatedAt: now}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s: %s (hết hạn %s)", f.FileName, dispatch.LinkURL(s.LinkDownloadBase, tok), exp.In(clock.Zone()).Format("15:04 02/01/2006")), nil
}

// LinkFile là tệp phục vụ qua liên kết tải.
type LinkFile struct {
	Body        io.ReadCloser
	Name        string
	ContentType string
	Size        int64
}

func contentTypeOf(format string) string {
	if format == dispatch.FormatXLSX {
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	return "application/pdf"
}

// OpenDispatchLink phục vụ GET /v1/public/report-links/{token} (không đăng nhập — mã thông báo là quyền truy cập).
// Mã sai định dạng/không có → 404 (không ghi sổ vì không biết liên kết); sai bí mật → 404 + sổ "invalid"; hết hạn →
// 410 + "expired"; thu hồi → 410 + "revoked"; điểm kiểm soát xuất từ chối → lỗi của điểm kiểm soát + "egress_denied";
// còn lại → "served" (cùng giao dịch với export_audit) rồi đọc tệp từ kho đối tượng trong lãnh thổ.
func (s *Service) OpenDispatchLink(ctx context.Context, token, ip string) (LinkFile, error) {
	tid, linkID, got, ok := dispatch.ParseLinkToken(token)
	if !ok {
		return LinkFile{}, ErrLinkNotFound
	}
	if s.Store == nil {
		return LinkFile{}, ErrDependency.WithDetail("chưa cấu hình kho đối tượng")
	}
	now := s.now()
	var l *store.DispatchLink
	var file store.DispatchFile
	var rc io.ReadCloser
	var size int64
	outcome := ""
	egressDenied := false
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		if l, err = store.DispatchLinkByID(ctx, tx, linkID); err != nil || l == nil {
			return err
		}
		switch {
		case !dispatch.TokenMatches(l.TokenHash, got):
			outcome = store.LinkInvalid
		case l.RevokedAt != nil:
			outcome = store.LinkRevoked
		case !now.Before(l.ExpiresAt):
			outcome = store.LinkExpired
		}
		if outcome != "" {
			return nil
		}
		fs, err := store.DispatchFiles(ctx, tx, l.DispatchID)
		if err != nil {
			return err
		}
		found := false
		for _, f := range fs {
			if f.ID == l.FileID {
				file, found = f, true
			}
		}
		if !found {
			return ErrLinkNotFound
		}
		// Điểm kiểm soát xuất: bên nhận = người nhận Chi cục (mã danh bạ), nguồn = lần gửi.
		if err := s.egress(ctx, tx, Egress{Kind: EgressDownload, TenantID: tid, ActorRef: l.RecipientRef, TemplateKey: "kn." + strings.ToLower(file.FormCode),
			Format: file.Format, JobID: l.DispatchID}); err != nil {
			egressDenied = true
			return err
		}
		// Mở tệp TRƯỚC khi ghi "served": kho đối tượng lỗi → giao dịch hủy, không có dòng sổ/export_audit cho lần tải
		// không thành.
		body, info, err := s.Store.Get(ctx, file.ObjectKey)
		if err != nil {
			return err
		}
		rc, size = body, info.Size
		outcome = store.LinkServed
		return s.logLinkAccess(ctx, tx, tid, *l, outcome, got, ip, now)
	})
	if err != nil && rc != nil {
		_ = rc.Close()
	}
	if l == nil && err == nil {
		return LinkFile{}, ErrLinkNotFound
	}
	if err != nil {
		var p *problem.Problem
		if l != nil && egressDenied && errors.As(err, &p) && p.Status != http.StatusNotFound {
			// Điểm kiểm soát từ chối (enforce): giao dịch trên đã hủy — ghi sổ ở giao dịch riêng.
			_ = s.tx(ctx, tid, func(tx pgx.Tx) error { return s.logLinkAccess(ctx, tx, tid, *l, store.LinkEgressDenied, got, ip, now) })
		}
		return LinkFile{}, err
	}
	if outcome != store.LinkServed {
		if err := s.tx(ctx, tid, func(tx pgx.Tx) error { return s.logLinkAccess(ctx, tx, tid, *l, outcome, got, ip, now) }); err != nil {
			return LinkFile{}, err
		}
		s.log().Warn("liên kết tải Chi cục bị từ chối", "event", "reports.dispatch_link", "tenant_id", tid, "link_id", l.ID,
			"dispatch_id", l.DispatchID, "outcome", outcome)
		switch outcome {
		case store.LinkRevoked:
			return LinkFile{}, ErrLinkRevoked
		case store.LinkExpired:
			return LinkFile{}, ErrLinkExpired
		}
		return LinkFile{}, ErrLinkNotFound
	}
	s.log().Info("tải tệp Chi cục qua liên kết", "event", "reports.dispatch_link", "tenant_id", tid, "link_id", l.ID, "dispatch_id", l.DispatchID,
		"outcome", "served")
	return LinkFile{Body: rc, Name: file.FileName, ContentType: contentTypeOf(file.Format), Size: size}, nil
}

// logLinkAccess ghi một dòng sổ truy cập (IP chỉ ở dạng HMAC theo liên kết).
func (s *Service) logLinkAccess(ctx context.Context, tx pgx.Tx, tid uuid.UUID, l store.DispatchLink, outcome string, tokenHash []byte, ip string,
	now time.Time) error {
	a := store.LinkAccess{ID: ids.New(), LinkID: l.ID, DispatchID: l.DispatchID, FileID: l.FileID, RecipientRef: l.RecipientRef, Outcome: outcome,
		EventTime: now}
	if outcome != store.LinkInvalid { // sai bí mật: không băm IP bằng khóa mà người gọi không có
		if h := dispatch.IPHash(tokenHash, ip); h != "" {
			a.IPHash = &h
		}
	}
	return store.InsertLinkAccess(ctx, tx, tid, &a)
}

// linkAdmin: quản trị nền tảng hoặc quản trị báo cáo (Admin API, MFA) thu hồi / xem sổ truy cập liên kết.
func linkAdmin(ctx context.Context) (authz.Principal, error) {
	return authz.Require(ctx, authz.RolePlatformAdmin, port.RoleReportsAdmin)
}

// RevokeDispatchLinks thu hồi mọi liên kết tải còn hiệu lực của một lần gửi (Admin, lý do 10–500 ký tự). Ghi
// dispatch_event "links_revoked". Gọi lặp (cùng khóa) → kết quả cũ; không còn liên kết hiệu lực → revoked = 0.
func (s *Service) RevokeDispatchLinks(ctx context.Context, dispatchID uuid.UUID, reason, idem string) (int64, error) {
	p, err := linkAdmin(ctx)
	if err != nil {
		return 0, err
	}
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return 0, problem.Validation.WithField("reason", "từ 10 đến 500 ký tự")
	}
	key := idemOr(idem, "revoke-links:"+dispatchID.String())
	var n int64
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		d, err := store.DispatchByID(ctx, tx, dispatchID, true)
		if err != nil {
			return err
		}
		if d == nil {
			return ErrDispatchNotFound
		}
		now := s.now()
		if n, err = store.RevokeDispatchLinks(ctx, tx, dispatchID, p.ActorRef, reason, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"revoked": n, "reason": reason})
		ev := store.DispatchEvent{ID: ids.New(), DispatchID: d.ID, Action: "links_revoked", Payload: payload, Department: d.Department,
			Province: d.Province, EventTime: now, ActorRef: p.ActorRef, IdemKey: key}
		return store.InsertDispatchEvent(ctx, tx, p.TenantID, &ev)
	})
	return n, err
}

// DispatchLinkLog trả sổ truy cập liên kết tải của một lần gửi (Admin).
func (s *Service) DispatchLinkLog(ctx context.Context, dispatchID uuid.UUID) ([]store.LinkAccess, error) {
	p, err := linkAdmin(ctx)
	if err != nil {
		return nil, err
	}
	var out []store.LinkAccess
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		d, err := store.DispatchByID(ctx, tx, dispatchID, false)
		if err != nil {
			return err
		}
		if d == nil {
			return ErrDispatchNotFound
		}
		out, err = store.LinkAccesses(ctx, tx, dispatchID)
		return err
	})
	return out, err
}
