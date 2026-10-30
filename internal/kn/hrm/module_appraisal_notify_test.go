package hrm

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
)

func TestAppraisalNotificationTargetsAndOwnerDirectory_W1126(t *testing.T) {
	tid, node, actor, self, leader := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	p := port.AppraisalChangedV1{NoticeID: uuid.New(), PeriodRef: uuid.New(), NodeRef: node, Year: 2026, Quarter: 4,
		Action: "open", Step: "self", DueAt: "2026-10-20T16:59:59Z", ActorRef: actor, Targets: []uuid.UUID{self, self}}
	drafts, err := build(t, nil, tid, port.EventAppraisalChanged, p)
	if err != nil || !slices.Equal(recipients(drafts), []uuid.UUID{self}) || drafts[0].Kind != port.KindAppraisal ||
		drafts[0].DeepLink != "/kn/nhan-su/danh-gia/dot/"+p.PeriodRef.String() {
		t.Fatalf("phiếu gắn AccountRef: %+v %v", drafts, err)
	}
	p.Targets, p.Units, p.Step, p.Action = nil, []string{"trong_trot"}, "manager", "self_submit"
	if _, err = build(t, nil, tid, port.EventAppraisalChanged, p); err == nil {
		t.Fatal("thiếu directory không được đoán lãnh đạo từ realm")
	}
	dir := &fakeDirectory{managers: map[string][]uuid.UUID{"trong_trot": {leader, actor}, "chan_nuoi": {uuid.New()}}}
	drafts, err = build(t, dir, tid, port.EventAppraisalChanged, p)
	if err != nil || !slices.Equal(recipients(drafts), []uuid.UUID{leader}) || len(dir.nodes) != 1 || dir.nodes[0] != node || dir.tenants[0] != tid {
		t.Fatalf("lãnh đạo đúng tenant/node/đơn vị: %+v %v %+v", drafts, err, dir)
	}
	p.StaffRef = &self
	drafts, err = build(t, dir, tid, port.EventAppraisalChanged, p)
	if err != nil || drafts[0].DeepLink != "/kn/nhan-su/danh-gia/dot/"+p.PeriodRef.String()+"/phieu/"+self.String() {
		t.Fatal("thông báo không mở đúng phiếu UUID", err)
	}
	p.NodeRef = uuid.Nil
	if _, err = build(t, dir, tid, port.EventAppraisalChanged, p); err == nil {
		t.Fatal("thiếu node vẫn gửi cho đơn vị trùng mã ở nơi khác")
	}
}
