package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// WeeklyService owns FN-TI-02.13 and shares only the existing reporting calendar.
type WeeklyService struct{ *TicketService }

// NewWeeklyService uses the module's existing DB, clock and calendar ports.
func NewWeeklyService(t *TicketService) *WeeklyService { return &WeeklyService{TicketService: t} }

type weeklyAccess struct {
	prof    port.Profile
	c       caller
	session port.SessionContext
	allowed bool
}

func (s *WeeklyService) accessWeekly(ctx context.Context, profile string) (weeklyAccess, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return weeklyAccess{}, err
	}
	a := weeklyAccess{prof: prof, c: c}
	if prof.Session == nil {
		a.allowed = c.level == "TINH" // Native realm-only profiles have no admin support context.
		return a, nil
	}
	a.session, err = prof.Session(ctx)
	if err != nil {
		return a, err
	}
	a.allowed = !a.session.ReadOnly && a.session.Department == prof.Department &&
		(c.level == "TINH" || (c.manage && a.session.Support))
	if a.session.Support {
		p, _ := authz.FromContext(ctx)
		a.allowed = a.allowed && p.MFA
	}
	return a, nil
}

// AuthorizeWeekly runs before shared idempotency, so a cached write cannot bypass current scope.
func (s *WeeklyService) AuthorizeWeekly(ctx context.Context, profile string) error {
	a, err := s.accessWeekly(ctx, profile)
	if err != nil {
		return err
	}
	if !a.allowed {
		return problem.Forbidden.WithDetail("chỉ Cấp Tỉnh phòng TTTT và Admin trong ngữ cảnh hỗ trợ được thao tác")
	}
	return nil
}

// WeeklySummary is a selectable reporting period.
type WeeklySummary struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Key    string `json:"key"`
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"`
}

// WeeklyView returns a selected projection and program audit, always within the caller's tenant.
type WeeklyView struct {
	Program  string                   `json:"program"`
	Title    string                   `json:"title"`
	PlanYear int                      `json:"plan_year"`
	Version  int64                    `json:"version"`
	Reports  []WeeklySummary          `json:"reports"`
	Report   *domain.WeeklyProjection `json:"report"`
	Log      []store.WeeklyRecord     `json:"log"`
}

func weeklyView(program, selected string, r store.WeeklyRecord, log []store.WeeklyRecord) (WeeklyView, error) {
	v := WeeklyView{Program: program, Title: domain.WeeklyTitle(program), PlanYear: 2026, Version: r.Seq, Reports: []WeeklySummary{}, Log: log}
	for _, w := range r.Book.Reports {
		v.Reports = append(v.Reports, WeeklySummary{w.ID, w.Number, w.Key, w.From, w.To, w.Status})
		if selected == w.ID || (selected == "" && w.ID == r.Book.Reports[len(r.Book.Reports)-1].ID) {
			p := domain.ProjectWeekly(w, r.Book.Deleted)
			v.Report = &p
		}
	}
	if v.Report == nil {
		return v, port.ErrWeeklyNotFound
	}
	return v, nil
}

func newWeeklyReport(week domain.Week, previous *domain.WeeklyReport, program string, deleted map[string]bool) domain.WeeklyReport {
	n := 0
	if at := strings.LastIndex(week.Key, "-W"); at >= 0 {
		n, _ = strconv.Atoi(week.Key[at+2:])
	}
	if n <= 0 {
		n = 1
		if previous != nil {
			n = previous.Number + 1
		}
	}
	r := domain.WeeklyReport{ID: ids.New().String(), Number: n, Key: week.Key, From: week.From.Format(time.DateOnly), To: week.To.Format(time.DateOnly), Status: "draft", Items: []domain.WeeklyItem{}, ImportIssues: []domain.WeeklyIssue{}}
	if previous == nil {
		byCode := map[string]string{}
		for _, c := range domain.WeeklyCatalog(program) {
			byCode[c.Code] = ids.New().String()
		}
		for _, c := range domain.WeeklyCatalog(program) {
			i := domain.WeeklyItem{ID: byCode[c.Code], Name: c.Name, Group: c.Plan == ""}
			if c.Parent != "" {
				i.ParentID = byCode[c.Parent]
			}
			if c.Plan != "" {
				i.Plan = decimal.RequireFromString(c.Plan)
			}
			r.Items = append(r.Items, i)
		}
		return r
	}
	for _, i := range previous.Items {
		if deleted[i.ID] {
			continue
		}
		i.OpeningProgress, i.OpeningMoney, _ = i.Cumulative()
		i.Progress, i.Money = decimal.Zero, decimal.Zero
		i.ProgressSet, i.MoneySet = false, false
		i.MoneyOverride = nil
		i.Evaluation = ""
		i.Note = ""
		r.Items = append(r.Items, i)
	}
	return r
}

