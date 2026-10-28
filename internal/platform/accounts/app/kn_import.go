package app

import (
	"context"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// Giới hạn tệp nạp tài khoản KN nhiều vai trò (xử lý đồng bộ, mỗi tài khoản một lượt Keycloak).
const (
	MaxKNImportBytes = 2 << 20
	MaxKNImportRows  = 300
)

// KNImportRow là kết quả một dòng (một lần gán vai trò). Reason không chứa dữ liệu nhập (R-14).
type KNImportRow struct {
	Row            int
	Status         string // created | rejected
	Reason         string
	UserID         uuid.UUID
	ActivationSent bool
}

// KNImportResult là kết quả cả tệp: Accounts = số tài khoản đã tạo, Created/Rejected = số dòng.
type KNImportResult struct {
	Accounts          int
	Created, Rejected int
	Rows              []KNImportRow
}

// Cột của tệp mẫu (tiêu đề đã chuẩn hóa bằng tabular.NormalizeHeader).
var knImportAliases = map[string]string{
	"ho_ten": "full_name", "ho_va_ten": "full_name", "full_name": "full_name",
	"email": "email", "thu_dien_tu": "email",
	"so_dien_thoai": "phone", "sdt": "phone", "phone": "phone",
	"vai_tro": "role", "role": "role",
	"ma_nut": "org", "ma_to_chuc": "org", "nut_to_chuc": "org", "org_code": "org", "org_ref": "org",
	"phong_ban": "departments", "ma_phong_ban": "departments", "departments": "departments",
	"cap_quan_ly": "mgmt_level", "mgmt_level": "mgmt_level",
	"xa_phu_trach": "communes", "ma_xa_phu_trach": "communes", "communes": "communes",
}

// knRoleOf nhận mã vai (kn_tw…) hoặc nhãn ("Cấp Quốc gia", "Cấp Tỉnh", "Cấp Xã"). Vai Admin không nạp qua tệp.
func knRoleOf(v string) string {
	switch tabular.NormalizeHeader(v) {
	case port.KNTW, "cap_quoc_gia", "cap_tw", "trung_uong":
		return port.KNTW
	case port.KNTinh, "cap_tinh":
		return port.KNTinh
	case port.KNXa, "cap_xa":
		return port.KNXa
	}
	return ""
}

// mgmtOf nhận mã hoặc nhãn cấp quản lý (danh mục tạm Q-05).
func (s *Service) mgmtOf(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if code, ok := s.AttributeCatalog().ManagementCode(v); ok {
		return code
	}
	return v // để NormalizeAssignment báo lỗi danh mục
}

func splitList(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == ',' || r == '\n' })
}

type knImportGroup struct {
	key     string
	first   int
	lines   []int
	profile domain.Profile
	asg     []domain.AssignmentInput
	bad     map[int]string // dòng → lý do (dòng lỗi riêng, cả nhóm bị loại)
}

// ImportKNUsers tạo tài khoản KN nhiều vai trò từ CSV/XLSX (UR-AD-04, FN-AD-04.04 "Nạp Excel"). Mỗi dòng là một lần
// gán vai trò; các dòng cùng Email gộp thành MỘT tài khoản nhiều vai (vd Cấp Xã 2 xã + Cấp Quốc gia). Cột: Họ tên,
// Email (bắt buộc), SĐT, Vai trò (kn_tw|kn_tinh|kn_xa hoặc "Cấp Quốc gia/Tỉnh/Xã"), Mã Nút (mã tổ chức hoặc UUID),
// Phòng ban (mã, phân tách ";"), Cấp quản lý, Xã phụ trách (mã, phân tách ";"). Tệp sai mẫu → từ chối cả tệp; tài
// khoản có dòng lỗi bị loại cả nhóm (không tạo tài khoản thiếu vai); tài khoản khác vẫn được tạo như "Tạo tài khoản"
// (CreateKNUser: tiên quyết Nút + phòng ban, liên kết kích hoạt). Khóa idem của tài khoản = <Idempotency-Key>:acc:<dòng đầu>.
func (s *Service) ImportKNUsers(ctx context.Context, src io.Reader, idemKey string) (KNImportResult, error) {
	a, err := s.knCaller(ctx)
	if err != nil {
		return KNImportResult{}, err
	}
	if err := requireKey(idemKey); err != nil {
		return KNImportResult{}, err
	}
	lim := tabular.Limits{MaxBytes: MaxKNImportBytes, MaxRows: MaxKNImportRows}
	tb, err := tabular.Read(src, lim)
	if err != nil {
		return KNImportResult{}, ErrImportInvalid.WithDetail(tabular.Message(err, lim))
	}
	col := tb.Columns(knImportAliases)
	if missing := tabular.Missing(col, "full_name", "email", "role", "org"); len(missing) > 0 {
		return KNImportResult{}, ErrImportInvalid.WithDetail("Tệp thiếu cột bắt buộc: Họ tên, Email, Vai trò, Mã Nút")
	}
	groups, order := s.groupKNRows(ctx, a, tb, col)
	res := KNImportResult{Rows: []KNImportRow{}}
	byLine := map[int]KNImportRow{}
	for _, k := range order {
		g := groups[k]
		reject := func(reason string) {
			for _, l := range g.lines {
				r := reason
				if own, ok := g.bad[l]; ok {
					r = own
				} else if len(g.bad) > 0 {
					r = "Tài khoản có dòng lỗi khác — sửa rồi nạp lại"
				}
				byLine[l] = KNImportRow{Row: l, Status: RowRejected, Reason: r}
			}
		}
		if len(g.bad) > 0 {
			reject("")
			continue
		}
		out, err := s.CreateKNUser(ctx, KNCreateInput{Profile: g.profile, Assignments: g.asg, IdemKey: idemKey + ":acc:" + strconv.Itoa(g.first)})
		if err != nil {
			if p := problem.From(err); p.Status >= 500 {
				// Lỗi hệ thống giữa chừng: client gửi lại cùng khóa; tài khoản đã tạo được phát lại theo khóa của nhóm.
				return KNImportResult{}, err
			}
			reject(knReason(err))
			continue
		}
		res.Accounts++
		for _, l := range g.lines {
			byLine[l] = KNImportRow{Row: l, Status: RowCreated, UserID: out.User.ID, ActivationSent: out.ActivationSent}
		}
	}
	for _, rec := range tb.Rows {
		r, ok := byLine[rec.Line]
		if !ok {
			continue
		}
		if r.Status == RowCreated {
			res.Created++
		} else {
			res.Rejected++
		}
		res.Rows = append(res.Rows, r)
	}
	return res, nil
}

