package domain

import "testing"

// FN17 DG04–08: lãnh đạo nhập đề xuất từ đầu, không cần phiếu điểm hay scan.
func TestAppraisalV4ManagerEntersProposalDirectly281(t *testing.T) {
	entry, _, manager, _ := appraisalEntryExample()
	next, err := ChangeAppraisalEntry(entry, AppraisalStepLocks{}, manager,
		AppraisalEntryChange{Action: "manager_submit", SelfRating: "HTTNV", CollectiveRating: "HTNV", Note: "Kết quả họp đơn vị"})
	if err != nil {
		t.Fatalf("FN17 phải lưu hai mức đề xuất trực tiếp khi chưa nhập: %v", err)
	}
	if next.Status != AppraisalEntered || next.SelfRating != "HTTNV" || next.CollectiveRating != "HTNV" {
		t.Fatalf("đề xuất không đủ DG04/05: %+v", next)
	}
	if next.Self.Total != "" || next.Manager.Total != "" {
		t.Fatal("đề xuất mới có điểm")
	}
}
