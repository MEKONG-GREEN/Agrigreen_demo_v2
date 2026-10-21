package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func templateScopeCtx(f *fx, actorRef uuid.UUID, role string, node uuid.UUID, scope authz.Scope, filter func(string) bool, target func(string, uuid.UUID) bool) context.Context {
	return fixtureContext(f.ctx, authz.Principal{ActorRef: actorRef, TenantID: f.tid},
		port.ResolvedActor{Role: role, Node: node, Scope: scope, AllowsTemplate: filter, AllowsTarget: target})
}

func TestTemplateScopeReadsWritesAndSync(t *testing.T) {
	f := newFx(t)
	node, actorRef := ids.New(), ids.New()
	admin := f.as(f.tid, ids.New(), "admin", node)
	pub := f.as(f.tid, ids.New(), "tinh", node, "U1", "U2")
	tpls, err := f.svc.InstallFixtures(admin, "fx", "")
	f.must(err, "install")
	from, to := f.clk.Now(), f.clk.Now().Add(6*24*time.Hour)
	publish := func(index int) PeriodView {
		t.Helper()
		v, err := f.svc.Publish(pub, PublishInput{Profile: "fx", TemplateID: tpls[index].ID,
			Label: "scoped fixture", DueAt: to.Add(time.Hour), Units: []string{"U1", "U2"},
			Period: &PeriodKey{Kind: "week", Key: "2026-W41", From: &from, To: &to, Rule: "fixture"}}, "")
		f.must(err, "publish")
		return v
	}
	a, b := publish(0), publish(1)
	full := f.as(f.tid, actorRef, "xa", ids.New(), "U1", "U2")
	_, err = f.svc.Submit(full, "fx", a.Period.ID, "U1", fx01, "")
	f.must(err, "submit A")
	sb, err := f.svc.Submit(full, "fx", b.Period.ID, "U1", fx02, "")
	f.must(err, "submit B before restriction")
	allowA := func(code string) bool { return code == "FX-01" }
	bounded := templateScopeCtx(f, actorRef, "xa", ids.New(), authz.Scope{Units: []string{"U1", "U2"}}, allowA, nil)
	boundedPub := templateScopeCtx(f, ids.New(), "tinh", node, authz.Scope{All: true}, allowA, nil)

	got, err := f.svc.Templates(bounded, "fx")
	f.must(err, "templates")
	if len(got) != 1 || got[0].Code != "FX-01" {
		t.Fatalf("template metadata leaked: %+v", got)
	}
	_, err = f.svc.Template(bounded, "fx", tpls[1].ID)
	f.want(err, port.ErrTemplateNotFound.Code, "blocked template")
	ps, err := f.svc.Periods(bounded, "fx", 100)
	f.must(err, "periods")
	if len(ps) != 1 || ps[0].ID != a.Period.ID {
		t.Fatalf("period list leaked: %+v", ps)
	}
	_, err = f.svc.Period(bounded, "fx", b.Period.ID)
	f.want(err, port.ErrPeriodNotFound.Code, "blocked period")
	tasks, err := f.svc.Tasks(bounded, "fx", TaskFilter{})
	f.must(err, "tasks")
	if len(tasks) != 2 {
		t.Fatalf("unexpected task count %d", len(tasks))
	}
	for _, task := range tasks {
		if task.Period.TemplateCode != "FX-01" {
			t.Fatal("blocked task")
		}
	}
	changes, _, err := (SyncSource{Svc: f.svc}).Changes(bounded, authz.Scope{All: true}, port.EntityFormsTask, 0, 100)
	f.must(err, "sync")
	if len(changes) == 0 {
		t.Fatal("allowed sync disappeared")
	}
	for _, change := range changes {
		var task TaskData
		if err := json.Unmarshal(change.Data, &task); err != nil || task.TemplateCode != "FX-01" {
			t.Fatalf("sync leaked blocked template: %+v %v", task, err)
		}
	}
	_, err = f.svc.Submissions(bounded, "fx", b.Period.ID, "U1")
	f.want(err, port.ErrPeriodNotFound.Code, "blocked history")
	_, err = f.svc.Submit(bounded, "fx", b.Period.ID, "U1", fx02, "")
	f.want(err, port.ErrPeriodNotFound.Code, "blocked write")
	_, err = f.svc.Previous(bounded, "fx", b.Period.ID, "U1")
	f.want(err, port.ErrPeriodNotFound.Code, "blocked previous")
	_, err = f.svc.Review(boundedPub, "fx", sb.Submission.ID, domain.SubAccepted, "", "")
	f.want(err, port.ErrSubmissionMissing.Code, "blocked review")
	_, err = f.svc.Publish(boundedPub, PublishInput{Profile: "fx", TemplateID: tpls[1].ID, Label: "blocked", DueAt: to, Units: []string{"U1"}}, "")
	f.want(err, port.ErrTemplateNotFound.Code, "blocked publish")
	_, err = (Publisher{Svc: f.svc}).PublishBatch(boundedPub, port.BatchInput{Profile: "fx", TemplateID: tpls[1].ID,
		Units: []string{"U1"}, Periods: []port.BatchPeriod{{Kind: "week", Key: "new", From: from, To: to, DueAt: to, Label: "blocked"}}}, "")
	f.want(err, port.ErrTemplateNotFound.Code, "blocked batch")
	_, err = f.svc.Close(boundedPub, "fx", b.Period.ID, "")
	f.want(err, port.ErrPeriodNotFound.Code, "blocked close")
	err = (Control{Svc: f.svc}).ChangeDue(boundedPub, "fx", b.Period.ID, to, "", "")
	f.want(err, port.ErrPeriodNotFound.Code, "blocked control")
	figs, err := (Figures{Svc: f.svc}).Figures(boundedPub, port.FiguresQuery{Profile: "fx", TemplateCode: "FX-02", From: from, To: to})
	f.must(err, "figures")
	if len(figs) != 0 {
		t.Fatal("blocked figures leaked")
	}
	_, err = f.svc.DefineTemplate(templateScopeCtx(f, ids.New(), "admin", node, authz.Scope{All: true}, allowA, nil),
		TemplateInput{Profile: "fx", Code: "FX-02", Title: "blocked", Fields: tpls[1].Fields}, "")
	f.want(err, "forbidden", "blocked definition")

	empty := templateScopeCtx(f, actorRef, "xa", ids.New(), authz.Scope{All: true}, func(string) bool { return false }, nil)
	if xs, err := f.svc.Templates(empty, "fx"); err != nil || len(xs) != 0 {
		t.Fatal("explicit empty template scope widened", err)
	}
	foreign := fixtureContext(f.ctx, authz.Principal{ActorRef: actorRef, TenantID: ids.New()},
		port.ResolvedActor{Role: "xa", Scope: authz.Scope{All: true}, AllowsTemplate: allowA})
	_, err = f.svc.Period(foreign, "fx", a.Period.ID)
	f.want(err, port.ErrPeriodNotFound.Code, "tenant isolation")
}

