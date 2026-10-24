package app

import (
	"context"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/domain"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

// limitFeed giả lập Feed.Latest theo đúng hợp đồng cap/forms: lọc mẫu/đơn vị/khoảng ngày đầu kỳ, bỏ loại kỳ trong
// ExcludePeriodKinds TRƯỚC LIMIT, mới trước, rồi cắt Limit.
type limitFeed struct {
	formsport.SubmissionFeed
	rows    []formsport.RecordedSubmission
	queries []formsport.SubmissionQuery
}

func (f *limitFeed) Latest(_ context.Context, _ uuid.UUID, q formsport.SubmissionQuery) ([]formsport.RecordedSubmission, error) {
	f.queries = append(f.queries, q)
	var out []formsport.RecordedSubmission
	for _, r := range f.rows {
		if r.TemplateCode != q.TemplateCode || (len(q.Units) > 0 && !slices.Contains(q.Units, r.Unit)) ||
			slices.Contains(q.ExcludePeriodKinds, r.PeriodKind) || r.From == nil ||
			(q.From != nil && r.From.Before(*q.From)) || (q.To != nil && r.From.After(*q.To)) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].From.After(*out[j].From) })
	limit := q.Limit
	if limit == 0 {
		limit = 200
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// P2 review #27: lịch sử trộn — 5 đợt điều tra `adhoc` (W11.50) mới hơn các kỳ định kỳ, đều nằm trong cửa sổ lùi
// cnts.lookback_periods (mặc định 4 → LIMIT 5). Bài nộp định kỳ vẫn phải so với kỳ định kỳ gần nhất, không bị đợt
// điều tra đẩy ra khỏi cửa sổ LIMIT.
func TestRegularHistoryBehindCampaigns_W1150(t *testing.T) {
	day := func(m time.Month, d int) *time.Time { v := time.Date(2026, m, d, 0, 0, 0, 0, time.UTC); return &v }
	row := func(kind string, from, to *time.Time, v string) formsport.RecordedSubmission {
		return formsport.RecordedSubmission{TemplateCode: domain.FormBM04, PeriodKind: kind, Unit: "30001", From: from, To: to,
			Values: map[string]string{"marker": v}}
	}
	feed := &limitFeed{rows: []formsport.RecordedSubmission{
		row("week", day(10, 5), day(10, 11), "tuan-truoc-nua"),
		row("week", day(10, 12), day(10, 18), "tuan-truoc"),
	}}
	for d := 13; d <= 17; d++ { // 5 đợt điều tra mới hơn kỳ định kỳ gần nhất, cùng biểu, cùng xã
		feed.rows = append(feed.rows, row(periodAdhoc, day(10, d), day(10, d+3), "dot-dieu-tra"))
	}
	s := &Service{Feed: feed}
	sub := formsport.RecordedSubmission{TemplateCode: domain.FormBM04, PeriodKind: "week", Unit: "30001",
		From: day(10, 19), To: day(10, 25), EventTime: *day(10, 25)}

	prev, err := s.previous(context.Background(), uuid.New(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if prev == nil {
		t.Fatal("mất kỳ định kỳ gần nhất sau 5 đợt điều tra adhoc trong cửa sổ lùi")
	}
	if prev["marker"] != "tuan-truoc" {
		t.Fatalf("so với %q, muốn kỳ định kỳ gần nhất \"tuan-truoc\"", prev["marker"])
	}
	if len(feed.queries) != 1 || !slices.Equal(feed.queries[0].ExcludePeriodKinds, []string{periodAdhoc}) {
		t.Fatalf("truy vấn lịch sử phải loại kỳ adhoc trước LIMIT: %+v", feed.queries)
	}
}
