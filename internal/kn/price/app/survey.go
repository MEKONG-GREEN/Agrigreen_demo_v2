package app

// Điều tra giá thành sản xuất theo vụ — đợt điều tra (W11.78, pf 20261020161354; BRD Báo giá v1.3 FN-TI-02.10, Mục II.3
// bảng 3.4):
//   - Cấp Tỉnh tạo / sửa / mở / đóng sớm / gia hạn đợt; Quản trị nghiệp vụ mở lại đợt đã chốt (lý do bắt buộc); Admin chỉ
//     xem (ma trận phân quyền Mục V.2). Cấp TW, Cấp Xã không vào màn đợt.
//   - Xã điều tra từ danh mục ĐVHC (k_ref.admin_unit cấp xã) + chỉ tiêu theo nhóm cây; cán bộ điều tra (họ tên, SĐT ở kho
//     định danh — chủ thể người, mục đích port.SurveyPurpose; không bắt buộc có tài khoản).
//   - "Đang rà soát" khi hết thời gian thu thập là phép chiếu lúc đọc (domain.RoundState, BR-03) — không job.
//   - Hai đợt trùng vụ, năm, nhóm cây → 409 price.survey_round_duplicate, lưu được khi confirm = true.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// SurveyService là điều tra giá thành (đợt, nhập tệp, rà soát, báo cáo) trên TicketService (xuất qua điểm kiểm soát chung).
type SurveyService struct {
	*TicketService
	// Vault: kho định danh — họ tên, SĐT nông hộ và cán bộ điều tra. nil → ghi / đọc dữ liệu cá nhân trả 503.
	Vault client.Client
	// PII: cổng dữ liệu thật (port.EnvSurveyPII — chờ DPIA, Q-005). false → nhập tệp, khai báo cán bộ trả 503.
	PII bool
}

// NewSurveyService dựng SurveyService.
func NewSurveyService(t *TicketService) *SurveyService { return &SurveyService{TicketService: t} }

const surveyPurpose = client.Purpose(port.SurveyPurpose)

// surveyAccess là quyền của người gọi với điều tra giá thành.
type surveyAccess struct {
	c       caller
	prof    port.Profile
	tinh    bool // Cấp Tỉnh: CRUD đợt, nhập tệp, rà soát
	read    bool // xem đợt, phiếu (Cấp Tỉnh, Admin)
	pii     bool // xem họ tên, SĐT nông hộ / cán bộ điều tra (Cấp Tỉnh, Admin — BR-03 FN-TI-02.11)
	steward bool // Quản trị nghiệp vụ: mở lại đợt đã chốt
}

func (s *SurveyService) access(ctx context.Context, profile string) (surveyAccess, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return surveyAccess{}, err
	}
	a := surveyAccess{c: c, prof: prof, tinh: c.level == "TINH", steward: c.steward}
	a.read = a.tinh || c.manage
	a.pii = a.read
	return a, nil
}

func (a surveyAccess) needRead() error {
	if !a.read {
		return problem.Forbidden.WithDetail("chỉ Cấp Tỉnh, Admin xem điều tra giá thành")
	}
	return nil
}

func (a surveyAccess) needTinh() error {
	if !a.tinh {
		return problem.Forbidden.WithDetail("chỉ Cấp Tỉnh quản lý đợt điều tra và rà soát phiếu")
	}
	return nil
}

func (s *SurveyService) today() time.Time { return domain.BusinessDay(s.Clock.Now()) }

// vault trả kho khi cổng dữ liệu thật bật.
func (s *SurveyService) vault() (client.Client, error) {
	if s.Vault == nil || !s.PII {
		return nil, port.ErrSurveyPIIDisabled
	}
	return s.Vault, nil
}

