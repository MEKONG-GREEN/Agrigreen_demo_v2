package app

// Ảnh thực địa của báo cáo kiểm tra Mẫu 03 (W11.53b, #158 — BRD CNTS v3.7 UR-TINH-21 "kiểm tra thực địa (kèm ảnh)").
//
// Ảnh JPEG/PNG ≤ 5 MB (kiểm đuôi + chữ ký đầu tệp), tối đa 20 ảnh hiện hành mỗi báo cáo, chú thích ≤ 300 ký tự (không số
// điện thoại/CCCD). Tệp ghi vào kho đối tượng (cùng kho của bản scan đã ký — khóa theo băm) TRƯỚC giao dịch; sổ
// pf_extension.cnts_knqg_photo chỉ giữ khóa, tên, kiểu, cỡ, SHA-256. Thêm/gỡ ảnh: người sửa được biểu (chủ nhiệm của dự án,
// lãnh đạo phòng, Admin — FN-19 BR-01) khi biểu đang Đang lập (đã ký → bản điều chỉnh mới thêm/gỡ được); xem/tải ảnh: người
// xem được biểu. Ghi tuần tự theo dự án (khóa tư vấn), phát lại theo Idempotency-Key (#112).
//
// Giả định (ghi ở docs/kn/gia-dinh/W11.53b.md): ảnh là hồ sơ hành chính của đợt kiểm tra (như bản scan) — không mã hóa
// phong bì theo chủ hộ như ảnh nhật ký; chưa in ảnh vào Word (mẫu Word gốc của phòng không có khung ảnh).

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/store"
	mdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	exdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Mã lỗi ảnh thực địa (docs/errors/pf-extension-cnts-knqg.md).
var (
	ErrPhotoInvalid  = problem.New(http.StatusUnprocessableEntity, "extension.cnts_knqg_photo_invalid", "Ảnh thực địa phải là JPG hoặc PNG, tối đa 5 MB; tối đa 20 ảnh mỗi báo cáo")
	ErrPhotoNotFound = problem.New(http.StatusNotFound, "extension.cnts_knqg_photo_not_found", "Không có ảnh thực địa này trong báo cáo")
)

// PhotoList là ảnh hiện hành của biểu kèm tên người tải và quyền thêm/gỡ của người gọi.
type PhotoList struct {
	Items    []store.Photo
	Names    map[uuid.UUID]string
	CanEdit  bool
	Replayed bool
}

// AddPhotoInput là một ảnh tải lên.
type AddPhotoInput struct {
	Ref      uuid.UUID
	FileName string
	Data     []byte
	Caption  string
	IdemKey  string
}

func idemPhoto(k string) string { return "knqgphoto:" + k }

// photoDoc đọc biểu hiện hành và dự án; kiểm người gọi xem được biểu.
func (s *Service) photoDoc(ctx context.Context, c caller, ref uuid.UUID) (*store.Doc, *pctx, error) {
	_, cur, pc, _, err := s.loadDoc(ctx, c, ref, "")
	if err != nil {
		return nil, nil, err
	}
	return cur, pc, nil
}

// photoWritable kiểm thêm/gỡ ảnh: Mẫu 03, người sửa được biểu, dự án chưa kết thúc, biểu đang lập.
func photoWritable(c caller, pc *pctx, d store.Doc) error {
	switch {
	case !domain.PhotoForms(d.Form):
		return ErrPhotoInvalid.WithDetail("chỉ báo cáo kiểm tra Mẫu 03 có ảnh thực địa")
	case !c.editor(pc.p):
		return problem.Forbidden.WithDetail("chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin thêm/gỡ ảnh (FN-19 BR-01)")
	case pc.p.Event == mdomain.EventClosed:
		return ErrProjectClose
	case !domain.Editable(d.Status):
		return ErrDocLocked.WithDetail("báo cáo đã trình ký/ký — lập bản điều chỉnh để thêm/gỡ ảnh")
	}
	return nil
}

// Photos liệt kê ảnh hiện hành của biểu (người xem được biểu).
func (s *Service) Photos(ctx context.Context, ref uuid.UUID) (PhotoList, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return PhotoList{}, err
	}
	cur, pc, err := s.photoDoc(ctx, c, ref)
	if err != nil {
		return PhotoList{}, err
	}
	return s.photoList(ctx, c, cur, pc)
}

