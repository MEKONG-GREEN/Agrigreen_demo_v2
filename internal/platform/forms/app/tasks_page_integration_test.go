package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// BF-CN-XA-DATA (#175 review): giới hạn số kỳ của /v1/forms/tasks đếm SAU khi lọc phạm vi — kỳ ngoài phạm vi mới hơn
// không đẩy kỳ trong phạm vi ra khỏi trang; trang theo con trỏ đọc đủ, con trỏ chỉ là khóa của kỳ đã trả.
func TestFormsTasksScopeBeforeLimit_175(t *testing.T) {
	f := newUnitFx(t)
	defer func(n int) { taskScanBatch = n }(taskScanBatch)
	taskScanBatch = 2 // lô nhỏ: buộc đọc nhiều lô qua khóa after

	pub := Publisher{Svc: f.svc}
	node := ids.New()
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	tinh := f.asDept(uuid.Nil, "tinh", "chan_nuoi", node, "X1", "X9")
	xa1 := f.asDept(uuid.Nil, "xa", "chan_nuoi", ids.New(), "X1")
	xa9 := f.asDept(uuid.Nil, "xa", "chan_nuoi", ids.New(), "X9")
	_, err := f.svc.InstallFixtures(admin, "cnts", "")
	f.must(err, "fixture")
	ref, err := pub.LatestTemplate(admin, "cnts", "FX-02")
	f.must(err, "biểu")

	day := func(d int) time.Time { return time.Date(2027, 3, d, 0, 0, 0, 0, time.UTC) }
	defs := func(from, to int) []port.PeriodDef {
		var out []port.PeriodDef
		for d := from; d <= to; d++ {
			out = append(out, port.PeriodDef{Label: day(d).Format("02/01/2006"), DueAt: day(d + 1), Kind: "day", Key: day(d).Format(time.DateOnly),
				From: day(d), To: day(d), Rule: "r"})
		}
		return out
	}
	// Ba kỳ cũ của X1, sáu kỳ mới hơn chỉ của X9 (ngoài phạm vi xa1).
	mine, err := pub.PublishBatch(tinh, port.BatchInput{Profile: "cnts", TemplateID: ref.ID, UnitKind: port.UnitCommune, Units: []string{"X1"}, Periods: defs(1, 3)}, "")
	f.must(err, "kỳ X1")
	_, err = pub.PublishBatch(tinh, port.BatchInput{Profile: "cnts", TemplateID: ref.ID, UnitKind: port.UnitCommune, Units: []string{"X9"}, Periods: defs(4, 9)}, "")
	f.must(err, "kỳ X9")

	// Trước sửa: LIMIT 1 áp trước lọc phạm vi → kỳ mới nhất (X9) bị loại, xa1 nhận rỗng.
	ts, err := f.svc.Tasks(xa1, "cnts", TaskFilter{Code: "FX-02", Limit: 1})
	f.must(err, "công việc limit 1")
	if len(ts) != 1 || ts[0].Unit.Unit != "X1" || ts[0].Period.ID != mine[2].ID {
		t.Fatalf("limit 1 phải trả kỳ X1 mới nhất: %+v", ts)
	}

	// Trang theo con trỏ: đủ 3 kỳ, mới trước, con trỏ là khóa của kỳ đã trả; trang cuối không con trỏ.
	var got []uuid.UUID
	cursor := ""
	for i := 0; ; i++ {
		pg, err := f.svc.TasksPage(xa1, "cnts", TaskFilter{Code: "FX-02", Limit: 1, Cursor: cursor})
		f.must(err, "trang")
		if len(pg.Items) != 1 {
			t.Fatalf("trang %d: %+v", i, pg.Items)
		}
		got = append(got, pg.Items[0].Period.ID)
		if pg.NextCursor == "" {
			break
		}
		k, err := decodeTaskCursor(pg.NextCursor)
		if err != nil || k == nil || k.ID != pg.Items[0].Period.ID {
			t.Fatalf("con trỏ phải là khóa của kỳ đã trả: %+v %v", k, err)
		}
		if i > 5 {
			t.Fatal("con trỏ không dừng")
		}
		cursor = pg.NextCursor
	}
	if len(got) != 3 || got[0] != mine[2].ID || got[1] != mine[1].ID || got[2] != mine[0].ID {
		t.Fatalf("thứ tự trang: %v (muốn %v)", got, []uuid.UUID{mine[2].ID, mine[1].ID, mine[0].ID})
	}
	if pg, _ := f.svc.TasksPage(xa1, "cnts", TaskFilter{Code: "FX-02", Limit: 3}); len(pg.Items) != 3 || pg.NextCursor != "" {
		t.Fatalf("limit 3 vừa đủ: %d %q", len(pg.Items), pg.NextCursor)
	}
	// Khoảng ngày chỉ giữ kỳ X1 ngày 2–3; X9 không chen vào.
	from, to := day(2), day(8)
	if pg, _ := f.svc.TasksPage(xa1, "cnts", TaskFilter{Code: "FX-02", From: &from, To: &to, Limit: 5}); len(pg.Items) != 2 || pg.NextCursor != "" {
		t.Fatalf("khoảng ngày: %d", len(pg.Items))
	}
	// Phía X9: 6 kỳ, limit 4 → 4 + con trỏ, trang sau 2.
	pg, err := f.svc.TasksPage(xa9, "cnts", TaskFilter{Code: "FX-02", Limit: 4})
	f.must(err, "x9 trang 1")
	if len(pg.Items) != 4 || pg.NextCursor == "" {
		t.Fatalf("x9 trang 1: %d %q", len(pg.Items), pg.NextCursor)
	}
	if pg, _ = f.svc.TasksPage(xa9, "cnts", TaskFilter{Code: "FX-02", Limit: 4, Cursor: pg.NextCursor}); len(pg.Items) != 2 || pg.NextCursor != "" {
		t.Fatalf("x9 trang 2: %d %q", len(pg.Items), pg.NextCursor)
	}
	for _, c := range []string{"!!!", "YWJj", "LXw", string(make([]byte, 101))} {
		_, err := f.svc.TasksPage(xa1, "cnts", TaskFilter{Cursor: c})
		f.want(err, problem.Validation.Code, "con trỏ sai "+c)
	}
}

