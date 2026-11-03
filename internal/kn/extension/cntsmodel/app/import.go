package app

// W11.53c (#176, BRD CNTS v3.7 FN-19 luồng phụ "Nạp Excel điểm, hộ"): chủ nhiệm (lãnh đạo phòng, Admin) tải tệp mẫu, điền
// điểm trình diễn và hộ tham gia, nạp lên để XEM TRƯỚC (không ghi gì) rồi mới GHI.
//   - Tệp .xlsx ≤ 2 MB, ≤ 200 điểm, ≤ 2000 hộ (giới hạn giải nén); sai bố cục → từ chối cả tệp; lỗi theo dòng/cột/lý do.
//   - Ghi chỉ khi tệp không còn lỗi nào; mọi điểm, hộ ghi trong MỘT giao dịch (không ghi nửa chừng). Kiểm lại trong giao
//     dịch: dự án còn mở, người gọi còn quyền, điểm/hộ trùng với dữ liệu đã có.
//   - Kiểm trùng: mã điểm, tên điểm cùng xã, chủ hộ (SĐT) cùng điểm — trong tệp và với dữ liệu đã có; xã ngoài tỉnh của dự án
//     hay ngoài phạm vi người gọi bị chặn.
//   - Chủ hộ vào kho định danh (vault) NGOÀI giao dịch DB; sổ KN chỉ giữ person_ref. Phản hồi (được lưu 48 giờ ở bảng
//     idempotency) chỉ có nhãn che bớt — không lưu họ tên/SĐT.
//   - Phát lại theo Idempotency-Key: mỗi dòng ghi với khóa idem suy từ khóa thao tác ("csite:imp:<khóa>:<dòng>"), nạp lại
//     cùng khóa → trả kết quả cũ, không tạo bản sao; nạp lại cùng tệp bằng khóa khác → báo trùng điểm/hộ đã có.
//   - Port module khác (đơn vị hành chính, danh mục, danh bạ Cấp Xã, tên cán bộ, kho định danh) đọc NGOÀI giao dịch.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	exdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// ErrImportInvalid: tệp nạp không đọc được, sai bố cục, hoặc còn lỗi theo dòng (kèm danh sách issues).
var ErrImportInvalid = problem.New(http.StatusUnprocessableEntity, "extension.cnts_import_invalid", "Tệp nạp điểm trình diễn, hộ tham gia không hợp lệ")

const (
	importUnzipBytes = 32 << 20
	importUnzipXML   = 16 << 20
	importScanRows   = 100000 // dòng (kể cả dòng trống có định dạng) tối đa duyệt mỗi sheet
	importMaxIssues  = 500    // lỗi trả về tối đa
	maxGuideCommunes = 500
)

// ImportSite là một dòng điểm sẽ tạo.
type ImportSite struct {
	domain.SiteRow
	OfficerName   string
	ModelTypeName string
}

// ImportHousehold là một dòng hộ sẽ tạo (không có họ tên/SĐT đầy đủ — chỉ nhãn che bớt).
type ImportHousehold struct {
	Row         int
	SiteKey     string // mã điểm trong tệp hoặc mã điểm đã có
	SiteName    string
	CommuneCode string
	OwnerLabel  string
	PhoneMasked string
	domain.Household

	owner   domain.Owner
	siteRef *uuid.UUID // điểm đã có
	siteIdx int        // chỉ số điểm trong tệp (khi siteRef nil)
}

// ImportPlan là kết quả xem trước hoặc ghi.
type ImportPlan struct {
	ProjectRef  uuid.UUID
	ProjectCode string
	Sites       []ImportSite
	Households  []ImportHousehold
	Issues      []domain.ImportIssue
	Truncated   bool // còn lỗi ngoài importMaxIssues lỗi đầu
	Committed   bool
	Replayed    bool
	SiteRefs    []uuid.UUID // điểm đã tạo (lần ghi đầu)
}

// Valid: không còn lỗi và có ít nhất một dòng.
func (p ImportPlan) Valid() bool { return len(p.Issues) == 0 && len(p.Sites)+len(p.Households) > 0 }

func (p *ImportPlan) issue(sheet string, row int, col, msg string) {
	p.Issues = append(p.Issues, domain.ImportIssue{Sheet: sheet, Row: row, Column: col, Message: msg})
}

func idemImportSite(k string, row int) string { return "csite:imp:" + k + ":" + strconv.Itoa(row) }
func idemImportHH(k string, row int) string   { return "chh:imp:" + k + ":" + strconv.Itoa(row) }