func (s *Service) photoList(ctx context.Context, c caller, cur *store.Doc, pc *pctx) (PhotoList, error) {
	var items []store.Photo
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.Photos(ctx, tx, cur.Ref)
		return e
	}); err != nil {
		return PhotoList{}, err
	}
	refs := make([]uuid.UUID, 0, len(items))
	for _, p := range items {
		refs = append(refs, p.ActorRef)
	}
	return PhotoList{Items: items, Names: s.names(ctx, refs), CanEdit: domain.PhotoForms(cur.Form) && photoWritable(c, pc, *cur) == nil}, nil
}

// AddPhoto tải một ảnh thực địa lên báo cáo Mẫu 03 đang lập.
func (s *Service) AddPhoto(ctx context.Context, in AddPhotoInput) (PhotoList, error) {
	c, err := s.writer(ctx, in.IdemKey, false)
	if err != nil {
		return PhotoList{}, err
	}
	st := s.objects()
	if st == nil {
		return PhotoList{}, ErrDependency.WithDetail("chưa nối kho tệp")
	}
	if len(in.Data) == 0 || len(in.Data) > domain.MaxPhotoBytes {
		return PhotoList{}, ErrPhotoInvalid.WithField("file", "ảnh 1 byte – 5 MB")
	}
	ct, name, ok := domain.PhotoType(in.FileName, in.Data)
	if !ok {
		return PhotoList{}, ErrPhotoInvalid.WithField("file", "chỉ nhận ảnh .jpg/.jpeg/.png đúng định dạng")
	}
	caption, ok := domain.NormalizeCaption(in.Caption)
	if !ok {
		return PhotoList{}, ErrPhotoInvalid.WithField("caption", "tối đa 300 ký tự")
	}
	if exdomain.ContainsPII(caption) {
		return PhotoList{}, ErrPhotoInvalid.WithField("caption", exdomain.ErrPIIInText.Error())
	}
	idem := idemPhoto(in.IdemKey)
	if replayed, err := s.photoReplay(ctx, c, idem, in.Ref); replayed || err != nil {
		if err != nil {
			return PhotoList{}, err
		}
		return s.photosReplayed(ctx, c, in.Ref)
	}
	// Kiểm quyền, trạng thái trước khi ghi tệp (không ghi tệp rác cho người không có quyền).
	cur, pc, err := s.photoDoc(ctx, c, in.Ref)
	if err != nil {
		return PhotoList{}, err
	}
	if err := photoWritable(c, pc, *cur); err != nil {
		return PhotoList{}, err
	}
	sum := objstore.SHA256Hex(in.Data)
	key := "cnts-knqg-photo/" + c.p.TenantID.String() + "/" + in.Ref.String() + "/" + sum
	if _, err := st.Put(ctx, key, bytes.NewReader(in.Data), int64(len(in.Data)), sum, ct); err != nil {
		return PhotoList{}, err
	}
	now := s.now()
	m := c.meta(now, idem)
	p := store.Photo{Meta: m, DocRef: in.Ref, PhotoRef: ids.New(), Event: store.PhotoAdded, Key: key, Name: name, Type: ct,
		Size: int64(len(in.Data)), SHA256: sum, Caption: caption}
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		latest, pc2, err := s.photoLockDoc(ctx, tx, cur)
		if err != nil {
			return err
		}
		if prev, err := store.PhotoByIdem(ctx, tx, idem); err != nil || prev != nil {
			replayed = prev != nil
			return err
		}
		if err := photoWritable(c, pc2, *latest); err != nil {
			return err
		}
		items, err := store.Photos(ctx, tx, in.Ref)
		if err != nil {
			return err
		}
		if len(items) >= domain.MaxPhotos {
			return ErrPhotoInvalid.WithField("file", "tối đa 20 ảnh mỗi báo cáo — gỡ bớt ảnh trước")
		}
		return store.InsertPhoto(ctx, tx, c.p.TenantID, &p)
	})
	if dup(err) || (err == nil && replayed) {
		return s.photosReplayed(ctx, c, in.Ref)
	}
	if err != nil {
		return PhotoList{}, err
	}
	return s.photoList(ctx, c, cur, pc)
}

