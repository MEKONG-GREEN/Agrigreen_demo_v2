package handler_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func warnings(r resp) []string {
	xs, _ := r.body["warnings"].([]any)
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

func mustWarnings(t *testing.T, r resp, what string, want ...string) {
	t.Helper()
	mustStatus(t, r, http.StatusCreated, what)
	got := warnings(r)
	if len(got) != len(want) {
		t.Fatalf("%s: cảnh báo %v (muốn %v)", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: cảnh báo %v (muốn %v)", what, got, want)
		}
	}
}

// R-INT-02 (kiến trúc v4.1 §39, §41.4, §43): lập lệnh gom đối chiếu HTX/thửa với BẢN SAO danh mục trong cụm
// mkg. Nền tảng ngừng: mục đã có → lập bình thường; bản sao quá max_age → gắn cờ directory_stale; mục chưa
// có → gắn cờ directory_unverified (HTX chưa đối chiếu thì phải ghi commune_code); bản sao ghi "không có"
// hoặc thửa thuộc HTX khác → vẫn từ chối. Job fieldops_directory làm mới bản sao.
func TestOrderDirectoryReplica_RINT02(t *testing.T) {
	f := newFixture(t, nil)
	sup := f.supervisor()
	ctx := context.Background()

	// Job làm mới: mục lệnh gom đang mở tham chiếu + mục đã có — chưa có lệnh nào → rỗng.
	if r, err := f.svc.RefreshDirectory(ctx, f.tenant); err != nil || r.HTX+r.Plots != 0 {
		t.Fatalf("bản sao rỗng: %+v %v", r, err)
	}
	// Lệnh đầu tiên: mục chưa có → tra trực tiếp (nền tảng chạy), ghi vào bản sao, không cảnh báo.
	r := sup.post("/v1/fieldops/orders", f.planBody())
	mustWarnings(t, r, "lập khi nền tảng chạy")
	if r.body["directory_as_of"] == nil {
		t.Fatalf("directory_as_of phải có: %s", r.raw)
	}
	if rr, err := f.svc.RefreshDirectory(ctx, f.tenant); err != nil || rr.HTX != 1 || rr.Plots != 2 || rr.Present != 3 {
		t.Fatalf("làm mới: %+v %v", rr, err)
	}

	// Nền tảng ngừng: mục đã có trong bản sao → lập được, không cảnh báo.
	f.dir.Down = true
	mustWarnings(t, sup.post("/v1/fieldops/orders", f.planBody()), "nền tảng ngừng, bản sao còn mới")
	if rr, err := f.svc.RefreshDirectory(ctx, f.tenant); !errors.Is(err, app.ErrDirectoryRefresh) || rr.Unavailable != 3 {
		t.Fatalf("làm mới khi nền tảng ngừng phải báo lỗi, giữ dòng cũ: %+v %v", rr, err)
	}

	// Thửa mới chưa có trong bản sao, nền tảng ngừng → lập được, gắn cờ chưa đối chiếu.
	fresh := ids.New()
	f.dir.AddPlot(fresh, f.htx, "31117")
	b := f.planBody()
	b["plot_refs"] = []string{f.plots[0].String(), fresh.String()}
	mustWarnings(t, sup.post("/v1/fieldops/orders", b), "thửa chưa có, nền tảng ngừng", app.WarnDirectoryUnverified)

	// HTX chưa từng thấy, nền tảng ngừng: không suy được mã xã → 422; ghi commune_code → lập, gắn cờ.
	newHTX := ids.New()
	b = f.planBody()
	b["htx_ref"], b["plot_refs"] = newHTX.String(), []string{ids.New().String()}
	mustCode(t, sup.post("/v1/fieldops/orders", b), http.StatusUnprocessableEntity, "validation_failed", "HTX chưa đối chiếu, thiếu mã xã")
	b["commune_code"] = "31117"
	mustWarnings(t, sup.post("/v1/fieldops/orders", b), "HTX chưa đối chiếu, có mã xã", app.WarnDirectoryUnverified)

	// Bản sao cũ hơn max_age (mặc định 6 giờ) → vẫn đối chiếu theo bản sao, gắn cờ dữ liệu cũ.
	f.clk.Advance(app.DefaultDirectoryMaxAge + time.Minute)
	mustWarnings(t, sup.post("/v1/fieldops/orders", f.planBody()), "bản sao cũ", app.WarnDirectoryStale)

	// Nền tảng trở lại; HTX chuyển thửa 1 sang HTX khác. Trước khi job chạy: bản sao (cũ) vẫn cho lập.
	f.dir.Down = false
	other := ids.New()
	f.dir.AddHTX(other, "31118")
	f.dir.AddPlot(f.plots[1], other, "31118")
	mustWarnings(t, sup.post("/v1/fieldops/orders", f.planBody()), "trước khi làm mới", app.WarnDirectoryStale)
	// Job chạy → bản sao mới: thửa thuộc HTX khác → 422, kể cả khi nền tảng ngừng ngay sau đó.
	if rr, err := f.svc.RefreshDirectory(ctx, f.tenant); err != nil || rr.Unavailable != 0 {
		t.Fatalf("làm mới sau khi nền tảng trở lại: %+v %v", rr, err)
	}
	f.dir.Down = true
	mustCode(t, sup.post("/v1/fieldops/orders", f.planBody()), http.StatusUnprocessableEntity, "fieldops.plot_other_htx",
		"bản sao mới: thửa HTX khác")

	// HTX lạ: nền tảng chạy → ghi "không có" vào bản sao; nền tảng ngừng → vẫn từ chối theo bản sao.
	f.dir.Down = false
	stranger := ids.New()
	b = f.planBody()
	b["htx_ref"] = stranger.String()
	mustCode(t, sup.post("/v1/fieldops/orders", b), http.StatusUnprocessableEntity, "fieldops.htx_not_found", "HTX lạ")
	f.dir.Down = true
	mustCode(t, sup.post("/v1/fieldops/orders", b), http.StatusUnprocessableEntity, "fieldops.htx_not_found", "HTX lạ, nền tảng ngừng")

	// Tắt tra trực tiếp (chỉ bản sao): nền tảng chạy nhưng thửa mới vẫn chỉ gắn cờ, không gọi nền tảng.
	f.dir.Down = false
	f.svc.DirectoryLookupBudget = -1
	b = f.planBody()
	b["plot_refs"] = []string{f.plots[0].String(), ids.New().String()}
	mustWarnings(t, sup.post("/v1/fieldops/orders", b), "chỉ bản sao", app.WarnDirectoryUnverified)
}

// Bản sao danh mục (mkg 000033): ảnh chụp cũ hơn không đè dòng mới hơn; "không có" xóa dữ liệu thửa; RLS theo tenant.
func TestDirectoryReplicaStore(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	plot, htx := ids.New(), ids.New()
	t0 := f.clk.Now()
	put := func(tid uuid.UUID, fn func(tx pgx.Tx) error) {
		t.Helper()
		if err := tenant.WithTenantTx(ctx, f.pool, tid, fn); err != nil {
			t.Fatal(err)
		}
	}
	put(f.tenant, func(tx pgx.Tx) error {
		if err := store.PutReplicaHTX(ctx, tx, f.tenant, store.ReplicaHTX{Ref: htx, Present: true, CommuneCode: "31117", Active: true, AsOf: t0}); err != nil {
			return err
		}
		return store.PutReplicaPlot(ctx, tx, f.tenant, store.ReplicaPlot{Ref: plot, Present: true, CooperativeRef: &htx, CommuneCode: "31117",
			Status: "active", AsOf: t0})
	})
	// Ảnh chụp cũ hơn (job chậm ghi sau tra dự phòng) không đè.
	put(f.tenant, func(tx pgx.Tx) error {
		return store.PutReplicaPlot(ctx, tx, f.tenant, store.ReplicaPlot{Ref: plot, AsOf: t0.Add(-time.Hour)})
	})
	put(f.tenant, func(tx pgx.Tx) error {
		ps, err := store.ReplicaPlots(ctx, tx, []uuid.UUID{plot})
		if err != nil {
			return err
		}
		if p := ps[plot]; !p.Present || p.CooperativeRef == nil || *p.CooperativeRef != htx || !p.AsOf.Equal(t0) {
			t.Fatalf("ảnh chụp cũ không được đè: %+v", p)
		}
		return nil
	})
	// Ảnh chụp mới hơn "không có" (thu hồi chia sẻ) đè và xóa dữ liệu thửa.
	put(f.tenant, func(tx pgx.Tx) error {
		return store.PutReplicaPlot(ctx, tx, f.tenant, store.ReplicaPlot{Ref: plot, CooperativeRef: &htx, Status: "active", AsOf: t0.Add(time.Hour)})
	})
	put(f.tenant, func(tx pgx.Tx) error {
		ps, err := store.ReplicaPlots(ctx, tx, []uuid.UUID{plot})
		if err != nil {
			return err
		}
		if p := ps[plot]; p.Present || p.CooperativeRef != nil || p.Status != "" {
			t.Fatalf("không có: %+v", p)
		}
		h, err := store.GetReplicaHTX(ctx, tx, htx)
		if err != nil || h == nil || !h.Active || h.CommuneCode != "31117" {
			t.Fatalf("HTX: %+v %v", h, err)
		}
		hs, plots, err := store.ReplicaRefs(ctx, tx)
		if err != nil || len(hs) != 1 || len(plots) != 1 {
			t.Fatalf("refs: %v %v %v", hs, plots, err)
		}
		return nil
	})
	// Tenant khác không thấy bản sao của tenant MKG (RLS p_tenant).
	put(ids.New(), func(tx pgx.Tx) error {
		h, err := store.GetReplicaHTX(ctx, tx, htx)
		if err != nil || h != nil {
			t.Fatalf("rò rỉ tenant: %+v %v", h, err)
		}
		return nil
	})
}
