package notify_test

import (
	"testing"
	"time"

	mrvport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func mrvAlert(kind string, level int, severity, unit string) mrvport.AlertRaisedV1 {
	return mrvport.AlertRaisedV1{AlertID: ids.New().String(), Kind: kind, Level: level, Severity: severity, AdminUnitCode: unit,
		SubjectType: "evidence", SubjectID: ids.New().String(), SubjectRootID: ids.New().String(), RuleVersion: "test",
		EventTime: time.Now().UTC().Format(time.RFC3339)}
}

// KN US-MRV-04: "Cảnh báo khẩn" → cấp trên (người duyệt + quản trị TTKN tỉnh của địa bàn), khẩn.
// KN FR-08: ghi AWD thiếu hạ tầng → cán bộ khuyến nông XÃ phụ trách (danh bạ trực).
// KN US-MRV-06: quá hạn mức 1 → người duyệt; mức 2 → quản trị TTKN tỉnh. Giao lặp không báo hai lần.
func TestMRVAlertNotificationsKNUSMRV04_06_FR08(t *testing.T) {
	f := newFixture(t)
	provScope := authz.Scope{Units: []string{prov91, ward1, ward2}}
	reviewer := f.user(provScope, mrvport.RoleReviewer)
	provAdmin := f.user(provScope, partyport.RoleKNProvinceAdmin)
	officer := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	otherWard := f.user(authz.Scope{Units: []string{ward2}}, mrvport.RoleReviewer)

	urgent := f.event(f.tenant, mrvport.EventAlertRaised, mrvAlert(mrvport.AlertPestUrgent, 0, "urgent", ward1))
	f.process(port.ConsumerMRV, urgent)
	f.process(port.ConsumerMRV, urgent)
	for _, c := range []caller{reviewer, provAdmin} {
		in := c.inbox()
		if len(in) != 1 || in[0]["kind"] != port.KindMRVUrgent || in[0]["severity"] != "urgent" {
			t.Fatalf("cấp trên nhận Cảnh báo khẩn đúng một lần: %v", in)
		}
	}
	if len(officer.inbox()) != 0 || len(otherWard.inbox()) != 0 {
		t.Fatal("cán bộ xã / người duyệt xã khác không nhận Cảnh báo khẩn của xã này")
	}

	// FR-08: danh bạ có cán bộ xã phụ trách → thông báo cá nhân.
	f.staff.add(f.tenant, port.StaffRef{ActorRef: officer.actor, UnitCode: ward1, ProvinceCode: prov91})
	awd := mrvAlert(mrvport.AlertAWDNoInfrastructure, 0, "normal", ward1)
	awd.SubjectType = "farm_log"
	f.process(port.ConsumerMRV, f.event(f.tenant, mrvport.EventAlertRaised, awd))
	if in := officer.inbox(); len(in) != 1 || in[0]["kind"] != port.KindMRVAnomaly || in[0]["topic"] != false {
		t.Fatalf("cán bộ xã phụ trách nhận cờ AWD: %v", in)
	}

	f.process(port.ConsumerMRV, f.event(f.tenant, mrvport.EventAlertRaised, mrvAlert(mrvport.AlertReviewOverdue, 1, "normal", ward1)))
	if in := reviewer.inbox(); len(in) != 2 || kindCount(in, port.KindMRVOverdue, "normal") != 1 {
		t.Fatalf("mức 1 → người duyệt: %v", in)
	}
	if n := len(provAdmin.inbox()); n != 1 {
		t.Fatalf("mức 1 không báo quản trị tỉnh: %d", n)
	}
	f.process(port.ConsumerMRV, f.event(f.tenant, mrvport.EventAlertRaised, mrvAlert(mrvport.AlertReviewOverdue, 2, "urgent", ward1)))
	if in := provAdmin.inbox(); len(in) != 2 || kindCount(in, port.KindMRVOverdue, "urgent") != 1 {
		t.Fatalf("mức 2 → quản trị TTKN tỉnh: %v", in)
	}
	if n := len(reviewer.inbox()); n != 2 {
		t.Fatalf("mức 2 không báo lại người duyệt: %d", n)
	}
	// Tenant khác không thấy.
	if n := len(f.userIn(ids.New(), ids.New(), provScope, mrvport.RoleReviewer).inbox()); n != 0 {
		t.Fatalf("tenant khác thấy %d thông báo", n)
	}
}

func kindCount(in []map[string]any, kind, severity string) int {
	n := 0
	for _, m := range in {
		if m["kind"] == kind && m["severity"] == severity {
			n++
		}
	}
	return n
}
