package app

// Consumer port.CNTSAlertConsumer: quy tắc liên biểu + cảnh báo tự động sau commit (FN-13 AC-01 "cảnh báo tạo trong
// 1 phút sau khi dữ liệu đồng bộ"). Chạy trong giao dịch inbox của vai extension_app; số liệu bài nộp đọc qua
// formsport.SubmissionFeed (giao dịch riêng của pf/forms).
//
// P0-POOL-SHARED-2 (#143, bàn giao từ #139): mọi lời gọi port module khác (bài nộp cap/forms, tham số masterdata, đơn
// vị hành chính, danh bạ cán bộ) đi qua txdeps.Get — inbox.Process gắn bộ nhớ txdeps nên chúng chạy NGOÀI giao dịch
// inbox (hủy, nạp, chạy lại thân); handler chỉ ghi cảnh báo/sự kiện trong giao dịch.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// Events là sự kiện consumer port.CNTSAlertConsumer đăng ký.
var Events = []string{formsport.EventSubmissionRecorded, port.EventOutbreakReported}

// HandleEvent là inbox.Handler của consumer port.CNTSAlertConsumer.
func (s *Service) HandleEvent(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	// inbox.Process đã gắn tenant của phong bì vào ctx (cổng module khác đọc tenant từ ngữ cảnh).
	switch env.EventType {
	case formsport.EventSubmissionRecorded:
		var p formsport.SubmissionRecordedV1
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return fmt.Errorf("extension: payload %s hỏng: %w", env.EventType, err)
		}
		if p.Profile != ProfileCNTS {
			return nil
		}
		if s.Feed == nil {
			return ErrDependency.WithDetail("chưa nối cap/forms (SubmissionFeed)")
		}
		id, err := uuid.Parse(p.SubmissionID)
		if err != nil {
			return nil
		}
		sub, err := txdeps.Get(ctx, "feed.recorded:"+env.TenantID.String()+":"+id.String(),
			func(ctx context.Context) (*formsport.RecordedSubmission, error) {
				return s.Feed.Recorded(ctx, env.TenantID, id)
			})
		if err != nil || sub == nil {
			return err
		}
		return s.onSubmission(ctx, tx, env.TenantID, *sub)
	case port.EventOutbreakReported:
		var p port.OutbreakReportedV1
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return fmt.Errorf("extension: payload %s hỏng: %w", env.EventType, err)
		}
		return s.onOutbreak(ctx, tx, env.TenantID, p, env.OccurredAt)
	}
	return nil
}

// threshold đọc tham số có phiên bản tại at; chưa có → def.
func (s *Service) threshold(ctx context.Context, key string, at time.Time, def int64) decimal.Decimal {
	if s.Params != nil {
		if v, err := txdeps.Get(ctx, "param:"+key+"@"+at.UTC().Format(time.RFC3339Nano), func(ctx context.Context) (decimal.Decimal, error) {
			pv, err := s.Params.At(ctx, key, at)
			return pv.Value, err
		}); err == nil {
			return v
		}
	}
	return decimal.NewFromInt(def)
}

// recipients giải người nhận theo danh sách vai Mục III.5.
type resolver struct {
	s        *Service
	tid      uuid.UUID
	province string
	officers []Officer
	loaded   bool
}

func (r *resolver) load(ctx context.Context) {
	if r.loaded || r.s.Dir == nil || r.province == "" {
		r.loaded = true
		return
	}
	r.loaded = true
	r.officers, _ = txdeps.Get(ctx, "officers:"+r.tid.String()+":"+r.province, func(ctx context.Context) ([]Officer, error) {
		return r.s.Dir.Officers(ctx, r.tid, r.province)
	})
}

func (r *resolver) resolve(ctx context.Context, roles []string, commune string, submitter uuid.UUID) []uuid.UUID {
	var out []uuid.UUID
	for _, role := range roles {
		switch role {
		case domain.RecipSubmitter:
			out = append(out, submitter)
		case domain.RecipFormOwner:
			if r.s.Dir != nil {
				if refs, err := txdeps.Get(ctx, "form_owners:"+r.tid.String(), func(ctx context.Context) ([]uuid.UUID, error) {
					return r.s.Dir.FormOwners(ctx, r.tid)
				}); err == nil {
					out = append(out, refs...)
				}
			}
		case domain.RecipCommune, domain.RecipManager, domain.RecipProvince:
			r.load(ctx)
			for _, o := range r.officers {
				switch {
				case role == domain.RecipCommune && o.Level == "commune" && containsStr(o.Communes, commune):
					out = append(out, o.ActorRef)
				case role == domain.RecipManager && o.Level == "province" && o.Manager:
					out = append(out, o.ActorRef)
				case role == domain.RecipProvince && o.Level == "province":
					out = append(out, o.ActorRef)
				}
			}
		}
	}
	return dedupRefs(out)
}

