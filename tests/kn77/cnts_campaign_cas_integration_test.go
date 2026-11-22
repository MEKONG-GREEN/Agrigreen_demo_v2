package kn77_test

// BF-CNTS-CAMPAIGN-CAS-CONSUMER (#201): consumer end-to-end của port CAS #159 (formsport.ConditionalPeriodControl) ở đợt điều
// tra CNTS. Mọi đường ghi kỳ native của đợt (gia hạn, đóng, mở lại + đổi hạn, đối soát, hủy bù phát hành) dùng revision ghi
// bền trong ý định của đợt; yêu cầu đã mất ý định bị cap/forms từ chối ngay. Dùng fixture/hook của
// cnts_campaign_pool_integration_test.go (newCampaignPoolFx, campHooks, rejectProbe).

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

const campOrigDue = "2026-10-09T03:00:00Z"

func (f *aggFx) publishCampaign(name string) campaignView {
	f.t.Helper()
	cv := f.newCampaign(name)
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
	f.ok(c, b, &cv)
	return cv
}

// eventCount: số sự kiện action của đợt.
func (f *aggFx) eventCount(id, action string) int {
	f.t.Helper()
	rows := f.admin.query(`SELECT count(*)::text FROM pf_extension.cnts_campaign_event WHERE tenant_id = $1 AND campaign_id = $2::uuid AND action = $3`,
		f.tid, id, action)
	var n int
	if _, err := fmt.Sscan(fmt.Sprint(rows[0][0]), &n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// adhocPeriod: kỳ native duy nhất của đợt (phát hành).
func (f *aggFx) adhocPeriod() string {
	f.t.Helper()
	rows := f.admin.query(`SELECT s.period_id::text FROM pf_forms.period_spec s WHERE s.tenant_id = $1 AND s.period_kind = 'adhoc'`, f.tid)
	if len(rows) != 1 {
		f.t.Fatalf("kỳ native của đợt: %v", rows)
	}
	return rows[0][0].(string)
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Due ABA: A gia hạn (revision ghi trong ý định), mất ý định trước ChangeDue; B gia hạn 13/10 xong; một người ghi khác (cổng
// legacy) đưa hạn native về đúng hạn ban đầu 09/10. Hạn native giống lúc A chụp nhưng head period_due đã tiến → A bị từ chối.
func TestBFCNTSCampaignCAS_DueABA(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.publishCampaign("Đợt kiểm ABA hạn nộp")
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
		if err := h.legacy.ChangeDue(ctx, "kn_cnts", uuid.MustParse(*cv.FormsPeriodID), mustTime(t, campOrigDue), "ABA về hạn cũ",
			"aba-"+ids.New().String()); err != nil {
			return err
		}
		err := next(ctx)
		probe.take(t, f, cv.ID, *cv.FormsPeriodID, err)
		return err
	})
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
		map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Yêu cầu cũ gặp ABA hạn nộp"})
	wantProblem(t, c, string(b), "extension.cnts_campaign_interrupted", http.StatusConflict)
	probe.wantRejected(t, campOrigDue, "open")
	if n := f.eventCount(cv.ID, "extended"); n != 1 {
		t.Fatalf("chỉ sự kiện gia hạn của B: %d", n)
	}
	// Người ghi legacy làm native lệch sổ đợt: A (bị từ chối) đánh dấu lệch, nhận lại đợt khi B đã trả → đối soát về sổ đợt.
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", "2026-10-13T03:00:00Z")
}

// Close/reopen ABA: A gia hạn, mất ý định trước ChangeDue; B đóng đợt rồi C (Lãnh đạo) mở lại hạn 16/10 — sổ đợt lại Đang thu
// thập như lúc A chụp. A bị từ chối; hạn native = 16/10.
func TestBFCNTSCampaignCAS_CloseReopenABA(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.publishCampaign("Đợt kiểm ABA đóng mở lại")
	var probe rejectProbe
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if op != "due" {
			return next(ctx)
		}
		h.set(nil)
		campIntentSQL(t, f, cv.ID, sqlCampExpire)
		if sc, sb := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/close", authz.KNTinh, false, nil); sc != http.StatusOK {
			return fmt.Errorf("đóng kế tiếp HTTP %d %s", sc, sb)
		}
		if sc, sb := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/reopen", authz.KNTinh, true,
			map[string]any{"due_at": "2026-10-16T03:00:00Z", "reason": "Mở lại kế tiếp sau khi đóng đợt"}); sc != http.StatusOK {
			return fmt.Errorf("mở lại kế tiếp HTTP %d %s", sc, sb)
		}
		err := next(ctx)
		probe.take(t, f, cv.ID, *cv.FormsPeriodID, err)
		return err
	})
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
		map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Yêu cầu cũ gặp ABA đóng mở lại"})
	wantProblem(t, c, string(b), "extension.cnts_campaign_interrupted", http.StatusConflict)
	probe.wantRejected(t, "2026-10-16T03:00:00Z", "reopened")
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", "2026-10-16T03:00:00Z")
}

