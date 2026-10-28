# Nghiệm thu CNTS v3.7 — W11.43 / W11.46

Phạm vi G1: cảnh báo liên biểu, vòng đời xử lý, nhắc nộp biểu CNTS và lịch trực/tiêm/công tác + check-in.
Nguồn và các giới hạn: [giả định W11.43](../kn/gia-dinh/W11.43.md).
Các kiểm thử tích hợp dùng PostgreSQL thật; nguồn bài nộp/danh bạ trong kiểm thử
cảnh báo là cổng giả. `TestFormsSubmissionFeed_W1143` kiểm chứng cổng nguồn thật
riêng, không thay thế diễn tập worker và gửi email trên môi trường triển khai.

| Tiêu chí | Bằng chứng tự động | Phạm vi |
|---|---|---|
| Biến động tổng đàn/khai thác vượt ngưỡng, KSGM bất thường hoặc bằng 0, lệch CN-06 và BM-01 | `TestCNTSAlerts_W1143`, `TestChangeFindingsHerd`, `TestSlaughterSpike`, `TestSlaughterZeroAndMismatch` | G1 |
| Ổ dịch mới sinh cảnh báo Khẩn; mỗi đối tượng × loại × kỳ chỉ một cảnh báo | `TestCNTSAlerts_W1143` | G1 |
| CNTS-SCOPE-001: cảnh báo không gắn xã vẫn chỉ hiện trong tỉnh phụ trách, phạm vi rỗng bị chặn | `TestProvinceAlertScope_W1143` | G1 |
| Giao, nhận, đóng có kết quả; kiểm tra phiên bản và phạm vi xã/phòng/vai | `TestCNTSAlerts_W1143`, `TestTransition` | G1 |
| Hết vi phạm tự đóng; quá hạn chỉ báo một lần theo hạn | `TestCNTSAlerts_W1143` | G1 |
| Nhắc trước hạn, khi trễ, nhắc lặp; cấu hình theo biểu và cảnh báo trễ liên tiếp | `TestCNTSAlerts_W1143`, `TestFormReminderConfig` | G1 |
| Sự kiện nộp biểu và đọc nguồn theo tenant | `TestFormsSubmissionFeed_W1143` | G1 |
| Người nhận không trùng; thông báo chủ đề khi thiếu người nhận; kênh email cho cảnh báo Khẩn/nhắc được cấu hình | `TestCNTSNotifications_W1143` | G1 |
| Danh sách và thao tác cảnh báo, cấu hình nhắc; menu tỉnh/xã | Vitest `src/kn/cnts/alert/alert.test.ts`; `TestWebMenuFixture`; `kn-menu.contract.test.ts` | G1 |
| Cảnh báo chất lượng nước, tăng trưởng, tỷ lệ chết, vị trí (dòng 6–9) từ lần ghi nhật ký; nhật ký trễ, lịch tiêm trễ (dòng 10–11) | `TestCNTSDiary_W1148` (dòng 6, 9 trên PostgreSQL thật), `TestLogRules`, `TestEvaluatePoultry` | Dòng 6–9: G1 (W11.48); dòng 10–11: chờ job |

Chưa nghiệm thu: widget “Việc cần làm” cấp Xã, người phụ trách riêng theo đợt
phát hành, bản đồ/biểu đồ cảnh báo và nút báo nghi dịch từ tỷ lệ chết.
Nhịp worker/sự kiện cần được kiểm trên môi trường triển khai để xác nhận SLA
một phút; kiểm thử hàm xử lý không tự chứng minh thời gian giao nhận thực tế.

Kết quả tiếp nhận ngày 15/10/2026 được ghi ở [sổ hồi quy](../REGRESSION.md).

## Lịch CNTS W11.46

