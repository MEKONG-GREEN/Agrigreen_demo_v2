package app

import (
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
)

func TestAppraisalOverrideCannotWidenAssignment_W1126(t *testing.T) {
	actor := uuid.MustParse("00000000-0000-7000-8000-000000000001")
	other := uuid.MustParse("00000000-0000-7000-8000-000000000002")
	mine := domain.AppraisalMember{AccountRef: &actor, ManagementUnit: "tt", Unit: "tt", SummaryKind: "MS05"}
	foreign := mine
	foreign.AccountRef, foreign.ManagementUnit, foreign.Unit = &other, "cnts", "cnts"
	c := appraisalActor{ref: actor, group: domain.GroupHCTH, scope: domain.ScopeSelf}
	if !c.canRead(mine) || c.canRead(foreign) {
		t.Fatal("override HCTH về self không được bỏ qua bởi vai trung tâm")
	}
	c = appraisalActor{ref: actor, group: domain.GroupManager, manager: true, unit: "tt", scope: domain.ScopeAll}
	if c.canRead(foreign) {
		t.Fatal("scope all không cấp quyền quản lý ngoài phân công HS37")
	}
	c.scope = domain.ScopeSelf
	assigned := mine
	assigned.AccountRef = &other
	if c.canRead(assigned) {
		t.Fatal("override self không cho quản lý phiếu của người khác trong đơn vị")
	}
	c.scope = domain.ScopeUnit
	if !c.canRead(assigned) {
		t.Fatal("lãnh đạo có scope unit mất phiếu được giao quản lý")
	}
	if err := appraisalActionPermission(c, AppraisalItem{AppraisalEntry: domain.AppraisalEntry{Member: assigned}}, domain.AppraisalEntryChange{Action: "self_submit"}); err == nil {
		t.Fatal("lãnh đạo không được tự nộp thay nếu thiếu bước scan proxy")
	}
	c.group = domain.GroupXa
	if c.canRead(assigned) || domain.CanManageAppraisal(appraisalAuthority(c), assigned) {
		t.Fatal("cờ Manager không cấp quyền quản lý cho vai Cấp Xã")
	}
}

func TestAppraisalExtensionAtomicAndLate_W1126(t *testing.T) {
	self := time.Date(2026, 10, 5, 16, 0, 0, 0, time.UTC)
	p := AppraisalPeriod{SelfDueAt: self, ManagerDueAt: self.Add(10 * 24 * time.Hour), SummaryDueAt: self.Add(20 * 24 * time.Hour)}
	before := p
	if extendAppraisalPeriod(&p, AppraisalPeriodChange{SelfDueAt: "2026-10-25T16:00:00Z"}) == nil || p.SelfDueAt != before.SelfDueAt {
		t.Fatal("gia hạn sai thứ tự đã làm đổi snapshot đầu vào")
	}
	item := AppraisalItem{AppraisalEntry: domain.AppraisalEntry{Status: domain.AppraisalSelfSubmitted, SelfSubmittedAt: &self}}
	appraisalLate(&item, p, self.Add(30*24*time.Hour))
	if item.SelfLate || !item.ManagerLate {
		t.Fatal("nộp đúng hạn tự chấm không bị gắn muộn theo ngày truy cập; bước quản lý còn thiếu phải muộn")
	}
}

func TestAppraisalKeyBounded_W1126(t *testing.T) {
	for _, raw := range []string{"", "short", "abcd\r\nefg", "khóa-dánh-giá"} {
		if appraisalKey(raw) == nil {
			t.Fatalf("accepted unsafe key %q", raw)
		}
	}
	if err := appraisalKey("appraisal-request-01"); err != nil {
		t.Fatal(err)
	}
}

func TestAppraisalQuarterReferencesRespectHistoricAssignment_W1126(t *testing.T) {
	actor, other := uuid.New(), uuid.New()
	current := domain.AppraisalMember{AccountRef: &other, Unit: "tt", ManagementUnit: "tt", SummaryKind: "MS05"}
	foreign := current
	foreign.Unit = "cnts"
	foreign.ManagementUnit = "cnts"
	own := current
	own.AccountRef = &actor
	entry := domain.AppraisalEntry{Member: current, QuarterResults: []domain.AppraisalQuarterSnapshot{{Quarter: 1, Member: current}, {Quarter: 2, Member: foreign}, {Quarter: 3, Member: own}}}
	payload, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	c := appraisalActor{ref: actor, group: domain.GroupManager, manager: true, unit: "tt", scope: domain.ScopeUnit}
	item, err := appraisalItem(store.AppraisalEvent{Payload: payload}, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.QuarterResults) != 2 || item.QuarterResults[0].Quarter != 1 || item.QuarterResults[1].Quarter != 3 {
		t.Fatal("lộ quý khác phân công lịch sử")
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"management_unit":"cnts"`) {
		t.Fatal("payload quý nội bộ lọt vào phản hồi")
	}
	c.scope = domain.ScopeSelf
	item, err = appraisalItem(store.AppraisalEvent{Payload: payload}, c)
	if err != nil || len(item.QuarterResults) != 1 || item.QuarterResults[0].Quarter != 3 {
		t.Fatal("self thấy tham chiếu quý người khác")
	}
}