// Read initializes the first draft with official catalog data, under the same program lock as writes.
// Unauthorized commune/national readers receive an empty response without querying the ledger.
func (s *WeeklyService) Read(ctx context.Context, profile, program, selected string) (WeeklyView, error) {
	a, err := s.accessWeekly(ctx, profile)
	if err != nil {
		return WeeklyView{}, err
	}
	if !a.allowed {
		if a.c.level == "XA" || a.c.level == "TW" {
			return WeeklyView{Reports: []WeeklySummary{}, Log: []store.WeeklyRecord{}}, nil
		}
		return WeeklyView{}, problem.Forbidden
	}
	if domain.WeeklyTitle(program) == "" {
		return WeeklyView{}, port.ErrWeeklyNotFound
	}
	week, err := s.weekOf(ctx, domain.BusinessDay(s.Clock.Now())) // Calendar I/O is outside the DB transaction.
	if err != nil {
		return WeeklyView{}, err
	}
	var result WeeklyView
	err = s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		if err := store.LockWeekly(ctx, tx, a.c.tenant, a.prof.Name, program); err != nil {
			return err
		}
		r, err := store.LoadWeekly(ctx, tx, a.prof.Name, program, "")
		if err != nil {
			return err
		}
		if r.Seq == 0 {
			w := newWeeklyReport(week, nil, program, nil)
			book := domain.WeeklyBook{Reports: []domain.WeeklyReport{w}, Deleted: map[string]bool{}}
			r, err = store.InsertWeekly(ctx, tx, a.c.tenant, a.prof.Name, program, w.ID, "initialize", "weekly-initialize", a.c.ref, s.Clock.Now(), []byte{}, book)
			if err != nil {
				return err
			}
		}
		log, err := store.WeeklyHistory(ctx, tx, a.prof.Name, program)
		if err != nil {
			return err
		}
		result, err = weeklyView(program, selected, r, log)
		return err
	})
	return result, err
}

// WeeklyNewItem creates a main item, a group with children, or one child under an empty main item.
type WeeklyNewItem struct {
	Name     string          `json:"name"`
	Plan     string          `json:"plan"`
	ParentID string          `json:"parent_id"`
	Children []WeeklyNewItem `json:"children"`
}

// WeeklyCommand is the CAS input for every modifying operation, including import confirmation.
type WeeklyCommand struct {
	Version  int64                `json:"version"`
	WeekID   string               `json:"week_id"`
	Action   string               `json:"action"`
	Rows     []domain.WeeklyPatch `json:"rows"`
	Item     *WeeklyNewItem       `json:"item"`
	ItemID   string               `json:"item_id"`
	Plan     string               `json:"plan"`
	Reason   string               `json:"reason"`
	Confirm  bool                 `json:"confirm"`
	Issues   []domain.WeeklyIssue `json:"-"`
	FileHash string               `json:"-"`
}

