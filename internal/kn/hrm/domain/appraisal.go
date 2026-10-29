package domain

// W11.26 — quy tắc thuần FN-15/21. MS05/06 chứa mức xếp loại, còn điểm của phiếu chỉ là gợi ý.
// Điểm lưu theo phần trăm của một điểm (hai chữ số lẻ), không dùng số thực hay đọc chuỗi số mũ.

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	AppraisalCivil   = "vien_chuc"
	AppraisalLeader  = "lanh_dao"
	AppraisalCommune = "can_bo_xa"
	// Trần kỹ thuật để giới hạn kích thước phiếu/tính toán; miền từng ô vẫn là [0, điểm tối đa tiêu chí].
	AppraisalCriterionLimit = 200
	AppraisalPointLimit     = int64(100_000_000) // 1.000.000 điểm, đơn vị 1/100 điểm
)

var appraisalPointSyntax = regexp.MustCompile(`^[0-9]{1,7}(?:\.[0-9]{1,2})?$`)
var appraisalCodeSyntax = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// AppraisalInputError giữ đường dẫn ô để ứng dụng trả validation_failed với errors[].field.
type AppraisalInputError struct{ Field, Message string }

func (e AppraisalInputError) Error() string { return e.Field + ": " + e.Message }
func appraisalInvalid(field, message string) error {
	return AppraisalInputError{Field: field, Message: message}
}

// ParseAppraisalPoints kiểm độ dài/cú pháp/biên bằng chuỗi TRƯỚC ParseInt hay phép tính theo đầu vào.
// max có đơn vị 1/100 điểm và phải là một biên đã biết của máy chủ, không lấy thẳng từ request.
func ParseAppraisalPoints(raw string, max int64) (int64, error) {
	if len(raw) > 32 || max < 0 || max > AppraisalPointLimit {
		return 0, fmt.Errorf("điểm vượt giới hạn")
	}
	text := strings.TrimSpace(raw)
	if !appraisalPointSyntax.MatchString(text) {
		return 0, fmt.Errorf("nhập số thập phân thường không âm, tối đa 2 số lẻ")
	}
	whole, fraction, _ := strings.Cut(text, ".")
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	fraction += strings.Repeat("0", 2-len(fraction))
	// So với biên bằng chuỗi trước chuyển kiểu; mỗi phép tính sau có tối đa 9 chữ số.
	limit := strconv.FormatInt(max/100, 10)
	if len(whole) > len(limit) || (len(whole) == len(limit) && whole > limit) ||
		(whole == limit && fraction > fmt.Sprintf("%02d", max%100)) {
		return 0, fmt.Errorf("không vượt quá %s điểm", AppraisalPoints(max))
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("điểm không hợp lệ: %w", err)
	}
	f, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("điểm không hợp lệ: %w", err)
	}
	return w*100 + f, nil
}

// AppraisalPoints trả dạng thập phân canonical từ một điểm đã kiểm, không qua float.
func AppraisalPoints(points int64) string {
	if points%100 == 0 {
		return strconv.FormatInt(points/100, 10)
	}
	return strings.TrimRight(fmt.Sprintf("%d.%02d", points/100, points%100), "0")
}

type AppraisalCriterion struct {
	Code    string `json:"code"`
	Group   string `json:"group"`
	Title   string `json:"title"`
	Maximum string `json:"maximum"`
	Guide   string `json:"guide"`
}

// AppraisalBand là cận dưới bao gồm; danh sách từ cao xuống thấp, cận cuối phải bằng 0.
type AppraisalBand struct {
	Minimum string `json:"minimum"`
	Rating  string `json:"rating"`
}

type AppraisalForm struct {
	Code      string               `json:"code"`
	Name      string               `json:"name"`
	Group     string               `json:"group"`
	Criteria  []AppraisalCriterion `json:"criteria"`
	Bands     []AppraisalBand      `json:"bands"`
	Questions []string             `json:"questions"`
}

func validAppraisalGroup(group string) bool {
	return group == AppraisalCivil || group == AppraisalLeader || group == AppraisalCommune
}

