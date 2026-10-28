package app

// Kho văn bản phòng (FN-20, UR-TINH-25): thư mục, văn bản có phiên bản, tệp ở kho đối tượng (khóa theo băm, ghi một
// lần — không lưu nội dung trong DB), "xóa" = ẩn có lý do, hết hiệu lực vẫn lưu và có nhãn.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

var libraryRoles = []string{port.SessionRoleAdmin, port.SessionRoleProvince, port.SessionRoleCommune}

// readerOf: Admin, Cấp Tỉnh, Cấp Xã của phòng (FN-20). Cấp TW không có.
func (s *Service) readerOf(ctx context.Context, dept string) (caller, error) {
	return s.resolve(ctx, dept, libraryRoles...)
}

// writerOf: Admin, Cấp Tỉnh của phòng (CRU), không ở ngữ cảnh chỉ xem.
func (s *Service) writerOf(ctx context.Context, dept string) (caller, error) {
	c, err := s.resolve(ctx, dept, libraryRoles...)
	if err != nil {
		return c, err
	}
	if c.commune() {
		return c, problem.Forbidden.WithDetail("Cấp Xã chỉ xem, tải văn bản (FN-20)")
	}
	if err := c.checkWrite(); err != nil {
		return c, err
	}
	return c, nil
}

// managerOf: Admin, Lãnh đạo phòng — ẩn/hiện lại văn bản, thư mục (FN-20 "D" của Lãnh đạo phòng).
func (s *Service) managerOf(ctx context.Context, dept string) (caller, error) {
	c, err := s.writerOf(ctx, dept)
	if err != nil {
		return c, err
	}
	if !c.manager() {
		return c, problem.Forbidden.WithDetail("chỉ Lãnh đạo phòng ẩn, hiện lại văn bản và thư mục")
	}
	return c, nil
}

func folderKey(ref *uuid.UUID) uuid.UUID {
	if ref == nil {
		return uuid.Nil
	}
	return *ref
}

// hiddenSet trả các thư mục bị ẩn (kể cả do thư mục cha bị ẩn).
func hiddenSet(fs []store.FolderRow) map[uuid.UUID]bool {
	by := map[uuid.UUID]store.FolderRow{}
	for _, f := range fs {
		by[f.Ref] = f
	}
	out := map[uuid.UUID]bool{}
	for _, f := range fs {
		cur, depth := f, 0
		for depth <= domain.MaxFolderDeep+1 {
			if cur.Hidden {
				out[f.Ref] = true
				break
			}
			if cur.ParentRef == nil {
				break
			}
			p, ok := by[*cur.ParentRef]
			if !ok {
				break
			}
			cur, depth = p, depth+1
		}
	}
	return out
}

// folderPath trả tên các thư mục từ gốc tới ref.
func folderPath(fs []store.FolderRow, ref *uuid.UUID) []string {
	if ref == nil {
		return []string{}
	}
	by := map[uuid.UUID]store.FolderRow{}
	for _, f := range fs {
		by[f.Ref] = f
	}
	var out []string
	cur := ref
	for i := 0; cur != nil && i <= domain.MaxFolderDeep+1; i++ {
		f, ok := by[*cur]
		if !ok {
			break
		}
		out = append([]string{f.Name}, out...)
		cur = f.ParentRef
	}
	return out
}

func depthOf(fs []store.FolderRow, ref uuid.UUID) int {
	return len(folderPath(fs, &ref))
}

// visibleTo: Cấp Xã chỉ xem văn bản có đối tượng "xa"; Admin, Cấp Tỉnh xem mọi văn bản của phòng.
func visibleTo(c caller, d store.DocRow) bool {
	return !c.commune() || slices.Contains(d.Audience, domain.AudienceXa)
}

// ---------------------------------------------------------------------------
// Thư mục
// ---------------------------------------------------------------------------

// FolderView là thư mục hiện hành kèm đường dẫn và số văn bản đang hiện (người gọi xem được).
type FolderView struct {
	store.FolderRow
	Path        []string
	Docs        int
	HiddenChain bool // bị ẩn trực tiếp hoặc do thư mục cha
}