type fakeReviewGuard struct {
	deny  map[uuid.UUID]bool
	calls *int
}

func (g fakeReviewGuard) AllowReview(_ context.Context, period uuid.UUID, _ string) error {
	*g.calls++
	if g.deny[period] {
		return problem.Forbidden.WithDetail("không phải người phụ trách")
	}
	return nil
}

// BF-CN-XA-DATA (#175 review): ReviewGuard của bên dùng chặn ghi rà soát ở máy chủ; bài nộp ngoài phạm vi vẫn 404 và
// không gọi guard.
func TestFormsReviewGuard_175(t *testing.T) {
	f := newUnitFx(t)
	pub := Publisher{Svc: f.svc}
	node := ids.New()
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	tinh := f.asDept(uuid.Nil, "tinh", "chan_nuoi", node, "X1", "X2")
	outsider := f.asDept(uuid.Nil, "tinh", "chan_nuoi", ids.New(), "X9")
	xa1 := f.asDept(uuid.Nil, "xa", "chan_nuoi", ids.New(), "X1")
	_, err := f.svc.InstallFixtures(admin, "cnts", "")
	f.must(err, "fixture")
	ref, err := pub.LatestTemplate(admin, "cnts", "FX-02")
	f.must(err, "biểu")
	day := time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC)
	ps, err := pub.PublishBatch(tinh, port.BatchInput{Profile: "cnts", TemplateID: ref.ID, UnitKind: port.UnitCommune, Units: []string{"X1", "X2"},
		Periods: []port.PeriodDef{{Label: "01/04", DueAt: day.AddDate(0, 0, 30), Kind: "day", Key: "2027-04-01", From: day, To: day, Rule: "r"}}}, "")
	f.must(err, "phát hành")
	zero := int64(0)
	s1, err := f.svc.SubmitWith(xa1, "cnts", ps[0].ID, SubmitInput{Unit: "X1", Values: fx02, BaseVersion: &zero}, "")
	f.must(err, "nộp")

	calls := 0
	f.svc.ReviewGuards = map[string]port.ReviewGuard{"cnts": fakeReviewGuard{deny: map[uuid.UUID]bool{ps[0].ID: true}, calls: &calls}}
	_, err = f.svc.Review(outsider, "cnts", s1.Submission.ID, "accepted", "", "")
	f.want(err, "forms.submission_not_found", "ngoài phạm vi: 404 trước guard")
	if calls != 0 {
		t.Fatalf("guard không được gọi cho bài ngoài phạm vi: %d", calls)
	}
	_, err = f.svc.Review(tinh, "cnts", s1.Submission.ID, "accepted", "", "")
	f.want(err, "forbidden", "guard chặn")
	if calls != 1 || f.scalar(`SELECT count(*) FROM pf_forms.review WHERE submission_id = $1`, s1.Submission.ID) != 0 {
		t.Fatalf("bị chặn thì không ghi rà soát: calls=%d", calls)
	}
	f.svc.ReviewGuards["cnts"] = fakeReviewGuard{deny: map[uuid.UUID]bool{}, calls: &calls}
	v, err := f.svc.Review(tinh, "cnts", s1.Submission.ID, "accepted", "", "")
	f.must(err, "guard cho qua")
	if v.Status != "accepted" || calls != 2 {
		t.Fatalf("rà soát: %+v calls=%d", v.Status, calls)
	}
}