// ValidateAppraisalForm trả bản sao canonical. Nháp cho phép chưa có tiêu chí/bảng quy đổi;
// phát hành/gắn vào đợt bắt buộc đầy đủ. Không sửa slice hay dữ liệu của người gọi.
func ValidateAppraisalForm(in AppraisalForm, publish bool) (AppraisalForm, error) {
	f := in
	f.Criteria, f.Bands, f.Questions = slices.Clone(in.Criteria), slices.Clone(in.Bands), slices.Clone(in.Questions)
	f.Code, f.Name = strings.TrimSpace(f.Code), strings.TrimSpace(f.Name)
	if !appraisalCodeSyntax.MatchString(f.Code) {
		return f, appraisalInvalid("code", "mã gồm chữ thường, số, _, bắt đầu bằng chữ; tối đa 40 ký tự")
	}
	if f.Name == "" || utf8.RuneCountInString(f.Name) > 200 {
		return f, appraisalInvalid("name", "nhập tên phiếu, tối đa 200 ký tự")
	}
	if !validAppraisalGroup(f.Group) {
		return f, appraisalInvalid("group", "chọn viên chức, lãnh đạo hoặc cán bộ xã")
	}
	if len(f.Criteria) > AppraisalCriterionLimit || (publish && len(f.Criteria) == 0) {
		return f, appraisalInvalid("criteria", "phiếu phát hành cần 1–200 tiêu chí")
	}
	seen := map[string]bool{}
	total := int64(0)
	for i := range f.Criteria {
		c := &f.Criteria[i]
		c.Code, c.Title = strings.TrimSpace(c.Code), strings.TrimSpace(c.Title)
		field := fmt.Sprintf("criteria.%d", i)
		if !appraisalCodeSyntax.MatchString(c.Code) || seen[c.Code] {
			return f, appraisalInvalid(field+".code", "mã tiêu chí không hợp lệ hoặc trùng")
		}
		seen[c.Code] = true
		if c.Title == "" || utf8.RuneCountInString(c.Title) > 500 || utf8.RuneCountInString(c.Group) > 200 || utf8.RuneCountInString(c.Guide) > 2000 {
			return f, appraisalInvalid(field+".title", "nhập tên tiêu chí tối đa 500 ký tự; nhóm tối đa 200, hướng dẫn tối đa 2.000")
		}
		point, err := ParseAppraisalPoints(c.Maximum, AppraisalPointLimit-total)
		if err != nil || point == 0 {
			return f, appraisalInvalid(field+".maximum", "điểm tối đa phải lớn hơn 0, tối đa 2 số lẻ; tổng phiếu không vượt 1.000.000")
		}
		c.Maximum = AppraisalPoints(point)
		total += point
	}
	if len(f.Bands) != 0 || publish {
		if len(f.Bands) != len(Ratings) {
			return f, appraisalInvalid("bands", "bảng quy đổi cần đủ bốn mức xếp loại")
		}
		previous := total + 1
		for i := range f.Bands {
			b := &f.Bands[i]
			point, err := ParseAppraisalPoints(b.Minimum, total)
			if err != nil || point >= previous || b.Rating != Ratings[i] {
				return f, appraisalInvalid(fmt.Sprintf("bands.%d.minimum", i), "cận giảm dần trong tổng điểm, theo thứ tự HTXSNV/HTTNV/HTNV/KHTNV")
			}
			b.Minimum = AppraisalPoints(point)
			previous = point
		}
		if previous != 0 {
			return f, appraisalInvalid("bands", "cận cuối bằng 0 để phủ hết thang điểm")
		}
	}
	if len(f.Questions) > 20 {
		return f, appraisalInvalid("questions", "tối đa 20 câu hỏi nhận xét")
	}
	for i, q := range f.Questions {
		f.Questions[i] = strings.TrimSpace(q)
		if f.Questions[i] == "" || utf8.RuneCountInString(q) > 1000 {
			return f, appraisalInvalid(fmt.Sprintf("questions.%d", i), "câu hỏi nhận xét cần nội dung, tối đa 1.000 ký tự")
		}
	}
	return f, nil
}

type AppraisalAnswer struct {
	Code   string `json:"code"`
	Points string `json:"points"`
	Note   string `json:"note"`
}

type AppraisalScoring struct {
	Answers   []AppraisalAnswer `json:"answers"`
	Total     string            `json:"total"`
	Suggested string            `json:"suggested"`
	Complete  bool              `json:"complete"`
}

