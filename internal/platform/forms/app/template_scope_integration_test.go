package app

import (
	"context"
	"encoding/json"
	"strings"
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
		Units: []string{"U1"}, Periods: []port.PeriodDef{{Kind: "week", Key: "new", From: from, To: to, DueAt: to, Label: "blocked"}}}, "")
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
	allowedFigures, err := (Figures{Svc: f.svc}).Figures(boundedPub, port.FiguresQuery{Profile: "fx", TemplateCode: "FX-01", From: from, To: to})
	f.must(err, "allowed figures")
	if len(allowedFigures) != 1 {
		t.Fatalf("allowed figures disappeared: %d", len(allowedFigures))
	}

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

// TestTargetScopeLifecycleAndMetadata exercises explicit target restrictions on
// both global and owner-node actors, including reopened-state projection.
func TestTargetScopeLifecycleAndMetadata(t *testing.T) {
	f := newFx(t)
	profile := fxProfile
	profile.UnitKinds = []string{port.UnitFacility}
	var err error
	f.svc, err = NewService(f.own, f.clk, sqlApprovals{}, []port.Profile{profile})
	f.must(err, "facility service")
	node, a, b := ids.New(), ids.New(), ids.New()
	pub := f.as(f.tid, ids.New(), "tinh", node, "U1", "U2")
	tpls, err := f.svc.InstallFixtures(f.as(f.tid, ids.New(), "admin", node), "fx", "")
	f.must(err, "install")
	allowTemplate := func(string) bool { return true }
	onlyA := func(_ string, ref uuid.UUID) bool { return ref == a }
	for _, tc := range []struct {
		name, key string
		node      uuid.UUID
		scope     authz.Scope
	}{
		{"global", "2026-W41", ids.New(), authz.Scope{All: true}},
		{"owner", "2026-W42", node, authz.Scope{Units: []string{"U1", "U2"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from, to := f.clk.Now(), f.clk.Now().Add(6*24*time.Hour)
			v, err := f.svc.Publish(pub, PublishInput{Profile: "fx", TemplateID: tpls[0].ID, Label: tc.name,
				DueAt: to, Units: []string{a.String(), b.String()}, UnitKind: port.UnitFacility,
				UnitParents: []string{"U1", "U2"},
				Period:      &PeriodKey{Kind: "week", Key: tc.key, From: &from, To: &to, Rule: "fixture"}}, "")
			f.must(err, "publish")
			bounded := templateScopeCtx(f, ids.New(), "tinh", tc.node, tc.scope, allowTemplate, onlyA)
			_, err = f.svc.Close(bounded, "fx", v.Period.ID, "")
			f.want(err, "forbidden", "whole close outside targets")
			_, err = f.svc.Cancel(bounded, "fx", v.Period.ID, "cancel target regression", "")
			f.want(err, "forbidden", "whole cancel outside targets")
			err = (Control{Svc: f.svc}).ChangeDue(bounded, "fx", v.Period.ID, to.Add(time.Hour), "", "")
			f.want(err, "forbidden", "whole due change outside targets")
			unchanged, err := f.svc.Period(pub, "fx", v.Period.ID)
			f.must(err, "unchanged")
			if len(unchanged.Events) != 1 || !unchanged.Period.DueAt.Equal(to) {
				t.Fatal("denied target write left an event or changed due date")
			}
			assertMetadata := func(view PeriodView) {
				t.Helper()
				if len(view.Period.Units) != 1 || view.Period.Units[0] != a.String() ||
					len(view.Spec.UnitParents) != 1 || view.Spec.UnitParents[0] != "U1" || len(view.Units) != 1 {
					t.Fatalf("target metadata not aligned: %+v %+v", view.Period.Units, view.Spec.UnitParents)
				}
				raw, err := json.Marshal(view)
				f.must(err, "metadata json")
				if strings.Contains(string(raw), b.String()) {
					t.Fatal("forbidden target ID remains in response metadata")
				}
			}
			pv, err := f.svc.Period(bounded, "fx", v.Period.ID)
			f.must(err, "bounded view")
			assertMetadata(pv)
			ps, err := f.svc.Periods(bounded, "fx", 100)
			f.must(err, "bounded list")
			for _, period := range ps {
				for _, unit := range period.Units {
					if unit != a.String() {
						t.Fatal("period list leaked target")
					}
				}
			}
			figs, err := (Figures{Svc: f.svc}).Figures(bounded, port.FiguresQuery{Profile: "fx", TemplateCode: "FX-01", From: from, To: to})
			f.must(err, "bounded figures")
			if len(figs) == 0 {
				t.Fatal("allowed figures missing")
			}
			for _, figures := range figs {
				if _, leaked := figures.Parents[b.String()]; leaked || len(figures.Parents) != 1 ||
					figures.Parents[a.String()] != "U1" {
					t.Fatal("figures parent map leaked a forbidden target")
				}
			}
			_, err = f.svc.Close(pub, "fx", v.Period.ID, "")
			f.must(err, "legacy close")
			for _, units := range [][]string{nil, {b.String()}} {
				_, err = f.svc.Reopen(bounded, "fx", v.Period.ID, ReopenInput{Units: units, Reason: "reopen target regression"}, "")
				f.want(err, "forbidden", "reopen outside selected targets")
			}
			reopened, err := f.svc.Reopen(bounded, "fx", v.Period.ID, ReopenInput{Units: []string{a.String()}, Reason: "reopen allowed target"}, "")
			f.must(err, "allowed subset reopen")
			assertMetadata(reopened)
			if !reopened.State.AcceptsUnit(a.String()) || reopened.State.Status != domain.PeriodReopened {
				t.Fatal("allowed subset reopen became closed")
			}
			_, err = f.svc.Close(pub, "fx", v.Period.ID, "")
			f.must(err, "legacy reclose")
			_, err = f.svc.Reopen(pub, "fx", v.Period.ID, ReopenInput{Units: []string{b.String()}, Reason: "reopen other target only"}, "")
			f.must(err, "legacy reopen B")
			visible, err := f.svc.Period(bounded, "fx", v.Period.ID)
			f.must(err, "bounded reopened view")
			assertMetadata(visible)
			if visible.State.AcceptsUnit(a.String()) || visible.State.Status != domain.PeriodClosed {
				t.Fatal("hidden subset reopen became an all-target reopen")
			}
			tasks, err := f.svc.Tasks(bounded, "fx", TaskFilter{})
			f.must(err, "bounded tasks")
			for _, task := range tasks {
				raw, err := json.Marshal(task)
				f.must(err, "task json")
				if task.Unit.Unit != a.String() || strings.Contains(string(raw), b.String()) {
					t.Fatal("task metadata leaked a forbidden target")
				}
				if task.Period.ID == v.Period.ID && task.State.AcceptsUnit(a.String()) {
					t.Fatal("task state widened a hidden subset reopen")
				}
			}
			denied := templateScopeCtx(f, ids.New(), "tinh", tc.node, tc.scope, allowTemplate, func(string, uuid.UUID) bool { return false })
			_, err = f.svc.Period(denied, "fx", v.Period.ID)
			f.want(err, port.ErrPeriodNotFound.Code, "empty target scope")
			// Explicit full permission still controls a period; the callback is
			// a restriction, not an unconditional lifecycle ban.
			full := templateScopeCtx(f, ids.New(), "tinh", tc.node, tc.scope, allowTemplate, func(string, uuid.UUID) bool { return true })
			_, err = f.svc.Close(full, "fx", v.Period.ID, "")
			f.must(err, "full explicit close")
		})
	}
}
