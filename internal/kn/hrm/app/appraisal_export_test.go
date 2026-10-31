package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"testing"
)

func TestAppraisalPaperRejectsIncomplete_W1126(t *testing.T) {
	item := AppraisalItem{AppraisalEntry: domain.AppraisalEntry{Status: domain.AppraisalSent, SelfRating: "HTTNV", CollectiveRating: "HTTNV"}}
	for _, status := range []string{domain.AppraisalNotStarted, domain.AppraisalSelfSubmitted, domain.AppraisalManagerSubmitted, domain.AppraisalChangeRequested} {
		item.Status = status
		if validateAppraisalExport([]AppraisalItem{item}) == nil {
			t.Fatalf("xuất được phiếu %s", status)
		}
	}
	for _, status := range []string{domain.AppraisalSent, domain.AppraisalClosed} {
		item.Status = status
		if err := validateAppraisalExport([]AppraisalItem{item}); err != nil {
			t.Fatal(err)
		}
	}
	item.CollectiveRating = "KHTNV"
	if validateAppraisalExport([]AppraisalItem{item}) == nil {
		t.Fatal("xuất KHTNV thiếu căn cứ")
	}
	item.Reason = "Căn cứ nhiệm vụ không hoàn thành"
	if err := validateAppraisalExport([]AppraisalItem{item}); err != nil {
		t.Fatal(err)
	}
}