// importFile là hai bảng đã đọc thô.
type importFile struct {
	sites, households [][]string
}

// readImportXLSX mở tệp (≤ 2 MB, giới hạn giải nén) và đọc hai sheet mẫu (theo tên, không phân biệt hoa thường).
func readImportXLSX(data []byte) (importFile, error) {
	switch {
	case len(data) == 0:
		return importFile{}, ErrImportInvalid.WithDetail("Tệp rỗng")
	case len(data) > domain.ImportMaxBytes:
		return importFile{}, ErrImportInvalid.WithDetail("Tệp vượt 2 MB")
	case len(data) >= 2 && data[0] == 0xd0 && data[1] == 0xcf:
		return importFile{}, ErrImportInvalid.WithDetail("Tệp .xls cũ — lưu lại dạng .xlsx")
	case len(data) < 2 || data[0] != 'P' || data[1] != 'K':
		return importFile{}, ErrImportInvalid.WithDetail("Chỉ nhận tệp Excel .xlsx (dùng tệp mẫu)")
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: importUnzipBytes, UnzipXMLSizeLimit: importUnzipXML})
	if err != nil {
		return importFile{}, ErrImportInvalid.WithDetail("Không mở được tệp .xlsx")
	}
	defer func() { _ = f.Close() }()
	var out importFile
	found := 0
	for _, name := range f.GetSheetList() {
		var dst *[][]string
		var max int
		switch domain.Fold(name) {
		case domain.Fold(domain.SheetSites):
			dst, max = &out.sites, domain.ImportMaxSites
		case domain.Fold(domain.SheetHouseholds):
			dst, max = &out.households, domain.ImportMaxHouseholds
		default:
			continue
		}
		if *dst != nil {
			return importFile{}, ErrImportInvalid.WithDetail(fmt.Sprintf("Tệp có hai sheet \"%s\"", name))
		}
		rows, err := importRows(f, name, max)
		if err != nil {
			return importFile{}, err
		}
		*dst = rows
		found++
	}
	if found == 0 {
		return importFile{}, ErrImportInvalid.WithDetail(fmt.Sprintf("Không thấy sheet \"%s\" hoặc \"%s\" — dùng tệp mẫu", domain.SheetSites, domain.SheetHouseholds))
	}
	return out, nil
}

// importRows đọc giá trị thô (không theo định dạng số: SĐT, ngày) tối đa ImportMaxCols cột; dòng trống giữ chỗ (nil) để
// số dòng báo lỗi đúng dòng Excel. Ngừng giữ nội dung khi đã quá max dòng có chữ (domain báo quá số dòng).
func importRows(f *excelize.File, name string, max int) ([][]string, error) {
	it, err := f.Rows(name)
	if err != nil {
		return nil, ErrImportInvalid.WithDetail(fmt.Sprintf("Không đọc được sheet \"%s\"", name))
	}
	defer func() { _ = it.Close() }()
	var out [][]string
	content := 0
	for it.Next() {
		if len(out) >= importScanRows {
			return nil, ErrImportInvalid.WithDetail(fmt.Sprintf("Sheet \"%s\" quá %d dòng", name, importScanRows))
		}
		cols, err := it.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, ErrImportInvalid.WithDetail(fmt.Sprintf("Không đọc được sheet \"%s\"", name))
		}
		if !slices.ContainsFunc(cols, func(s string) bool { return strings.TrimSpace(s) != "" }) {
			out = append(out, nil)
			continue
		}
		content++
		if content > max+domain.ImportHeaderScan+1 {
			out = append(out, nil)
			continue
		}
		if len(cols) > domain.ImportMaxCols {
			cols = cols[:domain.ImportMaxCols]
		}
		out = append(out, cols)
	}
	if err := it.Error(); err != nil {
		return nil, ErrImportInvalid.WithDetail(fmt.Sprintf("Không đọc được sheet \"%s\"", name))
	}
	if content > max+domain.ImportHeaderScan+1 {
		return nil, ErrImportInvalid.WithDetail(fmt.Sprintf("Sheet \"%s\" quá %d dòng — tách thành nhiều tệp", name, max))
	}
	return out, nil
}

