package domain

import (
	"testing"

	"github.com/google/uuid"
)

func appraisalEntryExample() (AppraisalEntry, AppraisalAuthority, AppraisalAuthority, AppraisalAuthority) {
	selfID := uuid.MustParse("00000000-0000-7000-8000-000000000001")
	managerID := uuid.MustParse("00000000-0000-7000-8000-000000000002")
	centerID := uuid.MustParse("00000000-0000-7000-8000-000000000003")
	e := AppraisalEntry{Member: AppraisalMember{StaffID: uuid.MustParse("00000000-0000-7000-8000-000000000011"), AccountRef: &selfID,
		Code: "XA-00001", Type: StaffCanBoXa, Group: AppraisalCommune, Unit: "xa_mhh", ManagementUnit: "tt", SummaryKind: "MS05"}, Status: AppraisalNotStarted}
	return e, AppraisalAuthority{Actor: selfID, Group: GroupXa}, AppraisalAuthority{Actor: managerID, Group: GroupManager, Unit: "tt", Manager: true}, AppraisalAuthority{Actor: centerID, Group: GroupHCTH}
}

func TestAppraisalScopeUsesSubjectAndHS37_W1126(t *testing.T) {
	e, self, manager, center := appraisalEntryExample()
	if CanReadAppraisal(self, e.Member) || !CanReadAppraisal(manager, e.Member) || !CanReadAppraisal(center, e.Member) {
		t.Fatal("quyền v4.0: chỉ lãnh đạo được phân công/HCTH xem đánh giá")
	}
	wrong := manager
	wrong.Unit = "cnts"
	if CanReadAppraisal(wrong, e.Member) {
		t.Fatal("lãnh đạo khác đơn vị xem được cán bộ xã")
	}
	wrong = self
	wrong.Actor = manager.Actor
	if CanReadAppraisal(wrong, e.Member) {
		t.Fatal("cấp xã khác xem được phiếu")
	}
	wrong = center
	wrong.Group = GroupTW
	if CanReadAppraisal(wrong, e.Member) {
		t.Fatal("TW xem phiếu")
	}
	wrong = self
	wrong.Group = ""
	if CanReadAppraisal(wrong, e.Member) {
		t.Fatal("ngữ cảnh không có vai vẫn xem được")
	}
	e.Member.SummaryKind = "MS06"
	if CanReadAppraisal(manager, e.Member) {
		t.Fatal("lãnh đạo phòng xem/chấm MS06 của người khác")
	}
}

