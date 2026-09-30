package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi G3 (docs/errors.md).
var (
	ErrSeasonNotFound       = problem.New(http.StatusNotFound, "mrv.season_not_found", "Không tìm thấy vụ (chưa liên kết) hoặc ngoài phạm vi")
	ErrFarmLogNotFound      = problem.New(http.StatusNotFound, "mrv.farm_log_not_found", "Nhật ký HTX chưa được liên kết vào vụ này")
	ErrComparisonExists     = problem.New(http.StatusConflict, "mrv.comparison_exists", "Đã có kết quả đối chiếu cho cặp nhật ký ↔ minh chứng này — sửa bằng bản ghi bù")
	ErrComparisonSuperseded = problem.New(http.StatusConflict, "mrv.comparison_superseded", "Kết quả đối chiếu đã có bản ghi bù mới hơn")
	ErrComparisonNotFound   = problem.New(http.StatusNotFound, "mrv.comparison_not_found", "Không tìm thấy kết quả đối chiếu")
	ErrMethodNotFound       = problem.New(http.StatusNotFound, "mrv.method_not_found", "Không tìm thấy phiên bản phương pháp")
	ErrMethodTransition     = problem.New(http.StatusConflict, "mrv.method_invalid_transition", "Phiên bản phương pháp đã có quyết định")
)

// errValve đánh dấu payload van không hợp lệ: không tự lành → giao lại rồi DLQ (R-62).
var errValve = errors.New("mrv: payload van công bố không hợp lệ")

// Mặc định trong mã khi chưa có tham số đã duyệt (prototype mekong-green; TẠM, chờ khách).
var paramDefaults = map[string]struct{ value, unit string }{
	port.ParamCO2OpenBurning:       {"1460", "kgCO2e/t"},
	port.ParamCO2TransportRoad:     {"0.062", "kgCO2e/t·km"},
	port.ParamCO2TransportWaterway: {"0.021", "kgCO2e/t·km"},
	port.ParamAnomalyNoRecordDays:  {"14", "ngày"},
	port.ParamCompareWindowDays:    {"3", "ngày"},
}

// param đọc tham số đã duyệt có hiệu lực tại at; chưa có → mặc định (Source = default).
// Tham số chuỗi (textParamDefaults, vd danh sách mã) lấy giá trị từ ParamValue.Text.
func (s *Service) param(ctx context.Context, key string, at time.Time) (domain.ParamUse, error) {
	def, isText := textParamDefaults[key]
	if !isText {
		def = paramDefaults[key]
	}
	out := domain.ParamUse{Key: key, Value: def.value, Unit: def.unit, Source: domain.ParamSourceDefault}
	if s.Params == nil {
		return out, nil
	}
	v, err := s.Params.At(ctx, key, at)
	if errors.Is(err, mdport.ErrParamNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	unit := v.Unit
	if unit == "" {
		unit = def.unit
	}
	val := v.Value.String()
	if isText {
		val = v.Text
	}
	return domain.ParamUse{Key: key, Version: v.Version, Value: val, Unit: unit, Source: domain.ParamSourceApproved}, nil
}

func (s *Service) intParam(ctx context.Context, key string, at time.Time) (int, error) {
	p, err := s.param(ctx, key, at)
	if err != nil {
		return 0, err
	}
	d, err := decimal.NewFromString(p.Value)
	if err != nil {
		return 0, fmt.Errorf("mrv: tham số %s không phải số: %w", key, err)
	}
	return int(d.IntPart()), nil
}

func parseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }

func occurred(env outbox.Envelope, s string) time.Time {
	if t, err := parseTime(s); err == nil {
		return t
	}
	return env.OccurredAt
}

// ---------------------------------------------------------------------------
// Consumer pf.mrv.farm_log_link

type seasonEventV1 struct {
	SeasonRef   uuid.UUID `json:"season_ref"`
	EventID     uuid.UUID `json:"event_id"`
	PlotRef     uuid.UUID `json:"plot_ref"`
	CommuneCode string    `json:"commune_code"`
	SeasonCode  string    `json:"season_code"`
	SeasonYear  int       `json:"season_year"`
	Status      string    `json:"status"`
	SowingDate  string    `json:"sowing_date"`
	ActorRef    uuid.UUID `json:"actor_ref"`
	EventTime   string    `json:"event_time"`
}

