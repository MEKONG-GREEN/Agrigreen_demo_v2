package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const (
	unitA = "30001"
	unitB = "30002"
	mod   = "pf/cooperative"
)

// fakeResolver là module chủ giả: chủ thể theo (loại, mã) → đơn vị hành chính; loại "season" không hỗ trợ.
type fakeResolver struct {
	mu    sync.Mutex
	known map[uuid.UUID]port.Resolved
	calls int
}

func (r *fakeResolver) Resolve(ctx context.Context, typ string, id uuid.UUID) (port.Resolved, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if _, err := tenant.FromContext(ctx); err != nil {
		return port.Resolved{}, err
	}
	if typ == "season" {
		return port.Resolved{}, port.ErrSubjectTypeUnsupported
	}
	res, ok := r.known[id]
	if !ok {
		return port.Resolved{}, port.ErrSubjectNotFound
	}
	return res, nil
}

func (r *fakeResolver) set(id uuid.UUID, res *port.Resolved) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if res == nil {
		delete(r.known, id)
		return
	}
	r.known[id] = *res
}

// fakeScope: phạm vi theo org_ref của người gọi.
type fakeScope map[uuid.UUID]authz.Scope

func (f fakeScope) Scope(ctx context.Context) (authz.Scope, error) {
	p, _ := authz.FromContext(ctx)
	return f[p.OrgRef], nil
}

type fakeConsent struct{ denied map[uuid.UUID]bool }

func (f fakeConsent) HasConsent(_ context.Context, subject uuid.UUID, _ string) (bool, error) {
	return !f.denied[subject], nil
}

type fx struct {
	t       *testing.T
	svc     *Service
	own     tenant.Beginner
	res     *fakeResolver
	vault   *clienttest.Fake
	consent fakeConsent
	objects *objstore.Local
	tid     uuid.UUID
	orgA    uuid.UUID // phạm vi unitA
	orgB    uuid.UUID // phạm vi unitB
}

func newFx(t *testing.T) *fx {
	env := pgtest.Get(t)
	objs, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{PresignSecret: []byte("presign-secret-kiem-thu-0123456789"), BaseURL: "http://objstore.test"})
	if err != nil {
		t.Fatal(err)
	}
	f := &fx{t: t, own: db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), DBModule),
		res: &fakeResolver{known: map[uuid.UUID]port.Resolved{}}, vault: clienttest.New(),
		consent: fakeConsent{denied: map[uuid.UUID]bool{}}, objects: objs, tid: ids.New(), orgA: ids.New(), orgB: ids.New()}
	f.svc = &Service{DB: f.own, Clock: clock.NewFixed(time.Now()), Deps: Deps{Store: objs, Vault: f.vault,
		Scope: fakeScope{f.orgA: {Units: []string{unitA}}, f.orgB: {Units: []string{unitB}}}, Consent: f.consent,
		Resolvers: map[string]port.Resolver{mod: f.res}}}
	return f
}

// as là context của người gọi trong tenant tid (như middleware xác thực: Principal + tenant).
func (f *fx) as(tid, org uuid.UUID, roles ...string) context.Context {
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: tid, OrgRef: org, Roles: roles})
	return tenant.WithID(ctx, tid)
}

var phoneSeq atomic.Int64

// seal mã hóa ảnh cho một hộ của kho định danh giả.
func (f *fx) seal(ctx context.Context, plaintext []byte) (envelope.Metadata, []byte) {
	f.t.Helper()
	cr, err := f.vault.Create(ctx, client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectHousehold,
		FullName: "Lê Văn C", Phone: fmt.Sprintf("09%08d", phoneSeq.Add(1)), AreaCode: unitA})
	if err != nil {
		f.t.Fatal(err)
	}
	pub, _ := envelope.Key32(cr.PublicKey)
	s, err := envelope.Seal(plaintext, pub)
	if err != nil {
		f.t.Fatal(err)
	}
	return envelope.NewMetadata(cr.Ref.String(), s, "image/jpeg", time.Now().Add(-time.Minute).Format(time.RFC3339)), s.Ciphertext
}

