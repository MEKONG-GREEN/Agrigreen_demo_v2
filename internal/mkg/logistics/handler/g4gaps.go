package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docfile"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// registerG4Gaps gắn route G4 đóng khoảng trống: tệp chứng từ, chứng từ chuyến có người ký (L-11), lịch sử phiên
// bản phương tiện.
func (h Handler) registerG4Gaps(rg *gin.RouterGroup) {
	httpx.AllowBody(rg, http.MethodPost, "/logistics/files", docfile.MaxBytes+64<<10)
	rg.POST("/logistics/files", h.uploadFile)
	rg.GET("/logistics/files/:id", h.getFile)
	rg.GET("/logistics/files/:id/content", h.fileContent)
	rg.GET("/logistics/trips/:id/documents", h.documents)
	rg.POST("/logistics/trips/:id/documents", h.recordDocument)
	rg.GET("/logistics/vessels/:id/versions", h.vesselVersions)
}

type fileDTO struct {
	ID          string `json:"id"`
	Purpose     string `json:"purpose"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	UploadedBy  string `json:"uploaded_by"`
	RuleVersion string `json:"rule_version"`
	RecordedAt  string `json:"recorded_at"`
}

func toFile(f store.File) fileDTO {
	return fileDTO{ID: f.ID.String(), Purpose: f.Purpose, FileName: f.FileName, ContentType: f.ContentType, Size: f.Size, SHA256: f.SHA256,
		UploadedBy: f.UploadedBy.String(), RuleVersion: f.RuleVersion, RecordedAt: f.RecordedAt.UTC().Format(time.RFC3339Nano)}
}

func (h Handler) uploadFile(c *gin.Context) {
	data, name, err := docfile.ReadMultipart(c.Request)
	if err != nil {
		httpx.Fail(c, app.FileProblem(err))
		return
	}
	f, replayed, err := h.Svc.UploadFile(c.Request.Context(), app.UploadInput{Data: data, FileName: name, SHA256: c.PostForm("sha256"),
		IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"file": toFile(f)})
}

func (h Handler) getFile(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrFileNotFound)
	if !ok {
		return
	}
	f, err := h.Svc.File(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"file": toFile(f)})
}

func (h Handler) fileContent(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrFileNotFound)
	if !ok {
		return
	}
	rc, f, err := h.Svc.OpenFile(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer func() { _ = rc.Close() }()
	docfile.WriteContent(c.Writer, rc, f.ContentType, f.Size, f.SHA256, f.FileName)
}

type documentDTO struct {
	ID               string  `json:"id"`
	TripID           string  `json:"trip_id"`
	Kind             string  `json:"kind"`
	FileRef          string  `json:"file_ref"`
	FileSHA256       string  `json:"file_sha256"`
	SignerRole       string  `json:"signer_role"`
	SignerRef        *string `json:"signer_ref"`
	SignedAt         string  `json:"signed_at"`
	Note             *string `json:"note"`
	SupersedesID     *string `json:"supersedes_id"`
	CorrectionReason *string `json:"correction_reason"`
	ActorRef         string  `json:"actor_ref"`
	RuleVersion      string  `json:"rule_version"`
	RecordedAt       string  `json:"recorded_at"`
}

func toDocument(d store.TripDocument) documentDTO {
	return documentDTO{ID: d.ID.String(), TripID: d.TripID.String(), Kind: d.Kind, FileRef: d.FileRef.String(), FileSHA256: d.FileSHA256,
		SignerRole: d.SignerRole, SignerRef: ref(d.SignerRef), SignedAt: d.SignedAt.UTC().Format(time.RFC3339Nano), Note: d.Note,
		SupersedesID: ref(d.SupersedesID), CorrectionReason: d.CorrectionReason, ActorRef: d.ActorRef.String(), RuleVersion: d.RuleVersion,
		RecordedAt: d.RecordedAt.UTC().Format(time.RFC3339Nano)}
}

type documentReq struct {
	Kind             string  `json:"kind"`
	FileRef          string  `json:"file_ref"`
	SignerRole       string  `json:"signer_role"`
	SignerRef        *string `json:"signer_ref"`
	SignedAt         string  `json:"signed_at"`
	Note             string  `json:"note"`
	SupersedesID     *string `json:"supersedes_id"`
	CorrectionReason string  `json:"correction_reason"`
}

func optID(name string, s *string) (*uuid.UUID, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil, problem.Validation.WithField(name, "phải là UUID")
	}
	return &id, nil
}

func (h Handler) recordDocument(c *gin.Context) {
	trip, ok := pathID(c, "id", app.ErrTripNotFound)
	if !ok {
		return
	}
	var req documentReq
	if !bind(c, &req) {
		return
	}
	file, err := uuid.Parse(req.FileRef)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file_ref", "phải là UUID — tải tệp qua POST /v1/logistics/files"))
		return
	}
	signedAt, err := time.Parse(time.RFC3339, req.SignedAt)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("signed_at", "RFC 3339"))
		return
	}
	signer, err := optID("signer_ref", req.SignerRef)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	sup, err := optID("supersedes_id", req.SupersedesID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var dev *string
	if d := c.GetHeader(DeviceHeader); d != "" {
		dev = &d
	}
	d, replayed, err := h.Svc.RecordDocument(c.Request.Context(), trip, app.DocumentInput{Kind: req.Kind, FileRef: file, SignerRole: req.SignerRole,
		SignerRef: signer, SignedAt: signedAt, Note: req.Note, SupersedesID: sup, CorrectionReason: req.CorrectionReason, DeviceID: dev,
		IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"document": toDocument(d)})
}

func (h Handler) documents(c *gin.Context) {
	trip, ok := pathID(c, "id", app.ErrTripNotFound)
	if !ok {
		return
	}
	ds, err := h.Svc.Documents(c.Request.Context(), trip)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]documentDTO, 0, len(ds))
	for _, d := range ds {
		out = append(out, toDocument(d))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

type vesselVersionDTO struct {
	EventID            string  `json:"event_id"`
	VesselID           string  `json:"vessel_id"`
	Kind               string  `json:"kind"`
	Version            int     `json:"version"`
	Code               string  `json:"code"`
	VehicleKind        string  `json:"vehicle_kind"`
	Name               *string `json:"name"`
	OwnerRef           *string `json:"owner_ref"`
	RegisteredTons     *string `json:"registered_tons"`
	StrawPayloadTons   *string `json:"straw_payload_tons"`
	RegistrationExpiry *string `json:"registration_expiry"`
	RateType           *string `json:"rate_type"`
	RateVND            *string `json:"rate_vnd"`
	Status             string  `json:"status"`
	Reason             *string `json:"reason"`
	ActorRef           string  `json:"actor_ref"`
	EventTime          string  `json:"event_time"`
	RecordedAt         string  `json:"recorded_at"`
}

// vesselVersions: GET /v1/logistics/vessels/{id}/versions?cursor=<version>&limit= — lịch sử phiên bản danh mục (GH-01).
func (h Handler) vesselVersions(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrVesselNotFound)
	if !ok {
		return
	}
	after := 0
	if s := c.Query("cursor"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "con trỏ không hợp lệ"))
			return
		}
		after = n
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	vs, next, err := h.Svc.VesselVersions(c.Request.Context(), id, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]vesselVersionDTO, 0, len(vs))
	for _, v := range vs {
		d := vesselVersionDTO{EventID: v.ID.String(), VesselID: v.VesselID.String(), Kind: v.Kind, Version: v.Version, Code: v.Code,
			VehicleKind: v.VehicleKind, Name: v.Name, OwnerRef: ref(v.OwnerRef), RegisteredTons: decp(v.RegisteredTons),
			StrawPayloadTons: decp(v.StrawPayloadTons), RateType: v.RateType, RateVND: decp(v.RateVND), Status: v.Status, Reason: v.Reason,
			ActorRef: v.ActorRef.String(), EventTime: v.EventTime.UTC().Format(time.RFC3339Nano), RecordedAt: v.RecordedAt.UTC().Format(time.RFC3339Nano)}
		if v.RegistrationExpiry != nil {
			s := v.RegistrationExpiry.Format(time.DateOnly)
			d.RegistrationExpiry = &s
		}
		out = append(out, d)
	}
	var nc *string
	if next != nil {
		s := strconv.Itoa(*next)
		nc = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "next_cursor": nc})
}
