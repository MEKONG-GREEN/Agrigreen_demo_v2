package domain

// Xã báo giá + phân công (W11.72, pf 000145; BRD Báo giá v1.3 FN-AD-04.02 Danh mục xã báo giá, FN-AD-04.01 phân công
// cán bộ Xã / cán bộ Cấp Tỉnh theo ngày hiệu lực): quy tắc thuần — mã xã tự sinh, kiểm dữ liệu, phép chiếu "xã có hiệu
// lực tại ngày", người phụ trách tại ngày. Không I/O, không đồng hồ.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// KindRegion là danh mục nhóm vùng (An Giang, Kiên Giang — BRD mục II "Nhóm vùng"): cùng dạng danh mục nền của pf 000138
// (sổ pf_price.region_group, chuỗi 'region:<mã>').
const KindRegion Kind = "region"

// Vai phụ trách một xã báo giá (BR-01, BR-02 FN-AD-04.01).
const (
	OfficerCommune  = "xa"   // cán bộ Xã (CTV) phụ trách chính — một người tại một thời điểm
	OfficerProvince = "tinh" // cán bộ Cấp Tỉnh được phân công — xã chưa phân công: mọi Cấp Tỉnh xử lý
)

// Trạng thái xã báo giá tại một ngày (Hoạt động / Chờ hiệu lực / Ngưng).
const (
	PointStateActive  = "active"
	PointStatePending = "pending"
	PointStateRetired = "retired"
)

