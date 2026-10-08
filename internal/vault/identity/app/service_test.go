package app_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/kms"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const role = "vault_app"

func newService(t *testing.T) (*app.Service, *pgtest.Env) {
	t.Helper()
	env := pgtest.Get(t)
	k, err := kms.NewLocal("test-1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := &app.Service{DB: env.Pool(t, migrations.Vault, role), Clock: clock.System{}, KMS: k, BlindKey: bytes.Repeat([]byte{0x17}, 32)}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s, env
}

func as(tid uuid.UUID, mfa bool, roles ...string) context.Context {
	return authz.WithPrincipal(context.Background(), authz.Principal{TenantID: tid, ActorRef: ids.New(), Roles: roles, MFA: mfa})
}

var allRoles = []string{client.RoleRegistrar, client.RoleEvidenceViewer, client.RoleEraser}

func sample(phone string) client.Identity {
	return client.Identity{
		Kind: client.SubjectHousehold, FullName: "Nguyễn Văn Thành", Phone: phone, NationalID: "",
		Address: "Ấp 3, xã Tân Phú Đông, Tiền Giang", AreaCode: "82-28783",
	}
}

func code(err error) string {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

// R-42: mọi thao tác đọc thiếu mục đích → vault.purpose_required, không chạm DB.
func TestPurposeRequired(t *testing.T) {
	s, _ := newService(t)
	ctx := as(ids.New(), true, allRoles...)
	c, err := s.Create(ctx, client.PurposeRegistryAdmin, sample("0901000001"))
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]error{}
	_, checks["create"] = s.Create(ctx, "", sample("0901000002"))
	_, checks["display"] = s.Display(ctx, "", c.Ref)
	_, checks["resolve"] = s.Resolve(ctx, "", c.Ref)
	_, checks["lookup"] = s.Lookup(ctx, "", "0901000001")
	_, checks["unwrap"] = s.Unwrap(ctx, "", c.Ref, make([]byte, 80))
	for op, err := range checks {
		if !errors.Is(err, client.ErrNoPurpose) || code(err) != client.CodePurposeRequired {
			t.Errorf("%s thiếu mục đích: %v", op, err)
		}
	}
	if _, err := s.Resolve(ctx, "Sai Dạng", c.Ref); code(err) != "validation_failed" {
		t.Fatalf("mục đích sai dạng: %v", err)
	}
	// Local adapter chặn sớm như nhau.
	if _, err := client.NewLocal(s).Resolve(ctx, "", c.Ref); !errors.Is(err, client.ErrNoPurpose) {
		t.Fatalf("NewLocal: %v", err)
	}
}

func TestRoles(t *testing.T) {
	s, _ := newService(t)
	tid := ids.New()
	c, err := s.Create(as(tid, false, client.RoleRegistrar), client.PurposeRegistryAdmin, sample("0901000010"))
	if err != nil {
		t.Fatal(err)
	}
	display := as(tid, false, client.RoleDisplay)
	if _, err := s.Display(display, client.PurposeContact, c.Ref); err != nil {
		t.Fatalf("vai display phải xem được nhãn: %v", err)
	}
	if _, err := s.Resolve(display, client.PurposeContact, c.Ref); code(err) != "forbidden" {
		t.Fatalf("vai display không được resolve: %v", err)
	}
	if _, err := s.Create(display, client.PurposeRegistryAdmin, sample("0901000011")); code(err) != "forbidden" {
		t.Fatalf("vai display không được tạo: %v", err)
	}
	if _, err := s.Unwrap(as(tid, false, client.RoleRegistrar), client.PurposeMRVEvidence, c.Ref, make([]byte, 80)); code(err) != "forbidden" {
		t.Fatalf("unwrap cần vai evidence: %v", err)
	}
	if err := s.Erase(as(tid, false, client.RoleEraser), c.Ref, "yêu cầu xóa của chủ hộ"); code(err) != "mfa_required" {
		t.Fatalf("xóa không MFA phải bị chặn: %v", err)
	}
	if err := s.Erase(as(tid, true, client.RoleRegistrar), c.Ref, "yêu cầu xóa của chủ hộ"); code(err) != "forbidden" {
		t.Fatalf("xóa cần vai eraser: %v", err)
	}
	if _, err := s.PubKeys(as(tid, false, client.RolePubKeySync), []string{"82-28783"}); err != nil {
		t.Fatalf("vai pubkey_sync: %v", err)
	}
}

func TestCreateResolveDisplayUnwrap(t *testing.T) {
	s, env := newService(t)
	tid := ids.New()
	ctx := as(tid, true, allRoles...)
	in := sample("+84 901 000 020")
	in.NationalID = "079 123 456 789"
	c, err := s.Create(ctx, client.PurposeRegistryAdmin, in)
	if err != nil {
		t.Fatal(err)
	}
	if c.Ref.Version() != 7 || len(c.PublicKey) != 32 {
		t.Fatalf("Created: %v %d", c.Ref, len(c.PublicKey))
	}
	got, err := s.Resolve(ctx, client.PurposeRegistryAdmin, c.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.FullName != "Nguyễn Văn Thành" || got.Phone != "84901000020" || got.NationalID != "079123456789" || got.Address != in.Address || got.AreaCode != in.AreaCode || got.Kind != client.SubjectHousehold {
		t.Fatalf("resolve lệch dữ liệu vào (đã chuẩn hóa)")
	}
	d, err := s.Display(ctx, client.PurposeFieldSupport, c.Ref)
	if err != nil || d.Label != "Nguyễn V. T." || d.MaskedPhone != "0901•••020" {
		t.Fatalf("display: %v", err)
	}
	for _, v := range []string{"0901000020", "079123456789"} {
		ref, err := s.Lookup(ctx, client.PurposeDedup, v)
		if err != nil || ref != c.Ref {
			t.Fatalf("lookup theo %s: %v", v, err)
		}
	}
	if _, err := s.Lookup(ctx, client.PurposeDedup, "0909999999"); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("lookup không có: %v", err)
	}

	pub, _ := envelope.Key32(c.PublicKey)
	sealed, err := envelope.Seal([]byte("ảnh minh chứng"), pub)
	if err != nil {
		t.Fatal(err)
	}
	dek, err := s.Unwrap(ctx, client.PurposeMRVEvidence, c.Ref, sealed.WrappedDEK)
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := envelope.DecryptWithDEK(dek, sealed.Nonce, sealed.Ciphertext); err != nil || string(pt) != "ảnh minh chứng" {
		t.Fatal("DEK mở được phải giải mã được ảnh")
	}
	bad := bytes.Clone(sealed.WrappedDEK)
	bad[40] ^= 1
	if _, err := s.Unwrap(ctx, client.PurposeMRVEvidence, c.Ref, bad); code(err) != client.CodeUnwrapFailed {
		t.Fatalf("wrapped_dek hỏng: %v", err)
	}

	// Mã hóa lưu trữ: bản mã trong DB không chứa giá trị rõ.
	admin := env.AdminPool(t, migrations.Vault)
	var name, phone, priv []byte
	if err := admin.QueryRow(context.Background(), `SELECT i.full_name_ct, i.phone_ct, k.wrapped_private_key
	    FROM vault.identity i JOIN vault.subject_key k USING (tenant_id, subject_ref) WHERE i.subject_ref = $1`, c.Ref).Scan(&name, &phone, &priv); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(name, []byte("Thành")) || bytes.Contains(phone, []byte("901000020")) || len(priv) < 32+28 {
		t.Fatal("dữ liệu định danh phải được mã hóa lưu trữ")
	}
}

// BR RB-3: xóa định danh → hủy khóa bí mật, xóa định danh và chỉ mục mù, giữ dấu mộ.
func TestErasure(t *testing.T) {
	s, env := newService(t)
	tid := ids.New()
	ctx := as(tid, true, allRoles...)
	in := sample("0901000030")
	in.NationalID = "001234567890"
	c, err := s.Create(ctx, client.PurposeRegistryAdmin, in)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := envelope.Key32(c.PublicKey)
	sealed, _ := envelope.Seal([]byte("ảnh"), pub)
	if _, err := s.Unwrap(ctx, client.PurposeMRVEvidence, c.Ref, sealed.WrappedDEK); err != nil {
		t.Fatal(err)
	}
	if err := s.Erase(ctx, c.Ref, "ngắn"); code(err) != "validation_failed" {
		t.Fatalf("lý do quá ngắn: %v", err)
	}
	if err := s.Erase(ctx, c.Ref, "Chủ hộ yêu cầu xóa dữ liệu theo NĐ 13"); err != nil {
		t.Fatal(err)
	}
	if err := s.Erase(ctx, c.Ref, "Chủ hộ yêu cầu xóa dữ liệu theo NĐ 13"); err != nil {
		t.Fatalf("xóa lặp phải thành công: %v", err)
	}
	if _, err := s.Unwrap(ctx, client.PurposeMRVEvidence, c.Ref, sealed.WrappedDEK); !errors.Is(err, client.ErrErased) || code(err) != client.CodeErased {
		t.Fatalf("unwrap sau xóa: %v", err)
	}
	if _, err := s.Resolve(ctx, client.PurposeRegistryAdmin, c.Ref); !errors.Is(err, client.ErrErased) {
		t.Fatalf("resolve sau xóa: %v", err)
	}
	d, err := s.Display(ctx, client.PurposeContact, c.Ref)
	if !errors.Is(err, client.ErrErased) || d.Label != client.ErasedLabel || d.MaskedPhone != "" {
		t.Fatalf("display sau xóa: %+v %v", d, err)
	}
	if _, err := s.Lookup(ctx, client.PurposeDedup, "0901000030"); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("lookup sau xóa: %v", err)
	}
	keys, err := s.PubKeys(ctx, []string{in.AreaCode})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if k.Ref == c.Ref {
			t.Fatal("khóa công khai của chủ thể đã xóa không được đồng bộ xuống thiết bị")
		}
	}
	admin := env.AdminPool(t, migrations.Vault)
	var nIdentity, nKey, nBlind int
	var erased bool
	if err := admin.QueryRow(context.Background(), `SELECT
	    (SELECT count(*) FROM vault.identity WHERE subject_ref = $1),
	    (SELECT count(*) FROM vault.subject_key WHERE subject_ref = $1),
	    (SELECT count(*) FROM vault.blind_index WHERE subject_ref = $1),
	    (SELECT erased_at IS NOT NULL FROM vault.subject WHERE ref = $1)`, c.Ref).Scan(&nIdentity, &nKey, &nBlind, &erased); err != nil {
		t.Fatal(err)
	}
	if nIdentity != 0 || nKey != 0 || nBlind != 0 || !erased {
		t.Fatalf("sau xóa: identity %d, key %d, blind %d, erased %v", nIdentity, nKey, nBlind, erased)
	}
	// SĐT được đăng ký lại cho chủ thể mới sau khi xóa.
	c2, err := s.Create(ctx, client.PurposeRegistryAdmin, in)
	if err != nil || c2.Ref == c.Ref {
		t.Fatalf("đăng ký lại sau xóa: %v", err)
	}
	if err := s.Erase(ctx, ids.New(), "Chủ hộ yêu cầu xóa dữ liệu theo NĐ 13"); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("xóa chủ thể không có: %v", err)
	}
}

