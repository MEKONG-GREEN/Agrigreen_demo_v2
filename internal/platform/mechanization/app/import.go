package app

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// MaxImportRows giới hạn số dòng xử lý đồng bộ trong một request (R-52: dưới 2 giây).
const MaxImportRows = 500

// Chế độ nhập máy (US-MAC-02).
const (
	ImportUpdate  = "update"  // "Cập nhật": máy vắng mặt trong tệp giữ nguyên
	ImportReplace = "replace" // "Thay thế toàn bộ": máy đang Active của HTX vắng mặt → Inactive (sự kiện, không xóa)
)

// Trạng thái từng dòng nhập.
const (
	RowCreated     = "created"
	RowUpdated     = "updated"
	RowReactivated = "reactivated"
	RowUnchanged   = "unchanged"
	RowRejected    = "rejected"
)

// ReasonAbsent là lý do vô hiệu hóa máy vắng mặt ở chế độ "Thay thế toàn bộ".
const ReasonAbsent = "Không còn trong tệp nhập (chế độ Thay thế toàn bộ)"

// ImportRow là kết quả một dòng. Reason không chứa dữ liệu cá nhân (R-14).
type ImportRow struct {
	Row        int
	Status     string
	Reason     string
	MachineRef uuid.UUID
	Code       string
}

// ImportResult là kết quả cả tệp.
type ImportResult struct {
	Mode                                               string
	Created, Updated, Reactivated, Unchanged, Rejected int
	Rows                                               []ImportRow
	Deactivated                                        []domain.Machine
}

// ImportInput là tệp CSV (UTF-8, dòng đầu là tiêu đề) danh sách máy của MỘT HTX.
type ImportInput struct {
	CooperativeRef uuid.UUID
	Mode           string
	Body           io.Reader
	IdemKey        string
}

// Cột của tệp mẫu (FN-05 "Cấu trúc file Excel nhập"); tên không dấu làm bí danh. Không có cột Mã máy, Khâu.
var importAliases = map[string]string{
	"ma_htx": "cooperative_code", "cooperative_code": "cooperative_code",
	"chung_loai": "machine_type", "ma_chung_loai": "machine_type", "machine_type": "machine_type",
	"hang": "brand", "brand": "brand", "model": "model",
	"nam_sx": "year_made", "nam_san_xuat": "year_made", "year_made": "year_made",
	"nhien_lieu": "fuel", "fuel": "fuel",
	"cong_suat": "power_hp", "power_hp": "power_hp",
	"nang_suat": "capacity_ha", "capacity_ha": "capacity_ha",
	"ma_csh": "owner_code", "chu_so_huu": "owner_code", "owner_code": "owner_code",
	"so_may": "serial_no", "so_may_sn": "serial_no", "serial_no": "serial_no",
	"so_khung": "chassis_no", "chassis_no": "chassis_no",
	"ngay_so_huu": "owned_since", "ngay_htx_so_huu": "owned_since", "owned_since": "owned_since",
	"tinh_trang": "status", "tinh_trang_ban_dau": "status", "status": "status",
}

var importRequired = []string{"machine_type", "brand", "model", "owner_code", "owned_since", "status"}

// forbiddenColumns: tệp có các cột này bị từ chối toàn bộ (mã do hệ thống sinh; khâu tự gán).
var forbiddenColumns = map[string]string{"ma_may": "Mã máy", "machine_code": "Mã máy", "khau": "Khâu sản xuất", "farm_stage": "Khâu sản xuất"}

func headerKey(h string) string {
	h = strings.TrimPrefix(h, string(rune(0xFEFF))) // BOM của Excel
	return strings.ReplaceAll(strings.ReplaceAll(domain.Fold(h), " ", "_"), "/", "_")
}

