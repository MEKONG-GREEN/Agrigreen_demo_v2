package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Danh mục Chăn nuôi – Thủy sản (W11.41, BRD CNTS v3.7 FN-06 / UR-ADM-04, D4 CN1; pf 000146). Cùng bảng
// catalog_item với danh mục Trồng trọt, tiền tố "cnts_". Thuộc tính ảnh hưởng tổng hợp có hiệu lực TỪ NGÀY
// (FN-06 BR-03, sổ phụ catalog_effective): tra theo ngày của sự việc qua CNTSCatalogs, không theo hôm nay.
const (
	// CatalogCNTSSpecies: vật nuôi / loài (heo, dê, trâu, bò, vịt, gà, chó mèo, yến…). attrs: group
	// (gia_suc|gia_cam|thu_cung|yen|thuy_san|khac), unit (con|nha|…), herd_column ("true" = cột tổng đàn CN-01 của
	// BM-04), order (thứ tự cột), description.
	CatalogCNTSSpecies = "cnts_species"
	// CatalogCNTSBreed: giống. attrs: species (mã loài, bắt buộc), description.
	CatalogCNTSBreed = "cnts_breed"
	// CatalogCNTSDisease: bệnh. attrs: species (danh sách mã loài), notifiable ("true" = bệnh phải công bố dịch), description.
	CatalogCNTSDisease = "cnts_disease"
	// CatalogCNTSVaccine: vaccine — cột tiêm phòng CN-02. attrs: diseases (danh sách mã bệnh), species (danh sách mã
	// loài SO SÁNH của quy tắc "số liều trong tuần ≤ tổng đàn" — ánh xạ vaccine–loài, BRD Mục III.5, bắt buộc),
	// report_group (heo|bo|cum_gia_cam|dai|khac — nhóm cộng của báo cáo tuần), order, description.
	CatalogCNTSVaccine = "cnts_vaccine"
	// CatalogCNTSChemical: hóa chất. attrs: unit (bắt buộc), target (gia_suc|gia_cam|chung), description.
	CatalogCNTSChemical = "cnts_chemical"
	// CatalogCNTSSlaughterFacility: cơ sở giết mổ (CSGM). attrs: facility_ref (UUID do hệ thống cấp), commune_code
	// (mã xã, bắt buộc — BR-02), facility_kind (tap_trung|nho_le), stamp_codes_gs / stamp_codes_gc (mã số khắc mộc
	// dấu gia súc / gia cầm, mỗi loại nhiều mã, vd "91.589.05,91.589.05.1"), species (loài được giết mổ), address,
	// description.
	CatalogCNTSSlaughterFacility = "cnts_slaughter_facility"
	// CatalogCNTSAquacultureObject: đối tượng nuôi thủy sản. attrs: scale_unit (ha|long|veo|m3, bắt buộc), description.
	CatalogCNTSAquacultureObject = "cnts_aquaculture_object"
	// CatalogCNTSFishingGroup: nhóm khai thác (cá, tôm, mực, khác). attrs: unit, order, description.
	CatalogCNTSFishingGroup = "cnts_fishing_group"
	// CatalogCNTSIndicator: chỉ tiêu theo dõi (pH, oxy, tăng trọng, tỷ lệ chết…). attrs: unit, value_type
	// (decimal|integer|percent), description. Ngưỡng mặc định: tham số có phiên bản IndicatorMinKey / IndicatorMaxKey.
	CatalogCNTSIndicator = "cnts_indicator"
	// CatalogCNTSModelType: loại mô hình. attrs: sector (chan_nuoi|thuy_san), species (danh sách mã loài), description.
	CatalogCNTSModelType = "cnts_model_type"
	// CatalogCNTSDocumentType: loại văn bản (quy trình, hướng dẫn, công văn…). attrs: description.
	CatalogCNTSDocumentType = "cnts_document_type"
	// CatalogCNTSVaccinationSchedule: quy trình tiêm theo ngày tuổi, gắn loại mô hình và/hoặc giống. attrs:
	// model_type, breed (ít nhất một), doses ("<vaccine>/<lần>@<ngày tuổi>" ngăn cách dấu phẩy, sắp theo ngày tuổi),
	// description.
	CatalogCNTSVaccinationSchedule = "cnts_vaccination_schedule"
)

// Thuộc tính dùng chung của danh mục CNTS.
const (
	AttrSpecies      = "species"
	AttrFacilityRef  = "facility_ref"
	AttrCommuneCode  = "commune_code"
	AttrFacilityKind = "facility_kind"
	AttrStampsGS     = "stamp_codes_gs"
	AttrStampsGC     = "stamp_codes_gc"
	AttrDoses        = "doses"
)

