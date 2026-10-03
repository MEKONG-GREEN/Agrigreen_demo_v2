# Bộ câu hỏi tồn đọng — AgriGreen v2 (main @ e6c57dd, 03/10/2026)

Gom mọi điểm còn mở sau G1–G5 và KN Phòng Trồng trọt v7.7. Mỗi câu ghi **người quyết** và **cái đang bị chặn**. Hệ thống hiện chạy bằng giá trị **tạm** (có phiên bản, phải qua người duyệt) cho các câu chưa trả lời. Mã gốc giữ nguyên để tra trong tài liệu nguồn.

Cách trả lời: ghi ngay dưới mỗi câu (`→ ...`), hoặc trả lời theo mã (vd "A1: ...").

---

## A. Quyết định nội bộ Mekong Green (dev đang chờ — trả lời được ngay)

| # | Câu hỏi | Đề xuất của dev | Chặn |
|---|---|---|---|
| A1 | Cán bộ xã có được **cấp mặc định vai `vault_registrar`** để ghi người đại diện HTX, chủ hộ, tên nông dân trên thửa? Không cấp → 403 khi thêm HTX có người đại diện | Cấp mặc định cho vai trò Cấp Xã, ghi nhật ký mục đích. **→ Đã chốt 03/10/2026 — theo đề xuất dev.** Đã làm: pf/accounts tự cấp vai realm `vault_registrar` khi gán `kn_xa` (ghi sổ `account_event` dấu tự cấp), thu lại khi lần gán `kn_xa` cuối bị thu hồi (trừ khi giữ vì lý do khác); mọi lời gọi kho mang mục đích (`client.Purpose`, kho ghi sổ truy cập) — `internal/platform/accounts/app/kn_vault.go`, `TestA1_KNXaDefaultVaultRegistrar`, E2E An Giang bỏ bước Admin cấp tay | FN-XA-03.03, 03.05 |
| A2 | **Thửa ruộng tối thiểu 3 điểm** (BRD v7.7) hay **4 điểm** (hiện tại, chặn tam giác)? | Theo BRD: 3 điểm (cần 1 migration nới ràng buộc). **→ Đã chốt 03/10/2026 — theo đề xuất dev.** Đã làm: `geo/domain.MinPoints = 3` (trùng/thẳng hàng vẫn chặn), migration pf 000084 nới CHECK `point_count >= 3` (`plot_boundary`, `cooperative_boundary`), lõi app di động `kMinBoundaryPoints = 3`, mock web | FN-XA-03.05 |
| A3 | **Khóa tạm 15 phút** sau 5 lần sai (BRD v7.7) thay cho **khóa vĩnh viễn** (GIS US-LOG-01 AC-3 cũ)? Sau bao nhiêu lần khóa tạm thì khóa hẳn? (= Q-21) | Khóa tạm; khóa hẳn sau 3 lần khóa tạm trong 24 h. **→ Đã chốt 03/10/2026 — theo đề xuất dev.** Đã làm: realm Keycloak 26.3 `bruteForceStrategy MULTIPLE`, `failureFactor 5`, chờ 900 s, `permanentLockout true`, `maxTemporaryLockouts 3`, `maxDeltaTimeSeconds 86400`; `realm_test`, `docs/brd/kn-p1-realm-patch.md` (realm production đã nhập phải áp tay) | FN-CH-00.01 |
| A4 | **Link tải báo cáo Chi cục** hết hạn sau 15 phút — quá ngắn cho người nhận ngoài. Thời hạn bao nhiêu, hay luôn gửi tệp đính kèm? | 7 ngày qua link tải có ghi nhật ký trong lãnh thổ. **→ Đã chốt 03/10/2026 — theo đề xuất dev.** Đã làm: liên kết do api-platform phục vụ `GET /v1/public/report-links/{token}` (mã ngẫu nhiên chỉ lưu băm, hạn tham số `reports.dispatch_link_ttl_hours` = 168), sổ truy cập `pf_reports.dispatch_link_access`, qua điểm kiểm soát xuất, Admin thu hồi (`POST /v1/reports/dispatches/{id}/revoke-links`), hết hạn/thu hồi → 410; migration pf 000085; worker cần `AG_REPORT_LINK_BASE` | FN-TI-02.06 |
| A5 | **Xuất lịch công tác** và **tải tệp thửa ruộng** của KN có đi qua điểm kiểm soát chống thất thoát dữ liệu (DLP) không? Có → ở chế độ chặn (Phase K) cán bộ nội bộ có thể bị chặn khi vượt hạn mức | Qua DLP ở chế độ quan sát, miễn chặn cho xuất nội bộ. **→ Đã chốt 03/10/2026 — theo đề xuất dev.** Đã làm: cờ `meteringport.Egress.Internal` (luôn observe, kể cả `AG_EGRESS_MODE=enforce`) dùng cho xuất lịch công tác và tải tệp thửa KN (kênh `report_export`, ghi `egress_event`, tính bất thường); ghi chú triển khai ở ADR-0024 | ADR-0024 |
| A6 | Menu Admin hiện hiện 4 màn Public API (G5) cho mọi Admin (màn tự kiểm vai). Có cần ẩn theo vai? | Ẩn theo vai `api_client_*`/`metering_*`/`billing_*` | — |
| A7 | **Phê duyệt ADR** đang "Đề xuất": 0014–0019 (G3), 0021 (K8s), 0022–0025 (Public API, metering, DLP, Phase K), 0026 (Google Sheets) | Kiến trúc sư + chủ sản phẩm ký | Đóng cổng G3–G5 |
| A8 | **Thanh toán GitHub Actions** của tổ chức MEKONG-GREEN đang lỗi → CI chưa chạy từ G2 | Sửa ở Billing & plans | Mọi bằng chứng CI |
| A9 | Độ tương phản nút chính `#00A82D` chữ trắng ≈ 3,2:1 (dưới WCAG AA). Giữ màu design hay đậm hơn? | Đậm hơn (≥ 4,5:1) | Giao diện KN |
| A10 | Grafana / Loki / Tempo là **AGPL** — có dùng không (cần ADR riêng)? | Dùng Prometheus + OTel, xem log qua công cụ khác | Quan sát |

