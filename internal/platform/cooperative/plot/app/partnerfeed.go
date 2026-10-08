package app

// Nguồn bản ghi feed plot_directory của cổng tích hợp (W5.5, ADR-0033 §2; W3.3: ở pf/cooperative): job dựng feed của pf/apiclient
// (cmd/worker) đọc danh bạ thửa của MỘT bên cấp — chỉ bên cấp đã có cấp quyền plot_directory đang hiệu lực cho một
// bên nhận có đăng ký feed (pf/apiclient chọn). Cùng trường tối thiểu với GET /v1/partner/plots trừ hình học
// (hợp đồng v1 không có): không nông hộ, không tên thửa. Đọc bằng pool vai r_cooperative (W3.3: thửa ở pf_coop) ở tenant bên cấp (RLS áp);
// sổ đọc ghi ở cổng mỗi lần bên nhận đọc trang feed (K-05).

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/store"
)

var _ apiclientport.PartnerSource = (*Service)(nil)

const partnerFeedPage = 1000

// plotRecord là bản ghi hợp đồng contracts/integration/feeds/plot_directory.v1.
type plotRecord struct {
	PlotRef        uuid.UUID  `json:"plot_ref"`
	Code           string     `json:"code"`
	CommuneCode    string     `json:"commune_code"`
	CooperativeRef *uuid.UUID `json:"cooperative_ref"`
	Status         string     `json:"status"`
	AreaHa         string     `json:"area_ha"`
	Revision       int64      `json:"revision"`
	UpdatedAt      string     `json:"updated_at"`
}

// PartnerRecords cài đặt apiclientport.PartnerSource cho tập plot_directory: mọi thửa của tenant bên cấp grantor,
// theo thứ tự ref.
func (s *Service) PartnerRecords(ctx context.Context, dataset string, grantor uuid.UUID) ([]json.RawMessage, error) {
	if dataset != apiclientport.FeedPlotDirectory {
		return nil, fmt.Errorf("cooperative: tập %q không có nguồn ở pf/cooperative", dataset)
	}
	if grantor == uuid.Nil {
		return nil, fmt.Errorf("cooperative: thiếu tenant bên cấp")
	}
	var out []json.RawMessage
	err := s.inTx(ctx, grantor, func(tx pgx.Tx) error {
		after := uuid.Nil
		for {
			ps, err := store.PartnerPlots(ctx, tx, uuid.Nil, after, partnerFeedPage)
			if err != nil {
				return err
			}
			for _, p := range ps {
				b, err := json.Marshal(plotRecord{PlotRef: p.Ref, Code: p.Code, CommuneCode: p.CommuneCode, CooperativeRef: p.CooperativeRef,
					Status: p.Status, AreaHa: p.AreaHa, Revision: p.Revision, UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339Nano)})
				if err != nil {
					return err
				}
				out = append(out, b)
			}
			if len(ps) < partnerFeedPage {
				return nil
			}
			after = ps[len(ps)-1].Ref
		}
	})
	return out, err
}
