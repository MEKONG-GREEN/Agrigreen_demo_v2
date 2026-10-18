package app

// D24 (CEO 13/10/2026 — W10.05, v4.3 P-4): vô hiệu tài khoản không hoạt động.
//
// Job port.JobDormant (worker, chỉ leader, mỗi ngày): với mỗi tài khoản đang hoạt động do nền tảng quản lý (bảng
// pf_accounts.account, trừ nông dân), mốc hoạt động = muộn nhất của lần đăng nhập thành công gần nhất (sự kiện
// LOGIN của Keycloak), phiên gần nhất, lần tạo/mở khóa ở sổ account_event, lúc tạo trong Keycloak. Không hoạt động
// từ accounts.dormant_warn_days (38) → ghi 'dormant_warned' + phát pf.accounts.user.dormant_warning (pf/notify báo
// cho chính tài khoản), một lần cho mỗi mốc; từ accounts.dormant_disable_days (45) → khóa (Keycloak enabled=false,
// thu hồi phiên, sổ 'locked' cause dormant_45d, actor = tác nhân hệ thống). Admin mở khóa như mọi lần khóa.
// Nông hộ tự kích hoạt lại bằng OTP chờ kênh SMS (W10.06): tài khoản nông dân chưa vào job.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Ngưỡng mặc định khi tenant chưa có tham số đã duyệt (D24).
const (
	DefaultDormantWarnDays    = 38
	DefaultDormantDisableDays = 45
)

// DormantReason là lý do ghi vào sổ khi job khóa (≥ 20 ký tự như khóa thủ công).
const DormantReason = "Tự động khóa: không hoạt động quá ngưỡng ngày (D24, job accounts_dormant)"

// loginEventsScan: số sự kiện đăng nhập mới nhất đọc để tìm LOGIN gần nhất.
const loginEventsScan = 50

// TenantLister trả danh sách tenant cần chạy job.
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// DormantResult đếm kết quả một lượt job (chạy thử: số SẼ báo trước / khóa).
type DormantResult struct {
	DryRun   bool
	Checked  int
	Warned   int
	Disabled int
}

// DormantPolicy là ngưỡng (ngày) của một tenant tại một thời điểm.
type DormantPolicy struct{ WarnDays, DisableDays int }

// dormantPolicy đọc hai tham số; thiếu hoặc sai miền (1 ≤ warn < disable ≤ 365) → mặc định 38/45.
func (s *Service) dormantPolicy(ctx context.Context, tid uuid.UUID, at time.Time) (DormantPolicy, error) {
	p := DormantPolicy{WarnDays: DefaultDormantWarnDays, DisableDays: DefaultDormantDisableDays}
	if s.Params == nil {
		return p, nil
	}
	tctx := tenant.WithID(ctx, tid)
	read := func(key string, def int) (int, error) {
		v, err := s.Params.At(tctx, key, at)
		if errors.Is(err, params.ErrParamNotFound) {
			return def, nil
		}
		if err != nil {
			return 0, err
		}
		if !v.Value.Equal(v.Value.Truncate(0)) || v.Value.LessThan(decimal.NewFromInt(1)) || v.Value.GreaterThan(decimal.NewFromInt(365)) {
			return def, nil
		}
		return int(v.Value.IntPart()), nil
	}
	w, err := read(mdport.ParamAccountsDormantWarnDays, p.WarnDays)
	if err != nil {
		return p, err
	}
	d, err := read(mdport.ParamAccountsDormantDisableDays, p.DisableDays)
	if err != nil {
		return p, err
	}
	if w < d {
		p.WarnDays, p.DisableDays = w, d
	}
	return p, nil
}

// RunDormant là thân job port.JobDormant. Lỗi ở một tenant không chặn tenant khác (trả lỗi gộp).
func (s *Service) RunDormant(ctx context.Context, tenants TenantLister) (DormantResult, error) {
	res := DormantResult{DryRun: s.DormantDryRun}
	if s.Dir == nil {
		return res, errNoDirectoryYet
	}
	sr, ok := s.Dir.(port.SessionReader)
	if !ok {
		// Không đọc được lần đăng nhập thì không phân biệt được tài khoản đang dùng — không khóa ai.
		return res, errors.New("accounts: Directory không đọc được phiên/sự kiện đăng nhập — bỏ job accounts_dormant")
	}
	tids, err := tenants(ctx)
	if err != nil {
		return res, err
	}
	var errs []error
	for _, tid := range tids {
		if err := s.dormantTenant(ctx, sr, tid, &res); err != nil {
			errs = append(errs, fmt.Errorf("tenant %s: %w", tid, err))
		}
	}
	return res, errors.Join(errs...)
}