## B. Khuyến nông — Phòng Trồng trọt v7.7 (TTKN An Giang / TTKNQG)

| # | Câu hỏi | Chặn |
|---|---|---|
| **Q-01** ⚠ | **Tệp Excel gốc (có công thức) của 14 mẫu** T01–T07, M01, V01–V06 (và CC01 nếu có), cùng danh mục dùng trong mẫu (54 cây trồng T04, 40 giống V04, 14 loại máy V03, sâu bệnh T05/T06…) | 12/14 mẫu đang là khung tạm; chặn UR-XA-02…15, FN-XA-03.02, FN-TI-02.05 |
| Q-02 | Tên, mã, thứ tự **6 phòng ban** (đã biết 4; 2 phòng còn lại?) | Menu phòng ban |
| Q-03 | Cán bộ **không có email** kích hoạt tài khoản thế nào? Chấp nhận link đặt mật khẩu thay mật khẩu tạm? Cần SMS (nhà cung cấp trong nước)? | Tạo tài khoản |
| Q-04 | Danh sách **Khu vực** (huyện cũ) và xã thuộc từng khu vực sau sắp xếp 2025 (đang tạm 26 khu vực) | Cộng tổng theo khu vực |
| Q-05 | Giá trị **"Cấp quản lý"**; người duyệt lịch = Trưởng phòng, có ủy quyền Phó phòng? | Lịch công tác |
| Q-06 | Cấp Quốc gia xem HTX/máy/thửa ở đâu khi menu TW không có nhóm Dữ liệu? | Menu TW |
| Q-07 | **"Báo cáo năm"**: cộng dồn các kỳ hay mẫu năm riêng? | FN-TW-01.02 |
| Q-08 | Báo cáo Chi cục dùng **mẫu riêng CC01** hay gửi đúng T01…V06? | FN-TI-02.06 |
| Q-09 | Email Chi cục và CC là **hộp thư đơn vị hay cá nhân**? | Lưu địa chỉ (vault) |
| Q-10 | Xuất **Word** áp dụng cho báo cáo nào? | Xuất báo cáo |
| Q-11 | Chỉ đạo khẩn, nhiệm vụ hỗ trợ, danh bạ trực (đã có từ G1): tắt hẳn ở An Giang hay giữ ở module dùng chung? | Menu |
| Q-12 | Báo cáo Đề án 1 triệu ha còn cần không; chuyển thành "Báo cáo đột xuất"? | Mẫu đột xuất |
| Q-13 | Công cụ quản trị nền tảng đặt trong nhóm Quản trị hệ thống của Admin? (đang tạm "Công cụ nền tảng") | Menu Admin |
| Q-14 | "Email công vụ" là **máy chủ thư tỉnh hay Google Workspace**? Có văn bản chấp thuận dùng Google Sheets (ngoài lãnh thổ) cho số liệu tổng hợp? Chia sẻ link theo email hay "ai có liên kết"? | Gửi Chi cục; ADR-0026 |
| Q-15 | Tự nhắc: trước hạn bao lâu, nhắc lại khi trễ bao lâu, ai nhận bản sao? (tạm 24 h / 24 h) | Nhắc nộp |
| Q-16 | Lịch của cán bộ Cấp Tỉnh do ai duyệt; hạn đăng ký / hạn duyệt mặc định? (tạm 17:00 thứ Sáu) | Lịch công tác |
| Q-17 | **Chia kỳ**: tuần ISO ("Tuần 36") hay tuần thứ n của vụ; tuần bắt đầu thứ Hai; báo cáo tháng nộp trước ngày 10? | Phát hành biểu |
| Q-18 | Danh mục vụ (Đông Xuân, Hè Thu, Thu Đông, lúa mùa) và khoảng ngày mặc định | Kỳ báo cáo |
| Q-19 | Bộ nhận diện TTKNQG: tên sản phẩm, logo, ảnh nền đăng nhập, footer (đang tạm "Khuyến nông số") | Giao diện |
| Q-20 | Nạp **số liệu lịch sử** từ Google Sheet để so sánh cùng kỳ năm trước? | Dashboard |
| Q-21 | (xem A3) Số lần khóa tạm trước khi khóa hẳn — **đã trả lời 03/10/2026: 3 lần khóa tạm trong 24 h** | Đăng nhập |
| Q-22 | MFA cho Admin: TOTP đủ? Admin Mekong Green dùng tài khoản riêng từng người? | Admin |
| Q-23 | Bản đồ nền vệ tinh cho vẽ thửa offline: nguồn tile nào (trong lãnh thổ hoặc qua proxy ADR-0007)? | Vẽ thửa |