// FolderList là cây thư mục phẳng của phòng.
type FolderList struct {
	Department string
	CanWrite   bool
	CanManage  bool
	Items      []FolderView
}

// Folders liệt kê thư mục của phòng (thư mục ẩn chỉ trả cho Admin/Lãnh đạo phòng khi includeHidden).
func (s *Service) Folders(ctx context.Context, dept string, includeHidden bool) (FolderList, error) {
	c, err := s.readerOf(ctx, dept)
	if err != nil {
		return FolderList{}, err
	}
	var fs []store.FolderRow
	var docs []store.DocRow
	if err := s.readTx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var err error
		if fs, err = store.FoldersCurrent(ctx, tx, c.dept); err != nil {
			return err
		}
		docs, err = store.DocsCurrent(ctx, tx, c.dept)
		return err
	}); err != nil {
		return FolderList{}, err
	}
	hidden := hiddenSet(fs)
	count := map[uuid.UUID]int{}
	for _, d := range docs {
		if !d.Hidden && visibleTo(c, d) {
			count[folderKey(d.FolderRef)]++
		}
	}
	out := FolderList{Department: c.dept, CanWrite: c.writer(), CanManage: c.manager(), Items: []FolderView{}}
	for _, f := range fs {
		if hidden[f.Ref] && (!includeHidden || !c.manager()) {
			continue
		}
		out.Items = append(out.Items, FolderView{FolderRow: f, Path: folderPath(fs, &f.Ref), Docs: count[f.Ref], HiddenChain: hidden[f.Ref]})
	}
	slices.SortStableFunc(out.Items, func(a, b FolderView) int {
		return strings.Compare(strings.Join(a.Path, "/"), strings.Join(b.Path, "/"))
	})
	return out, nil
}

// FolderInput là lần tạo thư mục.
type FolderInput struct {
	Department string
	ParentRef  *uuid.UUID
	Name       string
}

func checkSibling(fs []store.FolderRow, parent *uuid.UUID, name string, self uuid.UUID) error {
	for _, f := range fs {
		if f.Ref != self && folderKey(f.ParentRef) == folderKey(parent) && strings.EqualFold(f.Name, name) {
			return ErrFolderExists.WithField("name", "trùng tên thư mục cùng cấp")
		}
	}
	return nil
}

func findFolder(fs []store.FolderRow, ref uuid.UUID) (store.FolderRow, bool) {
	for _, f := range fs {
		if f.Ref == ref {
			return f, true
		}
	}
	return store.FolderRow{}, false
}

// CreateFolder tạo thư mục (Admin, Cấp Tỉnh của phòng).
func (s *Service) CreateFolder(ctx context.Context, in FolderInput, idem string) (store.FolderRow, error) {
	if idem == "" {
		return store.FolderRow{}, problem.IdempotencyRequired
	}
	c, err := s.writerOf(ctx, in.Department)
	if err != nil {
		return store.FolderRow{}, err
	}
	name, ok := domain.FolderName(in.Name)
	if !ok {
		return store.FolderRow{}, ErrFolderInvalid.WithField("name", "bắt buộc, tối đa 200 ký tự, không có '/' '\\'")
	}
	var out store.FolderRow
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.FolderByIdem(ctx, tx, idem); err != nil || prev != nil {
			if err != nil {
				return err
			}
			if err := c.replayable(prev.Department, prev.ActorRef, prev.Event, domain.FolderCreated); err != nil {
				return err
			}
			out = *prev
			return nil
		}
		if err := store.Lock(ctx, tx, "folders:"+c.dept); err != nil {
			return err
		}
		fs, err := store.FoldersCurrent(ctx, tx, c.dept)
		if err != nil {
			return err
		}
		if in.ParentRef != nil {
			p, ok := findFolder(fs, *in.ParentRef)
			if !ok || hiddenSet(fs)[p.Ref] {
				return ErrFolderNotFound.WithField("parent_ref", "không có thư mục cha (hoặc đã ẩn) trong phòng")
			}
			if depthOf(fs, p.Ref) >= domain.MaxFolderDeep {
				return ErrFolderInvalid.WithField("parent_ref", "tối đa 5 cấp thư mục")
			}
		}
		if err := checkSibling(fs, in.ParentRef, name, uuid.Nil); err != nil {
			return err
		}
		out = store.FolderRow{Ref: ids.New(), Department: c.dept, ParentRef: in.ParentRef, Name: name, Meta: c.meta(domain.FolderCreated, s.now(), idem)}
		return store.InsertFolder(ctx, tx, c.p.TenantID, &out)
	})
	if dup(err) {
		return out, ErrFolderConflict
	}
	return out, err
}

