// Package domain: thực thể và quy tắc của pf/content — không phụ thuộc hạ tầng,
// không gọi time.Now (R-12).
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
)

// RuleVersion ghi vào cột rule_version của hai sổ.
const RuleVersion = "content.v1"

// StreamKey là khóa chuỗi băm của một tài liệu (cả hai sổ).
func StreamKey(ref uuid.UUID) string { return "doc:" + ref.String() }

// Status là trạng thái của tài liệu (bảng chiếu).
type Status string

// Trạng thái tài liệu: draft → review → published → retired.
const (
	StatusDraft     Status = "draft"
	StatusReview    Status = "review"
	StatusPublished Status = "published"
	StatusRetired   Status = "retired"
)

// VersionStatus là trạng thái của một phiên bản, suy ra từ hai sổ.
type VersionStatus string

// Trạng thái phiên bản.
const (
	VersionDraft      VersionStatus = "draft"      // đang soạn
	VersionReview     VersionStatus = "review"     // đã gửi duyệt
	VersionPublished  VersionStatus = "published"  // bản hiện hành
	VersionSuperseded VersionStatus = "superseded" // đã xuất bản, có bản mới hơn thay (vẫn đọc được)
	VersionDiscarded  VersionStatus = "discarded"  // bản nháp bị bản nháp sau thay trước khi xuất bản
	VersionReturned   VersionStatus = "returned"   // bị người duyệt trả lại
)

// Action là loại chuyển trạng thái trong sổ document_event.
type Action string

// Các chuyển trạng thái.
const (
	ActionSubmitted Action = "submitted"
	ActionReturned  Action = "returned"
	ActionPublished Action = "published"
	ActionRetired   Action = "retired"
)

// BodyFormat là định dạng thân bài.
type BodyFormat string

// Định dạng được hỗ trợ.
const (
	FormatMarkdown BodyFormat = "markdown"
	FormatHTML     BodyFormat = "html"
)

// AttachmentRole: tệp đính kèm thường hay ảnh bìa (HTX US-CMS-01).
const (
	RoleAttachment = "attachment"
	RoleCover      = "cover"
)

// Giới hạn.
const (
	MaxTitle       = 300
	MaxSummary     = 2000
	MaxBodyBytes   = 512 * 1024
	MaxTags        = 20
	MaxAttachments = 30
	MaxCommunes    = 200
	MinReason      = 10
	// MaxAttachmentBytes: 15 MB mỗi tệp (sync-contract, US-LIB-03).
	MaxAttachmentBytes = 15 << 20
)

// TagCatalogs là danh mục pf/masterdata được dùng làm nhãn phân loại (cây
// trồng/giống, khâu, vụ, dịch hại — KN FR-12 bước 3, FR-14).
var TagCatalogs = []string{"rice_variety", "farm_stage", "season", "pest"}

// Categories là chuyên mục tin tức (HTX US-NEWS-01 AC-2: kỹ thuật, cảnh báo dịch hại, chính sách).
var Categories = []string{"technique", "pest_warning", "policy", "market", "event"}

// FieldError là lỗi theo trường để app/ ánh xạ sang problem 422.
type FieldError struct {
	Field string
	Err   error
}

func (e FieldError) Error() string { return e.Field + ": " + e.Err.Error() }

func fieldErr(field, msg string) error { return FieldError{Field: field, Err: errors.New(msg)} }

