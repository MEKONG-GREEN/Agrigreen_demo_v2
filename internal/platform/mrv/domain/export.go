package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// PackageFormat là mã định dạng gói xuất.
const PackageFormat = "agrigreen.mrv.package/v1"

// ExportFilters là bộ lọc ĐÃ GIẢI phạm vi, lưu bất biến trên export_job.
type ExportFilters struct {
	EvidenceType   string   `json:"evidence_type,omitempty"`
	AllUnits       bool     `json:"all_units"`
	AdminUnitCodes []string `json:"admin_unit_codes"` // rỗng khi AllUnits
	SeasonCode     string   `json:"season_code,omitempty"`
	EventFrom      string   `json:"event_from,omitempty"` // RFC 3339, bao gồm
	EventTo        string   `json:"event_to,omitempty"`   // RFC 3339, không bao gồm
	ReviewStatus   string   `json:"review_status,omitempty"`
}

// Validate kiểm bộ lọc (trước khi giải phạm vi).
func (f ExportFilters) Validate() []problem.FieldError {
	var errs []problem.FieldError
	add := func(fl, r string) { errs = append(errs, problem.FieldError{Field: "filters." + fl, Reason: r}) }
	if f.EvidenceType != "" && !evidenceTypeRe.MatchString(f.EvidenceType) {
		add("evidence_type", "mã loại minh chứng không hợp lệ")
	}
	if len(f.AdminUnitCodes) > 500 {
		add("admin_unit_codes", "tối đa 500 đơn vị")
	}
	for _, c := range f.AdminUnitCodes {
		if !unitCodeRe.MatchString(c) {
			add("admin_unit_codes", "mã đơn vị không hợp lệ: "+c)
		}
	}
	var from, to time.Time
	var err error
	if f.EventFrom != "" {
		if from, err = time.Parse(time.RFC3339Nano, f.EventFrom); err != nil {
			add("event_from", "phải theo RFC 3339")
		}
	}
	if f.EventTo != "" {
		if to, err = time.Parse(time.RFC3339Nano, f.EventTo); err != nil {
			add("event_to", "phải theo RFC 3339")
		}
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		add("event_to", "phải sau event_from")
	}
	switch ReviewStatus(f.ReviewStatus) {
	case "", ReviewPending, ReviewAccepted, ReviewRejected:
	default:
		add("review_status", "phải là pending, accepted hoặc rejected")
	}
	return errs
}

// Normalize sắp xếp và bỏ trùng danh sách đơn vị để ảnh chụp ổn định.
func (f ExportFilters) Normalize() ExportFilters {
	codes := slices.Clone(f.AdminUnitCodes)
	sort.Strings(codes)
	f.AdminUnitCodes = slices.Compact(codes)
	if f.AdminUnitCodes == nil {
		f.AdminUnitCodes = []string{}
	}
	return f
}

// ExportStatus là trạng thái job xuất.
type ExportStatus string

// Các trạng thái job.
const (
	ExportQueued    ExportStatus = "queued"
	ExportCompleted ExportStatus = "completed"
	ExportFailed    ExportStatus = "failed"
)

// ExportJob là một yêu cầu xuất gói (bảng pf_mrv.export_job).
type ExportJob struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	Status         ExportStatus
	Filters        ExportFilters
	RequestedBy    uuid.UUID
	RequestedAt    time.Time // mốc ảnh chụp (as_of)
	IdemKey        string
	ObjectKey      *string
	PackageSHA256  *string
	SnapshotSHA256 *string
	EvidenceCount  *int
	ErrorCode      *string
	CompletedAt    *time.Time
}

// ObjectKey trả khóa đối tượng của gói: packages/<tenant>/<export_id>.json.
func ObjectKey(tenantID, exportID uuid.UUID) string {
	return "packages/" + tenantID.String() + "/" + exportID.String() + ".json"
}

// SnapshotField là một trường danh mục trong ảnh chụp.
type SnapshotField struct {
	Key        string   `json:"key"`
	LabelVi    string   `json:"label_vi"`
	DataType   string   `json:"data_type"`
	Unit       *string  `json:"unit"`
	Required   bool     `json:"required"`
	Min        *string  `json:"min"`
	Max        *string  `json:"max"`
	MaxScale   *int     `json:"max_scale"`
	EnumValues []string `json:"enum_values"`
	Position   int      `json:"position"`
}

// SnapshotCatalog là một phiên bản danh mục được minh chứng trong gói ghim tới.
type SnapshotCatalog struct {
	EvidenceType string          `json:"evidence_type"`
	Version      int             `json:"version"`
	Provisional  bool            `json:"provisional"`
	SourceNote   string          `json:"source_note"`
	SetterRef    string          `json:"setter_ref"`
	ApproverRef  string          `json:"approver_ref"`
	ApprovedAt   string          `json:"approved_at"`
	Fields       []SnapshotField `json:"fields"`
}

// SnapshotGPS là toạ độ trong gói (chuỗi thập phân 7 chữ số lẻ).
type SnapshotGPS struct {
	Lat       string  `json:"lat" pii:"true"`
	Lon       string  `json:"lon" pii:"true"`
	AccuracyM *string `json:"accuracy_m"`
}