// FolderChange là đổi tên / ẩn / hiện lại thư mục.
type FolderChange struct {
	Department  string
	Ref         uuid.UUID
	Event       string // renamed | hidden | restored
	Name        string
	Reason      string
	BaseVersion int64
}

// ChangeFolder đổi tên (Cấp Tỉnh), ẩn/hiện lại (Lãnh đạo phòng) một thư mục. Ẩn chỉ khi không còn thư mục con/văn bản
// đang hiện.
func (s *Service) ChangeFolder(ctx context.Context, in FolderChange, idem string) (store.FolderRow, error) {
	if idem == "" {
		return store.FolderRow{}, problem.IdempotencyRequired
	}
	var c caller
	var err error
	if in.Event == domain.FolderRenamed {
		c, err = s.writerOf(ctx, in.Department)
	} else {
		c, err = s.managerOf(ctx, in.Department)
	}
	if err != nil {
		return store.FolderRow{}, err
	}
	reason := strings.TrimSpace(in.Reason)
	if in.Event != domain.FolderRenamed && (utf8.RuneCountInString(reason) < domain.MinReason || utf8.RuneCountInString(reason) > domain.MaxReason) {
		return store.FolderRow{}, ErrFolderInvalid.WithField("reason", "bắt buộc, 5–1000 ký tự")
	}
	var out store.FolderRow
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.FolderByIdem(ctx, tx, idem); err != nil || prev != nil {
			if err != nil {
				return err
			}
			if err := c.replayable(prev.Department, prev.ActorRef, prev.Event, in.Event); err != nil {
				return err
			}
			if prev.Ref != in.Ref {
				return problem.IdempotencyConflict
			}
			out = *prev
			return nil
		}
		if err := store.Lock(ctx, tx, "folders:"+c.dept); err != nil {
			return err
		}
		fs, err := store.FoldersCurrent(ctx, tx, c.dept)
		if err != nil {
			return err
		}
		cur, ok := findFolder(fs, in.Ref)
		if !ok {
			return ErrFolderNotFound
		}
		if cur.Seq != in.BaseVersion {
			return ErrFolderConflict.With("current_version", cur.Seq)
		}
		out = cur
		out.Meta = c.meta(in.Event, s.now(), idem)
		out.StatusReason = nil
		switch in.Event {
		case domain.FolderRenamed:
			name, ok := domain.FolderName(in.Name)
			if !ok {
				return ErrFolderInvalid.WithField("name", "bắt buộc, tối đa 200 ký tự, không có '/' '\\'")
			}
			if err := checkSibling(fs, cur.ParentRef, name, cur.Ref); err != nil {
				return err
			}
			out.Name = name
		case domain.FolderHidden:
			if cur.Hidden {
				return ErrDocState.WithDetail("thư mục đã ẩn")
			}
			for _, f := range fs {
				if f.ParentRef != nil && *f.ParentRef == cur.Ref && !f.Hidden {
					return ErrFolderNotEmpty
				}
			}
			docs, err := store.DocsCurrent(ctx, tx, c.dept)
			if err != nil {
				return err
			}
			for _, d := range docs {
				if d.FolderRef != nil && *d.FolderRef == cur.Ref && !d.Hidden {
					return ErrFolderNotEmpty
				}
			}
			out.Hidden, out.StatusReason = true, &reason
		case domain.FolderRestored:
			if !cur.Hidden {
				return ErrDocState.WithDetail("thư mục đang hiện")
			}
			out.Hidden, out.StatusReason = false, &reason
		default:
			return ErrFolderInvalid
		}
		return store.InsertFolder(ctx, tx, c.p.TenantID, &out)
	})
	if dup(err) {
		return out, ErrFolderConflict
	}
	return out, err
}