// Chỉ mục mù duy nhất theo tenant: trùng SĐT/CCCD cùng tenant → ErrDuplicate + ref cũ; tenant khác được.
func TestBlindIndexDuplicatePerTenant(t *testing.T) {
	s, _ := newService(t)
	tidA, tidB := ids.New(), ids.New()
	a := as(tidA, false, client.RoleRegistrar)
	first, err := s.Create(a, client.PurposeRegistryAdmin, sample("0901000040"))
	if err != nil {
		t.Fatal(err)
	}
	dup, err := s.Create(a, client.PurposeDedup, client.Identity{Kind: client.SubjectPerson, FullName: "Người khác", Phone: "84 901 000 040", AreaCode: "x"})
	if !errors.Is(err, client.ErrDuplicate) || code(err) != client.CodeDuplicate || dup.Ref != first.Ref || !bytes.Equal(dup.PublicKey, first.PublicKey) {
		t.Fatalf("trùng SĐT cùng tenant: %v %v", dup.Ref, err)
	}
	withNID := sample("0901000041")
	withNID.NationalID = "079000000001"
	c3, err := s.Create(a, client.PurposeRegistryAdmin, withNID)
	if err != nil {
		t.Fatal(err)
	}
	if d, err := s.Create(a, client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectPerson, FullName: "X", NationalID: "079000000001", AreaCode: "x"}); !errors.Is(err, client.ErrDuplicate) || d.Ref != c3.Ref {
		t.Fatalf("trùng CCCD: %v", err)
	}
	b := as(tidB, false, client.RoleRegistrar)
	other, err := s.Create(b, client.PurposeRegistryAdmin, sample("0901000040"))
	if err != nil || other.Ref == first.Ref {
		t.Fatalf("cùng SĐT ở tenant khác phải tạo được: %v", err)
	}
	if _, err := s.Create(a, client.PurposeRegistryAdmin, sample("12345")); code(err) != "validation_failed" {
		t.Fatalf("SĐT sai dạng: %v", err)
	}
}

