package handler

// W11.53c (#176, FN-19 luồng phụ): nạp Excel điểm trình diễn, hộ tham gia của dự án — GET …/import-template (tệp mẫu),
// POST …/import/preview (xem trước, không ghi), POST …/import (ghi cả tệp hoặc không ghi gì). Thân multipart/form-data có
// phần "file" (.xlsx ≤ 2 MB). Phản hồi không mang họ tên/SĐT chủ hộ (được lưu 48 giờ để phát lại theo Idempotency-Key).

import (
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

const maxImportUpload = domain.ImportMaxBytes + 64<<10

func (h Handler) importTemplate(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrProjectNotFound)
	if !ok {
		return
	}
	data, name, err := h.Svc.ImportTemplate(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="mau-nap-diem-ho.xlsx"; filename*=UTF-8''`+url.PathEscape(name))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

func issuesOut(is []domain.ImportIssue) []gin.H {
	out := make([]gin.H, 0, len(is))
	for _, x := range is {
		var row any
		if x.Row > 0 {
			row = x.Row
		}
		out = append(out, gin.H{"sheet": optStr(x.Sheet), "row": row, "column": optStr(x.Column), "message": x.Message})
	}
	return out
}

func importOut(p *app.ImportPlan) gin.H {
	sites := make([]gin.H, 0, len(p.Sites))
	for _, x := range p.Sites {
		sites = append(sites, gin.H{"row": x.Row, "code": x.Code, "name": x.Name, "commune_code": x.CommuneCode,
			"model_type_code": x.ModelType, "model_type_name": optStr(x.ModelTypeName), "scale": optDec(x.Scale.Value),
			"scale_unit": optStr(x.Scale.Unit), "diary_form": optStr(x.DiaryForm), "officer_ref": optRef(x.OfficerRef),
			"officer_display": optStr(x.OfficerName), "note": optStr(x.Note)})
	}
	hhs := make([]gin.H, 0, len(p.Households))
	for _, x := range p.Households {
		hhs = append(hhs, gin.H{"row": x.Row, "site_key": x.SiteKey, "site_name": x.SiteName, "commune_code": x.CommuneCode,
			"owner_label": x.OwnerLabel, "owner_phone_masked": x.PhoneMasked, "scale": optDec(x.Scale.Value),
			"scale_unit": optStr(x.Scale.Unit), "keeper_kind": x.KeeperKind, "joined_on": optDate(x.JoinedOn), "note": optStr(x.Note)})
	}
	refs := refsOut(p.SiteRefs)
	return gin.H{"project_ref": p.ProjectRef.String(), "project_code": optStr(p.ProjectCode), "valid": p.Valid(),
		"committed": p.Committed, "replayed": p.Replayed, "site_count": p.SiteCount(), "household_count": p.HouseholdCount(),
		"sites": sites, "households": hhs, "issues": issuesOut(p.Issues), "issues_truncated": p.Truncated, "site_refs": refs}
}

// readUpload đọc phần "file" của thân multipart (≤ 2 MB).
func readUpload(c *gin.Context) ([]byte, bool) {
	tooBig := app.ErrImportInvalid.WithField("file", "tệp .xlsx 1 byte – 2 MB")
	if err := c.Request.ParseMultipartForm(maxImportUpload); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, tooBig)
			return nil, false
		}
		httpx.Fail(c, problem.Validation.WithDetail("thân phải là multipart/form-data có phần file"))
		return nil, false
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	fh, err := c.FormFile("file")
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "bắt buộc"))
		return nil, false
	}
	if fh.Size <= 0 || fh.Size > domain.ImportMaxBytes {
		httpx.Fail(c, tooBig)
		return nil, false
	}
	fr, err := fh.Open()
	if err != nil {
		httpx.Fail(c, err)
		return nil, false
	}
	defer func() { _ = fr.Close() }()
	data, err := io.ReadAll(io.LimitReader(fr, domain.ImportMaxBytes+1))
	if err != nil {
		httpx.Fail(c, err)
		return nil, false
	}
	if len(data) > domain.ImportMaxBytes {
		httpx.Fail(c, tooBig)
		return nil, false
	}
	return data, true
}

// importFile: commit = false → xem trước (200, valid cho biết ghi được không); commit = true → ghi; tệp còn lỗi → 422
// extension.cnts_import_invalid kèm issues, không ghi gì.
func (h Handler) importFile(commit bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ref, ok := pathRef(c, app.ErrProjectNotFound)
		if !ok {
			return
		}
		data, ok := readUpload(c)
		if !ok {
			return
		}
		in := app.ImportInput{ProjectRef: ref, Data: data, IdemKey: idem(c)}
		var p *app.ImportPlan
		var err error
		if commit {
			p, err = h.Svc.CommitImport(c.Request.Context(), in)
		} else {
			p, err = h.Svc.PreviewImport(c.Request.Context(), in)
		}
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		if commit && !p.Committed {
			httpx.Fail(c, app.ErrImportInvalid.WithDetail("Tệp còn lỗi — chưa ghi điểm, hộ nào").With("issues", issuesOut(p.Issues)).
				With("issues_truncated", p.Truncated))
			return
		}
		st := http.StatusOK
		if commit && !p.Replayed {
			st = http.StatusCreated
		}
		c.JSON(st, importOut(p))
	}
}
