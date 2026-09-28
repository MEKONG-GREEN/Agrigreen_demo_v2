package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// registerRegistry gắn route Admin API của G-06 (danh sách danh mục) và G-07 (sổ đăng ký tham số).
func (h Handler) registerRegistry(rg *gin.RouterGroup) {
	rg.GET("/masterdata/catalogs", h.catalogs)
	rg.GET("/masterdata/params", h.params)
}

type catalogSummaryDTO struct {
	Catalog       string   `json:"catalog"`
	Name          string   `json:"name"`
	ItemCount     int      `json:"item_count"`
	InactiveCount int      `json:"inactive_count"`
	AttrsSchema   []string `json:"attrs_schema"`
}

func (h Handler) catalogs(c *gin.Context) {
	cs, err := h.Svc.ListCatalogs(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]catalogSummaryDTO, 0, len(cs))
	for _, s := range cs {
		items = append(items, catalogSummaryDTO{Catalog: s.Catalog, Name: s.Name, ItemCount: s.ItemCount, InactiveCount: s.InactiveCount,
			AttrsSchema: s.AttrsSchema})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type paramCurrentDTO struct {
	Version       int     `json:"version"`
	Value         *string `json:"value"`
	Text          *string `json:"text"`
	EffectiveFrom string  `json:"effective_from"`
}

type paramEntryDTO struct {
	Key            string           `json:"key"`
	Name           string           `json:"name"`
	Unit           string           `json:"unit"`
	Kind           string           `json:"kind"`
	Min            *string          `json:"min"`
	Max            *string          `json:"max"`
	Current        *paramCurrentDTO `json:"current"`
	SubmittedCount int              `json:"submitted_count"`
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (h Handler) params(c *gin.Context) {
	after, err := decodeCursor(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	pg, err := h.Svc.ListParams(c.Request.Context(), app.ParamsQuery{Prefix: c.Query("prefix"), After: after, Limit: limit(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]paramEntryDTO, 0, len(pg.Items))
	for _, e := range pg.Items {
		d := paramEntryDTO{Key: e.Key, Name: e.Name, Unit: e.Unit, Kind: e.Kind, Min: optStr(e.Min), Max: optStr(e.Max),
			SubmittedCount: e.SubmittedCount}
		if e.Current != nil {
			d.Current = &paramCurrentDTO{Version: e.Current.Version, Value: e.Current.Value, Text: e.Current.Text,
				EffectiveFrom: e.Current.EffectiveFrom.UTC().Format(time.RFC3339)}
		}
		items = append(items, d)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

// withNames điền setter_name/approver_name (tên cán bộ từ danh bạ pf/extension, G-07).
func (h Handler) withNames(ctx context.Context, vs []versionDTO) {
	var refs []uuid.UUID
	for _, v := range vs {
		if id, err := uuid.Parse(v.SetterRef); err == nil {
			refs = append(refs, id)
		}
		if v.ApproverRef != nil {
			if id, err := uuid.Parse(*v.ApproverRef); err == nil {
				refs = append(refs, id)
			}
		}
	}
	names := h.Svc.ActorNames(ctx, refs)
	for i := range vs {
		if id, err := uuid.Parse(vs[i].SetterRef); err == nil {
			vs[i].SetterName = optStr(names[id])
		}
		if vs[i].ApproverRef != nil {
			if id, err := uuid.Parse(*vs[i].ApproverRef); err == nil {
				vs[i].ApproverName = optStr(names[id])
			}
		}
	}
}