// latest đọc bài nộp cuối qua cap/forms bằng txdeps (ngoài giao dịch inbox — P0-POOL-SHARED-2).
func (s *Service) latest(ctx context.Context, tid uuid.UUID, q formsport.SubmissionQuery) ([]formsport.RecordedSubmission, error) {
	k, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	return txdeps.Get(ctx, "feed.latest:"+tid.String()+":"+string(k), func(ctx context.Context) ([]formsport.RecordedSubmission, error) {
		return s.Feed.Latest(ctx, tid, q)
	})
}

// periodAdhoc là loại kỳ native của đợt điều tra CNTS (W11.50 — extension/cntscampaign).
const periodAdhoc = "adhoc"

// regular bỏ bài nộp của kỳ `adhoc` (đợt điều tra) khỏi chuỗi so sánh của báo cáo định kỳ. Mọi truy vấn lịch sử đã
// loại `adhoc` TRƯỚC LIMIT qua SubmissionQuery.ExcludePeriodKinds (review #27 P2 — nhiều đợt mới hơn không được đẩy kỳ
// định kỳ ra khỏi cửa sổ); regular chỉ còn là chốt chặn phía KN.
func regular(rs []formsport.RecordedSubmission) []formsport.RecordedSubmission {
	out := rs[:0:0]
	for _, r := range rs {
		if r.PeriodKind != periodAdhoc {
			out = append(out, r)
		}
	}
	return out
}