// Chuỗi Reopen → ChangeDue có người chen giữa: A mở lại (ReopenPeriodCAS xong, revision áp dụng ghi vào ý định), mất ý định
// trước ChangeDue; B mở lại hạn 16/10 xong (kỳ native đã nhận bài → B chỉ đổi hạn). ChangeDue của A dùng revision áp dụng
// của CHÍNH bước mở lại của A → bị từ chối; hạn native = 16/10.
func TestBFCNTSCampaignCAS_ReopenChainSuccessorBetween(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.publishCampaign("Đợt kiểm chuỗi mở lại")
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/close", authz.KNTinh, false, nil)
	f.ok(c, b, &cv)
	var probe rejectProbe
	reopened := false
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		switch op {
		case "reopen":
			reopened = true
			return next(ctx)
		case "due":
		default:
			return next(ctx)
		}
		h.set(nil)
		if !reopened {
			return errors.New("A phải mở lại trước khi đổi hạn")
		}
		campIntentSQL(t, f, cv.ID, sqlCampExpire)
		if sc, sb := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/reopen", authz.KNTinh, true,
			map[string]any{"due_at": "2026-10-16T03:00:00Z", "reason": "Mở lại kế tiếp chen giữa chuỗi cũ"}); sc != http.StatusOK {
			return fmt.Errorf("mở lại kế tiếp HTTP %d %s", sc, sb)
		}
		err := next(ctx)
		probe.take(t, f, cv.ID, *cv.FormsPeriodID, err)
		return err
	})
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/reopen", authz.KNTinh, true,
		map[string]any{"due_at": "2026-10-15T03:00:00Z", "reason": "Chuỗi mở lại cũ không được đè hạn"})
	wantProblem(t, c, string(b), "extension.cnts_campaign_interrupted", http.StatusConflict)
	probe.wantRejected(t, "2026-10-16T03:00:00Z", "reopened")
	if n := f.eventCount(cv.ID, "reopened"); n != 1 {
		t.Fatalf("chỉ sự kiện mở lại của B: %d", n)
	}
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", "2026-10-16T03:00:00Z")
}

