package public

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

var sha256New = sha256.New

var (
	seasonRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)
	provinceRe = regexp.MustCompile(`^[0-9]{2}$`)
	monthRe    = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)
	idemRe     = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)
)

func checkFilters(f store.ExportFilters) error {
	switch {
	case f.SeasonCode != "" && !seasonRe.MatchString(f.SeasonCode):
		return problem.Validation.WithField("season_code", "^[A-Za-z0-9_.-]{1,40}$")
	case f.ProvinceCode != "" && !provinceRe.MatchString(f.ProvinceCode):
		return problem.Validation.WithField("province_code", "2 chữ số")
	case f.FromMonth != "" && !monthRe.MatchString(f.FromMonth):
		return problem.Validation.WithField("from_month", "YYYY-MM")
	case f.ToMonth != "" && !monthRe.MatchString(f.ToMonth):
		return problem.Validation.WithField("to_month", "YYYY-MM")
	case f.FromMonth != "" && f.ToMonth != "" && f.FromMonth > f.ToMonth:
		return problem.Validation.WithField("to_month", "không trước from_month")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Danh mục
// ---------------------------------------------------------------------------

type productDTO struct {
	Name     string   `json:"name"`
	Version  int      `json:"version"`
	Route    string   `json:"route"`
	Scopes   []string `json:"scopes"`
	Status   string   `json:"status"`
	Grantors []string `json:"grantors"`
}

func (s *Server) catalog(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	ctx := c.Request.Context()
	all, err := dataproduct.All()
	if err != nil {
		return nil, err
	}
	items := []productDTO{}
	for _, p := range all {
		if !p.Servable(s.d.Production) || !caller.Client.HasScope(port.ProductScope(p.Name, port.ActionRead)) {
			continue
		}
		scopes := []string{}
		for _, sc := range p.Scopes {
			if caller.Client.HasScope(sc) {
				scopes = append(scopes, sc)
			}
		}
		grantors := []string{}
		kind := partyport.DataProductKind(p.Name)
		if gl, ok := s.d.Grants.(GrantorLister); ok {
			ids, err := gl.ActiveGrantors(ctx, caller.Client.ClientTenantID, kind, caller.ReceivedAt)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				grantors = append(grantors, id.String())
			}
		} else if s.d.Grants != nil {
			if g, err := s.d.Grants.ActiveGrant(ctx, caller.Client.ClientTenantID, uuid.Nil, kind, caller.ReceivedAt); err == nil {
				grantors = append(grantors, g.Grantor.String())
			}
		}
		items = append(items, productDTO{Name: p.Name, Version: p.Version, Route: p.Route, Scopes: scopes, Status: p.Status, Grantors: grantors})
	}
	return jsonResult(http.StatusOK, map[string]any{"items": items})
}

// ---------------------------------------------------------------------------
// collection_volume
// ---------------------------------------------------------------------------

type aggregateMeta struct {
	KApplied        int64 `json:"k_applied"`
	SuppressedCells int64 `json:"suppressed_cells"`
}

func (s *Server) collection(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	ctx := c.Request.Context()
	f := store.ExportFilters{SeasonCode: c.Query("season_code"), ProvinceCode: c.Query("province_code"), FromMonth: c.Query("from_month"), ToMonth: c.Query("to_month")}
	if err := checkFilters(f); err != nil {
		return nil, err
	}
	limit, err := limitParam(c)
	if err != nil {
		return nil, err
	}
	cursor, err := cursorParam(c)
	if err != nil {
		return nil, err
	}
	grantor, err := grantorParam(c)
	if err != nil {
		return nil, err
	}
	a, err := s.authorize(ctx, caller, port.ProductCollectionVolume, port.ActionRead, grantor)
	if err != nil {
		return nil, err
	}
	if err := s.precheck(ctx, caller, a.product.Name); err != nil {
		return nil, err
	}
	if s.d.Collection == nil {
		return nil, app.ErrDependencyUnavailable
	}
	k := s.kFor(ctx, a.product, a.grant.Grantor, caller.ReceivedAt)
	page, err := s.d.Collection.CollectionCells(ctx, port.CollectionQuery{DataTenant: a.grant.Grantor, SeasonCode: f.SeasonCode, ProvinceCode: f.ProvinceCode,
		FromMonth: f.FromMonth, ToMonth: f.ToMonth, K: k, AsOf: caller.ReceivedAt, After: cursor, Limit: limit})
	if err != nil {
		return nil, err
	}
	raws, err := app.EncodeItems(a.product, page.Items)
	if err != nil {
		return nil, err
	}
	return dataResult(a, raws, page.Next, aggregateMeta{KApplied: k, SuppressedCells: page.Suppressed})
}

type exportReq struct {
	Format       string `json:"format"`
	SeasonCode   string `json:"season_code,omitempty"`
	ProvinceCode string `json:"province_code,omitempty"`
	FromMonth    string `json:"from_month,omitempty"`
	ToMonth      string `json:"to_month,omitempty"`
}

type jobDTO struct {
	ID          string  `json:"id"`
	Product     string  `json:"product"`
	Status      string  `json:"status"`
	Format      string  `json:"format"`
	RequestedAt string  `json:"requested_at"`
	CompletedAt *string `json:"completed_at"`
	Rows        *int64  `json:"rows"`
	SHA256      *string `json:"sha256"`
	FileURL     *string `json:"file_url"`
	ErrorCode   *string `json:"error_code,omitempty"`
}

func jobURL(id uuid.UUID) string { return "/v1/jobs/" + id.String() }

// acceptedJob là phản hồi 202 — chỉ từ trường BẤT BIẾN của job, nên gọi lặp trả đúng byte của lần đầu.
func acceptedJob(j store.ExportJob) jobDTO {
	return jobDTO{ID: j.ID.String(), Product: j.Product, Status: string(port.ExportQueued), Format: j.Format,
		RequestedAt: j.RequestedAt.UTC().Format(time.RFC3339Nano)}
}

func currentJob(j store.ExportJob) jobDTO {
	d := acceptedJob(j)
	d.Status, d.Rows, d.SHA256, d.ErrorCode = j.Status, j.Rows, j.SHA256, j.ErrorCode
	if j.CompletedAt != nil {
		s := j.CompletedAt.UTC().Format(time.RFC3339Nano)
		d.CompletedAt = &s
	}
	if j.Status == string(port.ExportCompleted) {
		u := jobURL(j.ID) + "/file"
		d.FileURL = &u
	}
	return d
}

func (s *Server) exportCollection(c *gin.Context, caller port.Caller, plan port.Plan) (*result, error) {
	ctx := c.Request.Context()
	key := c.GetHeader(idempotency.Header)
	if key == "" {
		return nil, problem.IdempotencyRequired
	}
	if !idemRe.MatchString(key) {
		return nil, problem.BadRequest.WithDetail("Idempotency-Key phải 8–128 ký tự [A-Za-z0-9._:-]")
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, problem.PayloadTooLarge
	}
	var req exportReq
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return nil, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ, không có trường lạ")
	}
	if req.Format != "csv" && req.Format != "json" {
		return nil, problem.Validation.WithField("format", "csv | json")
	}
	f := store.ExportFilters{SeasonCode: req.SeasonCode, ProvinceCode: req.ProvinceCode, FromMonth: req.FromMonth, ToMonth: req.ToMonth}
	if err := checkFilters(f); err != nil {
		return nil, err
	}
	grantor, err := grantorParam(c)
	if err != nil {
		return nil, err
	}
	a, err := s.authorize(ctx, caller, port.ProductCollectionVolume, port.ActionExport, grantor)
	if err != nil {
		return nil, err
	}
	if s.d.Svc == nil {
		return nil, app.ErrDependencyUnavailable
	}
	k := s.kFor(ctx, a.product, a.grant.Grantor, caller.ReceivedAt)
	hash := hashHex([]byte(routeTemplate(c)+"\n"+a.grant.Grantor.String()+"\n"), raw)
	job, replayed, err := s.d.Svc.RequestExport(ctx, app.ExportRequest{Caller: caller, DataTenant: a.grant.Grantor, Product: a.product, Format: req.Format,
		Filters: f, K: k, MaxRows: plan.MaxExportRows, IdemKey: key, RequestHash: hash, OperatorTenant: s.d.OperatorTenant,
		RequestedAtTime: caller.ReceivedAt})
	if err != nil {
		return nil, err
	}
	r, err := jsonResult(http.StatusAccepted, acceptedJob(job))
	if err != nil {
		return nil, err
	}
	r.header.Set("Location", jobURL(job.ID))
	r.header.Set(port.HeaderDataContract, a.product.ContractHeader())
	if replayed {
		r.header.Set(idempotency.ReplayedHeader, "true")
	}
	r.replayed, r.jobID, r.product, r.contractVersion = replayed, job.ID, a.product.Name, a.product.Version
	return r, nil
}

