package domain

import (
	"testing"
)

func TestAppraisalRemovedSelfCapabilityHC0(t *testing.T) {
	entry, self, manager, center := appraisalEntryExample()
	for _, group := range []string{GroupXa, GroupTinh, GroupTW} {
		if EffectiveMatrix(group, Matrix{PermAppraisal: ScopeAll}, Matrix{PermAppraisal: ScopeAll})[PermAppraisal] != ScopeNone {
			t.Errorf("override khôi phục assessment của %s", group)
		}
		actor := self
		actor.Group = group
		actor.Manager = true
		actor.Unit = entry.Member.ManagementUnit
		if CanReadAppraisal(actor, entry.Member) {
			t.Errorf("vai %s còn đọc được tự đánh giá đã gỡ", group)
		}
	}
	if !CanReadAppraisal(manager, entry.Member) || !CanReadAppraisal(center, entry.Member) {
		t.Fatal("gỡ tự đánh giá làm mất phạm vi lãnh đạo/HCTH")
	}
	// Dù chủ thể có vai HCTH/Admin, đường ghi tự chấm cũ cũng đã bị bỏ.
	for _, group := range []string{GroupAdmin, GroupHCTH, GroupManager, GroupXa} {
		actor := self
		actor.Group, actor.Manager, actor.Unit = group, true, entry.Member.ManagementUnit
		for _, action := range []string{"self_draft", "self_submit"} {
			if _, err := ChangeAppraisalEntry(entry, AppraisalStepLocks{}, actor,
				AppraisalEntryChange{Action: action}); err == nil {
				t.Errorf("vai %s còn ghi được %s", group, action)
			}
		}
	}
	if _, err := ChangeAppraisalEntry(entry, AppraisalStepLocks{}, manager, AppraisalEntryChange{Action: "proxy_self"}); err == nil {
		t.Fatal("HC-2 vẫn cho nhập phiếu giấy")
	}
}