// Ràng buộc ý định giữa hai bước: A mở lại xong ở native rồi mất ý định (người khác đang giữ) → KHÔNG gọi ChangeDue; chỉ đánh
// dấu lệch. Người kế tiếp (sau khi ý định kia hết hạn) mở lại hạn 17/10 → kỳ native khớp sổ đợt.
func TestBFCNTSCampaignCAS_ReopenChainStopsWhenIntentLost(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.publishCampaign("Đợt kiểm ràng buộc giữa chuỗi")
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/close", authz.KNTinh, false, nil)
	f.ok(c, b, &cv)
	dueCalls := 0
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		switch op {
		case "due":
			dueCalls++
		case "reopen":
			if err := next(ctx); err != nil {
				return err
			}
			campIntentSQL(t, f, cv.ID, sqlCampHold) // thao tác khác nhận đợt ngay sau bước mở lại của A
			return nil
		}
		return next(ctx)
	})
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/reopen", authz.KNTinh, true,
		map[string]any{"due_at": "2026-10-15T03:00:00Z", "reason": "Chuỗi mở lại mất ý định giữa hai bước"})
	wantProblem(t, c, string(b), "extension.cnts_campaign_interrupted", http.StatusConflict)
	if dueCalls != 0 {
		t.Fatalf("mất ý định giữa chuỗi vẫn gọi ChangeDue: %d", dueCalls)
	}
	if n := campIntentSQL(t, f, cv.ID, sqlCampDirty); n != 1 {
		t.Fatalf("bước mở lại đã ghi native mà sổ đợt chưa ghi → phải đánh dấu lệch: %d", n)
	}
	if n := f.eventCount(cv.ID, "reopened"); n != 0 {
		t.Fatalf("không được ghi sự kiện mở lại: %d", n)
	}
	h.set(nil)
	campIntentSQL(t, f, cv.ID, sqlCampExpire)
	c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/reopen", authz.KNTinh, true,
		map[string]any{"due_at": "2026-10-17T03:00:00Z", "reason": "Mở lại lại sau khi ý định kia hết hạn"})
	f.ok(c, b, nil)
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", "2026-10-17T03:00:00Z")
}

// Mất ACK sau khi cap/forms đã commit: không mặc nhận rollback hay thành công — vẫn giữ ý định → đối soát kỳ native về sổ đợt
// (hạn ban đầu), 503. Gọi lặp CÙNG Idempotency-Key: ý định mới, revision mới → gia hạn xong; lặp tiếp → không ghi thêm.
func TestBFCNTSCampaignCAS_LostAckThenRetrySameIntent(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.publishCampaign("Đợt kiểm mất ACK")
	first := true
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if op != "due" || !first {
			return next(ctx)
		}
		first = false
		if err := next(ctx); err != nil {
			return err
		}
		return errors.New("mất kết nối sau commit (ACK không về)")
	})
	key := "pool-ack-" + ids.New().String()
	body := map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Gia hạn gặp mất ACK sau commit"}
	path := "/v1/extension/cnts/campaigns/" + cv.ID + "/extend"
	code, out, err := f.campaignCallKey(context.Background(), http.MethodPost, path, false, key, body)
	if err != nil {
		t.Fatal(err)
	}
	wantProblem(t, code, out, "extension.dependency_unavailable", http.StatusServiceUnavailable)
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", campOrigDue)
	for i := 0; i < 2; i++ {
		code, out, err = f.campaignCallKey(context.Background(), http.MethodPost, path, false, key, body)
		if err != nil || code != http.StatusOK {
			t.Fatalf("gọi lặp cùng ý định lần %d: HTTP %d err=%v %.300s", i+1, code, err, out)
		}
	}
	if n := f.eventCount(cv.ID, "extended"); n != 1 {
		t.Fatalf("gọi lặp cùng ý định phải đúng một sự kiện gia hạn: %d", n)
	}
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", "2026-10-12T03:00:00Z")
}