// RemovePhoto gỡ ảnh khỏi báo cáo Mẫu 03 đang lập (dòng sổ removed; tệp vẫn ở kho).
func (s *Service) RemovePhoto(ctx context.Context, ref, photo uuid.UUID, idemKey string) (PhotoList, error) {
	c, err := s.writer(ctx, idemKey, false)
	if err != nil {
		return PhotoList{}, err
	}
	idem := idemPhoto(idemKey)
	if replayed, err := s.photoReplay(ctx, c, idem, ref); replayed || err != nil {
		if err != nil {
			return PhotoList{}, err
		}
		return s.photosReplayed(ctx, c, ref)
	}
	cur, pc, err := s.photoDoc(ctx, c, ref)
	if err != nil {
		return PhotoList{}, err
	}
	if err := photoWritable(c, pc, *cur); err != nil {
		return PhotoList{}, err
	}
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		latest, pc2, err := s.photoLockDoc(ctx, tx, cur)
		if err != nil {
			return err
		}
		if prev, err := store.PhotoByIdem(ctx, tx, idem); err != nil || prev != nil {
			replayed = prev != nil
			return err
		}
		if err := photoWritable(c, pc2, *latest); err != nil {
			return err
		}
		added, gone, err := store.PhotoAt(ctx, tx, ref, photo)
		if err != nil {
			return err
		}
		if added == nil || gone {
			return ErrPhotoNotFound
		}
		r := *added
		r.Meta = c.meta(s.now(), idem)
		r.Event = store.PhotoRemoved
		return store.InsertPhoto(ctx, tx, c.p.TenantID, &r)
	})
	if dup(err) || (err == nil && replayed) {
		return s.photosReplayed(ctx, c, ref)
	}
	if err != nil {
		return PhotoList{}, err
	}
	return s.photoList(ctx, c, cur, pc)
}

// photoLockDoc khóa dự án, đọc lại biểu và dự án trong giao dịch ghi.
func (s *Service) photoLockDoc(ctx context.Context, tx pgx.Tx, cur *store.Doc) (*store.Doc, *pctx, error) {
	if err := store.Lock(ctx, tx, "proj:"+cur.ProjectRef.String()); err != nil {
		return nil, nil, err
	}
	latest, err := store.Latest(ctx, tx, cur.Ref)
	if err != nil {
		return nil, nil, err
	}
	if latest == nil {
		return nil, nil, ErrDocNotFound
	}
	pc, err := loadProject(ctx, tx, cur.ProjectRef)
	if err != nil {
		return nil, nil, err
	}
	if pc == nil {
		return nil, nil, ErrDocNotFound
	}
	return latest, pc, nil
}

// photoReplay: khóa idem đã ghi? Khóa dùng cho biểu khác → xung đột.
func (s *Service) photoReplay(ctx context.Context, c caller, idem string, ref uuid.UUID) (bool, error) {
	var prev *store.Photo
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.PhotoByIdem(ctx, tx, idem)
		return e
	}); err != nil {
		return false, err
	}
	if prev == nil {
		return false, nil
	}
	if prev.DocRef != ref {
		return false, ErrDocConflict.WithDetail("Idempotency-Key đã dùng cho biểu khác")
	}
	return true, nil
}

func (s *Service) photosReplayed(ctx context.Context, c caller, ref uuid.UUID) (PhotoList, error) {
	cur, pc, err := s.photoDoc(ctx, c, ref)
	if err != nil {
		return PhotoList{}, err
	}
	l, err := s.photoList(ctx, c, cur, pc)
	l.Replayed = true
	return l, err
}

// PhotoFile là ảnh để tải.
type PhotoFile struct {
	store.Photo
	Body io.ReadCloser
}

// Photo trả nội dung một ảnh HIỆN HÀNH của biểu (người xem được biểu).
func (s *Service) Photo(ctx context.Context, ref, photo uuid.UUID) (PhotoFile, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return PhotoFile{}, err
	}
	if _, _, err := s.photoDoc(ctx, c, ref); err != nil {
		return PhotoFile{}, err
	}
	var added *store.Photo
	var gone bool
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		added, gone, e = store.PhotoAt(ctx, tx, ref, photo)
		return e
	}); err != nil {
		return PhotoFile{}, err
	}
	if added == nil || gone {
		return PhotoFile{}, ErrPhotoNotFound
	}
	st := s.objects()
	if st == nil {
		return PhotoFile{}, ErrDependency.WithDetail("chưa nối kho tệp")
	}
	rc, _, err := st.Get(ctx, added.Key)
	if errors.Is(err, objstore.ErrNotFound) {
		return PhotoFile{}, ErrPhotoNotFound
	}
	if err != nil {
		return PhotoFile{}, err
	}
	return PhotoFile{Photo: *added, Body: rc}, nil
}
