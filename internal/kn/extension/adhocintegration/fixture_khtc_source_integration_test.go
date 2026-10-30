// Test fixture copied from existing KN77 helpers only, without duplicating legacy Test cases.
// Colocated probes may inspect their own module internals; composition-root tests remain port-only.
package adhocintegration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"fmt"

	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"

	khtcgolden "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/golden/khtc"
)

// goldenReader là port KHTCSources giả (dạng kết quả của closedfig.Assemble — gốc lắp ráp không import closedfig): kỳ nguồn Trồng trọt (PL01, ô lua_td/luy_ke) theo một bước của golden.
type goldenReader struct {
	mu      sync.Mutex
	periods []khtcgolden.SourcePeriod
	status  string // đổi → chỉ status_hash đổi (nhận HTX — D2)
}

func (g *goldenReader) set(ps []khtcgolden.SourcePeriod, status string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.periods, g.status = ps, status
}

func hexSHA(s string) *string {
	h := sha256.Sum256([]byte(s))
	x := hex.EncodeToString(h[:])
	return &x
}

// khtcWeek trả khoảng ngày của tuần KHTC "YYYY-Www" (Thứ Tư – Thứ Ba, tuần 1 chứa 01/01 — lịch v2, golden Q1).
func khtcWeek(key string) (time.Time, time.Time) {
	var y, w int
	_, _ = fmt.Sscanf(key, "%d-W%d", &y, &w)
	jan1 := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
	start := jan1.AddDate(0, 0, -((int(jan1.Weekday()) - int(time.Wednesday) + 7) % 7))
	from := start.AddDate(0, 0, 7*(w-1))
	return from, from.AddDate(0, 0, 6)
}

// Periods dựng kỳ KHTC như port KHTCSources (closedfig.Assemble) cho các tuần có kỳ nguồn Trồng trọt: tuần Trồng trọt
// trùng tuần KHTC (v2), một kỳ nguồn mỗi tuần, ô "LUA_TD/luy_ke" = lũy kế nguồn.
func (g *goldenReader) Periods(_ context.Context, _ uuid.UUID, q extport.KHTCQuery) ([]extport.KHTCPeriod, error) {
	g.mu.Lock()
	ps, status := g.periods, g.status
	g.mu.Unlock()
	var out []extport.KHTCPeriod
	for _, p := range ps {
		from, to := khtcWeek(p.Key)
		if to.Before(q.From) || to.After(q.To) {
			continue
		}
		ver, sn := 2, int64(1)
		closedAt := to
		src := extport.SourcePeriodRef{CalendarID: partyport.DeptTrongTrot, CalendarVersion: &ver, NativeKey: p.Key, PeriodType: "week",
			From: from, To: to, Source: "reportcycle", SourceModule: "pf/extension", SourcePeriodID: uuid.NewSHA1(uuid.NameSpaceURL, []byte(p.Key)),
			FormCode: "PL01", Closed: p.Closed, ClosedAt: &closedAt, SnapshotNo: &sn, FiguresHash: hexSHA(p.Key + "=" + p.Cumulative.String()),
			StatusHash: hexSHA(p.Key + "=" + status), ConfirmationStatus: extport.ConfirmationConfirmed,
			Cells: map[string]string{"LUA_TD/luy_ke": p.Cumulative.String()}}
		cells := map[string]string{"LUA_TD/luy_ke": p.Cumulative.String()}
		fig := extport.TargetFigure{RuleVersion: extport.CrosswalkV1, DepartmentCode: partyport.DeptTrongTrot, FormCode: "PL01",
			TargetCalendarID: partyport.DeptKeHoachTaiChinh, TargetCalendarVersion: 2, TargetType: "week", TargetKey: p.Key, TargetFrom: from,
			TargetTo: to, Sources: []extport.SourcePeriodRef{src}, Closed: p.Closed, KeyClosed: p.Closed, Sum: cells, Last: cells,
			SumConfirmed: cells, LastConfirmed: cells, FiguresHash: *src.FiguresHash, StatusHash: *src.StatusHash}
		out = append(out, extport.KHTCPeriod{RuleVersion: extport.CrosswalkV1, TargetCalendarID: partyport.DeptKeHoachTaiChinh,
			TargetCalendarVersion: 2, TargetType: "week", TargetKey: p.Key, TargetFrom: from, TargetTo: to, Closed: p.Closed,
			Departments: []extport.DepartmentFigures{{DepartmentCode: partyport.DeptTrongTrot, SourceModule: "pf/extension",
				Forms: []extport.TargetFigure{fig}, Closed: p.Closed}}})
	}
	return out, nil
}

// tlog chuyển log lỗi của engine vào log kiểm thử (chẩn đoán HTTP 500).
type tlog struct{ t *testing.T }

func (w tlog) Write(p []byte) (int, error) { w.t.Log(string(p)); return len(p), nil }
