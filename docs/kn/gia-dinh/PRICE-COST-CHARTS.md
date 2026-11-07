## PRICE-COST-CHARTS — hai biểu đồ báo cáo giá thành (#218)

Nguồn duy nhất: BRD Báo giá v2.0, Function chi tiết FN-TW-01.04 B1/B3, BR-01–04, AC-01–03 và CT-05.
Giá bán lấy từ chính phiếu điều tra Đã duyệt của đợt Đã chốt. Câu nguồn bảng giá hằng ngày trong issue cũ không thắng Function chi tiết v2.

### Phạm vi và cách tính

- Tái sử dụng aggregate `SurveyService.Cost`, `rows.avg_costs` và `regions.avg_cost_per_kg/avg_price`; giữ trung bình cộng từng phiếu, số học decimal, trung vị ở bảng. Không thêm migration, dependency hay nguồn dữ liệu ngoài.
- Cột chồng thể hiện chi phí bình quân từng khoản theo mặt hàng, đơn vị đồng/phiếu như bảng hiện hữu; không cộng giá hoặc trộn mặt hàng thành một giá vùng. Cột đôi thể hiện giá thành và giá bán bình quân theo từng cặp mặt hàng–vùng, đơn vị đồng/kg.
- Bộ lọc đợt/so sánh, nhóm cây, mặt hàng, nhóm vùng, xã gửi một lần tới cùng endpoint cho bảng và hai biểu đồ. API bổ sung danh mục mã/tên mặt hàng, xã từ phiếu đã duyệt của các đợt đang xem; danh mục không thu hẹp khi lọc, không tải PII/raw phiếu về browser.
- Nhóm dưới 3 phiếu có Mẫu nhỏ trên biểu đồ và bảng vùng. Số 0 có chiều cao 0, không vẽ cột giả; dữ liệu rỗng, đang tải, lỗi/thử lại rõ ràng. Chuỗi số được giới hạn trước parse khi dựng hình SVG; chỉ tỷ lệ hình học dùng Number.
- Giữ quyền API trực tiếp: TW chỉ tổng hợp, Tỉnh/Admin xem và xuất Excel, Xã 403. Không thay đổi endpoint xuất hoặc thêm dữ liệu cá nhân. Màn/endpoint hiện hữu chỉ có Excel; PDF và các phần FN khác không mở rộng trong gói hai biểu đồ.
- Giữ menu/route hiện hữu, không ghi HTX. Tệp `brd-surface.yaml` chưa có trên base; bổ sung hàng bề mặt sau khi #262 thực sự vào dev, theo barrier điều phối.

### Kiểm và giới hạn bàn giao

- Test native `TestBG218CostChartsAggregatesAndFilters` dùng sáu khoản khác nhau, diện tích/sản lượng khác nhau, hai vùng, số 0, nhóm cây/mặt hàng/vùng/xã, quyền Tỉnh/Admin/Xã/TW. Test `TestBG_FN_TW_01_04_CostMedianNoPII` giữ kiểm phiếu Loại/Đã xóa/đợt chưa chốt, trung vị, so sánh, PII.
- Vitest kiểm biểu đồ từ mock đúng contract, filter chung/empty, zero/stack, chuỗi exponent/siêu dài, loading/error/retry, hai đợt và quyền xuất. Playwright production mock tạo tài khoản qua Admin UI, đăng nhập TW/Tỉnh, đi menu và kiểm desktop/mobile.
- Windows không có GCC thì `-race` không thực thi được: ghi nguyên lỗi, không coi native không race là thay thế; phải chạy race Linux tại cổng tích hợp đúng SHA.
- Chỉ local preparation. CI nhánh, self-review công khai và ticket merge giữ tới khi barrier dev #287/#286/#261/#262 được điều phối nhả. Không coi checkpoint local là nghiệm thu toàn bộ FN hoặc CI xanh.

### Cách chạy

Từ gốc worktree đặt toolchain theo `go.mod`, thư mục tạm và GOMAXPROCS theo CONTEXT; chạy `scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/price/... ./internal/platform/party/handler ./migrations` và bộ cổng CONTEXT §3.3.
Trong `apps/web-ops`: `npm ci`, `npm run check:api`, `npm run lint`, `npx tsc --noEmit`, `npx vitest run`, `npm run build:mock`; production `next start -p <port-riêng>`, rồi `npx playwright test tests/e2e/price-cost-charts218.spec.ts` bằng cấu hình cùng port. Không dùng dev server làm bằng chứng production.