// ---------------------------------------------------------------------------
// Văn bản
// ---------------------------------------------------------------------------

// DocView là văn bản hiện hành kèm trạng thái hiệu lực, đường dẫn thư mục, quyền của người gọi.
type DocView struct {
	store.DocRow
	Validity   string
	FolderPath []string
	ActorName  string
	CanEdit    bool
	CanManage  bool
	Versions   []store.DocRow // chỉ ở chi tiết, mới nhất trước
}

func (s *Service) docView(c caller, d store.DocRow, fs []store.FolderRow, today time.Time, versions []store.DocRow) DocView {
	return DocView{DocRow: d, Validity: domain.Validity(d.EffectiveFrom, d.EffectiveTo, today), FolderPath: folderPath(fs, d.FolderRef),
		CanEdit: c.writer(), CanManage: c.manager(), Versions: versions}
}

// DocFilter lọc danh sách văn bản (FN-20 B1: lọc, tìm).
type DocFilter struct {
	Department    string
	Folder        *uuid.UUID // nil = mọi thư mục
	Query         string     // tìm trong tên, số hiệu, cơ quan ban hành, lĩnh vực, trích yếu
	Kind          string
	Validity      string // "" | active | pending | expired
	IncludeHidden bool   // Admin, Lãnh đạo phòng
}

// DocList là trang kết quả.
type DocList struct {
	Department string
	CanWrite   bool
	CanManage  bool
	Total      int
	Items      []DocView
}

// ListDocs liệt kê văn bản hiện hành của phòng (hết hiệu lực vẫn có — nhãn "Hết hiệu lực", BR-01, AC-03).
func (s *Service) ListDocs(ctx context.Context, f DocFilter) (DocList, error) {
	c, err := s.readerOf(ctx, f.Department)
	if err != nil {
		return DocList{}, err
	}
	if f.Kind != "" && !slices.Contains(domain.Kinds, f.Kind) {
		return DocList{}, problem.Validation.WithField("kind", "loại văn bản không hợp lệ")
	}
	if f.Validity != "" && !slices.Contains([]string{domain.ValidityActive, domain.ValidityPending, domain.ValidityExpired}, f.Validity) {
		return DocList{}, problem.Validation.WithField("validity", "active | pending | expired")
	}
	var fs []store.FolderRow
	var docs []store.DocRow
	if err := s.readTx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var err error
		if fs, err = store.FoldersCurrent(ctx, tx, c.dept); err != nil {
			return err
		}
		docs, err = store.DocsCurrent(ctx, tx, c.dept)
		return err
	}); err != nil {
		return DocList{}, err
	}
	today := s.today()
	hidden := hiddenSet(fs)
	showHidden := f.IncludeHidden && c.manager()
	q := strings.ToLower(domain.OneLine(f.Query))
	out := DocList{Department: c.dept, CanWrite: c.writer(), CanManage: c.manager(), Items: []DocView{}}
	for _, d := range docs {
		if !visibleTo(c, d) || ((d.Hidden || hidden[folderKey(d.FolderRef)]) && !showHidden) {
			continue
		}
		if f.Folder != nil && folderKey(d.FolderRef) != *f.Folder {
			continue
		}
		if f.Kind != "" && d.Kind != f.Kind {
			continue
		}
		v := s.docView(c, d, fs, today, nil)
		if f.Validity != "" && v.Validity != f.Validity {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(strings.Join([]string{d.Title, d.Number, d.Issuer, d.Field, d.Summary, d.Name}, " ")), q) {
			continue
		}
		out.Items = append(out.Items, v)
	}
	out.Total = len(out.Items)
	refs := make([]uuid.UUID, 0, len(out.Items))
	for _, d := range out.Items {
		refs = append(refs, d.ActorRef)
	}
	nm := s.names(ctx, refs)
	for i := range out.Items {
		out.Items[i].ActorName = nm[out.Items[i].ActorRef]
	}
	return out, nil
}

