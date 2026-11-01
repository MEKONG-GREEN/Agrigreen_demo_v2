## BG-ITEM-PROPOSAL — hoàn thiện đường vào đề xuất mặt hàng

Nguồn: BRD Báo giá nông sản v1.3 (UR-XA-06, FN-XA-03.01 và FN-AD-04.03 AC05), prototype A `item_proposals` và menu Tỉnh/Xã. W11.73 đã cung cấp sổ đề xuất, API, kiểm duyệt và màn danh mục; W11.74 đã cung cấp lịch báo giá. Gói #37 tái sử dụng các phần đó, bổ sung menu và chứng cứ nghiệm thu còn thiếu.

### Phạm vi và quyết định

- Cấp Tỉnh và Cấp Xã có mục **Đề xuất mặt hàng** mở `/kn/bao-gia/de-xuat-mat-hang` ngay ở tab đề xuất; Cấp Xã giữ danh sách của chính người đề xuất, không có thao tác duyệt.
- Bật đường vào danh mục hiện có của Admin/Tỉnh. Route `/kn/bao-gia/mat-hang` vẫn mặc định mở tab Mặt hàng cho Tỉnh. Nguồn menu thuộc Price và dùng replacement với `before` chính xác, tránh sửa nguồn Extension của các gói khác.
- Giữ nguyên kiểm quyền QTNV, gán xã đề xuất, quyết định bất biến, lý do từ chối bắt buộc, ngày hiệu lực từ ngày báo giá kế tiếp và lịch sử danh mục trước ngày hiệu lực. Không ghi dữ liệu HTX.
- Không tạo lại ledger/API/migration; không thêm thông báo. BRD FN-AD-04.03 không quy định kênh thông báo cho AC05; thông báo kết quả vẫn là quyết định sản phẩm còn mở, không suy diễn thành chức năng mới.
- Refactor Dialog sang DetailPage/Log và chuẩn hóa toàn bộ form Price thuộc #182, không thuộc lát navigation này. Chỉ thêm prop chọn tab ban đầu vào ItemsScreen; các dialog và EntryScreen được giữ nguyên.

### Chứng cứ cần chốt trước READY

- `scripts/price-proposal-menus.test.mjs`: menu thực sự biên dịch có đúng một đường vào cho mỗi Tỉnh/Xã, không cấp cho TW; danh mục Admin/Tỉnh hoạt động và mọi mục đã bật có trang thật. Trước sửa: 2 ca lỗi vì thiếu menu/ComingSoon; sau sửa: 2 ca đạt.
- `src/kn/price/items/proposal-route.test.tsx`: đường riêng mở đúng bảng; đường cũ và quyền Xã được giữ. Không coi đây là kiểm chứng QTNV trên backend giả lập.
- `internal/kn/price/handler/proposal_flow_integration_test.go`: HTTP thật + PostgreSQL tạm + codec phiên sở hữu; QTNV, cách ly tenant/điểm/người đề xuất, quyết định lặp không thêm mặt hàng, lý do từ chối, lịch nghỉ cuối tuần/ngày lễ/ngày báo bổ sung và danh mục quá khứ.
- `tests/e2e/price-item-proposals.spec.ts`: đi từ menu Tỉnh/Xã, gửi hai đề xuất, duyệt/từ chối và trở lại lịch sử riêng. Mock chỉ chứng minh đường bấm/consumer; hiệu lực theo lịch W11.74 và QTNV lấy bằng chứng native.
- Cổng cuối chỉ ghi PASS sau khi thực thi. Thứ tự menu: #33 → #31 → #19 → #37. Trước READY phải lấy dev mới, merge --no-ff, chọn phiên bản menu lớn hơn dev thực tế và chạy đủ cổng trên nguồn kết hợp; không READY #37 trước #19.

### Checkpoint kỹ thuật ngày 30/10/2026

- HTTP thật/PostgreSQL/race đã đạt hai subcase `TestPriceProposalFlowHTTP`: cuối tuần kèm ngày lễ và ngày báo giá bổ sung thứ Bảy, không SKIP; bao gồm QTNV, cách ly tenant/điểm/người đề xuất, quyết định lặp và hiệu lực theo lịch.
- Browser production mock đạt 1/1: Admin bật phòng, tạo tài khoản Tỉnh chỉ có Thông tin Tuyên truyền qua UI; đăng nhập bình thường, mở menu Tỉnh/Xã, gửi hai đề xuất, Admin duyệt/từ chối rồi Xã xem đúng lịch sử riêng. Test dùng tên textbox accessible cho ô bắt buộc của màn tạo tài khoản. Không đổi persona/token/quyền bằng script; không sửa MenuLevelsScreen hay mock auth để dựng fixture.
- Controlled mutation trên nguồn menu Price: bật lại `ComingSoon` của đề xuất Tỉnh làm test đỏ; khôi phục đúng nội dung thì 2/2 menu test xanh. Hợp đồng menu web sau khi sinh lại fixture đạt 34/34; kỳ vọng Price đặt đề xuất ở Điều hành và danh mục ở Dữ liệu, giữ nguyên assertion các phòng khác.
- Full Vitest web một worker đạt 119 file/979 test, không trượt hay SKIP. Các cổng web `check:api`, TypeScript, full ESLint và `build:mock` đã đạt tại checkpoint; ESLint hai tệp test chỉnh sau cùng cũng đạt.
- Đây là checkpoint local của lát navigation. Cổng kết hợp với dev mới và phiên bản menu cuối vẫn chờ #33 → #31 → #19; không dùng chứng cứ này để báo READY toàn gói hoặc nghiệm thu toàn bộ form Price thuộc #182.
### Nguồn kết hợp đã kiểm ngày 31/10/2026