// Nhiều request song song cùng SĐT: đúng một chủ thể, các bên còn lại nhận ErrDuplicate với cùng ref.
func TestConcurrentCreateSamePhone(t *testing.T) {
	s, _ := newService(t)
	ctx := as(ids.New(), false, client.RoleRegistrar)
	const n = 8
	refs := make([]uuid.UUID, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := s.Create(ctx, client.PurposeRegistryAdmin, sample("0901000050"))
			refs[i], errs[i] = c.Ref, err
		}()
	}
	wg.Wait()
	created := 0
	for i := range n {
		switch {
		case errs[i] == nil:
			created++
		case !errors.Is(errs[i], client.ErrDuplicate):
			t.Fatalf("lỗi lạ: %v", errs[i])
		}
		if refs[i] != refs[0] {
			t.Fatal("mọi bên phải nhận cùng ref")
		}
	}
	if created != 1 {
		t.Fatalf("phải tạo đúng 1 chủ thể, tạo %d", created)
	}
}

// R-42: mỗi thao tác ghi một dòng sổ truy cập (mục đích, người yêu cầu), chuỗi băm hợp lệ, không có PII.
func TestAccessLogLedger(t *testing.T) {
	s, env := newService(t)
	tid := ids.New()
	ctx := as(tid, true, allRoles...)
	actor, _ := authz.FromContext(ctx)
	c, err := s.Create(ctx, client.PurposeRegistryAdmin, sample("0901000060"))
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := envelope.Key32(c.PublicKey)
	sealed, _ := envelope.Seal([]byte("x"), pub)
	_, _ = s.Display(ctx, client.PurposeContact, c.Ref)
	_, _ = s.Resolve(ctx, client.PurposeFieldSupport, c.Ref)
	_, _ = s.Lookup(ctx, client.PurposeDedup, "0901000060")
	_, _ = s.Unwrap(ctx, client.PurposeMRVEvidence, c.Ref, sealed.WrappedDEK)
	if err := s.Erase(ctx, c.Ref, "Chủ hộ yêu cầu xóa dữ liệu theo NĐ 13"); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Resolve(ctx, client.PurposeFieldSupport, c.Ref) // sau xóa: ghi outcome = erased
	_, _ = s.Lookup(ctx, client.PurposeDedup, "0909876543")  // không thấy: chuỗi miss

	var rows []store.Access
	err = tenant.WithTenantTx(context.Background(), s.DB, tid, func(tx pgx.Tx) error {
		var e error
		if rows, e = store.AccessBySubject(context.Background(), tx, c.Ref); e != nil {
			return e
		}
		for _, stream := range []string{"subject:" + c.Ref.String(), "miss"} {
			r, e := ledger.Verify(context.Background(), tx, "vault.access_log", tid, stream)
			if e != nil {
				return e
			}
			if !r.OK {
				t.Errorf("chuỗi %s đứt: %+v", stream, r)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ op, purpose, outcome string }{
		{"create", "registry.admin", "ok"}, {"display", "extension.contact", "ok"}, {"resolve", "extension.field_support", "ok"},
		{"lookup", "registry.dedup", "ok"}, {"unwrap", "mrv.evidence", "ok"}, {"erase", "registry.erasure", "ok"},
		{"resolve", "extension.field_support", "erased"},
	}
	if len(rows) != len(want) {
		t.Fatalf("số dòng sổ = %d, muốn %d", len(rows), len(want))
	}
	for i, w := range want {
		r := rows[i]
		if r.Op != w.op || r.Purpose != w.purpose || r.Outcome != w.outcome || r.ActorRef != actor.ActorRef || r.Seq != int64(i+1) {
			t.Errorf("dòng %d: %s/%s/%s seq %d", i, r.Op, r.Purpose, r.Outcome, r.Seq)
		}
	}
	// Sổ không chứa giá trị định danh.
	admin := env.AdminPool(t, migrations.Vault)
	var dump string
	if err := admin.QueryRow(context.Background(), "SELECT coalesce(string_agg(to_jsonb(a)::text, ' '), '') FROM vault.access_log a WHERE tenant_id = $1", tid).Scan(&dump); err != nil {
		t.Fatal(err)
	}
	for _, pii := range []string{"0901000060", "84901000060", "Thành", "Tân Phú", "0909876543"} {
		if strings.Contains(dump, pii) {
			t.Fatalf("sổ truy cập chứa dữ liệu cá nhân %q", pii)
		}
	}
	// Sổ chỉ ghi thêm: vai ứng dụng không sửa được.
	err = tenant.WithTenantTx(context.Background(), s.DB, tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "UPDATE vault.access_log SET purpose = 'registry.admin'")
		return e
	})
	if err == nil {
		t.Fatal("UPDATE sổ truy cập phải thất bại")
	}
}

// RB-2: tenant khác không thấy chủ thể, khóa, chỉ mục mù.
func TestTenantIsolation(t *testing.T) {
	s, _ := newService(t)
	a := as(ids.New(), true, allRoles...)
	b := as(ids.New(), true, append(allRoles, client.RoleDisplay)...)
	c, err := s.Create(a, client.PurposeRegistryAdmin, sample("0901000070"))
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := envelope.Key32(c.PublicKey)
	sealed, _ := envelope.Seal([]byte("x"), pub)
	if _, err := s.Display(b, client.PurposeContact, c.Ref); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("display chéo tenant: %v", err)
	}
	if _, err := s.Resolve(b, client.PurposeContact, c.Ref); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("resolve chéo tenant: %v", err)
	}
	if _, err := s.Unwrap(b, client.PurposeMRVEvidence, c.Ref, sealed.WrappedDEK); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("unwrap chéo tenant: %v", err)
	}
	if _, err := s.Lookup(b, client.PurposeDedup, "0901000070"); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("lookup chéo tenant: %v", err)
	}
	if err := s.Erase(b, c.Ref, "thử xóa chéo tenant xem sao"); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("erase chéo tenant: %v", err)
	}
	keys, err := s.PubKeys(b, []string{"82-28783"})
	if err != nil || len(keys) != 0 {
		t.Fatalf("pubkeys chéo tenant: %d %v", len(keys), err)
	}
	if _, err := s.Resolve(a, client.PurposeContact, c.Ref); err != nil {
		t.Fatalf("chủ sở hữu vẫn đọc được: %v", err)
	}
}

