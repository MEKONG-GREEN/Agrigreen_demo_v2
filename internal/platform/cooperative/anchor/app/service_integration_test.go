package app

// D2 (CEO 13/10/2026, ADR-0052): neo HTX theo MST, HTX tạm, gắn MST, nhận quyền (lũy đẳng, nguyên tử, chạy thử, vết
// kiểm toán, báo cáo xung đột), neo dữ liệu có trước D2, trạng thái xác nhận trên hồ sơ HTX (đường đọc của danh sách
// KN / báo cáo) và phân giải bí danh cho đồng bộ ngoại tuyến. Chạy dưới r_cooperative như gốc lắp ráp.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/anchor/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/anchor/store"
	memberstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fx struct {
	t     *testing.T
	svc   *Service
	admin *pgxpool.Pool
	tid   uuid.UUID
}

func newFx(t *testing.T) *fx {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	db := shareddb.ForModule(pool, shareddb.Module("pf/cooperative"))
	return &fx{t: t, svc: &Service{DB: db, Clock: clock.System{}}, admin: env.AdminPool(t, migrations.PF), tid: ids.New()}
}

func (f *fx) as(roles ...string) context.Context {
	p := authz.Principal{ActorRef: ids.New(), TenantID: f.tid, Roles: roles, MFA: true}
	return tenant.WithID(authz.WithPrincipal(context.Background(), p), f.tid)
}

func (f *fx) kn() context.Context  { return f.as(roleRegistryAdmin) }
func (f *fx) mkg() context.Context { return f.as(authz.RolePlatformAdmin) }

