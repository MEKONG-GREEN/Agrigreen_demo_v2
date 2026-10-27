package app

// BF-CN-WEEKLY (PRD UP-06, BRD CNTS UR-XA-09 CV-04): "Kế hoạch đã đăng ký thành Lịch công tác chờ tỉnh duyệt". Cán bộ
// tăng cường (CNTS Cấp Xã) đăng ký BM-05 Phần II ở cap/forms (/v1/forms — lý do bắt buộc khi sửa sau hạn: profile
// kn_cnts RequireLateReason); sau đó màn "Báo cáo công việc tuần" gọi ScheduleFromCNTSPlan: đọc bài nộp CUỐI của kỳ ×
// xã (formsport.SubmissionFeed, NGOÀI giao dịch KN), dựng mục lịch theo từng ngày có kế hoạch và GỬI DUYỆT lịch tuần
// của chính người gọi qua SaveSchedule (cùng kiểm Xã/Địa điểm, khóa tuần, người duyệt, thông báo chuông như đăng ký
// lịch thường — R-B1, R-B2).
//
// Mục sinh từ kế hoạch có mã ổn định theo NGUỒN (người, tuần, xã) và ngày — cán bộ phụ trách nhiều xã nộp một kế hoạch
// cho mỗi xã, nên mỗi xã là một nguồn riêng (#110 review Codex P2). Đăng ký lại kế hoạch của một xã chỉ thay mục của
// đúng xã đó (lịch sử phiên bản, "phần điều chỉnh chờ duyệt lại"); mục sinh từ kế hoạch xã khác và mục người dùng tự
// thêm ở Lịch công tác được giữ nguyên. Hai nguồn cùng ngày (Thứ Hai–Thứ Sáu luôn bắt buộc ở mọi xã) cho mục trùng
// giờ — Lịch công tác chứa được và đánh dấu "trùng giờ" (domain.Conflicts, FN-XA-03.08: cảnh báo, không chặn) để
// người dùng chỉnh ở Lịch công tác trước khi tỉnh duyệt; không âm thầm xóa kế hoạch xã khác.

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rcdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi đăng ký kế hoạch tuần → lịch công tác (docs/errors.md, mục pf/extension).
var (
	ErrCNTSPlanMissing  = problem.New(http.StatusConflict, "extension.cnts_plan_not_registered", "Chưa đăng ký kế hoạch tuần (BM-05 Phần II) của kỳ và xã này")
	ErrCNTSPlanNotOwner = problem.New(http.StatusForbidden, "extension.cnts_plan_not_owner", "Kế hoạch tuần do cán bộ khác đăng ký — chỉ người đăng ký tạo được lịch công tác từ kế hoạch")
)

// Giờ của mục lịch sinh từ kế hoạch (cả ngày làm việc) và địa điểm mặc định khi làm việc tại xã.
const (
	planStart        = "07:00"
	planEnd          = "17:00"
	planLocationHome = "Làm việc tại xã"
	planNoteMax      = 1000
)

// planItemNS là không gian tên UUIDv5 của mục lịch sinh từ kế hoạch tuần.
var planItemNS = uuid.MustParse("4b0f3c1e-6a52-5b8e-9d0c-2f7a1c3e5d10")

// PlanItemID là mã mục lịch ổn định của ngày offset (0 = Thứ Hai) trong kế hoạch tuần week của người owner tại xã
// commune (nguồn kế hoạch). Kỳ BM-05 Phần II ứng một-một với tuần nên (tuần, xã) xác định nguồn; kỳ cài lại cho cùng
// tuần vẫn là cùng nguồn (thay, không nhân đôi).
func PlanItemID(owner uuid.UUID, week time.Time, commune string, offset int) uuid.UUID {
	return uuid.NewSHA1(planItemNS, []byte(owner.String()+"|"+week.Format(time.DateOnly)+"|"+commune+"|"+strconv.Itoa(offset)))
}

// planScheduleItems đổi các ngày có kế hoạch thành mục lịch công tác của xã commune.
func planScheduleItems(owner uuid.UUID, week time.Time, commune string, days []rcdomain.PlanDay) []domain.ScheduleItem {
	out := make([]domain.ScheduleItem, 0, len(days))
	for _, d := range days {
		loc := d.Place
		if loc == "" {
			loc = planLocationHome
		}
		var note []string
		if d.Kind == rcdomain.WorkTypeOther && d.Other != "" {
			note = append(note, "Công tác khác: "+d.Other)
		}
		if d.Expected != "" {
			note = append(note, "Kết quả dự kiến: "+d.Expected)
		}
		n := strings.Join(note, " · ")
		if r := []rune(n); len(r) > planNoteMax {
			n = string(r[:planNoteMax])
		}
		content := d.Plan
		if content == "" {
			content = d.Kind
		}
		out = append(out, domain.ScheduleItem{ItemID: PlanItemID(owner, week, commune, d.Offset), Day: week.AddDate(0, 0, d.Offset),
			Session: domain.SessionMorning, Start: planStart, End: planEnd, Content: content, CommuneCode: commune, Location: loc, Note: n})
	}
	return out
}