func TestPubKeysPaging(t *testing.T) {
	s, _ := newService(t)
	ctx := as(ids.New(), false, client.RoleRegistrar)
	var made []uuid.UUID
	for i := range 5 {
		in := sample("")
		in.AreaCode = "xa-page"
		in.FullName = "Hộ số " + string(rune('A'+i))
		c, err := s.Create(ctx, client.PurposeRegistryAdmin, in)
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, c.Ref)
	}
	p1, next, err := s.PubKeysPage(ctx, []string{"xa-page"}, uuid.Nil, 2)
	if err != nil || len(p1) != 2 || next == nil {
		t.Fatalf("trang 1: %d %v", len(p1), err)
	}
	all, err := s.PubKeys(ctx, []string{"xa-page", "khac"})
	if err != nil || len(all) != len(made) {
		t.Fatalf("tất cả: %d %v", len(all), err)
	}
	if _, _, err := s.PubKeysPage(ctx, nil, uuid.Nil, 10); code(err) != "validation_failed" {
		t.Fatalf("thiếu area: %v", err)
	}
}

// HTX US-LOG-01 (ảnh nhật ký mã hóa trên máy nông dân, R-40): vai farmer đọc khóa công
// khai THEO REF nông hộ, không liệt kê theo địa bàn; chủ thể người, đã xóa, tenant khác và
// ref lạ bị bỏ qua; 1–50 ref.
func TestHouseholdPubKeysFarmer(t *testing.T) {
	s, _ := newService(t)
	tid := ids.New()
	reg := as(tid, true, client.RoleRegistrar, client.RoleEraser)
	mk := func(kind client.SubjectKind, phone string) uuid.UUID {
		in := sample(phone)
		in.Kind = kind
		c, err := s.Create(reg, client.PurposeRegistryAdmin, in)
		if err != nil {
			t.Fatal(err)
		}
		return c.Ref
	}
	h1, h2, person, erased := mk(client.SubjectHousehold, "0901000901"), mk(client.SubjectHousehold, "0901000902"),
		mk(client.SubjectPerson, "0901000903"), mk(client.SubjectHousehold, "0901000904")
	if err := s.Erase(reg, erased, "yêu cầu xóa của chủ hộ theo RB-3"); err != nil {
		t.Fatal(err)
	}
	other, err := s.Create(as(ids.New(), false, client.RoleRegistrar), client.PurposeRegistryAdmin, sample("0901000905"))
	if err != nil {
		t.Fatal(err)
	}
	farmer := as(tid, false, client.RoleFarmer)
	keys, err := s.HouseholdPubKeys(farmer, []uuid.UUID{h2, h1, person, erased, other.Ref, ids.New(), h1})
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || len(keys[0].PublicKey) != 32 || keys[0].Ref.String() > keys[1].Ref.String() {
		t.Fatalf("chỉ 2 nông hộ của tenant, chưa xóa, sắp theo ref: %+v", keys)
	}
	for _, k := range keys {
		if k.Ref != h1 && k.Ref != h2 {
			t.Fatalf("ref lạ: %v", k.Ref)
		}
	}
	if _, err := s.PubKeys(farmer, []string{"82-28783"}); code(err) != "forbidden" {
		t.Fatalf("farmer không được liệt kê theo địa bàn: %v", err)
	}
	if _, err := s.HouseholdPubKeys(as(tid, false, "kn_officer"), []uuid.UUID{h1}); code(err) != "forbidden" {
		t.Fatalf("vai khác không đọc được: %v", err)
	}
	if _, err := s.HouseholdPubKeys(farmer, nil); code(err) != "validation_failed" {
		t.Fatalf("thiếu ref: %v", err)
	}
	many := make([]uuid.UUID, client.MaxPubKeyRefs+1)
	for i := range many {
		many[i] = ids.New()
	}
	if _, err := s.HouseholdPubKeys(farmer, many); code(err) != "validation_failed" {
		t.Fatalf("quá 50 ref: %v", err)
	}
	if got, err := client.NewLocal(s).HouseholdPubKeys(as(tid, false, client.RolePubKeySync), []uuid.UUID{h1}); err != nil || len(got) != 1 {
		t.Fatalf("vai đọc khóa qua NewLocal: %v %v", got, err)
	}
}

