package app

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestTemplateCatalogVersionsScopeAndTenant(t *testing.T) {
	f := newFx(t)
	actorRef, node := ids.New(), ids.New()
	admin := f.as(f.tid, actorRef, "admin", node)
	fields := domain.Fixtures()[0].Fields
	first, err := f.svc.DefineTemplate(admin, TemplateInput{Profile: "fx", Code: "CUSTOM-191", Title: "Original", Fields: fields}, "catalog-first")
	f.must(err, "first version")
	second, err := f.svc.DefineTemplate(admin, TemplateInput{Profile: "fx", Code: "CUSTOM-191", Title: "Next", Fields: fields}, "catalog-next")
	f.must(err, "next version")
	if first.ID == second.ID || first.Version != 1 || second.Version != 2 {
		t.Fatalf("not separate versions: %+v %+v", first, second)
	}
	pb := Publisher{Svc: f.svc}
	read := templateScopeCtx(f, ids.New(), "tinh", node, authz.Scope{All: true}, func(code string) bool { return code == "CUSTOM-191" }, nil)
	list, err := pb.ListTemplates(read, "fx")
	f.must(err, "catalog list")
	if len(list) != 1 || list[0].ID != second.ID {
		t.Fatalf("current custom catalog: %+v", list)
	}
	old, err := pb.TemplateVersion(read, "fx", "CUSTOM-191", 1)
	f.must(err, "old version")
	if old.ID != first.ID || old.Title != "Original" || !json.Valid(old.Fields) {
		t.Fatalf("old version changed: %+v", old)
	}
	history, err := pb.TemplateHistory(read, "fx", "CUSTOM-191", 0, 1)
	f.must(err, "history first page")
	if len(history) != 1 || history[0].ID != second.ID {
		t.Fatalf("history order: %+v", history)
	}
	history, err = pb.TemplateHistory(read, "fx", "CUSTOM-191", history[0].Version, 1)
	f.must(err, "history next page")
	if len(history) != 1 || history[0].ID != first.ID {
		t.Fatalf("history cursor: %+v", history)
	}
	blocked := templateScopeCtx(f, ids.New(), "tinh", node, authz.Scope{All: true}, func(string) bool { return false }, nil)
	_, err = pb.TemplateVersion(blocked, "fx", "CUSTOM-191", 1)
	f.want(err, port.ErrTemplateNotFound.Code, "read filter exact version")
	_, err = pb.TemplateHistory(blocked, "fx", "CUSTOM-191", 0, 10)
	f.want(err, port.ErrTemplateNotFound.Code, "read filter history")
	foreign := f.as(ids.New(), actorRef, "admin", node)
	_, err = pb.TemplateVersion(foreign, "fx", "CUSTOM-191", 1)
	f.want(err, port.ErrTemplateNotFound.Code, "foreign tenant version")
	history, err = pb.TemplateHistory(foreign, "fx", "CUSTOM-191", 0, 10)
	f.must(err, "foreign history")
	if len(history) != 0 {
		t.Fatal("foreign tenant history leaked")
	}
	for _, limit := range []int{0, 201} {
		_, err = pb.TemplateHistory(read, "fx", "CUSTOM-191", 0, limit)
		f.want(err, "validation_failed", "bounded limit")
	}
	_, err = pb.TemplateHistory(read, "fx", "CUSTOM-191", -1, 10)
	f.want(err, "validation_failed", "negative cursor")
	_, err = pb.TemplateVersion(read, "fx", "CUSTOM-191", 0)
	f.want(err, "validation_failed", "invalid exact version")
	f.must(f.svc.tx(admin, f.tid, func(tx pgx.Tx) error {
		replay, e := store.TemplateByIdem(admin, tx, "catalog-first:template:CUSTOM-191")
		if e != nil {
			return e
		}
		if replay == nil || replay.ID != first.ID {
			t.Fatal("durable replay lost original version")
		}
		return nil
	}), "durable catalog replay")
}