| Tiêu chí | Bằng chứng tự động | Phạm vi |
|---|---|---|
| Lưới BM-03, sao chép, phiên bản lạc quan, cảnh báo trùng ca/chưa có người/chưa cấp cơ sở; Cấp Xã xem ca mình | `TestDutyRoster`, `TestNormalizeCellsAndWarnings`; schedules Vitest | G1 |
| Tách tỉnh cùng tenant: cơ sở/cán bộ, ô/ghi chú/lịch sử và số phiên bản; chặn chọn tỉnh/cán bộ/cơ sở ngoài phạm vi | `TestDutyRosterProvinceIsolation_W1146` (CNTS-ROSTER-SCOPE-001) | G1 |
| Admin phải chọn tỉnh; lịch chung cũ chỉ đọc, hash giữ nguyên | `TestDutyRosterAdminAndLegacy_W1146`, `DutyRosterScreen.test.tsx` (CNTS-ROSTER-SCOPE-002) | G1 |
| Đổi ca → hộp thư/đẩy đúng người bị ảnh hưởng, giao lại không trùng, không gửi ra tenant khác | `TestCNTSDutyChanged_W1146` (CNTS-DUTY-NOTIFY-001) | G1 |
| BM-07 chọn quy trình theo ngày nhận giống, ngày tuổi, sửa dự kiến, ghi thực tế/hủy, phạm vi xã | `TestVaccinationBatch`, `TestGenerateDosesAC03`, `TestDoseStatusAndUpdates` | G1 |
| Lịch công tác theo phòng, người theo dõi duyệt, check-in một lần, ngày/ngoại tuyến, khoảng cách; tọa độ ở bảng có hạn lưu, purge | `TestWorkScheduleCheckin`; domain schedule/check-in; field-app `work_schedule_screen_test.dart`, kiểm thử hàng đợi ngoại tuyến | G1 |

Xuất lịch trực hiện là CSV; nạp/xuất XLSX đúng mẫu còn thiếu. Chưa tự tạo lịch công tác từ bài nộp BM-05-II.
Cờ vị trí giả lập chưa lấy từ thiết bị; thời hạn giữ tọa độ theo tham số mặc định 90 ngày còn chờ xác nhận căn cứ/thời hạn.
SLA giao thông báo một phút và GPS trên thiết bị thật cần diễn tập triển khai; test handler không chứng minh SLA.

CNTS-CHECKIN-PRIVACY-001: `TestWorkScheduleCheckin` kiểm phản hồi đầu có vị trí cho người có quyền, cache check-in/duyệt chỉ metadata; xóa tọa độ sau hạn dưới r_extension không chạm tenant khác, giữ nguyên metadata/hash sổ. Lượt tái hiện trước sửa đã FAIL vì cache chứa location. CNTS-ROSTER-UI-001 kiểm đường vào Admin qua menu phòng đã bật, phân ca/lưu, chuyển tỉnh không lộ lịch, CSV và lịch cũ chỉ đọc. Kết quả cuối xem sổ hồi quy.

Kết quả tiếp nhận W11.46 ngày16/10/2026: các ca tự động trong bảng lịch trên đạt trên PostgreSQL thật; web560ca/65tệp, build theo lockfile và luồng trình duyệt Admin lịch trực +2luồng lịch công tác đạt. Mobile119ca/analyze đạt. Giới hạn triển khai và các mục chưa làm ở trên giữ nguyên, không coi tự động kiểm thử là đã nghiệm thu thiết bị/SLA thực tế. Chi tiết mã thoát và các lượt bổ sung tại [sổ hồi quy](../REGRESSION.md).

## CT/DA, điểm trình diễn, hộ mô hình, cấp quyền W11.47

Nguồn và giới hạn: [giả định W11.47](../kn/gia-dinh/W11.47.md) (Q-023, Q-076, Q-061 là giả định tạm theo prototype).

