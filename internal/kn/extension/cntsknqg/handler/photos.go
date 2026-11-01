package handler

// W11.53b (#158): ảnh thực địa của báo cáo kiểm tra Mẫu 03 — GET/POST …/photos, GET …/photos/{photo}, POST
// …/photos/{photo}/remove.

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

const maxPhotoUpload = domain.MaxPhotoBytes + 16<<10

func photosOut(l app.PhotoList) gin.H {
	items := make([]gin.H, 0, len(l.Items))
	for _, p := range l.Items {
		items = append(items, gin.H{"ref": p.PhotoRef, "name": p.Name, "content_type": p.Type, "size": p.Size, "sha256": p.SHA256,
			"caption": optStr(p.Caption), "uploaded_at": p.EventTime.UTC(), "uploaded_by": p.ActorRef, "uploaded_by_name": nameOf(l.Names, p.ActorRef)})
	}
	return gin.H{"items": items, "can_edit": l.CanEdit, "max_photos": domain.MaxPhotos, "replayed": l.Replayed}
}

func photoRef(c *gin.Context) (uuid.UUID, bool) {
	r, err := uuid.Parse(c.Param("photo"))
	if err != nil {
		httpx.Fail(c, app.ErrPhotoNotFound)
		return uuid.Nil, false
	}
	return r, true
}

func (h Handler) photos(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	l, err := h.Svc.Photos(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, photosOut(l))
}

func (h Handler) addPhoto(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	if err := c.Request.ParseMultipartForm(maxPhotoUpload); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, app.ErrPhotoInvalid.WithField("file", "ảnh 1 byte – 5 MB"))
			return
		}
		httpx.Fail(c, problem.Validation.WithDetail("thân phải là multipart/form-data có phần file (caption tùy chọn)"))
		return
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	fh, err := c.FormFile("file")
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "bắt buộc"))
		return
	}
	if fh.Size <= 0 || fh.Size > domain.MaxPhotoBytes {
		httpx.Fail(c, app.ErrPhotoInvalid.WithField("file", "ảnh 1 byte – 5 MB"))
		return
	}
	fr, err := fh.Open()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer func() { _ = fr.Close() }()
	data, err := io.ReadAll(io.LimitReader(fr, domain.MaxPhotoBytes+1))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	l, err := h.Svc.AddPhoto(c.Request.Context(), app.AddPhotoInput{Ref: ref, FileName: fh.Filename, Data: data,
		Caption: c.Request.FormValue("caption"), IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, photosOut(l))
}

func (h Handler) photo(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	pr, ok := photoRef(c)
	if !ok {
		return
	}
	f, err := h.Svc.Photo(c.Request.Context(), ref, pr)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer func() { _ = f.Body.Close() }()
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", `inline; filename*=UTF-8''`+urlEscape(f.Name))
	c.Header("X-Content-SHA256", f.SHA256)
	c.DataFromReader(http.StatusOK, f.Size, f.Type, f.Body, nil)
}

func (h Handler) removePhoto(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	pr, ok := photoRef(c)
	if !ok {
		return
	}
	l, err := h.Svc.RemovePhoto(c.Request.Context(), ref, pr, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, photosOut(l))
}