// SnapshotReview là quyết định duyệt của bản được xuất (tại as_of).
type SnapshotReview struct {
	ID          string `json:"id"`
	Decision    string `json:"decision"`
	Reason      string `json:"reason"`
	ReviewerRef string `json:"reviewer_ref"`
	EventTime   string `json:"event_time"`
	RecordedAt  string `json:"recorded_at"`
}

// SnapshotEvidence là bản hiện hành (tại as_of) của một chuỗi minh chứng.
type SnapshotEvidence struct {
	ID               string          `json:"id"`
	RootID           string          `json:"root_id"`
	ChainVersion     int             `json:"chain_version"`
	SupersedesID     *string         `json:"supersedes_id"`
	CorrectionReason *string         `json:"correction_reason"`
	EvidenceType     string          `json:"evidence_type"`
	CatalogVersion   int             `json:"catalog_version"`
	AdminUnitCode    string          `json:"admin_unit_code"`
	PlotRef          *string         `json:"plot_ref"`
	HouseholdRef     *string         `json:"household_ref"`
	SeasonCode       *string         `json:"season_code"`
	Values           json.RawMessage `json:"values"`
	FileRefs         []FileRef       `json:"file_refs"`
	GPS              *SnapshotGPS    `json:"gps"`
	EventTime        string          `json:"event_time"`
	RecordedAt       string          `json:"recorded_at"`
	CapturedBy       string          `json:"captured_by"`
	DeviceID         *string         `json:"device_id"`
	RowHash          string          `json:"row_hash"` // nối gói với chuỗi băm của sổ (mục 8.2)
	Review           *SnapshotReview `json:"review"`
}

// Snapshot là ảnh chụp dữ liệu của gói — phần được băm.
type Snapshot struct {
	Catalogs []SnapshotCatalog  `json:"catalogs"`
	Evidence []SnapshotEvidence `json:"evidence"`
}

// Sort đặt thứ tự ổn định: danh mục theo (loại, phiên bản), minh chứng theo root_id.
func (s *Snapshot) Sort() {
	sort.Slice(s.Catalogs, func(i, j int) bool {
		a, b := s.Catalogs[i], s.Catalogs[j]
		if a.EvidenceType != b.EvidenceType {
			return a.EvidenceType < b.EvidenceType
		}
		return a.Version < b.Version
	})
	sort.Slice(s.Evidence, func(i, j int) bool { return s.Evidence[i].RootID < s.Evidence[j].RootID })
	if s.Catalogs == nil {
		s.Catalogs = []SnapshotCatalog{}
	}
	if s.Evidence == nil {
		s.Evidence = []SnapshotEvidence{}
	}
}

// Canonical trả JSON chuẩn hóa của ảnh chụp và băm SHA-256 (hex) của nó.
//
// Dạng chuẩn: khóa đối tượng sắp theo thứ tự byte, không khoảng trắng, số giữ
// nguyên văn bản, chuỗi theo encoding/json. Bên thẩm định dựng lại được băm
// bằng cách chuẩn hóa trường "snapshot" của gói theo cùng quy tắc.
func (s Snapshot) Canonical() ([]byte, string, error) {
	s.Sort()
	b, err := CanonicalJSON(s)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]), nil
}

// CanonicalJSON tuần tự hóa v rồi chuẩn hóa: khóa sắp xếp, số giữ nguyên văn.
func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Package là nội dung tệp gói xuất lưu ở kho đối tượng.
type Package struct {
	Format         string          `json:"format"`
	ExportID       string          `json:"export_id"`
	TenantID       string          `json:"tenant_id"`
	AsOf           string          `json:"as_of"`
	RequestedBy    string          `json:"requested_by"`
	Filters        ExportFilters   `json:"filters"`
	Provisional    bool            `json:"provisional"` // có danh mục tạm (chờ C-1) trong gói
	SnapshotSHA256 string          `json:"snapshot_sha256"`
	Snapshot       json.RawMessage `json:"snapshot"`
}

// BuildPackage dựng gói từ job và ảnh chụp. Nội dung chỉ phụ thuộc dữ liệu tại
// as_of (không có thời điểm sinh), nên dựng lại cho ra đúng từng byte — worker
// giao lặp sau sự cố ghi lại cùng đối tượng.
func BuildPackage(job ExportJob, s Snapshot) (body []byte, snapshotSHA string, err error) {
	canon, sum, err := s.Canonical()
	if err != nil {
		return nil, "", err
	}
	prov := false
	for _, c := range s.Catalogs {
		prov = prov || c.Provisional
	}
	p := Package{
		Format: PackageFormat, ExportID: job.ID.String(), TenantID: job.TenantID.String(),
		AsOf: FormatTime(job.RequestedAt), RequestedBy: job.RequestedBy.String(), Filters: job.Filters.Normalize(),
		Provisional: prov, SnapshotSHA256: sum, Snapshot: canon,
	}
	body, err = CanonicalJSON(p)
	if err != nil {
		return nil, "", fmt.Errorf("mrv: tuần tự hóa gói: %w", err)
	}
	return body, sum, nil
}

// FormatTime định dạng thời điểm trong gói: UTC, RFC 3339 có phần lẻ micro giây
// (độ chính xác của timestamptz).
func FormatTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000Z07:00") }