// Hủy yêu cầu (context cancel) ngay sau khi cap/forms đã ghi: đối soát vẫn chạy (không theo ctx đã hủy) → kỳ native về sổ
// đợt, ý định trả, không sự kiện.
func TestBFCNTSCampaignCAS_ContextCancelAfterNativeWrite(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.publishCampaign("Đợt kiểm hủy yêu cầu")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.set(func(c context.Context, op string, next func(context.Context) error) error {
		if op != "due" {
			return next(c)
		}
		h.set(nil)
		if err := next(c); err != nil {
			return err
		}
		cancel()
		return c.Err()
	})
	_, _, _ = f.campaignCallKey(ctx, http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", false, "pool-cancel-"+ids.New().String(),
		map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Yêu cầu bị hủy sau khi native đã ghi"})
	if n := f.eventCount(cv.ID, "extended"); n != 0 {
		t.Fatalf("yêu cầu đã hủy không được ghi sự kiện: %d", n)
	}
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", campOrigDue)
}

// Đối soát cũ: A mất ACK → đối soát (CAS, revision chụp dưới quyền giữ); A mất ý định trước lời gọi đối soát, B gia hạn 13/10
// xong; lời gọi đối soát của A bị từ chối ngay (không đưa hạn về 09/10 đè B).
func TestBFCNTSCampaignCAS_StaleSettleRejected(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.publishCampaign("Đợt kiểm đối soát cũ")
	var probe rejectProbe
	calls := 0
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if op != "due" {
			return next(ctx)
		}
		calls++
		switch calls {
		case 1: // gia hạn của A: ghi xong, mất ACK
			if err := next(ctx); err != nil {
				return err
			}
			return errors.New("mất ACK sau commit")
		case 2: // lời gọi đối soát của A
			h.set(nil)
			campIntentSQL(t, f, cv.ID, sqlCampExpire)
			if sc, sb := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
				map[string]any{"due_at": "2026-10-13T03:00:00Z", "reason": "Gia hạn kế tiếp khi đối soát cũ treo"}); sc != http.StatusOK {
				return fmt.Errorf("yêu cầu kế tiếp HTTP %d %s", sc, sb)
			}
			err := next(ctx)
			probe.err, probe.due, probe.status = err, f.nativeDue(*cv.FormsPeriodID), f.formsPeriodStatus(*cv.FormsPeriodID)
			return err
		}
		return next(ctx)
	})
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
		map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Gia hạn cũ mất ACK rồi đối soát"})
	wantProblem(t, c, string(b), "extension.dependency_unavailable", http.StatusServiceUnavailable)
	if calls != 2 {
		t.Fatalf("phải có lời gọi đối soát của A: %d", calls)
	}
	probe.wantRejected(t, "2026-10-13T03:00:00Z", "open")
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", "2026-10-13T03:00:00Z")
}

// Hủy bù phát hành cũ: ghi phát hành lỗi (nháp bị sửa chen vào) → hủy bù là CAS; một người ghi khác đổi kỳ native ngay trước
// lời gọi hủy → hủy bù bị từ chối, kỳ native không bị hủy theo ảnh cũ.
func TestBFCNTSCampaignCAS_StaleUndoRejected(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.newCampaign("Đợt kiểm hủy bù cũ")
	var cancelErr error
	cancels := 0
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		switch op {
		case "publish":
			if err := next(ctx); err != nil {
				return err
			}
			campIntentSQL(t, f, cv.ID, sqlCampExpire)
			in := map[string]any{"owner_ref": ids.New().String(), "name": "Sửa nháp chen vào khi phát hành", "goal": "x", "field": "thuy_san", "template_code": "CNTS-BM17",
				"scope_mode": "communes", "communes": []string{"X1", "X2"}, "from_date": "2026-10-01", "to_date": "2026-10-05",
				"due_at": campOrigDue}
			if c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/draft", authz.KNTinh, false, in); c != http.StatusOK {
				return errors.New("sửa nháp chen vào: " + string(b))
			}
			return nil
		case "cancel":
			cancels++
			if err := h.legacy.ChangeDue(ctx, "kn_cnts", uuid.MustParse(f.adhocPeriod()), mustTime(t, "2026-10-11T03:00:00Z"), "Người ghi khác",
				"undo-"+ids.New().String()); err != nil {
				return err
			}
			cancelErr = next(ctx)
			return cancelErr
		}
		return next(ctx)
	})
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
	wantProblem(t, c, string(b), "extension.cnts_campaign_state", http.StatusConflict)
	if cancels != 1 || !isFormsConflict(cancelErr) {
		t.Fatalf("hủy bù phải là CAS và bị từ chối khi kỳ native đã đổi: lần=%d err=%v", cancels, cancelErr)
	}
	if st := f.formsPeriodStatus(f.adhocPeriod()); st != "open" {
		t.Fatalf("kỳ native không được hủy theo ảnh cũ: %s", st)
	}
	if got := f.getCampaign(cv.ID); got.State != "draft" {
		t.Fatalf("đợt sau phát hành lỗi: %s", got.State)
	}
	if n := campIntentSQL(t, f, cv.ID, sqlCampLive); n != 0 {
		t.Fatalf("ý định còn giữ: %d", n)
	}
}