Các dòng ngày 30/10 ở trên là lịch sử checkpoint. Phần này cập nhật sau khi gộp `origin/dev` `033f356c542e0db152df4e36523a3ef9fc9f1293` bằng merge --no-ff; menu tăng từ v11 của dev lên `kn.menu/v12`, đồng bộ native/extension/mock và assertion. Modgen, gen:api và hai golden đều sinh lại chính thức; web fixture có 18 ca, snapshot compatibility có 657 ca/118 kết quả. Gói có 0 SQL mới hay thay đổi SQL.

- Lượt affected-race đầu trên nguồn tích hợp có một lỗi assertion Price cũ trong `TestW1106_MenuV3DepartmentTemplates`: vẫn đòi Mặt hàng Admin là Sắp có. Chỉ sửa block Price viết tay: ba khóa nghiệp vụ phải có đúng một lần, đúng route, đã mở và đã đăng ký; giữ prefix Admin dùng chung, các block KHTC/HCTH và vùng sinh. Không thay nghiệp vụ để làm test xanh.
- Package Party domain được chạy lại toàn bộ với race trên PostgreSQL tạm mới: 14 test cấp đầu và 4 subtest đạt. Chọn kết quả mới nhất theo từng package, thay package lỗi bằng lượt này: 9 package có test đạt, 8 package không có file test; 197 test cấp đầu/154 subtest đạt, 0 lỗi/0 SKIP test. Không cộng thêm 14/4 vào số tổng và không coi lượt broad đầu là xanh. Hai subcase HTTP của đề xuất đều đạt trong affected-race.
- Migration gate thật dùng đúng bộ lọc `SchemaIsolation|RLSExplain|ModuleGroup|LoginRoles|NoCrossModule|LedgerAddColumn`: 8 test cấp đầu đạt, 0 SKIP. Các cụm PostgreSQL tạm của lượt này đều đã dọn.
- Build, vet, modgen check, migratecheck theo origin/dev, archlint, knboundary theo origin/dev, contractcheck, mfaflagcheck, mod verify và lint 0 lỗi mới đã đạt. Sau sửa assertion chỉ chạy lại vet/lint của Party domain; mã Go/TS sản phẩm không đổi. Docscheck đầu phát hiện sai quy ước tiêu đề và dòng roadmap của hai tệp #37; cập nhật tài liệu này sửa đúng định dạng, giữ lịch sử lỗi, rồi kiểm riêng docscheck lần cuối.
- Web check:api, full ESLint, TypeScript, build:mock đạt. Full Vitest một worker đạt 137 file/1.060 test, không lỗi/bỏ qua; route 3, ItemsScreen 6 và menu contract 34 là các tập con của lượt full này.
- Production-mock Playwright focused #37 đạt 1/1 riêng; full suite đạt 53 file/160 test, 0 skipped/unexpected/flaky, một worker, retry 0 và timeout/expect gốc 60/10 giây. Giữ fixture tạo Tỉnh chỉ có TTTT qua Admin UI và đăng nhập bình thường. Hai lượt browser khóa Date tại `2026-10-31T13:57:03.667Z` và `2026-10-31T14:11:19.807Z`, timezone `Asia/Ho_Chi_Minh`; đây là chứng cứ hành trình, không phải chứng minh độc lập mọi ngày tháng. Server riêng đã dừng, port đã giải phóng.
- Controlled mutation mới trên nguồn menu Price làm 1/2 test đỏ (exit 1); khôi phục nguyên byte thì 2/2 xanh (exit 0), SHA256 trước/sau `E7C092CB6FE877581EA3DA573CB142ACB1DB0A0A8827432C7475802FCCEBC68D`.

Kết quả kỹ thuật này không thay biên bản UAT của PO. Gói lưu local để điều phối đọc SHA/hash/source guard trước push thường và READY cho Claude; Codex không tự gộp. Phạm vi #182, #165, #183 và quyết định thông báo vẫn như phần đầu.
