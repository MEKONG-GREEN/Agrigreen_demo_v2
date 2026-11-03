package app

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
)

func bindingFixture199(at time.Time) (exportport.Request, exportport.Run, string) {
	template := WordTemplate("mau_1")
	req := exportport.Request{Template: template.Key, Version: template.Version, Subject: WordSubject(ids.New()), FileName: "bao-cao.docx", Data: doctpl.Data{Vars: map[string]string{VarNumber: "", VarPlaceDate: "", "nguon": "frozen"}, Lists: map[string][]doctpl.Item{"rows": {{"value": "giữ nguyên"}}}}, Number: &exportport.Numbering{Var: VarNumber, Series: WordSeries}, Date: &exportport.DateLine{Var: VarPlaceDate, Place: "An Giang"}}
	body := []byte("stored immutable Word bytes")
	hash := sha256.Sum256(body)
	expected := doctpl.Data{Vars: map[string]string{VarNumber: "1/BC-TTKN", VarPlaceDate: doctpl.PlaceDateLine("An Giang", at), "nguon": "frozen"}, Lists: map[string][]doctpl.Item{"rows": {{"value": "giữ nguyên"}}}}
	run := exportport.Run{ID: ids.New(), Seq: 1, Subject: req.Subject, Template: template.Key, TemplateVersion: template.Version, TemplateHash: template.Hash(), FileName: req.FileName, ContentType: docx.ContentType, Body: body, BodyHash: hex.EncodeToString(hash[:]), Size: int64(len(body)), InputHash: expected.Hash(), DocNumber: "1/BC-TTKN", DocNumberAuto: true, ActorRef: ids.New(), At: at}
	return req, run, template.Hash()
}

func TestArchive199WordBindingUsesActualRunDateAndFilledInputs(t *testing.T) {
	for _, at := range []time.Time{time.Date(2026, 11, 2, 16, 59, 59, 0, time.UTC), time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC)} {
		req, run, templateHash := bindingFixture199(at)
		before := req.Data.Hash()
		if err := verifyWordBinding(req, templateHash, run); err != nil {
			t.Fatal(err)
		}
		if req.Data.Hash() != before || req.Data.Vars[VarNumber] != "" || req.Data.Vars[VarPlaceDate] != "" {
			t.Fatal("binding changed frozen input")
		}
	}
	req, run, templateHash := bindingFixture199(time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC))
	if got := doctpl.PlaceDateLine("An Giang", run.At); got != "An Giang, ngày 03 tháng 11 năm 2026" {
		t.Fatalf("VN midnight: %q", got)
	}
	// A time inferred from the HTTP request instead of Run.At must not attest a run.
	wrong := copyWordData(req.Data)
	wrong.Vars[VarNumber] = run.DocNumber
	wrong.Vars[VarPlaceDate] = "An Giang, ngày 02 tháng 11 năm 2026"
	run.InputHash = wrong.Hash()
	if err := verifyWordBinding(req, templateHash, run); err == nil {
		t.Fatal("wrong side of midnight accepted")
	}
	req, run, templateHash = bindingFixture199(time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC))
	req.Data.Vars[VarNumber] = " 42/BC-TTKN "
	req.Data.Vars[VarPlaceDate] = "Ngày báo cáo đã điền từ snapshot"
	run.DocNumber = "42/BC-TTKN"
	run.DocNumberAuto = false
	run.InputHash = req.Data.Hash()
	before := req.Data.Hash()
	if err := verifyWordBinding(req, templateHash, run); err != nil {
		t.Fatal(err)
	}
	if req.Data.Hash() != before {
		t.Fatal("supplied placeholders were replaced")
	}
	req.Number = nil
	req.Data.Vars[VarNumber] = "…/BC-TTKN"
	run.DocNumber = ""
	run.InputHash = req.Data.Hash()
	if err := verifyWordBinding(req, templateHash, run); err != nil {
		t.Fatal(err)
	}
}