## C. Public API & bên thứ ba — G5 (MKG / pháp chế)

| # | Câu hỏi | Chặn |
|---|---|---|
| Q5-1 | **Bên thứ ba đầu tiên** là ai (đề xuất nhà máy sinh khối)? Sản phẩm, độ chi tiết, kỳ? Hợp đồng `SUB-DATA` 450 triệu đ có thật, kỳ nào? | Duyệt hợp đồng dữ liệu, bật Phase K |
| Q5-2 | **Pháp nhân vận hành** Public API (đăng ký khách hàng, lập hóa đơn)? | Tenant vận hành, bảng kê |
| Q5-3 | **Giá trị k** ẩn ô ít hộ (tạm 10; = C-3/Q-MKG-11). Áp cho cả gói MRV và báo cáo? | Sản phẩm dữ liệu |
| Q5-4 | Mô hình **tính phí** (đơn vị / thuê bao / bậc thang), đơn giá, hoàn đơn vị khi sự cố? | Bảng kê có giá |
| Q5-5 | **Hạn mức và ngưỡng bất thường** theo kênh; van **chặn** hay chỉ cảnh báo; ai nhận cảnh báo DLP? | Chế độ `enforce` |
| Q5-6 | MKG có đồng ý mở **dữ liệu hub** cho bên thứ ba? | Sản phẩm `hub_capacity` |
| Q5-7 | **Đơn vị thẩm định** là ai; cần OAuth/mTLS? Có nhận gói bằng chứng (C-1, bút danh, đồng ý, DPA)? | Sản phẩm MRV |
| Q5-8 | Mẫu **hợp đồng dữ liệu / DPA**; kênh giao bí mật khóa API | Onboarding |
| Q5-9 | Thời hạn lưu sổ sử dụng / bảng kê (đề xuất 10 năm) | Lưu trữ |
| Q5-10 | Nhà cung cấp **Valkey** trong lãnh thổ (giới hạn tần suất dùng chung) | Nhiều bản sao api-public |

## D. Vùng nội bộ MKG — G4 (kế toán, thương mại)

