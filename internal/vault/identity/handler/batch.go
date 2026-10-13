package handler

// W11.28: POST /v1/subjects/batch-read — đọc hồ sơ nhân sự theo lô (client.ProfileBatch).

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

func (h Handler) batchRead(c *gin.Context) {
	var req client.BatchReadRequest
	if !bind(c, &req) {
		return
	}
	if len(req.Refs) == 0 || len(req.Refs) > client.MaxBatchRefs {
		httpx.Fail(c, problem.Validation.WithField("refs", "1–200 chủ thể mỗi lần"))
		return
	}
	refs := make([]uuid.UUID, 0, len(req.Refs))
	for _, r := range req.Refs {
		ref, err := uuid.Parse(r)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("refs", "mã tham chiếu không hợp lệ"))
			return
		}
		refs = append(refs, ref)
	}
	out, err := h.Svc.ReadBatch(c.Request.Context(), client.BatchRequest{Purpose: purposeOf(c, req.Purpose), Refs: refs,
		Identity: req.Identity, Section: req.Section})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]client.BatchReadItem, 0, len(out))
	for _, x := range out {
		items = append(items, client.BatchReadItem{Ref: x.Ref.String(), FullName: x.FullName, Phone: x.Phone, Address: x.Address,
			Fields: x.Fields, Erased: x.Erased})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, client.BatchReadResponse{Items: items})
}