func TestArchive199WordBindingRejectsMisboundOrLossyMetadata(t *testing.T) {
	changes := map[string]func(*exportport.Request, *exportport.Run){
		"different_snapshot":     func(q *exportport.Request, _ *exportport.Run) { q.Data.Vars["nguon"] = "another frozen export" },
		"run_id_missing":         func(_ *exportport.Request, r *exportport.Run) { r.ID = uuid.Nil },
		"actor_missing":          func(_ *exportport.Request, r *exportport.Run) { r.ActorRef = uuid.Nil },
		"time_missing":           func(_ *exportport.Request, r *exportport.Run) { r.At = time.Time{} },
		"version_missing":        func(_ *exportport.Request, r *exportport.Run) { r.Seq = 0 },
		"other_subject":          func(_ *exportport.Request, r *exportport.Run) { r.Subject = WordSubject(ids.New()) },
		"other_template":         func(_ *exportport.Request, r *exportport.Run) { r.Template = "khtc.mau_2" },
		"other_template_version": func(_ *exportport.Request, r *exportport.Run) { r.TemplateVersion++ },
		"template_hash":          func(_ *exportport.Request, r *exportport.Run) { r.TemplateHash = strings.Repeat("0", 64) },
		"input_hash":             func(_ *exportport.Request, r *exportport.Run) { r.InputHash = strings.Repeat("0", 64) },
		"body_hash":              func(_ *exportport.Request, r *exportport.Run) { r.BodyHash = strings.Repeat("0", 64) },
		"body_size":              func(_ *exportport.Request, r *exportport.Run) { r.Size++ },
		"body_corruption":        func(_ *exportport.Request, r *exportport.Run) { r.Body[0] = 'x' },
		"mime":                   func(_ *exportport.Request, r *exportport.Run) { r.ContentType = "text/html" },
		"path_escape":            func(_ *exportport.Request, r *exportport.Run) { r.FileName = "../leak.docx" },
		"number_missing":         func(_ *exportport.Request, r *exportport.Run) { r.DocNumber = "" },
		"number_flag":            func(_ *exportport.Request, r *exportport.Run) { r.DocNumberAuto = false },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			req, run, hash := bindingFixture199(time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC))
			change(&req, &run)
			if err := verifyWordBinding(req, hash, run); err == nil {
				t.Fatal("misbound Word accepted")
			}
		})
	}
}

func TestArchive199WordInputCopyHasNoAliases(t *testing.T) {
	req, _, _ := bindingFixture199(time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC))
	before := req.Data.Hash()
	out := copyWordData(req.Data)
	out.Vars["nguon"] = "changed"
	out.Lists["rows"][0]["value"] = "changed"
	out.Lists["rows"] = append(out.Lists["rows"], doctpl.Item{"value": "new"})
	if req.Data.Hash() != before {
		t.Fatal("copy aliases frozen Word data")
	}
}

func TestArchive199AutoNumberBoundToRequestedSeries(t *testing.T) {
	for _, number := range []string{"1/OTHER", "0/BC-TTKN", "01/BC-TTKN", "-1/BC-TTKN", "1e100000/BC-TTKN", "1e-100000/BC-TTKN", strings.Repeat("9", 100000) + "/BC-TTKN", "9223372036854775808/BC-TTKN"} {
		req, run, hash := bindingFixture199(time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC))
		run.DocNumber = number
		filled := copyWordData(req.Data)
		filled.Vars[VarNumber] = number
		filled.Vars[VarPlaceDate] = doctpl.PlaceDateLine(req.Date.Place, run.At)
		run.InputHash = filled.Hash()
		if err := verifyWordBinding(req, hash, run); err == nil {
			t.Fatalf("auto number from another series or noncanonical counter accepted (length %d)", len(number))
		}
	}
	for _, number := range []string{"1/BC-TTKN", "9223372036854775807/BC-TTKN"} {
		req, run, hash := bindingFixture199(time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC))
		run.DocNumber = number
		filled := copyWordData(req.Data)
		filled.Vars[VarNumber] = number
		filled.Vars[VarPlaceDate] = doctpl.PlaceDateLine(req.Date.Place, run.At)
		run.InputHash = filled.Hash()
		if err := verifyWordBinding(req, hash, run); err != nil {
			t.Fatal(number, err)
		}
	}
}
