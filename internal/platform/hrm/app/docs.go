package app

// W11.24 — KHO TÀI LIỆU NHÂN SỰ (BRD HCTH v3.0 FN-14, UR-HC-25, UR-ADM-10). Xem domain/docs.go.
//
//   - Cây thư mục (sổ doc_folder): thư mục gốc chỉ Admin tạo (FN-14 điều kiện tiên quyết); thư mục con, đổi tên, quyền thư
//     mục cần docs.manage phạm vi toàn Trung tâm ở thư mục đó. Ẩn thư mục chỉ khi không còn thư mục con/tài liệu đang hiện.
//   - Tài liệu (sổ doc): siêu dữ liệu + phân loại (loại quyết định, năm, phòng ban, nhân viên). Tệp mã hóa phong bì ở
//     pf/evidence: máy chủ seal bằng khóa công khai của chủ thể "kho tài liệu" của tenant ở kho định danh (địa bàn
//     hrm_docs, tạo lần đầu), gắn bộ ba ('pf/hrm', 'hr_folder', thư mục); mở bằng OpenContent với mục đích hr_records.
//   - Xóa = ẩn (FN-14 luồng ngoại lệ): phiên bản mới status 'hidden' + lý do; tệp ở pf/evidence giữ nguyên.
//   - Nhật ký (BR-02): tải lên, sửa, ẩn là dòng của sổ doc; xem trước/tải xuống ghi doc_access TRƯỚC khi mở tệp
//     (fail-closed), pf/evidence ghi thêm pf_files.access_log, kho định danh ghi lần mở DEK.
//   - Tài liệu gắn nhân viên (hồ sơ W11.22) chỉ ghi/thấy khi hồ sơ bật (AG_HRM_RECORDS — Q-005); tắt → ẩn khỏi danh sách,
//     mở trực tiếp 403 hrm.records_disabled.

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// keyholderName là nhãn của chủ thể giữ khóa kho tài liệu ở kho định danh (không phải người thật).
const keyholderName = "Kho tài liệu nhân sự (khóa hệ thống)"

// DocStore là phần cấu hình kho tài liệu của Service (UseDocs).
type DocStore struct {
	Files evport.Files
	mu    sync.Mutex
	keys  map[uuid.UUID]client.PubKey // tenant → chủ thể giữ khóa
}

// UseDocs nối kho tệp pf/evidence cho kho tài liệu. Kho định danh lấy từ UseRecords (nối cả khi hồ sơ tắt).
func (s *Service) UseDocs(f evport.Files) {
	s.Docs = &DocStore{Files: f, keys: map[uuid.UUID]client.PubKey{}}
}

// dc là người gọi kho tài liệu đã giải quyền.
type dc struct {
	rc
	recordsOn bool
	mine      map[string]bool // hồ sơ gắn tài khoản người gọi (phạm vi "bản thân")
}

// docTx chạy fn trong giao dịch của module, tenant của người gọi.
func (s *Service) docTx(ctx context.Context, fn func(pgx.Tx) error) error {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return problem.Unauthenticated
	}
	return s.tx(ctx, p.TenantID, fn)
}

func (s *Service) recordsOn() bool { return s.Rec != nil && s.Rec.Enabled }

// docCaller kiểm ngữ cảnh KN, nạp ma trận FN-20 (kể cả khi hồ sơ tắt) và hồ sơ của chính người gọi (khi hồ sơ bật).
func (s *Service) docCaller(ctx context.Context, tx pgx.Tx) (dc, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return dc{}, problem.Unauthenticated
	}
	if p.KN == nil {
		return dc{}, problem.Forbidden.WithDetail("kho tài liệu nhân sự: cần ngữ cảnh phiên Khuyến nông")
	}
	c := dc{rc: rc{ref: p.ActorRef, tenant: p.TenantID, unitRef: p.KN.UnitRef, group: s.group(p.KN), unit: p.KN.InternalUnit,
		readOnly: p.KN.ReadOnly()}, recordsOn: s.recordsOn(), mine: map[string]bool{}}
	m, err := s.matrixOf(ctx, tx, c.group, c.ref)
	if err != nil {
		return dc{}, err
	}
	c.m = m
	if c.recordsOn {
		st, err := store.CurrentStaff(ctx, tx)
		if err != nil {
			return dc{}, err
		}
		for _, x := range st {
			if x.AccountRef != nil && *x.AccountRef == c.ref {
				c.mine[x.StaffID.String()] = true
			}
		}
	}
	return c, nil
}

// --- Cây thư mục. ---

type folderTree struct {
	byID     map[uuid.UUID]store.FolderRow
	acl      map[uuid.UUID]domain.FolderACL
	children map[uuid.UUID][]uuid.UUID // uuid.Nil = gốc
}

func loadTree(ctx context.Context, tx pgx.Tx) (folderTree, error) {
	rows, err := store.CurrentFolders(ctx, tx)
	if err != nil {
		return folderTree{}, err
	}
	t := folderTree{byID: map[uuid.UUID]store.FolderRow{}, acl: map[uuid.UUID]domain.FolderACL{}, children: map[uuid.UUID][]uuid.UUID{}}
	for _, r := range rows {
		t.byID[r.FolderID] = r
		a, _, err := domain.ParseFolderACL(r.ACL)
		if err != nil {
			return folderTree{}, err
		}
		t.acl[r.FolderID] = a
		parent := uuid.Nil
		if r.ParentID != nil {
			parent = *r.ParentID
		}
		t.children[parent] = append(t.children[parent], r.FolderID)
	}
	return t, nil
}

// chain trả các thư mục từ gốc tới id (kể cả id); rỗng nếu không có.
func (t folderTree) chain(id uuid.UUID) []store.FolderRow {
	var out []store.FolderRow
	for cur, n := id, 0; n <= domain.MaxFolderDepth+1; n++ {
		r, ok := t.byID[cur]
		if !ok {
			break
		}
		out = append(out, r)
		if r.ParentID == nil {
			break
		}
		cur = *r.ParentID
	}
	slices.Reverse(out)
	return out
}

// hidden: thư mục hoặc một thư mục cha đã ẩn.
func (t folderTree) hidden(id uuid.UUID) bool {
	for _, r := range t.chain(id) {
		if r.Status == "hidden" {
			return true
		}
	}
	return false
}

