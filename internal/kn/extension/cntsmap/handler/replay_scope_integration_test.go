package handler_test

// Hồi quy #28 (Codex P1): phát lại Idempotency-Key phải kiểm lại phạm vi HIỆN HÀNH trên vùng đã ghi — người đã mất phạm vi
// tỉnh/xã của vùng nhưng còn phiên ghi hợp lệ ở tỉnh khác (cùng tenant, cùng actor) không nhận lại vùng cũ.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	mapapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/app"
	mapdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestReviewZoneReplayRechecksCurrentProvince(t *testing.T) {
	f := newFix(t)
	svc := mapapp.Service{DB: db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule),
		Clock: clock.NewFixed(t0), Units: units{}, Catalogs: catalogs{}}
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	actor := ids.New()
	current := func(k authz.KNContext) context.Context {
		t.Helper()
		k.ID, k.OrgRef = ids.New(), ids.New()
		p := authz.Principal{ActorRef: actor, TenantID: f.tenant, MFA: true, Roles: []string{"kn_user"}, KN: &k}
		selected, err := registry.ResolveSelected(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		p.SelectedSession = &selected
		return authz.WithPrincipal(context.Background(), p)
	}
	a := current(tinh("vien_chuc"))
	bState := tinh("vien_chuc")
	bState.Scope = authz.Scope{Units: []string{"92", "31001"}}
	b := current(bState)
	geometry, err := json.Marshal(square(105.20, 10.55, 0.02))
	if err != nil {
		t.Fatal(err)
	}
	effective := time.Date(2027, 10, 1, 0, 0, 0, 0, time.UTC)
	input := mapapp.ZoneInput{Province: "89", IdemKey: key(), Zone: mapdomain.Zone{Kind: "vung_chan_nuoi", Name: "Owned province A",
		Communes: []string{"30002"}, Mode: "polygon", Geometry: geometry, Species: "HEO", EffectiveOn: &effective}}
	created, err := svc.CreateZone(a, input)
	if err != nil {
		t.Fatalf("seed authorized province A: %v", err)
	}
	reused := func(t *testing.T, what string, v mapapp.ZoneView, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: replay bypassed current province scope: returned original=%v province=%s geometryBytes=%d replayed=%v",
				what, v.Ref == created.Ref, v.Province, len(v.GeoJSON), v.Replayed)
		}
		var pb *problem.Problem
		if !errors.As(err, &pb) || (pb.Status != 422 && pb.Status != 403 && pb.Status != 404) {
			t.Fatalf("%s: want 422/403/404 problem, got %v", what, err)
		}
	}

	t.Run("create", func(t *testing.T) {
		fresh := input
		fresh.IdemKey = key()
		if _, err := svc.CreateZone(b, fresh); err == nil {
			t.Fatal("fixture did not establish scope denial for province B")
		}
		replay, err := svc.CreateZone(b, input)
		reused(t, "create", replay, err)
		var pb *problem.Problem
		if errors.As(err, &pb) && pb.Code != problem.IdempotencyConflict.Code {
			t.Fatalf("create replay: want %s, got %s", problem.IdempotencyConflict.Code, pb.Code)
		}
		// Cùng phạm vi: phát lại vẫn trả bản cũ (không đổi hành vi idempotent hợp lệ).
		same, err := svc.CreateZone(a, input)
		if err != nil || !same.Replayed || same.ID != created.ID {
			t.Fatalf("same-scope replay: replayed=%v sameID=%v err=%v", same.Replayed, same.ID == created.ID, err)
		}
	})

	t.Run("update", func(t *testing.T) {
		upd := input
		upd.IdemKey, upd.Ref, upd.BaseVersion, upd.Reason = key(), &created.Ref, &created.Seq, "Sửa ranh giới vùng A"
		upd.Name = "Owned province A v2"
		v2, err := svc.UpdateZone(a, upd)
		if err != nil {
			t.Fatalf("seed update in A: %v", err)
		}
		replay, err := svc.UpdateZone(b, upd)
		reused(t, "update", replay, err)
		// Khóa của thao tác sửa không phát lại được cho vùng khác/thao tác khác.
		other := upd
		other.Ref = &v2.Ref
		var pb *problem.Problem
		if _, err := svc.CreateZone(a, other); !errors.As(err, &pb) || pb.Code != problem.IdempotencyConflict.Code {
			t.Fatalf("update key replayed as create: %v", err)
		}
		same, err := svc.UpdateZone(a, upd)
		if err != nil || !same.Replayed || same.ID != v2.ID {
			t.Fatalf("same-scope update replay: replayed=%v err=%v", same.Replayed, err)
		}
	})

	t.Run("status", func(t *testing.T) {
		latest, err := svc.GetZone(a, created.Ref)
		if err != nil {
			t.Fatal(err)
		}
		idem := key()
		seq := latest.Seq
		if _, err := svc.SetZoneStatus(a, created.Ref, true, &seq, "Ngưng vùng A", nil, idem); err != nil {
			t.Fatalf("seed retire in A: %v", err)
		}
		replay, err := svc.SetZoneStatus(b, created.Ref, true, &seq, "Ngưng vùng A", nil, idem)
		reused(t, "status", replay, err)
	})
}