// loadDoc đọc lịch sử văn bản và kiểm người gọi xem được (khác phòng, ẩn với người không quản lý, Cấp Xã không thuộc đối
// tượng → 404 — không lộ sự tồn tại).
func (s *Service) loadDoc(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID) ([]store.DocRow, []store.FolderRow, error) {
	hist, err := store.DocHistory(ctx, tx, ref)
	if err != nil {
		return nil, nil, err
	}
	if len(hist) == 0 || hist[0].Department != c.dept || !visibleTo(c, hist[0]) {
		return nil, nil, ErrDocNotFound
	}
	fs, err := store.FoldersCurrent(ctx, tx, c.dept)
	if err != nil {
		return nil, nil, err
	}
	if (hist[0].Hidden || hiddenSet(fs)[folderKey(hist[0].FolderRef)]) && !c.manager() {
		return nil, nil, ErrDocNotFound
	}
	return hist, fs, nil
}

// GetDoc trả văn bản hiện hành và mọi phiên bản.
func (s *Service) GetDoc(ctx context.Context, dept string, ref uuid.UUID) (DocView, error) {
	c, err := s.readerOf(ctx, dept)
	if err != nil {
		return DocView{}, err
	}
	var hist []store.DocRow
	var fs []store.FolderRow
	if err := s.readTx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var err error
		hist, fs, err = s.loadDoc(ctx, tx, c, ref)
		return err
	}); err != nil {
		return DocView{}, err
	}
	v := s.docView(c, hist[0], fs, s.today(), hist)
	refs := make([]uuid.UUID, 0, len(hist))
	for _, h := range hist {
		refs = append(refs, h.ActorRef)
	}
	v.ActorName = s.names(ctx, refs)[v.ActorRef]
	return v, nil
}

// DocInput là thuộc tính văn bản khi tạo/sửa.
type DocInput struct {
	Department  string
	FolderRef   *uuid.UUID
	Meta        domain.DocMeta
	BaseVersion int64 // sửa/tải tệp mới/ẩn: phiên bản hiện hành (khóa lạc quan)
}

// FileInput là tệp tải lên (bản rõ — văn bản công khai trong ngành, xem migration).
type FileInput struct {
	Name string
	Data []byte
}

func (s *Service) checkFolder(ctx context.Context, tx pgx.Tx, dept string, ref *uuid.UUID) error {
	if ref == nil {
		return nil
	}
	fs, err := store.FoldersCurrent(ctx, tx, dept)
	if err != nil {
		return err
	}
	if _, ok := findFolder(fs, *ref); !ok || hiddenSet(fs)[*ref] {
		return ErrFolderNotFound.WithField("folder_ref", "không có thư mục (hoặc đã ẩn) trong phòng")
	}
	return nil
}

// putFile kiểm định dạng, dung lượng (≤ 20 MB — AC-02) rồi ghi tệp vào kho đối tượng (khóa theo băm — ghi lại cùng nội
// dung là bản cũ).
func (s *Service) putFile(ctx context.Context, c caller, ref uuid.UUID, in FileInput) (store.File, error) {
	if s.Store == nil {
		return store.File{}, ErrDependency
	}
	if len(in.Data) == 0 {
		return store.File{}, ErrDocInvalid.WithField("file", "tệp rỗng")
	}
	if len(in.Data) > domain.MaxFileBytes {
		return store.File{}, ErrFileTooLarge.WithDetail("tối đa 20 MB mỗi tệp (FN-20 AC-02)")
	}
	ct, name, ok := domain.FileType(in.Name, in.Data)
	if !ok {
		return store.File{}, ErrFileType.WithDetail("chỉ nhận " + strings.Join(domain.FileExtensions, ", ") + " đúng định dạng")
	}
	sum := objstore.SHA256Hex(in.Data)
	key := "dept-docs/" + c.p.TenantID.String() + "/" + c.dept + "/" + ref.String() + "/" + sum
	if _, err := s.Store.Put(ctx, key, bytes.NewReader(in.Data), int64(len(in.Data)), sum, ct); err != nil {
		return store.File{}, err
	}
	return store.File{ObjectKey: key, Name: name, ContentType: ct, Size: int64(len(in.Data)), SHA256: sum}, nil
}

