# Ma trận BRD/MOM ↔ màn HCTH, KHTC, Báo giá — BF-BRD-MATRIX (#116)

Ngày đối chiếu: 24/10/2026. Snapshot mã tích hợp: `520374d794cf17666d33f8ee189346cc9958a16f` (dev sau #69). Chỉ tệp này nằm trong ngoại lệ lane:kn đã giao cho Codex; không sửa API, màn hay mẫu để làm thay đổi kết quả đối chiếu.

## 1. Nguồn và cách đọc

Nguồn gốc chung: `G:/Shared drives/Product team/01. Tai Lieu Khuyen Nong/`. Đọc DOCX bằng Python `-I`, `zipfile`, regex `<w:t>` và giải mã entity; không chạy Word/macro. Pn dưới đây là thứ tự đoạn `w:p` trong `word/document.xml` (tính cả đoạn trống); FN được định danh cùng phòng để tránh trùng mã giữa KHTC/Báo giá.

| Nguồn | Phiên bản/ngày và vị trí | SHA-256 |
|---|---|---|
| M-HC | MOM HCTH 01/10; `1.1 Khuyen Nong An Giang/01. MOM/MoM_Phòng HCTH x MKG_01.10.docx` | `98971cc2f6c307424e75353002fcc22c919442fe8b60ef9de9152bc8f8bd3ca8` |
| M-KH | MOM KHTC 25/09; `1.1 Khuyen Nong An Giang/01. MOM/MoM_Phòng KHTC x MKG_25.09.docx` | `cf6cd36464551a50f45235ca55434d2e6226abc42f3e2a1b2b6665a4688171a4` |
| M-BG | MOM TTTT 02/10; `1.1 Khuyen Nong An Giang/01. MOM/MoM_Phong_TTTT x MKG_02.10.docx` | `8ced6b1c6a40078e2bec910153b0e6c226c412ab8bb0e33d18699f82842a1148` |
| HC | BRD HCTH v3.0, 07/10/2026; `1.2 App Khuyến Nông/01. BRD/BRD_Khuyen_nong_Module_Hanh_chinh_Tong_hop_v3_0.docx` | `1ed44aa50d2f797422ef63b32948f24c4f3856f0419814cf274cb64ac6f2db66` |
| KH | BRD KHTC v2.3, 08/10/2026; `1.1 Khuyen Nong An Giang/05. KẾ HOẠCH TÀI CHÍNH/01.preparation/BRD_He_thong_Khuyen_nong_Module_Ke_hoach_Tai_chinh_v2_3.docx` | `15859351f90574bf667190b1a12160465ba03f8ca9e408b94c90d285a3502e25` |
| BG | BRD Báo giá v1.3, 07/10/2026; `1.2 App Khuyến Nông/01. BRD/Copy of BRD_He_thong_Khuyen_nong_Module_Bao_gia_nong_san.docx` | `1798438857d3e22536c682d522c02b39b6cf29c14226860dfdc1adcf7609dad1` |

**Trạng thái:** Đạt = đủ yêu cầu trong hàng với bằng chứng đã kiểm; Lệch = có luồng/màn nhưng còn khác biệt nêu rõ hoặc nghiệm thu cần hoàn tất; Thiếu = chưa có màn/luồng chuyên biệt trong snapshot dev. Các hàng Thiếu có nhánh đang chờ review được ghi đúng ticket, không có nghĩa owner chưa viết mã. Ma trận này là kiểm source, không thay biên bản UAT hay bằng chứng trình duyệt. Vì nhóm A chưa được áp đầy đủ và nhiều gói owner chưa gộp, không đánh dấu toàn phòng “đạt”.

BRD mới hơn làm chuẩn mô tả thay cho cách làm hiện tại trong MOM; các quyết định PO sau đó giữ ưu tiên. KHTC dùng **v2.3**, tài liệu UAT tên v2.2 cũ chỉ là chứng cứ lịch sử. Không thay quyết định tenant KN từng Trung tâm tỉnh, MRV thuộc HTX/MKG, D2 G1 chỉ hồ sơ KN, mẫu KHTC nạp sẵn hay M1–M7.

## 2. Nhóm A áp cho cả ba phòng (R-E1)

| Mã | Màn/nguồn đã đọc | Trạng thái và việc cần làm |
|---|---|---|
| A1 — số không âm, lỗi tiếng Việt | `apps/web-ops/src/kn/hrm/PromotionScreens.tsx:1`; `apps/web-ops/src/kn/khtc/forms/KhtcFormView.tsx:200`; `apps/web-ops/src/kn/price/tickets/SheetLines.tsx:149` | Lệch: chưa dùng NumberField đã thống nhất. #97 chưa vào dev; review snapshot ea79f5c5 còn lỗi precision ở form.tsx:36–39 (3 ca tái hiện), cần owner sửa trước #117. Trường điều chỉnh/chênh lệch có ý nghĩa âm không được ép không âm; giữ decimal và kiểm server. |
| A2 — trang riêng, breadcrumb, quay lại, nút phải, cột Log | `apps/web-ops/src/kn/hrm/CatalogScreen.tsx:508`; `apps/web-ops/src/kn/khtc/reports/KhtcReportsScreen.tsx:21`; `apps/web-ops/src/kn/price/tickets/EntryScreen.tsx:163` | Lệch: các dialog sửa nghiệp vụ và trang thiếu bố cục log chưa chuyển. #117 áp đúng DetailPage sau97; owner KN nhận phần ngoài ngoại lệ. |
| A3 — typography: Noto Serif / Roboto | `apps/web-ops/src/app/globals.css:48`, `:86`, `:93`, `:377` | Lệch một phần: font nền Roboto và h1–h3 Noto Serif đã có; tên khối/thẻ không dùng h1–h3 (vd sec-title) chưa có quy tắc serif đầy đủ. #97/#117 cần kiểm computed font tại ba phòng, giữ font tự host/RB-7. Không nhầm A3 với popup (popup thuộc A2). |
| A4 — dấu * và lỗi theo trường | `apps/web-ops/src/kn/khtc/forms/KhtcFormView.tsx:160`; `apps/web-ops/src/kn/price/CalendarConfig.tsx:59` | Lệch: có kiểm máy chủ nhưng UI đang gom lỗi/title và form thủ công. #117 nối FormField/validateForm, giữ mã lỗi/guard cũ. |
| A5 — STT ở danh sách | `apps/web-ops/src/kn/hrm/ReportScreens.tsx:73`; `apps/web-ops/src/kn/khtc/reports/KhtcReportsScreen.tsx:94`; `apps/web-ops/src/kn/price/reports/ReportScreens.tsx:1` | Lệch: còn bảng thô, chưa thống nhất STT. #117 dùng DataTable; không sửa bố cục biểu NS/phiếu KHTC/phiếu in cố định hoặc golden chỉ để thêm cột. |

A1–A5 phải kiểm ở danh sách + tạo/sửa + chi tiết của mọi hàng có màn bên dưới, với từng vai được phép. Các hàng có “Sắp có” chỉ kiểm placeholder/menu, chưa được tính là đạt flow. Ngoài R-E1: sidebar ba nhóm/footer (R-A6) do #98/#76 phối hợp; không làm trong #116.

## 3. HCTH — 21 chức năng

| BRD / đoạn gốc | Yêu cầu | Màn | Bằng chứng mã | Trạng thái | Khác biệt / ticket | Khách chấp nhận / UAT |
|---|---|---|---|---|---|---|
| FN-01 · P751 | Quản trị danh mục đơn vị hành chính | `/kn/quan-tri/don-vi-hanh-chinh` | `apps/web-ops/src/kn/cycle/screens/AdminScreens.tsx:1276` | Lệch | Có màn danh mục; mẫu thêm/sửa còn dùng dialog, cần chuẩn A2/A4. #97 → owner màn KN. | Chưa có UAT. |
| FN-02 · P784 | Quản lý cơ cấu tổ chức 3 cấp | `/kn/quan-tri/co-cau-to-chuc` | `apps/web-ops/src/kn/cycle/screens/AdminScreens.tsx:970` | Lệch | Có cây 3 cấp; đối chiếu trang chi tiết/cột log và form A2/A4 trước nghiệm thu. Không đổi quyền theo cây bằng UI. #98. | Chưa có UAT. |
| FN-03 · P819 | Quản lý danh mục phòng ban | `/kn/quan-tri/phong-ban` | `apps/web-ops/src/kn/cycle/screens/AdminScreens.tsx:1112` | Lệch | Danh mục đã có; thêm/sửa còn dialog, cần A2/A4; giữ phòng ban điều khiển KNContext. Owner KN. | Chưa có UAT. |
| FN-04 · P852 | Quản lý tài khoản người dùng | `/kn/quan-tri/nguoi-dung` | `apps/web-ops/src/app/kn/quan-tri/nguoi-dung/page.tsx:1` | Lệch | Màn KN riêng có, menu Admin còn trỏ tài khoản native: #120. Shared #114 chỉ kiểm TT-13, không cấp quyền native cho kn_tinh. | Chưa có UAT. |
| FN-05 · P894 | Đăng nhập & chọn phòng ban | `/dang-nhap → chọn vai/phòng` | `apps/web-ops/src/app/kn/quan-tri/nguoi-dung/page.tsx:1` | Lệch | Dùng đăng nhập chung/KNContext, không phát triển login HCTH riêng. Cần nghiệm thu luồng chọn vai/phòng và menu cùng KNSPLIT; không hạ fail-closed. #53/#76/#98. | Chưa có UAT. |
| FN-06 · P963 | Dashboard nhân sự | `/kn/nhan-su/dashboard` | `apps/web-ops/src/kn/hrm/ReportScreens.tsx:466` | Lệch | Đã có chỉ số/phân bố/drill-down; HCTH thật còn gate Q-005 và E2E #23, chuẩn A theo mục chung. | Chưa có UAT. |
| FN-07 · P998 | Quản lý hồ sơ nhân sự | `/kn/nhan-su/ho-so[/id]` | `apps/web-ops/src/kn/hrm/StaffScreens.tsx:1169` | Lệch | Có thẻ hồ sơ/công tác/ngạch-bậc/TK Sở/log; giữ TK chỉ HCTH, không CCCD/BHXH/bảng lương. Form sửa còn dialog, gate Q-005 và A2/A4. #117/#23. | Chưa có UAT. |
| FN-08 · P1284 | Thông tin làm việc | `/kn/nhan-su/ho-so/thong-tin-lam-viec[/id]` | `apps/web-ops/src/kn/hrm/WorkInfoScreens.tsx:86` | Lệch | Có danh sách/chi tiết thông tin làm việc; chuẩn hóa form số/bắt buộc/trang log theo A1/A2/A4. #117. | Chưa có UAT. |
| FN-09 · P1320 | Danh sách nâng bậc (NL01–NL04) | `/kn/nhan-su/nang-bac[/key]` | `apps/web-ops/src/kn/hrm/PromotionScreens.tsx:141` | Lệch | NL01–04 và kiểm điều kiện có mã (#18 đã vào dev); dialog nhập quyết định/điều kiện còn cần A2/A4, nhắc nâng bậc #48/#50 chưa hoàn tất. | Chưa có UAT. |
| FN-10 · P1570 | Quyết định nâng bậc (sổ quyết định) | `/kn/nhan-su/quyet-dinh-nang-bac[/id]` | `apps/web-ops/src/kn/hrm/DecisionScreens.tsx:72` | Lệch | Có sổ quyết định đã ký ngoài hệ thống; không thêm trình ký/tiền lương. Kiểm A2/A4 và link người/đính kèm trong E2E #23/#117. | Chưa có UAT. |
| FN-11 · P1607 | Kế hoạch nhân lực năm | `/kn/nhan-su/ke-hoach-nhan-luc` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Chưa có màn chuyên biệt ở dev; owner #20 có nhánh kiểm 103 PASS nhưng hai finding plan_profile/plan chưa đóng. Không lấy tên route placeholder làm hoàn tất. | Chưa có UAT. |
| FN-12 · P1642 | Biểu mẫu báo cáo Sở (NS01–NS06) | `/kn/nhan-su/bao-cao-so[/code]` | `apps/web-ops/src/kn/hrm/ReportScreens.tsx:107` | Lệch | NS01–06/check/xuất Excel/lịch sử có; một số bảng thô chưa STT (A5), Q-005/E2E #23 còn mở. Không ghi thêm dữ liệu nhạy cảm ngoài v3.0. | Chưa có UAT. |
| FN-13 · P1703 | Contact | `/kn/dung-chung/contact` | `apps/web-ops/src/kn/contact/ContactScreens.tsx:68` | Lệch | field_set đã tách TW/Xã với Tỉnh/Admin; thêm/sửa liên hệ còn dialog (A2/A4), nguồn HRM vẫn gate Q-005. #117/#23. | Chưa có UAT. |
| FN-14 · P1745 | Kho tài liệu nhân sự | `/kn/nhan-su/kho-tai-lieu[/id]` | `apps/web-ops/src/kn/hrm/DocScreens.tsx:458` | Lệch | Có thư mục/tệp, quyền nguồn và log; form/thao tác còn cần chuẩn A2/A4. Không thay VNPT bằng tính năng suy đoán. #117. | Chưa có UAT. |
| FN-15 · P1782 | Mẫu đánh giá xếp loại (Mẫu số 05, Mẫu số 06) | `/kn/nhan-su/danh-gia` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Menu đánh giá ComingSoon ở dev; owner #19 phụ trách mẫu MS05/MS06, không coi catalog thang điểm hiện có là đầy đủ luồng. | Chưa có UAT. |
| FN-21 · P1818 | Phiếu đánh giá nhân sự | `/kn/nhan-su/danh-gia-cua-toi / link OTP` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Phiếu/link OTP cho người không có tài khoản chưa có màn chuyên biệt trên dev; #19 và cổng OTP, phải giữ kiểm chủ thể/hạn link. | Chưa có UAT. |
| FN-16 · P1862 | Đợt đánh giá | `/kn/nhan-su/danh-gia` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Đợt đánh giá, khóa mẫu/theo thời hạn đợi owner #19 vào dev. | Chưa có UAT. |
| FN-17 · P1902 | Đề xuất xếp loại | `/kn/nhan-su/danh-gia` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Đề xuất xếp loại và chuyển cấp đánh giá đợi #19; không thay bằng nhập kết quả năm tay. | Chưa có UAT. |
| FN-18 · P2019 | Tổng hợp kết quả và xuất Mẫu số 05, 06 | `/kn/nhan-su/danh-gia` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Tổng hợp 4 quý/kết quả năm/xuất MS05/06 đợi #19, nghiệm thu #23. | Chưa có UAT. |
| FN-19 · P2059 | Danh mục nhân sự | `/kn/phong-ban/hanh_chinh_tong_hop/danh-muc-nhan-su` | `apps/web-ops/src/kn/hrm/CatalogScreen.tsx:78` | Lệch | Catalog có phiên bản/thang/ngạch; nhiều ItemDialog/ScaleDialog/RuleDialog còn sửa dữ liệu bằng popup, trái A2. #117. | Chưa có UAT. |
| FN-20 · P2099 | Phân quyền hồ sơ | `/kn/phong-ban/hanh_chinh_tong_hop/phan-quyen-ho-so` | `apps/web-ops/src/kn/hrm/StaffScreens.tsx:42` | Lệch | Ma trận quyền hồ sơ có; cần E2E đúng nhân sự/phòng/địa bàn, dữ liệu nhạy cảm và chuẩn A2/A4. #23/#117. | Chưa có UAT. |

## 4. KHTC — 18 chức năng v2.3

| BRD / đoạn gốc | Yêu cầu | Màn | Bằng chứng mã | Trạng thái | Khác biệt / ticket | Khách chấp nhận / UAT |
|---|---|---|---|---|---|---|
| FN-CH-00.01 · P687 | Đăng nhập, chọn vai trò & phòng ban | `/dang-nhap → KNContext` | `apps/web-ops/src/app/kn/quan-tri/nguoi-dung/page.tsx:1` | Lệch | Luồng dùng chung; KHTC-DEFAULT-NAV #44 đã vào dev. Cần kiểm menu/profile cùng KNSPLIT, không dùng quyền bản cũ cho replay. | Chưa có UAT. |
| FN-TW-01.01 · P716 | Báo cáo đã xuất (dùng chung) | `/kn/khtc/bao-cao-da-xuat` | `apps/web-ops/src/kn/khtc/reports/KhtcReportsScreen.tsx:21` | Lệch | Có chế độ history dùng chung; màn TW/trang chủ owner #34 chưa vào dev. Bản xuất đã gửi bất biến, không tự phát hành lại (M5). | Chưa có UAT. |
| FN-TI-02.01 · P745 | Trang chủ tổng hợp | `/kn/khtc (mặc định theo profile)` | `apps/web-ops/src/kn/khtc/reports/KhtcReportsScreen.tsx:21` | Thiếu | Trang chủ tổng hợp riêng theo BRD do #34; không coi trang soạn báo cáo là dashboard đã nghiệm thu. | Chưa có UAT. |
| FN-TI-02.02 · P778 | Danh sách chương trình hoạt động | `/kn/khtc/ke-hoach-nam` | `apps/web-ops/src/kn/khtc/PlanScreen.tsx:51` | Lệch | Có kế hoạch năm; v2.3 yêu cầu điều chỉnh bản đang áp dụng kèm quyết định/ngày hiệu lực BR-DL-21. Cần đối chiếu đầy đủ phiên bản, UI A1/A2/A4. #117/owner KHTC. | Chưa có UAT. |
| FN-TI-02.03 · P839 | Kỳ báo cáo và tiến độ nộp | `/kn/khtc/ky-bao-cao` | `apps/web-ops/src/kn/khtc/periods/KhtcPeriodsScreen.tsx:41` | Lệch | Có kỳ/tiến độ/snapshot nguồn; dùng M1–M7 đã chốt (đặc biệt hạn 10:00 Thứ Tư), không tự đổi theo hạn nguồn khác. Form/dialog và STT cần #117. | Chưa có UAT. |
| FN-TI-02.04 · P889 | Rà soát phiếu số liệu | `/kn/khtc/ra-soat/[periodId]` | `apps/web-ops/src/kn/khtc/forms/KhtcFormScreens.tsx:7` | Lệch | Có phiếu/kiểm/lịch sử; cần trạng thái đủ + trình bày lỗi theo trường A4. Không cho sửa ô nguồn trực tiếp. | Chưa có UAT. |
| FN-TI-02.05 · P924 | Báo cáo tuần (Mẫu 1) | `/kn/khtc/bao-cao (mau_1)` | `apps/web-ops/src/kn/khtc/reports/KhtcReportsScreen.tsx:99` | Lệch | Nguồn/số liệu/lời văn/Excel có; chưa có nút xuất Word trên dev, chưa là biên tập toàn văn như Word v2.3 (BR-DL-19). #17/export và owner KHTC; giữ M1–M7. | Chưa có UAT. |
| FN-TI-02.06 · P1120 | Báo cáo tháng (Mẫu 2) | `/kn/khtc/bao-cao (mau_2)` | `apps/web-ops/src/kn/khtc/reports/KhtcReportsScreen.tsx:99` | Lệch | Mẫu 2 dùng editor chung; Word và bảng/mục biên tập v2.3 chưa đủ bằng chứng, cần owner/export; không đổi số nguồn khóa. | Chưa có UAT. |
| FN-TI-02.07 · P1246 | Báo cáo quý (Mẫu 3) | `/kn/khtc/bao-cao (mau_3)` | `apps/web-ops/src/kn/khtc/reports/KhtcReportsScreen.tsx:99` | Lệch | Mẫu 3 dùng editor chung; cần kiểm quý/6 tháng/9 tháng theo tuần Thứ Ba thuộc kỳ và Word; #17/owner KHTC. | Chưa có UAT. |
| FN-TI-02.08 · P1329 | Báo cáo năm (Mẫu 4) | `/kn/khtc/bao-cao (mau_4)` | `apps/web-ops/src/kn/khtc/reports/KhtcReportsScreen.tsx:99` | Lệch | Mẫu 4 dùng editor chung; cần Word, lũy kế năm đúng M3 và điều chỉnh chỉ cùng năm; không coi chứng cứ v2.2 là nghiệm thu v2.3. | Chưa có UAT. |
| FN-TI-02.09 · P1355 | Báo cáo tiến độ chương trình (Mẫu 5) | `/kn/khtc/tien-do / bao-cao (mau_5)` | `apps/web-ops/src/kn/khtc/reports/KhtcReportsScreen.tsx:80` | Lệch | Có lọc tiến độ/Mục II gợi từ kỳ trước; thiếu Word, bảng thô chưa STT và cột Log/DetailPage. #17/#117. | Chưa có UAT. |
| FN-TI-02.10 · P1456 | Báo cáo phát sinh | `/kn/khtc/bao-cao-phat-sinh` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Owner #33 chưa vào dev; MOM chỉ hỗ trợ theo mẫu, không cam kết trình soạn báo cáo tự do/trợ lý AI. | Chưa có UAT. |
| FN-PN-03.01 · P1487 | Phiếu kỳ này, phiếu đã gửi | `/kn/bao-cao-khtc / da-gui` | `apps/web-ops/src/kn/khtc/inbox/DepartmentFormsScreen.tsx:31` | Lệch | Có phiếu hiện tại/đã gửi theo phòng; cần đủ trạng thái, lỗi bắt buộc và layout A2/A4, giữ template-scope hiện hành. #71/#117. | Chưa có UAT. |
| FN-PN-03.02 · P1514 | Nhập phiếu số liệu | `/kn/khtc/phieu/[periodId]` | `apps/web-ops/src/kn/khtc/forms/KhtcFormView.tsx:181` | Lệch | Có tab/cột/hạng mục/công thức và ô nguồn khóa; kiểm Excel import/export đúng Mã hạng mục BR-DL-20 và lỗi từng trường, không thêm STT làm đổi mẫu cố định. #117/owner KHTC. | Chưa có UAT. |
| FN-PN-03.03 · P1595 | Tiến độ chương trình của phòng, báo cáo KHTC đã xuất | `/kn/phong-ban/[dept]/bao-cao-khtc` | `apps/web-ops/src/kn/khtc/inbox/DepartmentFormsScreen.tsx:31` | Lệch | Có inbox theo phòng; trang tiến độ chỉ xem/báo cáo đã xuất cần owner #34 và chuẩn A5. Không cho phòng khác sửa số của chủ nguồn. | Chưa có UAT. |
| FN-AD-04.01 · P1651 | Danh mục KHTC | `/kn/phong-ban/ke_hoach_tai_chinh/danh-muc-khtc` | `apps/web-ops/src/kn/khtc/CatalogScreen.tsx:27` | Lệch | Có catalog; đối chiếu nhóm/chương trình/hạng mục theo năm và form A1/A2/A4. Owner KHTC/#117. | Chưa có UAT. |
| FN-AD-04.02 · P1679 | Mẫu phiếu số liệu KHTC (dùng chung FN-AD-04.06) | `/kn/phong-ban/[dept]/mau-bao-cao` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Lệch | BRD mô tả thiết kế mẫu đầy đủ; PO đã chốt G1 giữ mẫu nạp sẵn. Ghi khác biệt phạm vi, không tự mở thêm builder hoặc coi UI placeholder là đã đạt BRD đầy đủ. | PO chốt chỉ mẫu seed ở G1; chưa UAT toàn BRD. |
| FN-AD-04.03 · P1709 | Mục lục mẫu báo cáo | `/kn/khtc/mau-bao-cao` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Mục lục động/biến khóa/preview v2.3 do #31; dev chưa có màn chuyên biệt. Không đổi seeded templates để che thiếu chức năng. | Chưa có UAT. |

## 5. Báo giá/TTTT — 28 chức năng

| BRD / đoạn gốc | Yêu cầu | Màn | Bằng chứng mã | Trạng thái | Khác biệt / ticket | Khách chấp nhận / UAT |
|---|---|---|---|---|---|---|
| FN-CH-00.01 · P631 | Đăng nhập, chọn vai trò & phòng ban | `/dang-nhap → KNContext` | `apps/web-ops/src/app/kn/quan-tri/nguoi-dung/page.tsx:1` | Lệch | Đăng nhập/profile dùng chung; không tạo realm/quyền riêng qua UI, cần full KNSPLIT/menu. | Chưa có UAT. |
| FN-TW-01.01 · P682 | Bảng giá tổng hợp chung | `/kn/bao-gia/bang-gia` | `apps/web-ops/src/kn/price/reports/ReportScreens.tsx:503` | Lệch | Có bảng đã duyệt và lọc theo ngày/nhóm; bảng thô cần A5, không mở dữ liệu chi tiết chưa duyệt sang công khai. #117. | Chưa có UAT. |
| FN-TW-01.02 · P719 | Xem báo cáo tăng giảm, biến động giá | `/kn/bao-gia/bien-dong` | `apps/web-ops/src/kn/price/reports/ReportScreens.tsx:635` | Lệch | Có báo cáo biến động; kiểm A5/export theo bộ lọc và cổng dữ liệu đã duyệt, không thêm dự báo AI. #117. | Chưa có UAT. |
| FN-TW-01.03 · P756 | Dashboard tổng hợp giá nông sản theo vùng | `/kn/bao-gia/dashboard-vung` | `apps/web-ops/src/kn/price/reports/ReportScreens.tsx:829` | Lệch | Có dashboard vùng; cần kiểm bộ lọc/nguồn đã duyệt/layout theo A2/A5 trong #117. | Chưa có UAT. |
| FN-TW-01.04 · P791 | Báo cáo giá thành sản xuất theo vụ | `/kn/bao-gia/gia-thanh` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Điều tra và báo cáo giá thành owner #35 chưa vào dev; dữ liệu PII còn gate AG_PRICE_SURVEY_PII/Q-005. Không bật gate từ tài liệu. | Chưa có UAT. |
| FN-TI-02.01 · P830 | Dashboard thống kê các xã chưa hoàn thiện báo giá | `/kn/bao-gia/tien-do` | `apps/web-ops/src/kn/price/reports/ReportScreens.tsx:276` | Lệch | Có dashboard xã chưa đủ; cần STT và hành động nhắc đúng vai/phạm vi, #117. | Chưa có UAT. |
| FN-TI-02.02 · P869 | Xem chi tiết toàn bộ báo giá | `/kn/bao-gia/kiem-duyet` | `apps/web-ops/src/kn/price/tickets/ReviewScreen.tsx:34` | Lệch | Có chi tiết/đối chiếu; form và log chưa theo DetailPage/A2/A4 đầy đủ. #117. | Chưa có UAT. |
| FN-TI-02.03 · P905 | Cảnh báo biến động bất thường, yêu cầu Xã xác minh | `/kn/bao-gia/kiem-duyet ↔ xac-minh` | `apps/web-ops/src/kn/price/tickets/ReviewScreen.tsx:34` | Lệch | Có cảnh báo/yêu cầu xác minh; phải kiểm trạng thái đã phản hồi/chưa phản hồi và quyền trước duyệt. Chuẩn A2/A4/#117. | Chưa có UAT. |
| FN-TI-02.04 · P960 | Kiểm tra, chỉnh sửa số liệu, xóa phiếu | `/kn/bao-gia/kiem-duyet` | `apps/web-ops/src/kn/price/tickets/ReviewScreen.tsx:34` | Lệch | Có chỉnh/xóa/ghi lý do; chỉnh dữ liệu bằng dialog cần trang A2, xóa vẫn được confirm. Giữ native audit/khóa kỳ. | Chưa có UAT. |
| FN-TI-02.05 · P1004 | Duyệt bảng giá | `/kn/bao-gia/kiem-duyet → bang-gia` | `apps/web-ops/src/kn/price/tickets/ReviewScreen.tsx:34` | Lệch | Có duyệt; cần regression không duyệt khi còn lỗi/chưa xác minh, và log A2/A4; #117 không đổi API. | Chưa có UAT. |
| FN-TI-02.06 · P1047 | Mở duyệt để sửa và cập nhật lại dữ liệu bảng giá | `/kn/bao-gia/kiem-duyet` | `apps/web-ops/src/kn/price/tickets/ReviewScreen.tsx:34` | Lệch | Có mở duyệt; kiểm lý do/phiên bản/lịch sử và cập nhật công bố đúng nguồn. Owner API xử lý nếu phát hiện gap, không sửa API trong117. | Chưa có UAT. |
| FN-TI-02.07 · P1087 | Tích hợp website Khuyến nông, đăng bảng giá lên website | `website / cổng tích hợp` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Owner #36 chưa vào dev; giả định A2 website vẫn chờ đầu mối/hợp đồng kỹ thuật, không coi renderer thành production-acceptance. | Chưa có UAT. |
| FN-TI-02.08 · P1118 | Thống kê phiếu báo giá | `/kn/bao-gia/thong-ke` | `apps/web-ops/src/kn/price/reports/ReportScreens.tsx:1024` | Lệch | Có thống kê phiếu; đối chiếu số phiếu/in tháng với bộ lọc và A5. Không bổ sung thanh toán CTV ngoài scope. | Chưa có UAT. |
| FN-TI-02.09 · P1152 | Nhập thay báo giá nộp muộn | `/kn/bao-gia/kiem-duyet` | `apps/web-ops/src/kn/price/tickets/ReviewScreen.tsx:34` | Lệch | Có nhập thay sau khóa (vai Tỉnh); kiểm lý do/không cho Xã bypass 12:00, chuẩn trang A2/A4. #117. | Chưa có UAT. |
| FN-TI-02.10 · P1190 | Quản lý đợt điều tra giá thành | `/kn/bao-gia/dieu-tra` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Quản lý đợt owner #35 đang review; ComingSoon tại dev, không nghiệm thu nhánh riêng như đã tích hợp. | Chưa có UAT. |
| FN-TI-02.11 · P1227 | Nhập dữ liệu điều tra giá thành từ file | `/kn/bao-gia/dieu-tra` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Nhập file/rà soát mapping dữ liệu owner #35; migration cũ owner #38, không in PII ra log. | Chưa có UAT. |
| FN-TI-02.12 · P1266 | Rà soát phiếu điều tra, cảnh báo bất thường, chốt đợt | `/kn/bao-gia/dieu-tra` | `apps/web-ops/src/app/kn/[...slug]/page.tsx:1` | Thiếu | Rà soát/chốt đợt, lỗi/ngoại lệ owner #35; giữ gate/duyệt và không thay nguồn đã chốt. | Chưa có UAT. |
| FN-XA-03.01 · P1308 | Nhập/sửa báo giá theo ngày, trước giờ khóa | `/kn/bao-gia/nhap` | `apps/web-ops/src/kn/price/tickets/EntryScreen.tsx:15` | Lệch | Có nhập/sửa khóa giờ; ô giá trong SheetLines chưa dùng NumberField mới, cần A1/A4 và giữ decimal/kiểm server. #117. | Chưa có UAT. |
| FN-XA-03.02 · P1358 | In phiếu báo giá | `/kn/bao-gia/in` | `apps/web-ops/src/kn/price/tickets/PrintView.tsx:135` | Lệch | Có phiếu in; cần đối chiếu mẫu ký/tháng thực tế, không thay mẫu in cố định chỉ để thêm STT UI. #117/owner. | Chưa có UAT. |
| FN-XA-03.03 · P1393 | Xem lịch sử báo giá của xã | `/kn/bao-gia/lich-su` | `apps/web-ops/src/kn/price/tickets/XaScreens.tsx:162` | Lệch | Có lịch sử; cần STT, chọn ngày/chi tiết đúng xã và A2/A5. #117. | Chưa có UAT. |
| FN-XA-03.04 · P1431 | Nhận nhắc nhập giá buổi sáng | `/thong-bao + lịch nhắc` | `apps/web-ops/src/kn/price/CalendarConfig.tsx:100` | Lệch | Có cấu hình/chạy thử/job nhắc, không còn chỉ Zalo; kiểm kênh app/email và scope trong E2E, tách form cấu hình A2/A4. #117. | Chưa có UAT. |
| FN-XA-03.05 · P1465 | Phản hồi yêu cầu xác minh giá | `/kn/bao-gia/xac-minh` | `apps/web-ops/src/kn/price/tickets/XaScreens.tsx:31` | Lệch | Có phản hồi; kiểm bắt buộc lý do/trạng thái và scope trước/sau khóa, chuyển sửa chi tiết khỏi popup A2/A4. #117. | Chưa có UAT. |
| FN-AD-04.01 · P1507 | Tài khoản và phân quyền | `/kn/quan-tri/nguoi-dung` | `apps/web-ops/src/app/kn/quan-tri/nguoi-dung/page.tsx:1` | Lệch | Tài khoản dùng shared; #114 kiểm KN tỉnh không có quyền account_admin ngầm, menu cũ cần120; quyền nghiệp vụ giá vẫn do owner định nghĩa. | Chưa có UAT. |
| FN-AD-04.02 · P1553 | Danh mục xã báo giá | `/kn/bao-gia/xa-bao-gia` | `apps/web-ops/src/kn/price/points/PointsScreen.tsx:43` | Lệch | Có xã báo giá theo danh mục, không hard-code26; thêm/sửa còn dialog cần A2/A4. #117. | Chưa có UAT. |
| FN-AD-04.03 · P1593 | Danh mục mặt hàng, nhóm hàng theo thời kỳ | `/kn/bao-gia/mat-hang` | `apps/web-ops/src/kn/price/items/ItemsScreen.tsx:49` | Lệch | Có mặt hàng/nhóm/hiệu lực; trạng thái menu/đề xuất owner #37/#98. Form dialog cần A1/A2/A4. #117. | Chưa có UAT. |
| FN-AD-04.04 · P1649 | Cấu hình kỳ báo giá, giờ khóa, hạn phản hồi | `/kn/bao-gia/cau-hinh` | `apps/web-ops/src/kn/price/CalendarConfig.tsx:59` | Lệch | Có lịch/ngoại lệ/giờ khóa; cấu hình số và bắt buộc cần A1/A4, log ở trang chi tiết A2. Không đổi 12:00 bằng thay đổi UI. | Chưa có UAT. |
| FN-AD-04.05 · P1689 | Cấu hình nhắc nhập giá | `/kn/bao-gia/cau-hinh` | `apps/web-ops/src/kn/price/CalendarConfig.tsx:100` | Lệch | Có nhắc/kênh/mẫu/chạy thử; dùng UI-KIT sau97, giữ job/idempotency và không gửi mail thật khi chưa cấu hình. | Chưa có UAT. |
| FN-AD-04.06 · P1726 | Cấu hình ngưỡng cảnh báo giá bất thường | `/kn/bao-gia/cau-hinh` | `apps/web-ops/src/kn/price/CalendarConfig.tsx:115` | Lệch | Có ngưỡng phần trăm dạng decimal; cần lỗi cạnh trường/dấu bắt buộc A1/A4 và trang chi tiết. #117. | Chưa có UAT. |

## 6. MOM và quyết định PO cần giữ khi sửa

| Nguồn | Yêu cầu / điểm khác biệt | Màn và trạng thái / hành động |
|---|---|---|
| M-HC P12–15, P40–43 | Biểu Sở thay đổi, cần nguồn dữ liệu một lần và chống ghi đè | Hồ sơ + NS01–06 có mã, Lệch trước E2E23 và A. Không đưa trường CCCD/BHXH/bảng lương vào HRM; HC v3.0 P24/P30/P1002 giới hạn dữ liệu và TK Sở. |
| M-HC P23–28, P48–51 | Cá nhân → lãnh đạo → HCTH, tổng hợp bốn quý/xếp loại năm | FN15–18/21 Thiếu trên dev, owner19. Không thay bằng ô nhập kết quả tay hiện dùng xét nâng bậc. |
| M-HC P35–38, P52–53 | Tài sản/tài chính có hệ riêng; VNPT cần trao đổi kỹ thuật | Ngoài phần phải làm của sweep117; không tự xây phần mềm kế toán/quản lý tài sản, không hứa tích hợp VNPT khi chưa có cổng. |
| M-KH P8–15, P23–25 | Tuần/tháng/quý/năm, 12 chương trình, dữ liệu sáu phòng, bảng + lời văn + Word | KH FN02.05–02.09 Lệch: editor/Excel có, Word và các điểm v2.3 chưa đóng. KHTC giữ số nguồn khóa, sửa số tại owner nguồn. |
| M-KH P26–29 | Trợ lý ảo sau; báo cáo đột xuất không cam kết hoàn toàn tự do | Không thêm AI/Word editor tự do trong117. Phát sinh đi theo owner33, phạm vi mẫu/đầu vào đã chốt. |
| M-BG P14–22, P33–38 | Hiện trạng tuần/tháng/Zalo; mong muốn khóa 12h và kiểm chứng trước công bố | Dùng BG v1.3 P13/P28–29 làm chuẩn báo giá ngày; giữ khóa server và scope, không áp lịch KHTC cho báo giá. Website36 còn giả định A2, điều tra35 còn gate. |
| PO G1 mẫu KHTC nạp sẵn | Giới hạn bàn giao G1, dù BRD AD04.02 mô tả thiết kế mẫu | Ghi Lệch so toàn BRD nhưng **đúng giới hạn đã duyệt**; không tự mở builder. Chủ động kiểm từng mẫu seed khi UAT, không đổi golden. |
| PO M1–M7 15/10 | `docs/uat/khtc-week-mapping.md:180` | M1 tập kỳ rỗng đã chốt; M2 gốc tuần trước lấy số đóng; M3 lũy kế nguồn hiển thị, năm KHTC cộng trong kỳ; M4 hai ngày giá duyệt gần nhất ≤ chốt; M5 chỉ cán bộ phụ trách cập nhật, không phát hành lại; M6 HTX cuối ngày chốt; M7 hạn tuần 10:00 Thứ Tư kế tiếp. Không diễn giải BRD lịch nguồn để đảo các giá trị này. |

## 7. Điều kiện đóng các khoảng trống

- #97 phải sửa precision và được review chéo/gộp trước #117. #117 chỉ đổi UI HCTH/KHTC/Báo giá, không đổi API/quyền/golden; mọi khoảng trống backend gửi owner ticket hiện có, không tự lấn làn.
- HCTH: owner19/20 + kiểm23; thông báo nâng bậc/kế hoạch48/50. #20 còn finding review cụ thể dù test đã xanh; không coi số PASS là đã chấp nhận nghiệp vụ.
- KHTC: 31 mục lục, 32 nguồn ngoài, 33 phát sinh, 34 trang chủ/TW; 17 cổng xuất Word và adapter nguồn thực tế. Không ghi “đạt v2.3” dựa vào tên mẫu 1–5 hoặc test v2.2.
- Báo giá: 35 điều tra/gia thành, 36 website, 37 đề xuất mặt hàng, 38 dữ liệu cũ; giữ các cờ PII/DPIA/hợp đồng tích hợp. Thiếu trên dev không phủ nhận mã trong nhánh review.
- Checklist UAT theo mỗi hàng: vai/địa bàn/phòng đúng, đọc và ghi bị chặn đúng quyền; danh sách có STT; tạo/sửa có lỗi tiếng Việt tại trường và *; chi tiết đúng bố cục/log; không sửa ô nguồn; xuất/in theo mẫu. Chỉ đổi trạng thái sang Đạt sau link commit đã gộp + lệnh/ảnh UAT thực tế, không sửa fixture để che lỗi.

## 8. Kiểm tính đầy đủ của tài liệu

Đã đối chiếu đủ 67 mã chức năng độc lập theo phòng: 21 HC + 18 KH + 28 BG; có 5 hàng A chung và 8 hàng MOM/PO. Các đường dẫn mã và anchor dòng được kiểm tồn tại tại snapshot nêu đầu tệp. Đây là thay đổi chỉ tài liệu; không phát sinh migration, thay schema, thay quyền, thay menu hoặc dữ liệu HTX.