| # | Câu hỏi | Chặn |
|---|---|---|
| Q-1 | **Phần mềm kế toán pháp định** (MISA/FAST/Bravo…), chế độ (TT200/TT133), hệ thống tài khoản, định dạng nhận | Đẩy bút toán thật |
| Q-2 | **Khóa phân bổ** chi phí đa chặng (khối lượng, tấn·km, doanh thu…) — tạm "khối lượng" | Giá thành |
| Q-3 | **Công thức giá quyết toán HTX**; thời hạn thanh toán (tạm 15 ngày) | Quyết toán |
| Q-4 | **Tỷ lệ chia doanh thu carbon** (tạm 45 % / 70-15-15) | Carbon |
| Q-5 | Ngưỡng **duyệt giá / chiết khấu** đơn bán (tạm 0 = mọi chênh lệch đều duyệt) | Đơn bán |
| Q-6 | Quy đổi / khấu trừ **ẩm độ** khi cân (= C-4) | Giá thành, nhận hàng |
| Q-7 | **Đơn giá cước** bộ/thủy (tạm 2 200 / 900 đ/t·km), ngưỡng chọn đường thủy (120 km), tải ghe (90 t) | Logistics |

## E. Đo lường, cân, MRV — G3 (MKG / khách)

| # | Câu hỏi | Chặn |
|---|---|---|
| C-1 | **Phương pháp tính tCO2e** (đường cơ sở, rò rỉ, chất khô) và ký phương pháp | Công bố tCO2e |
| C-4 | **Dung sai cân** giữa các điểm cân (tạm 3 %) và độ ẩm | Đối soát RB-6 |
| C-5 | **Đường nhận số cân của khách** (CUSTOMER_IN): tệp, API hay nhập tay? | Điểm cân khách |
| C-6 | **Mẫu cân và giao thức** cụ thể cho agent biên | Bộ điều hợp cân |
| C-7 | Cửa sổ kiểm toán / **tầng lưu lạnh** (R-71) | Lưu trữ |
| C-8 | **Định mức hao hụt sấy** | Hao hụt hub |
| C-9 | Thời hạn lưu **FN-21** | Lưu trữ GIS |

## F. G1–G2 còn treo

| # | Câu hỏi | Chặn |
|---|---|---|
| F-1 | Danh mục trường **MRV** đầy đủ (C-1 G1) | MRV |
| F-2 | **Mẫu báo cáo Bộ** (định dạng gửi Bộ/Cục) | Báo cáo Bộ |
| F-3 | Nguồn **dự báo DCRD** | Dự báo |
| F-4 | Hệ tọa độ **VN-2000** (tham số chuyển đổi theo tỉnh) | GIS |
| F-5 | **Add-on action-link Keycloak** cho kích hoạt nông dân bằng QR | Kích hoạt QR |
| F-6 | Nhà cung cấp **thời tiết / bản đồ nền** trong lãnh thổ (Q2-10) | `/v1/extdata` |
| F-7 | Rà giấy phép **Firebase** (FCM) | Thông báo đẩy |

## G. Hạ tầng (vận hành / MKG)

| # | Câu hỏi | Chặn |
|---|---|---|
| G-1 | **Nhà cung cấp cloud / Kubernetes trong lãnh thổ** (VNPT/Viettel/FPT…), dải IP, endpoint, HSM | Staging, mọi diễn tập |
| G-2 | **Cụm staging** để chạy thật NetworkPolicy, PKI, sao lưu/phục hồi (RPO 24 h, RTO 4 h), diễn tập R-22 trên bản sao dữ liệu thật | N-01…N-05, A-4 |
| G-3 | **Số liệu tải ĐTK 2** (thiết bị, cân, người dùng đồng thời; KN: 500 người dùng) | Diễn tập tải |
| G-4 | Keycloak, SMTP, FCM **thật trong lãnh thổ**; email công vụ gửi báo cáo | Môi trường thật |
| G-5 | Máy Android giá rẻ + 3G để đo thực tế app hiện trường | N-06, UR-KT-01 |
| G-6 | Nhật ký truy cập 1 năm (N-13), tầng lạnh trong lãnh thổ | ATTT cấp 3 |

---

**Ưu tiên trả lời:** ~~A1–A5~~ (đã chốt và đã làm 03/10/2026) → **Q-01** (tệp Excel 14 mẫu) → A8 (CI) → G-1/G-2 (staging) → Q5-1/Q5-2 (bật Public API) → Q-1 (kế toán).
