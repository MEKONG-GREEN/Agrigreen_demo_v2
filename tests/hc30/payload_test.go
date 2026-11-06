package hc30_test

import "github.com/google/uuid"

// Test payloads follow the public JSON contract, without importing HRM app/domain
// from the integration assembly root (archlint R-02).
type periodInput struct {
	Year         int                  `json:"year"`
	Quarter      int                  `json:"quarter"`
	CutoffOn     string               `json:"cutoff_on"`
	SelfDueAt    string               `json:"self_due_at"`
	ManagerDueAt string               `json:"manager_due_at"`
	SummaryDueAt string               `json:"summary_due_at"`
	Forms        map[string]uuid.UUID `json:"forms"`
	Summaries    map[string]uuid.UUID `json:"summaries"`
}
type templateChange struct {
	Status  string         `json:"status"`
	Summary map[string]any `json:"summary,omitempty"`
	Form    *formInput     `json:"form,omitempty"`
}
type formInput struct {
	Code     string           `json:"code"`
	Name     string           `json:"name"`
	Group    string           `json:"group"`
	Criteria []criterionInput `json:"criteria"`
	Bands    []bandInput      `json:"bands"`
}
type criterionInput struct {
	Code    string `json:"code"`
	Title   string `json:"title"`
	Maximum string `json:"maximum"`
}
type bandInput struct {
	Minimum string `json:"minimum"`
	Rating  string `json:"rating"`
}
type answerInput struct {
	Code   string `json:"code"`
	Points string `json:"points"`
	Note   string `json:"note"`
}
type entryInput struct {
	Action        string        `json:"action"`
	Version       int           `json:"version"`
	Answers       []answerInput `json:"answers"`
	Reason        string        `json:"reason"`
	ProxyDocument *uuid.UUID    `json:"proxy_document,omitempty"`
}

func summaryTemplate(kind string) map[string]any {
	code, number := "ms05", "05"
	if kind == "MS06" {
		code, number = "ms06", "06"
	}
	columns := []map[string]string{}
	for _, x := range []struct{ key, title string }{{"ordinal", "STT"}, {"staff", "Họ tên"}, {"position_unit", "Chức vụ,đơn vị"}, {"self_rating", "Cá nhân đề xuất"}, {"collective_rating", "Tập thể đề xuất"}, {"reason", "Căn cứ"}, {"personnel_proposal", "Công tác cán bộ"}, {"note", "Ghi chú"}} {
		columns = append(columns, map[string]string{"key": x.key, "title": x.title})
	}
	return map[string]any{"code": code, "kind": kind, "number": number, "name": "Mẫu số " + number, "title": "Tổng hợp quý {QUY} năm {NAM}", "audience": "Viên chức nghiệm thu", "signature": "Tập thể lãnh đạo ký", "columns": columns, "ratings": map[string]string{"HTXSNV": "Hoàn thành xuất sắc nhiệm vụ", "HTTNV": "Hoàn thành tốt nhiệm vụ", "HTNV": "Hoàn thành nhiệm vụ", "KHTNV": "Không hoàn thành nhiệm vụ"}}
}
