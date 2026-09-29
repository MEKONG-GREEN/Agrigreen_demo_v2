package handler

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// MaxFarmerImportBytes giới hạn tệp nhập tài khoản nông dân.
const MaxFarmerImportBytes = 2 << 20

// RegisterApp gắn route Application API (App HTX, không bắt MFA):
//   - quản lý HTX / cán bộ KN: tạo tài khoản nông dân, nhập hàng loạt, phát lại liên kết kích hoạt;
//   - nông dân: nông hộ của mình, danh bạ cán bộ HTX (HTX US-SUPPORT-02).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/farmer-accounts", h.listFarmers)
	rg.POST("/farmer-accounts", h.createFarmer)
	httpx.AllowBody(rg, http.MethodPost, "/farmer-accounts/import", MaxFarmerImportBytes)
	rg.POST("/farmer-accounts/import", h.importFarmers)
	rg.GET("/farmer-accounts/:id/activation", h.activations)
	rg.POST("/farmer-accounts/:id/activation", h.reissue)
	rg.GET("/farmer/households", h.myHouseholds)
	rg.GET("/farmer/coop-staff", h.coopStaff)
	rg.GET("/farmer/coop-staff/:id/contact", h.coopStaffContact)
}

func timeStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

type grantDTO struct {
	GrantID      string  `json:"grant_id"`
	Status       string  `json:"status"`
	Channel      string  `json:"channel"`
	IssuedBy     string  `json:"issued_by"`
	IssuedAt     string  `json:"issued_at"`
	ExpiresAt    string  `json:"expires_at"`
	UsedAt       *string `json:"used_at"`
	RevokedAt    *string `json:"revoked_at"`
	RevokeReason *string `json:"revoke_reason"`
	ReplacedBy   *string `json:"replaced_by"`
}

func toGrantDTO(g domain.Grant, status string) grantDTO {
	d := grantDTO{GrantID: g.ID.String(), Status: status, Channel: g.Channel, IssuedBy: g.IssuedBy.String(),
		IssuedAt: g.IssuedAt.UTC().Format(time.RFC3339), ExpiresAt: g.ExpiresAt.UTC().Format(time.RFC3339),
		UsedAt: timeStr(g.UsedAt), RevokedAt: timeStr(g.RevokedAt), RevokeReason: strPtr(g.RevokeReason)}
	if g.ReplacedBy != nil {
		d.ReplacedBy = refPtr(*g.ReplacedBy)
	}
	return d
}

// activationDTO: liên kết một lần + dữ liệu dựng QR. link/qr chỉ có ở phản hồi đầu
// tiên (bản lưu để phát lại theo Idempotency-Key đã lược bỏ — idempotency.SetStoredBody).
type activationDTO struct {
	grantDTO
	Link     *string `json:"link" pii:"true"`
	QR       *qrDTO  `json:"qr"`
	Replayed bool    `json:"replayed"`
}

type qrDTO struct {
	Format          string `json:"format"`           // "url": mã hóa nguyên liên kết
	Data            string `json:"data" pii:"true"`  // nội dung mã QR
	ErrorCorrection string `json:"error_correction"` // mức sửa lỗi đề xuất
	MinModulePx     int    `json:"min_module_px"`    // cỡ ô tối thiểu khi in (px)
}

func toActivationDTO(a *app.Activation, now time.Time, redact bool) *activationDTO {
	if a == nil {
		return nil
	}
	d := &activationDTO{grantDTO: toGrantDTO(a.Grant, a.Grant.Status(now)), Replayed: a.Replayed || redact}
	if a.Link != "" && !redact {
		l := a.Link
		d.Link = &l
		d.QR = &qrDTO{Format: "url", Data: l, ErrorCorrection: "M", MinModulePx: 4}
	}
	return d
}

type farmerDTO struct {
	UserRef        string    `json:"user_ref"`
	HouseholdRef   string    `json:"household_ref"`
	CooperativeRef string    `json:"cooperative_ref"`
	CommuneCode    string    `json:"commune_code"`
	LinkedAt       string    `json:"linked_at"`
	Enabled        *bool     `json:"enabled,omitempty"`
	LatestGrant    *grantDTO `json:"latest_activation,omitempty"`
}

func toFarmerDTO(l domain.FarmerLink) farmerDTO {
	return farmerDTO{UserRef: l.UserRef.String(), HouseholdRef: l.HouseholdRef.String(), CooperativeRef: l.CooperativeRef.String(),
		CommuneCode: l.CommuneCode, LinkedAt: l.LinkedAt.UTC().Format(time.RFC3339)}
}

// writeSecret gửi phản hồi chứa liên kết một lần: không cache, và bản lưu cho phát
// lại (Idempotency-Key) là bản đã lược liên kết.
func writeSecret(c *gin.Context, status int, full, redacted any) {
	if b, err := json.Marshal(redacted); err == nil {
		idempotency.SetStoredBody(c, b)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, full)
}