// RB-1/RB-2: cổng catalog của cụm vault phải sạch — access_log là sổ đủ trigger,
// các bảng xóa được không bị coi là sổ, mọi bảng có RLS.
func TestCatalogGates(t *testing.T) {
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.Vault)
	for _, view := range []string{"shared.v_missing_immutability", "shared.v_missing_rls"} {
		var probs []string
		rows, err := admin.Query(context.Background(), "SELECT schema_name || '.' || table_name || ': ' || problem FROM "+view)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				t.Fatal(err)
			}
			probs = append(probs, p)
		}
		rows.Close()
		if len(probs) > 0 {
			t.Errorf("%s không rỗng: %v", view, probs)
		}
	}
}

// M13: mục đích đúng dạng nhưng ngoài danh mục của thao tác → 422, không chạm DB.
func TestPurposeWhitelist(t *testing.T) {
	s, _ := newService(t)
	ctx := as(ids.New(), true, allRoles...)
	if _, err := s.Lookup(ctx, client.Purpose("marketing.campaign"), "0901000801"); code(err) != "validation_failed" {
		t.Fatalf("lookup mục đích lạ: %v", err)
	}
	if _, err := s.Lookup(ctx, client.PurposeContact, "0901000801"); code(err) != "validation_failed" {
		t.Fatalf("lookup với extension.contact phải bị từ chối: %v", err)
	}
	if _, err := s.Resolve(ctx, client.PurposeMRVEvidence, ids.New()); code(err) != "validation_failed" {
		t.Fatalf("resolve với mrv.evidence phải bị từ chối: %v", err)
	}
	if _, err := s.Unwrap(ctx, client.PurposeRegistryAdmin, ids.New(), make([]byte, 80)); code(err) != "validation_failed" {
		t.Fatalf("unwrap với registry.admin phải bị từ chối: %v", err)
	}
}

