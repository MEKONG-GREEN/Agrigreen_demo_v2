// Package handler: HTTP của pf/sync (contracts/openapi/application.yaml và
// admin.yaml, mục "pf/sync"; docs/dev/sync-contract.md).
package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API vào nhóm /v1 (xác thực + idempotency).
// POST /files được phép thân tới domain.MaxUploadBody thay cho giới hạn chung.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.POST("/devices", h.register)
	rg.GET("/sync/pull", h.pull)
	rg.POST("/sync/push", h.push)
	httpx.AllowBody(rg, http.MethodPost, "/files", domain.MaxUploadBody)
	rg.POST("/files", h.upload)
	rg.GET("/files/:ref", h.getFile)
	rg.GET("/files/:ref/content", h.content)
}

func bindJSON(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, problem.PayloadTooLarge)
			return false
		}
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func (h Handler) register(c *gin.Context) {
	var req deviceReq
	if !bindJSON(c, &req) {
		return
	}
	d, created, err := h.Svc.RegisterDevice(c.Request.Context(), domain.Registration{DeviceID: req.DeviceID, Platform: req.Platform, AppVersion: req.AppVersion})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, toDeviceDTO(d, false))
}

func (h Handler) pull(c *gin.Context) {
	limit := 0
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > app.MaxPullLimit {
			httpx.Fail(c, problem.Validation.WithField("limit", "số nguyên 1–1000"))
			return
		}
		limit = n
	}
	// entities: tùy chọn, lọc nhóm dữ liệu (lặp tham số hoặc phân tách bằng dấu phẩy) —
	// máy dùng khi lệnh sync_now có khóa "entities" (KN US-SYNC-02).
	var only []string
	for _, v := range c.QueryArray("entities") {
		for _, e := range strings.Split(v, ",") {
			if e = strings.TrimSpace(e); e != "" {
				only = append(only, e)
			}
		}
	}
	res, err := h.Svc.PullEntitiesOnly(c.Request.Context(), c.GetHeader(port.HeaderDeviceID), c.Query("cursor"), limit, only)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toPullDTO(res))
}

func (h Handler) push(c *gin.Context) {
	var req pushReq
	if !bindJSON(c, &req) {
		return
	}
	in := app.PushInput{DeviceHeader: c.GetHeader(port.HeaderDeviceID), DeviceID: req.DeviceID,
		BatchKey: idempotency.KeyFromContext(c.Request.Context())}
	for _, r := range req.Records {
		in.Records = append(in.Records, domain.RawRecord{ID: r.ID, IdemKey: r.IdemKey, Entity: r.Entity, Op: r.Op, EventTime: r.EventTime,
			SupersedesID: r.SupersedesID, CorrectionReason: r.CorrectionReason, Payload: r.Payload})
	}
	results, err := h.Svc.Push(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]resultDTO, 0, len(results))
	for _, r := range results {
		out = append(out, toResultDTO(r))
	}
	c.JSON(http.StatusOK, gin.H{"results": out})
}

// upload đọc multipart: phần meta (JSON phong bì), blob (bản mã), file_ref (tùy chọn).
func (h Handler) upload(c *gin.Context) {
	mr, err := c.Request.MultipartReader()
	if err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân phải là multipart/form-data với phần meta và blob"))
		return
	}
	in := app.UploadInput{DeviceHeader: c.GetHeader(port.HeaderDeviceID)}
	var gotMeta, gotBlob bool
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			httpx.Fail(c, uploadErr(err))
			return
		}
		switch part.FormName() {
		case "meta":
			raw, err := io.ReadAll(io.LimitReader(part, 64<<10))
			if err != nil {
				httpx.Fail(c, uploadErr(err))
				return
			}
			var m envelope.Metadata
			if err := json.Unmarshal(raw, &m); err != nil {
				httpx.Fail(c, problem.Validation.WithField("meta", "phải là JSON theo docs/dev/envelope-format.md"))
				return
			}
			in.Meta, gotMeta = m, true
		case "blob":
			b, err := io.ReadAll(io.LimitReader(part, domain.MaxFileBytes+1))
			if err != nil {
				httpx.Fail(c, uploadErr(err))
				return
			}
			in.Blob, gotBlob = b, true
		case "file_ref":
			b, err := io.ReadAll(io.LimitReader(part, 64))
			if err != nil {
				httpx.Fail(c, uploadErr(err))
				return
			}
			s := string(b)
			in.FileRef = &s
		default:
			httpx.Fail(c, problem.BadRequest.WithDetail("Phần multipart không được hỗ trợ: chỉ meta, blob, file_ref"))
			return
		}
	}
	if !gotMeta || !gotBlob {
		httpx.Fail(c, problem.Validation.WithField("meta", "bắt buộc phần meta và blob"))
		return
	}
	f, created, err := h.Svc.Upload(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.Header("Location", "/v1/files/"+f.Ref.String())
	c.JSON(status, gin.H{"file_ref": f.Ref.String(), "sha256_ciphertext": f.SHA256Ciphertext, "size": f.Size})
}

func uploadErr(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return problem.PayloadTooLarge
	}
	return problem.BadRequest.WithDetail("multipart không hợp lệ")
}

func purpose(c *gin.Context) client.Purpose { return client.Purpose(c.GetHeader(port.HeaderPurpose)) }

func (h Handler) getFile(c *gin.Context) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrFileNotFound)
		return
	}
	v, err := h.Svc.GetFile(c.Request.Context(), ref, purpose(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"file": toFileDTO(v.File), "download_url": v.URL, "expires_at": ts(v.ExpiresAt)})
}

func (h Handler) content(c *gin.Context) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrFileNotFound)
		return
	}
	ct, pt, err := h.Svc.ContentFile(c.Request.Context(), ref, purpose(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer clear(pt)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, ct, pt)
}

func cursorParam(c *gin.Context) (uuid.UUID, bool) {
	var after uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		if err != nil || len(b) != 16 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return after, false
		}
		copy(after[:], b)
	}
	return after, true
}

func limitParam(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

func nextCursor(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return base64.RawURLEncoding.EncodeToString(id[:])
}
