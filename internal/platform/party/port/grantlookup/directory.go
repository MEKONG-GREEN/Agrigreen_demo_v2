package grantlookup

// Nguồn bản ghi feed cooperative_directory (W5.5, ADR-0033 §2): job dựng feed của pf/apiclient (cmd/worker) đọc
// danh bạ HTX của MỘT bên cấp — chỉ bên cấp đã có cấp quyền cooperative_directory đang hiệu lực cho một bên nhận
// có đăng ký feed (pf/apiclient chọn, qua ActiveGrantors). Cùng trường tối thiểu với GET /v1/partner/cooperatives
// (store.CooperativeEntries — không người đại diện, không mã số thuế, không nông hộ). Đọc bằng pool vai r_party ở
// tenant bên cấp (RLS áp); vị trí hồ sơ HTX (pf_coop từ W3.3) ghép qua CoopLocations do pf/cooperative cài đặt (gốc lắp
// ráp nối — nil → tọa độ null). Không ghi sổ đọc ở đây — sổ đọc ghi ở cổng mỗi lần bên nhận đọc trang feed (K-05).

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// directoryPage là cỡ trang đọc danh bạ HTX (một giao dịch cho cả tập của một bên cấp).
const directoryPage = 1000

var communeCode = regexp.MustCompile(`^[0-9]{5}$`)

// CoopLocations (W3.3) đọc vị trí hồ sơ HTX của tenant bên cấp: Nút → [lon, lat] (chuỗi độ, 7 chữ số thập phân);
// thiếu hồ sơ / tọa độ → vắng. Bên tiêu thụ khai (gói này), pf/cooperative cài đặt.
type CoopLocations interface {
	CoopLocations(ctx context.Context, grantor uuid.UUID, refs []uuid.UUID) (map[uuid.UUID][2]string, error)
}

// cooperativeRecord là bản ghi hợp đồng contracts/integration/feeds/cooperative_directory.v1.
type cooperativeRecord struct {
	CooperativeRef uuid.UUID `json:"cooperative_ref"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	CommuneCode    *string   `json:"commune_code"`
	Active         bool      `json:"active"`
	Lon            *string   `json:"lon"`
	Lat            *string   `json:"lat"`
	UpdatedAt      string    `json:"updated_at"`
}

// PartnerRecords trả mọi bản ghi hiện hành của tập dataset (chỉ port.KindCooperativeDirectory) trên tenant bên
// cấp grantor, theo thứ tự ref. Mã xã không đúng 5 chữ số → null (hợp đồng mặc định từ chối giá trị lạ).
func (l *Lookup) PartnerRecords(ctx context.Context, dataset string, grantor uuid.UUID) ([]json.RawMessage, error) {
	if dataset != port.KindCooperativeDirectory {
		return nil, fmt.Errorf("grantlookup: tập %q không có nguồn ở pf/party", dataset)
	}
	if grantor == uuid.Nil {
		return nil, port.ErrShareGrantMissing
	}
	var entries []store.CooperativeEntry
	err := tenant.WithTenantTx(ctx, l.DB, grantor, func(tx pgx.Tx) error {
		after := uuid.Nil
		for {
			es, err := store.CooperativeEntries(ctx, tx, after, directoryPage)
			if err != nil {
				return err
			}
			entries = append(entries, es...)
			if len(es) < directoryPage {
				return nil
			}
			after = es[len(es)-1].Ref
		}
	})
	if err != nil {
		return nil, err
	}
	locs := map[uuid.UUID][2]string{}
	if l.Locations != nil && len(entries) > 0 {
		refs := make([]uuid.UUID, len(entries))
		for i, e := range entries {
			refs[i] = e.Ref
		}
		if locs, err = l.Locations.CoopLocations(ctx, grantor, refs); err != nil {
			return nil, err
		}
	}
	out := make([]json.RawMessage, 0, len(entries))
	for _, e := range entries {
		r := cooperativeRecord{CooperativeRef: e.Ref, Code: e.Code, Name: e.Name, Active: e.Active, UpdatedAt: e.UpdatedAt.UTC().Format(time.RFC3339Nano)}
		if e.CommuneCode != nil && communeCode.MatchString(*e.CommuneCode) {
			r.CommuneCode = e.CommuneCode
		}
		if ll, ok := locs[e.Ref]; ok {
			r.Lon, r.Lat = &ll[0], &ll[1]
		}
		b, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}