func (s *Service) dormantTenant(ctx context.Context, sr port.SessionReader, tid uuid.UUID, res *DormantResult) error {
	now := s.now()
	pol, err := s.dormantPolicy(ctx, tid, now)
	if err != nil {
		return err
	}
	var cands []domain.Account
	var react map[uuid.UUID]time.Time
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		if cands, e = store.DormantCandidates(ctx, tx); e != nil {
			return e
		}
		react, e = store.LastReactivations(ctx, tx)
		return e
	}); err != nil {
		return err
	}
	var errs []error
	for _, acc := range cands {
		if err := ctx.Err(); err != nil {
			return err
		}
		res.Checked++
		if err := s.dormantOne(ctx, sr, tid, acc, react[acc.UserRef], pol, now, res); err != nil {
			errs = append(errs, fmt.Errorf("tài khoản %s: %w", acc.UserRef, err))
		}
	}
	return errors.Join(errs...)
}

// LastActivity là mốc hoạt động muộn nhất của một tài khoản.
func (s *Service) lastActivity(ctx context.Context, sr port.SessionReader, u port.User, reactivated time.Time) (time.Time, error) {
	last := u.CreatedAt
	if reactivated.After(last) {
		last = reactivated
	}
	evs, err := sr.LoginEvents(ctx, u.ID, loginEventsScan)
	if err != nil {
		return time.Time{}, err
	}
	for _, e := range evs {
		if e.Type == "LOGIN" && e.Time.After(last) {
			last = e.Time
		}
	}
	sess, err := sr.UserSessions(ctx, u.ID)
	if err != nil {
		return time.Time{}, err
	}
	for _, x := range sess {
		if x.LastAccess.After(last) {
			last = x.LastAccess
		}
	}
	return last.UTC(), nil
}

// IdleDays là số ngày tròn không hoạt động.
func IdleDays(last, now time.Time) int { return int(now.Sub(last) / (24 * time.Hour)) }

func (s *Service) dormantOne(ctx context.Context, sr port.SessionReader, tid uuid.UUID, acc domain.Account, reactivated time.Time,
	pol DormantPolicy, now time.Time, res *DormantResult) error {
	u, err := s.Dir.GetUser(ctx, acc.UserRef)
	if errors.Is(err, port.ErrUserNotFound) {
		return nil // đã xóa khỏi Keycloak ngoài nền tảng — không có gì để khóa
	}
	if err != nil {
		return err
	}
	if u.TenantID != tid || !u.Enabled {
		return nil // khác tenant hoặc Keycloak đã tắt (khóa ngoài nền tảng) — để quản trị viên xử lý
	}
	last, err := s.lastActivity(ctx, sr, u, reactivated)
	if err != nil {
		return err
	}
	if last.IsZero() {
		return nil // không xác định được mốc — không khóa
	}
	idle := IdleDays(last, now)
	mark := strconv.FormatInt(last.Unix(), 10)
	if s.DormantDryRun {
		switch {
		case idle >= pol.DisableDays:
			res.Disabled++
		case idle >= pol.WarnDays:
			res.Warned++
		}
		return nil
	}
	switch {
	case idle >= pol.DisableDays:
		if err := s.Dir.SetEnabled(ctx, u.ID, false); err != nil {
			return err
		}
		revoked := s.Dir.Logout(ctx, u.ID) == nil
		a := acc
		a.Enabled, a.LockCause = false, port.LockDormant
		ev := newEvent(tid, port.SystemActorRef, a, domain.ActionLocked, now, "dormant_lock:"+u.ID.String()+":"+mark, DormantReason)
		ev.Details = map[string]any{"last_activity_at": last.Format(time.RFC3339), "idle_days": idle,
			"disable_days": pol.DisableDays, "sessions_revoked": revoked}
		replayed, err := s.write(ctx, tid, &ev)
		if err == nil && !replayed {
			res.Disabled++
		}
		return err
	case idle >= pol.WarnDays:
		key := "dormant_warn:" + u.ID.String() + ":" + mark
		if prev, err := s.replay(ctx, tid, key); err != nil || prev != nil {
			return err // đã báo cho mốc hoạt động này
		}
		disableAt := last.Add(time.Duration(pol.DisableDays) * 24 * time.Hour)
		ev := newEvent(tid, port.SystemActorRef, acc, domain.ActionDormantWarned, now, key, "")
		ev.Details = map[string]any{"last_activity_at": last.Format(time.RFC3339), "disable_at": disableAt.Format(time.RFC3339),
			"idle_days": idle, "warn_days": pol.WarnDays, "disable_days": pol.DisableDays}
		replayed, err := s.write(ctx, tid, &ev)
		if err == nil && !replayed {
			res.Warned++
		}
		return err
	}
	return nil
}