func TestTemplateScopeOfflineReplayRechecksCurrentACL(t *testing.T) {
	f := newFx(t)
	node, actorRef := ids.New(), ids.New()
	admin := f.as(f.tid, ids.New(), "admin", node)
	pub := f.as(f.tid, ids.New(), "tinh", node, "U1", "U2")
	tpls, err := f.svc.InstallFixtures(admin, "fx", "")
	f.must(err, "install")
	publish := func() PeriodView {
		v, err := f.svc.Publish(pub, PublishInput{Profile: "fx", TemplateID: tpls[0].ID, Label: "retry",
			DueAt: f.clk.Now().Add(time.Hour), Units: []string{"U1", "U2"}}, "")
		f.must(err, "publish")
		return v
	}
	v, other := publish(), publish()
	full := f.as(f.tid, actorRef, "xa", ids.New(), "U1", "U2")
	id := ids.New()
	in := SubmitInput{ID: &id, Unit: "U1", Values: fx01}
	first, err := f.svc.SubmitWith(full, "fx", v.Period.ID, in, "")
	f.must(err, "initial")
	_, err = f.svc.Review(pub, "fx", first.Submission.ID, domain.SubAccepted, "", "")
	f.must(err, "review")
	_, err = f.svc.Close(pub, "fx", v.Period.ID, "")
	f.must(err, "close")
	allowed := templateScopeCtx(f, actorRef, "xa", ids.New(), authz.Scope{Units: []string{"U1", "U2"}},
		func(code string) bool { return code == "FX-01" }, nil)
	retry, err := f.svc.SubmitWith(allowed, "fx", v.Period.ID, in, "")
	f.must(err, "closed legitimate retry")
	if !retry.Replayed || retry.Submission.ID != id || retry.Submission.Values["crop"] != fx01["crop"] {
		t.Fatal("legitimate closed-period replay changed")
	}
	revoked := templateScopeCtx(f, actorRef, "xa", ids.New(), authz.Scope{All: true}, func(string) bool { return false }, nil)
	_, err = f.svc.SubmitWith(revoked, "fx", v.Period.ID, in, "")
	f.want(err, port.ErrPeriodNotFound.Code, "revoked template retry")
	unitRevoked := templateScopeCtx(f, actorRef, "xa", ids.New(), authz.Scope{Units: []string{"U9"}}, func(string) bool { return true }, nil)
	_, err = f.svc.SubmitWith(unitRevoked, "fx", v.Period.ID, in, "")
	f.want(err, "forbidden", "revoked unit retry")
	_, err = f.svc.SubmitWith(allowed, "fx", other.Period.ID, in, "")
	f.want(err, "conflict", "ID bound to period")
	mismatch := in
	mismatch.Unit = "U2"
	_, err = f.svc.SubmitWith(allowed, "fx", v.Period.ID, mismatch, "")
	f.want(err, "conflict", "ID bound to unit")
	// Held submissions follow the same authorization and identity binding.
	heldID, at := ids.New(), f.clk.Now()
	heldIn := SubmitInput{ID: &heldID, Unit: "U1", Values: fx01, CapturedAt: &at}
	held, err := f.svc.SubmitWith(allowed, "fx", v.Period.ID, heldIn, "")
	f.must(err, "held")
	if held.Status != domain.SubAwaitingReopen {
		t.Fatalf("not held: %+v", held)
	}
	heldAgain, err := f.svc.SubmitWith(allowed, "fx", v.Period.ID, heldIn, "")
	f.must(err, "held retry")
	if !heldAgain.Replayed {
		t.Fatal("held retry not replayed")
	}
	_, err = f.svc.SubmitWith(revoked, "fx", v.Period.ID, heldIn, "")
	f.want(err, port.ErrPeriodNotFound.Code, "revoked held retry")
}