// M13: hạn mức theo người yêu cầu — vượt → 429 vault.quota_exceeded; người khác không bị ảnh hưởng.
func TestActorQuota(t *testing.T) {
	s, _ := newService(t)
	clk := clock.NewFixed(time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC))
	s.Quota = app.NewQuota(clk, nil, 60, 3) // 1 lượt/phút, dồn 3
	tid := ids.New()
	reg := as(tid, true, client.RoleRegistrar)
	c, err := s.Create(reg, client.PurposeRegistryAdmin, sample("0901000811"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if _, err := s.Lookup(reg, client.PurposeDedup, "0901000811"); err != nil {
			t.Fatalf("lượt %d: %v", i, err)
		}
	}
	err = func() error { _, e := s.Lookup(reg, client.PurposeDedup, "0901000811"); return e }()
	if code(err) != client.CodeQuotaExceeded || problem.From(err).Status != 429 {
		t.Fatalf("lượt thứ 4 phải 429 vault.quota_exceeded: %v", err)
	}
	// Resolve có bucket riêng theo thao tác.
	if _, err := s.Resolve(reg, client.PurposeRegistryAdmin, c.Ref); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// Người yêu cầu khác cùng tenant không bị ảnh hưởng.
	if _, err := s.Lookup(as(tid, true, client.RoleRegistrar), client.PurposeDedup, "0901000811"); err != nil {
		t.Fatalf("người khác: %v", err)
	}
	clk.Advance(time.Minute)
	if _, err := s.Lookup(reg, client.PurposeDedup, "0901000811"); err != nil {
		t.Fatalf("sau một phút phải có lại lượt: %v", err)
	}
}

// M14 (LOW): RequireMFASensitive → Resolve/Unwrap đòi MFA như Erase; Display/Lookup không.
func TestRequireMFASensitive(t *testing.T) {
	s, _ := newService(t)
	s.RequireMFASensitive = true
	tid := ids.New()
	noMFA := as(tid, false, allRoles...)
	c, err := s.Create(noMFA, client.PurposeRegistryAdmin, sample("0901000821"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(noMFA, client.PurposeRegistryAdmin, c.Ref); code(err) != "mfa_required" {
		t.Fatalf("resolve không MFA: %v", err)
	}
	if _, err := s.Unwrap(noMFA, client.PurposeMRVEvidence, c.Ref, make([]byte, 80)); code(err) != "mfa_required" {
		t.Fatalf("unwrap không MFA: %v", err)
	}
	if _, err := s.Lookup(noMFA, client.PurposeDedup, "0901000821"); err != nil {
		t.Fatalf("lookup không đòi MFA: %v", err)
	}
	if _, err := s.Resolve(as(tid, true, allRoles...), client.PurposeRegistryAdmin, c.Ref); err != nil {
		t.Fatalf("resolve có MFA: %v", err)
	}
}