// HC-2 thay assertions chấm điểm cũ bằng DG04–10; bộ decode/ScoreAppraisal lịch sử vẫn giữ ở appraisal_test.go.
func TestAppraisalV4StatesCorrectionsAndClose281(t *testing.T) {
	e, self, manager, center := appraisalEntryExample()
	save := AppraisalEntryChange{Action: "manager_submit", SelfRating: "HTXSNV", CollectiveRating: "HTTNV", Reason: "Căn cứ kết quả hoàn thành nhiệm vụ", PersonnelProposal: "Tiếp tục công tác", Note: "Biên bản đơn vị"}
	for _, actor := range []AppraisalAuthority{self, {Actor: manager.Actor, Group: GroupManager, Unit: "khac", Manager: true}, {Actor: manager.Actor, Group: GroupManager, Unit: "tt", Manager: true, ReadOnly: true}} {
		if _, err := ChangeAppraisalEntry(e, AppraisalStepLocks{}, actor, save); err == nil {
			t.Fatal("caller ngoài quyền vẫn nhập đề xuất")
		}
	}
	entered, err := ChangeAppraisalEntry(e, AppraisalStepLocks{}, manager, save)
	if err != nil || entered.Status != AppraisalEntered || entered.SelfRating != "HTXSNV" || entered.PersonnelProposal != save.PersonnelProposal {
		t.Fatalf("DG04–08: %+v %v", entered, err)
	}
	if e.Status != AppraisalNotStarted || e.SelfRating != "" {
		t.Fatal("lưu sửa phiên bản nguồn")
	}
	sent, err := ChangeAppraisalEntry(entered, AppraisalStepLocks{}, manager, AppraisalEntryChange{Action: "send"})
	if err != nil || sent.Status != AppraisalSent {
		t.Fatalf("gửi: %+v %v", sent, err)
	}
	if _, err = ChangeAppraisalEntry(sent, AppraisalStepLocks{}, manager, save); err == nil {
		t.Fatal("đã gửi còn sửa trực tiếp")
	}
	if _, err = ChangeAppraisalEntry(sent, AppraisalStepLocks{}, manager, AppraisalEntryChange{Action: "return", Reason: "Đề nghị phòng rà soát lại"}); err == nil {
		t.Fatal("lãnh đạo tự trả đề xuất đã gửi")
	}
	if _, err = ChangeAppraisalEntry(sent, AppraisalStepLocks{}, center, AppraisalEntryChange{Action: "return", Reason: "ááááá"}); err == nil {
		t.Fatal("5 ký tự được tính nhầm thành 10byte")
	}
	corrected, err := ChangeAppraisalEntry(sent, AppraisalStepLocks{}, center, AppraisalEntryChange{Action: "return", Reason: "Cần bổ sung căn cứ đề xuất"})
	if err != nil || corrected.Status != AppraisalChangeRequested || corrected.ReturnedTo != "manager" {
		t.Fatalf("trả: %+v %v", corrected, err)
	}
	corrected, err = ChangeAppraisalEntry(corrected, AppraisalStepLocks{}, manager, save)
	if err != nil {
		t.Fatal(err)
	}
	corrected, err = ChangeAppraisalEntry(corrected, AppraisalStepLocks{}, manager, AppraisalEntryChange{Action: "send"})
	if err != nil {
		t.Fatal(err)
	}
	official, err := ChangeAppraisalEntry(corrected, AppraisalStepLocks{}, center, AppraisalEntryChange{Action: "official", OfficialRating: "HTNV"})
	if err != nil {
		t.Fatal(err)
	}
	closed, err := CloseAppraisalEntry(official)
	if err != nil || closed.Status != AppraisalClosed || closed.ClosedRating != "HTNV" {
		t.Fatalf("chốt DG09: %+v %v", closed, err)
	}
	fallback, err := CloseAppraisalEntry(corrected)
	if err != nil || fallback.ClosedRating != "HTTNV" {
		t.Fatalf("chốt fallbackDG05: %+v %v", fallback, err)
	}
	if _, err = ChangeAppraisalEntry(closed, AppraisalStepLocks{Closed: true}, center, AppraisalEntryChange{Action: "official", OfficialRating: "HTTNV"}); err == nil {
		t.Fatal("sửa trực tiếp người đã chốt")
	}
	reopened, err := ChangeAppraisalEntry(closed, AppraisalStepLocks{Closed: true}, center, AppraisalEntryChange{Action: "reopen", Reason: "Điều chỉnh theo biên bản bổ sung"})
	if err != nil || !reopened.Reopened || reopened.Status != AppraisalChangeRequested || reopened.ClosedRating != "" {
		t.Fatalf("mở lại: %+v %v", reopened, err)
	}
	if closed.Status != AppraisalClosed || closed.ClosedRating != "HTNV" {
		t.Fatal("mở lại làm đổi snapshot chốt")
	}
	if _, err = ChangeAppraisalEntry(reopened, AppraisalStepLocks{Closed: true}, manager, save); err != nil {
		t.Fatal(err)
	}
	if _, err = ChangeAppraisalEntry(entered, AppraisalStepLocks{Closed: true}, manager, save); err == nil {
		t.Fatal("mở một người đã mở toàn đợt")
	}
}

func TestAppraisalV4RejectsIncompleteAndScoring281(t *testing.T) {
	e, _, manager, center := appraisalEntryExample()
	for _, self := range []string{"", "HTXSNV", "HTTNV", "HTNV", "KHTNV"} {
		for _, collective := range []string{"", "HTXSNV", "HTTNV", "HTNV", "KHTNV"} {
			in := AppraisalEntryChange{Action: "manager_submit", SelfRating: self, CollectiveRating: collective}
			_, err := ChangeAppraisalEntry(e, AppraisalStepLocks{}, manager, in)
			need := self == "" || collective == "" || self == "HTXSNV" || self == "KHTNV" || collective == "HTXSNV" || collective == "KHTNV"
			if (err != nil) != need {
				t.Errorf("%s/%s: %v", self, collective, err)
			}
		}
	}
	for _, action := range []string{"self_draft", "self_submit", "proxy_self", "manager_draft"} {
		if _, err := ChangeAppraisalEntry(e, AppraisalStepLocks{}, manager, AppraisalEntryChange{Action: action}); err == nil {
			t.Errorf("writer cũ %s còn tồn tại", action)
		}
	}
	if _, err := CloseAppraisalEntry(e); err == nil {
		t.Fatal("chốt người chưa nhập")
	}
	for _, who := range []AppraisalAuthority{manager, center} {
		if _, err := ChangeAppraisalEntry(e, AppraisalStepLocks{}, who, AppraisalEntryChange{Action: "proxy_self"}); err == nil {
			t.Fatal("nhận writer phiếu cũ")
		}
	}
	e.Member.SummaryKind = "MS06"
	save := AppraisalEntryChange{Action: "manager_submit", SelfRating: "HTTNV", CollectiveRating: "HTNV"}
	if _, err := ChangeAppraisalEntry(e, AppraisalStepLocks{}, manager, save); err == nil {
		t.Fatal("lãnh đạo ghi Mẫu06")
	}
	if _, err := ChangeAppraisalEntry(e, AppraisalStepLocks{}, center, save); err != nil {
		t.Fatal(err)
	}
}