// Loại cơ sở giết mổ.
const (
	FacilityConcentrated = "tap_trung"
	FacilitySmall        = "nho_le"
)

// Lỗi tra danh mục CNTS.
var (
	// ErrFacilityNotFound: không có cơ sở giết mổ với facility_ref.
	ErrFacilityNotFound = errors.New("masterdata: không có cơ sở giết mổ")
)

// SlaughterFacility là cơ sở giết mổ tại một ngày (giá trị có hiệu lực ngày đó, BR-03).
type SlaughterFacility struct {
	Ref         uuid.UUID
	Code        string
	Name        string
	CommuneCode string
	Kind        string   // tap_trung | nho_le
	StampsGS    []string // mã mộc dấu gia súc; rỗng = không giết mổ gia súc
	StampsGC    []string // mã mộc dấu gia cầm; rỗng = không giết mổ gia cầm
	Species     []string // loài được giết mổ (mã cnts_species)
	Address     string
	Active      bool
	ValidFrom   time.Time // ngày bắt đầu hiệu lực của giá trị này (00:00 UTC biểu diễn ngày)
}

// Dose là một mũi trong quy trình tiêm.
type Dose struct {
	Vaccine string // mã cnts_vaccine
	Shot    int    // lần (1, 2…)
	AgeDays int    // ngày tuổi
}

// VaccinationSchedule là quy trình tiêm có hiệu lực tại một ngày. Đổi quy trình chỉ áp dụng lứa mới: bên gọi tra
// theo ngày nhận giống của lứa.
type VaccinationSchedule struct {
	Code      string
	Name      string
	ModelType string
	Breed     string
	Doses     []Dose
	ValidFrom time.Time
}

// ExpectedDate = ngày nhận giống + ngày tuổi của mũi − tuổi lúc nhận (BRD UR-TINH-10). Ngày nghiệp vụ.
func (d Dose) ExpectedDate(received time.Time, ageAtReceipt int) time.Time {
	return received.AddDate(0, 0, d.AgeDays-ageAtReceipt)
}

// CNTSCatalogs tra danh mục CNTS THEO NGÀY của sự việc (FN-06 BR-03/BR-04) cho các gói CNTS (W11.42 biểu kỳ,
// W11.46 lịch tiêm, W11.49 dịch bệnh) và cho phạm vi Cấp Xã theo cơ sở (W11.07). Tenant lấy từ context; không kiểm
// vai (module gọi đã kiểm). Lỗi "không có" bọc ErrCatalogItemNotFound / ErrNotEffective / ErrFacilityNotFound.
type CNTSCatalogs interface {
	// ItemAt trả mục với giá trị có hiệu lực tại ngày on (kể cả mục đã ngừng dùng từ sau ngày đó).
	ItemAt(ctx context.Context, catalog, code string, on time.Time) (CatalogItem, error)
	// ListAt trả các mục đang dùng tại ngày on.
	ListAt(ctx context.Context, catalog string, on time.Time) ([]CatalogItem, error)
	// FacilitiesAt trả cơ sở giết mổ đang dùng tại ngày on, lọc theo xã (rỗng = mọi xã).
	FacilitiesAt(ctx context.Context, on time.Time, communeCodes []string) ([]SlaughterFacility, error)
	// Facility trả một cơ sở theo facility_ref tại ngày on.
	Facility(ctx context.Context, ref uuid.UUID, on time.Time) (SlaughterFacility, error)
	// UnknownFacilities trả các ref KHÔNG phải cơ sở giết mổ đang dùng hôm nay (kiểm phạm vi Cấp Xã theo cơ sở).
	UnknownFacilities(ctx context.Context, refs []uuid.UUID) ([]uuid.UUID, error)
	// FacilityCommunes trả mã xã của các cơ sở giết mổ đang dùng tại ngày on (W11.42: cơ sở của lần gán Cấp Xã và
	// đơn vị lá BM-01 phải thuộc xã). Ref không có/không dùng tại ngày on → vắng trong kết quả.
	FacilityCommunes(ctx context.Context, refs []uuid.UUID, on time.Time) (map[uuid.UUID]string, error)
	// ScheduleAt trả quy trình tiêm có hiệu lực tại ngày on.
	ScheduleAt(ctx context.Context, code string, on time.Time) (VaccinationSchedule, error)
	// VaccineSpecies trả ánh xạ vaccine → loài so sánh (quy tắc "số liều trong tuần ≤ tổng đàn") tại ngày on.
	VaccineSpecies(ctx context.Context, on time.Time) (map[string][]string, error)
}
