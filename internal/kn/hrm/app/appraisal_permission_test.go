package app

import (
	"testing"

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