// Revision sai định danh (tenant/kỳ/profile khác, seq 0) từ cổng CAS → fail closed: 503, không gọi lời ghi native, không sự
// kiện, ý định trả. Vai không ghi (Admin) → 403 trước mọi lời gọi cap/forms.
func TestBFCNTSCampaignCAS_RevisionIdentityAndRole(t *testing.T) {
	f, h := newCampaignPoolFx(t, nil, 0)
	cv := f.publishCampaign("Đợt kiểm định danh revision")
	writes := 0
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if op == "due" || op == "close" || op == "reopen" || op == "cancel" {
			writes++
		}
		return next(ctx)
	})
	for name, tamper := range map[string]func(*formsport.PeriodControlSnapshot){
		"tenant":  func(s *formsport.PeriodControlSnapshot) { s.Revision.TenantID = ids.New() },
		"period":  func(s *formsport.PeriodControlSnapshot) { s.Revision.PeriodID = ids.New() },
		"profile": func(s *formsport.PeriodControlSnapshot) { s.Revision.Profile = "kn_khtc" },
		"zero":    func(s *formsport.PeriodControlSnapshot) { s.Revision.EventSeq = 0 },
	} {
		h.mu.Lock()
		h.tamper = tamper
		h.mu.Unlock()
		c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
			map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Revision sai định danh " + name})
		wantProblem(t, c, string(b), "extension.dependency_unavailable", http.StatusServiceUnavailable)
	}
	h.mu.Lock()
	h.tamper = nil
	h.mu.Unlock()
	snaps := 0
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		if op == "snapshot" {
			snaps++
		}
		return next(ctx)
	})
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/close", authz.KNAdmin, false, nil)
	if c != http.StatusForbidden {
		t.Fatalf("Admin đóng đợt: HTTP %d %s", c, b)
	}
	if writes != 0 || snaps != 0 {
		t.Fatalf("lời gọi cap/forms khi revision sai / vai không ghi: ghi=%d chụp=%d", writes, snaps)
	}
	if n := f.eventCount(cv.ID, "extended"); n != 0 {
		t.Fatalf("không được ghi sự kiện: %d", n)
	}
	h.set(nil)
	assertNativeMatchesLedger(t, f, cv.ID, "collecting", campOrigDue)
}

// Gốc lắp ráp không nối cổng CAS: mọi thao tác ghi kỳ native trả 503 TRƯỚC khi nhận ý định / gọi cap/forms — không quay về
// lời gọi vô điều kiện.
func TestBFCNTSCampaignCAS_FailClosedWithoutPort(t *testing.T) {
	f, h := newCampaignPoolFxCond(t, nil, 0, false)
	cv := f.newCampaign("Đợt kiểm thiếu cổng CAS")
	calls := 0
	h.set(func(ctx context.Context, op string, next func(context.Context) error) error {
		calls++
		return next(ctx)
	})
	c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
	wantProblem(t, c, string(b), "extension.dependency_unavailable", http.StatusServiceUnavailable)
	if calls != 0 {
		t.Fatalf("thiếu cổng CAS vẫn gọi cap/forms: %d", calls)
	}
	if got := f.getCampaign(cv.ID); got.State != "draft" || got.FormsPeriodID != nil {
		t.Fatalf("đợt phải còn Nháp: %s", got.State)
	}
	if n := campIntentSQL(t, f, cv.ID, sqlCampLive); n != 0 {
		t.Fatalf("không được nhận ý định: %d", n)
	}
}
