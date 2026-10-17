package app

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// OutbreakView là một phiên bản ổ dịch kèm trạng thái, tên danh mục, người thao tác.
type OutbreakView struct {
	store.OutbreakRow
	Status      string
	DiseaseName string
	SpeciesName string
	Actor       Actor
}

func (s *Service) obViews(ctx context.Context, rs []store.OutbreakRow) []OutbreakView {
	refs := make([]uuid.UUID, 0, len(rs))
	for _, r := range rs {
		refs = append(refs, r.ActorRef)
	}
	act, nm := s.actors(ctx, refs), s.names(ctx)
	out := make([]OutbreakView, 0, len(rs))
	for _, r := range rs {
		v := OutbreakView{OutbreakRow: r, Status: domain.StatusOf(r.Event), Actor: act(r.ActorRef),
			DiseaseName: nm[knport.CatalogCNTSDisease+":"+r.DiseaseCode], SpeciesName: nm[knport.CatalogCNTSSpecies+":"+r.SpeciesCode]}
		if v.DiseaseName == "" {
			v.DiseaseName = r.DiseaseCode
		}
		if v.SpeciesName == "" {
			v.SpeciesName = r.SpeciesCode
		}
		out = append(out, v)
	}
	return out
}

// OutbreakQuery lọc danh sách ổ dịch (Cấp Tỉnh: theo dõi; Cấp Xã: ổ dịch của xã mình).
type OutbreakQuery struct {
	Status      string // "" = mọi trạng thái
	Commune     string // "" = mọi xã trong phạm vi
	DiseaseCode string
	From, To    *time.Time // ngày tiếp nhận DB-02
}

// Count là một dòng thống kê.
type Count struct {
	Key   string
	Label string
	N     int
}

// OutbreakList là danh sách ổ dịch (bản hiện hành) + thống kê theo trạng thái, bệnh (trước khi lọc trạng thái).
type OutbreakList struct {
	Items     []OutbreakView
	ByStatus  []Count
	ByDisease []Count
}

func (s *Service) currentInScope(ctx context.Context, c caller, q OutbreakQuery) ([]store.OutbreakRow, error) {
	if q.Status != "" && !slices.Contains(domain.Statuses, q.Status) {
		return nil, problem.Validation.WithField("status", "reported | monitoring | closed")
	}
	f := store.OutbreakFilter{DiseaseCode: q.DiseaseCode, From: q.From, To: q.To}
	if q.Commune != "" {
		if !c.sees(q.Commune) {
			return nil, ErrOutOfScope.WithField("commune", "xã ngoài phạm vi")
		}
		f.Communes = []string{q.Commune}
	}
	var rs []store.OutbreakRow
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rs, e = store.OutbreaksCurrent(ctx, tx, f)
		return e
	}); err != nil {
		return nil, err
	}
	out := rs[:0]
	for _, r := range rs {
		if c.sees(r.CommuneCode) {
			out = append(out, r)
		}
	}
	return out, nil
}

// ListOutbreaks trả bản hiện hành các ổ dịch trong phạm vi.
func (s *Service) ListOutbreaks(ctx context.Context, q OutbreakQuery) (OutbreakList, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return OutbreakList{}, err
	}
	rs, err := s.currentInScope(ctx, c, q)
	if err != nil {
		return OutbreakList{}, err
	}
	vs := s.obViews(ctx, rs)
	res := OutbreakList{Items: []OutbreakView{}}
	byStatus := map[string]int{}
	byDisease := map[string]*Count{}
	var diseases []string
	for _, v := range vs {
		byStatus[v.Status]++
		if byDisease[v.DiseaseCode] == nil {
			byDisease[v.DiseaseCode] = &Count{Key: v.DiseaseCode, Label: v.DiseaseName}
			diseases = append(diseases, v.DiseaseCode)
		}
		byDisease[v.DiseaseCode].N++
		if q.Status == "" || v.Status == q.Status {
			res.Items = append(res.Items, v)
		}
	}
	labels := map[string]string{domain.StatusReported: "Đã gửi", domain.StatusMonitoring: "Đang theo dõi", domain.StatusClosed: "Đã kết thúc"}
	for _, st := range domain.Statuses {
		res.ByStatus = append(res.ByStatus, Count{Key: st, Label: labels[st], N: byStatus[st]})
	}
	slices.Sort(diseases)
	res.ByDisease = []Count{}
	for _, d := range diseases {
		res.ByDisease = append(res.ByDisease, *byDisease[d])
	}
	return res, nil
}

// OutbreakPoints trả ổ dịch (bản hiện hành) có tọa độ cho lớp bản đồ — mặc định bỏ ổ dịch đã kết thúc.
func (s *Service) OutbreakPoints(ctx context.Context, q OutbreakQuery, includeClosed bool) ([]OutbreakView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	rs, err := s.currentInScope(ctx, c, q)
	if err != nil {
		return nil, err
	}
	out := []OutbreakView{}
	for _, v := range s.obViews(ctx, rs) {
		if v.Location == nil || (!includeClosed && v.Status == domain.StatusClosed) || (q.Status != "" && v.Status != q.Status) {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) history(ctx context.Context, c caller, ref uuid.UUID) ([]store.OutbreakRow, error) {
	var rs []store.OutbreakRow
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rs, e = store.OutbreakHistory(ctx, tx, ref)
		return e
	})
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 || !c.sees(rs[0].CommuneCode) {
		return nil, ErrOutbreakNotFound
	}
	return rs, nil
}