func (f *fx) attach(ctx context.Context, sub *port.Subject) (port.File, error) {
	m, blob := f.seal(ctx, []byte("ảnh hiện trường"))
	file, _, err := f.svc.Attach(ctx, port.AttachInput{Meta: m, Blob: blob, Subject: sub})
	return file, err
}

func (f *fx) log(tid, ref uuid.UUID) []store.Access {
	f.t.Helper()
	var out []store.Access
	if err := tenant.WithTenantTx(context.Background(), f.own, tid, func(tx pgx.Tx) error {
		var e error
		out, e = store.AccessByFile(context.Background(), tx, ref)
		return e
	}); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func code(err error) string {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

func pgCode(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return ""
}

// R-DB-07 khi ghi: chủ thể phải phân giải được qua Resolver của module chủ; bộ ba lưu cùng tệp.
func TestAttachResolvesSubject_W65(t *testing.T) {
	f := newFx(t)
	up := f.as(f.tid, f.orgA)
	plot := ids.New()
	f.res.set(plot, &port.Resolved{CommuneCode: unitA})

	sub := &port.Subject{Module: mod, Type: "plot", ID: plot}
	file, err := f.attach(up, sub)
	if err != nil {
		t.Fatal(err)
	}
	if file.Subject == nil || *file.Subject != *sub {
		t.Fatalf("bộ ba: %+v", file.Subject)
	}
	viewer := f.as(f.tid, f.orgA, client.RoleEvidenceViewer)
	got, err := f.svc.Open(viewer, file.Ref, string(client.PurposeMRVEvidence))
	if err != nil || got.File.Subject == nil || *got.File.Subject != *sub || got.URL == "" {
		t.Fatalf("đọc lại: %+v %v", got, err)
	}

	for name, c := range map[string]struct {
		sub  port.Subject
		want string
	}{
		"không có thực thể": {port.Subject{Module: mod, Type: "plot", ID: ids.New()}, ErrSubjectUnresolved.Code},
		"loại không hỗ trợ": {port.Subject{Module: mod, Type: "season", ID: plot}, ErrSubjectUnresolved.Code},
		"module chủ lạ":     {port.Subject{Module: "pf/geo", Type: "plot", ID: plot}, ErrSubjectUnresolved.Code},
		"bộ ba sai dạng":    {port.Subject{Module: "cooperative", Type: "plot", ID: plot}, problem.Validation.Code},
		"thiếu mã thực thể": {port.Subject{Module: mod, Type: "plot"}, problem.Validation.Code},
	} {
		if _, err := f.attach(up, &c.sub); code(err) != c.want {
			t.Errorf("%s: %v, muốn %s", name, err, c.want)
		}
	}
	// Tệp không có chủ thể (tải trước khi biết thực thể, như trước W6.5) vẫn nhận.
	if file, err := f.attach(up, nil); err != nil || file.Subject != nil {
		t.Fatalf("không chủ thể: %+v %v", file.Subject, err)
	}
}

// Phong bì và kho đối tượng (chuyển từ kiểm thử /v1/files của pf/sync): sai băm, wrapped_dek sai cỡ, phiên bản
// lạ bị từ chối trước khi ghi; kho giữ ĐÚNG bản mã tại files/<tenant>/<file_ref>; gửi lại cùng file_ref idempotent,
// khác bản mã → sync.file_ref_taken; mở nội dung qua kho định danh đúng một lần, có mục đích.
func TestAttachEnvelopeAndStore(t *testing.T) {
	f := newFx(t)
	up := f.as(f.tid, f.orgA)
	photo := bytes.Repeat([]byte("JPEG"), 512<<10)
	m, blob := f.seal(up, photo)

	bad := m
	bad.SHA256Ciphertext = strings.Repeat("0", 64)
	if _, _, err := f.svc.Attach(up, port.AttachInput{Meta: bad, Blob: blob}); code(err) != ErrFileChecksum.Code {
		t.Fatalf("sai băm: %v", err)
	}
	bad = m
	bad.WrappedDEK = m.WrappedDEK[:79]
	if _, _, err := f.svc.Attach(up, port.AttachInput{Meta: bad, Blob: blob}); code(err) != problem.Validation.Code {
		t.Fatalf("wrapped_dek 79 byte: %v", err)
	}
	bad = m
	bad.V, bad.SubjectRef = 2, ""
	if _, _, err := f.svc.Attach(up, port.AttachInput{Meta: bad, Blob: blob}); code(err) != problem.Validation.Code {
		t.Fatalf("v = 2, thiếu subject_ref: %v", err)
	}

	ref := ids.New().String()
	file, created, err := f.svc.Attach(up, port.AttachInput{FileRef: &ref, Meta: m, Blob: blob})
	if err != nil || !created || file.Ref.String() != ref {
		t.Fatalf("tải: %v %v", created, err)
	}
	rc, info, err := f.objects.Get(context.Background(), "files/"+f.tid.String()+"/"+ref)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(stored, blob) || bytes.Contains(stored, []byte("JPEGJPEG")) || info.ContentType != "application/octet-stream" {
		t.Fatalf("đối tượng lưu phải là bản mã (%d byte, %s)", len(stored), info.ContentType)
	}
	if _, created, err := f.svc.Attach(up, port.AttachInput{FileRef: &ref, Meta: m, Blob: blob}); err != nil || created {
		t.Fatalf("gửi lại: %v %v", created, err)
	}
	m2, blob2 := f.seal(up, []byte("ảnh khác"))
	if _, _, err := f.svc.Attach(up, port.AttachInput{FileRef: &ref, Meta: m2, Blob: blob2}); code(err) != ErrFileRefTaken.Code {
		t.Fatalf("file_ref đã dùng: %v", err)
	}

	viewer := f.as(f.tid, f.orgA, client.RoleEvidenceViewer)
	ct, pt, err := f.svc.OpenContent(viewer, file.Ref, string(client.PurposeMRVEvidence))
	if err != nil || ct != "image/jpeg" || !bytes.Equal(pt, photo) {
		t.Fatalf("nội dung giải mã: %s %v", ct, err)
	}
	var unwraps int
	for _, a := range f.vault.Accesses() {
		if a.Op == "unwrap" && a.Purpose == client.PurposeMRVEvidence && a.Ref == file.SubjectRef {
			unwraps++
		}
	}
	if unwraps != 1 {
		t.Fatalf("mở DEK phải qua kho định danh đúng một lần, có mục đích: %d", unwraps)
	}
	if _, err := f.svc.Open(viewer, ids.New(), string(client.PurposeMRVEvidence)); code(err) != ErrFileNotFound.Code {
		t.Fatalf("tệp lạ: %v", err)
	}
}

// Open(purpose): mục đích bắt buộc, người gọi phải thấy chủ thể qua Resolver + phạm vi; mọi quyết định ghi sổ.
func TestOpenChecksSubjectAndLogs_W65(t *testing.T) {
	f := newFx(t)
	up := f.as(f.tid, f.orgA)
	plot := ids.New()
	f.res.set(plot, &port.Resolved{CommuneCode: unitA})
	file, err := f.attach(up, &port.Subject{Module: mod, Type: "plot", ID: plot})
	if err != nil {
		t.Fatal(err)
	}
	purpose := string(client.PurposeMRVEvidence)
	viewerA := f.as(f.tid, f.orgA, client.RoleEvidenceViewer)
	viewerB := f.as(f.tid, f.orgB, client.RoleEvidenceViewer)

	// Mục đích bắt buộc và phải được phép; thiếu vai → forbidden. Chưa thấy tệp → không ghi sổ.
	if _, err := f.svc.Open(viewerA, file.Ref, ""); code(err) != ErrPurposeRequired.Code {
		t.Fatalf("thiếu mục đích: %v", err)
	}
	if _, err := f.svc.Open(viewerA, file.Ref, "registry.dedup"); code(err) != ErrPurposeNotAllowed.Code {
		t.Fatalf("mục đích sai: %v", err)
	}
	if _, err := f.svc.Open(up, file.Ref, purpose); code(err) != problem.Forbidden.Code {
		t.Fatalf("không có vai: %v", err)
	}
	if n := len(f.log(f.tid, file.Ref)); n != 0 {
		t.Fatalf("từ chối trước khi thấy tệp không ghi sổ: %d", n)
	}

	// Cùng xã → cấp (metadata + nội dung); xã khác → evidence.subject_forbidden.
	if _, err := f.svc.Open(viewerA, file.Ref, purpose); err != nil {
		t.Fatal(err)
	}
	ct, pt, err := f.svc.OpenContent(viewerA, file.Ref, purpose)
	if err != nil || ct != "image/jpeg" || !bytes.Equal(pt, []byte("ảnh hiện trường")) {
		t.Fatalf("nội dung: %s %v", ct, err)
	}
	if _, err := f.svc.Open(viewerB, file.Ref, purpose); code(err) != ErrSubjectForbidden.Code {
		t.Fatalf("xã khác: %v", err)
	}
	if _, _, err := f.svc.OpenContent(viewerB, file.Ref, purpose); code(err) != ErrSubjectForbidden.Code {
		t.Fatalf("xã khác (nội dung): %v", err)
	}
	// Qua cấp phép chia sẻ → cấp.
	f.res.set(plot, &port.Resolved{CommuneCode: unitA, ViaShareGrant: true})
	if _, err := f.svc.Open(viewerB, file.Ref, purpose); err != nil {
		t.Fatalf("qua cấp phép chia sẻ: %v", err)
	}
	// Hộ rút đồng ý → sync.consent_missing.
	f.consent.denied[file.SubjectRef] = true
	if _, err := f.svc.Open(viewerA, file.Ref, purpose); code(err) != ErrConsentMissing.Code {
		t.Fatalf("chưa đồng ý: %v", err)
	}
	delete(f.consent.denied, file.SubjectRef)
	// Thực thể không còn phân giải được → từ chối (fail-closed).
	f.res.set(plot, nil)
	if _, err := f.svc.Open(viewerA, file.Ref, purpose); code(err) != ErrSubjectForbidden.Code {
		t.Fatalf("chủ thể mất: %v", err)
	}

	want := []struct{ op, outcome string }{
		{domain.OpMetadata, domain.OutcomeGranted}, {domain.OpContent, domain.OutcomeGranted},
		{domain.OpMetadata, domain.OutcomeDeniedScope}, {domain.OpContent, domain.OutcomeDeniedScope},
		{domain.OpMetadata, domain.OutcomeGranted}, {domain.OpMetadata, domain.OutcomeDeniedConsent},
		{domain.OpMetadata, domain.OutcomeDeniedSubject},
	}
	got := f.log(f.tid, file.Ref)
	if len(got) != len(want) {
		t.Fatalf("sổ truy cập: %d dòng, muốn %d", len(got), len(want))
	}
	for i, a := range got {
		if a.Op != want[i].op || a.Outcome != want[i].outcome || a.Seq != int64(i+1) || a.Purpose != purpose ||
			a.Subject == nil || a.Subject.ID != plot || a.RuleVersion != domain.RuleVersion {
			t.Errorf("dòng %d: %+v, muốn %+v", i, a, want[i])
		}
	}
	if n := len(f.log(ids.New(), file.Ref)); n != 0 { // RLS: tenant khác không thấy sổ
		t.Fatalf("sổ của tenant khác: %d", n)
	}
}

// Tệp cũ chưa có bộ ba (trước pf 000123): chỉ vai + mục đích + đồng ý như trước W6.5; vẫn ghi sổ.
func TestOpenLegacyFileWithoutSubject_W65(t *testing.T) {
	f := newFx(t)
	file, err := f.attach(f.as(f.tid, f.orgA), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Open(f.as(f.tid, f.orgB, client.RoleEvidenceViewer), file.Ref, string(client.PurposeFieldSupport)); err != nil {
		t.Fatal(err)
	}
	if f.res.calls != 0 {
		t.Fatalf("tệp không chủ thể không gọi Resolver: %d", f.res.calls)
	}
	if got := f.log(f.tid, file.Ref); len(got) != 1 || got[0].Outcome != domain.OutcomeGranted || got[0].Subject != nil {
		t.Fatalf("sổ: %+v", got)
	}
}

// RLS và quyền: tenant khác không thấy tệp; sổ truy cập chỉ ghi thêm; bộ ba phải đủ cả ba ở DB; pf/sync
// không còn quyền trên pf_files.
func TestStorageInvariants_W65(t *testing.T) {
	f := newFx(t)
	file, err := f.attach(f.as(f.tid, f.orgA), nil)
	if err != nil {
		t.Fatal(err)
	}
	other := f.as(ids.New(), f.orgA, client.RoleEvidenceViewer)
	if _, err := f.svc.Open(other, file.Ref, string(client.PurposeMRVEvidence)); code(err) != ErrFileNotFound.Code {
		t.Fatalf("tệp của tenant khác: %v", err)
	}
	if _, err := f.svc.Open(f.as(f.tid, f.orgA, client.RoleEvidenceViewer), file.Ref, string(client.PurposeMRVEvidence)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for name, stmt := range map[string]string{
		"UPDATE sổ":  `UPDATE pf_files.access_log SET outcome = 'denied_scope' WHERE file_ref = $1`,
		"DELETE sổ":  `DELETE FROM pf_files.access_log WHERE file_ref = $1`,
		"UPDATE tệp": `UPDATE pf_files.file SET content_type = 'image/png' WHERE file_ref = $1`,
		"DELETE tệp": `DELETE FROM pf_files.file WHERE file_ref = $1`,
	} {
		err := tenant.WithTenantTx(ctx, f.own, f.tid, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, stmt, file.Ref); return e })
		if pgCode(err) != "42501" { // r_evidence chỉ có SELECT, INSERT
			t.Errorf("%s: %v", name, err)
		}
	}
	// Bộ ba thiếu thành phần → CHECK file_subject_triple.
	bad := file
	bad.Ref = ids.New()
	bad.ObjectKey = domain.ObjectKey(f.tid, bad.Ref)
	err = tenant.WithTenantTx(ctx, f.own, f.tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO pf_files.file (tenant_id, file_ref, subject_ref, v, alg, nonce, wrapped_dek,
		    sha256_ciphertext, size_bytes, content_type, object_key, uploaded_by, subject_module)
		  VALUES ($1, $2, $3, 1, $4, $5, $6, $7, $8, 'image/jpeg', $9, $10, 'pf/cooperative')`,
			f.tid, bad.Ref, bad.SubjectRef, bad.Alg, bad.Nonce, bad.WrappedDEK, bad.SHA256Ciphertext, bad.Size, bad.ObjectKey, bad.UploadedBy)
		return e
	})
	if pgCode(err) != "23514" {
		t.Fatalf("bộ ba thiếu: %v", err)
	}
	// pf/sync (vai cũ sync_app → r_sync) không còn đọc pf_files (pf 000123).
	env := pgtest.Get(t)
	err = tenant.WithTenantTx(ctx, env.Pool(t, migrations.PF, "sync_app"), f.tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `SELECT 1 FROM pf_files.file LIMIT 1`)
		return e
	})
	if pgCode(err) != "42501" {
		t.Fatalf("sync_app đọc pf_files: %v", err)
	}
}

// Kiểm thực thi bộ kiểm quyền từng tệp do module chủ cung cấp.
type fileCheckedResolver struct {
	*fakeResolver
	allow bool
}

func (r *fileCheckedResolver) AuthorizeFile(context.Context, port.File) (bool, error) {
	return r.allow, nil
}

// W11.24: mục đích hr_records (tài liệu nhân sự pf/hrm) được phép mở, KHÔNG kiểm đồng ý của chủ thể (chủ thể là khóa kho
// tài liệu, cơ sở là nghĩa vụ quản lý hồ sơ); mrv.evidence vẫn kiểm đồng ý.
func TestOpenHRRecordsWithoutConsent_W1124(t *testing.T) {
	f := newFx(t)
	up := f.as(f.tid, f.orgA)
	f.svc.Resolvers["pf/hrm"] = f.res
	f.svc.Purposes = map[string]port.PurposePolicy{string(client.PurposeHRRecords): {SubjectModule: "pf/hrm", SubjectType: "hr_folder"}}
	folder := ids.New()
	f.res.set(folder, &port.Resolved{ViaShareGrant: true})
	file, err := f.attach(up, &port.Subject{Module: "pf/hrm", Type: "hr_folder", ID: folder})
	if err != nil {
		t.Fatal(err)
	}
	f.consent.denied[file.SubjectRef] = true
	viewer := f.as(f.tid, f.orgB, client.RoleEvidenceViewer)
	if _, _, err := f.svc.OpenContent(viewer, file.Ref, string(client.PurposeHRRecords)); err != nil {
		t.Fatalf("hr_records không phụ thuộc đồng ý: %v", err)
	}
	if _, err := f.svc.Open(viewer, file.Ref, string(client.PurposeMRVEvidence)); code(err) != ErrConsentMissing.Code {
		t.Fatalf("mrv.evidence vẫn kiểm đồng ý: %v", err)
	}
	checker := &fileCheckedResolver{fakeResolver: f.res}
	f.svc.Resolvers["pf/hrm"] = checker
	if _, _, err := f.svc.OpenContent(viewer, file.Ref, string(client.PurposeHRRecords)); code(err) != ErrSubjectForbidden.Code {
		t.Fatalf("module chủ từ chối tệp phải chặn: %v", err)
	}
	checker.allow = true
	if _, _, err := f.svc.OpenContent(viewer, file.Ref, string(client.PurposeHRRecords)); err != nil {
		t.Fatal(err)
	}
	// HRM-DOC-PURPOSE-001: đổi mục đích không được vượt kiểm đồng ý của tệp
	// hiện trường, tệp cũ chưa có bộ ba, hoặc loại thực thể khác của HRM.
	for _, sub := range []*port.Subject{
		{Module: mod, Type: "plot", ID: folder},
		nil,
		{Module: "pf/hrm", Type: "staff", ID: folder},
	} {
		other, err := f.attach(up, sub)
		if err != nil {
			t.Fatal(err)
		}
		f.consent.denied[other.SubjectRef] = true
		if _, _, err := f.svc.OpenContent(viewer, other.Ref, string(client.PurposeHRRecords)); code(err) != ErrPurposeNotAllowed.Code {
			t.Fatalf("hr_records không được mở tệp %v: %v", sub, err)
		}
		if _, err := f.svc.Open(viewer, other.Ref, string(client.PurposeHRRecords)); code(err) != ErrPurposeNotAllowed.Code {
			t.Fatalf("hr_records không được cấp URL tệp %v: %v", sub, err)
		}
		logs := f.log(f.tid, other.Ref)
		if len(logs) != 2 || logs[0].Outcome != domain.OutcomeDeniedSubject || logs[1].Outcome != domain.OutcomeDeniedSubject {
			t.Fatal("từ chối sai mục đích phải ghi sổ cả metadata và nội dung")
		}
	}
}
