package domain_test

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"testing"
	"time"
)

func TestW1168Adhoc366DayRangeCanTouchThreeYears(t *testing.T) {
	from := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	p, err := domain.PlanOf(rc.DefaultRule, domain.Request{Type: domain.TypeAdhoc, Year: 2027, Name: "Qua ba năm", From: &from, To: &to}, time.UTC)
	if err != nil || p.From.Year() != 2025 || p.To.Year() != 2027 {
		t.Fatalf("range %v %+v", err, p)
	}
	tooLate := to.AddDate(0, 0, 1)
	if _, err = domain.PlanOf(rc.DefaultRule, domain.Request{Type: domain.TypeAdhoc, Year: 2027, Name: "Quá giới hạn", From: &from, To: &tooLate}, time.UTC); err == nil {
		t.Fatal("range over366days accepted")
	}
	outside := time.Date(2150, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err = domain.PlanOf(rc.DefaultRule, domain.Request{Type: domain.TypeAdhoc, Year: 2026, Name: "Ngoài giới hạn năm", From: &outside, To: &outside}, time.UTC); err == nil {
		t.Fatal("year bypass accepted")
	}
}