// perm là quyền tài liệu hiệu lực của người gọi ở thư mục id.
func (c dc) perm(t folderTree, id uuid.UUID) domain.Matrix {
	var acls []domain.FolderACL
	for _, r := range t.chain(id) {
		acls = append(acls, t.acl[r.FolderID])
	}
	return domain.FolderMatrix(c.m, c.group, c.ref.String(), acls)
}

func (c dc) seesFolder(m domain.Matrix) bool {
	return m[domain.PermDocView] != domain.ScopeNone || m[domain.PermDocManage] != domain.ScopeNone
}

// manageAll: được sửa cấu trúc thư mục (tạo thư mục con, đổi tên, quyền thư mục) — docs.manage toàn Trung tâm.
func (c dc) manageAll(m domain.Matrix) bool {
	return !c.readOnly && (c.group == domain.GroupAdmin || m[domain.PermDocManage] == domain.ScopeAll)
}

// onDoc kiểm phạm vi nhóm quyền p của m trên tài liệu d (bản thân / đơn vị / toàn Trung tâm). Tài liệu gắn nhân viên khi hồ
// sơ tắt: không ai thấy.
func (c dc) onDoc(m domain.Matrix, p domain.Perm, d store.DocRow) bool {
	if len(d.StaffIDs) > 0 && !c.recordsOn {
		return false
	}
	self, unit := domain.DocRelation(uuidStrings(d.StaffIDs), c.mine, d.UnitCode, c.unit)
	return m[p].Allows(self, unit)
}

func uuidStrings(xs []uuid.UUID) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.String()
	}
	return out
}

// FolderView là một thư mục người gọi thấy.
type FolderView struct {
	store.FolderRow
	Path      []string
	Hidden    bool // thư mục hoặc thư mục cha đã ẩn
	Docs      int  // số tài liệu đang hiện người gọi thấy (trực tiếp trong thư mục)
	CanView   bool
	CanManage bool // tải lên, sửa phân loại, ẩn tài liệu (trong phạm vi)
	CanAdmin  bool // tạo thư mục con, đổi tên, quyền thư mục, ẩn thư mục
	ACL       domain.FolderACL
}

// DocMeta là quyền chung của người gọi với kho tài liệu (màn hình).
type DocMeta struct {
	RecordsEnabled bool
	Group, Unit    string
	Perms          domain.Matrix // docs.view, docs.manage theo ma trận FN-20 (trước quyền thư mục)
	CanCreateRoot  bool
	Ready          bool // đã nối kho tệp + kho định danh
}

