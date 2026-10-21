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
| ★ FN-14 AC-02: thu hồi quyền của A chuyển phạm vi cho người nhận (quyền mới cùng phạm vi); A mất quyền sửa hộ ngay, người nhận sửa được | `TestCNTSModel_W1147` | G1 (chuyển việc biểu chưa nộp trong `cap/forms`: chưa) |
| ADR-0055 / Q-KNS-04: KN không tạo dữ liệu HTX | `TestCNTSModel_W1147` (đếm qua `pf_cooperative_pub`), `go run ./tools/knboundary` | G1 |
| Menu Tỉnh (Chương trình, dự án; Hộ mô hình; Cấp quyền xã, nông hộ), Xã (Điểm trình diễn, hộ mô hình; Quyền của tôi), TW (Dự án KNQG) | `TestWebMenuFixture`; `kn-menu.contract.test.ts` | G1 |

Chưa nghiệm thu: FN-14 AC-01/AC-03 (quyền KN vào phạm vi phiên `pf/accounts`, phạm vi KSGM BM-01 — ticket làn shared),
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