// ---------------------------------------------------------------------------
// hub_capacity, mrv_package_status
// ---------------------------------------------------------------------------

func (s *Server) hubs(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	ctx := c.Request.Context()
	province := c.Query("province_code")
	if province != "" && !provinceRe.MatchString(province) {
		return nil, problem.Validation.WithField("province_code", "2 chữ số")
	}
	limit, err := limitParam(c)
	if err != nil {
		return nil, err
	}
	cursor, err := cursorParam(c)
	if err != nil {
		return nil, err
	}
	grantor, err := grantorParam(c)
	if err != nil {
		return nil, err
	}
	a, err := s.authorize(ctx, caller, port.ProductHubCapacity, port.ActionRead, grantor)
	if err != nil {
		return nil, err
	}
	if err := s.precheck(ctx, caller, a.product.Name); err != nil {
		return nil, err
	}
	if s.d.Hubs == nil {
		return nil, app.ErrDependencyUnavailable
	}
	items, next, err := s.d.Hubs.HubCapacities(ctx, a.grant.Grantor, province, cursor, limit)
	if err != nil {
		return nil, err
	}
	raws, err := app.EncodeItems(a.product, items)
	if err != nil {
		return nil, err
	}
	return dataResult(a, raws, next, nil)
}

func (s *Server) mrvPackages(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	ctx := c.Request.Context()
	status := c.Query("status")
	switch port.ExportStatus(status) {
	case "", port.ExportQueued, port.ExportCompleted, port.ExportFailed:
	default:
		return nil, problem.Validation.WithField("status", "queued | completed | failed")
	}
	limit, err := limitParam(c)
	if err != nil {
		return nil, err
	}
	cursor, err := cursorParam(c)
	if err != nil {
		return nil, err
	}
	grantor, err := grantorParam(c)
	if err != nil {
		return nil, err
	}
	a, err := s.authorize(ctx, caller, port.ProductMRVPackageStatus, port.ActionRead, grantor)
	if err != nil {
		return nil, err
	}
	if err := s.precheck(ctx, caller, a.product.Name); err != nil {
		return nil, err
	}
	if s.d.MRV == nil {
		return nil, app.ErrDependencyUnavailable
	}
	items, next, err := s.d.MRV.Packages(ctx, a.grant.Grantor, status, cursor, limit)
	if err != nil {
		return nil, err
	}
	raws, err := app.EncodeItems(a.product, items)
	if err != nil {
		return nil, err
	}
	return dataResult(a, raws, next, nil)
}

