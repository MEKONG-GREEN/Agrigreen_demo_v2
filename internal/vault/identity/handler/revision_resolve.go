package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

func (h Handler) resolveRevisions(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req client.RevisionsRequest
	if !bind(c, &req) {
		return
	}
	if len(req.Refs) > client.MaxRevisionRefs {
		httpx.Fail(c, problem.Validation.WithField("refs", "tối đa 200 bản sửa mỗi lần"))
		return
	}
	refs := make([]uuid.UUID, 0, len(req.Refs))
	for _, v := range req.Refs {
		ref, err := uuid.Parse(v)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("refs", "mã tham chiếu không hợp lệ"))
			return
		}
		refs = append(refs, ref)
	}
	out, err := h.Svc.ResolveRevisions(c.Request.Context(), purposeOf(c, req.Purpose), refs)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]client.RevisionNamesItem, 0, len(out))
	for _, r := range out {
		items = append(items, client.RevisionNamesItem{Ref: r.Ref.String(), SubjectRef: r.SubjectRef.String(), PrevName: r.PrevName, NextName: r.NextName})
	}
	c.JSON(http.StatusOK, client.RevisionNamesResponse{Items: items})
}