type farmLogRecordedV1 struct {
	LogID          uuid.UUID `json:"log_id"`
	LogRef         uuid.UUID `json:"log_ref"`
	Version        int64     `json:"version"`
	SeasonRef      uuid.UUID `json:"season_ref"`
	PlotRef        uuid.UUID `json:"plot_ref"`
	CommuneCode    string    `json:"commune_code"`
	ActivityCode   string    `json:"activity_code"`
	LocationStatus string    `json:"location_status"`
	BecameCurrent  bool      `json:"became_current"`
	AuthorRef      uuid.UUID `json:"author_ref"`
	EventTime      string    `json:"event_time"`
}

type farmLogReviewedV1 struct {
	ReviewID    uuid.UUID `json:"review_id"`
	LogID       uuid.UUID `json:"log_id"`
	LogRef      uuid.UUID `json:"log_ref"`
	SeasonRef   uuid.UUID `json:"season_ref"`
	Decision    string    `json:"decision"`
	AuthorRef   uuid.UUID `json:"author_ref"`
	ReviewerRef uuid.UUID `json:"reviewer_ref"`
	EventTime   string    `json:"event_time"`
}

// HandleFarmLogLink là inbox.Handler của port.FarmLogLinkConsumer. Chạy trong giao
// dịch tenant của inbox.Process (vai mrv_app). Idempotent hai lớp: shared.inbox theo
// event_id và idem_key nghiệp vụ (season:<event_id>, log:<log_id>, review:<review_id>). Sổ ghi đủ mọi sự kiện; bản hiện hành
// tính khi đọc (phiên bản nhật ký lớn nhất; vụ theo thời điểm sự việc) nên thứ tự
// giao không ảnh hưởng kết quả.
func (s *Service) HandleFarmLogLink(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	ctx = tenant.WithID(ctx, env.TenantID)
	switch env.EventType {
	case port.EventSeasonOpened, port.EventSeasonCorrected, port.EventSeasonCompleted:
		var p seasonEventV1
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return fmt.Errorf("mrv: payload %s hỏng: %w", env.EventType, err)
		}
		// Khóa theo event_id nghiệp vụ trong payload (phát lại với event_id phong bì mới vẫn trùng).
		key := "season:" + p.EventID.String()
		if p.EventID == uuid.Nil {
			key = "evt:" + env.EventID.String()
		}
		if dup, err := store.IdemExists(ctx, tx, "season_link", key); err != nil || dup {
			return err
		}
		status := p.Status
		switch env.EventType {
		case port.EventSeasonOpened:
			status = "open"
		case port.EventSeasonCompleted:
			status = "completed"
		}
		row := store.SeasonRow{
			SeasonRef: p.SeasonRef, PlotRef: p.PlotRef, CommuneCode: p.CommuneCode, SeasonCode: p.SeasonCode,
			SeasonYear: p.SeasonYear, Status: status, SourceEvent: env.EventType, EventTime: occurred(env, p.EventTime),
			ActorRef: p.ActorRef, IdemKey: key,
		}
		if row.ActorRef == uuid.Nil {
			row.ActorRef = store.SystemActor
		}
		if d, err := time.Parse("2006-01-02", p.SowingDate); err == nil {
			row.SowingDate = &d
		}
		if err := store.InsertSeasonLink(ctx, tx, env.TenantID, &row); err != nil {
			return err
		}
		return s.recompute(ctx, tx, env.TenantID, p.SeasonRef, store.SystemActor)
	case port.EventFarmLogRecorded:
		var p farmLogRecordedV1
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return fmt.Errorf("mrv: payload %s hỏng: %w", env.EventType, err)
		}
		key := "log:" + p.LogID.String() // mỗi phiên bản nhật ký một liên kết
		if dup, err := store.IdemExists(ctx, tx, "farm_log_link", key); err != nil || dup {
			return err
		}
		// Ghim phiên bản danh mục htx_farm_log ĐÃ DUYỆT lúc liên kết (C-1); chưa có → NULL, không tính.
		cat, err := store.Approved(ctx, tx, port.EvidenceHTXFarmLog, 0)
		if err != nil {
			return err
		}
		var pin *int
		if cat != nil {
			v := cat.Version
			pin = &v
		}
		t := occurred(env, p.EventTime)
		ver, plot, commune, act, loc, cur := p.Version, p.PlotRef, p.CommuneCode, p.ActivityCode, p.LocationStatus, p.BecameCurrent
		if err := store.InsertFarmLogLink(ctx, tx, env.TenantID, store.FarmLogRow{
			Kind: "recorded", LogID: p.LogID, LogRef: p.LogRef, LogVersion: &ver, SeasonRef: p.SeasonRef, PlotRef: &plot,
			CommuneCode: &commune, ActivityCode: &act, LocationStatus: &loc, BecameCurrent: &cur, AuthorRef: p.AuthorRef,
			CatalogVersion: pin, LogEventTime: t, EventTime: env.OccurredAt, IdemKey: key,
		}); err != nil {
			return err
		}
		// KN FR-08: ghi AWD nhưng thửa/xã chưa có minh chứng hạ tầng đạt → cờ + thông báo cán bộ xã.
		return s.flagAWDInfra(ctx, tx, env.TenantID, p, t)
	case port.EventFarmLogReviewed:
		var p farmLogReviewedV1
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return fmt.Errorf("mrv: payload %s hỏng: %w", env.EventType, err)
		}
		key := "review:" + p.ReviewID.String()
		if p.ReviewID == uuid.Nil {
			key = "evt:" + env.EventID.String()
		}
		if dup, err := store.IdemExists(ctx, tx, "farm_log_link", key); err != nil || dup {
			return err
		}
		dec := p.Decision
		return store.InsertFarmLogLink(ctx, tx, env.TenantID, store.FarmLogRow{
			Kind: "reviewed", LogID: p.LogID, LogRef: p.LogRef, SeasonRef: p.SeasonRef, Decision: &dec, AuthorRef: p.AuthorRef,
			LogEventTime: occurred(env, p.EventTime), EventTime: env.OccurredAt, IdemKey: key,
		})
	}
	return nil
}