func (s *Server) mrvPackage(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("export_id"))
	if err != nil {
		return nil, problem.NotFound
	}
	grantor, err := grantorParam(c)
	if err != nil {
		return nil, err
	}
	a, err := s.authorize(ctx, caller, port.ProductMRVPackageStatus, port.ActionRead, grantor)
	if err != nil {
		return nil, err
	}
	if err := s.precheck(ctx, caller, a.product.Name); err != nil {
		return nil, err
	}
	if s.d.MRV == nil {
		return nil, app.ErrDependencyUnavailable
	}
	item, err := s.d.MRV.Package(ctx, a.grant.Grantor, id)
	if err != nil {
		return nil, err
	}
	raws, err := app.EncodeItems(a.product, []port.MRVPackageStatusV1{item})
	if err != nil {
		return nil, err
	}
	r := newResult(http.StatusOK)
	r.body = append(append([]byte(nil), raws[0]...), '\n')
	r.header.Set("Content-Type", "application/json; charset=utf-8")
	r.header.Set(port.HeaderDataContract, a.product.ContractHeader())
	r.rows, r.dataTenant, r.product, r.contractVersion = 1, a.grant.Grantor, a.product.Name, a.product.Version
	return r, nil
}

// ---------------------------------------------------------------------------
// Việc xuất và tệp
// ---------------------------------------------------------------------------