// parseImport đọc tệp thành các dòng (quy tắc thuần) + lỗi theo ô.
func parseImport(data []byte) ([]domain.SiteRow, []domain.HouseholdRow, []domain.ImportIssue, error) {
	f, err := readImportXLSX(data)
	if err != nil {
		return nil, nil, nil, err
	}
	var sites []domain.SiteRow
	var hhs []domain.HouseholdRow
	var issues []domain.ImportIssue
	if f.sites != nil {
		var is []domain.ImportIssue
		if sites, is, err = domain.ParseSites(f.sites); err != nil {
			return nil, nil, nil, ErrImportInvalid.WithDetail(strings.TrimPrefix(err.Error(), domain.ErrImportLayout.Error()+": "))
		}
		issues = append(issues, is...)
	}
	if f.households != nil {
		var is []domain.ImportIssue
		if hhs, is, err = domain.ParseHouseholds(f.households); err != nil {
			return nil, nil, nil, ErrImportInvalid.WithDetail(strings.TrimPrefix(err.Error(), domain.ErrImportLayout.Error()+": "))
		}
		issues = append(issues, is...)
	}
	return sites, hhs, issues, nil
}

// importProject đọc dự án, điểm, hộ hiện hành và kiểm quyền nạp (người sửa được dự án, dự án đang thực hiện).
func (s *Service) importProject(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID) (*store.Project, []store.Site, []store.Household, error) {
	p, err := store.LatestProject(ctx, tx, ref)
	if err != nil {
		return nil, nil, nil, err
	}
	if p == nil || !c.seesProject(*p, nil) {
		return nil, nil, nil, ErrProjectNotFound
	}
	if !c.canEditProject(*p) {
		return nil, nil, nil, problem.Forbidden.WithDetail("chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin nạp điểm, hộ")
	}
	if p.Event == domain.EventClosed {
		return nil, nil, nil, ErrProjectClosed
	}
	sites, err := store.ListSites(ctx, tx, store.SiteFilter{Project: &p.Ref})
	if err != nil {
		return nil, nil, nil, err
	}
	hhs, err := store.ListHouseholds(ctx, tx, store.HouseholdFilter{Project: &p.Ref})
	if err != nil {
		return nil, nil, nil, err
	}
	return p, sites, hhs, nil
}

// siteNameKey là khóa trùng điểm: xã + tên đã chuẩn hóa.
func siteNameKey(commune, name string) string { return commune + "\x00" + domain.Fold(name) }