var weeklyIdem = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,200}$`)

// Write authorizes before replay and checks the program version before changing any draft.
func (s *WeeklyService) Write(ctx context.Context, profile, program, idem string, in WeeklyCommand) (WeeklyView, error) {
	a, err := s.accessWeekly(ctx, profile)
	if err != nil {
		return WeeklyView{}, err
	}
	if !a.allowed {
		return WeeklyView{}, problem.Forbidden
	}
	if domain.WeeklyTitle(program) == "" {
		return WeeklyView{}, port.ErrWeeklyNotFound
	}
	if !weeklyIdem.MatchString(idem) {
		return WeeklyView{}, problem.IdempotencyRequired
	}
	if in.Version < 1 || in.WeekID == "" {
		return WeeklyView{}, problem.Validation.WithField("version", "cần phiên bản và kỳ đang xem")
	}
	// These fields are excluded from public JSON input but included in the replay identity.
	identity := in
	if in.Action == "import" {
		identity.Rows = nil
	}
	raw, err := json.Marshal(struct {
		Input    WeeklyCommand
		FileHash string
		Session  port.SessionContext
	}{identity, in.FileHash, a.session})
	if err != nil {
		return WeeklyView{}, err
	}
	hash := sha256.Sum256(raw)
	var next domain.Week
	if in.Action == "new" {
		v, err := s.Read(ctx, profile, program, in.WeekID)
		if err != nil {
			return WeeklyView{}, err
		}
		to, err := time.Parse(time.DateOnly, v.Report.To)
		if err != nil {
			return WeeklyView{}, err
		}
		next, err = s.weekOf(ctx, to.AddDate(0, 0, 1))
		if err != nil {
			return WeeklyView{}, err
		}
	}
	var result WeeklyView
	err = s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		if err := store.LockWeekly(ctx, tx, a.c.tenant, a.prof.Name, program); err != nil {
			return err
		}
		prior, err := store.LoadWeekly(ctx, tx, a.prof.Name, program, idem)
		if err != nil {
			return err
		}
		if prior.Seq != 0 {
			if prior.Actor != a.c.ref || !bytes.Equal(prior.Hash, hash[:]) {
				return problem.Conflict.WithDetail("Idempotency-Key đã dùng cho thao tác khác")
			}
			log, err := store.WeeklyHistory(ctx, tx, a.prof.Name, program)
			if err != nil {
				return err
			}
			log = slices.DeleteFunc(log, func(record store.WeeklyRecord) bool { return record.Seq > prior.Seq })
			result, err = weeklyView(program, prior.WeekID, prior, log)
			return err
		}
		r, err := store.LoadWeekly(ctx, tx, a.prof.Name, program, "")
		if err != nil {
			return err
		}
		if r.Seq != in.Version {
			return port.ErrWeeklyStale
		}
		at := slices.IndexFunc(r.Book.Reports, func(w domain.WeeklyReport) bool { return w.ID == in.WeekID })
		if at < 0 {
			return port.ErrWeeklyNotFound
		}
		w := &r.Book.Reports[at]
		selected := w.ID
		if in.Action == "new" {
			if w.Status != "closed" || at != len(r.Book.Reports)-1 {
				return port.ErrWeeklyClosed
			}
			if next.Key == w.Key || next.From.Format(time.DateOnly) <= w.To {
				return problem.Validation.WithField("week", "lịch tuần kế tiếp không hợp lệ")
			}
			created := newWeeklyReport(next, w, program, r.Book.Deleted)
			selected = created.ID
			r.Book.Reports = append(r.Book.Reports, created)
		} else {
			if w.Status != "draft" || at != len(r.Book.Reports)-1 {
				return port.ErrWeeklyClosed
			}
			if err := applyWeeklyCommand(&r.Book, w, program, in); err != nil {
				return err
			}
		}
		r, err = store.InsertWeekly(ctx, tx, a.c.tenant, a.prof.Name, program, selected, in.Action, idem, a.c.ref, s.Clock.Now(), hash[:], r.Book)
		if err != nil {
			return classify(err)
		}
		log, err := store.WeeklyHistory(ctx, tx, a.prof.Name, program)
		if err != nil {
			return err
		}
		result, err = weeklyView(program, selected, r, log)
		return err
	})
	return result, err
}

func applyWeeklyCommand(book *domain.WeeklyBook, w *domain.WeeklyReport, program string, in WeeklyCommand) error {
	switch in.Action {
	case "save", "close", "import":
		if in.Action == "close" && !in.Confirm {
			return port.ErrWeeklyConfirm.WithDetail("xác nhận chốt báo cáo tuần")
		}
		if err := domain.ApplyWeeklyPatches(w, book.Deleted, program, in.Rows); err != nil {
			return problem.Validation.WithField("rows", err.Error())
		}
		if in.Action == "close" {
			w.Status = "closed"
		}
		if in.Action == "import" {
			w.ImportIssues = in.Issues
		}
		return nil
	case "adjust":
		i := slices.IndexFunc(w.Items, func(i domain.WeeklyItem) bool { return i.ID == in.ItemID && !book.Deleted[i.ID] })
		if i < 0 {
			return port.ErrWeeklyNotFound
		}
		if w.Items[i].Group {
			return problem.Validation.WithField("item_id", "mục nhóm không có số kế hoạch")
		}
		plan, err := domain.ParseWeeklyNumber(in.Plan, false)
		if err != nil {
			return problem.Validation.WithField("plan", err.Error())
		}
		reason := strings.TrimSpace(in.Reason)
		if n := utf8.RuneCountInString(reason); n < 1 || n > 500 {
			return problem.Validation.WithField("reason", "cần lý do, tối đa 500 ký tự")
		}
		w.Items[i].AdjustedPlan = &plan
		w.Items[i].PlanReason = reason
		return nil
	case "add":
		return addWeeklyItem(book, w, in.Item)
	case "delete":
		return deleteWeeklyItem(book, w, in.ItemID, in.Confirm)
	default:
		return problem.Validation.WithField("action", "save | close | new | adjust | add | delete")
	}
}

func addWeeklyItem(book *domain.WeeklyBook, w *domain.WeeklyReport, input *WeeklyNewItem) error {
	if input == nil {
		return problem.Validation.WithField("item", "cần hạng mục")
	}
	if len(w.Items)+len(input.Children)+1 > 200 {
		return problem.Validation.WithField("item", "tối đa 200 dòng mỗi chương trình")
	}
	all := append([]WeeklyNewItem{*input}, input.Children...)
	seen := map[string]bool{}
	for _, i := range w.Items {
		if !book.Deleted[i.ID] {
			seen[tabular.NormalizeHeader(i.Name)] = true
		}
	}
	for j, i := range all {
		name := strings.TrimSpace(i.Name)
		key := tabular.NormalizeHeader(name)
		if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 200 || key == "" || seen[key] {
			return problem.Validation.WithField("item.name", "tên bắt buộc, không trùng, tối đa 200 ký tự")
		}
		if j > 0 && (len(i.Children) > 0 || i.ParentID != "") {
			return problem.Validation.WithField("item.children", "chỉ có một cấp mục con")
		}
		if j > 0 || len(input.Children) == 0 {
			if _, err := domain.ParseWeeklyNumber(i.Plan, false); err != nil {
				return problem.Validation.WithField("item.plan", err.Error())
			}
		}
		seen[key] = true
	}
	parentAt := -1
	if input.ParentID != "" {
		if len(input.Children) > 0 {
			return problem.Validation.WithField("item.parent_id", "mục con không chứa nhóm")
		}
		parentAt = slices.IndexFunc(w.Items, func(i domain.WeeklyItem) bool { return i.ID == input.ParentID && !book.Deleted[i.ID] })
		if parentAt < 0 || w.Items[parentAt].ParentID != "" {
			return problem.Validation.WithField("item.parent_id", "chỉ thêm con vào mục chính")
		}
		if !w.Items[parentAt].Group {
			for _, report := range book.Reports {
				for _, i := range report.Items {
					if i.ID == input.ParentID && i.HasData() {
						return problem.Validation.WithField("item.parent_id", "mục chính đã có số liệu, không thể chuyển thành nhóm")
					}
				}
			}
			w.Items[parentAt].Group = true
			w.Items[parentAt].Plan = decimal.Zero
			w.Items[parentAt].AdjustedPlan = nil
			w.Items[parentAt].PlanReason = ""
		}
	}
	root := domain.WeeklyItem{ID: ids.New().String(), Name: strings.TrimSpace(input.Name), ParentID: input.ParentID, Group: len(input.Children) > 0}
	if !root.Group {
		root.Plan = decimal.RequireFromString(input.Plan)
	}
	added := []domain.WeeklyItem{root}
	for _, c := range input.Children {
		added = append(added, domain.WeeklyItem{ID: ids.New().String(), ParentID: root.ID, Name: strings.TrimSpace(c.Name), Plan: decimal.RequireFromString(c.Plan)})
	}
	if parentAt < 0 {
		w.Items = append(w.Items, added...)
		return nil
	}
	pos := parentAt + 1
	for pos < len(w.Items) && w.Items[pos].ParentID == input.ParentID {
		pos++
	}
	w.Items = slices.Insert(w.Items, pos, added...)
	return nil
}

func deleteWeeklyItem(book *domain.WeeklyBook, w *domain.WeeklyReport, id string, confirm bool) error {
	i := slices.IndexFunc(w.Items, func(i domain.WeeklyItem) bool { return i.ID == id && !book.Deleted[i.ID] })
	if i < 0 {
		return port.ErrWeeklyNotFound
	}
	remove := map[string]bool{id: true}
	for _, report := range book.Reports {
		for _, item := range report.Items {
			if item.ParentID == id {
				remove[item.ID] = true
			}
		}
	}
	hasData := false
	for _, report := range book.Reports {
		for _, item := range report.Items {
			if remove[item.ID] && item.HasData() {
				hasData = true
			}
		}
	}
	if hasData && !confirm {
		return port.ErrWeeklyConfirm.WithDetail("xóa làm thay đổi lũy kế và tổng kinh phí các tuần, kể cả tuần đã chốt; cần xác nhận")
	}
	if book.Deleted == nil {
		book.Deleted = map[string]bool{}
	}
	for item := range remove {
		book.Deleted[item] = true
	}
	return nil
}