| Tiêu chí | Bằng chứng tự động | Phạm vi |
|---|---|---|
| ★ UR-TINH-19: tạo dự án (tên, căn cứ, loại, quy mô, thời gian, kinh phí, chủ nhiệm, tập huấn, giống/vật tư), gắn CT/DA chủ KHTC, mã không trùng, mỗi thay đổi một phiên bản, khóa lạc quan | `TestCNTSModel_W1147`, `TestProjectNormalize`; Vitest `src/kn/cnts/model/model.test.ts` | G1 |
| ★ FN-19 BR-01, AC-04: chủ nhiệm dự án khác không sửa được; chủ nhiệm không tự đổi chủ nhiệm; lãnh đạo phòng sửa được; dự án kết thúc khóa sửa điểm, hộ | `TestCNTSModel_W1147` | G1 |
| ★ Điểm trình diễn (xã, quy mô, tọa độ, cán bộ theo dõi, mẫu nhật ký): xã ngoài tỉnh bị chặn, loại mô hình theo danh mục, cán bộ phải phụ trách xã, HTX chỉ là mã có trong công bố HTX; Cấp Xã chỉ thấy điểm của xã mình | `TestCNTSModel_W1147` | G1 |
| ★ UR-XA-04: cán bộ theo dõi điểm (hoặc được cấp quyền theo dõi mô hình) thêm, sửa, rút hộ; chủ hộ ở kho định danh (sổ chỉ giữ `person_ref`), nhãn và SĐT che một phần (BR-02); SĐT trong ghi chú bị chặn; rút hộ xóa tọa độ (QĐ-8) | `TestCNTSModel_W1147`, `TestHouseholdKeeper`; Vitest giả lập | G1 |
| ★ UR-TINH-09, FN-14: cấp quyền theo vai, địa bàn (xã/cơ sở/điểm/hộ), biểu mẫu, hiệu lực; chỉ 4 vai Xã/Nông hộ (BR-01); địa bàn ngoài tỉnh bị chặn; chủ nhiệm chỉ cấp trong dự án mình (BR-03); người được cấp phải là tài khoản Cấp Xã có sẵn; mọi thay đổi có nhật ký | `TestCNTSModel_W1147`, `TestGrantRoles`, `TestGrantStatusAndCovers` | G1 |
| ★ FN-14 AC-02: thu hồi quyền của A chuyển phạm vi cho người nhận (quyền mới cùng phạm vi); A mất quyền sửa hộ ngay, người nhận sửa được | `TestCNTSModel_W1147` | G1 (#71: chuyển điều kiện công việc biểu qua phiên chủ — `TestCNTSGrantOwnerSession_71`; #49: con trỏ đồng bộ reset) |
| ADR-0055 / Q-KNS-04: KN không tạo dữ liệu HTX | `TestCNTSModel_W1147` (đếm qua `pf_cooperative_pub`), `go run ./tools/knboundary` | G1 |
| Menu Tỉnh (Chương trình, dự án; Hộ mô hình; Cấp quyền xã, nông hộ), Xã (Điểm trình diễn, hộ mô hình; Quyền của tôi), TW (Dự án KNQG) | `TestWebMenuFixture`; `kn-menu.contract.test.ts` | G1 |
| ★ FN-14 AC-01/AC-03, BR-02 (#71): quyền FN-14 vào phiên chủ KN ở request kế tiếp; BM-01 chỉ cơ sở được cấp; biểu được giao giới hạn biểu kỳ CNTS; tường minh rỗng/hết hạn/thu hồi/sửa thu hẹp xã = chặn (người đã từng có quyền không quay về W11.07); biên ngày tính cả ngày cuối; cùng người nhiều lần gán, phòng/tenant khác không nhận | `TestCNTSGrantOwnerSession_71`, `TestCNTSGrantNarrowingStaysExplicit_71`, `TestCNTSFormActorUsesOwnerGrantScope`, `TestCNTSGrant*` (port) | G1 |

Chưa nghiệm thu: chuyển nháp nhật ký khi thu hồi, Cấp Xã chưa từng được cấp quyền vẫn theo thuộc tính W11.07 (câu hỏi mở #71),
mẫu biểu dự án BM-12…BM-16 (W11.53), nạp Excel điểm/hộ, chọn tọa độ trên bản đồ (W11.52), nông hộ tự nhập (GĐ2).

## Nhật ký nuôi BM-08/09/19 W11.48

Nguồn và giới hạn: [giả định W11.48](../kn/gia-dinh/W11.48.md) (Q-077 mẫu BM-19, Q-005 hạn lưu GPS/cơ sở mở ảnh là giả định tạm).

| Tiêu chí | Bằng chứng tự động | Phạm vi |
|---|---|---|
| ★ UR-XA-11 (NC-02…NC-15): lứa nuôi gắn hộ mô hình KN (mẫu theo điểm, một lứa đang nuôi); lần ghi hằng ngày; NC-05 lệch số sống − chết − xuất bán → chặn; NC-06 > 0 thiếu NC-07 → chặn; xuất bán trong thời gian ngưng thuốc → cảnh báo; sổ vịt xiêm không có điều trị, xuất bán; một lần ghi mỗi ngày | `TestCNTSDiary_W1148`, `TestEntryNormalize`, `TestEvaluatePoultry`; Vitest `src/kn/cnts/diary/diary.test.ts`, `DiaryScreens.test.tsx` | G1 |
| ★ UR-XA-12 (NT-02…NT-15, Q-077 giả định): cân đo thiếu ảnh cân mẫu → chặn; pH 8,7 → cảnh báo môi trường nước tới chủ nhiệm và cán bộ theo dõi ngay khi gửi (FN-10 AC-02), thiếu biện pháp NT-12 → chặn | `TestCNTSDiary_W1148`, `TestEvaluateAquaticAndLocation` | G1 |
| ★ NT-15: vị trí lúc ghi không sửa được; khoảng cách tới tọa độ hộ, > 200 m đánh dấu và cảnh báo dòng 9; nghi giả lập (cờ thiết bị, sai số 0, trùng tọa độ); sổ không có tọa độ, tọa độ ở bảng thường không UPDATE, dọn theo hạn lưu giữ khoảng cách trong sổ (QĐ-8) | `TestCNTSDiary_W1148`, `TestEvaluateAquaticAndLocation` | G1 (cờ giả lập từ thiết bị: chưa) |
| ★ Ảnh: ≤ 500 KB, đúng kiểu; bản mã ở `pf/evidence`, mã hóa bằng khóa chủ hộ; chỉ người ghi được/chủ nhiệm/Admin xem; xóa định danh chủ hộ → ảnh không đọc được | `TestCNTSDiary_W1148` (kho tệp giả gọi Resolver/FileAuthorizer thật) | G1 |
| ★ UR-TINH-12, FN-13: chủ nhiệm chỉ rà soát chương trình mình; xác thực / yêu cầu bổ sung có lý do; người xác thực khác người ghi (ứng dụng + CHECK sổ); đã xác thực không có nút Sửa (AC-04), điều chỉnh tạo bản mới có lý do và chờ xác thực lại; sửa bản đã gửi quá 7 ngày bị chặn | `TestCNTSDiary_W1148`, `TestTransitionAndAccounting`; Vitest `DiaryScreens.test.tsx` | G1 |
| ★ FN-10 AC-03 (một phần): mã lần ghi máy khách, thời điểm thiết bị, gửi lại cùng khóa trả bản đã ghi | `TestCNTSDiary_W1148` | G1 (hàng đợi ngoại tuyến App xã: chưa) |
| NC-15: hạch toán cuối lứa (tổng thu bắt buộc), gợi ý chi thức ăn/thu từ nhật ký, lợi nhuận, giá thành, lợi nhuận trung bình/hộ theo điểm; mở lại lứa có lý do | `TestCNTSDiary_W1148`, `TestTransitionAndAccounting`; Vitest | G1 |
| Menu Tỉnh "Rà soát nhật ký", Xã "Nhật ký sản xuất" | `TestWebMenuFixture`; `kn-menu.contract.test.ts` | G1 |

Chưa nghiệm thu: ghi ngoại tuyến trên App xã (entity `pf/sync`), nông hộ tự ghi (W11.55), cảnh báo quá hạn ghi (dòng 10),
NC-11 cập nhật lịch tiêm BM-07, chuẩn kỹ thuật theo mẫu (UR-TINH-07), Mẫu 05 lấy thực tế (W11.53), bản đồ vị trí (W11.52).

## Bản đồ vùng nuôi, vùng dịch W11.52

Nguồn và giới hạn: [giả định W11.52](../kn/gia-dinh/W11.52.md) (bán kính mặc định 500 m/3 km/10 km là giả định; Xã chỉ xem theo BRD).

| Tiêu chí | Bằng chứng tự động | Phạm vi |
|---|---|---|
| ★ UR-TINH-17 B3–B4: vẽ vùng chăn nuôi/thủy sản/dịch/uy hiếp/đệm (đa giác hoặc bán kính) và hướng di chuyển; hình PostGIS 4326 hợp lệ — đa giác tự cắt, vòng hở, ngoài Việt Nam bị chặn ở ứng dụng và ràng buộc DB | `TestCNTSMap_W1152`, `TestZoneNormalize` | G1 |
| ★ Khoanh vùng từ ổ dịch BM-10 (W11.49): vùng dịch 500 m, vùng bị uy hiếp 3 km, vùng đệm 10 km (đổi được, tăng dần), gắn ổ dịch và vùng cha; gọi lặp không ghi thêm | `TestCNTSMap_W1152` | G1 |
| ★ Đối tượng trong vùng đệm: điểm trình diễn, hộ mô hình (W11.47) và ổ dịch nằm trong vùng 3 km/10 km, kèm khoảng cách | `TestCNTSMap_W1152` | G1 |
| ★ FN-18 BR-01, AC-03: vùng lưu theo ngày áp dụng; chọn ngày cũ thấy phiên bản của ngày đó; sửa là phiên bản mới có lý do và khóa lạc quan; ngưng/khôi phục không xóa dòng nào | `TestCNTSMap_W1152`; Vitest `src/kn/cnts/map/map.test.ts` | G1 |
| ★ FN-18 AC-01: ổ dịch đã xác nhận hiện ngay trên lớp bản đồ (đọc trực tiếp sổ BM-10 theo ngày) | `TestCNTSMap_W1152` (lớp `outbreaks`) | G1 |
| ★ FN-18 AC-02: Cấp Xã chỉ thấy vùng, ổ dịch, điểm, hộ của xã mình; không vẽ được | `TestCNTSMap_W1152` | G1 |
| Ma trận quyền FN-18: viên chức chuyên môn vẽ/sửa; lãnh đạo phòng chỉ xem; ngữ cảnh chỉ xem bị chặn; xã ngoài tỉnh bị chặn | `TestCNTSMap_W1152` | G1 |
| Cách ly tenant (RLS): tenant khác không thấy vùng, ổ dịch, điểm, hộ | `TestCNTSMap_W1152` | G1 |
| Lớp HTX chỉ qua công bố (thống kê thửa theo xã), KN không đọc bảng HTX | `go run ./tools/knboundary` (không tăng) | G1 |
| Màn `/kn/cnts/ban-do-dich`: lớp bật/tắt, thanh trượt ngày, vẽ/sửa/ngưng, khoanh từ ổ dịch, GeoJSON lên/xuống; menu Tỉnh/Xã | Vitest `map.test.ts`; `TestWebMenuFixture`; `kn-menu.contract.test.ts` | G1 |

Chưa nghiệm thu: tải KML, xuất ảnh, lớp cơ sở giết mổ, ranh giới xã, hình thửa/ranh giới HTX (cần công bố có hình học — ticket
làn shared); cảnh báo "vẽ ngoài ranh giới" chỉ hoạt động khi `pf/geo` có ranh giới xã (chưa có kiểm thử DB với ranh giới thật);
luồng trình duyệt chưa có Playwright.

## Trang chủ phòng + kho văn bản W11.54 (FN-17, FN-20 — kéo từ GĐ2 vào G1)

Áp dụng cho cả 5 phòng KN (thẻ theo phòng); giới hạn: [giả định W11.54](../kn/gia-dinh/W11.54.md).

| Tiêu chí | Bằng chứng tự động | Phạm vi |
|---|---|---|
| FN-17: Cấp Tỉnh mở trang chủ phòng mình; Admin chọn phòng (mỗi phòng có trang chủ); Cấp Xã 403 | `TestFN17_DeptHome` | G1 |
| FN-17: thẻ CNTS (cảnh báo, quá hạn, ổ dịch), lịch công tác, văn bản; thẻ "chờ duyệt" chỉ Lãnh đạo phòng/Ban Giám đốc; bấm thẻ mở màn chi tiết | `TestFN17_DeptHome`; Vitest `src/kn/depthome/depthome.test.ts` | G1 |
| FN-17 luồng phụ, BR-02/AC-02: Lãnh đạo phòng sửa giới thiệu, chức năng nhiệm vụ (phiên bản, khóa lạc quan); viên chức, Ban Giám đốc không sửa | `TestFN17_DeptHome` | G1 |
| FN-20 B1–B3: thư mục, thêm văn bản, lọc loại/hiệu lực/thư mục, tìm số hiệu; xem, tải; phát hiện sai định dạng theo nội dung | `TestFN20_DeptLibrary`, `TestFileType` | G1 |
| FN-20 AC-01: văn bản mới hiện ngay cho người được xem (kể cả Cấp Xã khi đối tượng "xa"); phòng khác không thấy | `TestFN20_DeptLibrary`, `TestFN17_DeptHome` | G1 |
| FN-20 AC-02: tệp > 20 MB bị chặn | `TestFN20_DeptLibrary`; Vitest `fileError` | G1 |
| FN-20 AC-03, BR-01: văn bản hết hiệu lực vẫn tìm được, nhãn "Hết hiệu lực" | `TestFN20_DeptLibrary`, `TestValidity` | G1 |
| Phiên bản tệp (tệp cũ tải được), ẩn = xóa có lý do (Lãnh đạo phòng), sổ chặn UPDATE | `TestFN20_DeptLibrary` | G1 |
| Menu: Trang chủ phòng, Quy trình, văn bản cho Tỉnh/Xã/Admin các phòng | `TestWebMenuFixture`; `kn-menu.contract.test.ts` | G1 |

Chưa nghiệm thu: AC-01 FN-17 (500 người dùng, < 5 giây trên 3G — cần đo tải trên môi trường triển khai), chỉ số tổng đàn/tiêm
phòng/KSGM tuần trên trang chủ, bộ lọc kỳ/khu vực/lĩnh vực, Nông hộ xem quy trình trên Mobile (W11.55).

## Chiến dịch điều tra W11.50

Nguồn và giới hạn: [giả định W11.50](../kn/gia-dinh/W11.50.md) (chờ Q-058 — theo prototype).

| Tiêu chí | Bằng chứng tự động | Phạm vi |
|---|---|---|
| FN-21 B1–B2: tạo đợt Nháp (tên, mục tiêu, lĩnh vực, biểu, phạm vi xã, thời gian, hạn), sửa nháp thành phiên bản mới, xem trước số việc, phát hành thành kỳ native `kn_cnts` loại `adhoc`; Cấp Xã thấy việc (UR-XA-14) | `TestCNTSCampaign_LifecycleSeparateFromCumulative_W1150`; Vitest `src/kn/cnts/campaign/campaign.test.ts` | G1 |
| AC-04, BR-02: số liệu đợt (dùng cùng biểu BM-17) không đổi lũy kế Phụ lục 2 (BM-18); bộ đọc/quy đổi số đã chốt của KHTC bỏ kỳ `adhoc` | `TestCNTSCampaign_LifecycleSeparateFromCumulative_W1150` (CNTS-CAMPAIGN-CUMUL-001) | G1 |
| B4: tiến độ theo khu vực/xã, nhắc xã chưa nộp, gia hạn có lý do (hạn mới sau hạn cũ) | `TestCNTSCampaign_LifecycleSeparateFromCumulative_W1150` | G1 |
| AC-03, BR-04: đóng đợt → xã chưa gửi "Không nộp", gửi sau đóng bị chặn; kết quả theo ảnh chụp, xuất Excel | `TestCNTSCampaign_LifecycleSeparateFromCumulative_W1150` | G1 |
| BR-01: mở lại chỉ Lãnh đạo phòng, lý do + thời hạn; sổ chỉ ghi thêm | `TestCNTSCampaign_LifecycleSeparateFromCumulative_W1150`, `TestPeriodKeyAndState` | G1 |
| Màn Cấp Tỉnh "Chiến dịch điều tra", menu `tinh.cnts.chien_dich.quan_ly` (mục "Tạo chiến dịch" còn Sắp có — tạo đợt ngay trên màn quản lý) | Vitest `campaign.test.ts`, `kn-menu.contract.test.ts`; `TestWebMenuFixture` | G1 |
| AC-01 (102 xã thấy việc trong 1 phút), phạm vi theo hộ, nhắc tự động theo cấu hình đợt | — | Chưa nghiệm thu / chưa làm |

## Báo cáo phòng BM-11 W11.45

Nguồn và giới hạn: [giả định W11.45](../kn/gia-dinh/W11.45.md) (Q-046 giả định: BM-11 gửi để lưu; số liệu KHTC qua phiếu + nguồn đã chốt).

| Tiêu chí | Bằng chứng tự động | Phạm vi |
|---|---|---|
| ★ UR-TINH-16, quy tắc 13: chỉ lập khi đã qua hạn nộp của tuần cuối và tỷ lệ xã đã nộp từng biểu nguồn ≥ mức Cấp Tỉnh đặt, hoặc mọi kỳ nguồn đã chốt (`extension.cnts_bm11_not_ready`) | `TestCNTSBM11_W1145` (quy tắc 13; tháng 9 sau chốt) | G1 |
| ★ AC-01, BR-01: phần tự động = tổng số hợp lệ của kỳ (tổng đàn tuần cuối, tiêm phòng/hóa chất/KSGM cộng tuần, khai thác + nuôi trồng + lũy kế năm), không sửa tay | `TestCNTSBM11_W1145`, `TestBM11CoverageAndLivestock` | G1 |
| ★ AC-03, BR-04: "Tổng hợp từ n/N xã" + danh sách xã thiếu, không ước tính | `TestCNTSBM11_W1145` ("Tổng hợp từ 1/2 xã", X2), `TestBM11CoverageAndLivestock`, `TestBM11WordTemplates` (99/102) | G1 |
| ★ Vòng đời Đang soạn → Chờ duyệt → Trả lại → Đã gửi → Đã nhận; một báo cáo mỗi kỳ (kể cả tạo đồng thời); khóa lạc quan; sổ không UPDATE | `TestCNTSBM11_W1145`, `TestBM11Next` | G1 |
| ★ AC-04: trả lại thiếu ý kiến không lưu được; chỉ Lãnh đạo phòng duyệt, BGĐ chỉ xem (BR-05) | `TestCNTSBM11_W1145`; Vitest `src/kn/cnts/bm11/bm11.test.tsx` | G1 |
| ★ AC-02, BR-03: duyệt xong tự gửi; báo cáo có ngay trong hộp "Báo cáo đến" của HCTH (tuần), không lộ cho phòng không phải nơi nhận; "Đã nhận" một lần | `TestCNTSBM11_W1145` | G1 |
| ★ BR-02: báo cáo đã gửi không sửa; số kỳ trước thay đổi sau khi gửi vào mục "Số liệu kỳ trước nộp bổ sung" | `TestCNTSBM11_W1145` (sửa sau gửi → 409), `TestBM11DiffAndText` | G1 |
| ★ Xuất Word theo bố cục bản giấy (tuần A–D, tháng/quý/năm I–III), số `{n}/BC-CNTS` chỉ cho bản đã gửi, xuất lại dùng lại số | `TestCNTSBM11_W1145`, `TestBM11WordTemplates` | G1 |
| Màn "Báo cáo định kỳ" (Cấp Tỉnh CNTS) và "Báo cáo đến" (HCTH, KHTC); menu | Vitest `bm11.test.tsx`, `kn-menu.contract.test.ts`; `TestWebMenuFixture` | G1 |
| Email công vụ, thông báo cho phòng nhận, phụ lục kinh phí của báo cáo tháng, cấu hình nơi nhận theo loại trên màn riêng | — | Chưa làm |