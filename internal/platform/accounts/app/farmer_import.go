package app

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// MaxFarmerImportRows: số dòng tối đa mỗi lần nhập tài khoản nông dân (xử lý đồng bộ).
const MaxFarmerImportRows = 200

// FarmerImportRow là kết quả một dòng. Reason không chứa dữ liệu nhập (R-14).
type FarmerImportRow struct {
	Row          int
	Status       string // created | rejected
	Reason       string
	UserRef      uuid.UUID
	HouseholdRef uuid.UUID
	Activation   *Activation // liên kết + QR của dòng đã tạo (chỉ trả một lần)
}

// FarmerImportResult là kết quả cả tệp (HTX US-ACC-02 AC-2).
type FarmerImportResult struct {
	Created, Rejected int
	Rows              []FarmerImportRow
}

// Cột của tệp mẫu (tiêu đề đã chuẩn hóa).
var farmerImportAliases = map[string]string{
	"phone": "phone", "so_dien_thoai": "phone", "sdt": "phone",
	"household_ref": "household_ref", "ma_nong_ho": "household_ref", "ma_ho": "household_ref",
	"full_name": "full_name", "ho_ten": "full_name", "ho_va_ten": "full_name",
}

// ImportFarmers tạo tài khoản nông dân hàng loạt từ CSV/XLSX (HTX US-ACC-02): tệp sai
// mẫu → từ chối cả tệp; dòng lỗi bị loại kèm lý do ("Thiếu SĐT", "SĐT đã tồn tại"…),
// dòng hợp lệ vẫn được tạo và nhận lượt kích hoạt. Khóa idem của dòng n là <Idempotency-Key>:row:<n>.
func (s *Service) ImportFarmers(ctx context.Context, src io.Reader, idemKey string) (FarmerImportResult, error) {
	a, err := s.farmerCaller(ctx)
	if err != nil {
		return FarmerImportResult{}, err
	}
	if err := requireKey(idemKey); err != nil {
		return FarmerImportResult{}, err
	}
	lim := tabular.Limits{MaxBytes: 2 << 20, MaxRows: MaxFarmerImportRows}
	tb, err := tabular.Read(src, lim)
	if err != nil {
		return FarmerImportResult{}, ErrImportInvalid.WithDetail(tabular.Message(err, lim))
	}
	col := tb.Columns(farmerImportAliases)
	if missing := tabular.Missing(col, "phone"); len(missing) > 0 {
		return FarmerImportResult{}, errBadTemplate.WithDetail("Tệp thiếu cột bắt buộc: phone (SĐT)")
	}
	res := FarmerImportResult{Rows: []FarmerImportRow{}}
	seen := map[string]int{}
	for _, rec := range tb.Rows {
		row := FarmerImportRow{Row: rec.Line, Status: RowRejected}
		reject := func(reason string) {
			row.Reason = reason
			res.Rejected++
			res.Rows = append(res.Rows, row)
		}
		prof := domain.FarmerProfile{Phone: rec.Get(col, "phone")}
		prof.FirstName, prof.LastName = domain.SplitFullName(rec.Get(col, "full_name"))
		norm, err := prof.Normalize()
		if err != nil {
			reject(reasonOf(err))
			continue
		}
		var hh uuid.UUID
		if v := strings.TrimSpace(rec.Get(col, "household_ref")); v != "" {
			if hh, err = uuid.Parse(v); err != nil || hh == uuid.Nil {
				reject("Sai định dạng mã nông hộ")
				continue
			}
		}
		if prev, dup := seen[norm.Phone]; dup {
			reject("SĐT trùng với dòng " + strconv.Itoa(prev) + " trong tệp")
			continue
		}
		seen[norm.Phone] = rec.Line
		out, err := s.createFarmer(ctx, a, FarmerInput{HouseholdRef: hh, Profile: norm, IdemKey: idemKey + ":row:" + strconv.Itoa(rec.Line)})
		if err != nil {
			if p := problem.From(err); p.Status >= 500 {
				// Lỗi hệ thống giữa chừng: client gửi lại cùng khóa; dòng đã tạo được phát lại theo khóa của dòng.
				return FarmerImportResult{}, err
			}
			reject(farmerReason(err))
			continue
		}
		row.Status, row.UserRef, row.HouseholdRef, row.Activation = RowCreated, out.Link.UserRef, out.Link.HouseholdRef, out.Activation
		res.Created++
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

// farmerReason quy lỗi về nhãn lý do theo AC (HTX US-ACC-02 AC-2), không chứa dữ liệu nhập.
func farmerReason(err error) string {
	switch problem.From(err).Code {
	case ErrFarmerExists.Code:
		return "Nông hộ đã có tài khoản"
	case ErrPhoneMismatch.Code:
		return domain.MsgPhoneHouseholdDiff
	case ErrHouseholdInvalid.Code:
		return domain.MsgHouseholdMissing
	}
	return reasonOf(err)
}
