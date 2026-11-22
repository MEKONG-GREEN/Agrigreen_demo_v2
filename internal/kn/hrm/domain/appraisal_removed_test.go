package domain

import (
	"github.com/google/uuid"
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
			if _, err := ChangeAppraisalEntry(entry, appraisalExample(), AppraisalStepLocks{}, actor,
				AppraisalEntryChange{Action: action, Answers: appraisalAnswers(7000)}); err == nil {
				t.Errorf("vai %s còn ghi được %s", group, action)
			}
		}
	}
	proof := uuid.New()
	manager.ProofVerified = true
	if _, err := ChangeAppraisalEntry(entry, appraisalExample(), AppraisalStepLocks{}, manager,
		AppraisalEntryChange{Action: "proxy_self", Answers: appraisalAnswers(7000), ProxyDocument: &proof}); err != nil {
		t.Fatalf("nhập thay của lãnh đạo có chứng từ còn thuộc gói HC-2: %v", err)
	}
}