func (s *Server) job(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return nil, app.ErrJobNotFound
	}
	if s.d.Svc == nil {
		return nil, app.ErrDependencyUnavailable
	}
	j, _, err := s.d.Svc.JobFor(c.Request.Context(), s.d.OperatorTenant, caller.Client.ID, id)
	if err != nil {
		return nil, err
	}
	r, err := jsonResult(http.StatusOK, currentJob(j))
	if err != nil {
		return nil, err
	}
	r.jobID, r.product, r.contractVersion = j.ID, j.Product, j.ContractVersion
	return r, nil
}

// jobFile phát luồng tệp qua api-public (không URL có chữ ký): kiểm lại scope + cấp quyền của bên
// cấp ghi trong job (thu hồi → 403 ngay), ghi sổ (đơn vị theo dòng) rồi mới gửi byte.
func (s *Server) jobFile(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return nil, app.ErrJobNotFound
	}
	if s.d.Svc == nil {
		return nil, app.ErrDependencyUnavailable
	}
	j, f, err := s.d.Svc.JobFor(ctx, s.d.OperatorTenant, caller.Client.ID, id)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, app.ErrJobNotReady
	}
	a, err := s.authorize(ctx, caller, j.Product, port.ActionExport, j.DataTenantID)
	if err != nil {
		return nil, err
	}
	if a.product.Version != j.ContractVersion {
		return nil, port.ErrForbidden
	}
	if err := s.precheck(ctx, caller, j.Product); err != nil {
		return nil, err
	}
	rc, err := s.d.Svc.OpenFile(ctx, *f)
	if err != nil {
		return nil, err
	}
	r := newResult(http.StatusOK)
	r.stream, r.size = rc, f.SizeBytes
	r.header.Set("Content-Type", f.ContentType)
	r.header.Set("Content-Disposition", `attachment; filename="`+j.Product+"-"+j.ID.String()+"."+j.Format+`"`)
	r.header.Set("Digest", digestHeader(f.SHA256))
	r.header.Set(port.HeaderDataContract, a.product.ContractHeader())
	r.rows, r.dataTenant, r.product, r.contractVersion, r.jobID = f.Rows, a.grant.Grantor, a.product.Name, a.product.Version, j.ID
	return r, nil
}

// ---------------------------------------------------------------------------
// Sử dụng của chính khách hàng
// ---------------------------------------------------------------------------

type usageDTO struct {
	Date    string `json:"date"`
	Product string `json:"product"`
	Calls   int64  `json:"calls"`
	Units   int64  `json:"units"`
}

func (s *Server) usage(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	if !caller.Client.HasScope(port.ScopeUsageRead) {
		return nil, port.ErrForbidden
	}
	from, err1 := time.ParseInLocation("2006-01-02", c.Query("from"), clock.Zone())
	to, err2 := time.ParseInLocation("2006-01-02", c.Query("to"), clock.Zone())
	switch {
	case err1 != nil:
		return nil, problem.Validation.WithField("from", "YYYY-MM-DD")
	case err2 != nil:
		return nil, problem.Validation.WithField("to", "YYYY-MM-DD")
	case to.Before(from):
		return nil, problem.Validation.WithField("to", "không trước from")
	case to.Sub(from) > 92*24*time.Hour:
		return nil, problem.Validation.WithField("to", "tối đa 93 ngày")
	}
	if s.d.Usage == nil {
		return nil, app.ErrDependencyUnavailable
	}
	days, err := s.d.Usage.UsageOf(c.Request.Context(), s.d.OperatorTenant, caller.Client.ID, from.UTC(), to.AddDate(0, 0, 1).UTC())
	if err != nil {
		return nil, err
	}
	items := make([]usageDTO, 0, len(days))
	for _, d := range days {
		items = append(items, usageDTO{Date: d.Date, Product: d.Product, Calls: d.Calls, Units: d.Units})
	}
	return jsonResult(http.StatusOK, map[string]any{"items": items})
}