// plan kiểm các dòng đã đọc với dự án, danh mục, đơn vị hành chính, danh bạ cán bộ (port đọc ngoài giao dịch qua txdeps).
func (s *Service) plan(ctx context.Context, c caller, p store.Project, existing []store.Site, sites []domain.SiteRow,
	hhs []domain.HouseholdRow, issues []domain.ImportIssue) (*ImportPlan, error) {
	if s.Catalogs == nil {
		return nil, ErrDependency
	}
	pl := &ImportPlan{ProjectRef: p.Ref, ProjectCode: p.Code, Issues: issues}
	taken := map[string]bool{}
	byRef := map[uuid.UUID]store.Site{}
	for _, x := range existing {
		byRef[x.Ref] = x
		if x.Event != domain.EventClosed {
			taken[siteNameKey(x.CommuneCode, x.Name)] = true
		}
	}
	day := s.today()
	types := s.catalogNames(ctx, knport.CatalogCNTSModelType)
	// Cán bộ theo dõi: ghép theo tên hiển thị trong số cán bộ Cấp Xã CNTS phụ trách xã của điểm.
	var officers map[uuid.UUID][]string
	var officerNames map[uuid.UUID]string
	if slices.ContainsFunc(sites, func(x domain.SiteRow) bool { return x.Officer != "" }) {
		idx, err := s.officers(ctx, c)
		if err != nil {
			return nil, err
		}
		officers = map[uuid.UUID][]string{}
		refs := make([]uuid.UUID, 0, len(idx))
		for r, x := range idx {
			officers[r], refs = x.Communes, append(refs, r)
		}
		officerNames = s.names(ctx, refs)
	}
	codeIdx := map[string]int{}
	for _, x := range sites {
		bad := false
		add := func(col, msg string) { pl.issue(domain.SheetSites, x.Row, col, msg); bad = true }
		prov, err := s.provinceOf(ctx, x.CommuneCode)
		switch {
		case err != nil:
			return nil, err
		case prov == "":
			add("C", "Mã xã không phải xã/phường còn hiệu lực")
		case prov != p.Province || !c.sees(x.CommuneCode):
			add("C", "Xã "+x.CommuneCode+" ngoài tỉnh của dự án hoặc ngoài phạm vi phụ trách (FN-14)")
		}
		it, err := txdeps.Get(ctx, "item:"+knport.CatalogCNTSModelType+":"+x.ModelType+"@"+day.Format(time.DateOnly),
			func(ctx context.Context) (mdport.CatalogItem, error) {
				return s.Catalogs.ItemAt(ctx, knport.CatalogCNTSModelType, x.ModelType, day)
			})
		switch {
		case errors.Is(err, mdport.ErrCatalogItemNotFound), errors.Is(err, mdport.ErrNotEffective):
			add("D", "Mã loại mô hình không có trong danh mục loại mô hình CNTS")
		case err != nil:
			return nil, err
		case !it.Active:
			add("D", "Loại mô hình đã ngừng dùng")
		}
		if exdomain.ContainsPII(x.Name) {
			add("B", "Tên điểm: "+exdomain.ErrPIIInText.Error())
		}
		if exdomain.ContainsPII(x.Note) {
			add("I", "Ghi chú: "+exdomain.ErrPIIInText.Error())
		}
		if taken[siteNameKey(x.CommuneCode, x.Name)] {
			add("B", "Điểm cùng tên, cùng xã đã có trong dự án (tệp đã nạp trước đó?)")
		}
		v := ImportSite{SiteRow: x, ModelTypeName: types[x.ModelType]}
		if x.Officer != "" {
			var match []uuid.UUID
			for r, cs := range officers {
				if slices.Contains(cs, x.CommuneCode) && domain.Fold(officerNames[r]) == domain.Fold(x.Officer) {
					match = append(match, r)
				}
			}
			switch len(match) {
			case 0:
				add("H", "Không có cán bộ Cấp Xã phòng Chăn nuôi – Thủy sản tên này phụ trách xã của điểm")
			case 1:
				v.OfficerRef, v.OfficerName = &match[0], officerNames[match[0]]
			default:
				add("H", "Nhiều cán bộ cùng tên phụ trách xã — để trống, giao cán bộ trên màn sau khi nạp")
			}
		}
		if !bad {
			codeIdx[x.Code] = len(pl.Sites)
			pl.Sites = append(pl.Sites, v)
		} else {
			codeIdx[x.Code] = -1
		}
	}
	for _, h := range hhs {
		bad := false
		add := func(col, msg string) { pl.issue(domain.SheetHouseholds, h.Row, col, msg); bad = true }
		v := ImportHousehold{Row: h.Row, SiteKey: h.SiteKey(), OwnerLabel: domain.MaskName(h.Owner.FullName),
			PhoneMasked: domain.MaskPhone(h.PhoneKey), Household: h.Household, owner: h.Owner, siteIdx: -1}
		var officer *uuid.UUID
		switch {
		case h.SiteRef != nil:
			x, ok := byRef[*h.SiteRef]
			switch {
			case !ok:
				add("A", "Không có điểm trình diễn mã này trong dự án")
			case x.Event == domain.EventClosed:
				add("A", "Điểm trình diễn đã đóng")
			default:
				r := x.Ref
				v.siteRef, v.SiteName, v.CommuneCode, officer = &r, x.Name, x.CommuneCode, x.OfficerRef
			}
		default:
			i, ok := codeIdx[h.SiteCode]
			switch {
			case !ok:
				add("A", "Mã điểm không có trong sheet \""+domain.SheetSites+"\" (hoặc dòng điểm của mã này đang lỗi)")
			case i < 0:
				add("A", "Dòng điểm của mã này đang lỗi — sửa dòng điểm trước")
			default:
				x := pl.Sites[i]
				v.siteIdx, v.SiteName, v.CommuneCode, officer = i, x.Name, x.CommuneCode, x.OfficerRef
			}
		}
		if exdomain.ContainsPII(h.Note) {
			add("H", "Ghi chú: "+exdomain.ErrPIIInText.Error())
		}
		if !bad && v.KeeperKind == domain.KeeperOfficer {
			if officer == nil {
				add("F", "Điểm chưa có cán bộ theo dõi — ghi \""+domain.KeeperTextSelf+"\" hoặc giao cán bộ cho điểm trước")
			} else {
				r := *officer
				v.KeeperRef = &r
			}
		}
		if !bad {
			if es := v.Normalize(); len(es) > 0 {
				add("", es.Error())
			}
		}
		if !bad {
			pl.Households = append(pl.Households, v)
		}
	}
	if len(sites)+len(hhs)+len(pl.Issues) == 0 {
		pl.issue("", 0, "", "Tệp không có dòng điểm hay hộ nào")
	}
	sortIssues(pl)
	return pl, nil
}

