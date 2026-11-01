package app

// W11.53b (#158 — BRD CNTS v3.7 FN-19 AC-02 "Bên A thấy Mẫu 07 trong 1 phút", UR-QG-01): thông báo hộp thư khi biểu dự
// án KNQG cần người khác xử lý:
//   - chủ nhiệm/lãnh đạo phòng GỬI Mẫu 07 → cán bộ Cấp TW (kn_tw) phòng Chăn nuôi – Thủy sản;
//   - Cấp TW XÁC NHẬN / TRẢ LẠI Mẫu 07 → chủ nhiệm của dự án;
//   - chủ nhiệm TRÌNH Mẫu 01 → lãnh đạo phòng Cấp Tỉnh của tỉnh dự án.
//
// Người nhận tra qua danh bạ CHỈ ĐỌC của pf/accounts TRƯỚC giao dịch ghi (lỗi #156: không I/O ngoài trong giao dịch DB).
// Sự kiện pf.extension.cnts_knqg.changed ghi outbox CÙNG giao dịch với dòng sổ (giao dịch hủy → không có thông báo;
// outbox-publisher và consumer pf.notify.cnts_knqg thử lại tới khi giao được — thường dưới vài giây, đáp ứng "1 phút").
// Tra người nhận lỗi / chưa nối danh bạ → thao tác vẫn thành công, sự kiện không có người nhận (không thông báo — không
// bao giờ mở rộng thành thông báo chủ đề theo vai realm); danh sách của TW vẫn đọc trực tiếp sổ nên thấy ngay (#29).

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/store"
	mstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Recipients là danh bạ người nhận thông báo (chỉ đọc; gốc lắp ráp nối pf/accounts — RoleHolders, KNDirectory).
type Recipients interface {
	// National: cán bộ Cấp TW (kn_tw) còn hiệu lực của phòng Chăn nuôi – Thủy sản.
	National(ctx context.Context, tid uuid.UUID) ([]uuid.UUID, error)
	// Managers: lãnh đạo phòng Chăn nuôi – Thủy sản Cấp Tỉnh của tỉnh province.
	Managers(ctx context.Context, tid uuid.UUID, province string) ([]uuid.UUID, error)
}

// maxRecipients chặn trên số người nhận của một sự kiện (payload outbox có giới hạn).
const maxRecipients = 200

// notifies cho biết thao tác act trên mẫu form có phát thông báo không.
func notifies(form, act string) bool {
	switch act {
	case domain.ActSend, domain.ActConfirm:
		return form == domain.Form07
	case domain.ActReturn:
		return form == domain.Form07
	case domain.ActSubmit:
		return form == domain.Form01
	}
	return false
}

// recipients tra người nhận của thao tác (NGOÀI giao dịch). Lỗi tra cứu → rỗng (không chặn thao tác nghiệp vụ).
func (s *Service) recipients(ctx context.Context, c caller, p mstore.Project, form, act string) []uuid.UUID {
	if !notifies(form, act) {
		return nil
	}
	var refs []uuid.UUID
	switch act {
	case domain.ActConfirm, domain.ActReturn:
		refs = []uuid.UUID{p.ManagerRef}
	case domain.ActSend:
		if s.Recipients != nil {
			r, err := s.Recipients.National(ctx, c.p.TenantID)
			if err != nil {
				slog.Default().WarnContext(ctx, "knqg: không tra được cán bộ Cấp TW nhận thông báo", "event", "knqg.notify.lookup", "outcome", "error")
			}
			refs = r
		}
	case domain.ActSubmit:
		if s.Recipients != nil {
			r, err := s.Recipients.Managers(ctx, c.p.TenantID, p.Province)
			if err != nil {
				slog.Default().WarnContext(ctx, "knqg: không tra được lãnh đạo phòng nhận thông báo", "event", "knqg.notify.lookup", "outcome", "error")
			}
			refs = r
		}
	}
	out := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		if r != uuid.Nil && r != c.me() && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	if len(out) > maxRecipients {
		out = out[:maxRecipients]
	}
	return out
}

// emitChanged ghi pf.extension.cnts_knqg.changed (outbox, cùng giao dịch với dòng sổ d). Không người nhận → không ghi.
func emitChanged(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p mstore.Project, d store.Doc, act string, recips []uuid.UUID, at time.Time) error {
	if len(recips) == 0 {
		return nil
	}
	payload := knport.CNTSKNQGChangedV1{DocID: d.ID, DocRef: d.Ref, ProjectRef: d.ProjectRef, ProjectCode: p.Code, ProjectName: p.Name,
		ProvinceCode: p.Province, Form: d.Form, Period: d.Period, Action: act, Status: d.Status, RecipientRefs: recips, ActorRef: d.ActorRef,
		Department: DeptCNTS, EventTime: at.UTC().Format(time.RFC3339)}
	return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: knport.EventCNTSKNQGChanged, SchemaVersion: 1,
		OccurredAt: at, TraceID: telemetry.TraceID(ctx), Producer: knport.EventProducerName, Payload: payload})
}