// ImportMachines nhập danh sách máy của một HTX (FN-05 BR-07/BR-10, US-MAC-02):
// đối chiếu từng dòng theo Số máy/SN (thiếu thì Số khung) — đã có → cập nhật,
// chưa có → tạo mới; chế độ "Thay thế toàn bộ" chuyển máy vắng mặt sang Inactive
// bằng sự kiện kèm Ngày vô hiệu hóa. Mỗi dòng hợp lệ ghi một dòng sổ riêng; chỉ
// ghi dòng hợp lệ, dòng lỗi liệt kê kèm lý do.
func (s *Service) ImportMachines(ctx context.Context, in ImportInput) (ImportResult, error) {
	p, sc, err := s.caller(ctx, writeRoles)
	if err != nil {
		return ImportResult{}, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return ImportResult{}, err
	}
	if in.Mode != ImportUpdate && in.Mode != ImportReplace {
		return ImportResult{}, problem.Validation.WithField("mode", "chọn một trong hai chế độ: update (Cập nhật) hoặc replace (Thay thế toàn bộ)")
	}
	ci, err := s.activeCoop(ctx, sc, in.CooperativeRef, "cooperative_ref")
	if err != nil {
		return ImportResult{}, err
	}
	org, err := s.Orgs.Get(ctx, ci.Ref)
	if err != nil {
		return ImportResult{}, ErrDependency.WithCause(err)
	}
	rd := csv.NewReader(in.Body)
	rd.FieldsPerRecord = -1
	rd.TrimLeadingSpace = true
	header, err := rd.Read()
	if err != nil {
		return ImportResult{}, ErrImportInvalid
	}
	col := map[string]int{}
	for i, h := range header {
		k := headerKey(h)
		if label, bad := forbiddenColumns[k]; bad {
			return ImportResult{}, ErrImportInvalid.WithDetail("Tệp không đúng mẫu quy định: không được có cột \"" + label + "\" (hệ thống tự sinh/tự gán)")
		}
		if canon, ok := importAliases[k]; ok {
			col[canon] = i
		}
	}
	var missing []string
	for _, c := range importRequired {
		if _, ok := col[c]; !ok {
			missing = append(missing, c)
		}
	}
	_, hasSerial := col["serial_no"]
	_, hasChassis := col["chassis_no"]
	if !hasSerial && !hasChassis {
		missing = append(missing, "serial_no|chassis_no")
	}
	if len(missing) > 0 {
		return ImportResult{}, ErrImportInvalid.WithDetail("Thiếu cột bắt buộc: " + strings.Join(missing, ", "))
	}
	records, err := rd.ReadAll()
	if err != nil {
		return ImportResult{}, ErrImportInvalid.WithDetail("Sai định dạng tệp CSV")
	}
	if len(records) > MaxImportRows {
		return ImportResult{}, ErrImportInvalid.WithDetail(fmt.Sprintf("Tối đa %d dòng mỗi lần nhập", MaxImportRows))
	}

	res := ImportResult{Mode: in.Mode, Rows: []ImportRow{}, Deactivated: []domain.Machine{}}
	present := map[string]bool{} // khóa định danh xuất hiện trong tệp (kể cả dòng lỗi) — không bị vô hiệu hóa
	seen := map[string]int{}
	stages := map[string]string{}
	for i, rec := range records {
		rowNo := i + 2 // dòng 1 là tiêu đề
		get := func(k string) string {
			if j, ok := col[k]; ok && j < len(rec) {
				return strings.TrimSpace(rec[j])
			}
			return ""
		}
		serial, chassis := domain.NormalizeIdent(get("serial_no")), domain.NormalizeIdent(get("chassis_no"))
		for _, k := range []string{"sn:" + serial, "ck:" + chassis} {
			if len(k) > 3 {
				present[k] = true
			}
		}
		row := ImportRow{Row: rowNo}
		reject := func(reason string) {
			row.Status, row.Reason = RowRejected, "Dòng "+strconv.Itoa(rowNo)+": "+reason
			res.Rejected++
			res.Rows = append(res.Rows, row)
		}
		key := "sn:" + serial
		if serial == "" {
			key = "ck:" + chassis
		}
		if serial == "" && chassis == "" {
			reject("Thiếu Số máy/SN và Số khung")
			continue
		}
		if prev, dup := seen[key]; dup {
			reject(fmt.Sprintf("Số máy/SN trùng với dòng %d trong cùng file", prev))
			continue
		}
		seen[key] = rowNo
		if code := get("cooperative_code"); code != "" && !strings.EqualFold(code, org.Code) && domain.Fold(code) != domain.Fold(org.Name) {
			reject("HTX \"" + code + "\" không khớp HTX của tệp nhập")
			continue
		}
		m, reason := s.parseImportRow(ctx, get, stages)
		if reason != "" {
			reject(reason)
			continue
		}
		m.SerialNo, m.ChassisNo, m.CooperativeRef = serial, chassis, ci.Ref
		out, err := s.importRow(ctx, p, m, get("owner_code"), ci.ProvinceLabel, in.IdemKey+":r:"+key)
		if err != nil {
			var pr *problem.Problem
			if !errors.As(err, &pr) || pr.Status >= 500 {
				return ImportResult{}, err // lỗi hệ thống: dừng, gọi lại an toàn nhờ idem_key theo dòng
			}
			reject(problemReason(pr))
			continue
		}
		row.Status, row.MachineRef, row.Code = out.status, out.machine.Ref, out.machine.Code
		switch out.status {
		case RowCreated:
			res.Created++
		case RowUpdated:
			res.Updated++
		case RowReactivated:
			res.Reactivated++
		default:
			res.Unchanged++
		}
		res.Rows = append(res.Rows, row)
	}

	if in.Mode == ImportReplace {
		today, now := s.today(), s.now()
		err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			ms, err := store.ActiveMachines(ctx, tx, ci.Ref, uuid.Nil)
			if err != nil {
				return err
			}
			for _, m := range ms {
				if (m.SerialNo != "" && present["sn:"+m.SerialNo]) || (m.ChassisNo != "" && present["ck:"+m.ChassisNo]) {
					continue
				}
				on := today
				if on.Before(m.OwnedSince) {
					on = m.OwnedSince
				}
				key := in.IdemKey + ":d:" + m.Ref.String()
				if prev, err := store.MachineEventByIdem(ctx, tx, key); err != nil {
					return err
				} else if prev != nil {
					continue
				}
				r, err := deactivateMachineTx(ctx, tx, p, m, on, ReasonAbsent, domain.SourceImport, now, key)
				if err != nil {
					return err
				}
				res.Deactivated = append(res.Deactivated, r.Machine)
			}
			return nil
		})
		if err != nil {
			return ImportResult{}, classify(err)
		}
	}
	return res, nil
}