// identities đọc họ tên, SĐT của các chủ thể (một lượt hạn mức cho mỗi 200 chủ thể khi kho hỗ trợ đọc theo lô; không thì
// từng chủ thể). Kho chưa nối → rỗng. Chủ thể lạ / đã xóa định danh → không có trong kết quả.
func (s *SurveyService) identities(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]client.Identity, error) {
	out := map[uuid.UUID]client.Identity{}
	if s.Vault == nil || len(refs) == 0 {
		return out, nil
	}
	uniq := slices.Compact(slices.SortedFunc(slices.Values(refs), func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) }))
	if b, ok := client.AsProfileBatch(s.Vault); ok {
		for i := 0; i < len(uniq); i += client.MaxBatchRefs {
			chunk := uniq[i:min(i+client.MaxBatchRefs, len(uniq))]
			items, err := b.ReadBatch(ctx, client.BatchRequest{Purpose: surveyPurpose, Identity: true, Refs: chunk})
			if err != nil {
				return nil, err
			}
			for _, it := range items {
				if !it.Erased {
					out[it.Ref] = client.Identity{FullName: it.FullName, Phone: it.Phone}
				}
			}
		}
		return out, nil
	}
	for _, r := range uniq {
		id, err := s.Vault.Resolve(ctx, surveyPurpose, r)
		if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrErased) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[r] = id
	}
	return out, nil
}

// MaskPhone che SĐT: 4 số đầu · 3 số cuối (như nhãn của kho định danh).
func MaskPhone(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "84") && len(p) == 11 {
		p = "0" + p[2:]
	}
	if len(p) < 8 {
		return "•••"
	}
	return p[:4] + "•••" + p[len(p)-3:]
}

// ---------------------------------------------------------------------------------------------------------------------
// Đợt điều tra
// ---------------------------------------------------------------------------------------------------------------------

// CommuneInput là một xã điều tra + chỉ tiêu theo nhóm cây.
type CommuneInput struct {
	Code    string
	Targets map[string]int
}

// OfficerInput là một cán bộ điều tra: ID có → giữ chủ thể cũ (đổi xã / đơn vị); ID rỗng → khai báo mới (họ tên, SĐT bắt
// buộc — ghi vào kho định danh).
type OfficerInput struct {
	ID       uuid.UUID
	Commune  string
	FullName string
	Phone    string
	Unit     string
}

// RoundInput là dữ liệu tạo / sửa đợt (3 bước: Thông tin – Xã & chỉ tiêu – Cán bộ điều tra).
type RoundInput struct {
	Name     string
	Season   string
	Year     int
	From, To time.Time
	Crops    []string
	Note     string
	Communes []CommuneInput
	Officers []OfficerInput
	Confirm  bool  // lưu dù trùng vụ, năm, nhóm cây
	Open     bool  // tạo và mở đợt ngay (B4)
	Seq      int64 // sửa: phiên bản đang xem (khóa lạc quan)
}

func (in *RoundInput) validate() error {
	v, bad := problem.Validation, false
	in.Name = strings.TrimSpace(in.Name)
	in.Note = strings.TrimSpace(in.Note)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 120 {
		v, bad = v.WithField("name", "bắt buộc, tối đa 120 ký tự"), true
	}
	if _, ok := domain.Seasons[in.Season]; !ok {
		v, bad = v.WithField("season", "dong_xuan | he_thu | thu_dong | khac"), true
	}
	if in.Year < 2000 || in.Year > 2100 {
		v, bad = v.WithField("year", "năm 2000–2100"), true
	}
	if in.From.IsZero() || in.To.IsZero() || in.To.Before(in.From) {
		v, bad = v.WithField("collect_to", "thời gian thu thập từ ≤ đến"), true
	}
	if utf8.RuneCountInString(in.Note) > 500 {
		v, bad = v.WithField("note", "tối đa 500 ký tự"), true
	}
	seen := map[string]bool{}
	for _, c := range in.Crops {
		if !domain.ValidCrop(c) || seen[c] {
			v, bad = v.WithField("crops", "nhóm cây lua | rau_mau | nam | cay_an_trai, không lặp"), true
			break
		}
		seen[c] = true
	}
	cs := map[string]bool{}
	for i, c := range in.Communes {
		in.Communes[i].Code = strings.TrimSpace(c.Code)
		if in.Communes[i].Code == "" || cs[in.Communes[i].Code] {
			v, bad = v.WithField("communes", "mã xã bắt buộc, không lặp"), true
			break
		}
		cs[in.Communes[i].Code] = true
		for crop, n := range c.Targets {
			if !domain.ValidCrop(crop) || n < 0 || n > 100000 {
				v, bad = v.WithField("communes", "chỉ tiêu theo nhóm cây ≥ 0"), true
			}
		}
	}
	for i, o := range in.Officers {
		in.Officers[i].Unit = strings.TrimSpace(o.Unit)
		in.Officers[i].FullName = strings.TrimSpace(o.FullName)
		if !cs[o.Commune] {
			v, bad = v.WithField("officers", "cán bộ điều tra phải thuộc xã điều tra của đợt"), true
			break
		}
		if n := utf8.RuneCountInString(in.Officers[i].Unit); n < 1 || n > 200 {
			v, bad = v.WithField("officers", "đơn vị công tác bắt buộc, tối đa 200 ký tự"), true
			break
		}
		if o.ID == uuid.Nil {
			if n := utf8.RuneCountInString(in.Officers[i].FullName); n < 1 || n > 200 {
				v, bad = v.WithField("officers", "họ tên cán bộ điều tra bắt buộc"), true
				break
			}
			if _, ok := domain.NormalizePhone(o.Phone); !ok {
				v, bad = v.WithField("officers", "số điện thoại cán bộ điều tra không hợp lệ"), true
				break
			}
		}
	}
	if bad {
		return v
	}
	return nil
}

