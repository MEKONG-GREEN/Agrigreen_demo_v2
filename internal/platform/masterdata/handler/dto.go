// Package handler: HTTP của pf/masterdata — Application API chỉ đọc
// (contracts/openapi/application.yaml) và Admin API (contracts/openapi/admin.yaml).
package handler

import (
	"encoding/base64"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

type unitDTO struct {
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	Level          string   `json:"level"`
	ParentCode     *string  `json:"parent_code"`
	ValidFrom      string   `json:"valid_from"`
	ValidTo        *string  `json:"valid_to"`
	SuccessorCodes []string `json:"successor_codes"`
}

func toUnitDTO(u domain.Unit) unitDTO {
	d := unitDTO{Code: u.Code, Name: u.Name, Level: string(u.Level), ValidFrom: u.ValidFrom.Format(time.DateOnly), SuccessorCodes: u.Successors}
	if d.SuccessorCodes == nil {
		d.SuccessorCodes = []string{}
	}
	if u.ParentCode != "" {
		pc := u.ParentCode
		d.ParentCode = &pc
	}
	if u.ValidTo != nil {
		vt := u.ValidTo.Format(time.DateOnly)
		d.ValidTo = &vt
	}
	return d
}

type treeNode struct {
	unitDTO
	Children []*treeNode `json:"children"`
}

// buildTree dựng cây từ danh sách (gốc đứng đầu) theo parent_code.
func buildTree(us []domain.Unit) *treeNode {
	nodes := make(map[string]*treeNode, len(us))
	for _, u := range us {
		nodes[u.Code] = &treeNode{unitDTO: toUnitDTO(u), Children: []*treeNode{}}
	}
	root := nodes[us[0].Code]
	for _, u := range us[1:] {
		if p, ok := nodes[u.ParentCode]; ok {
			p.Children = append(p.Children, nodes[u.Code])
		}
	}
	return root
}

type itemDTO struct {
	Catalog            string            `json:"catalog"`
	Code               string            `json:"code"`
	Name               string            `json:"name"`
	Attrs              map[string]string `json:"attrs"`
	Active             bool              `json:"active"`
	DeactivationReason *string           `json:"deactivation_reason"`
	Revision           int               `json:"revision"`
	UpdatedAt          string            `json:"updated_at"`
	// FormsInUse: số mẫu báo cáo đang dùng mục (KN v7.7 FN-AD-04.05 "biểu đang dùng"); vắng khi chưa nối P3.
	FormsInUse *int `json:"forms_in_use,omitempty"`
}

func toItemDTO(it domain.Item) itemDTO {
	d := itemDTO{Catalog: it.Catalog, Code: it.Code, Name: it.Name, Attrs: it.Attrs, Active: it.Active, Revision: it.Revision,
		UpdatedAt: it.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	if d.Attrs == nil {
		d.Attrs = map[string]string{}
	}
	if it.DeactivationReason != "" {
		r := it.DeactivationReason
		d.DeactivationReason = &r
	}
	return d
}

type changeDTO struct {
	ID         string           `json:"id"`
	Action     string           `json:"action"`
	Before     *domain.Snapshot `json:"before"`
	After      domain.Snapshot  `json:"after"`
	Reason     *string          `json:"reason"`
	EventTime  string           `json:"event_time"`
	ActorRef   string           `json:"actor_ref"`
	Seq        int64            `json:"seq"`
	RecordedAt string           `json:"recorded_at"`
}

func toChangeDTO(c domain.Change) changeDTO {
	d := changeDTO{ID: c.ID.String(), Action: c.Action, Before: c.Before, After: c.After, EventTime: c.EventTime.UTC().Format(time.RFC3339Nano),
		ActorRef: c.ActorRef.String(), Seq: c.Seq, RecordedAt: c.RecordedAt.UTC().Format(time.RFC3339Nano)}
	if c.Reason != "" {
		r := c.Reason
		d.Reason = &r
	}
	return d
}

type versionDTO struct {
	ID               string  `json:"id"`
	Key              string  `json:"key"`
	Version          int     `json:"version"`
	Value            *string `json:"value"`
	Text             *string `json:"text"`
	Unit             string  `json:"unit"`
	EffectiveFrom    string  `json:"effective_from"`
	SourceDoc        string  `json:"source_doc"`
	CorrectionReason *string `json:"correction_reason"`
	Status           string  `json:"status"`
	SetterRef        string  `json:"setter_ref"`
	SubmittedAt      *string `json:"submitted_at"`
	ApproverRef      *string `json:"approver_ref"`
	DecidedAt        *string `json:"decided_at"`
	DecisionNote     *string `json:"decision_note"`
	CreatedAt        string  `json:"created_at"`
	// SetterName/ApproverName: tên cán bộ từ danh bạ pf/extension (G-07); null khi chưa có trong danh bạ.
	SetterName   *string `json:"setter_name"`
	ApproverName *string `json:"approver_name"`
}

func ts(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

func toVersionDTO(v domain.ParamVersion) versionDTO {
	d := versionDTO{ID: v.ID.String(), Key: v.Key, Version: v.Version, Text: v.Text, Unit: v.Unit,
		EffectiveFrom: v.EffectiveFrom.UTC().Format(time.RFC3339), SourceDoc: v.SourceDoc, CorrectionReason: v.CorrectionReason,
		Status: string(v.Status), SetterRef: v.SetterRef.String(), SubmittedAt: ts(v.SubmittedAt), DecidedAt: ts(v.DecidedAt),
		DecisionNote: v.DecisionNote, CreatedAt: v.CreatedAt.UTC().Format(time.RFC3339Nano)}
	if v.Value != nil {
		s := v.Value.String()
		d.Value = &s
	}
	if v.ApproverRef != nil {
		s := v.ApproverRef.String()
		d.ApproverRef = &s
	}
	return d
}

type paramValueDTO struct {
	Key            string  `json:"key"`
	Value          *string `json:"value"`
	Text           *string `json:"text"`
	Unit           string  `json:"unit"`
	Version        int     `json:"version"`
	EffectiveFrom  string  `json:"effective_from"`
	SourceDoc      string  `json:"source_doc"`
	ApprovedAt     string  `json:"approved_at"`
	ParamVersionID string  `json:"param_version_id"`
	SupersedesID   *string `json:"supersedes_id"`
	Seq            int64   `json:"seq"`
}

func toParamValueDTO(a domain.Approved) paramValueDTO {
	d := paramValueDTO{Key: a.Key, Text: a.Text, Unit: a.Unit, Version: a.Version, EffectiveFrom: a.EffectiveFrom.UTC().Format(time.RFC3339),
		SourceDoc: a.SourceDoc, ApprovedAt: a.EventTime.UTC().Format(time.RFC3339Nano), ParamVersionID: a.ParamVersionID.String(), Seq: a.Seq}
	if a.Value != nil {
		s := a.Value.String()
		d.Value = &s
	}
	if a.SupersedesID != nil {
		s := a.SupersedesID.String()
		d.SupersedesID = &s
	}
	return d
}

// at đọc tham số ?at= (RFC 3339 hoặc YYYY-MM-DD giờ Việt Nam); mặc định là hiện tại.
func (h Handler) at(c *gin.Context) (time.Time, error) {
	s := c.Query("at")
	if s == "" {
		return h.Svc.Clock.Now(), nil
	}
	t, err := domain.ParseInstant(s)
	if err != nil {
		return time.Time{}, problem.Validation.WithField("at", "phải theo RFC 3339 hoặc YYYY-MM-DD")
	}
	return t, nil
}

func limit(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

func encodeCursor(s *string) any {
	if s == nil {
		return nil
	}
	return base64.RawURLEncoding.EncodeToString([]byte(*s))
}

func decodeCursor(c *gin.Context) (string, error) {
	cur := c.Query("cursor")
	if cur == "" {
		return "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(cur)
	if err != nil || len(b) == 0 || len(b) > 200 {
		return "", problem.Validation.WithField("cursor", "không hợp lệ")
	}
	return string(b), nil
}