// ---------------------------------------------------------------------------
// Consumer van pf.mrv.collection_evidence

// collectionEvidenceV1 là payload hợp đồng van mrv_collection_evidence v1
// (contracts/publication/mrv_collection_evidence.json). Không import mkg (R-03).
type collectionEvidenceV1 struct {
	EvidenceRef uuid.UUID  `json:"evidence_ref"`
	PlotRef     uuid.UUID  `json:"plot_ref"`
	SeasonRef   *uuid.UUID `json:"season_ref"`
	CollectedOn string     `json:"collected_on"`
	Bucket      string     `json:"collected_kg_bucket"`
	BucketRule  string     `json:"bucket_rule_version"`
	Revision    int64      `json:"revision"`
	// MassBasis (G3-2, tùy chọn): HUB_IN (vắng mặt) hoặc CUSTOMER_IN_AGGREGATED (tuyến DIRECT —
	// khối lượng khách cân, gộp). Bậc vẫn tính như nhau; cờ gộp chỉ là chỉ báo (chờ C-1).
	MassBasis string `json:"mass_basis"`
}

// HandleCollectionEvidence kiểm payload bằng bộ kiểm hợp đồng lúc chạy
// (contracts/publication — khóa lạ, sai kiểu, thiếu trường → lỗi, giao lại rồi DLQ),
// rồi ghi sổ bằng chứng thu gom theo (evidence_ref, revision) — phát lại cùng
// revision là trùng. Sau đó tính lại ước tính TẠM của vụ.
func (s *Service) HandleCollectionEvidence(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != port.EventMKGCollectionEvid {
		return nil
	}
	c, err := publication.Get(port.ContractCollectionEvidence)
	if err != nil {
		return err
	}
	if err := c.Validate(env.Payload); err != nil {
		return fmt.Errorf("%w (%s): %w", errValve, env.EventType, err)
	}
	var p collectionEvidenceV1
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("%w: %w", errValve, err)
	}
	on, err := time.Parse("2006-01-02", p.CollectedOn)
	if err != nil {
		return fmt.Errorf("%w: collected_on: %w", errValve, err)
	}
	if _, ok := domain.BucketLowerKg(p.Bucket); !ok || p.Revision < 0 || p.BucketRule == "" {
		return fmt.Errorf("%w: bậc/revision/bucket_rule_version", errValve)
	}
	ctx = tenant.WithID(ctx, env.TenantID)
	key := store.CollectionIdemKey(p.EvidenceRef, p.Revision)
	if dup, err := store.IdemExists(ctx, tx, "collection_evidence", key); err != nil || dup {
		return err
	}
	row := store.CollectionRow{
		EvidenceRef: p.EvidenceRef, Revision: p.Revision, PlotRef: p.PlotRef, SeasonRef: p.SeasonRef, CollectedOn: on,
		Bucket: p.Bucket, BucketRule: p.BucketRule, SourceEventID: env.EventID, EventTime: env.OccurredAt, IdemKey: key,
	}
	if err := store.InsertCollection(ctx, tx, env.TenantID, &row); err != nil {
		return err
	}
	if p.SeasonRef == nil {
		return nil // chưa gắn vụ: lưu làm bằng chứng, không tính (không có thửa/vụ để đối chiếu duyệt)
	}
	return s.recompute(ctx, tx, env.TenantID, *p.SeasonRef, store.SystemActor)
}