// RoundSummary là một dòng danh sách đợt.
type RoundSummary struct {
	ID          uuid.UUID
	Name        string
	Season      string
	Year        int
	From, To    time.Time
	Crops       []string
	Status      string // trạng thái hiệu lực (domain.RoundState)
	Communes    int
	Forms       int // phiếu đã nhập (không kể Đã xóa)
	Target      int
	Seq         int64
	Pending     int // phiếu chờ rà soát / xác minh
	CreatedAt   time.Time
	StoredState string
}

// RoundOfficer là cán bộ điều tra trong chi tiết đợt.
type RoundOfficer struct {
	ID       uuid.UUID
	Commune  string
	FullName string // chỉ Cấp Tỉnh, Admin
	Phone    string // chỉ Cấp Tỉnh, Admin
	Unit     string
	Forms    int
}

// RoundCommune là xã điều tra + tiến độ (AC-03: phiếu đã nhập / chỉ tiêu).
type RoundCommune struct {
	Code     string
	Name     string
	Targets  map[string]int
	Imported map[string]int
	Officers []RoundOfficer
}

// RoundLog là một dòng nhật ký đợt.
type RoundLog struct {
	Seq    int64
	Op     string
	Status string
	Reason string
	At     time.Time
	Actor  uuid.UUID
}

// RoundDetail là chi tiết đợt.
type RoundDetail struct {
	RoundSummary
	Note       string
	CommuneSet []RoundCommune
	Log        []RoundLog
	CanEdit    bool
	CanReopen  bool
	Warnings   []string // xã chưa giao cán bộ điều tra (cảnh báo trước khi mở đợt)

	officerRefs map[uuid.UUID]uuid.UUID // cán bộ → chủ thể kho định danh (không ra API)
}

type roundSnap struct {
	round    store.SurveyRound
	communes []store.SurveyCommune
	officers []store.SurveyOfficer
	forms    []store.SurveyForm
}