// CreateDoc thêm văn bản (FN-20 B3) — Admin, Cấp Tỉnh của phòng.
func (s *Service) CreateDoc(ctx context.Context, in DocInput, file FileInput, idem string) (DocView, error) {
	if idem == "" {
		return DocView{}, problem.IdempotencyRequired
	}
	c, err := s.writerOf(ctx, in.Department)
	if err != nil {
		return DocView{}, err
	}
	meta := in.Meta
	if es := meta.Validate(); len(es) > 0 {
		return DocView{}, fieldsProblem(ErrDocInvalid, es)
	}
	// Phát lại: cùng khóa → bản đã ghi (không ghi tệp lần nữa).
	var prev *store.DocRow
	if err := s.readTx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var err error
		if prev, err = store.DocByIdem(ctx, tx, idem); err != nil || prev != nil {
			return err
		}
		return s.checkFolder(ctx, tx, c.dept, in.FolderRef)
	}); err != nil {
		return DocView{}, err
	}
	if prev != nil {
		return s.GetDoc(ctx, in.Department, prev.Ref)
	}
	ref := ids.New()
	f, err := s.putFile(ctx, c, ref, file)
	if err != nil {
		return DocView{}, err
	}
	f.Version = 1
	row := store.DocRow{Ref: ref, Department: c.dept, FolderRef: in.FolderRef, DocMeta: meta, File: f, Meta: c.meta(domain.DocCreated, s.now(), idem)}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := s.checkFolder(ctx, tx, c.dept, in.FolderRef); err != nil {
			return err
		}
		return store.InsertDoc(ctx, tx, c.p.TenantID, &row)
	})
	if dup(err) {
		return DocView{}, ErrDocConflict
	}
	if err != nil {
		return DocView{}, err
	}
	return s.GetDoc(ctx, in.Department, ref)
}

// changeDoc ghi phiên bản mới của văn bản ref sau khi mutate sửa bản hiện hành (khóa lạc quan base).
func (s *Service) changeDoc(ctx context.Context, c caller, ref uuid.UUID, base int64, event, idem string, mutate func(tx pgx.Tx, cur store.DocRow, next *store.DocRow) error) (DocView, error) {
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		// Phát lại cùng khóa: không ghi thêm, trả bản hiện hành.
		if prev, err := store.DocByIdem(ctx, tx, idem); err != nil || prev != nil {
			return err
		}
		if err := store.Lock(ctx, tx, "doc:"+ref.String()); err != nil {
			return err
		}
		hist, _, err := s.loadDoc(ctx, tx, c, ref)
		if err != nil {
			return err
		}
		cur := hist[0]
		if cur.Seq != base {
			return ErrDocConflict.With("current_version", cur.Seq)
		}
		next := cur
		next.Meta = c.meta(event, s.now(), idem)
		next.StatusReason = nil
		if err := mutate(tx, cur, &next); err != nil {
			return err
		}
		return store.InsertDoc(ctx, tx, c.p.TenantID, &next)
	})
	if dup(err) {
		return DocView{}, ErrDocConflict
	}
	if err != nil {
		return DocView{}, err
	}
	return s.GetDoc(ctx, c.dept, ref)
}

// UpdateDoc sửa thuộc tính, chuyển thư mục (giữ tệp) — Admin, Cấp Tỉnh.
func (s *Service) UpdateDoc(ctx context.Context, ref uuid.UUID, in DocInput, idem string) (DocView, error) {
	if idem == "" {
		return DocView{}, problem.IdempotencyRequired
	}
	c, err := s.writerOf(ctx, in.Department)
	if err != nil {
		return DocView{}, err
	}
	meta := in.Meta
	if es := meta.Validate(); len(es) > 0 {
		return DocView{}, fieldsProblem(ErrDocInvalid, es)
	}
	return s.changeDoc(ctx, c, ref, in.BaseVersion, domain.DocUpdated, idem, func(tx pgx.Tx, cur store.DocRow, next *store.DocRow) error {
		if cur.Hidden {
			return ErrDocState.WithDetail("văn bản đang ẩn — hiện lại trước khi sửa")
		}
		if err := s.checkFolder(ctx, tx, c.dept, in.FolderRef); err != nil {
			return err
		}
		next.DocMeta, next.FolderRef = meta, in.FolderRef
		return nil
	})
}