// problemReason rút lý do ngắn từ problem (không có dữ liệu cá nhân).
func problemReason(pr *problem.Problem) string {
	if len(pr.Errors) > 0 {
		return pr.Errors[0].Reason
	}
	if pr.Detail != "" {
		return pr.Detail
	}
	return pr.Title
}

// parseDate nhận dd/mm/yyyy (mẫu BRD) hoặc yyyy-mm-dd.
func parseDate(s string) (time.Time, bool) {
	for _, layout := range []string{"02/01/2006", "2/1/2006", time.DateOnly} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func optDecimal(s string) (*decimal.Decimal, bool) {
	if s == "" {
		return nil, true
	}
	d, err := decimal.NewFromString(strings.ReplaceAll(s, ",", "."))
	if err != nil || d.IsNegative() {
		return nil, false
	}
	return &d, true
}

// parseImportRow đọc một dòng thành hồ sơ máy (chưa có HTX, định danh). Trả lý do nếu lỗi.
func (s *Service) parseImportRow(ctx context.Context, get func(string) string, stages map[string]string) (domain.Machine, string) {
	m := domain.Machine{
		MachineType: get("machine_type"), Brand: get("brand"), Model: get("model"), Fuel: get("fuel"), StatusSource: domain.SourceImport,
	}
	for _, f := range []struct{ key, label string }{{"machine_type", "Chủng loại"}, {"brand", "Hãng"}, {"model", "Model"}, {"owner_code", "Chủ sở hữu"}} {
		if get(f.key) == "" {
			return m, "Thiếu " + f.label
		}
	}
	stage, ok := stages[m.MachineType]
	if !ok {
		st, err := s.machineType(ctx, m.MachineType)
		if err != nil {
			return m, "Chủng loại \"" + m.MachineType + "\" không khớp danh mục"
		}
		stages[m.MachineType], stage = st, st
	}
	m.FarmStage = stage
	owned, ok := parseDate(get("owned_since"))
	if !ok {
		return m, "Ngày HTX sở hữu phải có dạng dd/mm/yyyy"
	}
	m.OwnedSince = owned
	st, ok := domain.ParseStatus(get("status"))
	if !ok {
		return m, "Tình trạng \"" + get("status") + "\" không hợp lệ (Hoạt động / Bảo trì / Hỏng / Ngừng hoạt động)"
	}
	m.Status = st
	if y := get("year_made"); y != "" {
		n, err := strconv.Atoi(y)
		if err != nil {
			return m, "Năm sản xuất phải là số nguyên"
		}
		m.YearMade = &n
	}
	if m.PowerHP, ok = optDecimal(get("power_hp")); !ok {
		return m, "Công suất phải là số không âm"
	}
	if m.CapacityHa, ok = optDecimal(get("capacity_ha")); !ok {
		return m, "Năng suất phải là số không âm"
	}
	if err := domain.ValidateMachine(domain.Machine{
		SerialNo: "x", OwnedSince: m.OwnedSince, YearMade: m.YearMade, PowerHP: m.PowerHP, CapacityHa: m.CapacityHa, Status: m.Status,
	}, s.today()); err != nil {
		var fe domain.FieldError
		if errors.As(err, &fe) {
			return m, fe.Err.Error()
		}
		return m, err.Error()
	}
	return m, ""
}

type importOutcome struct {
	status  string
	machine domain.Machine
}

// importRow ghi một dòng hợp lệ trong giao dịch riêng (một dòng lỗi không làm hỏng cả tệp).
func (s *Service) importRow(ctx context.Context, p authz.Principal, m domain.Machine, ownerCode, provinceLabel, key string) (importOutcome, error) {
	var out importOutcome
	now := s.now()
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.MachineEventByIdem(ctx, tx, key); err != nil {
			return err
		} else if prev != nil {
			out = importOutcome{status: RowUnchanged, machine: prev.Machine}
			return nil
		}
		o, err := store.OwnerByCode(ctx, tx, strings.ToUpper(ownerCode))
		if err != nil {
			return err
		}
		if o == nil || !o.Active {
			return problem.Validation.WithField("owner_code", "Chủ sở hữu \""+ownerCode+"\" không tồn tại ở Hồ sơ chủ sở hữu")
		}
		m.OwnerID = o.ID
		var cur *domain.Machine
		if m.SerialNo != "" {
			cur, err = store.MachineBySerial(ctx, tx, m.SerialNo)
		} else {
			cur, err = store.MachineByChassis(ctx, tx, m.ChassisNo)
		}
		if err != nil {
			return err
		}
		if cur == nil {
			m.Ref, m.Active, m.StatusAt = ids.New(), true, now
			r, err := s.insertMachineTx(ctx, tx, p, m, provinceLabel, domain.SourceImport, key, now)
			out = importOutcome{status: RowCreated, machine: r.Machine}
			return err
		}
		if cur.CooperativeRef != m.CooperativeRef {
			return s.takenProblem(ctx, ErrSerialTaken, "serial_no", "Số máy/SN này đã tồn tại trên hệ thống", *cur)
		}
		next := *cur
		next.MachineType, next.FarmStage, next.OwnerID = m.MachineType, m.FarmStage, m.OwnerID
		next.Brand, next.Model, next.Fuel, next.YearMade, next.PowerHP, next.CapacityHa = m.Brand, m.Model, m.Fuel, m.YearMade, m.PowerHP, m.CapacityHa
		next.OwnedSince = m.OwnedSince
		if m.SerialNo != "" {
			next.SerialNo = m.SerialNo
		}
		if m.ChassisNo != "" {
			next.ChassisNo = m.ChassisNo
		}
		if err := s.identTaken(ctx, tx, next.SerialNo, next.ChassisNo, cur.Ref); err != nil {
			return err
		}
		statusChanged := next.Status != m.Status
		if statusChanged {
			next.Status, next.StatusSource, next.StatusAt = m.Status, domain.SourceImport, now
		}
		action, status := domain.MachineUpdated, RowUpdated
		switch {
		case !cur.Active:
			next.Active, next.DeactivatedOn, next.StatusReason = true, nil, ""
			action, status = domain.MachineReactivated, RowReactivated
		case sameMachine(*cur, next) && !statusChanged:
			out = importOutcome{status: RowUnchanged, machine: *cur}
			return nil
		case statusChanged && sameMachine(*cur, next):
			action = domain.MachineStatusChanged
		}
		if next.DeactivatedOn != nil && next.DeactivatedOn.Before(next.OwnedSince) {
			return problem.Validation.WithField("owned_since", domain.ErrDeactBefore.Error())
		}
		r, err := writeMachineTx(ctx, tx, p, next, action, domain.SourceImport, "", now, key)
		out = importOutcome{status: status, machine: r.Machine}
		return err
	})
	return out, classify(err)
}
