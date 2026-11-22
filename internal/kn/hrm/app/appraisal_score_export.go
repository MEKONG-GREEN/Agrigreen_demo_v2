package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"strconv"
	"strings"
)

// AppraisalScores là bảng nội bộ FN21; không phải biểu giấy tám cột MS05/MS06.
const AppraisalScores = "PHIEU_DIEM"

type AppraisalScoreLayout struct {
	Version string                   `json:"version"`
	Title   string                   `json:"title"`
	Columns []domain.AppraisalColumn `json:"columns"`
	Ratings map[string]string        `json:"ratings"`
}

func defaultAppraisalScoreLayout() AppraisalScoreLayout {
	return AppraisalScoreLayout{Version: "hrm.appraisal-scores/v1", Title: "TỔNG HỢP ĐIỂM PHIẾU ĐÁNH GIÁ QUÝ {QUY}, NĂM {NAM}", Ratings: domain.DefaultAppraisalSummary("MS05").Ratings, Columns: []domain.AppraisalColumn{
		{Key: "ordinal", Title: "STT"}, {Key: "staff", Title: "Họ và tên"}, {Key: "position_unit", Title: "Chức vụ, đơn vị"}, {Key: "status", Title: "Trạng thái"},
		{Key: "self_points", Title: "Tổng điểm tự đánh giá"}, {Key: "manager_points", Title: "Tổng điểm cấp quản lý"}, {Key: "suggested", Title: "Mức gợi ý theo phiếu"},
		{Key: "self_rating", Title: "Cá nhân đề xuất"}, {Key: "collective_rating", Title: "Tập thể đề xuất"}, {Key: "official_rating", Title: "Kết luận chính thức"}}}
}
func appraisalScoreTemplate(l AppraisalScoreLayout) domain.AppraisalSummaryTemplate {
	return domain.AppraisalSummaryTemplate{Kind: AppraisalScores, Code: "phieu_diem", Title: l.Title, Audience: "Bảng theo dõi nội bộ; không thay Mẫu số 05, 06", Signature: "Người tổng hợp", Columns: l.Columns, Ratings: l.Ratings}
}
func appraisalScoreCells(x AppraisalItem, ordinal int, t domain.AppraisalSummaryTemplate) []string {
	suggested := x.Self.Suggested
	if x.Manager.Complete {
		suggested = x.Manager.Suggested
	}
	status := map[string]string{domain.AppraisalNotStarted: "Chưa tự đánh giá", domain.AppraisalSelfSubmitted: "Đã tự đánh giá", domain.AppraisalManagerSubmitted: "Cấp quản lý đã đánh giá", domain.AppraisalSent: "Đã gửi HCTH", domain.AppraisalChangeRequested: "Yêu cầu điều chỉnh", domain.AppraisalClosed: "Đã chốt"}[x.Status]
	name := x.Name
	if name == "" {
		name = x.Member.Code
	}
	values := map[string]string{"ordinal": strconv.Itoa(ordinal), "staff": name, "position_unit": strings.Trim(strings.Join([]string{x.Member.PositionName, x.Member.UnitName}, ", "), ", "), "status": status,
		"self_points": x.Self.Total, "manager_points": x.Manager.Total, "suggested": t.Ratings[suggested], "self_rating": t.Ratings[x.SelfRating], "collective_rating": t.Ratings[x.CollectiveRating], "official_rating": t.Ratings[x.OfficialRating]}
	out := make([]string, 0, len(t.Columns))
	for _, c := range t.Columns {
		out = append(out, values[c.Key])
	}
	return out
}