// sortIssues xếp lỗi theo sheet (điểm trước), dòng; cắt importMaxIssues lỗi đầu.
func sortIssues(pl *ImportPlan) {
	rank := func(sheet string) int {
		switch sheet {
		case domain.SheetSites:
			return 1
		case domain.SheetHouseholds:
			return 2
		}
		return 0
	}
	slices.SortStableFunc(pl.Issues, func(a, b domain.ImportIssue) int {
		if d := rank(a.Sheet) - rank(b.Sheet); d != 0 {
			return d
		}
		return a.Row - b.Row
	})
	if len(pl.Issues) > importMaxIssues {
		pl.Issues, pl.Truncated = pl.Issues[:importMaxIssues], true
	}
}

// ImportInput là một lần nạp tệp.
type ImportInput struct {
	ProjectRef uuid.UUID
	Data       []byte
	IdemKey    string
}

func (s *Service) prepareImport(ctx context.Context, c caller, in ImportInput) (*ImportPlan, error) {
	sites, hhs, issues, err := parseImport(in.Data)
	if err != nil {
		return nil, err
	}
	var p *store.Project
	var existing []store.Site
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		p, existing, _, e = s.importProject(ctx, tx, c, in.ProjectRef)
		return e
	}); err != nil {
		return nil, err
	}
	return s.plan(ctx, c, *p, existing, sites, hhs, issues)
}

// PreviewImport đọc và kiểm tệp — KHÔNG ghi gì (không gọi kho định danh).
func (s *Service) PreviewImport(ctx context.Context, in ImportInput) (*ImportPlan, error) {
	ctx = txdeps.With(ctx)
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return nil, err
	}
	return s.prepareImport(ctx, c, in)
}

// errImportRace: kiểm lại trong giao dịch thấy trùng (dữ liệu đổi giữa lúc kiểm và lúc ghi) — lỗi ở plan.Issues.
var errImportRace = errors.New("import: trùng dữ liệu khi ghi")

// replayedImport cho biết lần nạp với khóa k đã ghi vào dự án project (dòng đầu tiên mang khóa idem suy từ k).
func replayedImport(ctx context.Context, tx pgx.Tx, k string, project uuid.UUID, pl *ImportPlan) (bool, error) {
	if len(pl.Sites) > 0 {
		prev, err := store.SiteByIdem(ctx, tx, idemImportSite(k, pl.Sites[0].Row))
		return prev != nil && prev.ProjectRef == project, err
	}
	if len(pl.Households) > 0 {
		prev, err := store.HouseholdByIdem(ctx, tx, idemImportHH(k, pl.Households[0].Row))
		return prev != nil && prev.ProjectRef == project, err
	}
	return false, nil
}

