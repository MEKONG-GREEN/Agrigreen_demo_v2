package kn77_test

// BF-CNTS-CAMPAIGN-POOL (#156, hậu kiểm #27 W11.50): thao tác chuyển trạng thái đợt điều tra CNTS (phát hành, nhắc, gia hạn,
// đóng, mở lại) KHÔNG giữ giao dịch pf_extension trong khi gọi cap/forms (giao dịch riêng trên CÙNG pool app_platform).
//   - Pool chỉ còn MỘT kết nối trống: mọi thao tác vẫn xong (bản cũ: giao dịch ngoài chiếm kết nối cuối, lời gọi cap/forms chờ
//     kết nối thứ hai tới hạn → 500). Chuyển thể từ TestReviewerCampaignPublishPoolBoundary của Codex.
//   - Pool chung cỡ 2, nhiều đợt thao tác song song (kể cả trùng đợt): không treo; trạng thái đợt khớp kỳ native.
//   - Ý định thao tác (cnts_campaign_intent) thay khóa giữ suốt: chặn thao tác/sửa nháp khác khi còn hạn; cap/forms lỗi/quá
//     hạn → trả ý định; mất ý định → nhận lại nếu không ai giữ, người khác giữ → 409 interrupted; phát hành ghi lỗi → hủy bù
//     kỳ native.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// campHooks: hook (nếu đặt) bọc lời gọi cap/forms theo tên thao tác (publish, progress; cổng CAS #201: snapshot, close, due,
// reopen, cancel). legacy là cổng vô điều kiện của pf/forms — kiểm thử dùng làm "người ghi khác" (ABA) ngoài đường ý định.
type campHooks struct {
	mu     sync.Mutex
	fn     func(ctx context.Context, op string, next func(context.Context) error) error
	legacy formsport.PeriodControl
	// tamper (nếu đặt) sửa ảnh kỳ native trả về từ ControlSnapshot — kiểm consumer fail closed với revision sai định danh.
	tamper func(*formsport.PeriodControlSnapshot)
}

func (h *campHooks) set(fn func(ctx context.Context, op string, next func(context.Context) error) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fn = fn
}

func (h *campHooks) run(ctx context.Context, op string, next func(context.Context) error) error {
	h.mu.Lock()
	fn := h.fn
	h.mu.Unlock()
	if fn == nil {
		return next(ctx)
	}
	return fn(ctx, op, next)
}

type campPublisher struct {
	formsport.Publisher
	h *campHooks
}

func (p campPublisher) PublishBatch(ctx context.Context, in formsport.BatchInput, idem string) (out []formsport.PublishedPeriod, err error) {
	err = p.h.run(ctx, "publish", func(ctx context.Context) error {
		var e error
		out, e = p.Publisher.PublishBatch(ctx, in, idem)
		return e
	})
	return out, err
}

type campControl struct {
	formsport.PeriodControl
	h *campHooks
}

func (c campControl) Progress(ctx context.Context, profile string, id uuid.UUID) (out formsport.PeriodProgress, err error) {
	err = c.h.run(ctx, "progress", func(ctx context.Context) error {
		var e error
		out, e = c.PeriodControl.Progress(ctx, profile, id)
		return e
	})
	return out, err
}

// campCond bọc cổng CAS của pf/forms (#159) — mọi lời gọi ghi kỳ native của đợt (#201).
type campCond struct {
	formsport.ConditionalPeriodControl
	h *campHooks
}

func (c campCond) ControlSnapshot(ctx context.Context, profile string, id uuid.UUID) (out formsport.PeriodControlSnapshot, err error) {
	err = c.h.run(ctx, "snapshot", func(ctx context.Context) error {
		var e error
		out, e = c.ConditionalPeriodControl.ControlSnapshot(ctx, profile, id)
		return e
	})
	c.h.mu.Lock()
	tamper := c.h.tamper
	c.h.mu.Unlock()
	if err == nil && tamper != nil {
		tamper(&out)
	}
	return out, err
}

func (c campCond) casRun(ctx context.Context, op string, f func(context.Context) (formsport.PeriodRevision, error)) (out formsport.PeriodRevision, err error) {
	err = c.h.run(ctx, op, func(ctx context.Context) error {
		var e error
		out, e = f(ctx)
		return e
	})
	if err != nil {
		return formsport.PeriodRevision{}, err
	}
	return out, nil
}

func (c campCond) ChangeDueCAS(ctx context.Context, profile string, id uuid.UUID, r formsport.PeriodRevision, due time.Time, reason, idem string) (formsport.PeriodRevision, error) {
	return c.casRun(ctx, "due", func(ctx context.Context) (formsport.PeriodRevision, error) {
		return c.ConditionalPeriodControl.ChangeDueCAS(ctx, profile, id, r, due, reason, idem)
	})
}

func (c campCond) ClosePeriodCAS(ctx context.Context, profile string, id uuid.UUID, r formsport.PeriodRevision, idem string) (formsport.PeriodRevision, error) {
	return c.casRun(ctx, "close", func(ctx context.Context) (formsport.PeriodRevision, error) {
		return c.ConditionalPeriodControl.ClosePeriodCAS(ctx, profile, id, r, idem)
	})
}

