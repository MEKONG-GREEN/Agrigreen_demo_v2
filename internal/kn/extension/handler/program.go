package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// Tiến độ Đề án 1 triệu ha (FR-03)
// ---------------------------------------------------------------------------

type programReq struct {
	ID             *string `json:"id"`
	CommuneCode    string  `json:"commune_code"`
	CooperativeRef string  `json:"cooperative_ref"`
	SeasonCode     string  `json:"season_code"`
	SeasonYear     int     `json:"season_year"`
	RegisteredHa   string  `json:"registered_area_ha"`
	DeployingHa    string  `json:"deploying_area_ha"`
	CompletedHa    string  `json:"completed_area_ha"`
	Note           string  `json:"note"`
	EventTime      *string `json:"event_time"`
}

type programDTO struct {
	ID             string  `json:"id"`
	CommuneCode    string  `json:"commune_code"`
	ProvinceCode   string  `json:"province_code"`
	CooperativeRef string  `json:"cooperative_ref"`
	SeasonCode     string  `json:"season_code"`
	SeasonYear     int     `json:"season_year"`
	RegisteredHa   string  `json:"registered_area_ha"`
	DeployingHa    string  `json:"deploying_area_ha"`
	CompletedHa    string  `json:"completed_area_ha"`
	Note           *string `json:"note"`
	Version        int     `json:"version"`
	PreviousID     *string `json:"previous_id"`
	ActorRef       string  `json:"actor_ref"`
	EventTime      string  `json:"event_time"`
	RecordedAt     string  `json:"recorded_at"`
	Seq            int64   `json:"seq"`
}

func toProgram(r domain.ProgramReport) programDTO {
	return programDTO{
		ID: r.ID.String(), CommuneCode: r.CommuneCode, ProvinceCode: r.ProvinceCode, CooperativeRef: r.CooperativeRef.String(),
		SeasonCode: r.SeasonCode, SeasonYear: r.SeasonYear, RegisteredHa: r.RegisteredHa.String(), DeployingHa: r.DeployingHa.String(),
		CompletedHa: r.CompletedHa.String(), Note: opt(r.Note), Version: r.Version, PreviousID: ref(r.PreviousID),
		ActorRef: r.ActorRef.String(), EventTime: ts(r.EventTime), RecordedAt: ts(r.RecordedAt), Seq: r.Seq,
	}
}

func parseHa(v *problem.Problem, bad *bool, field, s string) (decimal.Decimal, *problem.Problem) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		*bad = true
		return decimal.Zero, v.WithField(field, "bắt buộc, số thập phân dạng chuỗi (ha)")
	}
	return d, v
}

func (h Handler) submitProgram(c *gin.Context) {
	var req programReq
	if !bind(c, &req) {
		return
	}
	v, bad := problem.Validation, false
	r := domain.ProgramReport{CommuneCode: req.CommuneCode, SeasonCode: req.SeasonCode, SeasonYear: req.SeasonYear, Note: req.Note}
	coop, err := ids.ParseRef(req.CooperativeRef)
	if err != nil {
		v, bad = v.WithField("cooperative_ref", "bắt buộc, phải là UUID"), true
	}
	r.CooperativeRef = coop
	r.RegisteredHa, v = parseHa(v, &bad, "registered_area_ha", req.RegisteredHa)
	r.DeployingHa, v = parseHa(v, &bad, "deploying_area_ha", req.DeployingHa)
	r.CompletedHa, v = parseHa(v, &bad, "completed_area_ha", req.CompletedHa)
	in := app.ProgramInput{Report: r, IdemKey: idem(c)}
	if req.ID != nil {
		id, err := ids.Parse(*req.ID)
		if err != nil {
			v, bad = v.WithField("id", "phải là UUIDv7"), true
		} else {
			in.ID = &id
		}
	}
	in.EventTime, v = parseEventTime(v, &bad, req.EventTime)
	if bad {
		httpx.Fail(c, v)
		return
	}
	res, err := h.Svc.SubmitProgramReport(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/extension/program-reports/"+res.Report.ID.String())
	c.JSON(status, gin.H{"report": toProgram(res.Report), "approved": false})
}

type approveReq struct {
	Note string `json:"note"`
}

func (h Handler) approveProgram(c *gin.Context) {
	id, ok := pathID(c, app.ErrProgramNotFound)
	if !ok {
		return
	}
	var req approveReq
	if !bind(c, &req) {
		return
	}
	r, err := h.Svc.ApproveProgramReport(c.Request.Context(), id, req.Note)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"report": toProgram(r), "approved": true})
}

func (h Handler) programProgress(c *gin.Context) {
	year := 0
	if y := c.Query("season_year"); y != "" {
		n, err := strconv.Atoi(y)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("season_year", "phải là số năm"))
			return
		}
		year = n
	}
	d, err := h.Svc.ProgramProgress(c.Request.Context(), c.Query("unit"), c.Query("season_code"), year)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(d.Rows))
	for _, r := range d.Rows {
		items = append(items, gin.H{
			"province_code": r.ProvinceCode, "commune_code": r.CommuneCode, "cooperative_ref": r.CooperativeRef.String(),
			"season_code": r.SeasonCode, "season_year": r.SeasonYear, "registered_area_ha": r.RegisteredHa.String(),
			"deploying_area_ha": r.DeployingHa.String(), "completed_area_ha": r.CompletedHa.String(), "approved": r.Approved,
		})
	}
	targets := gin.H{}
	for p, t := range d.Targets {
		targets[p] = decp(t)
	}
	c.JSON(http.StatusOK, gin.H{
		"items": items, "targets": targets, "unapproved": d.Completeness.Unapproved, "provisional": d.Completeness.Provisional,
	})
}