// CommitImport kiểm lại tệp và ghi mọi điểm, hộ trong một giao dịch. Tệp còn lỗi → plan.Valid() = false, không ghi gì.
func (s *Service) CommitImport(ctx context.Context, in ImportInput) (*ImportPlan, error) {
	ctx = txdeps.With(ctx)
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return nil, err
	}
	sites, hhs, issues, err := parseImport(in.Data)
	if err != nil {
		return nil, err
	}
	// Phát lại trước mọi kiểm "đã có" (lần ghi trước đã tạo chính các điểm/hộ này).
	probe := &ImportPlan{}
	for _, x := range sites {
		probe.Sites = append(probe.Sites, ImportSite{SiteRow: x})
	}
	for _, h := range hhs {
		probe.Households = append(probe.Households, ImportHousehold{Row: h.Row})
	}
	var p *store.Project
	var existing []store.Site
	replayed := false
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		// Quyền, dự án kiểm trước cả khi phát lại (người khác/dự án đã kết thúc không nhận lại kết quả).
		if p, existing, _, e = s.importProject(ctx, tx, c, in.ProjectRef); e != nil {
			return e
		}
		if len(issues) == 0 {
			replayed, e = replayedImport(ctx, tx, in.IdemKey, in.ProjectRef, probe)
		}
		return e
	}); err != nil {
		return nil, err
	}
	if replayed {
		probe.ProjectRef, probe.Committed, probe.Replayed = in.ProjectRef, true, true
		return probe, nil
	}
	pl, err := s.plan(ctx, c, *p, existing, sites, hhs, issues)
	if err != nil || !pl.Valid() {
		return pl, err
	}
	// Chủ hộ vào kho định danh NGOÀI giao dịch (trùng SĐT → chủ thể cũ). Lỗi kiểm của kho → lỗi dòng, không ghi gì.
	persons := make([]uuid.UUID, len(pl.Households))
	if len(pl.Households) > 0 && s.Vault == nil {
		return nil, ErrDependency
	}
	for i, h := range pl.Households {
		cr, err := s.Vault.Create(ctx, client.PurposeContact, client.Identity{Kind: client.SubjectHousehold,
			FullName: h.owner.FullName, Phone: h.owner.Phone, AreaCode: h.CommuneCode})
		var pb *problem.Problem
		switch {
		case errors.Is(err, client.ErrDuplicate) && cr.Ref != uuid.Nil, err == nil:
			persons[i] = cr.Ref
		case errors.As(err, &pb) && pb.Status == http.StatusUnprocessableEntity:
			pl.issue(domain.SheetHouseholds, h.Row, "", "Kho định danh từ chối họ tên/SĐT chủ hộ")
		default:
			return nil, err
		}
	}
	if !pl.Valid() {
		sortIssues(pl)
		return pl, nil
	}
	refs := make([]uuid.UUID, len(pl.Sites))
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		pl.Issues, replayed = nil, false
		var e error
		if replayed, e = replayedImport(ctx, tx, in.IdemKey, in.ProjectRef, pl); e != nil || replayed {
			return e
		}
		if e = store.Lock(ctx, tx, "proj:"+in.ProjectRef.String()); e != nil {
			return e
		}
		cur, cs, chs, e := s.importProject(ctx, tx, c, in.ProjectRef)
		if e != nil {
			return e
		}
		taken := map[string]bool{}
		for _, x := range cs {
			if x.Event != domain.EventClosed {
				taken[siteNameKey(x.CommuneCode, x.Name)] = true
			}
		}
		open := map[uuid.UUID]bool{}
		for _, x := range cs {
			open[x.Ref] = x.Event != domain.EventClosed
		}
		member := map[[2]uuid.UUID]bool{}
		for _, h := range chs {
			if h.Event != domain.EventWithdrawn {
				member[[2]uuid.UUID{h.SiteRef, h.PersonRef}] = true
			}
		}
		for _, x := range pl.Sites {
			if taken[siteNameKey(x.CommuneCode, x.Name)] {
				pl.issue(domain.SheetSites, x.Row, "B", "Điểm cùng tên, cùng xã đã có trong dự án")
			}
		}
		for i, h := range pl.Households {
			switch {
			case h.siteRef != nil && !open[*h.siteRef]:
				pl.issue(domain.SheetHouseholds, h.Row, "A", "Điểm trình diễn đã đóng")
			case h.siteRef != nil && member[[2]uuid.UUID{*h.siteRef, persons[i]}]:
				pl.issue(domain.SheetHouseholds, h.Row, "C", "Chủ hộ (cùng SĐT) đã là hộ đang tham gia của điểm")
			}
		}
		if len(pl.Issues) > 0 {
			return errImportRace
		}
		at := s.now()
		for i, x := range pl.Sites {
			refs[i] = ids.New()
			row := store.Site{Meta: c.meta(at, idemImportSite(in.IdemKey, x.Row)), Ref: refs[i], Event: domain.EventCreated,
				ProjectRef: cur.Ref, Site: x.Site}
			row.OfficerRef = x.OfficerRef
			if e := store.InsertSite(ctx, tx, c.p.TenantID, &row); e != nil {
				return e
			}
		}
		for i, h := range pl.Households {
			var site uuid.UUID
			if h.siteRef != nil {
				site = *h.siteRef
			} else {
				site = refs[h.siteIdx] // plan: hộ không có siteRef luôn trỏ một điểm hợp lệ của tệp
			}
			row := store.Household{Meta: c.meta(at, idemImportHH(in.IdemKey, h.Row)), Ref: ids.New(), Event: domain.EventCreated,
				SiteRef: site, ProjectRef: cur.Ref, CommuneCode: h.CommuneCode, PersonRef: persons[i], Household: h.Household}
			if e := store.InsertHousehold(ctx, tx, c.p.TenantID, &row); e != nil {
				return e
			}
		}
		return nil
	})
	switch {
	case errors.Is(err, errImportRace):
		sortIssues(pl)
		return pl, nil
	case dup(err):
		replayed = true
	case err != nil:
		return nil, err
	}
	pl.Committed, pl.Replayed = true, replayed
	if !replayed {
		pl.SiteRefs = refs
	}
	return pl, nil
}

