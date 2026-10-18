package notify_test

import (
	"strings"
	"testing"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// D24 (W10.05): pf.accounts.user.dormant_warning → thông báo cá nhân KHẨN cho chính tài khoản sắp bị khóa, kèm email;
// người khác không nhận; giao lặp không tạo trùng.
func TestDormantWarningToAccountItself_D24(t *testing.T) {
	f := newFixture(t)
	sc := authz.Scope{Units: []string{ward1}}
	me := f.user(sc, authz.KNXa)
	other := f.user(sc, authz.KNXa)
	f.emails.set(me.actor, "can.bo.xa@mekong.vn")
	env := f.event(f.tenant, accport.EventUserDormantWarning, accport.DormantWarningV1{UserRef: me.actor, OrgRef: me.org,
		LastActivityAt: "2026-09-01T02:00:00Z", DisableAt: "2026-10-16T02:00:00Z", IdleDays: 38, EventTime: "2026-10-09T02:00:00Z", Seq: 3})
	if !f.process(port.ConsumerAccounts, env) || f.process(port.ConsumerAccounts, env) {
		t.Fatal("lần giao đầu xử lý, giao lặp bị inbox bỏ qua")
	}
	in := me.inbox()
	if len(in) != 1 || in[0]["kind"] != port.KindAccountDormant || in[0]["severity"] != port.SeverityUrgent ||
		in[0]["deep_link"] != "/login" || !strings.Contains(in[0]["body"].(string), "38 ngày") ||
		!strings.Contains(in[0]["body"].(string), "16/10/2026") {
		t.Fatalf("tài khoản nhận một báo trước: %v", in)
	}
	if n := other.inbox(); len(n) != 0 {
		t.Fatalf("người khác không nhận: %v", n)
	}
	if err := f.flushEmail(f.tenant); err != nil {
		t.Fatal(err)
	}
	if m := f.mailer.to("can.bo.xa@mekong.vn"); len(m) != 1 || !strings.Contains(m[0].subject, "sắp bị khóa") {
		t.Fatalf("một email báo trước: %+v", m)
	}
}