// ---------------------------------------------------------------------------
// Ước tính CO2 (mrv.co2.estimate.v1 — TẠM)

// recompute tính lại ước tính của một vụ trong giao dịch tx và ghi dòng mới khi
// đầu vào, tham số hoặc tư cách chính thức thay đổi. Chưa biết vụ hoặc không có
// bằng chứng thu gom (và chưa từng có ước tính) → không làm gì.
func (s *Service) recompute(ctx context.Context, tx pgx.Tx, tid, seasonRef, actor uuid.UUID) error {
	_, err := s.recomputeRow(ctx, tx, tid, seasonRef, actor)
	return err
}

func (s *Service) recomputeRow(ctx context.Context, tx pgx.Tx, tid, seasonRef, actor uuid.UUID) (*store.EstimateRow, error) {
	season, err := store.SeasonAt(ctx, tx, seasonRef, nil)
	if err != nil || season == nil {
		return nil, err
	}
	if err := store.LockEstimate(ctx, tx, tid, seasonRef); err != nil {
		return nil, err
	}
	cols, err := store.Collections(ctx, tx, store.CollectionFilter{SeasonRef: &seasonRef, All: true})
	if err != nil {
		return nil, err
	}
	prev, err := store.LatestEstimate(ctx, tx, seasonRef)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 && prev == nil {
		return nil, nil
	}
	approved, err := store.HasAcceptedAt(ctx, tx, season.PlotRef, season.SeasonCode, nil)
	if err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	var ps [3]domain.ParamUse
	for i, k := range []string{port.ParamCO2OpenBurning, port.ParamCO2TransportRoad, port.ParamCO2TransportWaterway} {
		if ps[i], err = s.param(ctx, k, now); err != nil {
			return nil, err
		}
	}
	inputs := make([]domain.EstimateInput, 0, len(cols))
	for _, c := range cols {
		inputs = append(inputs, domain.EstimateInput{
			EvidenceRef: c.EvidenceRef.String(), Revision: c.Revision, Bucket: c.Bucket, BucketRule: c.BucketRule,
			CollectedOn: c.CollectedOn.Format("2006-01-02"),
		})
	}
	// Không có dữ liệu chặng vận chuyển qua van ở G3 → phần vận chuyển no_leg_data.
	res := domain.ComputeEstimate(port.RuleEstimate, inputs, approved, nil, ps[0], ps[1], ps[2])
	method, err := store.ApprovedMethod(ctx, tx, port.RuleEstimate)
	if err != nil {
		return nil, err
	}
	var methodID *uuid.UUID
	if method != nil && domain.MethodCovers(method.Params, res.Params) {
		id := method.ID
		methodID = &id
	}
	if prev != nil && prev.InputSHA256 == res.InputSHA256 && prev.CommuneCode == season.CommuneCode &&
		prev.SeasonCode == season.SeasonCode && sameID(prev.MethodVersionID, methodID) {
		return prev, nil // không đổi: không ghi dòng mới
	}
	row := store.EstimateRow{
		SeasonRef: seasonRef, PlotRef: season.PlotRef, CommuneCode: season.CommuneCode, SeasonCode: season.SeasonCode,
		InputSHA256: res.InputSHA256, CountedTonnes: res.CountedTonnes, AvoidedKg: res.AvoidedKg, TransportKg: res.TransportKg,
		TransportStatus: res.TransportStatus, Inputs: res.Inputs, Params: res.Params, Official: methodID != nil,
		MethodVersionID: methodID, EventTime: now, ActorRef: actor, RuleVersion: port.RuleEstimate,
		IdemKey: "estimate:" + seasonRef.String() + ":1",
	}
	if prev != nil {
		id, reason := prev.ID, "Tính lại do đầu vào, tham số hoặc phương pháp thay đổi"
		row.SupersedesID, row.CorrectionReason = &id, &reason
		row.IdemKey = "estimate:" + seasonRef.String() + ":" + strconv.FormatInt(prev.Seq+1, 10)
	}
	if err := store.InsertEstimate(ctx, tx, tid, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