// GetOutbreak trả mọi phiên bản (diễn biến) của một ổ dịch, mới nhất trước — phần tử đầu là bản hiện hành.
func (s *Service) GetOutbreak(ctx context.Context, ref uuid.UUID) ([]OutbreakView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	rs, err := s.history(ctx, c, ref)
	if err != nil {
		return nil, err
	}
	return s.obViews(ctx, rs), nil
}

// OutbreakInput là nội dung lập/cập nhật một ổ dịch.
type OutbreakInput struct {
	Outbreak    domain.Outbreak
	BaseVersion *int64 // bắt buộc khi cập nhật: phiên bản người nhập đã thấy
	IdemKey     string
}

func (s *Service) replayOutbreak(ctx context.Context, c caller, key string) (*OutbreakView, error) {
	var r *store.OutbreakRow
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		r, e = store.OutbreakByIdem(ctx, tx, key)
		return e
	})
	if err != nil || r == nil {
		return nil, err
	}
	v := s.obViews(ctx, []store.OutbreakRow{*r})[0]
	return &v, nil
}

// withReplay chạy ghi; khóa idempotency đã dùng (trước hoặc đồng thời) → trả bản đã ghi (replayed = true).
func (s *Service) withReplay(ctx context.Context, c caller, key string, write func() (store.OutbreakRow, error)) (OutbreakView, bool, error) {
	if key == "" {
		return OutbreakView{}, false, problem.IdempotencyRequired
	}
	if v, err := s.replayOutbreak(ctx, c, key); err != nil || v != nil {
		if err != nil {
			return OutbreakView{}, false, err
		}
		return *v, true, nil
	}
	row, err := write()
	if dup(err) {
		if v, rerr := s.replayOutbreak(ctx, c, key); rerr == nil && v != nil {
			return *v, true, nil
		}
	}
	if err != nil {
		return OutbreakView{}, false, err
	}
	return s.obViews(ctx, []store.OutbreakRow{row})[0], false, nil
}

// prepare kiểm nội dung, xã, danh mục của một phiên bản ổ dịch mới.
func (s *Service) prepare(ctx context.Context, c caller, o *domain.Outbreak) (mdport.CatalogItem, error) {
	if es := domain.NormalizeOutbreak(o, s.now(), s.today()); len(es) > 0 {
		return mdport.CatalogItem{}, fieldsProblem(ErrOutbreakInvalid, es)
	}
	return s.checkCatalogs(ctx, ErrOutbreakInvalid, o.DiseaseCode, o.SpeciesCode, o.AsOf)
}

// ReportOutbreak lập báo cáo dịch bệnh mới và gửi Cấp Tỉnh (phiên bản 1, FN-11 B1–B4) — phát
// pf.extension.outbreak.reported (cảnh báo Khẩn, BR-01).
func (s *Service) ReportOutbreak(ctx context.Context, in OutbreakInput) (OutbreakView, bool, error) {
	c, err := s.reporter(ctx)
	if err != nil {
		return OutbreakView{}, false, err
	}
	o := in.Outbreak
	o.SuspectRef = nil
	if o.AsOf.IsZero() && !o.ReceivedAt.IsZero() {
		o.AsOf = day(o.ReceivedAt) // phiên bản 1: ngày diễn biến = ngày tiếp nhận
	}
	return s.withReplay(ctx, c, in.IdemKey, func() (store.OutbreakRow, error) {
		disease, err := s.prepare(ctx, c, &o)
		if err != nil {
			return store.OutbreakRow{}, err
		}
		unit, err := s.checkCommune(ctx, ErrOutbreakInvalid, c, o.CommuneCode)
		if err != nil {
			return store.OutbreakRow{}, err
		}
		row := store.OutbreakRow{Ref: newID(), Outbreak: o, Meta: c.meta(domain.EventReported, s.now(), in.IdemKey)}
		err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			if err := store.InsertOutbreak(ctx, tx, c.p.TenantID, &row); err != nil {
				return err
			}
			return emitReported(ctx, tx, c, row, unit, disease)
		})
		return row, err
	})
}