// groupKNRows gộp dòng theo Email và kiểm từng dòng (không gọi Keycloak).
func (s *Service) groupKNRows(ctx context.Context, a *actor, tb *tabular.Table, col map[string]int) (map[string]*knImportGroup, []string) {
	groups := map[string]*knImportGroup{}
	var order []string
	codes, _ := s.Orgs.(partyport.OrgCodes)
	for _, rec := range tb.Rows {
		email := strings.ToLower(strings.TrimSpace(rec.Get(col, "email")))
		key := email
		if key == "" {
			key = "#" + strconv.Itoa(rec.Line) // dòng thiếu Email: nhóm riêng, bị loại
		}
		g := groups[key]
		if g == nil {
			g = &knImportGroup{key: key, first: rec.Line, bad: map[int]string{}}
			first, last := domain.SplitFullName(rec.Get(col, "full_name"))
			g.profile = domain.Profile{Email: email, Phone: rec.Get(col, "phone"), FirstName: first, LastName: last}
			groups[key] = g
			order = append(order, key)
		}
		g.lines = append(g.lines, rec.Line)
		bad := func(reason string) { g.bad[rec.Line] = reason }
		if email == "" {
			bad(domain.MsgEmailMissing)
			continue
		}
		if len(g.lines) > 1 {
			first, last := domain.SplitFullName(rec.Get(col, "full_name"))
			if (first != "" || last != "") && (first != g.profile.FirstName || last != g.profile.LastName) {
				bad("Họ tên khác dòng " + strconv.Itoa(g.first) + " của cùng Email")
				continue
			}
			if p := domain.NormalizePhone(rec.Get(col, "phone")); p != "" && p != domain.NormalizePhone(g.profile.Phone) {
				bad("SĐT khác dòng " + strconv.Itoa(g.first) + " của cùng Email")
				continue
			}
		}
		role := knRoleOf(rec.Get(col, "role"))
		if role == "" {
			bad("Vai trò không hợp lệ (Cấp Quốc gia, Cấp Tỉnh, Cấp Xã)")
			continue
		}
		org, reason := s.orgOfCell(ctx, a, codes, rec.Get(col, "org"))
		if reason != "" {
			bad(reason)
			continue
		}
		in := domain.AssignmentInput{Role: role, OrgRef: org, Departments: splitList(rec.Get(col, "departments")),
			MgmtLevel: s.mgmtOf(rec.Get(col, "mgmt_level")), Communes: splitList(rec.Get(col, "communes"))}
		if slices.ContainsFunc(g.asg, func(x domain.AssignmentInput) bool { return x.Role == in.Role && x.OrgRef == in.OrgRef }) {
			bad("Trùng vai trò và Nút với dòng khác của cùng Email")
			continue
		}
		g.asg = append(g.asg, in)
	}
	return groups, order
}

// orgOfCell: ô "Mã Nút" là UUID hoặc mã tổ chức (tra qua party OrgCodes nếu bản cài có).
func (s *Service) orgOfCell(ctx context.Context, a *actor, codes partyport.OrgCodes, v string) (uuid.UUID, string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return uuid.Nil, "Thiếu Mã Nút"
	}
	if id, err := uuid.Parse(v); err == nil {
		return id, ""
	}
	if codes == nil {
		return uuid.Nil, "Mã Nút phải là UUID (chưa nối tra mã tổ chức)"
	}
	o, err := codes.ByCode(ctx, v)
	if err != nil {
		return uuid.Nil, "Không có Nút với mã này"
	}
	a.orgs[o.Ref] = &o
	return o.Ref, ""
}

// knReason quy lỗi tạo tài khoản KN về lý do theo dòng, không chứa dữ liệu nhập.
func knReason(err error) string {
	p := problem.From(err)
	switch p.Code {
	case ErrDuplicateIdentity.Code:
		return "Email hoặc SĐT đã dùng cho tài khoản khác"
	case ErrAssignmentDuplicate.Code:
		return "Trùng vai trò và Nút"
	case ErrPrerequisite.Code:
		if p.Detail != "" {
			return "Chưa đủ tiên quyết: " + p.Detail
		}
		return "Chưa đủ tiên quyết (Nút, phòng ban)"
	case ErrDepartmentInvalid.Code:
		return "Phòng ban không thuộc đơn vị của Nút"
	case ErrCommuneInvalid.Code:
		return "Xã phụ trách không thuộc Nút"
	}
	return reasonOf(err)
}