// ScheduleFromCNTSPlan gửi duyệt Lịch công tác tuần của người gọi từ kế hoạch BM-05 Phần II đã đăng ký (kỳ periodID,
// xã unit). Gọi lặp cùng khóa trả kết quả lần đầu.
func (s *Service) ScheduleFromCNTSPlan(ctx context.Context, periodID uuid.UUID, unit, key string) (ScheduleDetail, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return ScheduleDetail{}, err
	}
	if a.ReadOnly {
		return ScheduleDetail{}, problem.Forbidden.WithDetail("ngữ cảnh chỉ xem")
	}
	if a.Role != port.KNRoleXa || a.Department != partyport.DeptChanNuoiThuySan {
		return ScheduleDetail{}, problem.Forbidden.WithDetail("chỉ cán bộ xã Phòng Chăn nuôi – Thủy sản đăng ký kế hoạch tuần")
	}
	if err := a.registrant(); err != nil {
		return ScheduleDetail{}, err
	}
	unit = strings.TrimSpace(unit)
	if !slices.Contains(a.Communes, unit) {
		return ScheduleDetail{}, problem.Forbidden.WithDetail("xã " + unit + " không thuộc các xã phụ trách")
	}
	if key == "" {
		return ScheduleDetail{}, problem.IdempotencyRequired
	}
	planKey := "cntsplan:" + key
	if replay, ok, err := s.scheduleReplay(ctx, a, idemSave(planKey)); err != nil || ok {
		return replay, err
	}
	if s.PlanFeed == nil {
		return ScheduleDetail{}, ErrDependency.WithDetail("chưa nối cap/forms (SubmissionFeed)")
	}
	subs, err := s.PlanFeed.Latest(ctx, a.TenantID, formsport.SubmissionQuery{Profile: rcdomain.ProfileCNTS,
		TemplateCode: rcdomain.CNTSBM05II, Units: []string{unit}, Limit: 200})
	if err != nil {
		return ScheduleDetail{}, err
	}
	var sub *formsport.RecordedSubmission
	for i := range subs {
		if subs[i].PeriodID == periodID && subs[i].Unit == unit {
			sub = &subs[i]
			break
		}
	}
	if sub == nil || sub.From == nil {
		return ScheduleDetail{}, ErrCNTSPlanMissing
	}
	if sub.Submitter != a.ActorRef {
		return ScheduleDetail{}, ErrCNTSPlanNotOwner
	}
	f := sub.From.UTC()
	week := time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, time.UTC)
	if !domain.ValidWeekStart(week) {
		return ScheduleDetail{}, ErrCNTSPlanMissing.WithDetail("kỳ kế hoạch không bắt đầu từ Thứ Hai")
	}
	days := rcdomain.PlanDays(sub.Values)
	if len(days) == 0 {
		return ScheduleDetail{}, problem.Validation.WithField("values", "kế hoạch tuần chưa có ngày nào")
	}
	plan := planScheduleItems(a.ActorRef, week, unit, days)

	// Lịch hiện hành của tuần: chỉ thay mục sinh từ kế hoạch CỦA XÃ NÀY (mọi ngày của tuần); giữ mục của kế hoạch xã
	// khác và mục người dùng tự thêm.
	var cur *domain.Schedule
	var curItems []domain.ScheduleItem
	err = tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		var e error
		if cur, e = store.ScheduleByOwnerWeek(ctx, tx, a.ActorRef, a.Department, a.level(), week, false); e != nil || cur == nil {
			return e
		}
		curItems, e = store.ScheduleItems(ctx, tx, cur.ID, cur.Version)
		return e
	})
	if err != nil {
		return ScheduleDetail{}, err
	}
	planIDs := map[uuid.UUID]bool{}
	for i := range rcdomain.WorkDays {
		planIDs[PlanItemID(a.ActorRef, week, unit, i)] = true
	}
	items := make([]domain.ScheduleItem, 0, len(curItems)+len(plan))
	for _, it := range curItems {
		if !planIDs[it.ItemID] {
			it.RowID, it.Version, it.SupersedesID = uuid.Nil, 0, nil
			items = append(items, it)
		}
	}
	items = append(items, plan...)
	var base *int
	if cur != nil {
		v := cur.Version
		base = &v
	}
	return s.SaveSchedule(ctx, ScheduleSaveInput{WeekStart: week, Action: domain.SaveSubmit, BaseVersion: base, Items: items, IdemKey: planKey})
}
