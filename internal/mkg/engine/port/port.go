// Package port là cửa công khai DUY NHẤT của module mkg/engine (R-02): kho quan sát của engine điều phối nội bộ
// (W8.2 — ADR-0036 §3; kiến trúc v4.1 §27.2 C, §41.4). Engine (W8.3, chờ MKG v1.3) đọc quan sát mới nhất mỗi
// (kind, entity_key) qua Observations; quan sát quá max_age luôn tách riêng (Snapshot.Stale), không bao giờ lẫn vào
// Snapshot.Fresh.
package port

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// JobObservationFeed là tên job nạp quan sát từ feed của cổng tích hợp (AG_MKG_JOBS của api-internal).
const JobObservationFeed = "engine_observation_feed"

// Nguồn quan sát (ADR-0036 §3).
const (
	SourceAPI    = "api"    // feed cổng tích hợp (as_of của nền tảng)
	SourceManual = "manual" // nhập tay ở vùng mkg (chưa có đường ghi — W8.3)
	SourceSensor = "sensor" // thiết bị (chưa có đường ghi — W8.3)
)

// Loại quan sát (kind) do bộ nạp feed sinh. Một feed có thể sinh nhiều kind (harvest_progress_by_commune → hai).
const (
	KindCooperative          = "cooperative"            // cooperative_directory — HTX, vị trí (payload), không giá trị
	KindPlot                 = "plot"                   // plot_directory — giá trị = diện tích thửa (ha)
	KindHarvestShare         = "harvest_share"          // harvest_progress_by_commune — tỷ lệ đã gặt (ratio)
	KindHarvestAreaRemaining = "harvest_area_remaining" // harvest_progress_by_commune — diện tích còn chờ gặt (ha)
	KindAdminUnit            = "admin_unit"             // admin_units — đơn vị hành chính có hiệu lực + kế thừa
	KindSeasonHarvestWindow  = "season_harvest_window"  // season_harvest_dates — ngày gieo / gặt dự kiến (payload)
	KindWaterwayNetwork      = "waterway_network"       // waterway_routes — siêu dữ liệu phiên bản đồ thị
)

// Đơn vị giá trị.
const (
	UnitHectare = "ha"
	UnitRatio   = "ratio"
)

// Observation là một quan sát trạng thái ngoài (một dòng sổ mkg_engine.observation).
type Observation struct {
	ID        uuid.UUID
	Kind      string
	EntityKey string
	Source    string
	// Nguồn gốc feed (Source = api): tên hợp đồng, phiên bản, seq + event_id của mục, cursor của trang chứa mục.
	Feed            string
	ContractVersion int
	FeedSeq         int64
	EventID         uuid.UUID
	FeedCursor      string
	// AsOf: mốc dữ liệu của nguồn khi quan sát được ghi. EffectiveAsOf: AsOf, hoặc as_of của lần đọc tới đầu feed
	// gần nhất nếu mới hơn — feed là dòng trạng thái, bản ghi chưa bị thay / rút vẫn đúng tới mốc đó (as_of tiến cả
	// khi không đổi). Hạn dùng tính trên EffectiveAsOf.
	AsOf          time.Time
	EffectiveAsOf time.Time
	MaxAge        time.Duration
	Retracted     bool
	// Value (decimal, giữ số lẻ của nguồn) + Unit; nil khi quan sát không có giá trị số. Uncertainty: nửa độ rộng
	// tuyệt đối cùng đơn vị (≥ 0; nil khi không có giá trị); UncertaintyBasis: mã căn cứ.
	Value            *decimal.Decimal
	Unit             string
	Uncertainty      *decimal.Decimal
	UncertaintyBasis []string
	Payload          json.RawMessage
	MapperVersion    string
	RecordedAt       time.Time
}

// Snapshot là quan sát mới nhất mỗi entity của một kind tại thời điểm At.
type Snapshot struct {
	Kind string
	At   time.Time
	// Fresh: At ≤ EffectiveAsOf + MaxAge. Stale: quá hạn — engine KHÔNG được dùng như dữ liệu tươi (tăng tồn đệm,
	// gắn cờ kế hoạch — §41.4). Missing: entity được hỏi mà chưa có quan sát, hoặc quan sát mới nhất là rút
	// (retract của feed, thu hồi cấp quyền).
	Fresh   []Observation
	Stale   []Observation
	Missing []string
}

// Query là yêu cầu đọc quan sát mới nhất.
type Query struct {
	Kind string
	// EntityKeys rỗng = mọi entity của kind (Missing khi đó chỉ gồm entity có quan sát mới nhất là rút).
	EntityKeys []string
}

// Observations là port đọc của engine (W8.3): quan sát mới nhất mỗi (kind, entity_key), tenant tường minh.
type Observations interface {
	Latest(ctx context.Context, tid uuid.UUID, q Query) (Snapshot, error)
}

// Lỗi của module.
var (
	ErrUnknownKind      = errors.New("engine: kind quan sát không có trong danh mục")
	ErrFeedUnconfigured = errors.New("engine: chưa cấu hình feed của cổng tích hợp")
)