func (s *SurveyService) loadRound(ctx context.Context, tx pgx.Tx, profile string, id uuid.UUID, withForms bool) (*roundSnap, error) {
	h, err := store.SurveyRoundHistory(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if len(h) == 0 || h[len(h)-1].Profile != profile {
		return nil, port.ErrSurveyRoundNotFound
	}
	sn := &roundSnap{round: h[len(h)-1]}
	cm, err := store.SurveyCommunes(ctx, tx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	for _, c := range cm[id] {
		if c.Active {
			sn.communes = append(sn.communes, c)
		}
	}
	os, err := store.SurveyOfficers(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	for _, o := range os {
		if o.Active {
			sn.officers = append(sn.officers, o)
		}
	}
	if withForms {
		if sn.forms, err = store.SurveyForms(ctx, tx, []uuid.UUID{id}); err != nil {
			return nil, err
		}
	}
	return sn, nil
}

func (sn *roundSnap) state(today time.Time) string {
	return domain.RoundState(sn.round.Status, sn.round.CollectTo, today)
}

func targetSum(cs []store.SurveyCommune, crops []string) int {
	n := 0
	for _, c := range cs {
		for _, cr := range crops {
			n += c.Targets[cr]
		}
	}
	return n
}

// ListRounds trả danh sách đợt (Cấp Tỉnh, Admin), mới tạo trước.
func (s *SurveyService) ListRounds(ctx context.Context, profile string) ([]RoundSummary, error) {
	a, err := s.access(ctx, profile)
	if err != nil {
		return nil, err
	}
	if err := a.needRead(); err != nil {
		return nil, err
	}
	today := s.today()
	var out []RoundSummary
	err = s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		rs, err := store.SurveyRounds(ctx, tx, a.prof.Name)
		if err != nil {
			return err
		}
		rids := make([]uuid.UUID, 0, len(rs))
		for _, r := range rs {
			rids = append(rids, r.RoundID)
		}
		cm, err := store.SurveyCommunes(ctx, tx, rids)
		if err != nil {
			return err
		}
		fs, err := store.SurveyForms(ctx, tx, rids)
		if err != nil {
			return err
		}
		nForms, nPending := map[uuid.UUID]int{}, map[uuid.UUID]int{}
		for _, f := range fs {
			if f.Status == domain.FormDeleted {
				continue
			}
			nForms[f.RoundID]++
			if domain.FormPending(f.Status) {
				nPending[f.RoundID]++
			}
		}
		for _, r := range rs {
			var act []store.SurveyCommune
			for _, c := range cm[r.RoundID] {
				if c.Active {
					act = append(act, c)
				}
			}
			out = append(out, RoundSummary{ID: r.RoundID, Name: r.Name, Season: r.Season, Year: r.Year, From: r.CollectFrom, To: r.CollectTo,
				Crops: r.Crops, Status: domain.RoundState(r.Status, r.CollectTo, today), Communes: len(act), Forms: nForms[r.RoundID],
				Target: targetSum(act, r.Crops), Seq: r.Seq, Pending: nPending[r.RoundID], StoredState: r.Status})
		}
		return nil
	})
	return out, err
}

// GetRound trả chi tiết đợt: thông tin, xã + chỉ tiêu + tiến độ theo nhóm cây, cán bộ điều tra (họ tên, SĐT — Cấp Tỉnh,
// Admin), nhật ký.
func (s *SurveyService) GetRound(ctx context.Context, profile string, id uuid.UUID) (RoundDetail, error) {
	a, err := s.access(ctx, profile)
	if err != nil {
		return RoundDetail{}, err
	}
	if err := a.needRead(); err != nil {
		return RoundDetail{}, err
	}
	var (
		d    RoundDetail
		refs []uuid.UUID
	)
	today := s.today()
	err = s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		sn, err := s.loadRound(ctx, tx, a.prof.Name, id, true)
		if err != nil {
			return err
		}
		h, err := store.SurveyRoundHistory(ctx, tx, id)
		if err != nil {
			return err
		}
		names, err := communeNames(ctx, tx)
		if err != nil {
			return err
		}
		d = s.detail(sn, today, names)
		for _, r := range h {
			d.Log = append(d.Log, RoundLog{Seq: r.Seq, Op: r.Op, Status: r.Status, Reason: r.Reason, At: r.EventTime, Actor: r.Actor})
		}
		d.CreatedAt = h[0].EventTime
		d.CanEdit = a.tinh && d.Status != domain.RoundClosed
		d.CanReopen = a.steward && d.Status == domain.RoundClosed
		for _, o := range sn.officers {
			refs = append(refs, o.SubjectRef)
		}
		return nil
	})
	if err != nil || !a.pii {
		return d, err
	}
	idm, err := s.identities(ctx, refs)
	if err != nil {
		return d, err
	}
	for i := range d.CommuneSet {
		for j := range d.CommuneSet[i].Officers {
			o := &d.CommuneSet[i].Officers[j]
			id := idm[subjectOf(o.ID, d.officerRefs)]
			o.FullName, o.Phone = id.FullName, id.Phone
		}
	}
	return d, nil
}

// subjectOf tra mã chủ thể của cán bộ điều tra.
func subjectOf(officer uuid.UUID, m map[uuid.UUID]uuid.UUID) uuid.UUID { return m[officer] }

