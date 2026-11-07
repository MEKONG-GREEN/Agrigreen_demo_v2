## KN-STANDALONE-8-BAOGIA-WEB — Hoãn đăng website Báo giá trong KN (#268)

Căn cứ duy nhất: BRD Báo giá v2.0, Function chi tiết FN-TI-02.07 (UR-TI-07,
BR-01–03), FN-TI-02.05 BR-06; quyết định D2 của PLAN-KN-STANDALONE-20261107.
FN-TI-02.07 hoãn pha kết nối. #238 là phụ thuộc của pha đó và vẫn bị chặn;
gói này không triển khai phương án website/A2.

1. Không thêm dependency. Cấu hình `Profile.WebsiteDeferred` do server sở hữu;
   chỉ `assembly.KNPriceProfile` bật. Profile khác giữ hành vi feed hiện hữu.
2. Migration mới chỉ thêm `pf_price.board_website_attempt`, sổ append-only/RLS
   qua `shared.apply_ledger`, quyền `r_price` SELECT/INSERT. Không sửa board,
   phiên bản, băm, snapshot, port hoặc dữ liệu đã công bố.
3. Nguồn KN trả 403 `price.website_not_connected` trước transaction; không trả
   snapshot rỗng thành công vì sẽ làm feed builder retract dữ liệu cũ. Worker
   kiểm capability trước đọc publisher và không đăng ký PublishedSource KN.
4. `POST /v1/price/boards/{day}/website` chỉ nhận Cấp Tỉnh/Admin trong phiên có
   quyền ghi và bảng giá đang Đã duyệt, đúng seq. Chỉ ghi trạng thái
   `not_connected`, thời điểm yêu cầu, phiên bản và mã tham chiếu; không có URL,
   PII, outbox hoặc I/O ngoài. Retry cùng khóa không thêm dòng. GET board có
   website nullable và quyền `can.website` do server tính.
5. Khối Kiểm duyệt & công bố giữ trạng thái Website: Chưa kết nối sau khi bấm.
   Thời điểm hiển thị là lúc ghi yêu cầu trong KN, không phải lúc đăng.

### Cổng gateway và giới hạn triển khai

Chưa có deployment profile KN chính thức trên base của gói. Theo seam đã chốt,
`apiclient.Deps.DisabledFeeds`, `PublicDeps.DisabledFeeds` và gateway
`assembly.Config.DisabledFeeds` nhận policy server-owned. Map nil giữ nguyên
shared gateway; map chứa `price_board_daily: true` chặn build/read/snapshot/
checksum bằng 403 trước truy cập dữ liệu hoặc ký URL. Cả route feed, snapshot
và checksum dùng cùng `feedAccess`; header/query giả profile không đổi policy.

S10 (#272) chịu trách nhiệm nối map này cho cấu hình KN và loại gateway khỏi
profile KN theo kế hoạch. Gói #268 không thêm biến môi trường/deployment tree,
không tuyên bố shared gateway mặc định đã tắt, không thay đổi feed HTX/MKG hợp lệ.
Sự kiện board approved và port đọc nội bộ vẫn dùng cho thông báo/báo cáo trong KN.

### Tệp → bước → Function

| Tệp/nhóm | Bước | FN/UR/BR |
|---|---|---|
| `migrations/pf/*_pf_price_board_website_attempt.up.sql`, `internal/kn/price/{domain,store}/website.go` | 2 | FN-TI-02.07 B2, D2: chỉ ghi nhận chưa kết nối |
| `internal/kn/price/{app/website.go,app/board.go,app/feed.go,port/port.go,port/feed.go}`, `cmd/api-platform/assembly/price.go`, `cmd/worker/feeds.go` | 3 | FN-TI-02.07 BR-01, D2; giữ FN-TI-02.05 |
| `internal/platform/apiclient/{module.go,app/*feed*.go,app/checksums.go,app/service.go,port/publishedfeed.go,public/{feeds,server}.go}`, `cmd/integration-gateway/assembly/assembly.go` | 3–4 | Hạ tầng FN-TI-02.07, D2; policy riêng cấu hình KN |
| `internal/kn/price/handler/board.go`, `contracts/openapi/modules/price/application.yaml` và tệp sinh | 4 | FN-TI-02.07 Người dùng, BR-01, B2; D2 |
| `apps/web-ops/src/kn/price/tickets/{api,BoardPanel}.tsx/ts`, mock boards và test website268 | 5 | FN-TI-02.07 thiết kế trạng thái/nút; D2 |

### Chạy và kiểm

Từ gốc worktree, dùng toolchain theo `go.mod` rồi migrate bình thường với module
`pf/price`; test chỉ dùng `scripts/pg-ephemeral.ps1 run go test -count=1 -p 1
-timeout 55m -v ./internal/kn/price/... ./cmd/api-platform/assembly ./cmd/worker
./cmd/integration-gateway/... ./internal/platform/apiclient/... ./migrations`.

Web: `npm ci`, `npm run gen:api`, `npm run check:api`, `npm run lint`,
`npx tsc --noEmit`, `npx vitest run`, `npm run build:mock`, production Playwright
`tests/e2e/price-website268.spec.ts`. Giờ kiểm thử được cố định, không phụ thuộc
ngày máy. Test negative dùng source/DB trap và gateway loopback; không gọi website thật.

Surface map sẽ bổ sung sau khi #262 thực sự vào dev, trước cổng tích hợp cuối.