type createFarmerReq struct {
	HouseholdRef *string `json:"household_ref"`
	Phone        string  `json:"phone" pii:"true"`
	FullName     string  `json:"full_name" pii:"true"`
}

func (h Handler) createFarmer(c *gin.Context) {
	var req createFarmerReq
	if !bindJSON(c, &req) {
		return
	}
	hh, err := optRef(req.HouseholdRef, "household_ref")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	prof := domain.FarmerProfile{Phone: req.Phone}
	prof.FirstName, prof.LastName = domain.SplitFullName(req.FullName)
	res, err := h.Svc.CreateFarmer(c.Request.Context(), app.FarmerInput{HouseholdRef: hh, Profile: prof, IdemKey: key(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	now := h.Svc.Clock.Now()
	body := func(redact bool) gin.H {
		b := gin.H{"farmer": toFarmerDTO(res.Link), "replayed": res.Replayed, "activation": toActivationDTO(res.Activation, now, redact),
			"activation_error": nil}
		if res.ActivationErr != nil {
			b["activation_error"] = problem.From(res.ActivationErr).Code
		}
		return b
	}
	c.Header("Location", "/v1/farmer-accounts/"+res.Link.UserRef.String()+"/activation")
	writeSecret(c, status, body(false), body(true))
}

func (h Handler) reissue(c *gin.Context) {
	id, ok := pathFarmer(c)
	if !ok {
		return
	}
	a, err := h.Svc.ReissueActivation(c.Request.Context(), id, key(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	now := h.Svc.Clock.Now()
	writeSecret(c, http.StatusCreated, gin.H{"activation": toActivationDTO(a, now, false)}, gin.H{"activation": toActivationDTO(a, now, true)})
}

func pathFarmer(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrFarmerNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h Handler) activations(c *gin.Context) {
	id, ok := pathFarmer(c)
	if !ok {
		return
	}
	gs, err := h.Svc.Activations(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]grantDTO, 0, len(gs))
	for _, g := range gs {
		out = append(out, toGrantDTO(g.Grant, g.Status))
	}
	c.JSON(http.StatusOK, gin.H{"user_ref": id.String(), "items": out})
}

func (h Handler) listFarmers(c *gin.Context) {
	var coop, after uuid.UUID
	if s := c.Query("cooperative_ref"); s != "" {
		u, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("cooperative_ref", "phải là UUID"))
			return
		}
		coop = u
	}
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		u, perr := uuid.ParseBytes(b)
		if err != nil || perr != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		after = u
	}
	pg, err := h.Svc.ListFarmers(c.Request.Context(), coop, after, queryLimit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]farmerDTO, 0, len(pg.Items))
	for _, it := range pg.Items {
		d := toFarmerDTO(it.Link)
		en := it.Enabled
		d.Enabled = &en
		if it.Latest != nil {
			g := toGrantDTO(it.Latest.Grant, it.Latest.Status)
			d.LatestGrant = &g
		}
		items = append(items, d)
	}
	resp := gin.H{"items": items, "next_cursor": nil}
	if pg.Next != nil {
		resp["next_cursor"] = base64.RawURLEncoding.EncodeToString([]byte(pg.Next.String()))
	}
	c.JSON(http.StatusOK, resp)
}

func queryLimit(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

func (h Handler) importFarmers(c *gin.Context) {
	res, err := h.Svc.ImportFarmers(c.Request.Context(), io.LimitReader(c.Request.Body, MaxFarmerImportBytes+1), key(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	now := h.Svc.Clock.Now()
	rows := func(redact bool) []gin.H {
		out := make([]gin.H, 0, len(res.Rows))
		for _, r := range res.Rows {
			row := gin.H{"row": r.Row, "status": r.Status, "reason": strPtr(r.Reason), "user_ref": refPtr(r.UserRef),
				"household_ref": refPtr(r.HouseholdRef), "activation": toActivationDTO(r.Activation, now, redact)}
			out = append(out, row)
		}
		return out
	}
	writeSecret(c, http.StatusOK,
		gin.H{"created": res.Created, "rejected": res.Rejected, "rows": rows(false)},
		gin.H{"created": res.Created, "rejected": res.Rejected, "rows": rows(true)})
}

func (h Handler) myHouseholds(c *gin.Context) {
	links, err := h.Svc.MyHouseholds(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(links))
	for _, l := range links {
		out = append(out, gin.H{"household_ref": l.HouseholdRef.String(), "cooperative_ref": l.CooperativeRef.String(), "commune_code": l.CommuneCode})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) coopStaff(c *gin.Context) {
	staff, err := h.Svc.CoopStaffDirectory(c.Request.Context(), c.Query("q"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(staff))
	for _, st := range staff {
		out = append(out, gin.H{"user_ref": st.UserRef.String(), "cooperative_ref": st.CooperativeRef.String(),
			"display_name": st.DisplayName, "title": st.Title, "has_phone": st.HasPhone})
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) coopStaffContact(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrStaffNotFound)
		return
	}
	phone, err := h.Svc.CoopStaffPhone(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"user_ref": id.String(), "phone": phone})
}