func (s *SurveyService) detail(sn *roundSnap, today time.Time, names map[string]string) RoundDetail {
	r := sn.round
	d := RoundDetail{RoundSummary: RoundSummary{ID: r.RoundID, Name: r.Name, Season: r.Season, Year: r.Year, From: r.CollectFrom,
		To: r.CollectTo, Crops: r.Crops, Status: sn.state(today), Communes: len(sn.communes), Target: targetSum(sn.communes, r.Crops),
		Seq: r.Seq, StoredState: r.Status}, Note: r.Note, officerRefs: map[uuid.UUID]uuid.UUID{}}
	byCommune := map[string]map[string]int{}
	byOfficer := map[uuid.UUID]int{}
	for _, f := range sn.forms {
		if f.Status == domain.FormDeleted {
			continue
		}
		d.Forms++
		if domain.FormPending(f.Status) {
			d.Pending++
		}
		if byCommune[f.Commune] == nil {
			byCommune[f.Commune] = map[string]int{}
		}
		byCommune[f.Commune][f.Crop]++
		if f.OfficerID != nil {
			byOfficer[*f.OfficerID]++
		}
	}
	for _, c := range sn.communes {
		rc := RoundCommune{Code: c.Commune, Name: names[c.Commune], Targets: c.Targets, Imported: map[string]int{}}
		for _, cr := range domain.Crops {
			rc.Imported[cr] = byCommune[c.Commune][cr]
		}
		for _, o := range sn.officers {
			if o.Commune == c.Commune {
				rc.Officers = append(rc.Officers, RoundOfficer{ID: o.OfficerID, Commune: o.Commune, Unit: o.UnitName, Forms: byOfficer[o.OfficerID]})
				d.officerRefs[o.OfficerID] = o.SubjectRef
			}
		}
		if len(rc.Officers) == 0 {
			d.Warnings = append(d.Warnings, fmt.Sprintf("Xã %s chưa giao cán bộ điều tra", nameOr(names, c.Commune)))
		}
		d.CommuneSet = append(d.CommuneSet, rc)
	}
	return d
}

func nameOr(names map[string]string, code string) string {
	if n := names[code]; n != "" {
		return n
	}
	return code
}

// communeNames trả tên hiện hành (phiên bản mới nhất) của mọi ĐVHC cấp xã của tenant.
func communeNames(ctx context.Context, tx pgx.Tx) (map[string]string, error) {
	units, err := store.LoadAdminUnits(ctx, tx, "commune")
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, u := range units { // sắp theo mã rồi valid_from → phiên bản sau cùng thắng
		out[u.Code] = u.Name
	}
	return out, nil
}

// officerSubjects ghi chủ thể kho định danh cho cán bộ điều tra mới (NGOÀI giao dịch DB). Trùng SĐT → dùng chủ thể cũ.
func (s *SurveyService) officerSubjects(ctx context.Context, in []OfficerInput) (map[int]uuid.UUID, error) {
	out := map[int]uuid.UUID{}
	var v client.Client
	for i, o := range in {
		if o.ID != uuid.Nil {
			continue
		}
		if v == nil {
			var err error
			if v, err = s.vault(); err != nil {
				return nil, err
			}
		}
		cr, err := v.Create(ctx, surveyPurpose, client.Identity{Kind: client.SubjectPerson, FullName: o.FullName, Phone: o.Phone,
			AreaCode: o.Commune})
		if err != nil && !errors.Is(err, client.ErrDuplicate) {
			return nil, err
		}
		out[i] = cr.Ref
	}
	return out, nil
}