func TestTemplateScopeSnapshotBytesAndTargetRestriction(t *testing.T) {
	f := newFx(t)
	profile := fxProfile
	profile.UnitKinds = []string{port.UnitFacility}
	var err error
	f.svc, err = NewService(f.own, f.clk, sqlApprovals{}, []port.Profile{profile})
	f.must(err, "facility service")
	node, a, b := ids.New(), ids.New(), ids.New()
	admin := f.as(f.tid, ids.New(), "admin", node)
	pub := f.as(f.tid, ids.New(), "tinh", node, "U1", "U2")
	tpls, err := f.svc.InstallFixtures(admin, "fx", "")
	f.must(err, "install")
	v, err := f.svc.Publish(pub, PublishInput{Profile: "fx", TemplateID: tpls[0].ID, Label: "snapshot", DueAt: f.clk.Now().Add(time.Hour),
		Units: []string{a.String(), b.String()}, UnitKind: port.UnitFacility, UnitParents: []string{"U1", "U2"}}, "")
	f.must(err, "publish")
	closed, err := f.svc.Close(pub, "fx", v.Period.ID, "")
	f.must(err, "close")
	snapshot := closed.Snapshots[0]
	onlyA := func(_ string, ref uuid.UUID) bool { return ref == a }
	bounded := templateScopeCtx(f, ids.New(), "xa", ids.New(), authz.Scope{All: true}, func(code string) bool { return code == "FX-01" }, onlyA)
	pv, err := f.svc.Period(bounded, "fx", v.Period.ID)
	f.must(err, "bounded period")
	if len(pv.Units) != 1 || pv.Units[0].Unit != a.String() {
		t.Fatal("all scope bypassed target restriction")
	}
	_, err = f.svc.Snapshot(bounded, "fx", snapshot.ID)
	f.want(err, port.ErrSnapshotNotFound.Code, "whole snapshot would leak B")
	all := templateScopeCtx(f, ids.New(), "xa", ids.New(), authz.Scope{All: true},
		func(code string) bool { return code == "FX-01" }, func(string, uuid.UUID) bool { return true })
	got, err := f.svc.Snapshot(all, "fx", snapshot.ID)
	f.must(err, "fully authorized snapshot")
	legacy, err := f.svc.Snapshot(pub, "fx", snapshot.ID)
	f.must(err, "legacy snapshot")
	if !got.HashValid || string(got.Content) != string(legacy.Content) {
		t.Fatal("immutable snapshot bytes/hash changed")
	}

	// Mirror has no native template/unit metadata: bounded access cannot be proven.
	mirrorRef := ids.New()
	caller := db.ForModule(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), db.Module("pf/extension"))
	payload, err := json.Marshal(map[string]any{"schema": "fixture", "values": []string{"private"}})
	f.must(err, "marshal")
	var mirror port.MirrorResult
	err = tenant.WithTenantTx(f.ctx, caller, f.tid, func(tx pgx.Tx) error {
		var e error
		mirror, e = f.svc.Record(db.JoinTx(f.ctx, tx), port.MirrorInput{Profile: "fx", PeriodRef: mirrorRef, Label: "mirror",
			Action: port.ActionClosed, Content: payload, Actor: ids.New(), At: f.clk.Now(), IdemKey: ids.New().String()})
		return e
	})
	f.must(err, "mirror")
	_, err = f.svc.Snapshot(pub, "fx", *mirror.SnapshotID)
	f.must(err, "legacy mirror remains")
	boundedPub := templateScopeCtx(f, ids.New(), "tinh", node, authz.Scope{All: true}, func(string) bool { return true }, nil)
	_, err = f.svc.Snapshot(boundedPub, "fx", *mirror.SnapshotID)
	f.want(err, port.ErrSnapshotNotFound.Code, "unknown mirror denied")
	_, err = f.svc.Snapshots(boundedPub, "fx", mirrorRef)
	f.want(err, "forbidden", "unknown mirror list denied")
}