// latest đọc bản hiện hành (đã khóa) và kiểm khóa lạc quan.
func latest(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID, base *int64) (store.OutbreakRow, error) {
	if err := store.Lock(ctx, tx, "outbreak:"+ref.String()); err != nil {
		return store.OutbreakRow{}, err
	}
	hist, err := store.OutbreakHistory(ctx, tx, ref)
	if err != nil {
		return store.OutbreakRow{}, err
	}
	if len(hist) == 0 || !c.sees(hist[0].CommuneCode) {
		return store.OutbreakRow{}, ErrOutbreakNotFound
	}
	cur := hist[0]
	if base != nil && *base != cur.Seq {
		return cur, ErrOutbreakConflict.WithDetail("Đã có người cập nhật lúc " +
			cur.RecordedAt.In(clock.Zone()).Format("15:04 02/01/2006") + " (phiên bản " + strconv.FormatInt(cur.Seq, 10) + ")")
	}
	return cur, nil
}

// UpdateOutbreak ghi diễn biến mới (phiên bản đủ trường, FN-11 B5, BR-02). Xã, tin báo gốc giữ nguyên; ngày diễn biến
// không lùi so với bản hiện hành; ổ dịch đã kết thúc không cập nhật (Cấp Tỉnh mở lại trước).
func (s *Service) UpdateOutbreak(ctx context.Context, ref uuid.UUID, in OutbreakInput) (OutbreakView, bool, error) {
	c, err := s.reporter(ctx)
	if err != nil {
		return OutbreakView{}, false, err
	}
	if in.BaseVersion == nil {
		return OutbreakView{}, false, errBaseVersionMissing
	}
	o := in.Outbreak
	if o.AsOf.IsZero() {
		o.AsOf = s.today()
	}
	return s.withReplay(ctx, c, in.IdemKey, func() (store.OutbreakRow, error) {
		if _, err := s.prepare(ctx, c, &o); err != nil {
			return store.OutbreakRow{}, err
		}
		var row store.OutbreakRow
		err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			cur, err := latest(ctx, tx, c, ref, in.BaseVersion)
			if err != nil {
				return err
			}
			switch {
			case domain.StatusOf(cur.Event) == domain.StatusClosed:
				return ErrOutbreakClosed
			case o.CommuneCode != cur.CommuneCode:
				return ErrOutbreakInvalid.WithField("commune_code", "không đổi xã của ổ dịch")
			case o.AsOf.Before(cur.AsOf):
				return ErrOutbreakInvalid.WithField("as_of", "ngày diễn biến trước ngày của phiên bản hiện hành ("+cur.AsOf.Format("02/01/2006")+")")
			}
			o.SuspectRef = cur.SuspectRef
			row = store.OutbreakRow{Ref: ref, Outbreak: o, Meta: c.meta(domain.EventUpdated, s.now(), in.IdemKey)}
			return store.InsertOutbreak(ctx, tx, c.p.TenantID, &row)
		})
		return row, err
	})
}

// StatusInput là yêu cầu kết thúc / mở lại ổ dịch.
type StatusInput struct {
	BaseVersion *int64
	Reason      string
	IdemKey     string
}

func (s *Service) setStatus(ctx context.Context, ref uuid.UUID, in StatusInput, event string) (OutbreakView, bool, error) {
	c, err := s.closer(ctx)
	if err != nil {
		return OutbreakView{}, false, err
	}
	if in.BaseVersion == nil {
		return OutbreakView{}, false, errBaseVersionMissing
	}
	reason, es := domain.Reason(in.Reason)
	if len(es) > 0 {
		return OutbreakView{}, false, fieldsProblem(ErrOutbreakInvalid, es)
	}
	return s.withReplay(ctx, c, in.IdemKey, func() (store.OutbreakRow, error) {
		var row store.OutbreakRow
		err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			cur, err := latest(ctx, tx, c, ref, in.BaseVersion)
			if err != nil {
				return err
			}
			closed := domain.StatusOf(cur.Event) == domain.StatusClosed
			if event == domain.EventClosed && closed {
				return ErrOutbreakClosed
			}
			if event == domain.EventReopened && !closed {
				return ErrOutbreakNotClosed
			}
			// Bản kết thúc/mở lại chụp lại nội dung hiện hành; ngày diễn biến = hôm nay (không lùi).
			o := cur.Outbreak
			o.ProgressNote, o.CapturedAt = "", nil
			if t := s.today(); t.After(o.AsOf) {
				o.AsOf = t
			}
			row = store.OutbreakRow{Ref: ref, Outbreak: o, Meta: c.meta(event, s.now(), in.IdemKey)}
			row.StatusReason = &reason
			return store.InsertOutbreak(ctx, tx, c.p.TenantID, &row)
		})
		return row, err
	})
}

// CloseOutbreak: Cấp Tỉnh xác nhận kết thúc ổ dịch.
func (s *Service) CloseOutbreak(ctx context.Context, ref uuid.UUID, in StatusInput) (OutbreakView, bool, error) {
	return s.setStatus(ctx, ref, in, domain.EventClosed)
}

// ReopenOutbreak: Cấp Tỉnh mở lại ổ dịch đã kết thúc (kết thúc nhầm, dịch tái phát) — Cấp Xã cập nhật tiếp.
func (s *Service) ReopenOutbreak(ctx context.Context, ref uuid.UUID, in StatusInput) (OutbreakView, bool, error) {
	return s.setStatus(ctx, ref, in, domain.EventReopened)
}