// CreateRound tạo đợt (Nháp; Open → mở ngay sau khi kiểm đủ nhóm cây, xã).
func (s *SurveyService) CreateRound(ctx context.Context, profile string, in RoundInput, idem string) (uuid.UUID, error) {
	a, err := s.access(ctx, profile)
	if err != nil {
		return uuid.Nil, err
	}
	if err := a.needTinh(); err != nil {
		return uuid.Nil, err
	}
	if err := in.validate(); err != nil {
		return uuid.Nil, err
	}
	for _, o := range in.Officers {
		if o.ID != uuid.Nil {
			return uuid.Nil, problem.Validation.WithField("officers", "đợt mới chỉ khai báo cán bộ mới (không có id)")
		}
	}
	subjects, err := s.officerSubjects(ctx, in.Officers)
	if err != nil {
		return uuid.Nil, err
	}
	id := ids.New()
	err = s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		if err := store.LockSurveyRound(ctx, tx, a.c.tenant, uuid.Nil); err != nil { // khóa tạo đợt của tenant (kiểm trùng)
			return err
		}
		if err := s.checkRoundInput(ctx, tx, a, uuid.Nil, in); err != nil {
			return err
		}
		status, op := domain.RoundDraft, "create"
		if in.Open {
			if len(in.Crops) == 0 || len(in.Communes) == 0 {
				return port.ErrSurveyRoundIncomplete
			}
			status = domain.RoundCollecting
		}
		r := store.SurveyRound{RoundID: id, Profile: a.prof.Name, Name: in.Name, Season: in.Season, Year: in.Year, CollectFrom: in.From,
			CollectTo: in.To, Crops: nonNilStrings(in.Crops), Note: in.Note, Status: status, Op: op}
		if _, err := store.InsertSurveyRound(ctx, tx, ids.New(), a.c.tenant, r, s.Clock.Now(), a.c.ref, idemKey(idem, "round")); err != nil {
			return err
		}
		if in.Open { // một phiên bản "open" để nhật ký có B4
			r.Op = "open"
			if _, err := store.InsertSurveyRound(ctx, tx, ids.New(), a.c.tenant, r, s.Clock.Now(), a.c.ref, idemKey(idem, "open")); err != nil {
				return err
			}
		}
		return s.writeParts(ctx, tx, a, id, nil, nil, in, subjects, idem)
	})
	return id, err
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// checkRoundInput kiểm xã thuộc danh mục ĐVHC cấp xã có hiệu lực, và trùng vụ / năm / nhóm cây với đợt khác.
func (s *SurveyService) checkRoundInput(ctx context.Context, tx pgx.Tx, a surveyAccess, self uuid.UUID, in RoundInput) error {
	units, err := store.LoadAdminUnits(ctx, tx, "commune")
	if err != nil {
		return err
	}
	for _, c := range in.Communes {
		if _, ok := communeAt(units, c.Code, s.today()); !ok {
			return port.ErrAdminUnitInvalid.WithField("communes", "mã ĐVHC "+c.Code+" không phải xã có hiệu lực")
		}
	}
	if in.Confirm {
		return nil
	}
	rs, err := store.SurveyRounds(ctx, tx, a.prof.Name)
	if err != nil {
		return err
	}
	for _, r := range rs {
		if r.RoundID == self || r.Season != in.Season || r.Year != in.Year {
			continue
		}
		for _, c := range in.Crops {
			if slices.Contains(r.Crops, c) {
				return port.ErrSurveyRoundDuplicate.WithDetail(fmt.Sprintf("Đợt %q cùng vụ, năm, nhóm cây %s — gửi confirm = true để lưu", r.Name,
					domain.CropNames[c]))
			}
		}
	}
	return nil
}

// writeParts ghi phiên bản mới của xã điều tra / cán bộ điều tra khác hiện hành (bỏ = active false).
func (s *SurveyService) writeParts(ctx context.Context, tx pgx.Tx, a surveyAccess, round uuid.UUID, curC []store.SurveyCommune,
	curO []store.SurveyOfficer, in RoundInput, subjects map[int]uuid.UUID, idem string) error {
	now := s.Clock.Now()
	want := map[string]bool{}
	for _, c := range in.Communes {
		want[c.Code] = true
		tg := map[string]int{}
		for _, cr := range domain.Crops {
			tg[cr] = c.Targets[cr]
		}
		if i := slices.IndexFunc(curC, func(x store.SurveyCommune) bool { return x.Commune == c.Code }); i >= 0 && sameTargets(curC[i].Targets, tg) {
			continue
		}
		if err := store.InsertSurveyCommune(ctx, tx, ids.New(), a.c.tenant, round, store.SurveyCommune{Commune: c.Code, Targets: tg, Active: true},
			now, a.c.ref, idemKey(idem, "commune:"+c.Code)); err != nil {
			return err
		}
	}
	for _, c := range curC {
		if !want[c.Commune] {
			c.Active = false
			if err := store.InsertSurveyCommune(ctx, tx, ids.New(), a.c.tenant, round, c, now, a.c.ref, idemKey(idem, "commune-off:"+c.Commune)); err != nil {
				return err
			}
		}
	}
	keep := map[uuid.UUID]bool{}
	for i, o := range in.Officers {
		oid, subj := o.ID, subjects[i]
		if oid != uuid.Nil {
			j := slices.IndexFunc(curO, func(x store.SurveyOfficer) bool { return x.OfficerID == oid })
			if j < 0 {
				return problem.Validation.WithField("officers", "không có cán bộ điều tra "+oid.String()+" trong đợt")
			}
			keep[oid] = true
			if curO[j].Commune == o.Commune && curO[j].UnitName == o.Unit {
				continue
			}
			subj = curO[j].SubjectRef
		} else {
			oid = ids.New()
		}
		if err := store.InsertSurveyOfficer(ctx, tx, ids.New(), a.c.tenant, round, store.SurveyOfficer{OfficerID: oid, Commune: o.Commune,
			SubjectRef: subj, UnitName: o.Unit, Active: true}, now, a.c.ref, idemKey(idem, "officer:"+oid.String())); err != nil {
			return err
		}
	}
	for _, o := range curO {
		if !keep[o.OfficerID] {
			o.Active = false
			if err := store.InsertSurveyOfficer(ctx, tx, ids.New(), a.c.tenant, round, o, now, a.c.ref, idemKey(idem, "officer-off:"+o.OfficerID.String())); err != nil {
				return err
			}
		}
	}
	return nil
}