func (f *fx) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.admin.Exec(context.Background(), q, args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

// coop tạo Nút HTX + hồ sơ (tax rỗng = chưa có MST trong hồ sơ).
func (f *fx) coop(tax string) uuid.UUID {
	f.t.Helper()
	ref := ids.New()
	f.exec(`INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, admin_unit_code, last_event_id, last_seq)
	  VALUES ($1, $2, 'cooperative', $3, 'HTX D2', '30001', gen_random_uuid(), 1)`, f.tid, ref, "H"+ref.String()[24:])
	var t *string
	if tax != "" {
		t = &tax
	}
	f.exec(`INSERT INTO pf_coop.cooperative (tenant_id, org_ref, tax_code) VALUES ($1, $2, $3)`, f.tid, ref, t)
	return ref
}

// member gắn một hộ thành viên vào HTX (dữ liệu của Nút).
func (f *fx) member(coop uuid.UUID) {
	hh := ids.New()
	f.exec(`INSERT INTO pf_coop.household (tenant_id, household_ref, commune_code, created_by) VALUES ($1, $2, '30001', gen_random_uuid())`, f.tid, hh)
	f.exec(`INSERT INTO pf_coop.household_membership (tenant_id, id, household_ref, org_ref, since_date) VALUES ($1, gen_random_uuid(), $2, $3, '2026-01-01')`,
		f.tid, hh, coop)
}

func code(err error) string {
	if p := problem.From(err); p != nil && err != nil {
		return p.Code
	}
	return ""
}

func wantCode(t *testing.T, err error, c, what string) {
	t.Helper()
	if code(err) != c {
		t.Fatalf("%s: muốn %s, có %v", what, c, err)
	}
}

func TestCreateProvisionalDuplicateMalformedBranch_D2(t *testing.T) {
	f := newFx(t)
	a, b, c, d := f.coop(""), f.coop(""), f.coop(""), f.coop("")

	// KN lập HTX tạm có MST (nhập có khoảng trắng, gạch) → khóa chính tắc.
	key := "k-" + ids.New().String()
	got, replayed, err := f.svc.Create(f.kn(), CreateInput{OrgRef: a, CreatedBy: domain.CreatedByKN, MST: "0312 345-678", IdemKey: key})
	if err != nil || replayed || got.AnchorKey != "0312345678" || got.Status != domain.StatusProvisional ||
		got.Confirmation != domain.ConfirmationUnconfirmed || got.CreatedByKind != "kn" {
		t.Fatalf("lập HTX tạm: %+v %v", got, err)
	}
	// Gọi lặp cùng khóa → cùng neo.
	again, replayed, err := f.svc.Create(f.kn(), CreateInput{OrgRef: a, CreatedBy: domain.CreatedByKN, MST: "0312345678", IdemKey: key})
	if err != nil || !replayed || again.AnchorID != got.AnchorID {
		t.Fatalf("gọi lặp: %+v %v", again, err)
	}
	// MKG lập trùng MST cho Nút khác → 409, chỉ về neo đã có (không gộp ngầm).
	_, _, err = f.svc.Create(f.mkg(), CreateInput{OrgRef: b, CreatedBy: domain.CreatedByMKG, MST: "0312-345-678", IdemKey: "k2"})
	wantCode(t, err, "cooperative.htx_anchor_mst_taken", "MKG trùng MST")
	var pe *problem.Problem
	if !errors.As(err, &pe) || pe.Extra["existing_anchor_id"] != got.AnchorID.String() || pe.Extra["existing_created_by"] != "kn" {
		t.Fatalf("problem phải chỉ neo đã có: %+v", pe)
	}
	// Vai KN không lập được HTX tạm "do MKG".
	_, _, err = f.svc.Create(f.kn(), CreateInput{OrgRef: b, CreatedBy: domain.CreatedByMKG, IdemKey: "k3"})
	if !errors.Is(err, problem.Forbidden) {
		t.Fatalf("KN lập thay MKG: %v", err)
	}
	// MST sai dạng.
	for _, bad := range []string{"031234567", "0312345678-000", "03123A5678"} {
		_, _, err = f.svc.Create(f.mkg(), CreateInput{OrgRef: b, CreatedBy: domain.CreatedByMKG, MST: bad, IdemKey: "k4" + bad})
		wantCode(t, err, "cooperative.htx_anchor_mst_malformed", "MST "+bad)
	}
	// Chi nhánh: neo riêng, khóa 13 số có gạch.
	br, _, err := f.svc.Create(f.mkg(), CreateInput{OrgRef: c, CreatedBy: domain.CreatedByMKG, MST: "0312345678001", IdemKey: "k5"})
	if err != nil || br.AnchorKey != "0312345678-001" || br.CreatedByKind != "mkg" {
		t.Fatalf("chi nhánh: %+v %v", br, err)
	}
	// Không có MST → mã tạm.
	tmp, _, err := f.svc.Create(f.kn(), CreateInput{OrgRef: d, CreatedBy: domain.CreatedByKN, IdemKey: "k6"})
	if err != nil || tmp.MST != nil || tmp.AnchorKey != domain.TempCode(tmp.AnchorID) {
		t.Fatalf("mã tạm: %+v %v", tmp, err)
	}
	// Nút đã có neo → 409; Nút không có hồ sơ HTX → 422.
	_, _, err = f.svc.Create(f.mkg(), CreateInput{OrgRef: d, CreatedBy: domain.CreatedByMKG, IdemKey: "k7"})
	wantCode(t, err, "cooperative.htx_anchor_org_anchored", "Nút đã có neo")
	_, _, err = f.svc.Create(f.mkg(), CreateInput{OrgRef: ids.New(), CreatedBy: domain.CreatedByMKG, IdemKey: "k8"})
	wantCode(t, err, "cooperative.htx_anchor_org_not_cooperative", "Nút lạ")
	// MST khác MST hồ sơ của Nút.
	e := f.coop("0399999999")
	_, _, err = f.svc.Create(f.mkg(), CreateInput{OrgRef: e, CreatedBy: domain.CreatedByMKG, MST: "0388888888", IdemKey: "k9"})
	wantCode(t, err, "cooperative.htx_anchor_mst_profile_mismatch", "lệch hồ sơ")
	// MST đã ở hồ sơ HTX khác (chưa neo) → 409.
	_, _, err = f.svc.Create(f.mkg(), CreateInput{OrgRef: b, CreatedBy: domain.CreatedByMKG, MST: "0399999999", IdemKey: "k10"})
	wantCode(t, err, "cooperative.htx_anchor_mst_taken", "MST của hồ sơ khác")
}

func TestAttachMSTClaimIdempotentAudit_D2(t *testing.T) {
	f := newFx(t)
	org := f.coop("")
	f.member(org) // dữ liệu KN nhập dưới HTX tạm
	a, _, err := f.svc.Create(f.kn(), CreateInput{OrgRef: org, CreatedBy: domain.CreatedByKN, IdemKey: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	account := ids.New()
	claim := func(ctx context.Context, acc uuid.UUID, mst string, dry bool, key string) (ClaimReport, error) {
		return f.svc.Claim(ctx, a.AnchorID, ClaimInput{AccountRef: acc, VerifiedMST: mst, VerificationRef: "CV-123/TCT", DryRun: dry, IdemKey: key})
	}
	// Chưa có MST → không nhận quyền được.
	_, err = claim(f.kn(), account, "0312345679", false, "x1")
	wantCode(t, err, "cooperative.htx_anchor_mst_required", "nhận quyền khi chưa có MST")
	// Gắn MST (KN quản trị tỉnh được gắn).
	if a2, err := f.svc.AttachMST(f.as(roleKNProvinceAdmin), a.AnchorID, "0312-345-679", "m1"); err != nil || a2.AnchorKey != "0312345679" {
		t.Fatalf("gắn MST: %+v %v", a2, err)
	}
	_, err = f.svc.AttachMST(f.kn(), a.AnchorID, "0300000000", "m2")
	wantCode(t, err, "cooperative.htx_anchor_mst_present", "gắn MST lần hai khác")
	// MST xác minh khác → 422.
	_, err = claim(f.kn(), account, "0300000000", false, "x2")
	wantCode(t, err, "cooperative.htx_anchor_mst_mismatch", "MST xác minh khác")
	// KN quản trị tỉnh không nhận quyền thay được (chỉ registry_admin/platform_admin).
	if _, err = claim(f.as(roleKNProvinceAdmin), account, "0312345679", false, "x3"); !errors.Is(err, problem.Forbidden) {
		t.Fatalf("vai KN tỉnh nhận quyền: %v", err)
	}
	// Chạy thử: báo cáo đầy đủ, không ghi.
	dry, err := claim(f.kn(), account, "0312345679", true, "x4")
	if err != nil || !dry.DryRun || !dry.WouldSucceed || dry.EventID != nil || len(dry.Moved) != 1 || dry.Moved[0].Counts.Memberships != 1 ||
		dry.DataCopied || dry.Confirmation.Before != "unconfirmed" || dry.Confirmation.After != "confirmed" {
		t.Fatalf("chạy thử: %+v %v", dry, err)
	}
	if g, _, _ := f.svc.Get(f.kn(), a.AnchorID); g.Status != domain.StatusProvisional {
		t.Fatalf("chạy thử đã ghi: %+v", g)
	}
	// Nhận quyền.
	rep, err := claim(f.kn(), account, "0312345679", false, "x5")
	if err != nil || rep.EventID == nil || rep.Replayed || rep.To.Party != "htx" || *rep.To.AccountRef != account || rep.From.CreatedBy != "kn" {
		t.Fatalf("nhận quyền: %+v %v", rep, err)
	}
	// Lũy đẳng: cùng khóa → phát lại; khóa khác, cùng tài khoản + Nút → phát lại; tài khoản khác → 409.
	if r2, err := claim(f.kn(), account, "0312345679", false, "x5"); err != nil || !r2.Replayed || *r2.EventID != *rep.EventID {
		t.Fatalf("gọi lặp cùng khóa: %+v %v", r2, err)
	}
	if r3, err := claim(f.mkg(), account, "0312345679", false, "x6"); err != nil || !r3.Replayed || *r3.EventID != *rep.EventID {
		t.Fatalf("gọi lặp khóa khác: %+v %v", r3, err)
	}
	_, err = claim(f.kn(), ids.New(), "0312345679", false, "x7")
	wantCode(t, err, "cooperative.htx_anchor_already_claimed", "tài khoản khác nhận lại")

	// Vết kiểm toán: ba dòng sổ theo thứ tự, dòng claimed mang details = báo cáo.
	g, evs, err := f.svc.Get(f.mkg(), a.AnchorID)
	if err != nil || g.Status != domain.StatusClaimed || g.Confirmation != domain.ConfirmationConfirmed || *g.HTXAccountRef != account {
		t.Fatalf("neo sau nhận quyền: %+v %v", g, err)
	}
	for _, tc := range []struct {
		at   time.Time
		want bool
	}{{g.ClaimedAt.Add(-time.Nanosecond), false}, {*g.ClaimedAt, true}, {g.ClaimedAt.Add(time.Hour), true}} {
		got, e := f.svc.ConfirmedAt(f.kn(), []uuid.UUID{org}, tc.at)
		if e != nil || got[org] != tc.want {
			t.Fatalf("historical confirmation at %s want %v got %v err %v", tc.at, tc.want, got, e)
		}
	}
	var acts []string
	for _, e := range evs {
		acts = append(acts, e.Action)
	}
	if len(acts) != 3 || acts[0] != "provisional_created" || acts[1] != "mst_attached" || acts[2] != "claimed" {
		t.Fatalf("sổ: %v", acts)
	}
	var audit ClaimReport
	if err := json.Unmarshal(evs[2].Details, &audit); err != nil || audit.Moved[0].OrgRef != org || audit.Moved[0].Counts.Memberships != 1 ||
		*evs[2].VerificationRef != "CV-123/TCT" {
		t.Fatalf("details: %s %v", evs[2].Details, err)
	}
	// Chuỗi băm của sổ neo sạch (chỉ ghi thêm).
	tx, err := f.admin.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(context.Background(), `SELECT set_config('app.tenant_id', $1, true)`, f.tid.String()); err != nil {
		t.Fatal(err)
	}
	if v, err := ledger.Verify(context.Background(), tx, "pf_coop.htx_anchor_event", f.tid, "anchor:"+a.AnchorID.String()); err != nil ||
		!v.OK || v.RowsChecked != 3 {
		t.Fatalf("verify_chain: %+v %v", v, err)
	}
}

func TestClaimRollbackOnFailure_D2(t *testing.T) {
	f := newFx(t)
	prov, target := f.coop("0311111111"), f.coop("")
	a, _, err := f.svc.Create(f.mkg(), CreateInput{OrgRef: prov, CreatedBy: domain.CreatedByMKG, MST: "0311111111", IdemKey: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("lỗi giả sau khi ghi")
	f.svc.failAfterWrite = func() error { return boom }
	in := ClaimInput{TargetOrgRef: target, AccountRef: ids.New(), VerifiedMST: "0311111111", VerificationRef: "BB-01", IdemKey: "r2"}
	if _, err := f.svc.Claim(f.kn(), a.AnchorID, in); !errors.Is(err, boom) {
		t.Fatalf("muốn lỗi giả: %v", err)
	}
	// Không còn dấu vết: trạng thái, sổ, bí danh, khóa nhận quyền đều như trước.
	g, evs, _ := f.svc.Get(f.kn(), a.AnchorID)
	if g.Status != domain.StatusProvisional || g.OrgRef != prov || len(evs) != 1 || len(g.Aliases) != 1 {
		t.Fatalf("sau lỗi phải lùi toàn bộ: %+v %d sự kiện", g, len(evs))
	}
	var n int
	if err := f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pf_coop.htx_anchor_claim WHERE tenant_id = $1`, f.tid).Scan(&n); err != nil || n != 0 {
		t.Fatalf("khóa nhận quyền còn: %d %v", n, err)
	}
	if cur, _ := f.svc.CurrentOrg(f.kn(), f.tid, target); cur != target {
		t.Fatalf("Nút đích không được thành bí danh: %v", cur)
	}
	// Gọi lại (cùng khóa) sau khi hết lỗi → thành công.
	f.svc.failAfterWrite = nil
	if rep, err := f.svc.Claim(f.kn(), a.AnchorID, in); err != nil || rep.Replayed || rep.AliasAdded == nil || *rep.AliasAdded != target {
		t.Fatalf("gọi lại: %+v %v", rep, err)
	}
}

func TestClaimConflictReportAliasOffline_D2(t *testing.T) {
	f := newFx(t)
	prov, busy, empty := f.coop(""), f.coop(""), f.coop("")
	f.member(prov)
	f.member(busy) // HTX thật đã tự đăng ký và có dữ liệu
	a, _, err := f.svc.Create(f.kn(), CreateInput{OrgRef: prov, CreatedBy: domain.CreatedByKN, MST: "0322222222", IdemKey: "o1"})
	if err != nil {
		t.Fatal(err)
	}
	acc := ids.New()
	in := func(target uuid.UUID, dry bool, key string) ClaimInput {
		return ClaimInput{TargetOrgRef: target, AccountRef: acc, VerifiedMST: "0322222222", VerificationRef: "VB-9", DryRun: dry, IdemKey: key}
	}
	dry, err := f.svc.Claim(f.kn(), a.AnchorID, in(busy, true, "o2"))
	if err != nil || dry.WouldSucceed || len(dry.Conflicts) != 1 || dry.Conflicts[0].Reason != "target_has_data" ||
		dry.Conflicts[0].Counts.Memberships != 1 {
		t.Fatalf("báo cáo xung đột chạy thử: %+v %v", dry, err)
	}
	_, err = f.svc.Claim(f.kn(), a.AnchorID, in(busy, false, "o3"))
	wantCode(t, err, "cooperative.htx_anchor_claim_conflict", "nhận quyền vào Nút có dữ liệu")
	var pe *problem.Problem
	if !errors.As(err, &pe) || pe.Extra["conflict_report"] == nil {
		t.Fatalf("409 phải kèm conflict_report: %+v", pe)
	}

	// Trạng thái xác nhận trước nhận quyền: cả hai Nút "Chưa xác nhận", vẫn có trong danh sách mặc định.
	conf, err := f.svc.Confirmed(f.kn(), []uuid.UUID{prov, empty})
	if err != nil || len(conf) != 2 || conf[prov] || conf[empty] {
		t.Fatalf("trước nhận quyền: %v", conf)
	}
	// Bản ghi ngoại tuyến tạo dưới Nút tạm (trước nhận quyền) — đồng bộ sau nhận quyền phân giải qua bí danh.
	if cur, _ := f.svc.CurrentOrg(f.kn(), f.tid, prov); cur != prov {
		t.Fatalf("trước nhận quyền Nút tạm là chính nó: %v", cur)
	}
	rep, err := f.svc.Claim(f.kn(), a.AnchorID, in(empty, false, "o4"))
	if err != nil || !rep.WouldSucceed || rep.AliasAdded == nil || rep.From.OrgRef != prov || rep.To.OrgRef != empty {
		t.Fatalf("nhận quyền vào Nút trống: %+v %v", rep, err)
	}
	if cur, _ := f.svc.CurrentOrg(f.kn(), f.tid, prov); cur != empty {
		t.Fatalf("sau nhận quyền Nút tạm phải phân giải về Nút HTX thật: %v", cur)
	}
	if cur, _ := f.svc.CurrentOrg(f.kn(), f.tid, busy); cur != busy {
		t.Fatalf("Nút không thuộc neo giữ nguyên: %v", cur)
	}
	conf, err = f.svc.Confirmed(f.kn(), []uuid.UUID{prov, empty, busy})
	if err != nil || !conf[prov] || !conf[empty] || conf[busy] {
		t.Fatalf("sau nhận quyền: %v", conf)
	}

	// Đường đọc hồ sơ HTX (danh sách KN, báo cáo của pf/party): mang trạng thái, lọc được, mặc định gồm cả HTX tạm,
	// dữ liệu không bị sao chép (số dòng xã viên giữ nguyên dưới Nút tạm).
	err = tenant.WithTenantTx(f.kn(), f.svc.DB, f.tid, func(tx pgx.Tx) error {
		ps, err := memberstore.ProfilesByRefs(context.Background(), tx, []uuid.UUID{prov, empty, busy})
		if err != nil {
			return err
		}
		if len(ps) != 3 || !ps[prov].Confirmed || !ps[empty].Confirmed || ps[busy].Confirmed {
			t.Errorf("hồ sơ mang trạng thái xác nhận: %+v", ps)
		}
		all, err := memberstore.MatchProfiles(context.Background(), tx, "", nil, nil, "")
		if err != nil {
			return err
		}
		un, err := memberstore.MatchProfiles(context.Background(), tx, "", nil, nil, domain.ConfirmationUnconfirmed)
		if err != nil {
			return err
		}
		ok, err := memberstore.MatchProfiles(context.Background(), tx, "", nil, nil, domain.ConfirmationConfirmed)
		if err != nil {
			return err
		}
		if len(all) != 3 || len(un) != 1 || un[0] != busy || len(ok) != 2 {
			t.Errorf("lọc: tất cả %v, chưa xác nhận %v, đã xác nhận %v", all, un, ok)
		}
		c, err := store.CountData(context.Background(), tx, prov)
		if err != nil {
			return err
		}
		if c.Memberships != 1 {
			t.Errorf("dữ liệu dưới Nút tạm phải giữ nguyên (không sao chép, không chuyển dòng): %+v", c)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestClaimConcurrentOnce_D2(t *testing.T) {
	f := newFx(t)
	org := f.coop("0333333333")
	a, _, err := f.svc.Create(f.mkg(), CreateInput{OrgRef: org, CreatedBy: domain.CreatedByMKG, MST: "0333333333", IdemKey: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = f.svc.Claim(f.kn(), a.AnchorID, ClaimInput{AccountRef: ids.New(), VerifiedMST: "0333333333",
				VerificationRef: "SS-1", IdemKey: "p-" + ids.New().String()})
		}(i)
	}
	wg.Wait()
	okN := 0
	for _, e := range errs {
		switch {
		case e == nil:
			okN++
		case code(e) == "cooperative.htx_anchor_already_claimed":
		default:
			t.Errorf("lỗi lạ: %v", e)
		}
	}
	if okN != 1 {
		t.Fatalf("đúng một lần nhận quyền thành công, có %d", okN)
	}
}

func TestBackfillLegacy_D2(t *testing.T) {
	f := newFx(t)
	withTax, noTax := f.coop("0344444444"), f.coop("")
	dry, err := f.svc.Backfill(f.mkg(), true, 0)
	if err != nil || dry.Created != 0 || dry.WithMST != 1 || dry.TempCode != 1 || len(dry.Items) != 2 {
		t.Fatalf("chạy thử: %+v %v", dry, err)
	}
	if _, err := f.svc.Backfill(f.as(roleKNProvinceAdmin), false, 0); !errors.Is(err, problem.Forbidden) {
		t.Fatalf("KN tỉnh chạy neo hàng loạt: %v", err)
	}
	res, err := f.svc.Backfill(f.mkg(), false, 0)
	if err != nil || res.Created != 2 {
		t.Fatalf("neo: %+v %v", res, err)
	}
	as, _, err := f.svc.List(f.kn(), store.Filter{})
	if err != nil || len(as) != 2 {
		t.Fatalf("danh sách: %v %v", as, err)
	}
	for _, x := range as {
		if x.Status != domain.StatusProvisional || x.CreatedByKind != "kn" || x.Confirmation != domain.ConfirmationUnconfirmed {
			t.Errorf("neo có trước D2 phải là HTX tạm do KN lập: %+v", x)
		}
		if x.OrgRef == withTax && (x.MST == nil || *x.MST != "0344444444") {
			t.Errorf("MST hồ sơ phải thành khóa: %+v", x)
		}
		if x.OrgRef == noTax && x.MST != nil {
			t.Errorf("không MST → mã tạm: %+v", x)
		}
	}
	if again, err := f.svc.Backfill(f.mkg(), false, 0); err != nil || again.Created != 0 {
		t.Fatalf("chạy lại: %+v %v", again, err)
	}
	if as, _, _ := f.svc.List(f.kn(), store.Filter{Q: "0344 444 444"}); len(as) != 1 || as[0].OrgRef != withTax {
		t.Fatalf("tìm theo MST chưa chuẩn hóa: %v", as)
	}
}