// ScoreAppraisal dùng đúng phiên bản đã chụp của phiếu, từ chối cả mã lạ/trùng và điểm vượt từng tiêu chí.
// Nháp thiếu điểm không có gợi ý; nộp phải đầy đủ. Mức chính thức không được quyết định bởi hàm này.
func ScoreAppraisal(form AppraisalForm, answers []AppraisalAnswer, submit bool) (AppraisalScoring, error) {
	f, err := ValidateAppraisalForm(form, true)
	if err != nil {
		return AppraisalScoring{}, err
	}
	if len(answers) > len(f.Criteria) {
		return AppraisalScoring{}, appraisalInvalid("answers", "số câu trả lời vượt tiêu chí của phiếu")
	}
	byCode := map[string]AppraisalAnswer{}
	for _, answer := range answers {
		if _, ok := byCode[answer.Code]; ok {
			return AppraisalScoring{}, appraisalInvalid("answers", "trùng mã tiêu chí")
		}
		byCode[answer.Code] = answer
	}
	out := AppraisalScoring{Answers: make([]AppraisalAnswer, 0, len(answers)), Complete: true}
	total := int64(0)
	for i, criterion := range f.Criteria {
		answer, ok := byCode[criterion.Code]
		delete(byCode, criterion.Code)
		field := fmt.Sprintf("answers.%d.points", i)
		if utf8.RuneCountInString(answer.Note) > 2000 {
			return out, appraisalInvalid(fmt.Sprintf("answers.%d.note", i), "nhận xét tối đa 2.000 ký tự")
		}
		if !ok || strings.TrimSpace(answer.Points) == "" {
			if submit {
				return out, appraisalInvalid(field, "chấm đủ điểm từng tiêu chí trước khi nộp")
			}
			out.Complete = false
			if ok {
				answer.Points = ""
				out.Answers = append(out.Answers, answer)
			}
			continue
		}
		max, _ := ParseAppraisalPoints(criterion.Maximum, AppraisalPointLimit)
		points, err := ParseAppraisalPoints(answer.Points, max)
		if err != nil {
			return out, appraisalInvalid(field, err.Error())
		}
		answer.Points = AppraisalPoints(points)
		out.Answers = append(out.Answers, answer)
		total += points
	}
	if len(byCode) != 0 {
		return out, appraisalInvalid("answers", "mã tiêu chí không thuộc phiên bản phiếu của đợt")
	}
	out.Total = AppraisalPoints(total)
	if out.Complete {
		for _, band := range f.Bands {
			min, _ := ParseAppraisalPoints(band.Minimum, AppraisalPointLimit)
			if total >= min {
				out.Suggested = band.Rating
				break
			}
		}
	}
	return out, nil
}

// AppraisalSummaryCode phân người từ chức vụ/loại tại ngày chốt, không từ lựa chọn MS05/06 của client.
func AppraisalSummaryCode(typ StaffType, leader bool) string {
	if typ != StaffCanBoXa && leader {
		return "MS06"
	}
	return "MS05"
}

// ValidateAppraisalProposal giữ mức xếp loại tách khỏi điểm phiếu, bắt căn cứ cho cả hai cột DG04/DG05.
func ValidateAppraisalProposal(self, collective, reason string, complete bool) error {
	for _, value := range []struct{ field, rating string }{{"self_rating", self}, {"collective_rating", collective}} {
		field, rating := value.field, value.rating
		if rating == "" && !complete {
			continue
		}
		if !slices.Contains(Ratings, rating) {
			return appraisalInvalid(field, "chọn một trong bốn mức xếp loại")
		}
	}
	if utf8.RuneCountInString(reason) > 2000 {
		return appraisalInvalid("reason", "tóm tắt căn cứ tối đa 2.000 ký tự")
	}
	if (self == "HTXSNV" || self == "KHTNV" || collective == "HTXSNV" || collective == "KHTNV") && strings.TrimSpace(reason) == "" {
		return appraisalInvalid("reason", "căn cứ bắt buộc khi cá nhân hoặc tập thể đề xuất HTXSNV/KHTNV")
	}
	return nil
}