// ImportTemplate trả tệp mẫu .xlsx của dự án: sheet điểm, sheet hộ (tiêu đề cột, một dòng ví dụ để xóa) và sheet hướng
// dẫn (quy tắc, mã loại mô hình, mã xã của tỉnh, cán bộ Cấp Xã, điểm đã có kèm mã để nạp thêm hộ).
func (s *Service) ImportTemplate(ctx context.Context, ref uuid.UUID) ([]byte, string, error) {
	ctx = txdeps.With(ctx)
	c, err := s.reader(ctx)
	if err != nil {
		return nil, "", err
	}
	var p *store.Project
	var existing []store.Site
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		p, existing, _, e = s.importProject(ctx, tx, c, ref)
		return e
	}); err != nil {
		return nil, "", err
	}
	x := excelize.NewFile()
	defer func() { _ = x.Close() }()
	if err := x.SetSheetName("Sheet1", domain.SheetSites); err != nil {
		return nil, "", err
	}
	for _, name := range []string{domain.SheetHouseholds, domain.SheetGuide} {
		if _, err := x.NewSheet(name); err != nil {
			return nil, "", err
		}
	}
	head, _ := x.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"E2EFDA"}},
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "center"}})
	textFmt, _ := x.NewStyle(&excelize.Style{NumFmt: 49}) // "@": giữ số 0 đầu của mã xã, SĐT
	writeRows := func(sheet string, rows [][]any) error {
		for i, r := range rows {
			cell, _ := excelize.CoordinatesToCellName(1, i+1)
			if err := x.SetSheetRow(sheet, cell, &r); err != nil {
				return err
			}
		}
		return nil
	}
	sitesHead := make([]any, len(domain.SiteHeaders))
	for i, h := range domain.SiteHeaders {
		sitesHead[i] = h
	}
	hhHead := make([]any, len(domain.HouseholdHeaders))
	for i, h := range domain.HouseholdHeaders {
		hhHead[i] = h
	}
	if err := writeRows(domain.SheetSites, [][]any{sitesHead}); err != nil {
		return nil, "", err
	}
	if err := writeRows(domain.SheetHouseholds, [][]any{hhHead}); err != nil {
		return nil, "", err
	}
	for sheet, n := range map[string]int{domain.SheetSites: len(domain.SiteHeaders), domain.SheetHouseholds: len(domain.HouseholdHeaders)} {
		last, _ := excelize.ColumnNumberToName(n)
		_ = x.SetCellStyle(sheet, "A1", last+"1", head)
		_ = x.SetColWidth(sheet, "A", last, 20)
		_ = x.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	}
	_ = x.SetCellStyle(domain.SheetSites, "A2", fmt.Sprintf("C%d", domain.ImportMaxSites+1), textFmt)
	_ = x.SetCellStyle(domain.SheetHouseholds, "A2", fmt.Sprintf("C%d", domain.ImportMaxHouseholds+1), textFmt)
	for _, dv := range []struct {
		sheet, sqref string
		list         []string
	}{
		{domain.SheetSites, fmt.Sprintf("F2:F%d", domain.ImportMaxSites+1), []string{"con", "m2", "m3", "ha", "lồng", "bè"}},
		{domain.SheetSites, fmt.Sprintf("G2:G%d", domain.ImportMaxSites+1), domain.DiaryForms},
		{domain.SheetHouseholds, fmt.Sprintf("E2:E%d", domain.ImportMaxHouseholds+1), []string{"con", "m2", "m3", "ha", "lồng", "bè"}},
		{domain.SheetHouseholds, fmt.Sprintf("F2:F%d", domain.ImportMaxHouseholds+1), []string{domain.KeeperTextOfficer, domain.KeeperTextSelf}},
	} {
		v := excelize.NewDataValidation(true)
		v.Sqref = dv.sqref
		if err := v.SetDropList(dv.list); err == nil {
			_ = x.AddDataValidation(dv.sheet, v)
		}
	}

	guide := [][]any{
		{"Tệp nạp điểm trình diễn, hộ tham gia — dự án " + p.Code + " · " + p.Name},
		{"Mỗi dòng của sheet \"" + domain.SheetSites + "\" là một điểm mới; mỗi dòng của sheet \"" + domain.SheetHouseholds + "\" là một hộ. Không đổi tên sheet, tên và thứ tự cột. Cột có (*) bắt buộc."},
		{fmt.Sprintf("Tối đa %d điểm, %d hộ, tệp 2 MB. Nạp lên để xem trước; hệ thống chỉ ghi khi tệp không còn lỗi (ghi cả tệp hoặc không ghi gì).", domain.ImportMaxSites, domain.ImportMaxHouseholds)},
		{"Mã điểm: tự đặt trong tệp (chữ, số, . _ / -), dùng để nối hộ với điểm. Thêm hộ vào điểm đã có: ghi đúng mã UUID của điểm ở bảng dưới."},
		{"Quy mô: số thường, dấu phẩy hoặc dấu chấm thập phân (vd 1500 hoặc 2,5); không dùng số mũ. Nhập cả quy mô và đơn vị hoặc bỏ trống cả hai."},
		{"SĐT chủ hộ bắt buộc (khóa kiểm trùng chủ hộ). Họ tên, SĐT lưu ở kho định danh, không lưu trong sổ dự án. Ghi chú không ghi SĐT/CCCD."},
		{"Người ghi nhật ký: \"" + domain.KeeperTextOfficer + "\" (mặc định — cán bộ theo dõi của điểm) hoặc \"" + domain.KeeperTextSelf + "\". Ngày tham gia dạng dd/mm/yyyy."},
		{},
		{"Mã loại mô hình", "Tên"},
	}
	types := s.catalogNames(ctx, knport.CatalogCNTSModelType)
	codes := make([]string, 0, len(types))
	for k := range types {
		codes = append(codes, k)
	}
	slices.Sort(codes)
	for _, k := range codes {
		guide = append(guide, []any{k, types[k]})
	}
	guide = append(guide, []any{}, []any{"Mã xã", "Tên xã/phường của tỉnh " + p.Province})
	for _, u := range s.provinceCommunes(ctx, p.Province) {
		guide = append(guide, []any{u.Code, u.Name})
	}
	if s.Officers != nil {
		if idx, err := s.officers(ctx, c); err == nil {
			refs := make([]uuid.UUID, 0, len(idx))
			for r := range idx {
				refs = append(refs, r)
			}
			names := s.names(ctx, refs)
			type off struct{ name, communes string }
			var offs []off
			for r, o := range idx {
				if names[r] != "" {
					offs = append(offs, off{names[r], strings.Join(o.Communes, ", ")})
				}
			}
			slices.SortFunc(offs, func(a, b off) int { return strings.Compare(a.name, b.name) })
			guide = append(guide, []any{}, []any{"Cán bộ theo dõi (tên)", "Xã phụ trách"})
			for _, o := range offs {
				guide = append(guide, []any{o.name, o.communes})
			}
		}
	}
	guide = append(guide, []any{}, []any{"Mã điểm đã có (UUID)", "Tên điểm", "Mã xã", "Trạng thái"})
	for _, e := range existing {
		st := "Đang hoạt động"
		if e.Event == domain.EventClosed {
			st = "Đã đóng"
		}
		guide = append(guide, []any{e.Ref.String(), e.Name, e.CommuneCode, st})
	}
	if err := writeRows(domain.SheetGuide, guide); err != nil {
		return nil, "", err
	}
	_ = x.SetColWidth(domain.SheetGuide, "A", "A", 40)
	_ = x.SetColWidth(domain.SheetGuide, "B", "D", 30)
	x.SetActiveSheet(0)
	buf, err := x.WriteToBuffer()
	if err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "mau-nap-diem-ho-" + strings.ToLower(strings.ReplaceAll(p.Code, "/", "-")) + ".xlsx", nil
}

// provinceCommunes trả xã/phường còn hiệu lực của tỉnh (lỗi tra cứu → rỗng; tối đa maxGuideCommunes).
func (s *Service) provinceCommunes(ctx context.Context, province string) []partyport.AdminUnit {
	if s.Units == nil {
		return nil
	}
	day := s.today()
	codes, err := s.Units.Descendants(ctx, province, day)
	if err != nil {
		return nil
	}
	slices.Sort(codes)
	var out []partyport.AdminUnit
	for _, code := range codes {
		if len(out) >= maxGuideCommunes {
			break
		}
		u, err := s.unit(ctx, code)
		if err == nil && u.Level == partyport.LevelCommune {
			out = append(out, u)
		}
	}
	return out
}
