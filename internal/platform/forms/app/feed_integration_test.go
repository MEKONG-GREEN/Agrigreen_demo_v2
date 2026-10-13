package app

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// W11.43: sá»± kiá»‡n pf.forms.submission.recorded cÃ¹ng giao dá»‹ch ná»™p; cá»•ng SubmissionFeed Ä‘á»c bÃ i ná»™p cuá»‘i theo ká»³/Ä‘Æ¡n vá»‹/xÃ£
// cha, khoáº£ng ngÃ y Ä‘áº§u ká»³, ká»³ Ä‘áº¿n háº¡n kÃ¨m tiáº¿n Ä‘á»™ (Ä‘á»c cá»§a há»‡ thá»‘ng â€” tenant truyá»n tÆ°á»ng minh).
func TestFormsSubmissionFeed_W1143(t *testing.T) {
	f := newUnitFx(t)
	svc := f.svc
	facA, facB := ids.New(), ids.New()
	svc.Facilities = fakeFacilities{facA: "X1", facB: "X2"}
	pub := Publisher{Svc: svc}
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	tinh := f.asDept(uuid.Nil, "tinh", "chan_nuoi", ids.New(), "X1", "X2")
	xa := f.asDept(uuid.Nil, "xa", "chan_nuoi", ids.New(), "X1", "X2")
	_, err := svc.InstallFixtures(admin, "cnts", "")
	f.must(err, "fixture")
	ref, err := pub.LatestTemplate(admin, "cnts", "FX-02")
	f.must(err, "biá»ƒu")
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	out, err := pub.PublishBatch(tinh, port.BatchInput{Profile: "cnts", TemplateID: ref.ID, UnitKind: port.UnitFacility,
		Units: []string{facA.String(), facB.String()}, Periods: []port.PeriodDef{
			{Label: "NgÃ y 04/10", DueAt: day(6), Kind: "day", Key: "2026-10-04", From: day(4), To: day(4), Rule: "r"},
			{Label: "NgÃ y 05/10", DueAt: day(7), Kind: "day", Key: "2026-10-05", From: day(5), To: day(5), Rule: "r"},
		}}, "")
	f.must(err, "phÃ¡t hÃ nh")
	d4, d5 := out[0].ID, out[1].ID
	a, b := facA.String(), facB.String()
	s1, err := svc.SubmitWith(xa, "cnts", d4, SubmitInput{Unit: a, Values: fx02}, "")
	f.must(err, "ná»™p A 04/10")
	_, err = svc.SubmitWith(xa, "cnts", d4, SubmitInput{Unit: a, Values: map[string]string{"pest": "sÃ¢u", "area_infected": "1"}}, "")
	f.must(err, "ná»™p láº¡i A 04/10")
	_, err = svc.SubmitWith(xa, "cnts", d5, SubmitInput{Unit: a, Values: fx02, NilReport: false}, "")
	f.must(err, "ná»™p A 05/10")
	_, err = svc.SubmitWith(xa, "cnts", d4, SubmitInput{Unit: b, Values: fx02}, "")
	f.must(err, "ná»™p B 04/10")

	// Sá»± kiá»‡n: má»™t sá»± kiá»‡n má»—i phiÃªn báº£n bÃ i ná»™p, khÃ´ng mang sá»‘ liá»‡u.
	var n int
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(f.ctx, `SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2`,
		f.tid, port.EventSubmissionRecorded).Scan(&n); err != nil || n != 4 {
		t.Fatalf("pf.forms.submission.recorded: %d %v", n, err)
	}

	feed := Feed{Svc: svc}
	r, err := feed.Recorded(f.ctx, f.tid, s1.Submission.ID)
	f.must(err, "Recorded")
	if r == nil || r.TemplateCode != "FX-02" || r.Parent != "X1" || r.UnitKind != port.UnitFacility || r.PeriodKey != "2026-10-04" ||
		r.From == nil || !r.From.Equal(day(4)) || r.Values["area_infected"] != "3.5" {
		t.Fatalf("Recorded: %+v", r)
	}
	if r, err := feed.Recorded(f.ctx, ids.New(), s1.Submission.ID); err != nil || r != nil {
		t.Fatalf("tenant khÃ¡c khÃ´ng tháº¥y: %+v %v", r, err)
	}
	ls, err := feed.Latest(f.ctx, f.tid, port.SubmissionQuery{Profile: "cnts", TemplateCode: "FX-02", Units: []string{a}})
	f.must(err, "Latest theo Ä‘Æ¡n vá»‹")
	if len(ls) != 2 || ls[0].PeriodKey != "2026-10-05" || ls[1].Version != 2 || ls[1].Values["pest"] != "sÃ¢u" {
		t.Fatalf("bÃ i cuá»‘i theo ká»³, má»›i trÆ°á»›c: %+v", ls)
	}
	from, to := day(4), day(4)
	ls, err = feed.Latest(f.ctx, f.tid, port.SubmissionQuery{Profile: "cnts", TemplateCode: "FX-02", Parents: []string{"X2"}, From: &from, To: &to})
	f.must(err, "Latest theo xÃ£ cha")
	if len(ls) != 1 || ls[0].Unit != b {
		t.Fatalf("xÃ£ cha X2 ngÃ y 04/10: %+v", ls)
	}
	ps, err := feed.DuePeriods(f.ctx, f.tid, "cnts", day(5), day(6))
	f.must(err, "DuePeriods")
	if len(ps) != 1 || ps[0].ID != d4 || ps[0].PeriodKey != "2026-10-04" || len(ps[0].Units) != 2 {
		t.Fatalf("ká»³ Ä‘áº¿n háº¡n: %+v", ps)
	}
	for _, u := range ps[0].Units {
		if u.Versions == 0 || u.Parent == "" {
			t.Fatalf("tiáº¿n Ä‘á»™ Ä‘Æ¡n vá»‹: %+v", u)
		}
	}
}