// ReplaceFile tải tệp mới cho văn bản (phiên bản tệp + 1; tệp cũ vẫn tải được) — Admin, Cấp Tỉnh.
func (s *Service) ReplaceFile(ctx context.Context, dept string, ref uuid.UUID, base int64, file FileInput, idem string) (DocView, error) {
	if idem == "" {
		return DocView{}, problem.IdempotencyRequired
	}
	c, err := s.writerOf(ctx, dept)
	if err != nil {
		return DocView{}, err
	}
	// Kiểm quyền xem văn bản trước khi ghi tệp vào kho.
	if err := s.readTx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		_, _, err := s.loadDoc(ctx, tx, c, ref)
		return err
	}); err != nil {
		return DocView{}, err
	}
	f, err := s.putFile(ctx, c, ref, file)
	if err != nil {
		return DocView{}, err
	}
	return s.changeDoc(ctx, c, ref, base, domain.DocFileReplaced, idem, func(_ pgx.Tx, cur store.DocRow, next *store.DocRow) error {
		if cur.Hidden {
			return ErrDocState.WithDetail("văn bản đang ẩn — hiện lại trước khi tải tệp mới")
		}
		f.Version = cur.Version + 1
		next.File = f
		return nil
	})
}

// SetDocHidden ẩn ("xóa") hoặc hiện lại văn bản, có lý do — Admin, Lãnh đạo phòng.
func (s *Service) SetDocHidden(ctx context.Context, dept string, ref uuid.UUID, hidden bool, base int64, reason, idem string) (DocView, error) {
	if idem == "" {
		return DocView{}, problem.IdempotencyRequired
	}
	c, err := s.managerOf(ctx, dept)
	if err != nil {
		return DocView{}, err
	}
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < domain.MinReason || n > domain.MaxReason {
		return DocView{}, ErrDocInvalid.WithField("reason", "bắt buộc, 5–1000 ký tự")
	}
	event := domain.DocRestored
	if hidden {
		event = domain.DocHidden
	}
	return s.changeDoc(ctx, c, ref, base, event, idem, func(_ pgx.Tx, cur store.DocRow, next *store.DocRow) error {
		if cur.Hidden == hidden {
			return ErrDocState
		}
		next.Hidden, next.StatusReason = hidden, &reason
		return nil
	})
}

// Opened là nội dung tệp để tải xuống.
type Opened struct {
	File   store.File
	Reader io.ReadCloser
}

// OpenFile mở tệp của văn bản (phiên bản version của văn bản; 0 = hiện hành) — FN-20 B2 xem, tải.
func (s *Service) OpenFile(ctx context.Context, dept string, ref uuid.UUID, version int64) (Opened, error) {
	c, err := s.readerOf(ctx, dept)
	if err != nil {
		return Opened{}, err
	}
	if s.Store == nil {
		return Opened{}, ErrDependency
	}
	var row store.DocRow
	if err := s.readTx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		hist, _, err := s.loadDoc(ctx, tx, c, ref)
		if err != nil {
			return err
		}
		row = hist[0]
		if version > 0 {
			i := slices.IndexFunc(hist, func(h store.DocRow) bool { return h.Seq == version })
			if i < 0 {
				return ErrDocNotFound.WithDetail("không có phiên bản này")
			}
			row = hist[i]
		}
		return nil
	}); err != nil {
		return Opened{}, err
	}
	rc, _, err := s.Store.Get(ctx, row.ObjectKey)
	if errors.Is(err, objstore.ErrNotFound) {
		return Opened{}, ErrDocNotFound.WithDetail("tệp không còn trong kho")
	}
	if err != nil {
		return Opened{}, err
	}
	return Opened{File: row.File, Reader: rc}, nil
}