func containsStr(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func dateOf(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("02/01")
}

func subjectLabel(sub formsport.RecordedSubmission) string {
	switch sub.UnitKind {
	case "facility":
		return "cơ sở giết mổ (xã " + sub.Parent + ")"
	case "household":
		return "hộ (xã " + sub.Parent + ")"
	}
	return "xã " + sub.Unit
}

func scopeOf(sub formsport.RecordedSubmission) string {
	if sub.PeriodKey != "" {
		return sub.PeriodKey
	}
	return sub.PeriodID.String()
}

// base dựng yêu cầu cảnh báo từ bài nộp.
func (s *Service) base(kind string, sub formsport.RecordedSubmission, province string) Raise {
	k, _ := domain.KindOf(kind)
	pid, sid := sub.PeriodID, sub.ID
	subjKind := sub.UnitKind
	if subjKind == "" {
		subjKind = "commune"
	}
	return Raise{Kind: kind, Subject: sub.Unit, SubjectKind: subjKind, Scope: scopeOf(sub), CommuneCode: sub.Parent, ProvinceCode: province,
		TemplateCode: sub.TemplateCode, PeriodKey: sub.PeriodKey, PeriodRef: &pid, SourceRef: &sid,
		Title: k.Label + " · " + domain.FormLabel(sub.TemplateCode) + " · " + sub.PeriodLabel + " · " + subjectLabel(sub),
		Extra: map[string]any{"period_label": sub.PeriodLabel, "period_from": dateOf(sub.From), "period_to": dateOf(sub.To),
			"submission_version": sub.Version}, At: sub.EventTime}
}

// verdict sinh cảnh báo khi có vi phạm, tự đóng khi hết vi phạm.
func (s *Service) verdict(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r Raise, fs []domain.Finding, hit bool, rs *resolver, submitter uuid.UUID) error {
	if !hit {
		return s.autoClose(ctx, tx, tid, r.Kind, r.Subject, r.Scope, r.SourceRef, r.At)
	}
	k, _ := domain.KindOf(r.Kind)
	r.Findings = fs
	r.Recipients = rs.resolve(ctx, k.Recipients, r.CommuneCode, submitter)
	_, _, err := s.raise(ctx, tx, tid, r)
	return err
}

func (s *Service) onSubmission(ctx context.Context, tx pgx.Tx, tid uuid.UUID, sub formsport.RecordedSubmission) error {
	if sub.PeriodKind == periodAdhoc {
		return nil // W11.50: bài của đợt điều tra không so với báo cáo định kỳ (FN-21 BR-02)
	}
	at := sub.EventTime
	province := s.provinceOf(ctx, sub.Parent, at)
	rs := &resolver{s: s, tid: tid, province: province}
	switch sub.TemplateCode {
	case domain.FormBM04:
		prev, err := s.previous(ctx, tid, sub)
		if err != nil {
			return err
		}
		fs := domain.ChangeFindings(sub.Values, prev, domain.HerdFields, s.threshold(ctx, port.ParamCNTSHerdChangePct, at, 15))
		if err := s.verdict(ctx, tx, tid, s.base(domain.KindHerdChange, sub, province), fs, len(fs) > 0, rs, sub.Submitter); err != nil {
			return err
		}
		return s.ksgmCheck(ctx, tx, tid, sub, rs)
	case domain.FormBM17:
		prev, err := s.previous(ctx, tid, sub)
		if err != nil {
			return err
		}
		fs := domain.ChangeFindings(sub.Values, prev, domain.FishingFields, s.threshold(ctx, port.ParamCNTSFishingChangePct, at, 50))
		return s.verdict(ctx, tx, tid, s.base(domain.KindFishingChange, sub, province), fs, len(fs) > 0, rs, sub.Submitter)
	case domain.FormBM01:
		zero := domain.SlaughterZero(sub.Values, sub.NilReport)
		r := s.base(domain.KindSlaughterZero, sub, province)
		r.Extra["reason"] = "Mọi loài = 0 khi cơ sở hoạt động — kiểm tra lại hoặc báo \"Cơ sở không hoạt động\""
		if err := s.verdict(ctx, tx, tid, r, nil, zero, rs, sub.Submitter); err != nil {
			return err
		}
		if err := s.spikeCheck(ctx, tx, tid, sub, rs); err != nil {
			return err
		}
		return s.bm01ChangedWeek(ctx, tx, tid, sub, rs)
	}
	return nil
}

// previous trả số liệu kỳ gần nhất đã nộp của cùng đơn vị, tìm lùi tối đa cnts.lookback_periods kỳ (Mục III.5). nil =
// không có kỳ gần nhất.
func (s *Service) previous(ctx context.Context, tid uuid.UUID, sub formsport.RecordedSubmission) (map[string]string, error) {
	if sub.From == nil || sub.To == nil {
		return nil, nil
	}
	n := s.threshold(ctx, port.ParamCNTSLookbackPeriods, sub.EventTime, 4).IntPart()
	if n < 1 {
		n = 1
	}
	span := int(sub.To.Sub(*sub.From).Hours()/24) + 1
	from := sub.From.AddDate(0, 0, -span*int(n))
	to := sub.From.AddDate(0, 0, -1)
	rs, err := s.latest(ctx, tid, formsport.SubmissionQuery{Profile: ProfileCNTS, TemplateCode: sub.TemplateCode, Units: []string{sub.Unit},
		From: &from, To: &to, Limit: int(n) + 1, ExcludePeriodKinds: []string{periodAdhoc}})
	rs = regular(rs)
	if err != nil || len(rs) == 0 {
		return nil, err
	}
	return rs[0].Values, nil
}

// spikeCheck: dòng 2 — KSGM trong ngày > hệ số × trung bình các ngày có báo cáo trong 4 tuần trước.
func (s *Service) spikeCheck(ctx context.Context, tx pgx.Tx, tid uuid.UUID, sub formsport.RecordedSubmission, rs *resolver) error {
	if sub.From == nil {
		return nil
	}
	from, to := sub.From.AddDate(0, 0, -28), sub.From.AddDate(0, 0, -1)
	hs, err := s.latest(ctx, tid, formsport.SubmissionQuery{Profile: ProfileCNTS, TemplateCode: domain.FormBM01, Units: []string{sub.Unit},
		From: &from, To: &to, Limit: 60, ExcludePeriodKinds: []string{periodAdhoc}})
	if err != nil {
		return err
	}
	hs = regular(hs)
	var hist []map[string]string
	for _, h := range hs {
		if !h.NilReport {
			hist = append(hist, h.Values)
		}
	}
	at := sub.EventTime
	fs := domain.SlaughterSpike(sub.Values, hist, s.threshold(ctx, port.ParamCNTSSlaughterSpikeFactor, at, 3),
		int(s.threshold(ctx, port.ParamCNTSSlaughterSpikeMinDays, at, 7).IntPart()))
	return s.verdict(ctx, tx, tid, s.base(domain.KindSlaughterSpike, sub, rs.province), fs, len(fs) > 0, rs, sub.Submitter)
}

// ksgmCheck: quy tắc liên biểu số 1 / dòng 4 — CN-06 của BM-04 so tổng BM-01 các cơ sở trong xã trong tuần.
func (s *Service) ksgmCheck(ctx context.Context, tx pgx.Tx, tid uuid.UUID, bm04 formsport.RecordedSubmission, rs *resolver) error {
	if bm04.From == nil || bm04.To == nil {
		return nil
	}
	ds, err := s.latest(ctx, tid, formsport.SubmissionQuery{Profile: ProfileCNTS, TemplateCode: domain.FormBM01, Parents: []string{bm04.Unit},
		From: bm04.From, To: bm04.To, Limit: 1000, ExcludePeriodKinds: []string{periodAdhoc}})
	if err != nil {
		return err
	}
	ds = regular(ds)
	vals := make([]map[string]string, 0, len(ds))
	for _, d := range ds {
		vals = append(vals, d.Values)
	}
	fs := domain.KSGMMismatch(bm04.Values, vals)
	r := s.base(domain.KindKSGMMismatch, bm04, rs.province)
	r.Extra["bm01_reports"] = len(ds)
	return s.verdict(ctx, tx, tid, r, fs, len(fs) > 0, rs, bm04.Submitter)
}

// bm01ChangedWeek: BM-01 đổi sau khi xã gửi BM-04 → so lại CN-06 của tuần chứa ngày đó (dòng 4).
func (s *Service) bm01ChangedWeek(ctx context.Context, tx pgx.Tx, tid uuid.UUID, bm01 formsport.RecordedSubmission, rs *resolver) error {
	if bm01.From == nil || bm01.Version < 2 {
		return nil // chỉ khi BM-01 ĐỔI (phiên bản sau) — bài đầu của ngày được so khi xã gửi BM-04
	}
	day := *bm01.From
	from := day.AddDate(0, 0, -6)
	ws, err := s.latest(ctx, tid, formsport.SubmissionQuery{Profile: ProfileCNTS, TemplateCode: domain.FormBM04, Units: []string{bm01.Parent},
		From: &from, To: &day, Limit: 7, ExcludePeriodKinds: []string{periodAdhoc}})
	if err != nil {
		return err
	}
	ws = regular(ws)
	for _, w := range ws {
		if w.From != nil && w.To != nil && !day.Before(*w.From) && !day.After(*w.To) {
			return s.ksgmCheck(ctx, tx, tid, w, rs)
		}
	}
	return nil
}

// onOutbreak: dòng 12 — ổ dịch mới (BM-10), báo nghi dịch đã xác nhận → Khẩn tới Lãnh đạo phòng, cán bộ tỉnh.
func (s *Service) onOutbreak(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p port.OutbreakReportedV1, occurred time.Time) error {
	at := occurred
	if t, err := time.Parse(time.RFC3339, p.ReportedAt); err == nil {
		at = t
	}
	province := ""
	if p.ProvinceCode != nil {
		province = *p.ProvinceCode
	}
	if province == "" {
		province = s.provinceOf(ctx, p.CommuneCode, at)
	}
	rs := &resolver{s: s, tid: tid, province: province}
	k, _ := domain.KindOf(domain.KindOutbreak)
	src := p.VersionID
	title := fmt.Sprintf("Ổ dịch mới · bệnh %s trên %s · xã %s · đàn %d con, chết %d", p.DiseaseCode, p.SpeciesCode, p.CommuneCode, p.HerdSize, p.Dead)
	if p.SuspectRef != nil {
		title += " (báo nghi dịch đã xác nhận)"
	}
	r := Raise{Kind: domain.KindOutbreak, Subject: p.OutbreakRef.String(), SubjectKind: "outbreak", CommuneCode: p.CommuneCode, ProvinceCode: province,
		SourceRef: &src, Title: title, Recipients: rs.resolve(ctx, k.Recipients, p.CommuneCode, p.ReporterRef), At: at,
		Extra: map[string]any{"outbreak_ref": p.OutbreakRef.String(), "disease_code": p.DiseaseCode, "species_code": p.SpeciesCode,
			"notifiable": p.Notifiable, "herd_size": p.HerdSize, "dead": p.Dead, "as_of": p.AsOf,
			"reason": "Báo cáo dịch bệnh mới — xem chi tiết ở màn Báo cáo dịch bệnh"}}
	if p.SuspectRef != nil {
		r.Extra["suspect_ref"] = p.SuspectRef.String()
	}
	_, _, err := s.raise(ctx, tx, tid, r)
	return err
}

// today trả ngày (giờ Việt Nam) của t, dạng UTC 00:00.
func today(t time.Time) time.Time {
	v := t.In(clock.Zone())
	return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
}
