package hc30_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/google/uuid"
)

// Evidence is an in-memory port adapter. HRM metadata/ACL remain real PostgreSQL;
// the adapter enforces tenant, purpose, resolver, file authorization and decryption.
type proofFile struct {
	file evport.File
	blob []byte
}
type proofFiles struct {
	vault *clienttest.Fake
	res   evport.Resolver
	files map[uuid.UUID]proofFile
}

func (f *proofFiles) Attach(ctx context.Context, in evport.AttachInput) (evport.File, bool, error) {
	p, err := authz.Require(ctx, client.RoleRegistrar)
	if err != nil {
		return evport.File{}, false, err
	}
	if in.Subject == nil || in.Subject.Module != port.SubjectModule || in.Subject.Type != port.SubjectTypeFolder || in.FileRef == nil || envelope.SHA256Hex(in.Blob) != in.Meta.SHA256Ciphertext {
		return evport.File{}, false, problem.Validation
	}
	if _, err := f.res.Resolve(ctx, in.Subject.Type, in.Subject.ID); err != nil {
		return evport.File{}, false, err
	}
	subject := *in.Subject
	file := evport.File{TenantID: p.TenantID, Ref: uuid.MustParse(*in.FileRef), SubjectRef: uuid.MustParse(in.Meta.SubjectRef),
		Nonce: in.Meta.Nonce, WrappedDEK: in.Meta.WrappedDEK, ContentType: in.Meta.ContentType, SHA256Ciphertext: in.Meta.SHA256Ciphertext, Subject: &subject}
	f.files[file.Ref] = proofFile{file: file, blob: in.Blob}
	return file, true, nil
}

func (f *proofFiles) Open(ctx context.Context, ref uuid.UUID, purpose string) (evport.Opened, error) {
	if _, _, err := f.OpenContent(ctx, ref, purpose); err != nil {
		return evport.Opened{}, err
	}
	return evport.Opened{File: f.files[ref].file}, nil
}

func (f *proofFiles) OpenContent(ctx context.Context, ref uuid.UUID, purpose string) (string, []byte, error) {
	p, err := authz.Require(ctx, client.RoleEvidenceViewer)
	if err != nil {
		return "", nil, err
	}
	if purpose != string(client.PurposeHRRecords) {
		return "", nil, problem.Forbidden
	}
	stored, ok := f.files[ref]
	if !ok || stored.file.TenantID != p.TenantID {
		return "", nil, problem.NotFound
	}
	file := stored.file
	r, err := f.res.Resolve(ctx, file.Subject.Type, file.Subject.ID)
	if err != nil {
		return "", nil, err
	}
	if !r.ViaShareGrant {
		return "", nil, problem.Forbidden
	}
	checker, ok := f.res.(evport.FileAuthorizer)
	if !ok {
		return "", nil, problem.Forbidden
	}
	allowed, err := checker.AuthorizeFile(ctx, file)
	if err != nil {
		return "", nil, err
	}
	if !allowed {
		return "", nil, problem.Forbidden
	}
	dek, err := f.vault.Unwrap(ctx, client.Purpose(purpose), file.SubjectRef, file.WrappedDEK)
	if err != nil {
		return "", nil, err
	}
	plaintext, err := envelope.DecryptWithDEK(dek, file.Nonce, stored.blob)
	return file.ContentType, plaintext, err
}

func (f *fixture) appraisalProof(unit string, staff []string) uuid.UUID {
	f.t.Helper()
	f.call("admin", http.MethodPost, "/v1/hrm/docs/folders/defaults", "", nil, http.StatusCreated)
	items := f.call("hcth", http.MethodGet, "/v1/hrm/docs/folders", "", nil, http.StatusOK)["items"].([]any)
	folder := ""
	for _, item := range items {
		v := item.(map[string]any)
		if v["system_key"] == "danh_gia" {
			folder = v["folder_id"].(string)
		}
	}
	if folder == "" {
		f.t.Fatal("missing appraisal folder")
	}
	meta := map[string]any{"folder_id": folder, "title": "Bản giấy đánh giá đã đối chiếu", "doc_type": "danh_gia", "unit_code": unit, "staff_ids": staff}
	raw, err := json.Marshal(meta)
	if err != nil {
		f.t.Fatal(err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("meta", string(raw)); err != nil {
		f.t.Fatal(err)
	}
	file, err := w.CreateFormFile("file", "ban-giay.pdf")
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := file.Write([]byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\ntrailer\n%%EOF\n")); err != nil {
		f.t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		f.t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/hrm/docs", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Test-As", "hcth")
	response := httptest.NewRecorder()
	f.r.ServeHTTP(response, req)
	if response.Code != http.StatusCreated {
		f.t.Fatalf("proof upload: %d %s", response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		f.t.Fatal(err)
	}
	return uuid.MustParse(result["doc_id"].(string))
}
