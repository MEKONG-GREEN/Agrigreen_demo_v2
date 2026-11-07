package hc30_test

import "github.com/google/uuid"

// Test payloads follow the public JSON contract, without importing HRM app/domain
// from the integration assembly root (archlint R-02).
type periodInput struct {
	Year         int                  `json:"year"`
	Quarter      int                  `json:"quarter"`
	CutoffOn     string               `json:"cutoff_on"`
	ManagerDueAt string               `json:"manager_due_at"`
	Groups       []string             `json:"groups"`
	Summaries    map[string]uuid.UUID `json:"summaries"`
}
type templateChange struct {
	Status  string         `json:"status"`
	Summary map[string]any `json:"summary,omitempty"`
}
type entryInput struct {
	Action           string `json:"action"`
	SelfRating       string `json:"self_rating"`
	CollectiveRating string `json:"collective_rating"`
	Version          int    `json:"version"`
	Reason           string `json:"reason"`
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
