package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/hrmappraisal"
)

func (a appraisalFixture) finish(t *testing.T, period string) {
	t.Helper()
	for _, who := range []string{"xa", "vc_tt", "hcth2"} {
		a.change(t, period, who, who, "self-ready-"+period+who, domain.AppraisalEntryChange{Action: "self_submit", Version: 1, Answers: a.answers, Reason: "Căn cứ nhiệm vụ của cá nhân"}, http.StatusCreated)
		as := "ld_tt"
		if who == "hcth2" {
			as = "hcth"
		}
		a.change(t, period, who, as, "manager-ready-"+period+who, domain.AppraisalEntryChange{Action: "manager_submit", Version: 2, Answers: a.answers, CollectiveRating: "HTTNV", Reason: "Căn cứ họp tập thể của đơn vị"}, http.StatusCreated)
	}
	appraisalCall(t, a.f, "ld_tt", http.MethodPost, "/v1/hrm/appraisals/periods/"+period, "send-ready-"+period, app.AppraisalPeriodChange{Action: "send", Version: 1, Unit: "trong_trot"}, http.StatusCreated)
	a.change(t, period, "hcth2", "hcth", "send-ms06-ready-"+period, domain.AppraisalEntryChange{Action: "send", Version: 3}, http.StatusCreated)
}
func appraisalAuditCount(t *testing.T, a appraisalFixture, as, scope string) int {
	t.Helper()
	var n int
	err := db.WithModuleTenantTx(context.Background(), a.f.svc.DB, app.DBModule, a.f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_hrm.appraisal_event WHERE entity_kind='access' AND actor_ref=$1 AND payload->>'scope'=$2", a.f.who[as].ActorRef, scope).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func appraisalZip(t *testing.T, data []byte, path string) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == path {
			r, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			raw, e := io.ReadAll(r)
			_ = r.Close()
			if e != nil {
				t.Fatal(e)
			}
			return string(raw)
		}
	}
	t.Fatal("thiếu phần ZIP " + path)
	return ""
}
func TestHRMAppraisalExportAndAuditHTTP_W1126(t *testing.T) {
	a := newAppraisalFixture(t)
	f := a.f
	// Cùng một pool giới hạn 1 cho nghiệp vụ và cap/export: I/O lồng TX sẽ không hoàn tất.
	pool := f.env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	f.svc.DB = db.ForModule(pool, app.DBModule)
	ex, err := hrmappraisal.NewExporter(pool, f.svc.Clock, exportport.TemplateRegistration{Owner: app.AppraisalWordOwner, Templates: app.AppraisalWordTemplates()})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.AppraisalExporter = ex
	period := a.open(t, "open-export-q3")
	base := "/v1/hrm/appraisals/periods/" + period
	appraisalCall(t, f, "hcth", http.MethodPost, base+"/exports", "export-incomplete", app.AppraisalExportInput{Kind: "MS05", Format: "xlsx", Version: 1}, http.StatusUnprocessableEntity)
	a.finish(t, period)
	var firstVersion string
	for _, path := range []string{base + "/entries", base + "/entries/" + a.staff["xa"], base + "/entries/" + a.staff["xa"] + "/history"} {
		before := appraisalAuditCount(t, a, "xa", "self")
		value := appraisalCall(t, f, "xa", http.MethodGet, path, "", nil, http.StatusOK)
		if path == base+"/entries/"+a.staff["xa"] {
			firstVersion = value["item"].(map[string]any)["id"].(string)
		}
		if after := appraisalAuditCount(t, a, "xa", "self"); after != before+1 {
			t.Fatalf("không audit đúng scope self %s: %d→%d", path, before, after)
		}
	}
	before := appraisalAuditCount(t, a, "xa", "self")
	appraisalCall(t, f, "xa", http.MethodGet, base+"/entries/"+a.staff["xa"]+"/versions/"+firstVersion, "", nil, http.StatusOK)
	if appraisalAuditCount(t, a, "xa", "self") != before+1 {
		t.Fatal("thiếu audit bản lịch sử")
	}
	before = appraisalAuditCount(t, a, "hcth", "all")
	paper := appraisalCall(t, f, "hcth", http.MethodGet, base+"/paper/MS05", "", nil, http.StatusOK)
	if len(paper["items"].([]any)) != 2 || len(paper["template"].(map[string]any)["columns"].([]any)) != 8 {
		t.Fatal("MS05 sai dân số/tám cột")
	}
	if appraisalAuditCount(t, a, "hcth", "all") != before+1 {
		t.Fatal("thiếu audit xem trước")
	}
	appraisalCall(t, f, "xa", http.MethodGet, base+"/paper/MS05", "", nil, http.StatusForbidden)
	appraisalCall(t, f, "ld_cn", http.MethodGet, base+"/paper/MS05", "", nil, http.StatusNotFound)
	appraisalCall(t, f, "ld_tt", http.MethodGet, base+"/paper/MS06", "", nil, http.StatusForbidden)
	input := app.AppraisalExportInput{Kind: "MS05", Format: "xlsx", Version: 2}
	xl := appraisalCall(t, f, "hcth", http.MethodPost, base+"/exports", "export-xlsx-one", input, http.StatusCreated)
	again := appraisalCall(t, f, "hcth", http.MethodPost, base+"/exports", "export-xlsx-one", input, http.StatusCreated)
	if xl["id"] != again["id"] {
		t.Fatal("retry xuất tạo bản khác")
	}
	path := base + "/exports/" + xl["id"].(string) + "/file"
	xw := appraisalRequest(f, "hcth", http.MethodGet, path, "", nil)
	if xw.Code != http.StatusOK {
		t.Fatalf("Excel: %d %s", xw.Code, xw.Body.String())
	}
	file, err := excelize.OpenReader(bytes.NewReader(xw.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := file.GetRows(file.GetSheetName(0))
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	header := false
	for _, r := range rows {
		if len(r) > 0 && r[0] == "STT" {
			header = true
			if len(r) != 8 {
				t.Fatal("Excel không đúng tám cột")
			}
		}
	}
	if !header {
		t.Fatal("thiếu tiêu đề Excel")
	}
	sum := sha256.Sum256(xw.Body.Bytes())
	if hex.EncodeToString(sum[:]) != xl["sha256"] {
		t.Fatal("Excel hash không khớp lần xuất")
	}
	word := appraisalCall(t, f, "hcth", http.MethodPost, base+"/exports", "export-docx-one", app.AppraisalExportInput{Kind: "MS05", Format: "docx", Version: 2}, http.StatusCreated)
	wordPath := base + "/exports/" + word["id"].(string) + "/file"
	ww := appraisalRequest(f, "hcth", http.MethodGet, wordPath, "", nil)
	if ww.Code != http.StatusOK {
		t.Fatalf("Word: %d %s", ww.Code, ww.Body.String())
	}
	grid8 := false
	for _, part := range strings.Split(appraisalZip(t, ww.Body.Bytes(), "word/document.xml"), "<w:tblGrid>")[1:] {
		grid, _, _ := strings.Cut(part, "</w:tblGrid>")
		if strings.Count(grid, "<w:gridCol ") == 8 {
			grid8 = true
		}
	}
	if !grid8 {
		t.Fatal("Word thiếu bảng dữ liệu tám cột")
	}

	doc, err := docx.Extract(ww.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"QUÝ III, NĂM 2026", "Người Đánh Giá A", "Người Đánh Giá B", "XÁC NHẬN TẬP THỂ"} {
		if !strings.Contains(doc.Text(), want) {
			t.Fatalf("Word thiếu %q", want)
		}
	}
	appraisalCall(t, f, "other", http.MethodPost, base+"/exports", "cross-tenant-export", input, http.StatusNotFound)
	appraisalCall(t, f, "xa", http.MethodPost, base+"/exports", "self-no-export", input, http.StatusForbidden)
	// Byte Word giữ nguyên, Excel hiện hữu cố ý báo drift khi tên trong vault đổi; không lưu PII vào HRM.
	subject := uuid.MustParse(paper["items"].([]any)[0].(map[string]any)["member"].(map[string]any)["subject_ref"].(string))
	ctx := authz.WithPrincipal(context.Background(), f.who["hcth"])
	identity, err := f.vault.Resolve(ctx, client.PurposeHRMProfile, subject)
	if err != nil {
		t.Fatal(err)
	}
	identity.FullName = "Tên được sửa ở kho định danh"
	if err = f.vault.Amend(ctx, client.PurposeHRMProfile, subject, identity); err != nil {
		t.Fatal(err)
	}
	ww2 := appraisalRequest(f, "hcth", http.MethodGet, wordPath, "", nil)
	if ww2.Code != http.StatusOK || !bytes.Equal(ww.Body.Bytes(), ww2.Body.Bytes()) {
		t.Fatal("Word đã lưu thay byte theo tên hiện tại")
	}
	xw2 := appraisalRequest(f, "hcth", http.MethodGet, path, "", nil)
	if xw2.Code != http.StatusOK || xw2.Header().Get("X-Data-Drift") != "true" || bytes.Equal(xw.Body.Bytes(), xw2.Body.Bytes()) {
		t.Fatal("Excel tái dựng không báo drift tên")
	}
	var unsafe int
	err = db.WithModuleTenantTx(context.Background(), f.svc.DB, app.DBModule, f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM pf_hrm.appraisal_event WHERE entity_kind='access' AND (payload::text LIKE '%Người Đánh Giá%' OR payload::text LIKE '%Căn cứ%' OR payload::text LIKE '%Tên được sửa%' OR payload ? 'name' OR payload ? 'answers')`).Scan(&unsafe)
	})
	if err != nil || unsafe != 0 {
		t.Fatalf("audit chứa PII/nội dung phiếu: %d %v", unsafe, err)
	}
	// RLS/append-only của audit mới không được đổi vì thêm phạm vi self.
	for _, stmt := range []string{"UPDATE pf_hrm.appraisal_event SET actor_ref=actor_ref WHERE entity_kind='access'", "DELETE FROM pf_hrm.appraisal_event WHERE entity_kind='access'"} {
		e := db.WithModuleTenantTx(context.Background(), f.svc.DB, app.DBModule, f.tid, func(tx pgx.Tx) error { _, e := tx.Exec(context.Background(), stmt); return e })
		if e == nil {
			t.Fatal("audit cho sửa/xóa")
		}
	}
}

func TestHRMAppraisalAnnualAndRacesHTTP_W1126(t *testing.T) {
	a := newAppraisalFixture(t)
	quarter := a.open(t, "open-annual-reference-q3")
	a.finish(t, quarter)
	appraisalCall(t, a.f, "hcth", http.MethodPost, "/v1/hrm/appraisals/periods/"+quarter, "close-reference-q3", app.AppraisalPeriodChange{Action: "close", Version: 2, AcknowledgeUnsigned: true}, http.StatusCreated)
	qitem := a.detail(t, quarter, "xa")
	a.input.Quarter = 0
	period := a.open(t, "open-annual-2026")
	base := "/v1/hrm/appraisals/periods/" + period
	annual := a.detail(t, period, "xa")
	refs := annual["quarter_results"].([]any)
	if len(refs) != 1 || refs[0].(map[string]any)["entry_version"] != qitem["id"] || refs[0].(map[string]any)["rating"] != "HTTNV" || annual["official_rating"] != "" {
		t.Fatal("tham khảo quý không đúng phiên bản hoặc tự suy kết luận năm")
	}
	qversion, _ := qitem["version"].(json.Number).Int64()
	a.change(t, quarter, "xa", "hcth", "reopen-reference-q3", domain.AppraisalEntryChange{Action: "reopen", Version: qversion, Reason: "Mở lại quý sau khi đã chụp đợt năm"}, http.StatusCreated)
	if a.detail(t, period, "xa")["quarter_results"].([]any)[0].(map[string]any)["entry_version"] != qitem["id"] {
		t.Fatal("mở lại quý thay tham chiếu năm")
	}
	a.finish(t, period)
	// Hai kết luận cùng phiên bản: chỉ một bản được ghi, không lost update.
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, rating := range []string{"HTNV", "KHTNV"} {
		wg.Add(1)
		go func(rating string) {
			defer wg.Done()
			w := appraisalRequest(a.f, "hcth", http.MethodPost, base+"/entries/"+a.staff["xa"], "annual-race-"+rating, domain.AppraisalEntryChange{Action: "official", Version: 4, OfficialRating: rating, Reason: "Kết luận tập thể lãnh đạo cơ quan"})
			statuses <- w.Code
		}(rating)
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for code := range statuses {
		counts[code]++
	}
	if counts[http.StatusCreated] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("race kết luận: %v", counts)
	}
	item := a.detail(t, period, "xa")
	official := item["official_rating"].(string)
	body := app.AppraisalPeriodChange{Action: "close", Version: 2, AcknowledgeUnsigned: true}
	closed := appraisalCall(t, a.f, "hcth", http.MethodPost, base, "close-annual", body, http.StatusCreated)
	replay := appraisalCall(t, a.f, "hcth", http.MethodPost, base, "close-annual", body, http.StatusCreated)
	if closed["version"] != replay["version"] {
		t.Fatal("retry chốt thêm phiên bản")
	}
	var count int
	var actual string
	err := db.WithModuleTenantTx(context.Background(), a.f.svc.DB, app.DBModule, a.f.tid, func(tx pgx.Tx) error {
		if e := tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_hrm.annual_rating WHERE year=2026 AND source='evaluation'").Scan(&count); e != nil {
			return e
		}
		return tx.QueryRow(context.Background(), "SELECT rating FROM pf_hrm.annual_rating WHERE staff_id=$1 ORDER BY seq DESC LIMIT 1", uuid.MustParse(a.staff["xa"])).Scan(&actual)
	})
	if err != nil || count != 3 || actual != official {
		t.Fatalf("kết luận năm không đến hồ sơ nâng bậc: %d %q %v", count, actual, err)
	}
	// Mở riêng một người và kết luận lại: thêm dòng thay thế đúng người, giữ kết quả hai người khác.
	n, _ := a.detail(t, period, "xa")["version"].(json.Number).Int64()
	a.change(t, period, "xa", "hcth", "annual-reopen", domain.AppraisalEntryChange{Action: "reopen", Version: n, Reason: "Điều chỉnh kết luận năm của riêng cán bộ"}, http.StatusCreated)
	a.change(t, period, "xa", "ld_tt", "annual-reassess", domain.AppraisalEntryChange{Action: "manager_submit", Version: n + 1, Answers: a.answers, CollectiveRating: "HTTNV", Reason: "Căn cứ biên bản điều chỉnh kết luận năm"}, http.StatusCreated)
	a.change(t, period, "xa", "ld_tt", "annual-resend", domain.AppraisalEntryChange{Action: "send", Version: n + 2}, http.StatusCreated)
	appraisalCall(t, a.f, "hcth", http.MethodPost, base, "annual-reclose", app.AppraisalPeriodChange{Action: "close", Version: 3, AcknowledgeUnsigned: true, Reason: "Điều chỉnh kết luận năm của riêng cán bộ"}, http.StatusCreated)
	err = db.WithModuleTenantTx(context.Background(), a.f.svc.DB, app.DBModule, a.f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_hrm.annual_rating WHERE year=2026 AND supersedes_id IS NOT NULL").Scan(&count)
	})
	if err != nil || count != 1 {
		t.Fatalf("chốt lại không ghi đúng một sự kiện bù: %d %v", count, err)
	}
}

// Adapter kiểm thử thực thi quyền/chủ thể/giải mã hiện hữu rồi trả metadata như Open của evidence thật.
type appraisalProofFiles struct {
	*fakeFiles
	tenant uuid.UUID
}

func (f appraisalProofFiles) Open(ctx context.Context, ref uuid.UUID, purpose string) (evport.Opened, error) {
	if _, _, err := f.OpenContent(ctx, ref, purpose); err != nil {
		return evport.Opened{}, err
	}
	file := f.files[ref]
	file.TenantID = f.tenant
	return evport.Opened{File: file}, nil
}
func TestHRMAppraisalProofAndSignedHTTP_W1126(t *testing.T) {
	a := newAppraisalFixture(t)
	f := a.f
	ff := withDocs(t, f)
	// withDocs cũ dựng OrgRef ngẫu nhiên; phiên cán bộ xã mới phải đúng UnitRef.
	xa := f.who["xa"]
	k := *xa.KN
	k.OrgRef = k.UnitRef
	xa.KN = &k
	f.who["xa"] = xa
	f.svc.UseDocs(appraisalProofFiles{ff, f.tid})
	f.call("admin", http.MethodPost, "/v1/hrm/docs/folders/defaults", nil, http.StatusCreated)
	folder := folderByKey(t, f.call("hcth", http.MethodGet, "/v1/hrm/docs/folders", nil, http.StatusOK), "danh_gia")
	upload := func(who string) uuid.UUID {
		d := f.upload("hcth", map[string]any{"folder_id": folder, "title": "Phiếu giấy có ký", "doc_type": "danh_gia", "staff_ids": []string{a.staff[who]}}, "phieu.pdf", pdf, http.StatusCreated)
		return uuid.MustParse(d["doc_id"].(string))
	}
	good, bad := upload("vc_tt"), upload("hcth2")
	period := a.open(t, "open-proxy-proof")
	base := "/v1/hrm/appraisals/periods/" + period
	body := domain.AppraisalEntryChange{Action: "proxy_self", Version: 1, Answers: a.answers, Reason: "Bản giấy có chữ ký đã đối chiếu", ProxyDocument: &bad}
	a.change(t, period, "vc_tt", "ld_tt", "proxy-wrong-binding", body, http.StatusNotFound)
	body.ProxyDocument = &good
	a.change(t, period, "vc_tt", "xa", "proxy-self-forbidden", body, http.StatusNotFound)
	first := a.change(t, period, "vc_tt", "ld_tt", "proxy-good-document", body, http.StatusCreated)
	replay := a.change(t, period, "vc_tt", "ld_tt", "proxy-good-document", body, http.StatusCreated)
	if first["id"] != replay["id"] || first["proxy_document"] != good.String() {
		t.Fatal("nhập thay mất scan/replay")
	}
	for _, who := range []string{"xa", "hcth2"} {
		a.change(t, period, who, who, "self-proof-"+who, domain.AppraisalEntryChange{Action: "self_submit", Version: 1, Answers: a.answers, Reason: "Căn cứ thực hiện nhiệm vụ cá nhân"}, http.StatusCreated)
	}
	for _, who := range []string{"xa", "vc_tt", "hcth2"} {
		as := "ld_tt"
		if who == "hcth2" {
			as = "hcth"
		}
		a.change(t, period, who, as, "manage-proof-"+who, domain.AppraisalEntryChange{Action: "manager_submit", Version: 2, Answers: a.answers, CollectiveRating: "HTTNV", Reason: "Biên bản họp tập thể đánh giá"}, http.StatusCreated)
	}
	appraisalCall(t, f, "ld_tt", http.MethodPost, base, "send-proof-tt", app.AppraisalPeriodChange{Action: "send", Version: 1, Unit: "trong_trot"}, http.StatusCreated)
	a.change(t, period, "hcth2", "hcth", "send-proof-ms06", domain.AppraisalEntryChange{Action: "send", Version: 3}, http.StatusCreated)
	appraisalCall(t, f, "hcth", http.MethodPost, base, "signed-attach-proof", app.AppraisalPeriodChange{Action: "attach_signed", Version: 2, SignedDocuments: map[string]uuid.UUID{"MS05": good, "MS06": bad}}, http.StatusCreated)
	appraisalCall(t, f, "hcth", http.MethodPost, base, "signed-close-proof", app.AppraisalPeriodChange{Action: "close", Version: 3}, http.StatusCreated)
	if a.detail(t, period, "vc_tt")["status"] != "closed" {
		t.Fatal("đợt có đủ bản ký chưa chốt")
	}
}

func TestHRMAppraisalScoreSummaryHTTP_W1126(t *testing.T) {
	a := newAppraisalFixture(t)
	period := a.open(t, "open-score-summary")
	base := "/v1/hrm/appraisals/periods/" + period
	load := func(id string) [][]string {
		w := appraisalRequest(a.f, "hcth", http.MethodGet, base+"/exports/"+id+"/file", "", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("score download: %d %s", w.Code, w.Body.String())
		}
		f, e := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
		if e != nil {
			t.Fatal(e)
		}
		rows, e := f.GetRows(f.GetSheetName(0))
		_ = f.Close()
		if e != nil {
			t.Fatal(e)
		}
		return rows
	}
	blank := appraisalCall(t, a.f, "hcth", http.MethodPost, base+"/exports", "scores-before-submission", app.AppraisalExportInput{Kind: app.AppraisalScores, Format: "xlsx", Version: 1}, http.StatusCreated)
	if blank["count"] != json.Number("3") {
		t.Fatal("tổng hợp chưa nộp thiếu người")
	}
	before := load(blank["id"].(string))
	a.finish(t, period)
	next := appraisalCall(t, a.f, "hcth", http.MethodPost, base+"/exports", "scores-after-submission", app.AppraisalExportInput{Kind: app.AppraisalScores, Format: "xlsx", Version: 2}, http.StatusCreated)
	rows := load(next["id"].(string))
	seen, header := 0, false
	for _, row := range rows {
		if len(row) > 0 && row[0] == "STT" {
			header = true
			if len(row) != 10 {
				t.Fatal("tổng hợp điểm không đúng10cột riêng")
			}
		}
		if len(row) >= 7 && strings.HasPrefix(row[1], "Người Đánh Giá") {
			seen++
			if row[4] != "82" || row[5] != "82" {
				t.Fatalf("sai tổng điểm phiếu: %v", row)
			}
		}
	}
	if !header || seen != 3 {
		t.Fatalf("thiếu header/đủ3người: %v %d", header, seen)
	}
	if again := load(blank["id"].(string)); !slices.EqualFunc(before, again, func(a, b []string) bool { return slices.Equal(a, b) }) {
		t.Fatal("lần xuất điểm cũ bị thay theo phiếu nộp mới")
	}
	unit := appraisalCall(t, a.f, "ld_tt", http.MethodPost, base+"/exports", "scores-unit-only", app.AppraisalExportInput{Kind: app.AppraisalScores, Format: "xlsx", Version: 2}, http.StatusCreated)
	if unit["count"] != json.Number("2") {
		t.Fatal("tổng hợp điểm lãnh đạo lộ MS06/đơn vị khác")
	}
	appraisalCall(t, a.f, "xa", http.MethodPost, base+"/exports", "scores-self-denied", app.AppraisalExportInput{Kind: app.AppraisalScores, Format: "xlsx", Version: 2}, http.StatusForbidden)
	appraisalCall(t, a.f, "hcth", http.MethodPost, base+"/exports", "scores-no-word", app.AppraisalExportInput{Kind: app.AppraisalScores, Format: "docx", Version: 2}, http.StatusUnprocessableEntity)
}

func TestHRMAppraisalPositionSnapshotHTTP_W1126(t *testing.T) {
	a := newAppraisalFixture(t)
	old := a.open(t, "position-before-promotion")
	a.f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+a.staff["vc_tt"]+"/events", ev("position", "2026-10-01", map[string]any{"position": "pho_truong_phong", "appointed_on": "2026-10-01"}), http.StatusCreated)
	a.input.Quarter = 4
	a.input.CutoffOn = "2026-10-02"
	next := a.open(t, "position-after-promotion")
	oldMember := a.detail(t, old, "vc_tt")["member"].(map[string]any)
	newMember := a.detail(t, next, "vc_tt")["member"].(map[string]any)
	if oldMember["summary_kind"] != "MS05" || oldMember["position"] != "vien_chuc" || newMember["summary_kind"] != "MS06" || newMember["position"] != "pho_truong_phong" {
		t.Fatal("chức vụ mới không chia lại đúng hoặc đổi snapshot đợt cũ")
	}
}