// Folders trả cây thư mục người gọi thấy (thư mục cha của thư mục thấy được cũng hiện để giữ cây). includeHidden chỉ có tác
// dụng với người quản trị thư mục.
func (s *Service) Folders(ctx context.Context, includeHidden bool) (DocMeta, []FolderView, error) {
	var meta DocMeta
	var out []FolderView
	err := s.docTx(ctx, func(tx pgx.Tx) error {
		c, err := s.docCaller(ctx, tx)
		if err != nil {
			return err
		}
		meta = DocMeta{RecordsEnabled: c.recordsOn, Group: c.group, Unit: c.unit, Perms: domain.Matrix{},
			CanCreateRoot: c.group == domain.GroupAdmin && !c.readOnly, Ready: s.docsReady()}
		for _, p := range domain.DocPerms {
			meta.Perms[p] = c.m[p]
		}
		t, err := loadTree(ctx, tx)
		if err != nil {
			return err
		}
		docs, err := store.CurrentDocs(ctx, tx)
		if err != nil {
			return err
		}
		count := map[uuid.UUID]int{}
		perms := map[uuid.UUID]domain.Matrix{}
		for id := range t.byID {
			perms[id] = c.perm(t, id)
		}
		for _, d := range docs {
			if d.Status == "active" && c.onDoc(perms[d.FolderID], domain.PermDocView, d) {
				count[d.FolderID]++
			}
		}
		keep := map[uuid.UUID]bool{}
		for id, r := range t.byID {
			m := perms[id]
			hid := t.hidden(id)
			if !c.seesFolder(m) || (hid && (!includeHidden || !c.manageAll(m))) {
				continue
			}
			for _, a := range t.chain(r.FolderID) {
				keep[a.FolderID] = true
			}
		}
		for id := range keep {
			r, m := t.byID[id], perms[id]
			v := FolderView{FolderRow: r, Hidden: t.hidden(id), Docs: count[id], CanView: m[domain.PermDocView] != domain.ScopeNone,
				CanManage: !c.readOnly && m[domain.PermDocManage] != domain.ScopeNone, CanAdmin: c.manageAll(m)}
			if r.ParentID == nil {
				v.CanAdmin = c.group == domain.GroupAdmin && !c.readOnly
			}
			for _, a := range t.chain(id) {
				v.Path = append(v.Path, a.Name)
			}
			if v.CanAdmin {
				v.ACL = t.acl[id]
			}
			out = append(out, v)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.Join(out[i].Path, "\x00"), strings.Join(out[j].Path, "\x00")
		if len(out[i].Path) == len(out[j].Path) && out[i].SortOrder != out[j].SortOrder && sameParent(out[i], out[j]) {
			return out[i].SortOrder < out[j].SortOrder
		}
		return a < b
	})
	return meta, out, err
}

func sameParent(a, b FolderView) bool {
	return (a.ParentID == nil && b.ParentID == nil) || (a.ParentID != nil && b.ParentID != nil && *a.ParentID == *b.ParentID)
}

func (s *Service) docsReady() bool {
	return s.Docs != nil && s.Docs.Files != nil && s.Rec != nil && s.Rec.Vault != nil
}

// FolderInput là thân tạo/sửa thư mục.
type FolderInput struct {
	Parent  *uuid.UUID
	Name    string
	DocType string
}

// FolderResult là kết quả ghi thư mục.
type FolderResult struct {
	FolderID uuid.UUID
	Version  int64
}

func checkSibling(t folderTree, parent *uuid.UUID, name string, self uuid.UUID) error {
	p := uuid.Nil
	if parent != nil {
		p = *parent
	}
	for _, id := range t.children[p] {
		r := t.byID[id]
		if id != self && r.Status == "active" && strings.EqualFold(r.Name, name) {
			return port.ErrFolderExists
		}
	}
	return nil
}

// CreateFolder tạo thư mục (FN-14 B2): gốc — Admin; con — docs.manage toàn Trung tâm ở thư mục cha.
func (s *Service) CreateFolder(ctx context.Context, in FolderInput, idem string) (FolderResult, error) {
	name, err := domain.NormalizeName(in.Name, 120, true)
	if err != nil {
		return FolderResult{}, problem.Validation.WithField("name", err.Error())
	}
	if in.DocType != "" && !domain.ValidDocType(in.DocType) {
		return FolderResult{}, problem.Validation.WithField("doc_type", "loại tài liệu không có")
	}
	var res FolderResult
	err = s.docTx(ctx, func(tx pgx.Tx) error {
		c, err := s.docCaller(ctx, tx)
		if err != nil {
			return err
		}
		if err := store.LockDocs(ctx, tx, c.tenant); err != nil {
			return err
		}
		t, err := loadTree(ctx, tx)
		if err != nil {
			return err
		}
		if in.Parent == nil {
			if c.group != domain.GroupAdmin || c.readOnly {
				return problem.Forbidden.WithDetail("thư mục gốc do Admin tạo (FN-14)")
			}
		} else {
			if _, ok := t.byID[*in.Parent]; !ok || t.hidden(*in.Parent) {
				return port.ErrFolderNotFound
			}
			if m := c.perm(t, *in.Parent); !c.manageAll(m) {
				return forbidDocs(c, m)
			}
			if len(t.chain(*in.Parent)) >= domain.MaxFolderDepth {
				return problem.Validation.WithField("parent", "vượt số cấp thư mục tối đa")
			}
		}
		if err := checkSibling(t, in.Parent, name, uuid.Nil); err != nil {
			return err
		}
		res.FolderID = ids.New()
		res.Version, err = store.InsertFolder(ctx, tx, c.tenant, store.FolderRow{ID: ids.New(), FolderID: res.FolderID, ParentID: in.Parent,
			Name: name, DocType: in.DocType, Status: "active", Change: "created", Actor: c.ref}, s.Clock.Now(),
			stableKey(idem, "folder:create"))
		return classify(err)
	})
	return res, err
}

// forbidDocs: thư mục người gọi không thấy → 404 (không lộ sự tồn tại); thấy mà không đủ quyền → 403.
func forbidDocs(c dc, m domain.Matrix) error {
	if !c.seesFolder(m) {
		return port.ErrFolderNotFound
	}
	if c.readOnly {
		return problem.Forbidden.WithDetail("ngữ cảnh chỉ xem không ghi được kho tài liệu")
	}
	return problem.Forbidden.WithDetail("kho tài liệu: không có quyền quản lý thư mục này (FN-20 docs.manage)")
}

// InstallDocFolders cài cấu trúc thư mục gốc mặc định còn thiếu (theo system_key) — Admin.
func (s *Service) InstallDocFolders(ctx context.Context, idem string) (int, error) {
	n := 0
	err := s.docTx(ctx, func(tx pgx.Tx) error {
		c, err := s.docCaller(ctx, tx)
		if err != nil {
			return err
		}
		if c.group != domain.GroupAdmin || c.readOnly {
			return problem.Forbidden.WithDetail("thư mục gốc do Admin tạo (FN-14)")
		}
		if err := store.LockDocs(ctx, tx, c.tenant); err != nil {
			return err
		}
		t, err := loadTree(ctx, tx)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		for _, r := range t.byID {
			if r.SystemKey != "" {
				have[r.SystemKey] = true
			}
		}
		for i, d := range domain.DefaultFolders {
			if have[d.Key] {
				continue
			}
			if checkSibling(t, nil, d.Name, uuid.Nil) != nil {
				continue // Admin đã tự tạo thư mục cùng tên
			}
			if _, err := store.InsertFolder(ctx, tx, c.tenant, store.FolderRow{ID: ids.New(), FolderID: ids.New(), Name: d.Name,
				SystemKey: d.Key, DocType: d.DocType, SortOrder: (i + 1) * 10, Status: "active", Change: "created", Actor: c.ref},
				s.Clock.Now(), stableKey(idem, "folder:default:"+d.Key)); err != nil {
				return classify(err)
			}
			n++
		}
		return nil
	})
	return n, err
}

// folderWrite nạp thư mục id để ghi phiên bản mới: gốc — Admin; còn lại — docs.manage toàn Trung tâm ở thư mục.
func (s *Service) folderWrite(ctx context.Context, tx pgx.Tx, id uuid.UUID) (dc, folderTree, store.FolderRow, error) {
	c, err := s.docCaller(ctx, tx)
	if err != nil {
		return c, folderTree{}, store.FolderRow{}, err
	}
	if err := store.LockDocs(ctx, tx, c.tenant); err != nil {
		return c, folderTree{}, store.FolderRow{}, err
	}
	t, err := loadTree(ctx, tx)
	if err != nil {
		return c, t, store.FolderRow{}, err
	}
	r, ok := t.byID[id]
	if !ok {
		return c, t, r, port.ErrFolderNotFound
	}
	m := c.perm(t, id)
	switch {
	case r.ParentID == nil && (c.group != domain.GroupAdmin || c.readOnly):
		if !c.seesFolder(m) {
			return c, t, r, port.ErrFolderNotFound
		}
		return c, t, r, problem.Forbidden.WithDetail("thư mục gốc do Admin quản lý (FN-14)")
	case !c.manageAll(m):
		return c, t, r, forbidDocs(c, m)
	}
	return c, t, r, nil
}

// RenameFolder đổi tên thư mục (FN-14 B2).
func (s *Service) RenameFolder(ctx context.Context, id uuid.UUID, name, idem string) (FolderResult, error) {
	name, err := domain.NormalizeName(name, 120, true)
	if err != nil {
		return FolderResult{}, problem.Validation.WithField("name", err.Error())
	}
	res := FolderResult{FolderID: id}
	err = s.docTx(ctx, func(tx pgx.Tx) error {
		c, t, r, err := s.folderWrite(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.Name == name {
			res.Version = r.Seq
			return nil
		}
		if err := checkSibling(t, r.ParentID, name, id); err != nil {
			return err
		}
		r.ID, r.Name, r.Change, r.Note, r.Actor = ids.New(), name, "renamed", "", c.ref
		res.Version, err = store.InsertFolder(ctx, tx, c.tenant, r, s.Clock.Now(), stableKey(idem, "folder:rename:"+id.String()))
		return classify(err)
	})
	return res, err
}

// SetFolderACL ghi quyền riêng của thư mục (FN-14 luồng phụ "gán quyền theo thư mục"; ghi đè ma trận FN-20 cho nhóm quyền
// tài liệu ở thư mục và thư mục con). Sổ thư mục là nhật ký đổi quyền.
func (s *Service) SetFolderACL(ctx context.Context, id uuid.UUID, raw []byte, note, idem string) (FolderResult, error) {
	_, canon, err := domain.ParseFolderACL(raw)
	if err != nil {
		return FolderResult{}, problem.Validation.WithDetail(err.Error())
	}
	if n, err2 := domain.NormalizeName(note, 500, false); err2 == nil {
		note = n
	} else {
		note = ""
	}
	res := FolderResult{FolderID: id}
	err = s.docTx(ctx, func(tx pgx.Tx) error {
		c, _, r, err := s.folderWrite(ctx, tx, id)
		if err != nil {
			return err
		}
		r.ID, r.ACL, r.Change, r.Note, r.Actor = ids.New(), canon, "acl", note, c.ref
		res.Version, err = store.InsertFolder(ctx, tx, c.tenant, r, s.Clock.Now(), stableKey(idem, "folder:acl:"+id.String()))
		return classify(err)
	})
	return res, err
}

// SetFolderHidden ẩn (hidden = true) hoặc hiện lại thư mục. Ẩn chỉ khi không còn thư mục con/tài liệu đang hiện.
func (s *Service) SetFolderHidden(ctx context.Context, id uuid.UUID, hidden bool, reason, idem string) (FolderResult, error) {
	reason, err := domain.NormalizeName(reason, 500, false)
	if err != nil || len([]rune(reason)) < 10 {
		return FolderResult{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	res := FolderResult{FolderID: id}
	err = s.docTx(ctx, func(tx pgx.Tx) error {
		c, t, r, err := s.folderWrite(ctx, tx, id)
		if err != nil {
			return err
		}
		want, change := "active", "restored"
		if hidden {
			want, change = "hidden", "hidden"
		}
		if r.Status == want {
			res.Version = r.Seq
			return nil
		}
		if hidden {
			for _, ch := range t.children[id] {
				if t.byID[ch].Status == "active" {
					return port.ErrFolderNotEmpty
				}
			}
			docs, err := store.CurrentDocs(ctx, tx)
			if err != nil {
				return err
			}
			for _, d := range docs {
				if d.FolderID == id && d.Status == "active" {
					return port.ErrFolderNotEmpty
				}
			}
		} else if err := checkSibling(t, r.ParentID, r.Name, id); err != nil {
			return err
		}
		r.ID, r.Status, r.Change, r.Note, r.Actor = ids.New(), want, change, reason, c.ref
		res.Version, err = store.InsertFolder(ctx, tx, c.tenant, r, s.Clock.Now(), stableKey(idem, "folder:"+change+":"+id.String()))
		return classify(err)
	})
	return res, err
}

// FolderHistory trả mọi phiên bản của thư mục (nhật ký đổi tên/quyền/ẩn) — người quản trị thư mục.
func (s *Service) FolderHistory(ctx context.Context, id uuid.UUID) ([]store.FolderRow, error) {
	var out []store.FolderRow
	err := s.docTx(ctx, func(tx pgx.Tx) error {
		c, err := s.docCaller(ctx, tx)
		if err != nil {
			return err
		}
		t, err := loadTree(ctx, tx)
		if err != nil {
			return err
		}
		if _, ok := t.byID[id]; !ok {
			return port.ErrFolderNotFound
		}
		if m := c.perm(t, id); c.group != domain.GroupAdmin && m[domain.PermDocManage] != domain.ScopeAll {
			if !c.seesFolder(m) {
				return port.ErrFolderNotFound
			}
			return problem.Forbidden.WithDetail("kho tài liệu: lịch sử thư mục cần docs.manage toàn Trung tâm")
		}
		out, err = store.FolderHistory(ctx, tx, id)
		return err
	})
	return out, err
}

// --- Tài liệu. ---

var reUnitCode = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,63}$`)

// DocInput là phân loại của tài liệu (FN-14 B3).
type DocInput struct {
	FolderID uuid.UUID
	Title    string
	DocType  string
	Year     *int
	UnitCode string
	StaffIDs []uuid.UUID
	DocNo    string
	IssuedOn *time.Time
	Note     string
}

func normDocInput(in DocInput) (DocInput, error) {
	v, bad := problem.Validation, false
	var err error
	if in.Title, err = domain.NormalizeName(in.Title, 300, false); err != nil {
		v, bad = v.WithField("title", err.Error()), true
	}
	if !domain.ValidDocType(in.DocType) {
		v, bad = v.WithField("doc_type", "loại tài liệu không có"), true
	}
	if in.Year != nil && (*in.Year < 1975 || *in.Year > 2100) {
		v, bad = v.WithField("year", "năm 1975–2100"), true
	}
	in.UnitCode = strings.TrimSpace(in.UnitCode)
	if in.UnitCode != "" && !reUnitCode.MatchString(in.UnitCode) {
		v, bad = v.WithField("unit_code", "mã đơn vị nội bộ sai dạng"), true
	}
	in.DocNo = strings.Join(strings.Fields(in.DocNo), " ")
	if len([]rune(in.DocNo)) > 60 {
		v, bad = v.WithField("doc_no", "tối đa 60 ký tự"), true
	}
	in.Note = strings.TrimSpace(in.Note)
	if len([]rune(in.Note)) > 1000 {
		v, bad = v.WithField("note", "tối đa 1000 ký tự"), true
	}
	if len(in.StaffIDs) > 500 {
		v, bad = v.WithField("staff_ids", "tối đa 500 nhân viên"), true
	}
	seen := map[uuid.UUID]bool{}
	staff := in.StaffIDs[:0:0]
	for _, id := range in.StaffIDs {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			staff = append(staff, id)
		}
	}
	in.StaffIDs = staff
	if in.Year == nil && in.IssuedOn != nil {
		y := in.IssuedOn.Year()
		in.Year = &y
	}
	if bad {
		return in, v
	}
	return in, nil
}

// checkDocRefs kiểm phân loại theo dữ liệu: đơn vị nội bộ đang dùng, nhân viên có hồ sơ (cần hồ sơ bật). Phòng ban trống
// và có nhân viên → lấy đơn vị công tác hiện tại của nhân viên đầu tiên (để phạm vi "đơn vị mình" thấy tài liệu).
func (s *Service) checkDocRefs(ctx context.Context, tx pgx.Tx, c dc, in *DocInput) error {
	if len(in.StaffIDs) > 0 {
		if !c.recordsOn {
			return port.ErrRecordsDisabled.WithDetail("gắn tài liệu với nhân viên cần hồ sơ nhân sự đã bật")
		}
		st, err := store.CurrentStaff(ctx, tx)
		if err != nil {
			return err
		}
		have := map[uuid.UUID]bool{}
		for _, x := range st {
			have[x.StaffID] = true
		}
		for i, id := range in.StaffIDs {
			if !have[id] {
				return problem.Validation.WithField("staff_ids", "không có hồ sơ nhân sự thứ "+strconv.Itoa(i+1))
			}
		}
		if in.UnitCode == "" {
			f, err := loadFile(ctx, tx, in.StaffIDs[0])
			if err != nil {
				return err
			}
			in.UnitCode = domain.Project(f.events, s.day(time.Time{}), func(string) int { return 0 }).Unit
		}
	}
	if in.UnitCode != "" && s.Rec != nil {
		set, err := s.unitSet(ctx, c.rc)
		if err != nil {
			return err
		}
		if set != nil && !set[in.UnitCode] && len(in.StaffIDs) == 0 {
			return problem.Validation.WithField("unit_code", "không phải đơn vị nội bộ đang dùng")
		}
	}
	return nil
}

// docWrite kiểm quyền ghi tài liệu d ở thư mục folder: docs.manage có phạm vi phủ tài liệu.
func (c dc) docWrite(t folderTree, folder uuid.UUID, d store.DocRow) error {
	if _, ok := t.byID[folder]; !ok || t.hidden(folder) {
		return port.ErrFolderNotFound
	}
	m := c.perm(t, folder)
	if c.readOnly || m[domain.PermDocManage] == domain.ScopeNone {
		return forbidDocs(c, m)
	}
	if !c.onDoc(m, domain.PermDocManage, d) {
		return problem.Forbidden.WithDetail("kho tài liệu: tài liệu ngoài phạm vi quản lý của người gọi (phòng ban/nhân viên)")
	}
	return nil
}

// keyholder trả chủ thể giữ khóa kho tài liệu của tenant (địa bàn hrm_docs), tạo lần đầu. Hai lần tạo song song sinh hai
// chủ thể — vô hại: mỗi tệp ghi chủ thể của nó (subject_ref của phong bì).
func (s *Service) keyholder(ctx context.Context, tid uuid.UUID) (client.PubKey, error) {
	s.Docs.mu.Lock()
	k, ok := s.Docs.keys[tid]
	s.Docs.mu.Unlock()
	if ok {
		return k, nil
	}
	pks, err := s.Rec.Vault.PubKeys(ctx, []string{client.AreaHRDocs})
	if err != nil {
		return k, err
	}
	if len(pks) > 0 {
		k = pks[0]
	} else {
		cr, err := s.Rec.Vault.Create(ctx, client.PurposeHRMProfile, client.Identity{Kind: client.SubjectPerson, FullName: keyholderName,
			AreaCode: client.AreaHRDocs})
		if err != nil {
			return k, err
		}
		k = client.PubKey{Ref: cr.Ref, AreaCode: client.AreaHRDocs, PublicKey: cr.PublicKey}
	}
	if len(k.PublicKey) != envelope.KeySize {
		return k, errors.New("hrm: khóa công khai của kho tài liệu sai độ dài")
	}
	s.Docs.mu.Lock()
	s.Docs.keys[tid] = k
	s.Docs.mu.Unlock()
	return k, nil
}

// DocResult là kết quả ghi tài liệu.
type DocResult struct {
	DocID   uuid.UUID
	Version int64
}

// UploadDoc tải lên một tệp với phân loại (FN-14 B3): kiểm quyền, định dạng, dung lượng; seal bằng khóa kho tài liệu; lưu
// bản mã qua pf/evidence; ghi sổ doc.
func (s *Service) UploadDoc(ctx context.Context, in DocInput, fileName string, content []byte, idem string) (DocResult, error) {
	if !s.docsReady() {
		return DocResult{}, port.ErrDocsUnavailable
	}
	if len(content) == 0 {
		return DocResult{}, problem.Validation.WithField("file", "bắt buộc")
	}
	if len(content) > domain.MaxDocBytes {
		return DocResult{}, port.ErrDocTooLarge.WithDetail("tối đa 15 MB mỗi tệp")
	}
	ct, name, err := domain.DocContentType(fileName, content)
	if err != nil {
		return DocResult{}, port.ErrDocFormat.WithDetail(err.Error())
	}
	in, err = normDocInput(in)
	if err != nil {
		return DocResult{}, err
	}
	row := store.DocRow{DocID: ids.New(), FolderID: in.FolderID, FileName: name, ContentType: ct, Size: int64(len(content)),
		Status: "active", Change: "created"}
	var tid uuid.UUID
	check := func(tx pgx.Tx) (dc, error) {
		c, err := s.docCaller(ctx, tx)
		if err != nil {
			return c, err
		}
		if err := s.checkDocRefs(ctx, tx, c, &in); err != nil {
			return c, err
		}
		applyDocInput(&row, in)
		t, err := loadTree(ctx, tx)
		if err != nil {
			return c, err
		}
		return c, c.docWrite(t, in.FolderID, row)
	}
	if err := s.docTx(ctx, func(tx pgx.Tx) error {
		c, err := check(tx)
		tid = c.tenant
		return err
	}); err != nil {
		return DocResult{}, err
	}
	k, err := s.keyholder(ctx, tid)
	if err != nil {
		return DocResult{}, err
	}
	var pub [envelope.KeySize]byte
	copy(pub[:], k.PublicKey)
	sealed, err := envelope.Seal(content, &pub)
	if err != nil {
		return DocResult{}, err
	}
	fileRef := ids.New().String()
	f, _, err := s.Docs.Files.Attach(ctx, evport.AttachInput{FileRef: &fileRef,
		Meta: envelope.NewMetadata(k.Ref.String(), sealed, ct, ""), Blob: sealed.Ciphertext,
		Subject: &evport.Subject{Module: port.SubjectModule, Type: port.SubjectTypeFolder, ID: in.FolderID}})
	if err != nil {
		return DocResult{}, err
	}
	row.FileRef = f.Ref
	res := DocResult{DocID: row.DocID}
	err = s.docTx(ctx, func(tx pgx.Tx) error {
		c, err := check(tx) // kiểm lại trong giao dịch ghi (thư mục có thể vừa bị ẩn)
		if err != nil {
			return err
		}
		row.ID, row.Actor = ids.New(), c.ref
		res.Version, err = store.InsertDoc(ctx, tx, c.tenant, row, s.Clock.Now(), stableKey(idem, "doc:create"))
		return classify(err)
	})
	return res, err
}

func applyDocInput(r *store.DocRow, in DocInput) {
	r.FolderID, r.Title, r.DocType, r.Year, r.UnitCode, r.StaffIDs = in.FolderID, in.Title, in.DocType, in.Year, in.UnitCode, in.StaffIDs
	r.DocNo, r.IssuedOn, r.Note = in.DocNo, in.IssuedOn, in.Note
}

func changedDocFields(a, b store.DocRow) []string {
	var out []string
	add := func(cond bool, f string) {
		if cond {
			out = append(out, f)
		}
	}
	add(a.Title != b.Title, "title")
	add(a.DocType != b.DocType, "doc_type")
	add(!eqInt(a.Year, b.Year), "year")
	add(a.UnitCode != b.UnitCode, "unit_code")
	add(!slices.Equal(a.StaffIDs, b.StaffIDs), "staff_ids")
	add(a.DocNo != b.DocNo, "doc_no")
	add(!eqDate(a.IssuedOn, b.IssuedOn), "issued_on")
	add(a.Note != b.Note, "note")
	return out
}

func eqInt(a, b *int) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func eqDate(a, b *time.Time) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.Format(time.DateOnly) == b.Format(time.DateOnly))
}

// docHead nạp phiên bản hiện hành của tài liệu id; không có → hrm.doc_not_found.
func docHead(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]store.DocRow, store.DocRow, error) {
	h, err := store.DocHistory(ctx, tx, id)
	if err != nil {
		return nil, store.DocRow{}, err
	}
	if len(h) == 0 {
		return nil, store.DocRow{}, port.ErrDocNotFound
	}
	return h, h[len(h)-1], nil
}

// visibleDoc kiểm người gọi thấy tài liệu d (404 nếu không — không lộ sự tồn tại); tài liệu gắn nhân viên khi hồ sơ tắt →
// hrm.records_disabled. Tài liệu đã ẩn chỉ người quản lý (trong phạm vi) thấy.
func (c dc) visibleDoc(t folderTree, d store.DocRow) (domain.Matrix, error) {
	if len(d.StaffIDs) > 0 && !c.recordsOn {
		return nil, port.ErrRecordsDisabled
	}
	m := c.perm(t, d.FolderID)
	view := c.onDoc(m, domain.PermDocView, d) || c.onDoc(m, domain.PermDocManage, d)
	if !view || ((d.Status == "hidden" || t.hidden(d.FolderID)) && !c.onDoc(m, domain.PermDocManage, d)) {
		return m, port.ErrDocNotFound
	}
	return m, nil
}

// UpdateDoc sửa phân loại tài liệu (thư mục không đổi — bộ ba chủ thể của tệp gắn thư mục lúc tải lên).
func (s *Service) UpdateDoc(ctx context.Context, id uuid.UUID, in DocInput, idem string) (DocResult, error) {
	res := DocResult{DocID: id}
	err := s.docTx(ctx, func(tx pgx.Tx) error {
		c, err := s.docCaller(ctx, tx)
		if err != nil {
			return err
		}
		_, cur, err := docHead(ctx, tx, id)
		if err != nil {
			return err
		}
		t, err := loadTree(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := c.visibleDoc(t, cur); err != nil {
			return err
		}
		if err := c.docWrite(t, cur.FolderID, cur); err != nil {
			return err
		}
		if cur.Status != "active" {
			return problem.Conflict.WithDetail("tài liệu đã ẩn — hiện lại trước khi sửa")
		}
		in.FolderID = cur.FolderID
		if in, err = normDocInput(in); err != nil {
			return err
		}
		if err := s.checkDocRefs(ctx, tx, c, &in); err != nil {
			return err
		}
		next := cur
		applyDocInput(&next, in)
		if err := c.docWrite(t, cur.FolderID, next); err != nil {
			return err
		}
		fields := changedDocFields(cur, next)
		if len(fields) == 0 {
			res.Version = cur.Seq
			return nil
		}
		next.ID, next.Change, next.ChangedFields, next.Reason, next.Actor = ids.New(), "updated", fields, "", c.ref
		res.Version, err = store.InsertDoc(ctx, tx, c.tenant, next, s.Clock.Now(), stableKey(idem, "doc:update:"+id.String()))
		return classify(err)
	})
	return res, err
}

// SetDocHidden ẩn ("xóa" — FN-14 luồng ngoại lệ: chỉ ẩn, lưu vết) hoặc hiện lại tài liệu; lý do bắt buộc.
func (s *Service) SetDocHidden(ctx context.Context, id uuid.UUID, hidden bool, reason, idem string) (DocResult, error) {
	reason, err := domain.NormalizeName(reason, 500, false)
	if err != nil || len([]rune(reason)) < 10 {
		return DocResult{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	res := DocResult{DocID: id}
	err = s.docTx(ctx, func(tx pgx.Tx) error {
		c, err := s.docCaller(ctx, tx)
		if err != nil {
			return err
		}
		_, cur, err := docHead(ctx, tx, id)
		if err != nil {
			return err
		}
		t, err := loadTree(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := c.visibleDoc(t, cur); err != nil {
			return err
		}
		if err := c.docWrite(t, cur.FolderID, cur); err != nil {
			return err
		}
		want, change := "active", "restored"
		if hidden {
			want, change = "hidden", "hidden"
		}
		if cur.Status == want {
			res.Version = cur.Seq
			return nil
		}
		next := cur
		next.ID, next.Status, next.Change, next.ChangedFields, next.Reason, next.Actor = ids.New(), want, change, nil, reason, c.ref
		res.Version, err = store.InsertDoc(ctx, tx, c.tenant, next, s.Clock.Now(), stableKey(idem, "doc:"+change+":"+id.String()))
		return classify(err)
	})
	return res, err
}

// DocFilter là bộ lọc tra cứu (FN-14 B4: theo tên, phân loại; AC-01 theo nhân viên).
type DocFilter struct {
	Folder        *uuid.UUID
	Recursive     bool
	Q             string
	DocType       string
	Year          *int
	Unit          string
	Staff         *uuid.UUID
	IncludeHidden bool
	Offset, Limit int
}

// DocItem là một tài liệu trong danh sách.
type DocItem struct {
	store.DocRow
	FolderPath []string
	StaffCodes []string
	CanManage  bool
	Created    time.Time // lúc tải lên
	Uploader   uuid.UUID
}

// DocList là trang kết quả.
type DocList struct {
	Total int
	Items []DocItem
}

// ListDocs tra cứu tài liệu người gọi thấy.
func (s *Service) ListDocs(ctx context.Context, f DocFilter) (DocList, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	var out DocList
	err := s.docTx(ctx, func(tx pgx.Tx) error {
		c, err := s.docCaller(ctx, tx)
		if err != nil {
			return err
		}
		if f.Staff != nil && !c.recordsOn {
			return port.ErrRecordsDisabled
		}
		t, err := loadTree(ctx, tx)
		if err != nil {
			return err
		}
		if f.Folder != nil {
			if _, ok := t.byID[*f.Folder]; !ok || !c.seesFolder(c.perm(t, *f.Folder)) {
				return port.ErrFolderNotFound
			}
		}
		docs, err := store.CurrentDocs(ctx, tx)
		if err != nil {
			return err
		}
		codes := map[uuid.UUID]string{}
		if c.recordsOn {
			st, err := store.CurrentStaff(ctx, tx)
			if err != nil {
				return err
			}
			for _, x := range st {
				codes[x.StaffID] = x.Code
			}
		}
		created := map[uuid.UUID]store.DocRow{}
		if err := firstVersions(ctx, tx, created); err != nil {
			return err
		}
		q := strings.ToLower(strings.TrimSpace(f.Q))
		perms := map[uuid.UUID]domain.Matrix{}
		for _, d := range docs {
			if f.Folder != nil && d.FolderID != *f.Folder && (!f.Recursive || !under(t, d.FolderID, *f.Folder)) {
				continue
			}
			if (f.DocType != "" && d.DocType != f.DocType) || (f.Year != nil && !eqInt(d.Year, f.Year)) ||
				(f.Unit != "" && d.UnitCode != f.Unit) || (f.Staff != nil && !slices.Contains(d.StaffIDs, *f.Staff)) {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(d.Title+"\x00"+d.DocNo+"\x00"+d.FileName), q) {
				continue
			}
			if d.Status == "hidden" && !f.IncludeHidden {
				continue
			}
			if _, err := c.visibleDoc(t, d); err != nil {
				continue
			}
			m, ok := perms[d.FolderID]
			if !ok {
				m = c.perm(t, d.FolderID)
				perms[d.FolderID] = m
			}
			it := DocItem{DocRow: d, CanManage: !c.readOnly && c.onDoc(m, domain.PermDocManage, d)}
			for _, a := range t.chain(d.FolderID) {
				it.FolderPath = append(it.FolderPath, a.Name)
			}
			for _, id := range d.StaffIDs {
				it.StaffCodes = append(it.StaffCodes, codes[id])
			}
			if c0, ok := created[d.DocID]; ok {
				it.Created, it.Uploader = c0.RecordedAt, c0.Actor
			}
			out.Items = append(out.Items, it)
		}
		return nil
	})
	if err != nil {
		return DocList{}, err
	}
	out.Total = len(out.Items)
	lo := min(f.Offset, len(out.Items))
	hi := min(lo+f.Limit, len(out.Items))
	out.Items = out.Items[lo:hi]
	return out, nil
}

// firstVersions nạp phiên bản đầu (tải lên) của mọi tài liệu: người tải, ngày tải (BR-03).
func firstVersions(ctx context.Context, tx pgx.Tx, out map[uuid.UUID]store.DocRow) error {
	rows, err := tx.Query(ctx, `SELECT doc_id, recorded_at, actor_ref FROM pf_hrm.doc WHERE change = 'created'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r store.DocRow
		if err := rows.Scan(&r.DocID, &r.RecordedAt, &r.Actor); err != nil {
			return err
		}
		out[r.DocID] = r
	}
	return rows.Err()
}

// under: thư mục id nằm dưới (không kể chính nó) thư mục root.
func under(t folderTree, id, root uuid.UUID) bool {
	for _, r := range t.chain(id) {
		if r.FolderID == root && r.FolderID != id {
			return true
		}
	}
	return false
}

// DocDetail là chi tiết tài liệu (FN-14 Hình 28).
type DocDetail struct {
	DocItem
	History   []store.DocRow
	Access    []store.DocAccess // nhật ký xem trước/tải xuống — chỉ người quản lý tài liệu
	CanAccess bool
}

// GetDoc trả chi tiết tài liệu: phiên bản hiện hành, lịch sử (tải lên, sửa, ẩn), nhật ký xem/tải (người quản lý).
func (s *Service) GetDoc(ctx context.Context, id uuid.UUID) (DocDetail, error) {
	var d DocDetail
	err := s.docTx(ctx, func(tx pgx.Tx) error {
		c, err := s.docCaller(ctx, tx)
		if err != nil {
			return err
		}
		h, cur, err := docHead(ctx, tx, id)
		if err != nil {
			return err
		}
		t, err := loadTree(ctx, tx)
		if err != nil {
			return err
		}
		m, err := c.visibleDoc(t, cur)
		if err != nil {
			return err
		}
		d.DocItem = DocItem{DocRow: cur, CanManage: !c.readOnly && c.onDoc(m, domain.PermDocManage, cur), Created: h[0].RecordedAt,
			Uploader: h[0].Actor}
		for _, a := range t.chain(cur.FolderID) {
			d.FolderPath = append(d.FolderPath, a.Name)
		}
		if len(cur.StaffIDs) > 0 {
			st, err := store.CurrentStaff(ctx, tx)
			if err != nil {
				return err
			}
			codes := map[uuid.UUID]string{}
			for _, x := range st {
				codes[x.StaffID] = x.Code
			}
			for _, sid := range cur.StaffIDs {
				d.StaffCodes = append(d.StaffCodes, codes[sid])
			}
		}
		d.History = h
		d.CanAccess = c.onDoc(m, domain.PermDocManage, cur)
		if d.CanAccess {
			d.Access, err = store.DocAccessOf(ctx, tx, id, 200)
		}
		return err
	})
	return d, err
}

// Opened là nội dung tài liệu đã giải mã — CHỈ trong bộ nhớ của request.
type Opened struct {
	FileName    string
	ContentType string
	Content     []byte
}

// OpenDoc mở tài liệu để xem trước (preview) hoặc tải xuống (download): kiểm quyền xem, GHI NHẬT KÝ doc_access trước
// (fail-closed), rồi pf/evidence OpenContent(hr_records) — evidence kiểm vai, chủ thể (Resolver của pf/hrm), ghi sổ của nó.
func (s *Service) OpenDoc(ctx context.Context, id uuid.UUID, download bool) (Opened, error) {
	if !s.docsReady() {
		return Opened{}, port.ErrDocsUnavailable
	}
	var cur store.DocRow
	err := s.docTx(ctx, func(tx pgx.Tx) error {
		c, err := s.docCaller(ctx, tx)
		if err != nil {
			return err
		}
		var e error
		if _, cur, e = docHead(ctx, tx, id); e != nil {
			return e
		}
		t, err := loadTree(ctx, tx)
		if err != nil {
			return err
		}
		m, err := c.visibleDoc(t, cur)
		if err != nil {
			return err
		}
		scope := m[domain.PermDocView]
		if !c.onDoc(m, domain.PermDocView, cur) {
			scope = m[domain.PermDocManage]
		}
		action := "preview"
		if download {
			action = "download"
		}
		return store.InsertDocAccess(ctx, tx, c.tenant, store.DocAccess{ID: ids.New(), Action: action, DocID: id, FileRef: cur.FileRef,
			Scope: string(scope), Actor: c.ref}, s.Clock.Now())
	})
	if err != nil {
		return Opened{}, err
	}
	ct, pt, err := s.Docs.Files.OpenContent(ctx, cur.FileRef, string(client.PurposeHRRecords))
	if err != nil {
		return Opened{}, err
	}
	return Opened{FileName: cur.FileName, ContentType: ct, Content: pt}, nil
}

// --- Resolver cho pf/evidence (R-DB-07): chủ thể 'hr_folder'. ---

// FileResolver là Resolver của pf/hrm cho pf/evidence.
type FileResolver struct{ S *Service }

var _ evport.Resolver = FileResolver{}

// Resolve: thư mục có trong tenant người gọi; người gọi có quyền tài liệu ở thư mục → ViaShareGrant (pf/evidence coi là
// thấy được); không có quyền → Resolved rỗng (evidence từ chối denied_scope, trừ phạm vi toàn quốc).
func (r FileResolver) Resolve(ctx context.Context, subjectType string, id uuid.UUID) (evport.Resolved, error) {
	if subjectType != port.SubjectTypeFolder {
		return evport.Resolved{}, evport.ErrSubjectTypeUnsupported
	}
	if r.S == nil || r.S.DB == nil {
		return evport.Resolved{}, evport.ErrResolverUnavailable
	}
	var out evport.Resolved
	err := r.S.docTx(ctx, func(tx pgx.Tx) error {
		t, err := loadTree(ctx, tx)
		if err != nil {
			return err
		}
		if _, ok := t.byID[id]; !ok {
			return evport.ErrSubjectNotFound
		}
		c, err := r.S.docCaller(ctx, tx)
		if err != nil {
			return nil //nolint:nilerr // người gọi không có ngữ cảnh KN: chủ thể có nhưng không thấy
		}
		out.ViaShareGrant = c.seesFolder(c.perm(t, id))
		return nil
	})
	return out, err
}

// AuthorizeFile kiểm lại tài liệu cụ thể khi gọi evidence trực tiếp bằng
// file_ref: quyền xem thư mục không tự cấp quyền mọi tài liệu trong thư mục.
func (r FileResolver) AuthorizeFile(ctx context.Context, file evport.File) (bool, error) {
	if r.S == nil || r.S.DB == nil {
		return false, evport.ErrResolverUnavailable
	}
	if file.Subject == nil || file.Subject.Module != port.SubjectModule || file.Subject.Type != port.SubjectTypeFolder {
		return false, nil
	}
	allowed := false
	err := r.S.docTx(ctx, func(tx pgx.Tx) error {
		id, err := store.DocIDForFile(ctx, tx, file.Ref)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, cur, err := docHead(ctx, tx, id)
		if err != nil {
			return err
		}
		if cur.FileRef != file.Ref || cur.FolderID != file.Subject.ID {
			return nil
		}
		c, err := r.S.docCaller(ctx, tx)
		if err != nil {
			return nil
		} // không có ngữ cảnh nghiệp vụ hợp lệ: từ chối
		tree, err := loadTree(ctx, tx)
		if err != nil {
			return err
		}
		_, err = c.visibleDoc(tree, cur)
		allowed = err == nil
		return nil
	})
	return allowed, err
}

var _ evport.FileAuthorizer = FileResolver{}