func sameTargets(a, b map[string]int) bool {
	for _, c := range domain.Crops {
		if a[c] != b[c] {
			return false
		}
	}
	return true
}

// UpdateRound sửa đợt chưa chốt (thông tin, xã + chỉ tiêu, cán bộ điều tra). Trạng thái giữ nguyên; đợt đã mở không bỏ hết
// nhóm cây / xã.
func (s *SurveyService) UpdateRound(ctx context.Context, profile string, id uuid.UUID, in RoundInput, idem string) error {
	a, err := s.access(ctx, profile)
	if err != nil {
		return err
	}
	if err := a.needTinh(); err != nil {
		return err
	}
	if err := in.validate(); err != nil {
		return err
	}
	subjects, err := s.officerSubjects(ctx, in.Officers)
	if err != nil {
		return err
	}
	return s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		if err := store.LockSurveyRound(ctx, tx, a.c.tenant, id); err != nil {
			return err
		}
		sn, err := s.loadRound(ctx, tx, a.prof.Name, id, false)
		if err != nil {
			return err
		}
		if in.Seq != 0 && in.Seq != sn.round.Seq {
			return port.ErrSurveyStale
		}
		if sn.round.Status == domain.RoundClosed {
			return port.ErrSurveyRoundState.WithDetail("Đợt đã chốt — Quản trị nghiệp vụ mở lại trước khi sửa")
		}
		if sn.round.Status != domain.RoundDraft && (len(in.Crops) == 0 || len(in.Communes) == 0) {
			return port.ErrSurveyRoundIncomplete
		}
		if err := s.checkRoundInput(ctx, tx, a, id, in); err != nil {
			return err
		}
		r := sn.round
		r.Name, r.Season, r.Year, r.CollectFrom, r.CollectTo, r.Crops, r.Note = in.Name, in.Season, in.Year, in.From, in.To, nonNilStrings(in.Crops), in.Note
		r.Op, r.Reason = "edit", ""
		if _, err := store.InsertSurveyRound(ctx, tx, ids.New(), a.c.tenant, r, s.Clock.Now(), a.c.ref, idemKey(idem, "round")); err != nil {
			return err
		}
		return s.writeParts(ctx, tx, a, id, sn.communes, sn.officers, in, subjects, idem)
	})
}

// RoundAction là thao tác đổi trạng thái đợt.
type RoundAction struct {
	Op     string // open | close_early | extend | close | reopen
	Reason string
	To     time.Time // extend: thu thập đến ngày
	Seq    int64
}

