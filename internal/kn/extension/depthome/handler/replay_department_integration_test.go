package handler_test

// Hồi quy #30 (Codex P2): phát lại Idempotency-Key phải kiểm phòng của bản ghi đã lưu với ngữ cảnh HIỆN HÀNH — người còn
// phiên ghi hợp lệ ở phòng B (cùng tenant, cùng actor) dùng lại khóa đã tạo ở phòng A không nhận lại thư mục/giới thiệu A.

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	depthomeapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestReviewDepartmentReplayRechecksCurrentDepartment(t *testing.T) {
	f := newFix(t)
	svc := depthomeapp.Service{DB: db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule), Clock: clock.NewFixed(t0)}
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	actor := ids.New()
	current := func(dept string) context.Context {
		t.Helper()
		k := tinh(dept, true)
		k.ID, k.OrgRef = ids.New(), ids.New()
		p := authz.Principal{ActorRef: actor, TenantID: f.tenant, MFA: true, Roles: []string{"kn_user"}, KN: &k}
		selected, e := registry.ResolveSelected(context.Background(), p)
		if e != nil {
			t.Fatal(e)
		}
		p.SelectedSession = &selected
		return authz.WithPrincipal(context.Background(), p)
	}
	a, b := current(cnts), current(khtc)
	reused := func(t *testing.T, e error) {
		t.Helper()
		var pb *problem.Problem
		if !errors.As(e, &pb) || pb.Code != problem.IdempotencyConflict.Code {
			t.Fatalf("want %s, got %v", problem.IdempotencyConflict.Code, e)
		}
	}
	t.Run("folder", func(t *testing.T) {
		idem := key()
		input := depthomeapp.FolderInput{Department: cnts, Name: "Department A folder"}
		created, e := svc.CreateFolder(a, input, idem)
		if e != nil {
			t.Fatalf("authorized folder seed: %v", e)
		}
		if _, e = svc.CreateFolder(b, input, key()); e == nil {
			t.Fatal("fresh cross-department request must deny")
		}
		replayInput := depthomeapp.FolderInput{Department: khtc, Name: "Department B valid request"}
		replay, e := svc.CreateFolder(b, replayInput, idem)
		if e == nil {
			t.Fatalf("replay crossed department: returnedOriginal=%v department=%s name=%q", replay.Ref == created.Ref, replay.Department, replay.Name)
		}
		reused(t, e)
		// Cùng phòng: phát lại vẫn trả bản cũ.
		same, e := svc.CreateFolder(a, input, idem)
		if e != nil || same.ID != created.ID {
			t.Fatalf("same-department replay: sameID=%v err=%v", same.ID == created.ID, e)
		}
		// Khóa của thao tác tạo không phát lại thành thao tác đổi tên.
		_, e = svc.ChangeFolder(a, depthomeapp.FolderChange{Department: cnts, Ref: created.Ref, Event: domain.FolderRenamed,
			Name: "Renamed", BaseVersion: created.Seq}, idem)
		reused(t, e)
	})
	t.Run("folder_change", func(t *testing.T) {
		created, e := svc.CreateFolder(a, depthomeapp.FolderInput{Department: cnts, Name: "Department A folder 2"}, key())
		if e != nil {
			t.Fatal(e)
		}
		idem := key()
		change := depthomeapp.FolderChange{Department: cnts, Ref: created.Ref, Event: domain.FolderRenamed, Name: "Department A renamed",
			BaseVersion: created.Seq}
		if _, e = svc.ChangeFolder(a, change, idem); e != nil {
			t.Fatalf("authorized rename seed: %v", e)
		}
		bChange := change
		bChange.Department = khtc
		replay, e := svc.ChangeFolder(b, bChange, idem)
		if e == nil {
			t.Fatalf("rename replay crossed department: department=%s name=%q", replay.Department, replay.Name)
		}
		reused(t, e)
	})
	t.Run("profile", func(t *testing.T) {
		idem := key()
		input := depthomeapp.ProfileInput{Department: cnts, Intro: "Department A private introduction", Duties: "Department A duties"}
		created, e := svc.SaveProfile(a, input, idem)
		if e != nil {
			t.Fatalf("authorized profile seed: %v", e)
		}
		if _, e = svc.SaveProfile(b, input, key()); e == nil {
			t.Fatal("fresh cross-department profile must deny")
		}
		replayInput := depthomeapp.ProfileInput{Department: khtc, Intro: "Department B valid introduction", Duties: "Department B duties"}
		replay, e := svc.SaveProfile(b, replayInput, idem)
		if e == nil {
			t.Fatalf("replay crossed department: originalIntro=%v department=%s intro=%q", replay.Intro == created.Intro, replay.Department, replay.Intro)
		}
		reused(t, e)
		same, e := svc.SaveProfile(a, input, idem)
		if e != nil || same.ID != created.ID {
			t.Fatalf("same-department profile replay: sameID=%v err=%v", same.ID == created.ID, e)
		}
	})
}