func (c campCond) CancelPeriodCAS(ctx context.Context, profile string, id uuid.UUID, r formsport.PeriodRevision, reason, idem string) (formsport.PeriodRevision, error) {
	return c.casRun(ctx, "cancel", func(ctx context.Context) (formsport.PeriodRevision, error) {
		return c.ConditionalPeriodControl.CancelPeriodCAS(ctx, profile, id, r, reason, idem)
	})
}

func (c campCond) ReopenPeriodCAS(ctx context.Context, profile string, id uuid.UUID, r formsport.PeriodRevision, reason, idem string) (formsport.PeriodRevision, error) {
	return c.casRun(ctx, "reopen", func(ctx context.Context) (formsport.PeriodRevision, error) {
		return c.ConditionalPeriodControl.ReopenPeriodCAS(ctx, profile, id, r, reason, idem)
	})
}

// newCampaignPoolFx dựng pf/forms + pf/extension thật trên pool cho trước (nil → mặc định), cổng cap/forms của đợt bọc hook;
// lease > 0 → hạn ý định thao tác của đợt.
func newCampaignPoolFx(t *testing.T, pool *pgxpool.Pool, lease time.Duration) (*aggFx, *campHooks) {
	return newCampaignPoolFxCond(t, pool, lease, true)
}