var (
	rePointAuto  = regexp.MustCompile(`^XBG[0-9]{4}$`)
	reRegionCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	reAdminUnit  = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z_.-]{0,19}$`)
)

// PointCode trả mã xã báo giá thứ n (BR-01: hệ thống sinh, không tái sử dụng): "XBG" + 4 chữ số.
func PointCode(n int) string { return fmt.Sprintf("XBG%04d", n) }

// ValidAutoPointCode: mã xã báo giá đúng dạng hệ thống sinh.
func ValidAutoPointCode(s string) bool { return rePointAuto.MatchString(s) }

// ValidRegionCode: mã nhóm vùng đúng dạng.
func ValidRegionCode(s string) bool { return reRegionCode.MatchString(s) }

// ValidAdminUnitCode: mã ĐVHC đúng dạng (mã của danh mục ĐVHC dùng chung — pf/masterdata, k_ref.admin_unit).
func ValidAdminUnitCode(s string) bool { return reAdminUnit.MatchString(s) }

// Point là nội dung một phiên bản xã báo giá.
type Point struct {
	Code          string
	AdminUnitCode string // mã ĐVHC xã liên kết (ADR-0032 — có hiệu lực theo ngày)
	Name          string // tên xã theo ĐVHC tại ngày hiệu lực của phiên bản (ảnh chụp — BRD B2 "tên xã lấy theo ĐVHC")
	RegionCode    string // nhóm vùng (BR-04: báo cáo theo nhóm vùng của xã tại thời điểm báo giá)
	Status        string // active | retired
	ValidFrom     time.Time
	Note          string // lý do ngưng / chuyển ĐVHC
}

// ValidatePoint kiểm nội dung xã báo giá (cùng ràng buộc CHECK của pf 000145).
func ValidatePoint(p Point) error {
	switch {
	case !ValidAutoPointCode(p.Code):
		return fmt.Errorf("%w: mã xã báo giá %q", ErrInvalid, p.Code)
	case !ValidAdminUnitCode(p.AdminUnitCode):
		return fmt.Errorf("%w: mã ĐVHC %q", ErrInvalid, p.AdminUnitCode)
	case !ValidRegionCode(p.RegionCode):
		return fmt.Errorf("%w: nhóm vùng %q", ErrInvalid, p.RegionCode)
	case p.Status != StatusActive && p.Status != StatusRetired:
		return fmt.Errorf("%w: trạng thái %q", ErrInvalid, p.Status)
	case p.ValidFrom.IsZero():
		return fmt.Errorf("%w: thiếu ngày hiệu lực", ErrInvalid)
	case p.Status == StatusRetired && strings.TrimSpace(p.Note) == "":
		return fmt.Errorf("%w: ngưng xã báo giá phải có lý do", ErrInvalid)
	}
	if err := runeLen("tên xã", p.Name, 1, 100); err != nil {
		return err
	}
	if p.Note != "" {
		return runeLen("lý do", p.Note, 1, 500)
	}
	return nil
}

// PointState là xã báo giá nhìn từ một ngày: phiên bản hiển thị, trạng thái, ngày bắt đầu báo giá (Since) và ngày
// hiệu lực đến (ngày trước phiên bản 'retired' kế tiếp; zero = không thời hạn).
type PointState struct {
	Point
	Version int64
	State   string // active | pending | retired
	Since   time.Time
	ValidTo time.Time
}

// PointStates chiếu sổ xã báo giá tại ngày at (cùng phép chiếu với ItemStates).
func PointStates(vs []Ver[Point], at time.Time) map[string]PointState {
	eff, up := EffectiveAt(vs, at), Upcoming(vs, at)
	since, retireAfter := map[string]time.Time{}, map[string]time.Time{}
	keys := map[string]bool{}
	for _, v := range vs {
		keys[v.Key] = true
		if v.Status == StatusActive {
			if s, ok := since[v.Key]; !ok || v.ValidFrom.Before(s) {
				since[v.Key] = v.ValidFrom
			}
		}
		if v.Status == StatusRetired && v.ValidFrom.After(at) {
			if r, ok := retireAfter[v.Key]; !ok || v.ValidFrom.Before(r) {
				retireAfter[v.Key] = v.ValidFrom
			}
		}
	}
	out := make(map[string]PointState, len(keys))
	for k := range keys {
		var st PointState
		if e, ok := eff[k]; ok {
			st = PointState{Point: e.V, Version: e.Seq, State: PointStateActive}
			if e.Status == StatusRetired {
				st.State, st.ValidTo = PointStateRetired, e.ValidFrom.AddDate(0, 0, -1)
			}
		} else if u, ok := up[k]; ok {
			st = PointState{Point: u.V, Version: u.Seq, State: PointStatePending}
		} else {
			continue
		}
		if st.State != PointStateRetired {
			if r, ok := retireAfter[k]; ok {
				st.ValidTo = r.AddDate(0, 0, -1)
			}
		}
		st.Since = since[k]
		out[k] = st
	}
	return out
}

// AdminUnitClash trả mã xã báo giá KHÁC self đang (hoặc sẽ) gắn mã ĐVHC au từ ngày from trở đi (luồng ngoại lệ
// FN-AD-04.02: "Mã ĐVHC đã gắn với xã báo giá khác đang hoạt động → chặn", AC-02). Rỗng = không trùng. Xét phiên bản
// có hiệu lực tại from và mọi phiên bản sau from.
func AdminUnitClash(vs []Ver[Point], au, self string, from time.Time) string {
	for code, v := range EffectiveAt(vs, from) {
		if code != self && v.Status == StatusActive && v.V.AdminUnitCode == au {
			return code
		}
	}
	for _, v := range vs {
		if v.Key != self && v.ValidFrom.After(from) && v.Status == StatusActive && v.V.AdminUnitCode == au {
			return v.Key
		}
	}
	return ""
}

// OfficerAssign là một phiên bản phân công người phụ trách một xã báo giá (chuỗi '<vai>:<mã xã>'). OfficerRef Nil = bỏ
// phân công từ ngày hiệu lực (xã "Chưa có cán bộ Xã" / "Chưa phân công").
type OfficerAssign struct {
	PointCode  string
	Role       string // OfficerCommune | OfficerProvince
	OfficerRef uuid.UUID
}

// OfficerKey là khóa chuỗi phân công.
func OfficerKey(role, point string) string { return role + ":" + point }

// ValidOfficerRole: vai phụ trách hợp lệ.
func ValidOfficerRole(r string) bool { return r == OfficerCommune || r == OfficerProvince }

// OfficerAt trả người phụ trách vai role của xã point tại ngày at (uuid.Nil = chưa có) và ngày bắt đầu phân công.
func OfficerAt(vs []Ver[OfficerAssign], role, point string, at time.Time) (uuid.UUID, time.Time) {
	v, ok := EffectiveAt(vs, at)[OfficerKey(role, point)]
	if !ok {
		return uuid.Nil, time.Time{}
	}
	return v.V.OfficerRef, v.ValidFrom
}

// OfficerPoints trả mã các xã (đang hoạt động tại at theo states) mà ref phụ trách với vai role tại ngày at, sắp tăng.
func OfficerPoints(vs []Ver[OfficerAssign], states map[string]PointState, role string, ref uuid.UUID, at time.Time) []string {
	var out []string
	for _, v := range EffectiveAt(vs, at) {
		if v.V.Role != role || v.V.OfficerRef != ref || ref == uuid.Nil {
			continue
		}
		if st, ok := states[v.V.PointCode]; ok && st.State == PointStateActive {
			out = append(out, v.V.PointCode)
		}
	}
	sort.Strings(out)
	return out
}

// DefaultRegions là 2 nhóm vùng mặc định (BRD mục II: An Giang và Kiên Giang "như hệ thống hiện tại"; Q-053 — nhóm
// vùng là thuộc tính riêng của Báo giá, không dùng khu vực của pf/party).
func DefaultRegions(validFrom time.Time) []Entry {
	return []Entry{
		{Kind: KindRegion, Code: "an_giang", Name: "An Giang", SortOrder: 10, ValidFrom: validFrom},
		{Kind: KindRegion, Code: "kien_giang", Name: "Kiên Giang", SortOrder: 20, ValidFrom: validFrom},
	}
}
