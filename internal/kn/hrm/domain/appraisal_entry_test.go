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
	if !CanReadAppraisal(self, e.Member) || !CanReadAppraisal(manager, e.Member) || !CanReadAppraisal(center, e.Member) {
		t.Fatal("chủ thể/đơn vị quản lý/HCTH không xem được")
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

func TestAppraisalReasonCountsCharacters_W1126(t *testing.T) {
	e, _, manager, _ := appraisalEntryExample()
	e.Status = AppraisalSelfSubmitted
	if _, err := ChangeAppraisalEntry(e, appraisalExample(), AppraisalStepLocks{}, manager,
		AppraisalEntryChange{Action: "return", ReturnTo: "self", Reason: "ááááá"}); err == nil {
		t.Fatal("5 ký tự không được tính thành10 vì UTF8 có10byte")
	}
	if _, err := ChangeAppraisalEntry(e, appraisalExample(), AppraisalStepLocks{}, manager,
		AppraisalEntryChange{Action: "return", ReturnTo: "self", Reason: "Cần sửa lại phần căn cứ"}); err != nil {
		t.Fatal(err)
	}
}

func TestAppraisalAuthenticatedFlowAndImmutableClose_W1126(t *testing.T) {
	e, self, manager, center := appraisalEntryExample()
	form := appraisalExample()
	var err error
	e, err = ChangeAppraisalEntry(e, form, AppraisalStepLocks{}, self, AppraisalEntryChange{Action: "self_submit", Answers: appraisalAnswers(8200), Reason: "Hoàn thành tốt kết quả công việc và tiến độ"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != AppraisalSelfSubmitted || e.SelfRating != "HTXSNV" {
		t.Fatalf("%+v", e)
	}
	if _, err = ChangeAppraisalEntry(e, form, AppraisalStepLocks{}, self, AppraisalEntryChange{Action: "self_draft"}); err == nil {
		t.Fatal("đã nộp vẫn tự sửa")
	}
	e, err = ChangeAppraisalEntry(e, form, AppraisalStepLocks{}, manager, AppraisalEntryChange{Action: "manager_submit", Answers: appraisalAnswers(7000), CollectiveRating: "HTTNV", Reason: "Kết quả thực hiện nhiệm vụ theo phân công"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Manager.Total != "70" || e.Manager.Suggested != "HTTNV" || e.Self.Suggested != "HTXSNV" {
		t.Fatal("mất điểm tự chấm hoặc gợi ý cấp quản lý")
	}
	e, err = ChangeAppraisalEntry(e, form, AppraisalStepLocks{}, manager, AppraisalEntryChange{Action: "send"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ChangeAppraisalEntry(e, form, AppraisalStepLocks{}, manager, AppraisalEntryChange{Action: "manager_draft"}); err == nil {
		t.Fatal("đã gửi HCTH vẫn sửa")
	}
	e, err = ChangeAppraisalEntry(e, form, AppraisalStepLocks{}, center, AppraisalEntryChange{Action: "official", OfficialRating: "HTNV", Reason: "Kết luận theo biên bản họp tập thể lãnh đạo"})
	if err != nil {
		t.Fatal(err)
	}
	closed, err := CloseAppraisalEntry(e)
	if err != nil || closed.ClosedRating != "HTNV" || closed.Status != AppraisalClosed {
		t.Fatalf("%+v %v", closed, err)
	}
	if e.ClosedRating != "" || e.Status != AppraisalSent {
		t.Fatal("chốt đã sửa phiên bản nguồn")
	}
	if _, err = ChangeAppraisalEntry(closed, form, AppraisalStepLocks{Closed: true}, center, AppraisalEntryChange{Action: "official", OfficialRating: "HTTNV"}); err == nil {
		t.Fatal("sửa trực tiếp bản đã chốt")
	}
	reopened, err := ChangeAppraisalEntry(closed, form, AppraisalStepLocks{Closed: true}, center, AppraisalEntryChange{Action: "reopen", Reason: "Đính chính kết quả theo biên bản bổ sung"})
	if err != nil || !reopened.Reopened || reopened.ReturnedTo != "manager" {
		t.Fatalf("%+v %v", reopened, err)
	}
	if closed.Status != AppraisalClosed || closed.ClosedRating != "HTNV" {
		t.Fatal("mở lại đã làm đổi ảnh chụp chốt")
	}
}

func TestAppraisalLocksReadonlyAndProxyProof_W1126(t *testing.T) {
	e, self, manager, _ := appraisalEntryExample()
	form := appraisalExample()
	input := AppraisalEntryChange{Action: "self_submit", Answers: appraisalAnswers(7000)}
	if _, err := ChangeAppraisalEntry(e, form, AppraisalStepLocks{Self: true}, self, input); err == nil {
		t.Fatal("khóa bước vẫn nộp")
	}
	self.ReadOnly = true
	if _, err := ChangeAppraisalEntry(e, form, AppraisalStepLocks{}, self, input); err == nil {
		t.Fatal("chỉ xem vẫn ghi")
	}
	proof := uuid.MustParse("00000000-0000-7000-8000-000000000021")
	input.Action = "proxy_self"
	input.ProxyDocument = &proof
	if _, err := ChangeAppraisalEntry(e, form, AppraisalStepLocks{}, manager, input); err == nil {
		t.Fatal("tin mã scan chưa kiểm quyền")
	}
	manager.ProofVerified = true
	got, err := ChangeAppraisalEntry(e, form, AppraisalStepLocks{}, manager, input)
	if err != nil || got.ProxyDocument == nil {
		t.Fatalf("%+v %v", got, err)
	}
	*got.ProxyDocument = uuid.Nil
	if proof == uuid.Nil {
		t.Fatal("bản phiếu giữ con trỏ đầu vào")
	}
	if _, err := CloseAppraisalEntry(e); err == nil {
		t.Fatal("chốt phiếu chưa nộp")
	}
}

func TestAppraisalReturnRequiresReasonAndAssignment_W1126(t *testing.T) {
	e, self, manager, center := appraisalEntryExample()
	form := appraisalExample()
	e, err := ChangeAppraisalEntry(e, form, AppraisalStepLocks{}, self, AppraisalEntryChange{Action: "self_submit", Answers: appraisalAnswers(7000)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ChangeAppraisalEntry(e, form, AppraisalStepLocks{}, manager, AppraisalEntryChange{Action: "return", ReturnTo: "self", Reason: "sửa"}); err == nil {
		t.Fatal("trả phiếu không đủ lý do")
	}
	if _, err = ChangeAppraisalEntry(e, form, AppraisalStepLocks{Self: true}, manager, AppraisalEntryChange{Action: "return", ReturnTo: "self", Reason: "Cần bổ sung nội dung tự đánh giá"}); err == nil {
		t.Fatal("lãnh đạo tự mở khóa bước HCTH đã khóa")
	}
	returned, err := ChangeAppraisalEntry(e, form, AppraisalStepLocks{}, manager, AppraisalEntryChange{Action: "return", ReturnTo: "self", Reason: "Cần bổ sung nội dung tự đánh giá"})
	if err != nil {
		t.Fatal(err)
	}
	returned, err = ChangeAppraisalEntry(returned, form, AppraisalStepLocks{}, self, AppraisalEntryChange{Action: "self_submit", Answers: appraisalAnswers(7500)})
	if err != nil || returned.Self.Total != "75" || returned.Status != AppraisalSelfSubmitted {
		t.Fatalf("%+v %v", returned, err)
	}
	if _, err = ChangeAppraisalEntry(returned, form, AppraisalStepLocks{}, center, AppraisalEntryChange{Action: "return", ReturnTo: "manager", Reason: "Cần điều chỉnh tập thể đề xuất"}); err == nil {
		t.Fatal("trả đề xuất chưa được gửi HCTH")
	}
}

func TestAppraisalIndividualReturnDoesNotUnlockOtherEntries_W1126(t *testing.T) {
	e, _, manager, center := appraisalEntryExample()
	e.Status = AppraisalSent
	locks := AppraisalStepLocks{Self: true, Manager: true}
	returned, err := ChangeAppraisalEntry(e, appraisalExample(), locks, center,
		AppraisalEntryChange{Action: "return", ReturnTo: "manager", Reason: "Cần đơn vị rà soát lại đề xuất"})
	if err != nil || !returned.Reopened {
		t.Fatalf("HCTH trả riêng: %+v %v", returned, err)
	}
	returned, err = ChangeAppraisalEntry(returned, appraisalExample(), locks, manager,
		AppraisalEntryChange{Action: "return", ReturnTo: "self", Reason: "Cần cá nhân bổ sung căn cứ"})
	if err != nil || returned.ReturnedTo != "self" {
		t.Fatalf("lãnh đạo trả tiếp đúng phiếu đã mở riêng: %+v %v", returned, err)
	}
	if _, err = ChangeAppraisalEntry(e, appraisalExample(), locks, manager,
		AppraisalEntryChange{Action: "return", ReturnTo: "self", Reason: "Không được tự mở phiếu khác"}); err == nil {
		t.Fatal("mở riêng một phiếu làm mở khóa cả đơn vị")
	}
}