// newCampaignPoolFxCond như newCampaignPoolFx; cas=false → gốc lắp ráp KHÔNG nối cổng CAS (kiểm fail closed).
func newCampaignPoolFxCond(t *testing.T, pool *pgxpool.Pool, lease time.Duration, cas bool) (*aggFx, *campHooks) {
	h := &campHooks{}
	f := newAggFxPool(t, pool, facilityDir{}, func(fm *forms.Module, kn *extension.Module) {
		h.legacy = fm.Control()
		var cond formsport.ConditionalPeriodControl
		if cas {
			cond = campCond{fm.ConditionalControl(), h}
		}
		kn.UseCNTSCampaigns(extension.CNTSCampaignDeps{Forms: campPublisher{fm.Publisher(), h}, Control: campControl{fm.Control(), h},
			Conditional: cond, Figures: fm.Figures()})
		if lease > 0 {
			kn.UseCNTSCampaignLease(lease)
		}
	})
	if err := tenant.WithTenantTx(context.Background(), pgtest.Get(t).AdminPool(t, migrations.PF), f.tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO k_ref.org (tenant_id, ref, kind, code, admin_unit_code, active, source_seq)
		    VALUES ($1, $2, 'ttkn_province', 'POOL89', '89', true, 1)`, f.tid, f.node)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	return f, h
}

func (f *aggFx) newCampaign(name string) campaignView {
	f.t.Helper()
	in := map[string]any{"owner_ref": ids.New().String(), "name": name, "goal": "Kiểm giao dịch cap/forms", "field": "thuy_san", "template_code": "CNTS-BM17",
		"scope_mode": "communes", "communes": []string{"X1", "X2"}, "from_date": "2026-10-01", "to_date": "2026-10-05",
		"due_at": "2026-10-09T03:00:00Z"}
	var cv campaignView
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns", authz.KNTinh, false, in)
	f.ok(c, b, &cv)
	return cv
}

// campaignCallCtx như campaignCall (Cấp Tỉnh CNTS; manager = Lãnh đạo phòng) với ctx của yêu cầu; trả lỗi thay vì Fatal.
func (f *aggFx) campaignCallCtx(ctx context.Context, method, path string, manager bool, body any) (int, string, error) {
	return f.campaignCallKey(ctx, method, path, manager, "pool-"+ids.New().String(), body)
}

// campaignCallKey như campaignCallCtx với Idempotency-Key cho trước (gọi lặp cùng ý định).
func (f *aggFx) campaignCallKey(ctx context.Context, method, path string, manager bool, key string, body any) (int, string, error) {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	k := authz.KNContext{Role: authz.KNTinh, Department: aggDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1", "X2"}}, Manager: manager}
	kj, _ := json.Marshal(k)
	req.Header.Set("X-Test-KN", string(kj))
	// Cùng Idempotency-Key = cùng người gọi (khóa idempotent gắn người thao tác).
	req.Header.Set("X-Test-Actor", uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)).String())
	req.Header.Set(idempotency.Header, key)
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.String(), ctx.Err()
}

// campIntentSQL chạy câu lệnh trên cnts_campaign_intent của đợt ($1 tenant, $2 đợt) bằng vai quản trị; SELECT trả một số.
func campIntentSQL(t *testing.T, f *aggFx, id, q string) int {
	t.Helper()
	n := 0
	if err := tenant.WithTenantTx(context.Background(), pgtest.Get(t).AdminPool(t, migrations.PF), f.tid, func(tx pgx.Tx) error {
		if strings.HasPrefix(q, "SELECT") {
			return tx.QueryRow(context.Background(), q, f.tid, id).Scan(&n)
		}
		_, err := tx.Exec(context.Background(), q, f.tid, id)
		return err
	}); err != nil {
		t.Error(err)
	}
	return n
}

const (
	sqlCampLive   = `SELECT count(*)::int FROM pf_extension.cnts_campaign_intent WHERE tenant_id = $1 AND campaign_id = $2::uuid AND claim_id IS NOT NULL`
	sqlCampExpire = `UPDATE pf_extension.cnts_campaign_intent SET claim_until = now() - interval '1 second' WHERE tenant_id = $1 AND campaign_id = $2::uuid`
	sqlCampHold   = `INSERT INTO pf_extension.cnts_campaign_intent (tenant_id, campaign_id, claim_id, action, claim_until)
	  VALUES ($1, $2::uuid, gen_random_uuid(), 'published', now() + interval '1 minute')
	  ON CONFLICT (tenant_id, campaign_id) DO UPDATE SET claim_id = EXCLUDED.claim_id, action = EXCLUDED.action, claim_until = EXCLUDED.claim_until`
	sqlCampEvents = `SELECT count(*)::int FROM pf_extension.cnts_campaign_event WHERE tenant_id = $1 AND campaign_id = $2::uuid`
)

func (f *aggFx) getCampaign(id string) campaignView {
	f.t.Helper()
	var cv campaignView
	c, b := f.campaignCall(http.MethodGet, "/v1/extension/cnts/campaigns/"+id, authz.KNTinh, false, nil)
	f.ok(c, b, &cv)
	return cv
}

func (f *aggFx) formsPeriodStatus(pid string) string {
	f.t.Helper()
	var pv struct {
		Status string `json:"status"`
	}
	c, b := f.call(http.MethodGet, "/v1/forms/periods/"+pid+"?profile=kn_cnts", authz.KNTinh, nil)
	f.ok(c, b, &pv)
	return pv.Status
}

func wantProblem(t *testing.T, status int, body string, code string, wantStatus int) {
	t.Helper()
	if status != wantStatus || !strings.Contains(body, `"`+code+`"`) {
		t.Fatalf("muốn %d %s, nhận %d %s", wantStatus, code, status, body)
	}
}

// Mọi thao tác chuyển trạng thái khi pool app_platform chỉ còn MỘT kết nối trống (bản cũ: phát hành 500 sau hạn — Codex
// TestReviewerCampaignPublishPoolBoundary; nhắc/gia hạn/đóng/mở lại cùng mẫu change()).
func TestBFCNTSCampaignPool_TransitionsWithOneFreeConn(t *testing.T) {
	f, _ := newCampaignPoolFx(t, nil, 0)
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	cv := f.newCampaign("Đợt kiểm pool một kết nối")
	steps := []struct {
		name, path string
		manager    bool
		body       any
		state      string
	}{
		{"publish", "/publish", false, nil, "collecting"},
		{"remind", "/remind", false, nil, "collecting"},
		{"extend", "/extend", false, map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Xã X2 mất điện nhiều ngày"}, "collecting"},
		{"close", "/close", false, nil, "closed"},
		{"reopen", "/reopen", true, map[string]any{"due_at": "2026-10-15T03:00:00Z", "reason": "Bổ sung số liệu xã X2 theo chỉ đạo"}, "collecting"},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			max := int(pool.Config().MaxConns)
			if max < 2 {
				t.Fatal("cần pool ≥ 2 kết nối")
			}
			var held []*pgxpool.Conn
			acq, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for i := 0; i < max-1; i++ {
				c, err := pool.Acquire(acq)
				if err != nil {
					t.Fatal(err)
				}
				held = append(held, c)
			}
			ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			st, body, err := f.campaignCallCtx(ctx, http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+s.path, s.manager, s.body)
			for _, c := range held {
				c.Release()
			}
			if err != nil || st != http.StatusOK {
				t.Fatalf("%s khi pool chỉ còn 1 kết nối trống: HTTP %d err=%v %.300s", s.name, st, err, body)
			}
			if got := f.getCampaign(cv.ID); got.State != s.state {
				t.Fatalf("%s: trạng thái %q, muốn %q — %.300s", s.name, got.State, s.state, body)
			}
		})
	}
	got := f.getCampaign(cv.ID)
	acts := []string{}
	for _, e := range got.Events {
		acts = append(acts, e.Action)
	}
	if strings.Join(acts, ",") != "published,reminded,extended,closed,reopened" {
		t.Fatalf("sự kiện: %v", acts)
	}
	if got.DueAt != "2026-10-15T03:00:00Z" || got.Progress == nil || got.Progress.PeriodStatus != "reopened" {
		t.Fatalf("đợt sau mở lại: due=%s progress=%+v", got.DueAt, got.Progress)
	}
	if n := campIntentSQL(t, f, cv.ID, sqlCampLive); n != 0 {
		t.Fatalf("ý định còn giữ sau thao tác: %d", n)
	}
}

// Pool chung cỡ 2 (gốc lắp ráp: pf/forms + pf/extension), nhiều đợt phát hành song song — mỗi đợt hai yêu cầu trùng: không
// treo; mỗi đợt đúng MỘT sự kiện phát hành và một kỳ native đang mở. Rồi đóng + gia hạn song song trên từng đợt: trạng
// thái đợt khớp kỳ native.
func TestBFCNTSCampaignPool_ConcurrentOnSmallSharedPool(t *testing.T) {
	const poolSize = 2
	f, _ := newCampaignPoolFx(t, sharedUnitPool(t, pgtest.Get(t), poolSize), 0)
	var cs []campaignView
	for i := 0; i < 3; i++ {
		cs = append(cs, f.newCampaign(fmt.Sprintf("Đợt song song %d", i)))
	}
	var calls []poolCall
	for _, cv := range cs {
		for j := 0; j < 2; j++ {
			calls = append(calls, poolCall{name: fmt.Sprintf("Publish#%s/%d", cv.ID[:8], j), want: []int{http.StatusOK, http.StatusConflict},
				do: func(ctx context.Context) (int, string, error) {
					return f.campaignCallCtx(ctx, http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", false, nil)
				}})
		}
	}
	runPoolStorm(t, "CNTS phát hành đợt", poolSize, calls)
	for _, cv := range cs {
		got := f.getCampaign(cv.ID)
		pub := 0
		for _, e := range got.Events {
			if e.Action == "published" {
				pub++
			}
		}
		if got.State != "collecting" || pub != 1 || got.FormsPeriodID == nil || f.formsPeriodStatus(*got.FormsPeriodID) != "open" {
			t.Fatalf("đợt %s sau phát hành song song: state=%s published=%d period=%v", cv.ID, got.State, pub, got.FormsPeriodID)
		}
		if n := campIntentSQL(t, f, cv.ID, sqlCampLive); n != 0 {
			t.Fatalf("ý định còn giữ: %d", n)
		}
	}
	calls = nil
	for _, cv := range cs {
		calls = append(calls,
			poolCall{name: "Close#" + cv.ID[:8], want: []int{http.StatusOK, http.StatusConflict}, do: func(ctx context.Context) (int, string, error) {
				return f.campaignCallCtx(ctx, http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/close", false, nil)
			}},
			poolCall{name: "Extend#" + cv.ID[:8], want: []int{http.StatusOK, http.StatusConflict}, do: func(ctx context.Context) (int, string, error) {
				return f.campaignCallCtx(ctx, http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", false,
					map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Gia hạn song song khi đóng đợt"})
			}})
	}
	runPoolStorm(t, "CNTS đóng + gia hạn đợt", poolSize, calls)
	for _, cv := range cs {
		got := f.getCampaign(cv.ID)
		fs := f.formsPeriodStatus(*got.FormsPeriodID)
		if (got.State == "closed") != (fs == "closed") {
			t.Fatalf("đợt %s lệch kỳ native: đợt %s, kỳ %s", cv.ID, got.State, fs)
		}
		if n := campIntentSQL(t, f, cv.ID, sqlCampLive); n != 0 {
			t.Fatalf("ý định còn giữ: %d", n)
		}
	}
}

// Ý định còn hạn chặn thao tác khác và sửa nháp (409 busy, KHÔNG gọi cap/forms); hết hạn → chạy được, ý định được trả.
func TestBFCNTSCampaignPool_IntentBlocksOtherWriters(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.newCampaign("Đợt kiểm ý định")
	formsCalls := 0
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if op == "publish" {
			formsCalls++
		}
		return next(ctx)
	})
	campIntentSQL(t, f, cv.ID, sqlCampHold)
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
	wantProblem(t, c, string(b), "extension.cnts_campaign_busy", http.StatusConflict)
	in := map[string]any{"owner_ref": ids.New().String(), "name": "Sửa nháp khi đang phát hành", "goal": "x", "field": "thuy_san", "template_code": "CNTS-BM17",
		"scope_mode": "communes", "communes": []string{"X1"}, "from_date": "2026-10-01", "to_date": "2026-10-05", "due_at": "2026-10-09T03:00:00Z"}
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/draft", authz.KNTinh, false, in)
	wantProblem(t, c, string(b), "extension.cnts_campaign_busy", http.StatusConflict)
	if formsCalls != 0 {
		t.Fatalf("cap/forms bị gọi khi đợt đang bận: %d", formsCalls)
	}
	campIntentSQL(t, f, cv.ID, sqlCampExpire)
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
	f.ok(c, b, nil)
	if n := campIntentSQL(t, f, cv.ID, sqlCampLive); n != 0 || formsCalls != 1 {
		t.Fatalf("sau phát hành: ý định còn %d, gọi cap/forms %d", n, formsCalls)
	}
	// Sự kiện ghi ngoài đường ý định bị trigger chặn khi ý định còn hạn.
	campIntentSQL(t, f, cv.ID, sqlCampHold)
	err := tenant.WithTenantTx(context.Background(), pgtest.Get(t).AdminPool(t, migrations.PF), f.tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), `INSERT INTO pf_extension.cnts_campaign_event (id, tenant_id, stream_key, campaign_id, action,
		    reminded, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'cnts_campaign_event:' || $3::uuid::text, $3, 'reminded', 1, now(), $4, $5, 'cnts.campaign/v1')`,
			ids.New(), f.tid, cv.ID, ids.New(), "pool-test:"+ids.New().String())
		return e
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "cnts_campaign_busy" {
		t.Fatalf("trigger phải chặn sự kiện khi ý định còn hạn: %v", err)
	}
}

// cap/forms treo: lời gọi bị cắt TRƯỚC hạn ý định (hạn neo trước giao dịch nhận), 503; ý định trả ngay; đợt không đổi.
func TestBFCNTSCampaignPool_FormsDeadlineReleasesIntent(t *testing.T) {
	const lease = 4 * time.Second
	f, h := newCampaignPoolFx(t, nil, lease)
	cv := f.newCampaign("Đợt kiểm hạn cap/forms")
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
	f.ok(c, b, &cv)
	adm := pgtest.Get(t).AdminPool(t, migrations.PF)
	var gap time.Duration
	var sawDeadline bool
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if op != "close" {
			return next(ctx)
		}
		dl, ok := ctx.Deadline()
		sawDeadline = ok
		var until time.Time
		if err := tenant.WithTenantTx(context.Background(), adm, f.tid, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), `SELECT claim_until FROM pf_extension.cnts_campaign_intent
			  WHERE tenant_id = $1 AND campaign_id = $2::uuid AND claim_id IS NOT NULL`, f.tid, cv.ID).Scan(&until)
		}); err != nil {
			return err
		}
		gap = until.Sub(dl)
		<-ctx.Done()
		return ctx.Err()
	})
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/close", authz.KNTinh, false, nil)
	wantProblem(t, c, string(b), "extension.dependency_unavailable", http.StatusServiceUnavailable)
	if !sawDeadline || gap < 500*time.Millisecond {
		t.Fatalf("hạn lời gọi cap/forms phải sớm hơn hạn ý định: có hạn=%v, cách %v", sawDeadline, gap)
	}
	if n := campIntentSQL(t, f, cv.ID, sqlCampLive); n != 0 {
		t.Fatalf("ý định phải được trả ngay khi cap/forms lỗi: %d", n)
	}
	if got := f.getCampaign(cv.ID); got.State != "collecting" || f.formsPeriodStatus(*got.FormsPeriodID) == "closed" {
		t.Fatalf("đợt sau lỗi cap/forms: %s", got.State)
	}
	h.set(nil)
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/close", authz.KNTinh, false, nil)
	f.ok(c, b, &cv)
	if cv.State != "closed" || cv.Progress.PeriodStatus != "closed" {
		t.Fatalf("đóng lại sau khi cap/forms hồi: %s", b)
	}
}

// Mất ý định sau khi cap/forms đã đổi: không ai giữ → nhận lại, ghi sự kiện (nhất quán); người khác giữ → 409 interrupted,
// không ghi gì.
func TestBFCNTSCampaignPool_LostIntent(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.newCampaign("Đợt kiểm mất ý định")
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
	f.ok(c, b, &cv)

	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if err := next(ctx); err != nil || op != "due" {
			return err
		}
		campIntentSQL(t, f, cv.ID, sqlCampExpire)
		return nil
	})
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
		map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Ý định hết hạn giữa chừng"})
	f.ok(c, b, &cv)
	if cv.DueAt != "2026-10-12T03:00:00Z" || campIntentSQL(t, f, cv.ID, sqlCampLive) != 0 {
		t.Fatalf("nhận lại ý định: %s", b)
	}

	before := campIntentSQL(t, f, cv.ID, sqlCampEvents)
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if err := next(ctx); err != nil || op != "due" {
			return err
		}
		campIntentSQL(t, f, cv.ID, sqlCampHold) // thao tác khác nhận đợt sau khi ý định của yêu cầu này hết hạn
		return nil
	})
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
		map[string]any{"due_at": "2026-10-13T03:00:00Z", "reason": "Người khác giữ đợt khi ghi"})
	wantProblem(t, c, string(b), "extension.cnts_campaign_interrupted", http.StatusConflict)
	if after := campIntentSQL(t, f, cv.ID, sqlCampEvents); after != before {
		t.Fatalf("ghi sự kiện dù mất ý định: %d → %d", before, after)
	}
	if n := campIntentSQL(t, f, cv.ID, sqlCampLive); n != 1 {
		t.Fatalf("ý định của người khác phải còn: %d", n)
	}
}

// Phát hành ghi lỗi sau khi cap/forms đã tạo kỳ native (ý định mất, nháp bị sửa chen vào) → 409 state, kỳ native bị hủy bù,
// đợt vẫn Nháp, ý định được trả.
func TestBFCNTSCampaignPool_PublishCompensatesOnRecordFailure(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.newCampaign("Đợt kiểm bù phát hành")
	cancelled := 0
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		switch op {
		case "cancel":
			cancelled++
			return next(ctx)
		case "publish":
		default:
			return next(ctx)
		}
		if err := next(ctx); err != nil {
			return err
		}
		campIntentSQL(t, f, cv.ID, sqlCampExpire)
		in := map[string]any{"owner_ref": ids.New().String(), "name": "Sửa nháp chen vào khi phát hành", "goal": "x", "field": "thuy_san", "template_code": "CNTS-BM17",
			"scope_mode": "communes", "communes": []string{"X1", "X2"}, "from_date": "2026-10-01", "to_date": "2026-10-05",
			"due_at": "2026-10-09T03:00:00Z"}
		c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/draft", authz.KNTinh, false, in)
		if c != http.StatusOK {
			return errors.New("sửa nháp chen vào: " + string(b))
		}
		return nil
	})
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
	wantProblem(t, c, string(b), "extension.cnts_campaign_state", http.StatusConflict)
	if cancelled != 1 {
		t.Fatalf("phải hủy bù kỳ native đúng một lần: %d", cancelled)
	}
	rows := f.admin.query(`SELECT s.period_id::text FROM pf_forms.period_spec s WHERE s.tenant_id = $1 AND s.period_kind = 'adhoc'`, f.tid)
	if len(rows) != 1 {
		t.Fatalf("kỳ native của đợt: %v", rows)
	}
	if st := f.formsPeriodStatus(rows[0][0].(string)); st != "cancelled" {
		t.Fatalf("kỳ native sau bù: %s", st)
	}
	got := f.getCampaign(cv.ID)
	if got.State != "draft" || got.Version != 2 || got.FormsPeriodID != nil {
		t.Fatalf("đợt sau bù: state=%s version=%d period=%v", got.State, got.Version, got.FormsPeriodID)
	}
	if n := campIntentSQL(t, f, cv.ID, sqlCampLive); n != 0 {
		t.Fatalf("ý định còn giữ sau bù: %d", n)
	}
}

// ---------------------------------------------------------------------------
// Review Codex #156 [P2]: lời gọi cap/forms của yêu cầu đã MẤT ý định không được để kỳ native lệch kết quả đã cam kết của
// yêu cầu kế tiếp. Sau mọi xen kẽ dưới đây: kỳ native (trạng thái + hạn hiệu lực) = sổ đợt; không còn ý định giữ; không còn
// dấu lệch chưa đối soát.
// ---------------------------------------------------------------------------

const sqlCampDirty = `SELECT count(*)::int FROM pf_extension.cnts_campaign_intent
  WHERE tenant_id = $1 AND campaign_id = $2::uuid AND stale_gen > synced_gen`

// nativeDue: hạn HIỆU LỰC của kỳ native (sổ phụ period_due).
func (f *aggFx) nativeDue(pid string) string {
	f.t.Helper()
	rows := f.admin.query(`SELECT to_char(pf_forms.effective_due(tenant_id, id, due_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
	  FROM pf_forms.period WHERE tenant_id = $1 AND id = $2::uuid ORDER BY seq DESC LIMIT 1`, f.tid, pid)
	if len(rows) != 1 {
		f.t.Fatalf("kỳ native %s: %v", pid, rows)
	}
	return fmt.Sprint(rows[0][0])
}

// assertNativeMatchesLedger: kỳ native khớp sổ đợt (Đang thu thập → kỳ nhận bài, hạn = hạn đợt; Đã đóng → kỳ chốt); ý định
// trả; không còn dấu lệch.
func assertNativeMatchesLedger(t *testing.T, f *aggFx, id, wantState, wantDue string) {
	t.Helper()
	got := f.getCampaign(id)
	if got.State != wantState || got.DueAt != wantDue || got.FormsPeriodID == nil {
		t.Fatalf("sổ đợt: state=%s due=%s, muốn %s %s", got.State, got.DueAt, wantState, wantDue)
	}
	st := f.formsPeriodStatus(*got.FormsPeriodID)
	switch wantState {
	case "collecting":
		if st != "open" && st != "reopened" {
			t.Fatalf("đợt Đang thu thập nhưng kỳ native %s", st)
		}
		if due := f.nativeDue(*got.FormsPeriodID); due != wantDue {
			t.Fatalf("kỳ native lệch sổ đợt: hạn native %s, hạn đợt %s", due, got.DueAt)
		}
	case "closed":
		if st != "closed" {
			t.Fatalf("đợt Đã đóng nhưng kỳ native %s", st)
		}
	}
	if n := campIntentSQL(t, f, id, sqlCampLive); n != 0 {
		t.Fatalf("ý định còn giữ: %d", n)
	}
	if n := campIntentSQL(t, f, id, sqlCampDirty); n != 0 {
		t.Fatalf("còn dấu lệch chưa đối soát: %d", n)
	}
}

// isFormsConflict: lỗi cap/forms là forms.period_conflict (CAS #159 từ chối revision cũ).
func isFormsConflict(err error) bool {
	var p *problem.Problem
	return errors.As(err, &p) && p.Code == formsport.ErrPeriodConflict.Code
}

// rejectProbe ghi lại kết quả lời gọi ghi native của yêu cầu cũ và trạng thái kỳ native NGAY khi lời gọi trả về — trước mọi
// bước đánh dấu lệch / đối soát của yêu cầu cũ (mô phỏng yêu cầu cũ chết ngay sau lời gọi).
type rejectProbe struct {
	err    error
	due    string
	status string
	dirty  int
}

func (p *rejectProbe) take(t *testing.T, f *aggFx, id, pid string, err error) {
	t.Helper()
	p.err, p.due, p.status, p.dirty = err, f.nativeDue(pid), f.formsPeriodStatus(pid), campIntentSQL(t, f, id, sqlCampDirty)
}

func (p *rejectProbe) wantRejected(t *testing.T, due, status string) {
	t.Helper()
	if !isFormsConflict(p.err) {
		t.Fatalf("lời gọi native của yêu cầu cũ phải bị CAS từ chối ngay (forms.period_conflict), nhận: %v", p.err)
	}
	if p.due != due || p.status != status {
		t.Fatalf("ngay sau lời gọi bị từ chối (chưa đối soát): kỳ native %s hạn %s, muốn %s hạn %s", p.status, p.due, status, due)
	}
	if p.dirty != 0 {
		t.Fatalf("kết quả không được dựa vào dấu lệch/đối soát: dirty=%d", p.dirty)
	}
}

// Đúng xen kẽ Codex tái hiện: A gia hạn tới 12/10, mất ý định ngay trước ChangeDue; B gia hạn tới 13/10 xong (native + sổ
// đợt, 200); rồi ChangeDue của A chạy. #201: revision A ghi trong ý định là của TRƯỚC B → cap/forms từ chối ngay
// (forms.period_conflict), kỳ native giữ 13/10 mà KHÔNG cần A đánh dấu/đối soát; A → 409 interrupted.
func TestBFCNTSCampaignPool_StaleExtendDoesNotOverwriteSuccessor(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.newCampaign("Đợt kiểm gia hạn cũ ghi đè")
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
	f.ok(c, b, &cv)
	var probe rejectProbe
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if op != "due" {
			return next(ctx)
		}
		h.set(nil)
		campIntentSQL(t, f, cv.ID, sqlCampExpire)
		sc, sb := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
			map[string]any{"due_at": "2026-10-13T03:00:00Z", "reason": "Yêu cầu kế tiếp giữ kết quả hạn nộp"})
		if sc != http.StatusOK {
			return fmt.Errorf("yêu cầu kế tiếp HTTP %d %s", sc, sb)
		}
		err := next(ctx)
		probe.take(t, f, cv.ID, *cv.FormsPeriodID, err)
		return err
	})
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
		map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Yêu cầu cũ không được đè yêu cầu kế tiếp"})
	wantProblem(t, c, string(b), "extension.cnts_campaign_interrupted", http.StatusConflict)
	probe.wantRejected(t, "2026-10-13T03:00:00Z", "open")
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", "2026-10-13T03:00:00Z")
}

// Như trên nhưng hạn của yêu cầu cũ LỚN hơn (20/10 sau 13/10): đối soát phải đưa hạn native lùi về sổ đợt.
func TestBFCNTSCampaignPool_StaleLaterExtendDoesNotOverwriteSuccessor(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.newCampaign("Đợt kiểm gia hạn cũ dài hơn")
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
	f.ok(c, b, &cv)
	var probe rejectProbe
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if op != "due" {
			return next(ctx)
		}
		h.set(nil)
		campIntentSQL(t, f, cv.ID, sqlCampExpire)
		sc, sb := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
			map[string]any{"due_at": "2026-10-13T03:00:00Z", "reason": "Yêu cầu kế tiếp giữ kết quả hạn nộp"})
		if sc != http.StatusOK {
			return fmt.Errorf("yêu cầu kế tiếp HTTP %d %s", sc, sb)
		}
		err := next(ctx)
		probe.take(t, f, cv.ID, *cv.FormsPeriodID, err)
		return err
	})
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
		map[string]any{"due_at": "2026-10-20T03:00:00Z", "reason": "Yêu cầu cũ hạn dài hơn không được đè"})
	wantProblem(t, c, string(b), "extension.cnts_campaign_interrupted", http.StatusConflict)
	probe.wantRejected(t, "2026-10-13T03:00:00Z", "open")
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", "2026-10-13T03:00:00Z")
}

// Yêu cầu cũ ghi native SAU yêu cầu kế tiếp trong lúc yêu cầu kế tiếp còn GIỮ ý định: A không nhận lại được → 409
// interrupted, chỉ đánh dấu lệch; B ghi xong thấy dấu lệch → đối soát → native = hạn của B.
func TestBFCNTSCampaignPool_StaleWriteWhileSuccessorHolds(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.newCampaign("Đợt kiểm người giữ đối soát")
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
	f.ok(c, b, &cv)
	var n atomic.Int32
	bWrote, aWrote, aDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	type res struct {
		code int
		body string
		err  error
	}
	bRes := make(chan res, 1)
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if op != "due" {
			return next(ctx)
		}
		switch n.Add(1) {
		case 1: // A: mất ý định, B nhận đợt và ghi native trước; rồi A ghi native.
			campIntentSQL(t, f, cv.ID, sqlCampExpire)
			go func() {
				code, body, err := f.campaignCallCtx(context.Background(), http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", false,
					map[string]any{"due_at": "2026-10-13T03:00:00Z", "reason": "Yêu cầu kế tiếp giữ kết quả hạn nộp"})
				bRes <- res{code, body, err}
			}()
			select {
			case <-bWrote:
			case <-time.After(30 * time.Second):
				return errors.New("B không ghi native")
			}
			err := next(ctx)
			close(aWrote)
			return err
		case 2: // B: ghi native, chờ A ghi đè và A trả kết quả, rồi mới ghi sự kiện (vẫn giữ ý định).
			err := next(ctx)
			close(bWrote)
			<-aWrote
			select {
			case <-aDone:
			case <-time.After(30 * time.Second):
			}
			return err
		}
		return next(ctx)
	})
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
		map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Yêu cầu cũ không được đè yêu cầu kế tiếp"})
	close(aDone)
	wantProblem(t, c, string(b), "extension.cnts_campaign_interrupted", http.StatusConflict)
	if n := campIntentSQL(t, f, cv.ID, sqlCampDirty); n != 1 {
		t.Fatalf("A phải đánh dấu lệch cho người giữ: %d", n)
	}
	r := <-bRes
	if r.err != nil || r.code != http.StatusOK {
		t.Fatalf("B: HTTP %d err=%v %.300s", r.code, r.err, r.body)
	}
	h.set(nil)
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", "2026-10-13T03:00:00Z")
}

// Đóng đợt cũ: A đóng, mất ý định ngay trước ClosePeriod; B gia hạn tới 13/10 xong; rồi ClosePeriod của A chốt kỳ native.
// Sổ đợt vẫn Đang thu thập → đối soát mở lại kỳ native và đặt hạn 13/10.
func TestBFCNTSCampaignPool_StaleCloseDoesNotOverwriteSuccessor(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.newCampaign("Đợt kiểm đóng cũ ghi đè")
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
	f.ok(c, b, &cv)
	var probe rejectProbe
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if op != "close" {
			return next(ctx)
		}
		h.set(nil)
		campIntentSQL(t, f, cv.ID, sqlCampExpire)
		sc, sb := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
			map[string]any{"due_at": "2026-10-13T03:00:00Z", "reason": "Gia hạn xong trong lúc lệnh đóng cũ treo"})
		if sc != http.StatusOK {
			return fmt.Errorf("yêu cầu kế tiếp HTTP %d %s", sc, sb)
		}
		err := next(ctx)
		probe.take(t, f, cv.ID, *cv.FormsPeriodID, err)
		return err
	})
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/close", authz.KNTinh, false, nil)
	wantProblem(t, c, string(b), "extension.cnts_campaign_interrupted", http.StatusConflict)
	probe.wantRejected(t, "2026-10-13T03:00:00Z", "open")
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", "2026-10-13T03:00:00Z")
	// Ảnh chụp đóng băng / outbox chốt kỳ của lệnh đóng cũ không được sinh.
	if n := f.closedArtifacts(*cv.FormsPeriodID); n != "0/0" {
		t.Fatalf("lệnh đóng cũ bị từ chối vẫn sinh ảnh chụp/outbox: %s", n)
	}
}

// closedArtifacts: "<số ảnh chụp chốt>/<số outbox chốt kỳ>" của kỳ native pid.
func (f *aggFx) closedArtifacts(pid string) string {
	f.t.Helper()
	rows := f.admin.query(`SELECT (SELECT count(*) FROM pf_forms.close_snapshot WHERE tenant_id = $1 AND period_ref = $2::text::uuid)::text || '/' ||
	    (SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = $3::text AND payload->>'period_ref' = $2::text)::text`,
		f.tid, pid, formsport.EventPeriodClosed)
	return fmt.Sprint(rows[0][0])
}

// Mở lại đợt cũ: đợt Đã đóng; A mở lại (hạn 15/10), mất ý định ngay trước ReopenPeriod; B mở lại (16/10) rồi C đóng đợt, cả hai
// xong; rồi ReopenPeriod + ChangeDue của A mở kỳ native. Sổ đợt Đã đóng → đối soát chốt lại kỳ native.
func TestBFCNTSCampaignPool_StaleReopenDoesNotOverwriteSuccessor(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.newCampaign("Đợt kiểm mở lại cũ ghi đè")
	for _, p := range []string{"/publish", "/close"} {
		c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+p, authz.KNTinh, false, nil)
		f.ok(c, b, &cv)
	}
	var probe rejectProbe
	dueCalls := 0
	countDue := func(ctx context.Context, op string, next func(context.Context) error) error {
		if op == "due" {
			dueCalls++
		}
		return next(ctx)
	}
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if op != "reopen" {
			return next(ctx)
		}
		h.set(nil)
		defer h.set(countDue) // sau lời gọi bị từ chối: đếm lời gọi đổi hạn còn lại của yêu cầu cũ
		campIntentSQL(t, f, cv.ID, sqlCampExpire)
		sc, sb := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/reopen", authz.KNTinh, true,
			map[string]any{"due_at": "2026-10-16T03:00:00Z", "reason": "Mở lại xong trong lúc lệnh mở lại cũ treo"})
		if sc != http.StatusOK {
			return fmt.Errorf("mở lại kế tiếp HTTP %d %s", sc, sb)
		}
		sc, sb = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/close", authz.KNTinh, false, nil)
		if sc != http.StatusOK {
			return fmt.Errorf("đóng kế tiếp HTTP %d %s", sc, sb)
		}
		err := next(ctx)
		probe.take(t, f, cv.ID, *cv.FormsPeriodID, err)
		return err
	})
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/reopen", authz.KNTinh, true,
		map[string]any{"due_at": "2026-10-15T03:00:00Z", "reason": "Lệnh mở lại cũ không được đè kết quả"})
	wantProblem(t, c, string(b), "extension.cnts_campaign_interrupted", http.StatusConflict)
	probe.wantRejected(t, "2026-10-16T03:00:00Z", "closed")
	if dueCalls != 0 {
		t.Fatalf("mở lại cũ bị từ chối không được gọi bước đổi hạn: %d", dueCalls)
	}
	assertNativeMatchesLedger(t, f, cv.ID, "closed", "2026-10-16T03:00:00Z")
}