// Attachment là siêu dữ liệu của một tệp đính kèm trong một phiên bản.
type Attachment struct {
	ID          uuid.UUID `json:"id"`
	Key         string    `json:"key"`
	SHA256      string    `json:"sha256"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	FileName    string    `json:"file_name"`
	Role        string    `json:"role"`
}

// Audience là phạm vi áp dụng.
type Audience struct {
	Level        port.AudienceLevel
	Units        []string // rỗng với national; [tỉnh] với province; các xã với commune
	ProvinceCode string   // tỉnh sở hữu (province/commune)
}

// Content là phần nội dung của một phiên bản.
type Content struct {
	Title       string
	Summary     string
	Body        string // đã lọc (Sanitize)
	BodyFormat  BodyFormat
	Category    string
	Tags        []string // "<danh mục>:<mã>"
	Attachments []Attachment
}

// Version là một dòng của sổ document_version.
type Version struct {
	ID             uuid.UUID
	DocumentRef    uuid.UUID
	VersionNo      int
	Content        Content
	BasedOnVersion *int
	EventTime      time.Time
	RecordedAt     time.Time
	AuthorRef      uuid.UUID
	IdemKey        string
	Seq            int64
}

// Event là một dòng của sổ document_event.
type Event struct {
	ID          uuid.UUID
	DocumentRef uuid.UUID
	VersionNo   *int
	Action      Action
	Reason      string
	EventTime   time.Time
	RecordedAt  time.Time
	ActorRef    uuid.UUID
	IdemKey     string
	Seq         int64
}

// Document là trạng thái hiện tại (bảng chiếu).
type Document struct {
	Ref                uuid.UUID
	Kind               port.Kind
	Audience           Audience
	ParentRef          *uuid.UUID
	Status             Status
	CreatedBy          uuid.UUID
	CreatedAt          time.Time
	UpdatedAt          time.Time
	LastVersionNo      int
	WorkingVersionNo   *int
	WorkingStatus      *VersionStatus
	PublishedVersionNo *int
	PublishedVersionID *uuid.UUID
	PublishedAt        *time.Time
	PublisherRef       *uuid.UUID
	Title              string
	Summary            string
	Category           string
	Tags               []string
	RetiredAt          *time.Time
	RetireReason       string
	SyncSeq            *int64
}

// Visible cho biết tài liệu hiển thị cho người đọc (đã xuất bản, chưa gỡ).
func (d Document) Visible() bool { return d.Status == StatusPublished && d.PublishedVersionNo != nil }

// ValidKind kiểm loại tài liệu.
func ValidKind(k port.Kind) bool {
	return slices.Contains([]port.Kind{port.KindProcedure, port.KindGuide, port.KindNews, port.KindAlert, port.KindSupplement}, k)
}

var (
	unitRe     = regexp.MustCompile(`^[0-9A-Za-z]{2,10}$`)
	tagCodeRe  = regexp.MustCompile(`^[a-z_]+:[A-Za-z0-9_.-]{1,40}$`)
	categoryRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
)

// ValidateAudience kiểm hình dạng phạm vi theo loại tài liệu (không kiểm quyền).
func ValidateAudience(kind port.Kind, a Audience, parent *uuid.UUID) error {
	switch a.Level {
	case port.AudienceNational:
		if len(a.Units) != 0 {
			return fieldErr("audience.units", "phạm vi toàn quốc không kèm mã đơn vị")
		}
	case port.AudienceProvince:
		if len(a.Units) != 1 {
			return fieldErr("audience.units", "phạm vi tỉnh cần đúng một mã tỉnh")
		}
	case port.AudienceCommune:
		if len(a.Units) == 0 || len(a.Units) > MaxCommunes {
			return fieldErr("audience.units", fmt.Sprintf("phạm vi xã cần từ 1 đến %d mã xã", MaxCommunes))
		}
	default:
		return fieldErr("audience.level", "phải là national, province hoặc commune")
	}
	for _, u := range a.Units {
		if !unitRe.MatchString(u) {
			return fieldErr("audience.units", "mã đơn vị hành chính không hợp lệ: "+u)
		}
	}
	if len(dedupe(a.Units)) != len(a.Units) {
		return fieldErr("audience.units", "mã đơn vị bị lặp")
	}
	switch kind {
	case port.KindProcedure:
		// KN BR-06: quy trình kỹ thuật chuẩn chỉ ở cấp quốc gia.
		if a.Level != port.AudienceNational {
			return fieldErr("audience.level", "quy trình kỹ thuật chuẩn có phạm vi toàn quốc; tỉnh dùng loại supplement")
		}
	case port.KindSupplement:
		if a.Level == port.AudienceNational {
			return fieldErr("audience.level", "hướng dẫn đặc thù địa phương phải gắn tỉnh hoặc xã")
		}
		if parent == nil {
			return fieldErr("parent_ref", "hướng dẫn đặc thù địa phương phải gắn quy trình gốc TW")
		}
	}
	if kind != port.KindSupplement && parent != nil {
		return fieldErr("parent_ref", "chỉ loại supplement được gắn quy trình gốc")
	}
	return nil
}

// ValidateContent kiểm nội dung một phiên bản (sau khi đã lọc thân bài).
func ValidateContent(kind port.Kind, c Content) error {
	switch {
	case strings.TrimSpace(c.Title) == "":
		return fieldErr("title", "bắt buộc")
	case utf8.RuneCountInString(c.Title) > MaxTitle:
		return fieldErr("title", fmt.Sprintf("tối đa %d ký tự", MaxTitle))
	case utf8.RuneCountInString(c.Summary) > MaxSummary:
		return fieldErr("summary", fmt.Sprintf("tối đa %d ký tự", MaxSummary))
	case len(c.Body) > MaxBodyBytes:
		return fieldErr("body", "thân bài vượt 512 KB")
	case c.BodyFormat != FormatMarkdown && c.BodyFormat != FormatHTML:
		return fieldErr("body_format", "phải là markdown hoặc html")
	case len(c.Tags) > MaxTags:
		return fieldErr("tags", fmt.Sprintf("tối đa %d nhãn", MaxTags))
	case len(c.Attachments) > MaxAttachments:
		return fieldErr("attachment_ids", fmt.Sprintf("tối đa %d tệp", MaxAttachments))
	}
	if c.Category != "" && !categoryRe.MatchString(c.Category) {
		return fieldErr("category", "mã chuyên mục không hợp lệ")
	}
	if c.Category != "" && !slices.Contains(Categories, c.Category) {
		return fieldErr("category", "chuyên mục phải là một trong "+strings.Join(Categories, ", "))
	}
	for _, t := range c.Tags {
		cat, _, ok := strings.Cut(t, ":")
		if !ok || !tagCodeRe.MatchString(t) || !slices.Contains(TagCatalogs, cat) {
			return fieldErr("tags", "nhãn phải dạng <danh mục>:<mã> với danh mục thuộc "+strings.Join(TagCatalogs, ", "))
		}
	}
	if len(dedupe(c.Tags)) != len(c.Tags) {
		return fieldErr("tags", "nhãn bị lặp")
	}
	covers := 0
	for _, a := range c.Attachments {
		if a.Role == RoleCover {
			covers++
		}
	}
	if covers > 1 {
		return fieldErr("attachment_ids", "chỉ một ảnh bìa")
	}
	return nil
}

// ValidatePublishable kiểm điều kiện riêng khi xuất bản.
func ValidatePublishable(kind port.Kind, c Content) error {
	// KN BR-05: quy trình phải gắn ít nhất một phân loại (cây trồng/khâu/vụ...).
	if kind == port.KindProcedure && len(c.Tags) == 0 {
		return fieldErr("tags", "quy trình kỹ thuật phải gắn ít nhất một phân loại (BR-05)")
	}
	// HTX US-CMS-01 AC-3: tin tức phải có tiêu đề và chuyên mục trước khi xuất bản.
	if kind == port.KindNews && c.Category == "" {
		return fieldErr("category", "Vui lòng nhập tiêu đề và chọn chuyên mục trước khi xuất bản.")
	}
	if strings.TrimSpace(c.Body) == "" && len(c.Attachments) == 0 {
		return fieldErr("body", "cần nội dung hoặc tệp đính kèm trước khi xuất bản")
	}
	return nil
}

// ValidateReason kiểm lý do (trả lại, gỡ): ít nhất MinReason ký tự.
func ValidateReason(r string) error {
	if utf8.RuneCountInString(strings.TrimSpace(r)) < MinReason {
		return fieldErr("reason", fmt.Sprintf("lý do ít nhất %d ký tự", MinReason))
	}
	return nil
}

// VersionStatuses suy ra trạng thái từng phiên bản từ hai sổ.
func VersionStatuses(versions []Version, events []Event) map[int]VersionStatus {
	st := make(map[int]VersionStatus, len(versions))
	published := 0
	for _, v := range versions {
		st[v.VersionNo] = VersionDraft
	}
	// Bản nháp bị bản sau thay: mọi bản < bản cuối mà chưa từng được xuất bản.
	for _, e := range events {
		if e.VersionNo == nil {
			continue
		}
		n := *e.VersionNo
		switch e.Action {
		case ActionSubmitted:
			st[n] = VersionReview
		case ActionReturned:
			st[n] = VersionReturned
		case ActionPublished:
			if published > 0 {
				st[published] = VersionSuperseded
			}
			st[n] = VersionPublished
			published = n
		}
	}
	last := 0
	for _, v := range versions {
		last = max(last, v.VersionNo)
	}
	for n, s := range st {
		if n < last && (s == VersionDraft || s == VersionReturned) {
			st[n] = VersionDiscarded
		}
	}
	return st
}

func dedupe(xs []string) []string {
	out := slices.Clone(xs)
	slices.Sort(out)
	return slices.Compact(out)
}