// ActRound đổi trạng thái đợt theo bảng 3.4: Mở đợt (Nháp → Đang thu thập; đủ nhóm cây, xã), Đóng sớm (Đang thu thập →
// Đang rà soát), Gia hạn (lý do; Đang rà soát vì hết hạn → Đang thu thập), Chốt đợt (mọi phiếu Đã duyệt / Loại — FN-TI-02.12
// B5), Mở lại (Đã chốt → Đang rà soát; Quản trị nghiệp vụ, lý do bắt buộc).
func (s *SurveyService) ActRound(ctx context.Context, profile string, id uuid.UUID, act RoundAction, idem string) error {
	a, err := s.access(ctx, profile)
	if err != nil {
		return err
	}
	if act.Op == "reopen" {
		if !a.steward {
			return problem.Forbidden.WithDetail("chỉ Quản trị nghiệp vụ báo giá mở lại đợt đã chốt")
		}
	} else if err := a.needTinh(); err != nil {
		return err
	}
	act.Reason = strings.TrimSpace(act.Reason)
	if (act.Op == "extend" || act.Op == "reopen") && (act.Reason == "" || utf8.RuneCountInString(act.Reason) > 500) {
		return problem.Validation.WithField("reason", "lý do bắt buộc, tối đa 500 ký tự")
	}
	today := s.today()
	return s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		if err := store.LockSurveyRound(ctx, tx, a.c.tenant, id); err != nil {
			return err
		}
		sn, err := s.loadRound(ctx, tx, a.prof.Name, id, act.Op == "close")
		if err != nil {
			return err
		}
		if act.Seq != 0 && act.Seq != sn.round.Seq {
			return port.ErrSurveyStale
		}
		r, st := sn.round, sn.state(today)
		r.Op, r.Reason = act.Op, act.Reason
		switch act.Op {
		case "open":
			if st != domain.RoundDraft {
				return port.ErrSurveyRoundState
			}
			if len(r.Crops) == 0 || len(sn.communes) == 0 {
				return port.ErrSurveyRoundIncomplete
			}
			r.Status = domain.RoundCollecting
		case "close_early":
			if st != domain.RoundCollecting {
				return port.ErrSurveyRoundState
			}
			r.Status = domain.RoundReviewing
		case "extend":
			if st != domain.RoundCollecting && st != domain.RoundReviewing {
				return port.ErrSurveyRoundState
			}
			if act.To.IsZero() || !act.To.After(r.CollectTo) || act.To.Before(today) {
				return problem.Validation.WithField("collect_to", "ngày gia hạn phải sau ngày kết thúc hiện tại và không trước hôm nay")
			}
			r.CollectTo, r.Status = act.To, domain.RoundCollecting // gia hạn: thu thập tiếp tới ngày mới
		case "close":
			if st != domain.RoundCollecting && st != domain.RoundReviewing {
				return port.ErrSurveyRoundState
			}
			n := 0
			for _, f := range sn.forms {
				if domain.FormPending(f.Status) {
					n++
				}
			}
			if n > 0 {
				return port.ErrSurveyRoundPending.WithDetail(fmt.Sprintf("Còn %d phiếu chờ rà soát hoặc chờ xác minh", n))
			}
			r.Status = domain.RoundClosed
		case "reopen":
			if st != domain.RoundClosed {
				return port.ErrSurveyRoundState
			}
			r.Status = domain.RoundReviewing
		default:
			return problem.Validation.WithField("op", "open | close_early | extend | close | reopen")
		}
		_, err = store.InsertSurveyRound(ctx, tx, ids.New(), a.c.tenant, r, s.Clock.Now(), a.c.ref, idemKey(idem, "round:"+act.Op))
		return err
	})
}

// CloneRound nhân bản đợt làm điểm bắt đầu (luồng phụ FN-TI-02.10): đợt mới ở Nháp, cùng nhóm cây, xã + chỉ tiêu, cán bộ
// điều tra (cùng chủ thể kho định danh); tên, thời gian thu thập do người dùng sửa sau.
func (s *SurveyService) CloneRound(ctx context.Context, profile string, id uuid.UUID, idem string) (uuid.UUID, error) {
	a, err := s.access(ctx, profile)
	if err != nil {
		return uuid.Nil, err
	}
	if err := a.needTinh(); err != nil {
		return uuid.Nil, err
	}
	nid := ids.New()
	err = s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		sn, err := s.loadRound(ctx, tx, a.prof.Name, id, false)
		if err != nil {
			return err
		}
		r := sn.round
		r.RoundID, r.Status, r.Op, r.Reason = nid, domain.RoundDraft, "create", ""
		name := []rune(r.Name + " (bản sao)")
		if len(name) > 120 {
			name = name[:120]
		}
		r.Name = string(name)
		if _, err := store.InsertSurveyRound(ctx, tx, ids.New(), a.c.tenant, r, s.Clock.Now(), a.c.ref, idemKey(idem, "round")); err != nil {
			return err
		}
		now := s.Clock.Now()
		for _, c := range sn.communes {
			if err := store.InsertSurveyCommune(ctx, tx, ids.New(), a.c.tenant, nid, c, now, a.c.ref, idemKey(idem, "commune:"+c.Commune)); err != nil {
				return err
			}
		}
		for _, o := range sn.officers {
			o.OfficerID = ids.New()
			if err := store.InsertSurveyOfficer(ctx, tx, ids.New(), a.c.tenant, nid, o, now, a.c.ref, idemKey(idem, "officer:"+o.OfficerID.String())); err != nil {
				return err
			}
		}
		return nil
	})
	return nid, err
}
