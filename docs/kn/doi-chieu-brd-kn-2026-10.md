# AgriGreen — Đối chiếu Kiến trúc v4.3, Thiết kế vùng nội bộ MKG v1.4 và các BRD Khuyến nông với repo

> **Ghi chú khi đưa vào repo (W11.01, 10/10/2026):** bản gốc `AgriGreen-doi-chieu-v4.3-MKG-v1.4-BRD-KN.md` (09/10/2026), giữ nguyên, chỉ sửa một chỗ: sổ song phương nằm ở pf 000135 (`pf_coop.bilateral_record`/`bilateral_pair_event`), mkg 000038 chỉ có bảng gửi/trạng thái feed/bản sao. **PO chưa trả lời** các quyết định §2 và câu hỏi §4; trong đợt W11, **"Đề xuất của dev" là mặc định làm việc**. Giả định từng gói ghi ở [gia-dinh-brd-2026-10.md](gia-dinh-brd-2026-10.md).

**Ngày:** 09/10/2026 · **Người nhận:** Product owner · **Repo:** `Agrigreen_demo_v2`, nhánh tích hợp `dev` @ `e548c776` (`main` = `27afd593`). Bốn bản phân tích gốc đọc repo ở `27afd593`; khác biệt tới `e548c776` không đổi kết luận nào dưới đây. Gói v4.1 W0.1–W9.1 đã xong.

**Số cấp tiếp theo hiện tại (dùng cho mọi số "tạm" trong tài liệu):** migration `pf` 000137 · `mkg` 000039 · `shared` 000015 (000013 bỏ trống, không dùng) · `ts` 000005 · `vault` 000004 · ADR repo 0039 (repo đã có tới ADR-0038).

## 0. Đầu vào và cách dùng

### 0.1 Bảy tài liệu nguồn

| # | Tài liệu | Phiên bản / ngày | Đường dẫn | Phân tích ở |
|---|---|---|---|---|
| 1 | Kiến trúc hệ thống AgriGreen | v4.3, 06/10/2026 | `G:\Shared drives\Development\AgriGreen-Kien-truc-he-thong-v4_3.md` | D1 (Phụ lục A) |
| 2 | Thiết kế chi tiết vùng nội bộ MKG | v1.4 | `G:\Shared drives\Development\Thiet-ke-chi-tiet-vung-noi-bo-MKG-v1_4.md` | D2 (Phụ lục B) |
| 3 | BRD KN Module Trồng trọt "Final" | bìa ghi v7.7, 01/10/2026 | `G:\Shared drives\Product team\01. Tai Lieu Khuyen Nong\1.2 App Khuyến Nông\01. BRD\BRD_He_thong_Khuyen_nong_Module_Trong_trot_Final.docx` | D3 (Phụ lục C) |
| 4 | BRD KN Module Hành chính – Tổng hợp | v3.0, 07/10/2026 | `…\01. BRD\BRD_Khuyen_nong_Module_Hanh_chinh_Tong_hop_v3_0.docx` | D3 (Phụ lục C) |
| 5 | BRD KN Module Chăn nuôi – Thủy sản | v3.7, 07/10/2026 | `…\01. BRD\BRD_He_thong_Khuyen_nong_Module_Chan Nuôi_Thuy San.docx` | D4 (Phụ lục D) |
| 6 | BRD KN Module Kế hoạch – Tài chính | v2.2, 07/10/2026 | `…\01. BRD\BRD_He_thong_Khuyen_nong_Module_Ke_hoach_Tai_chinh_v2_2.docx` | D4 (Phụ lục D) |
| 7 | BRD KN Module Báo giá nông sản | v1.3, 07/10/2026 | `…\01. BRD\Copy of BRD_He_thong_Khuyen_nong_Module_Bao_gia_nong_san.docx` | D4 (Phụ lục D) |

`…` = `G:\Shared drives\Product team\01. Tai Lieu Khuyen Nong\1.2 App Khuyến Nông`. Tệp Báo giá trên ổ chung mang tên "Copy of …"; phiên bản v1.3 lấy theo trang bìa.

### 0.2 Cách dùng tài liệu này

1. Đọc **mục 1** (tóm tắt) và **mục 2** (10 quyết định chặn nhiều việc nhất). Mỗi quyết định gom vài câu hỏi ở mục 4.
2. Trả lời ở **mục 4**: ghi vào cột **Trả lời** của từng câu. Câu có ⚠ là gấp (chặn G1 15/12/2026 hoặc chặn nhiều gói). Có thể trả lời ngắn: "đồng ý đề xuất của dev", "phương án (b)", "chờ pháp chế đến dd/mm".
3. Đánh dấu gói ở **mục 3.2**: ghi vào cột **PO** một trong: `OK` (làm theo đề xuất) · `Hoãn` · `Bỏ` · `Đổi đợt → n`.
4. Mục 5 liệt kê mâu thuẫn giữa các tài liệu. Phần lớn đã có câu hỏi tương ứng ở mục 4. Phụ lục A–D là bốn bản phân tích đầy đủ, dùng khi cần bằng chứng (đường dẫn mã, số migration, số mục tài liệu).

Ký hiệu nguồn: `D1 §2.1#4` = bản D1, mục 2.1, dòng 4. `v4.3 §21.1`, `v1.4 §7.2` = mục của tài liệu nguồn. `D3 Q-HC-8`, `D4 Q20` = câu hỏi trong bản phân tích.

---

## 1. Tóm tắt điều hành

**Từng nguồn.**
- **Kiến trúc v4.3 (D1).** Không lật lại dữ liệu và tích hợp của v4.1 (Phần III, VI còn hiệu lực), nên W2.x–W6.x giữ nguyên. v4.3 thêm ba lớp: (1) căn cứ pháp lý mới (Luật ANM 116/2025, NĐ 331/2026, TCVN 14423:2026, Luật BVDLCN 91/2025 + NĐ 356/2025), đề xuất nền tảng **cấp độ 3 ngay**, đối chiếu TCVN **0/19 mục đạt trọn**; (2) **11 thay đổi nền tảng P-1…P-11** (MFA cho mọi người dùng không phải nông hộ, mật khẩu 14 ký tự, vô hiệu sau 45 ngày, khóa phiên 5/15 phút, pgaudit, EDR, DLP, kênh báo lỗ hổng…); (3) bốn ADR: stack Go cho hệ nội bộ (chấp nhận, repo đã đạt), hai hệ thống thông tin, MFA theo nhóm, chữ ký số trên lô dữ liệu thương mại.
- **MKG v1.4 (D2).** Khung `internal/mkg/*` đã đúng hướng, nhưng còn thiếu phần lớn engine: T1/T2/T3, điểm gom, ghép ghe, danh mục quan sát, ảnh chụp đầu vào, bộ ước tính tồn VFT. Có **xung đột với phần đã xây**: lược đồ quan sát W8.2 (`kind` ≠ `key`, thiếu `epistemic kind`, `max_age` lấy sai nguồn), chỉ có ba điểm cân, CHECK cấm định mức hao hụt cho lô DIRECT, GPS người đếm cuộn lưu trong sổ không xóa được, `member_refs` dùng subject tài khoản thay cho `worker_ref`.
- **BRD Trồng trọt "Final" (D3).** Thực chất là **v7.7, không có yêu cầu mới**. Còn bốn lỗi văn bản. Mã còn mấy chỗ thiếu: màn "Báo cáo đột xuất" chưa có trang, vẽ thửa trên Web, nạp Excel tài khoản; UAT đã cũ sau W3.3.
- **BRD Hành chính – Tổng hợp v3.0 (D3).** Đây là **module HRM mới hoàn toàn**, 21 FN, PII dày (dân tộc, tôn giáo, Đảng). Cần `pf/hrm`, năng lực Contact, liên kết khách có OTP, xuất Word. Ước 14–20 tuần-người.
- **BRD Chăn nuôi – Thủy sản v3.7, Kế hoạch – Tài chính v2.2, Báo giá v1.3 (D4).** CNTS và KHTC là **cấu hình phòng ban của `dom/extension`**, cộng các mở rộng năng lực: đơn vị nộp không phải xã, kỳ ngày, cảnh báo, nhật ký có GPS. Báo giá là **module năng lực mới `pf/price` (`cap/price`)**, đã định nghĩa sẵn ở v4.1 §26. KHTC buộc tách `cap/export` → `pf/export` để soạn và xuất .docx. Ba BRD này **mâu thuẫn với nhau** ở tuần báo cáo, đường CNTS→KHTC, cách sửa số kỳ cũ và mã FN.

**Xung đột lớn nhất với phần đã xây.**
1. **Cấu hình đăng nhập KN đã chốt với khách** (phiên 4 giờ/24 giờ, mật khẩu 12 ký tự/không trùng 5, MFA chỉ cho vai `[MFA]`, khóa bằng `realm_test.go`) trái P-1/P-3/P-5 của v4.3 (D1 §2.1#4–8).
2. **Chủ dữ liệu HTX:** W3.1 giả định H-1 (a), còn v4.3 §21.1 chọn (b) "HTX là bên kiểm soát". Phải đổi trước khi có dữ liệu thật; sau đó chỉ sửa được bằng bản ghi bù (D1 §2.1#19).
3. **Engine W8.2 nhận dữ liệu mức thửa** (`plot_directory`, `cooperative_directory`), trái v4.3 §21.1 "chỉ tổng hợp theo xã qua k" nếu HTX không cấp phép cho mục đích này (D1 §2.1#20).
4. **Hai hệ thống thông tin chưa tách thật:** van RabbitMQ `hub_valve`, `mrv` còn chạy; còn dùng chung realm, cụm, CI (D1 §2.1#3).
5. **Lô song phương W5.6 chưa có chữ ký số** theo ADR-24. Không thêm được cột vào sổ đã có chuỗi băm, nên phải làm sổ phụ (D1 §2.1#17, D2 §1.7).
6. **Lược đồ MKG:** quan sát W8.2, ba điểm cân, đối soát lô DIRECT, GPS trong sổ không xóa được (D2 §2 C-1…C-9).
7. **RV-03:** nếu G1 có dữ liệu thật mà bị coi là "đưa vào sử dụng" theo NĐ 331, hồ sơ cấp độ phải xong trước 15/12/2026. Khi đó mã xong cũng chưa được nhận dữ liệu thật (D1 §2.1#25).

**Khuyến nghị và thứ tự.**
1. **Tuần 13–17/10:** họp PO + pháp chế chốt 10 quyết định ở mục 2, nhất là QĐ-1 (G1 có phải vận hành chính thức), QĐ-2 (chủ dữ liệu HTX) và QĐ-5 (phạm vi KN ở G1). Song song làm W10.00 và W11.01 (đưa tài liệu vào repo, đánh số ADR 0039–0042, sửa UAT). Hai gói này không cần khách.
2. **Đợt 1 tới 15/12:** các gói tuân thủ làm được ngay (W10.08 pgaudit, W10.09 DLP, W10.10 security.txt). Đổi chủ dữ liệu HTX nếu chọn (b) (W10.13). Đóng khoảng trống Trồng trọt (W11.02). Báo giá BG1–BG6 nếu QĐ-5 đưa vào G1. Nền kỳ chung (W11.03, W11.05). Phía dữ liệu engine MKG (W12.01–W12.06). Riêng MFA/phiên/mật khẩu viết mã sau cờ, bật khi khách KN đồng ý.
3. **Đợt 2–3 (12/2026–05/2027):** HCTH, CNTS GĐ1, KHTC, xuất Word (`pf/export`), SMS/OTP, phần lược đồ MKG (W12.07–W12.19), chữ ký số ở G2.
4. **Không kịp trước 15/12:** HCTH, CNTS, KHTC, chữ ký số, hạ tầng cấp độ 3. Riêng Đợt 1 đã khoảng 33 tuần-người, 42 nếu tính cả MKG (mục 3.4). Nếu vẫn làm một gói một agent tuần tự thì không kịp. Phải chạy song song ít nhất 4 luồng hoặc cắt Báo giá khỏi G1.

---

## 2. Quyết định chặn nhiều việc nhất

| QĐ | Câu hỏi | Vì sao quan trọng | Phương án | Đề xuất của dev | Mở khóa | Hạn |
|---|---|---|---|---|---|---|
| **QĐ-1** | G1 15/12/2026 có dữ liệu thật thì có bị coi là "đưa vào sử dụng" theo NĐ 331/2026 không? Ai là đơn vị chuyên trách ANM, nhân sự bảo vệ DLCN, có DPIA chưa? (Q-001, Q-003, Q-005) | Nếu có, 4 bước hồ sơ cấp độ phải xong trước G1. Khi đó mã xong cũng không được nạp dữ liệu thật (v4.3 §20, RV-03) | (a) G1 chạy dữ liệu tổng hợp hoặc phạm vi người dùng hạn chế, có thỏa thuận; (b) lùi mốc dữ liệu thật; (c) hồ sơ cấp độ xong trước 15/12 | Chọn (a) cho G1, đồng thời khởi động hồ sơ (c) cho mốc kế tiếp | W3.5 (hợp nhất dữ liệu UAT thật), H-17, W10.16, mọi nghiệm thu bằng dữ liệu thật | **17/10/2026** (v4.3 #1 "tuần này") |
| **QĐ-2** | **ĐÃ QUYẾT 13/10/2026 (CEO D2, [ADR-0052](../adr/0052-dong-so-huu-mkg-htx-neo-mst.md)): MKG + HTX đồng sở hữu; KN là bên thứ ba.** HTX là bên kiểm soát dữ liệu hộ/xã viên/thửa do HTX nhập (H-1 b)? Cơ quan KN là bên kiểm soát dữ liệu khuyến nông? (Q-023, Q-004) | W3.1 đang làm theo (a). Đổi chủ sau khi có dữ liệu thật phải làm bằng bản ghi bù (D1 §2.2 W3.x) | (a) chương trình KN là chủ (như hiện tại); (b) HTX là chủ, MKG là bên xử lý | Theo v4.3: chọn (b). Làm W10.13 trước 15/12 | W10.13, W11.47 (hộ mô hình CNTS), hợp đồng xử lý dữ liệu, `basis_ref` của `share_grant` | **31/10/2026** (để kịp làm và kiểm thử trước 15/12) |
| **QĐ-3** | Khai hai hệ thống thông tin (ADR-22)? Hạ tầng dùng chung nào được khai là "kết nối", hạ tầng nào phải tách? Có cần realm quản trị riêng? (Q-002) | Quyết định phạm vi hồ sơ cấp độ của hai hệ, việc gỡ van RabbitMQ và cách đặt chính sách mật khẩu (Keycloak đặt theo realm) | (a) hai hệ, tách RabbitMQ, realm quản trị riêng từng hệ; (b) hai hệ, giữ cụm K8s/CI chung và khai là kết nối; (c) một hệ cấp độ 3 | (b), cộng realm quản trị riêng | W10.01, W10.02, W10.05 (realm), W10.11 | 24/10/2026 (v4.3 #2) |
| **QĐ-4** | **ĐÃ QUYẾT 13/10/2026 (CEO D24, [ADR-0041](../adr/0041-mfa-theo-nhom-d24.md)): phương án (c) — TOTP ngay từ G1, phiên giữ 4 giờ/24 giờ kèm đánh giá rủi ro, 14 ký tự chỉ ở realm quản trị riêng (D25), vô hiệu 45 ngày báo trước 7 ngày.** Chính sách đăng nhập cho cán bộ KN: giữ phiên 4 giờ/24 giờ (kèm đánh giá rủi ro) hay chuyển 15/5 phút? MFA cho mọi cán bộ từ G1 hay theo lộ trình? Mật khẩu 14 ký tự? Vô hiệu sau 45 ngày? (Q-016…Q-019) | Trái cấu hình D9 đã chốt với TTKN An Giang và khóa bằng kiểm thử. Cần ai đó trao đổi lại với khách | (a) theo đủ v4.3 từ G1; (b) giữ D9 kèm đánh giá rủi ro, MFA theo lộ trình trong hồ sơ; (c) kết hợp: MFA TOTP ngay, phiên giữ 4 giờ | (c). Mọi giá trị là tham số có phiên bản | W10.03, W10.05, W10.04 | 15/11/2026 (đóng băng cấu hình G1) |
| **QĐ-5** | KN đưa module nào vào G1 15/12? v4.1 Phụ lục A nói G1 cần `cap/import`, `cap/forms`, `cap/price` tối thiểu. Ai giữ danh mục mặt hàng giá? (Q-044, Q-082, Q-063) | Báo giá BG1–BG6 (~10 tuần-người) chỉ kịp nếu bắt đầu ngay. Q-01 (14 mẫu thật) vẫn chặn Trồng trọt | (a) G1 = Trồng trọt + Báo giá vận hành ngày; (b) G1 = Trồng trọt, Báo giá sang Đợt 2 | (a) nếu chạy được ≥ 4 luồng song song; nếu không thì (b) | W11.71–W11.76, ADR-0049 (`cap/price`) | **17/10/2026** |
| **QĐ-6** | Một lịch kỳ chung cho mọi module (tuần Thứ Tư – Thứ Ba theo KHTC, hay Thứ Ba – Thứ Hai theo CNTS?). Sửa số kỳ cũ hay ghi điều chỉnh ở kỳ sau? Thế nào là "nguồn đã chốt"? (Q-045…Q-048) | Ba BRD ngày 07/10 nói khác nhau, repo lại tính tuần từ Thứ Hai. Làm X1/X4 trước khi chốt thì CN2, KH2 phải làm lại | Mỗi câu có 2–3 phương án (mục 4.5) | Tuần Thứ Tư – Thứ Ba (KHTC ghi "đã chốt"), KSGM 4 tuần/tháng giữ song song; nguồn sửa được trước chốt, KHTC đọc lũy kế của nguồn | W11.03, W11.05, W11.42, W11.62, W11.10 | 31/10/2026 |
| **QĐ-7** | Danh mục phòng ban (tên, mã, HCTH), danh mục cấp quản lý chung, danh mục đơn vị nội bộ (phòng/trạm/trại), lãnh đạo phòng làm việc HCTH trong phiên nào (Q-051, Q-059, Q-067, Q-068) | Seed phòng ban và `KNContext` là nền cho cả HCTH, CNTS, KHTC, Báo giá. Repo còn dùng `phong_ban_5/6` tạm | — | Theo đề xuất ở từng câu | W11.06, W11.07, toàn bộ HCTH | 31/10/2026 |
| **QĐ-8** | Engine MKG có được nhận `plot_directory`/`cooperative_directory` không? Tạm dừng thu GPS người đếm cuộn ở production tới khi có căn cứ và thời hạn lưu? (Q-024, Q-038) | W8.2 đã ánh xạ dữ liệu mức thửa vào engine. `roll_count_location` lưu vị trí người trong sổ không xóa được | (a) chỉ nhận tổng hợp, gỡ hai bộ ánh xạ; (b) nhận khi cấp phép của HTX có mục đích "điều phối" | (b) kèm kiểm thử; tạm dừng GPS bằng cờ tính năng ngay | W12.02, W12.19, W12.14 | 15/11/2026 (trước khi MKG có dữ liệu thật) |
| **QĐ-9** | Chọn nhà cung cấp trong nước cho SMS/OTP và cho chứng thư số/HSM; pháp chế chấp nhận "nền tảng ký thay có ủy quyền" theo hình thức nào? (Q-020, Q-022) | Hai năng lực dùng chung cho nông hộ (P-2, CNTS GĐ2), HCTH (liên kết + OTP), song phương HTX↔MKG và VFT | — | Đợt 1 dùng OTP qua email cho HCTH; SMS khi có hợp đồng | W10.06, W10.07, W10.18, W11.55, W12.08 | 30/11/2026 (SMS); chữ ký số theo mốc G2 |
| **QĐ-10** | Đánh số ADR 0039–0042 cho ADR-21…24 của v4.3, cấp migration từ số trống tiếp theo, và gửi bản SAD/BRD định danh v1.2, MKG v1.4 dạng md, `docs/db/` (Q-010, Q-011, Q-012) | Việc rẻ nhưng chặn W10.00 và mọi gói viết ADR. Bộ chạy `golang-migrate` bỏ qua im lặng tệp có số nhỏ hơn số đã áp | — | Đồng ý; kiến trúc sư dùng tiền tố "KT-ADR-" ở bản sau | W10.00, W7.2–W7.4, W10.03, W10.12, W12.* | 17/10/2026 |

---

## 3. Kế hoạch gói việc hợp nhất

### 3.1 Quy ước

- **Đánh số:** `W10.xx` nền tảng và tuân thủ (v4.3, năng lực dùng chung), `W11.xx` Khuyến nông, `W12.xx` MKG. Trong W11: `01–11` dùng chung và Trồng trọt · `21–31` Hành chính – Tổng hợp (H1→W11.21 … H11→W11.31) · `41–55` Chăn nuôi – Thủy sản (CNk→W11.(40+k)) · `61–69` Kế hoạch – Tài chính (KHk→W11.(60+k)) · `71–81` Báo giá (BGk→W11.(70+k)). Số bỏ trống: W11.51 (CN11) và W11.80 (BG10), vì đã gộp vào W11.10/W11.11.
- **Cỡ:** S ≤ 3 ngày · M 1–2 tuần · L 2–4 tuần · XL > 4 tuần (một kỹ sư/agent, gồm kiểm thử). Quy đổi để cộng: S = 0,5 · M = 1,5 · L = 3 · XL = 5 tuần-người.
- **Migration (tạm):** cấp theo thứ tự đợt ở 3.3. Người tích hợp cấp lại theo thứ tự gộp thật. Không dùng số nhỏ hơn số đã áp: `golang-migrate` bỏ qua im lặng (D2 §3). Bỏ giữ chỗ cũ mkg 000027–000032 của plan W8.4/W8.5. `shared` 000013 để trống. Sổ đã có dữ liệu thì không `ADD COLUMN`, dùng sổ phụ (bài học W4.1). Nếu buộc phải thêm cột thì làm cột NULL theo ADR-0012.
- **ADR (tạm):** 0039 = v4.3 ADR-21 (stack Go) · 0040 = ADR-22 (hai hệ) · 0041 = ADR-23 (MFA theo nhóm, P-1…P-5) · 0042 = ADR-24 (chữ ký số) · 0043 vai trò kiểm soát/xử lý · 0044 căn cứ ANM, cấp độ · 0045 phân loại dữ liệu 4 mức · 0046 phụ thuộc OR-Tools · 0047 dữ liệu vị trí người lái/người đếm · 0048 liên kết khách có OTP · 0049 `cap/price` · 0050 `dom/hrm` (`pf/hrm`, W11.21 — đã viết) · **0051** tách `cap/export` → `pf/export` + .docx (trước giữ 0050; số đó đã dùng cho `pf/hrm`, nên lấy số trống kế tiếp). Sổ số đã giữ chỗ: [ADR README](../adr/README.md#số-đã-giữ-chỗ); `go run ./tools/docscheck` kiểm số ADR không trùng.
- **Trạng thái:** `Làm ngay` (không chờ ai) · `Mã được, bật chờ …` (viết mã sau cờ, bật production khi có câu trả lời) · `Chặn Q-xxx`.

**Đã khử trùng lặp** (một gói thay cho nhiều đề xuất):

| Chủ đề | Đề xuất gốc | Gói hợp nhất |
|---|---|---|
| Đưa v4.3 / v1.4 vào `docs/`, bảng ánh xạ ADR, sửa tài liệu sai | D1 W10.0 · D2 D-9, C-5, C-6 · D3 T0 (phần KN) | W10.00 (kiến trúc), W11.01 (KN) |
| Đánh số ADR-21…24 | D1 §0.2, Q18 · D2 D-9, Q17 | W10.00 + Q-010 |
| Đánh số migration | D2 §3 "rủi ro số", Q8 · D3 §3 · D4 §3 | Quy ước 3.1 + Q-011 |
| Chữ ký số trên lô | D1 W10.7 · D2 W8.6 | W10.07 |
| Cổng nhận VFT ở vùng nội bộ | D1 W10.8 · D2 W8.3b(3) | W12.08 |
| OTP SMS / kênh SMS | D1 W10.6 · D3 Q-HC-8 · D4 CN15, Q20 | W10.06 |
| Liên kết khách có OTP | D3 §2.5, H6 | W10.18 (năng lực) + W11.26 (dùng) |
| Ghi .docx, tách `cap/export` | D3 H8 (renderer .docx) · D4 X7 | W10.17 |
| Mở phòng ban, menu kn.menu/v3 | D3 H0 (seed, menu) · D4 X2 | W11.06 |
| Cấp quản lý, phạm vi đơn vị, nhóm Cấp Tỉnh | D3 H0 (phạm vi) · D4 X3 | W11.07 |
| `cap/forms` tổng quát đơn vị lá | D4 X4 · D3 §2.5 (đơn vị = người, nhiều bước) | W11.05 |
| "Báo cáo cho KHTC" trong menu mọi phòng | D4 X5, CN11, BG10 (menu) | W11.11 |
| Nguồn module `_pub` | D4 X6, BG10 (dữ liệu) | W11.10 |
| Sửa W8.2 theo v4.3 §21.1 | D1 W10.15 | W12.02 |
| `cap/workflow` ở cụm mkg | D1 W10.17 · v4.3 §9 | W12.04 |

### 3.2 Danh sách gói

#### W10 — Nền tảng và tuân thủ

| Mã | Tên | Nội dung | Nguồn | Phụ thuộc | Cỡ | Migration (tạm) | Trạng thái | Đợt | PO |
|---|---|---|---|---|---|---|---|---|---|
| W10.00 | Tài liệu kiến trúc + ADR | Đưa v4.3, v1.4 (md, giữ nguyên byte) vào `docs/kien-truc/` kèm README phạm vi thay thế. Bảng ánh xạ ADR-21…24 ↔ 0039…0042, viết ADR-0039 (ghi lại). Mục I của `cau-hoi-ton-dong.md`, Q-1 thêm TT 99/2025, sửa C-5/C-6, ADR-0036 §3, H-8 | D1 W10.0 · D2 D-9, C-5, C-6 | Q-010, Q-012 (bản md) | S | — | Làm ngay | 0 | |
| W10.01 | ADR-0040 + kiểm kê tách hai hệ | Liệt kê mọi điểm dùng chung (RabbitMQ, namespace, Argo CD, realm, CI, kho đối tượng, Valkey), quyết "kết nối" hay "tách". Thêm khẳng định vào `check_flows.py` | D1 W10.1 | W10.00 | M | — | Chặn Q-002 | 1 | |
| W10.02 | Gỡ van RabbitMQ còn lại | `hub_valve`, `mrv` chuyển sang hợp đồng nạp của cổng (mở rộng W5.10) | D1 W10.2 | W5.10, W10.01 | M | — | Mã được, bật chờ hợp đồng `hub_facility`/MRV; Chặn Q-002 | 2 | |
| W10.03 | ADR-0041 MFA theo nhóm | Luồng xác thực có điều kiện (mật khẩu + TOTP/passkey). `RequireMFA` mở rộng sang Application API theo loại tài khoản, claim `amr`/`acr`, `realm_test.go` mới. Xác nhận đẩy chỉ khi kiểm chứng được SPI | D1 W10.3 | W10.00, SAD v1.2 | L | pf 20261013134254 | **Phần D24 xong (gói MFA, 13/10/2026):** TOTP bắt buộc theo vai cho cán bộ KN + vai [MFA] (realm `login_totp_required`, luồng `agrigreen browser`), đặt lại TOTP có lý do, trạng thái MFA ở quản trị tài khoản — [ADR-0041](../adr/0041-mfa-theo-nhom-d24.md). Còn: `RequireMFA` Application API theo loại tài khoản, `acr`, passkey, issuer realm quản trị | 1 | D24 |
| W10.04 | Thiết bị đăng ký + PIN = 2 yếu tố | Khóa ký trong Android Keystore/StrongBox, chứng thực khi đăng ký, `amr`={hwk,pin}, app khóa sau 5 phút | D1 W10.4 | W10.03 | M | — | Mã được, bật chờ G-5, Q-016 | 2 | |
| W10.05 | Mật khẩu, phiên, vô hiệu 45 ngày | Chính sách 14 ký tự, không trùng 10, quản trị đổi 60 ngày. Job `accounts_dormant` (lý do `dormant_45d`), báo trước, nông hộ tự kích hoạt lại. Thời gian phiên là tham số có phiên bản kèm đánh giá rủi ro | D1 W10.5 | W10.03, W10.01 (realm) | M | pf 20261013134254, 20261013142347 | **Xong theo D24/D25 (gói MFA, 13/10/2026):** realm quản trị riêng `agrigreen-kn-admin` (14 ký tự + độ phức tạp, không trùng 10, đổi 60 ngày); phiên giữ 4 giờ/24 giờ kèm [đánh giá rủi ro](../security/danh-gia-rui-ro-phien-4h-24h.md); job `accounts_dormant` (báo trước ngày 38, khóa ngày 45, `dormant_45d`, tham số có phiên bản); [runbook](../runbooks/keycloak-mfa-d24.md). Còn: nông hộ tự kích hoạt lại bằng OTP (chờ W10.06) | 1 | D24 |
| W10.06 | Kênh SMS + OTP | Kênh SMS cho `pf/notify` qua nhà cung cấp trong nước (ADR-0007/0015), OTP cho nông hộ, CTV, kích hoạt lại; trần chi phí. Đánh giá rủi ro nhóm nông hộ | D1 W10.6 · D3 Q-HC-8 · D4 CN15, Q20 | W10.05 | M | có thể 1 số pf (cấp lúc gộp) | Chặn Q-020 | 2 | |
| W10.07 | ADR-0042 chữ ký số trên lô | Sổ phụ `bilateral_signature` (không thêm cột vào `bilateral_record`). `ingest_batch.v1` thêm chữ ký tách rời (CAdES/PKCS#7), kiểm CA/OCSP. Chế độ ký thay có ủy quyền; `mkg/fieldops` ký lô gửi | D1 W10.7 · D2 W8.6 · v4.3 ADR-24, v1.4 §10.2 | W5.6, W10.00 | L | pf 000159 + mkg 000051 | Chặn Q-022 | 3 (G2) | |
| W10.08 | Tường lửa CSDL tương đương (P-6) | `pgaudit` cho vai đặc quyền, đẩy nhật ký vào giám sát, luật cảnh báo truy vấn bất thường; khẳng định NetworkPolicy cho `vault`/`idp` | D1 W10.9 | W2.6 | M | — (cấu hình cụm) | Làm ngay (mã); hạ tầng chờ G-1 | 1 | |
| W10.09 | Phương án DLP chính thức (P-8) | Văn bản liệt kê mọi đường xuất đều ghi `egress_event`. Vá chỗ thiếu (`r_agg` W6.4, feed W5.3) + kiểm thử phủ | D1 W10.10 | W10.00 | M | — | Làm ngay | 1 | |
| W10.10 | Chu kỳ 6 tháng, kênh báo lỗ hổng, giới hạn kết nối | `/.well-known/security.txt` + quy trình tiếp nhận; lịch rà soát có biên bản; giới hạn kết nối đồng thời theo nguồn (`httpx`) | D1 W10.11 | W10.00 | S | — | Làm ngay | 1 | |
| W10.11 | Hạ tầng cấp độ 3 | EDR, quản lý bản vá, nhật ký luồng mạng, bản sao lưu thứ ba trên phương tiện khác loại, vùng văn phòng + Wi-Fi vào ADR-0008. Viết ADR-0044 (căn cứ ANM) | D1 W10.12 · v4.3 §11, RV-19, RV-21 | W10.01 | L | — | Chặn G-1, Q-008 | 2–3 | |
| W10.12 | Phân loại dữ liệu 4 mức (ADR-0045) | Trường `data_class` trong `module.yaml`, `modgen -check` buộc có nhãn; danh mục `personal_sensitive` theo NĐ 356 | D1 W10.13 | W10.00 | M | — | Mã được; danh mục chờ Q-005 | 2 | |
| W10.13 | Vai trò kiểm soát/xử lý (ADR-0043) | Nếu chọn H-1 (b): đổi chủ dữ liệu HTX/hộ/thửa theo `docs/design/cooperative.md` §11. `basis_ref` của `share_grant` trỏ tới hợp đồng xử lý; quyền chủ thể chuyển tới HTX | D1 W10.14 | W3.3, W4.1 | M–L | pf 000138 | Chặn Q-023 — **phải xong trước 15/12** | 1 | |
| W10.14 | Thông báo vi phạm (RV-18, K-10) | Runbook P1 hai nghĩa vụ, thời hạn, đầu mối, mẫu thông báo; diễn tập có biên bản | D1 W10.16 | W10.00 | S | — | Chặn Q-007 | 2 | |
| W10.15 | Rà dữ liệu ra nước ngoài (RV-15) | Kiểm kê MRV, phòng dữ liệu VFT, GitHub CI, FCM, Google Sheets, theo dõi lỗi; cổng CI cấm đích ngoài danh mục RB-7 | D1 W10.18 | W10.09 | S | — | Mã được, bật chờ Q-006, Q-026 | 2 | |
| W10.16 | Gói bằng chứng hồ sơ cấp độ | Sinh tự động từ repo: danh mục tài sản (SBOM + `module.yaml`), ma trận luồng, cổng CI W9.1, kết quả diễn tập | D1 W10.19 | W10.00, CI chạy lại | M | — | Chặn Q-013; mẫu hồ sơ NĐ 331 | 2 | |
| W10.17 | `pf/export` + ghi .docx (ADR-0051) | Tách `cap/export` (điều kiện W6.6 (i)). Mẫu văn bản có phiên bản, biến, khuôn câu, lặp, lần xuất có băm; bộ ghi .docx dùng chung cho KHTC Mẫu 1–5, CNTS BM-11, MS05/06 HCTH, Mẫu KNQG | D4 X7 · D3 H8 (.docx), Q-HC-11 · D4 Q26 | W10.00 | XL | pf 20261024090957 | **Đã làm 20/10/2026** (nhánh `w1017`): module + Word KHTC Mẫu 1–5; bật chờ Q-062; BM-11, MS05/06, KNQG nối port ở gói của chúng | 2 | |
| W10.18 | Liên kết khách có OTP (ADR-0048) | Token một chủ thể, hạn = khóa bước, OTP 6 số, giới hạn thử, nhật ký mỗi lần mở (theo mẫu `dispatch_link`). Email trước, SMS sau | D3 §2.5, H6 | W10.06 (SMS), W10.03 | M | có thể 1 số pf (cấp lúc gộp) | Mã được, bật chờ Q-021, Q-005 | 2 | |
| W10.19 | Hạ tầng nghiệm thu KN | Kiểm tải 500 người dùng trên staging, FCM thật < 1 phút, Android 3G, nhật ký truy cập ≥ 1 năm, diễn tập RPO/RTO | D3 T5 | cụm staging (G-2) | M | — | Chặn G-2, Q-009 | 1–2 | |

**Bổ sung gói cũ** (D1 §3): W5.2 — mTLS bắt buộc cho đối tác máy khi ADR-0041 được ký · W5.10 — mở rộng bằng W10.02 · W7.2–W7.3 — đổi phụ thuộc từ SAD v1.1 sang **v1.2** · W3.5 — chỉ nạp dữ liệu thật khi có QĐ-1.

#### W11 — Khuyến nông

**Dùng chung và Trồng trọt**

| Mã | Tên | Nội dung | Nguồn | Phụ thuộc | Cỡ | Migration (tạm) | Trạng thái | Đợt | PO |
|---|---|---|---|---|---|---|---|---|---|
| W11.01 | Tài liệu KN | Cập nhật UAT/gap sau W3.3/W6.2 (đường dẫn `pf/cooperative`, menu mobile), thêm dòng "Báo cáo đột xuất"; gửi BA lỗi văn bản; tiền tố module cho mã FN/UR trong ma trận nghiệm thu | D3 T0, C8 · D4 Q5 | — | S | — | Làm ngay | 0 | |
| W11.02 | Đóng khoảng trống Trồng trọt | Màn "Báo cáo đột xuất", vẽ thửa trên Web, nạp Tỉnh giao từ Excel, di chuyển Nút xã, nạp Excel tài khoản nhiều vai, tab Phiên/Lịch sử đăng nhập, đếm theo phòng ban, X-12, X-02 | D3 T1 | — | M–L | — | Làm ngay; vẽ thửa Web chờ Q-064 | 1 | |
| W11.03 | Lịch kỳ hệ thống dùng chung | Quy tắc chia kỳ v2 (tuần theo QĐ-6, kỳ ngày, KSGM 4 tuần/tháng có sửa ranh giới, tháng/quý/năm) là tham số có phiên bản + hàm thuần trong `period.go`; thay Q-17 tạm | D4 X1 | — | M | — (tham số) | Chặn Q-045 | 1 | |
| W11.04 | Cắt KN sang `cap/forms` native | Kỳ KN native trên `/v1/forms`; engine lưới thành `forms.field_types`; web-ops/field-app đổi client; Chi cục đọc `close_snapshot`; `/v1/extension/report-*` chỉ đọc một đợt | D3 T2 | W6.2 | XL | pf 000156 | Mã được; thứ tự chờ Q-065 | 2 | |
| W11.05 | `cap/forms` tổng quát đơn vị lá | `unit_kind` (xã, cơ sở GM, hộ/điểm, phòng ban); Profile `kn_cnts`, `kn_khtc`; trạng thái Không nộp, Đã chốt – chưa điều chỉnh, Chờ mở kỳ; hủy kỳ; giới hạn lần gửi. Thiết kế chừa chỗ cho đơn vị = người, nhiều bước (HCTH) | D4 X4 · D3 §2.5 | W11.03 | L | pf 000139 | Chặn Q-045, Q-047 | 1 | |
| W11.06 | Mở phòng ban + menu kn.menu/v3 | Seed `chan_nuoi_thuy_san`, `ke_hoach_tai_chinh`, `thong_tin_*`, `hanh_chinh_tong_hop`; khuôn menu theo phòng ban + nhóm "Dùng chung" | D4 X2 · D3 H0 (seed, menu), C6 | — | S | — (seed) | Chặn Q-051 | 1 | |
| W11.07 | Phân quyền trong vai + đơn vị nội bộ | `mgmt_level` mở rộng (lãnh đạo đơn vị, phụ trách); 5 nhóm Cấp Tỉnh, cờ QTNV báo giá, người phụ trách biểu, ủy quyền có hạn; phạm vi Xã theo cơ sở/hộ; `KNContext.UnitRef` + "cán bộ xã được phân công" (HS-37); danh mục đơn vị nội bộ | D4 X3 · D3 H0, C3–C5 | W11.06 | M | pf 000137 | Chặn Q-059, Q-067, Q-068 | 1 | |
| W11.08 | 14 mẫu thật | Nạp tệp gốc bằng "Nạp từ Excel"; ca vàng công thức (N-14) | D3 T3 | Q-01 | M | — | Chặn Q-063 | 1 (khi có tệp) | |
| W11.09 | Google Sheet cho Chi cục | ADR-0026 → Chấp nhận; bộ chuyển `reports/gsheet` tắt mặc định | D3 T4 | — | M | pf 000157 (nếu chọn) | Chặn Q-026 | 2 | |
| W11.10 | Hợp đồng nguồn module `_pub` | View "số liệu đã chốt theo tuần báo cáo" cho Trồng trọt, CNTS, Giá (bảng giá đã duyệt + số phiếu theo tuần); cờ chốt của kỳ nguồn | D4 X6, BG10 (dữ liệu) | W11.03, W11.05 | M | pf 000146 | Chặn Q-048 | 2 | |
| W11.11 | "Báo cáo cho KHTC" dùng chung | FN-PN-03.01–03 trong `dom/extension`, hiện trong menu mọi phòng (TT, CNTS, TTTT, HCTH) | D4 X5, CN11, BG10 (menu) | W11.05, W11.61 | M | pf 000172 | Chặn Q-046 | 3 | |

**Hành chính – Tổng hợp** (H0 đã gộp vào W11.06/W11.07)

| Mã | Tên | Nội dung | Nguồn | Phụ thuộc | Cỡ | Migration (tạm) | Trạng thái | Đợt | PO |
|---|---|---|---|---|---|---|---|---|---|
| W11.21 | Danh mục nhân sự (FN-19) | Chức vụ, ngạch/CDNN, bảng hệ số có hiệu lực (sổ), trình độ, chuyên ngành; quy tắc nâng bậc, quy đổi thành tích là tham số có phiên bản | D3 H1 | W11.07 | M | pf 000147 | Mã được; dữ liệu chờ Q-070 | 2 | |
| W11.22 | `pf/hrm` + hồ sơ (FN-07, 08, 20) | Schema `pf_hrm`, PII qua vault, thẻ TK nhạy cảm có khóa/mục đích riêng, quá trình có hiệu lực, bậc = phép chiếu sự kiện, truy vấn as-of, nhật ký xem/xuất, ma trận quyền hồ sơ | D3 H2 | W11.07, W11.21 | L | pf 000148 + vault 000004 (nếu cần mục đích mới) | Mã được, bật chờ Q-005, Q-069 | 2 | |
| W11.23 | Contact (FN-13) | Năng lực dùng chung `pf/contact` (`cap/directory`): một bản ghi/người, lọc trường ở API theo vai, liên hệ ngoài tỉnh, thay `pf_extension.staff` | D3 H3 | W11.22 | M | pf 000149 | Chặn Q-071, Q-072 | 2 | |
| W11.24 | Kho tài liệu nhân sự (FN-14) | Cây thư mục, phân loại, quyền thư mục; tệp qua `pf/evidence` (`hr_records`); xóa = ẩn | D3 H4 | W11.22 | M | pf 000150 | Mã được | 2 | |
| W11.25 | Nâng bậc (FN-09, 10) | Job tính mốc NLTX/TNVK/NLTH, điều kiện, trạng thái xử lý, xuất Mẫu 1/2/4, sổ quyết định (hủy = sự kiện bù) | D3 H5 | W11.21, W11.22, W11.24 | L | pf 000152 | Chặn Q-069, Q-070, Q-075 | 3 | |
| W11.26 | Đánh giá (FN-15–18, 21) | MS05/06 có phiên bản, phiếu, đợt (3 luồng, hạn từng bước), cấp quản lý, chốt = ảnh chụp, xuất Word + Excel, liên kết + OTP, nhắc | D3 H6 | W11.22, W11.24, W10.17, W10.18, W11.04 | XL | pf 000153 | Mã được, bật chờ Q-021, Q-062, Q-074 | 3 | |
| W11.27 | Kế hoạch nhân lực (FN-11) | Profile `hrm_plan` của `cap/forms` (lá = đơn vị nội bộ) | D3 H7 | W11.05, W11.22 | M | pf 000154 | Mã được | 3 | |
| W11.28 | Báo cáo Sở NS01–06 + Dashboard | As-of tại ngày chốt, quy tắc kiểm chéo, nhấn ô xem người, lưu tệp + lịch sử xuất, nhật ký trường nhạy cảm; dashboard < 5 s | D3 H8 (trừ .docx → W10.17) | W11.21, W11.22 | L | pf 000151 | Mã được | 2 | |
| W11.29 | web-ops HCTH | ~30 màn + Contact cho mọi vai; giả lập `src/api/mock/hrm*`; Playwright `hc-*.spec.ts` | D3 H9 | OpenAPI của W11.22–W11.28 | L | — | Mã được (theo hợp đồng) | 2–3 | |
| W11.30 | Chuyển đổi dữ liệu HCTH | Công cụ một lần đọc Excel qua `shared/tabular`, chuẩn hóa, báo cáo dòng lỗi | D3 H10 | W11.21, W11.22 | M | — | Mã được (`cmd/hrmimport`, ánh xạ cột cấu hình được); nạp thật chờ tệp nguồn Q-069 | 3 | |
| W11.31 | Nghiệm thu HCTH | `docs/uat/hc-v3.0-acceptance.md`, E2E `tests/hc30`, tải 500 lượt nộp/ngày | D3 H11 | tất cả | M | — | Mã được | 3 | |

**Chăn nuôi – Thủy sản** (CN11 → W11.11)

| Mã | Tên | Nội dung | Nguồn | Phụ thuộc | Cỡ | Migration (tạm) | Trạng thái | Đợt | PO |
|---|---|---|---|---|---|---|---|---|---|
| W11.41 | Danh mục CNTS | ~10 danh mục, ngưỡng mặc định, quy trình tiêm theo ngày tuổi, CSGM nhiều mã mộc dấu, ánh xạ vaccine–loài | D4 CN1 | W11.06 | M | — (enum); bảng mũi tiêm nếu cần | Mã được | 2 | |
| W11.42 | Biểu kỳ Cấp Xã BM-01/04/05/17 | Kỳ ngày + tuần mới, hạn riêng, đơn vị = cơ sở GM, sao chép kỳ trước, không phát sinh, khóa lạc quan; Mobile 3 màn + offline | D4 CN2 | W11.03, W11.05, W11.07, W11.41 | XL | pf 000160 | Chặn Q-045 | 2 | |
| W11.43 | Quy tắc liên biểu + 12 cảnh báo | Vòng đời cảnh báo trên nhiệm vụ SLA (`cap/task` cài ở extension), nhắc cấu hình theo biểu | D4 CN3 | W11.42 | L | pf 000161 | Mã được | 2 | |
| W11.44 | Tổng hợp BM-02, BM-18, lũy kế | Dòng điều chỉnh, file Chi cục có phiên bản, Phụ lục 2 | D4 CN4 | W11.42, W11.10 | L | pf 000162 | Chờ Q-078, Q-079 | 2 | |
| W11.45 | Báo cáo phòng BM-11 | Soạn tự động + phần viết, LĐ duyệt, gửi theo nơi nhận, phiên bản | D4 CN5 | W11.44, W10.17 | L | pf 000166 | Chặn Q-046 | 3 | |
| W11.46 | Lịch trực, lịch tiêm, lịch công tác + check-in GPS | BM-03, BM-07, BM-05/06 | D4 CN6 | W11.41, W11.07 | L | pf 000163 | Chờ Q-057 | 2 | |
| W11.47 | Hộ mô hình, điểm, CT/DA, cấp quyền | ~~`pf_coop` site/batch~~ → bản ghi KN `pf_extension` (CT/DA, điểm, hộ mô hình, quyền CNTS; ADR-0055, Q-KNS-04: không ghi HTX) | D4 CN7 | W10.13, W11.61 | L | pf 20261020161323 | Đã làm 20/10/2026 — giả định tạm Q-023, Q-076, Q-061 ([gia-dinh/W11.47](gia-dinh/W11.47.md)) | 3 | |
| W11.48 | Nhật ký BM-08/09/19 | Lứa, lần ghi có chỉ số, GPS bất biến + 200 m + phát hiện giả lập, ảnh, offline, xác thực, hạch toán NC-15 | D4 CN8 | W11.47, W11.43 | XL | pf 20261024090937 | Đã làm 21/10/2026 — giả định tạm Q-005, Q-077 ([gia-dinh/W11.48](gia-dinh/W11.48.md)); App xã ngoại tuyến chưa | 3 | |
| W11.49 | Dịch bệnh BM-10 | Ổ dịch có phiên bản diễn biến + báo nghi dịch | D4 CN9 | W11.41, W11.07 | M | pf 000164 | Mã được | 2 | |
| W11.50 | Chiến dịch điều tra | Đợt trên kỳ `adhoc`, tách khỏi lũy kế | D4 CN10 | W11.42 | M | pf 000165 | Chờ Q-058 | 2 | |
| W11.52 | Bản đồ vùng nuôi/dịch (GĐ2) | Vẽ vùng, bán kính, hướng di chuyển, theo ngày | D4 CN12 | W11.49, W11.47 | L | pf 20261024090947 | Đã làm 21/10/2026 (kéo vào G1) — giả định bán kính 500 m/3 km/10 km ([gia-dinh/W11.52](gia-dinh/W11.52.md)) | 4 | |
| W11.53 | Dự án KNQG Mẫu 01–07 (GĐ2) | Ký scan, TW xác nhận | D4 CN13 | W11.47, W11.48, W10.17 | L | pf 000178 | GĐ2; chờ Q-055, Q-027 | 4 | |
| W11.54 | Trang chủ phòng + kho văn bản (GĐ2) | `pf/content`, `_pub` | D4 CN14 | W11.44, W11.10 | M | `_pub` | GĐ2 | 4 | Kéo vào G1: làm trong `pf/extension` (`extension/depthome`), pf 20261020175048 — [giả định](gia-dinh/W11.54.md) |
| W11.55 | Nông hộ Mobile (GĐ2) | OTP SMS, nhật ký hộ, lịch tiêm + nhắc, báo nghi dịch | D4 CN15 | W11.48, W11.49, W10.06 | L | pf 000179 | GĐ2; Chặn Q-020, Q-080 | 4 | |

**Kế hoạch – Tài chính**

| Mã | Tên | Nội dung | Nguồn | Phụ thuộc | Cỡ | Migration (tạm) | Trạng thái | Đợt | PO |
|---|---|---|---|---|---|---|---|---|---|
| W11.61 | Danh mục KHTC + Kế hoạch năm | CT/nhóm/hạng mục, 6 tab, phiên bản theo QĐ + ngày hiệu lực, nhập/xuất Excel, sao chép năm trước | D4 KH1 | W11.06 | L | pf 000169 | Chờ Q-061 | 3 | |
| W11.62 | Kỳ KHTC | Mở kỳ, sinh phiếu theo phòng, gắn kỳ nguồn, tiến độ, đóng/hủy, 3 máy trạng thái | D4 KH2 | W11.03, W11.05, W11.61 | L | pf 000170 | Chặn Q-045, Q-081 | 3 | |
| W11.63 | Mẫu phiếu + nhập phiếu | Kiểu cột "Từ Kế hoạch năm", "Lũy kế kỳ trước"; điều chỉnh kỳ trước; so sánh lần gửi | D4 KH3 | W11.62, W11.11 | L | pf 000171 | Chờ Q-047 | 3 | |
| W11.64 | Bộ đọc nguồn module | "Cập nhật số liệu", mốc dữ liệu, "Nguồn đã thay đổi" | D4 KH4 | W11.10 | M | — | Chờ Q-048 | 3 | |
| W11.65 | Mục lục mẫu | Mục, lặp, khuôn câu có biến, khóa, xem trước, phát hành | D4 KH5 | W10.17, W11.61 | XL | trong `pf_export` | Mã được | 3 | |
| W11.66 | Màn soạn + Mẫu 1–5 | Xuất Word/Excel + Bản cuối | D4 KH6 | W11.64, W11.65 | XL | pf 000173 | Mã được | 3 | |
| W11.67 | Số liệu nguồn ngoài | Nhập, Excel hai bước, minh chứng | D4 KH7 | W11.62 | M | pf 000174 | Mã được | 3 | |
| W11.68 | Báo cáo phát sinh | + yêu cầu số liệu bổ sung | D4 KH8 | W11.66, W11.11 | M | pf 000175 | Mã được | 3 | |
| W11.69 | Trang chủ KHTC + báo cáo đã xuất cho TW | `_pub` | D4 KH9 | W11.66 | M | `_pub` | Chờ Q-055 | 3 | |

**Báo giá nông sản** (BG10 → W11.10 + W11.11)

| Mã | Tên | Nội dung | Nguồn | Phụ thuộc | Cỡ | Migration (tạm) | Trạng thái | Đợt | PO |
|---|---|---|---|---|---|---|---|---|---|
| W11.71 | Khung `pf/price` (ADR-0049) | `module.yaml`, vai `r_price`, `pf_price`/`pf_price_pub`, `agnofloat`, thêm vào `app/KN` | D4 BG1 | W11.06 | M | pf 000140 | Chặn Q-044 (kỹ thuật làm được ngay) | 1 | |
| W11.72 | Xã báo giá + phân công | ↔ ĐVHC có hiệu lực, nhóm vùng, CTV/Tỉnh theo ngày, cờ QTNV | D4 BG2 | W11.71, W11.07 | M | pf 000141 | Mã được | 1 | |
| W11.73 | Mặt hàng theo thời kỳ | Nhóm, gán xã, đề xuất, bộ theo vụ, "thay thế cho" | D4 BG3 | W11.71 | M | pf 000142 | Chặn Q-082 | 1 | |
| W11.74 | Lịch báo giá, giờ khóa, nhắc, ngưỡng | Job khóa 12:00 ±1 phút, hạn phản hồi 13:30 | D4 BG4 | W11.71 | M | pf 000143 | Chờ Q-086 | 1 | |
| W11.75 | Nhập báo giá + xác minh | Web responsive, cảnh báo tạm tính, xác minh hai chiều, nhập thay, hiệu chỉnh/xóa mềm, in File 2 | D4 BG5 | W11.72–W11.74 | L | pf 000144 | Mã được | 1 | |
| W11.76 | Duyệt/mở duyệt bảng giá | Phiên bản, khóa "đang duyệt", `price.published` | D4 BG6 | W11.75 | M | pf 000145 | Mã được | 1 | |
| W11.77 | Báo cáo giá | Tiến độ, bảng giá tổng hợp, biến động, dashboard vùng, thống kê phiếu CTV | D4 BG7 | W11.76 | M | `_pub` | Mã được; phần Đợt 2 BRD ở đợt 4 | 2 | |
| W11.78 | Điều tra giá thành | Đợt, nhập file hai bước (có thể kích hoạt tách `cap/import`), PII → vault, chống trùng, CT-05/06 | D4 BG8 | W11.71, W11.73 | XL | pf 000176 | Chờ Q-083, Q-005 | 2 | |
| W11.79 | Chuyển dữ liệu báo giá cũ | ≥ 3 tháng + Hè Thu 2026 | D4 BG9 | W11.75, W11.78 | M | — | Chờ dữ liệu khách (A5) | 2 | |
| W11.81 | Website (Đợt 2 BRD) | Tập dữ liệu `price_board_daily` qua cổng tích hợp | D4 BG11 | W11.76 | M | hợp đồng `contracts/integration` | Chờ A2 (khách) | 4 | |

#### W12 — Vùng nội bộ MKG

| Mã | Tên | Nội dung | Nguồn | Phụ thuộc | Cỡ | Migration (tạm) | Trạng thái | Đợt | PO |
|---|---|---|---|---|---|---|---|---|---|
| W12.01 | Kho quan sát v2 | C-1…C-4: `epistemic_kind`, `valid_from/to`, `source_ref`, `entered_by`, `catalog_version`, nới CHECK `source`/`unit`; đường ghi `manual`; danh mục `contracts/engine/catalog.v1.json` (tạm `provisional`); `Observations.Select` theo quy tắc 7.3 | D2 W8.2b | W8.2 | M | mkg 000039 (hoặc sửa thẳng 000037 nếu Q-028 = chưa áp) | Làm ngay | 1 | |
| W12.02 | Engine chỉ nhận tổng hợp hoặc có mục đích | Chặn `plot_directory`/`cooperative_directory` vào engine nếu cấp phép không có `purpose_code` engine; kiểm thử | D1 W10.15 | W8.2, W4.1 | S | — | Chặn Q-024 | 1 | |
| W12.03 | Ảnh chụp đầu vào engine | `input_snapshot` + `input_snapshot_key`, phiên bản các sổ tham số, ảnh chụp ra kho đối tượng kèm SHA-256, `snapshot.v1.schema.json` | D2 W8.2c | W12.01 | M | mkg 000040 | Làm ngay | 1 | |
| W12.04 | `cap/workflow` ở cụm mkg | `zones: [pf, mkg]`, schema `mkg_workflow`, bốn mắt ở DB cho ba chỗ có tiền (v4.3 §9) | D1 W10.17 | W6.1 | M | mkg 000041 | Làm ngay (mã); dùng thật chờ W12.11–W12.16 | 1 | |
| W12.05 | Dịch vụ engine nội bộ (khung) | Đơn vị triển khai Python thứ hai ở vùng mkg, vhost `mkg.internal`, đọc ảnh chụp, ghi `optcore_version`; không có luồng sang nền tảng | D2 W8.3c | W8.1, W12.03 | M | — | Làm ngay | 1 | |
| W12.06 | Làm thử T2a/T2b (D-6) + ADR-0046 OR-Tools | Bộ sinh dữ liệu cỡ Phase 1, đo p95, giải lại, khoảng chênh chi phí; báo cáo đạt/không đạt | D2 W8.3e | ADR-0046 | M–L | — | Làm ngay; ngưỡng đạt chờ Q-043 | 1 | |
| W12.07 | Cổng tệp mẫu chuẩn MKG | Mẫu CSV/XLSX, staging → người xác nhận → ghi sổ, `source = file` | D2 W8.3a | W12.01 | M | mkg 000042 | Mã được | 2 | |
| W12.08 | VFT: bộ ước tính tồn + cổng nhận nội bộ | Khóa `plant.*`, bộ ước tính Go theo v1.4 §7.5; kênh API nội bộ (khóa API + mTLS, cùng mã cổng tích hợp, R-MOD-10), chữ ký số bắt buộc | D2 W8.3b · D1 W10.8 | W12.01, W12.03; W10.07 (kênh) | M–L | dùng mkg 000039 | Ước tính: mã được; kênh: Chặn Q-040, Q-022 | 2 | |
| W12.09 | Producer feed + thuộc tính đồ thị | Producer `waterway_routes`, `season_harvest_dates`; cỡ ghe tối đa, tĩnh không cầu, mực nước ở đoạn luồng | D2 W8.3h | W5.3 | M | pf 000158 | Mã được | 2 | |
| W12.10 | Chế độ mô phỏng MKG (D-1) | Mở rộng `lib/optcore`: điểm gom, cỡ ghe, T1 HiGHS theo ngày, Monte Carlo thời tiết, tồn an toàn; không đổi byte golden App SIM | D2 W8.3d | W12.05, W12.09 | L | — | Mã được với dữ liệu giả định; chờ Q-042 | 2 | |
| W12.11 | `logistics`: ghe thuê + chặng ghép ghe | Cam kết thuê, khóa "ghe khả dụng theo cỡ/ngày", loại chặng `transshipment` N→1 | D2 W8.5c | W12.01 | M | mkg 000043 | Mã được | 2 | |
| W12.12 | Gia phả dùng chung cho ghép ghe | `facility.kind = consolidation_point` (đề xuất), nút trộn, nới 1 lệnh ↔ 1 lô thành N ↔ 1 | D2 W8.5b | W12.11 | M–L | mkg 000044 | Chặn Q-036 | 2 | |
| W12.13 | `measurement`: CONSOLIDATION + lô DIRECT (D-4) | Điểm cân thứ tư, `weighing_mode`, `sampling`/`roll_estimate` có phiên bản, đối soát DIRECT với `loss_norm:direct`, cờ độ tin cậy; OpenAPI + web-internal + MRV | D2 W8.5a, C-9 | W12.12 | L | mkg 000045 | Lược đồ: Chặn Q-035, Q-037; định mức chờ khách MKG | 2 | |
| W12.14 | `fieldops`: máy cuộn, hỏng hóc | Danh mục 500+ máy có phiên bản, gán tổ, sự kiện hỏng/sửa | D2 W8.5d | W12.18 (người lái) | M | mkg 000046 | Mã được; gắn người lái chờ Q-038 | 2 | |
| W12.15 | `orders`: nguồn cung hai độ tin cậy | Tỷ lệ đội MKG/HTX, năng suất máy là tham số chính sách | D2 W8.5e | — | S | — | Làm ngay | 2 | |
| W12.16 | Kế hoạch đề xuất, vùng đóng băng, tự duyệt (D-7) | `plan_proposal`, `plan_decision`, bộ kiểm 5 điều kiện, `engine.replan.v1`/`autoapprove.v1`, metric đảo ngược | D2 W8.3f | W12.05, W12.11 | L | mkg 000047 | Mã được, bật chờ Q-034 | 3 | |
| W12.17 | Kế hoạch cuộn 7–14 ngày + T3 | Chạy hằng ngày trên ảnh chụp thật; T3 gợi ý phân công tổ | D2 W8.3g | W12.10, W12.16, W12.14 | L | — | Chặn dữ liệu thật (A7, k) | 3 | |
| W12.18 | `mkg/workforce` | Schema định danh riêng trong cụm mkg (mã hóa mức trường, HMAC, xóa bằng hủy khóa), `worker_ref`, ca, chấm công, khoán, xuất bảng công; C-7 | D2 W8.4 · v4.3 §8 | W2.6 | L | mkg 000048–000049 | Chặn Q-039, H-10 (pháp chế) | 3 | |
| W12.19 | Dữ liệu vị trí (D-12, ADR-0047) | Cờ tạm dừng GPS người đếm (làm ngay); tọa độ chính xác ra kho có thời hạn lưu (ts phân vùng); chỉ thu trong giờ làm việc | D2 W8.7, C-8 · v4.3 §8 | — | S–M | mkg 000050 + ts 000005 | Cờ: Làm ngay; phần còn lại Chặn Q-038 | 1 (cờ) / 3 | |

D2 đề xuất số mkg khác (000039–000047 theo thứ tự gói của D2). Bảng trên cấp lại theo thứ tự đợt. Số chỉ để giữ chỗ.

### 3.3 Trình tự đợt

| Đợt | Thời gian (đề xuất) | Gói | Lý do |
|---|---|---|---|
| **0 — Quyết định + tài liệu** | 13–24/10/2026 | W10.00, W11.01; họp QĐ-1…QĐ-10 | Rẻ, không cần khách; mọi gói sau dựa vào số ADR/migration và tài liệu trong repo |
| **1 — Hướng G1** | 27/10–15/12/2026 | Tuân thủ làm ngay: W10.08, W10.09, W10.10 · Đã quyết: W10.01, W10.13 (**hạn cứng 15/12** nếu chọn H-1 b) · Đăng nhập (mã sau cờ): W10.03, W10.05 · KN: W11.02, W11.03, W11.05, W11.06, W11.07, W11.08 (khi có Q-01) · Báo giá nếu QĐ-5 (a): W11.71–W11.76 · MKG phía dữ liệu: W12.01–W12.06, cờ W12.19 · Hạ tầng: W10.19 (khi có staging) | Theo nguyên tắc v4.1 "xếp theo KN nhận dữ liệu thật 15/12"; v4.1 Phụ lục A đòi `cap/forms`, `cap/price` tối thiểu ở G1; đổi chủ dữ liệu phải xong trước dữ liệu thật |
| **2 — Mở rộng KN + lược đồ MKG** | 12/2026–02/2027 | W10.02, W10.04, W10.06, W10.11, W10.12, W10.14–W10.18 · KN: W11.04, W11.09, W11.10 · HCTH: W11.21–W11.24, W11.28, W11.29 (bắt đầu) · CNTS GĐ1: W11.41–W11.44, W11.46, W11.49, W11.50 · Báo giá: W11.77–W11.79 · MKG: W12.07–W12.15 | HCTH dựng nền hồ sơ trước, nghiệp vụ sau; CNTS cần lịch kỳ và `cap/forms` tổng quát của Đợt 1; MKG làm lược đồ sau khi kho quan sát v2 ổn định |
| **3 — Hoàn thiện nghiệp vụ** | 03–05/2027 | W10.07 (G2) · HCTH: W11.25–W11.27, W11.29–W11.31 · CNTS: W11.45, W11.47, W11.48 · KHTC: W11.61–W11.69, W11.11 · MKG: W12.16–W12.19 | KHTC đọc nguồn "đã chốt" của mọi phòng nên đi sau; chữ ký số theo mốc G2 (v4.3 #16) |
| **4 — GĐ2 / Đợt 2 BRD** | sau 05/2027 | W11.52–W11.55, W11.81, phần Đợt 2 của W11.77 | Theo phân kỳ trong BRD CNTS, Báo giá |

### 3.4 Phần vừa trước 15/12/2026

| Nhóm | Gói | Tuần-người (ước) |
|---|---|---|
| Bắt buộc (tuân thủ + dữ liệu thật) | W10.00 (0,5), W10.08 (1,5), W10.09 (1,5), W10.10 (0,5), W10.13 (2,5), W11.01 (0,5) | ≈ 7 |
| Đăng nhập theo QĐ-4 | W10.03 (3), W10.05 (1,5) | ≈ 4,5 |
| Trồng trọt + nền KN | W11.02 (3), W11.03 (1,5), W11.05 (3), W11.06 (0,5), W11.07 (1,5), W11.08 (1,5) | ≈ 11 |
| Báo giá (nếu QĐ-5 a) | W11.71–W11.76 | ≈ 10,5 |
| MKG phía dữ liệu (không gắn G1, làm song song) | W12.01–W12.06 | ≈ 9 |
| **Tổng** | | **≈ 42** (≈ 33 nếu bỏ MKG) |

Khoảng 9 tuần lịch. Muốn kịp phải chạy song song ít nhất 4–5 luồng. Nếu giữ nhịp một gói một agent tuần tự thì nên chọn QĐ-5 (b) (Báo giá sang Đợt 2) và lùi MKG. **Chắc chắn không kịp 15/12:** HCTH (14–20 tuần-người), CNTS, KHTC, `pf/export`, chữ ký số, hạ tầng cấp độ 3 và hồ sơ cấp độ (nên cần QĐ-1 phương án a).

---

## 4. Câu hỏi mở

86 câu, gom từ 90 câu của D1–D4 (19 + 19 + 22 + 30), cộng một câu mới Q-044 rút từ D4 §3.4. ⚠ = gấp (15 câu). Cột **Trả lời** để trống cho PO.

### 4.1 Kiến trúc và tuân thủ

| Mã | Câu hỏi | Bối cảnh / ảnh hưởng | Chặn gói | Nguồn | Đề xuất của dev | **Trả lời** |
|---|---|---|---|---|---|---|
| ⚠ Q-001 | Pháp chế xác nhận G1 15/12 có dữ liệu thật có phải "đưa vào sử dụng" theo NĐ 331/2026 không? Nếu phải mà hồ sơ không kịp: (a) G1 chạy dữ liệu tổng hợp hoặc phạm vi hạn chế có thỏa thuận, hay (b) lùi mốc dữ liệu thật? | v4.3 §20, RV-03; W3.5, H-17 | W3.5, mọi nghiệm thu bằng dữ liệu thật | D1 Q1 | (a) cho G1 + khởi động hồ sơ | |
| ⚠ Q-002 | Chấp nhận khai hai hệ thống thông tin (ADR-22)? Hạ tầng dùng chung nào được giữ và khai là "kết nối" (cụm K8s, RabbitMQ khác vhost, `agrigreen-data`, Argo CD/Prometheus, realm chung, CI)? Có cần realm quản trị riêng từng hệ? | v4.3 §12, ADR-22; D1 §2.1#3 | W10.01, W10.02, W10.05, W10.11 | D1 Q2 | Hai hệ; giữ cụm/CI chung, khai là kết nối; tách vhost; realm quản trị riêng | |
| ⚠ Q-003 | Ai là đơn vị chuyên trách ANM của MKG, nhân sự bảo vệ DLCN? Bộ phận bảo vệ ANM độc lập là nội bộ hay thuê ngoài trong lãnh thổ? Ai là đơn vị độc lập giám sát nghiệm thu? | v4.3 §19, #3, #9; cấp độ 3 cần trước G1 | W10.16, hồ sơ cấp độ | D1 Q11 | Chỉ định trong 2 tuần (v4.3 #3) | |
| Q-004 | Chủ quản App Khuyến nông là MKG hay TTKNQG? | Quyết ai phê duyệt hồ sơ cấp độ, ai ký hợp đồng xử lý dữ liệu (RV-16) | W10.13, hồ sơ cấp độ | D1 Q10 | — | |
| ⚠ Q-005 | DPIA: cần hồ sơ đánh giá tác động trước khi nhận dữ liệu thật cho (i) nền tảng, (ii) hệ nội bộ, (iii) nhật ký CNTS có GPS hộ, (iv) phiếu điều tra có SĐT nông hộ, (v) hồ sơ nhân sự HCTH có dân tộc/tôn giáo/Đảng? | v4.3 §21.2 (2 hồ sơ — chưa có); NĐ 356 thay NĐ 13 | W10.12, W10.18, W11.22, W11.48, W11.78 | D4 Q30 · D1 §1.7 (#5) | Có; một DPIA khung + phụ lục theo module | |
| Q-006 | Có chấp nhận các đường ra nước ngoài đang có: FCM (F-7), GitHub Actions (A8), theo dõi lỗi SaaS, MRV, phòng dữ liệu VFT? Pháp chế đã kiểm danh mục dữ liệu quan trọng/cốt lõi cho dữ liệu nông nghiệp tổng hợp chưa? (Google Sheets: Q-026) | RV-15, RV-17; Luật Dữ liệu 60/2024 | W10.15 | D1 Q14 (phần) | Giữ FCM/CI với dữ liệu không định danh; ghi vào hồ sơ | |
| Q-007 | Thời hạn thông báo vi phạm DLCN và sự cố ANM đã chốt từ toàn văn NĐ 356 / NĐ 331 chưa? | RV-18, K-10 | W10.14 | D1 Q15 | — | |
| Q-008 | Phương tiện thứ hai cho sao lưu 3-2-1 là gì? Vùng mạng văn phòng MKG và Wi-Fi hub do ai thiết kế, vận hành? | RV-19, RV-21 | W10.11 | D1 Q16 (phần) | — | |
| Q-009 | Chỉ tiêu sẵn sàng/RPO/RTO chung cho mọi module KN lấy theo v7.7 (≥ 99,5 %, RPO 24 giờ, RTO 4 giờ)? RPO nền tảng chốt 1 giờ (v4.1) hay 24 giờ (repo, H-13)? | HCTH, Báo giá để trống mục này | W10.19, W10.11 | D1 Q16 (phần) · D3 Q-HC-15 · D4 Q21 (phần) | Theo v7.7 cho KN; RPO 24 giờ cho G1 | |
| Q-010 | Đồng ý đánh số repo ADR-0039…0042 cho ADR-21…24 của v4.3 (tránh trùng ADR-0021…0024)? Yêu cầu kiến trúc sư dùng tiền tố khác (vd "KT-ADR-21") ở bản sau? Ai duyệt? | D1 §0.2 | W10.00 | D1 Q18 · D2 Q17 | Đồng ý; dùng bảng ánh xạ trong `docs/kien-truc/` | |
| Q-011 | Cấp migration từ số trống tiếp theo (mkg ≥ 000039, pf ≥ 000137) và bỏ giữ chỗ mkg 000027–000032, hay chờ bộ chạy v2 (ADR-0038) thành mặc định? | `golang-migrate` bỏ qua im lặng số nhỏ hơn số đã áp | mọi gói có migration | D2 Q8 | Cấp số mới; `shared` 000013 để trống | |
| Q-012 | Xin bản SAD định danh v1.2, BRD định danh v1.2, bản md của MKG v1.4, bộ `docs/db/` (DB-02, DB-05) để đưa vào repo | H-8 mở rộng | W7.2–W7.4, W10.00, W10.03, W10.12 | D1 Q17 | — | |
| ⚠ Q-013 | Sửa thanh toán GitHub Actions (A8) | Bằng chứng CI là đầu vào hồ sơ cấp độ (TCVN 5.17) | W10.16 | D1 Q19 | Sửa ngay | |
| Q-014 | Người review mã thứ hai (nền tảng + hệ nội bộ) là ai, từ khi nào? Đội Odoo nội bộ chuyển sang Go hay vai trò khác? | ADR-21 Việc 1; v1.4 D-10 | — | D1 Q12 | — | |
| Q-015 | Phần mềm kế toán pháp định đã chọn chưa (TT 99/2025, HĐĐT, lương mùa vụ, kho phụ tùng/nhiên liệu)? Định dạng nhận bút toán cho `mkg/acctbridge`? | ADR-21 Việc 2; v1.4 D-11; Q-1 cũ | `acctbridge` | D1 Q13 | — | |

### 4.2 Bảo mật và tài khoản

| Mã | Câu hỏi | Bối cảnh / ảnh hưởng | Chặn gói | Nguồn | Đề xuất của dev | **Trả lời** |
|---|---|---|---|---|---|---|
| ⚠ Q-016 | Giữ phiên Web 4 giờ / app 24 giờ đã chốt với khách KN (kèm đánh giá rủi ro biện minh), hay chuyển 15 phút Web / 5 phút app khóa PIN / 5 phút quản trị? Ai trao đổi lại với TTKN An Giang/TTKNQG? | v4.3 P-5 trái D9 (`realm_test.go`) | W10.05, W10.04 | D1 Q3 | Giữ 4 giờ cho Web nghiệp vụ kèm đánh giá rủi ro; quản trị 5 phút | |
| ⚠ Q-017 | MFA bắt buộc cho toàn bộ cán bộ KN, HTX, Cục KTHT trên Web từ G1, hay theo lộ trình ghi trong hồ sơ? Yếu tố ưu tiên: TOTP, passkey, hay xác nhận đẩy (cần SPI Keycloak chưa kiểm chứng)? | v4.3 P-1, ADR-23; hiện chỉ vai `[MFA]` ở Admin API | W10.03 | D1 Q4 | TOTP + passkey từ G1 cho Cấp Tỉnh/TW; Cấp Xã theo lộ trình | |
| Q-018 | Nâng mật khẩu lên 14 ký tự + độ phức tạp + không trùng 10 cho mọi tài khoản (Keycloak đặt theo realm), hay tách realm quản trị để chỉ quản trị đổi mật khẩu 2 tháng? | v4.3 P-3; repo 12/5 | W10.05 | D1 Q5 | Tách realm quản trị (đi cùng Q-002) | |
| Q-019 | Đồng ý vô hiệu sau 45 ngày không hoạt động cho mọi loại tài khoản (cán bộ và nông hộ), nông hộ tự kích hoạt lại bằng OTP? | v4.3 P-4; repo chưa có cơ chế | W10.05 | D1 Q6 (phần) | Đồng ý; báo trước 7 ngày | |
| Q-020 | Nhà cung cấp SMS trong nước nào cho OTP (nông hộ CNTS, CTV Báo giá, viên chức HCTH không có tài khoản)? Trần chi phí? Chấp nhận chỉ email ở đợt 1? Thống nhất đăng nhập: Nông hộ SĐT + OTP (CNTS), CTV email/SĐT + mật khẩu + PIN (Báo giá), cán bộ PIN 6 số + offline 72 giờ (Trồng trọt)? | Repo chưa có SMS; ADR-0007/0015 | W10.06, W11.55, W10.18 | D1 Q6 (phần) · D3 Q-HC-8 (phần) · D4 Q20 | Email trước; SMS khi có hợp đồng | |
| Q-021 | Liên kết phiếu + OTP cho viên chức không có tài khoản: liên kết hết hạn khi khóa bước; có giới hạn số lần mở/thử OTP? Nếu hồ sơ thiếu cả email lẫn SĐT thì chỉ còn đường lãnh đạo phòng nhập thay? | Bề mặt tấn công mới với người ngoài Keycloak; cần ADR-0048 | W10.18, W11.26 | D3 Q-HC-8 (phần), Q-HC-13 | 5 lần thử/OTP, 10 lần mở/liên kết; thiếu liên hệ thì nhập thay | |
| Q-022 | Chọn nhà cung cấp chứng thư số (và HSM/ký từ xa) cho MKG. HTX chưa có chứng thư thì nền tảng ký thay có ủy quyền: pháp chế chấp nhận hình thức ủy quyền nào? Ai cấp chứng thư cho VFT? Hạn G2 có giữ? | v4.3 ADR-24, v1.4 §10.2 | W10.07, W12.08 | D1 Q9 · D2 Q14 (phần) | — | |

### 4.3 Dữ liệu và chủ sở hữu

| Mã | Câu hỏi | Bối cảnh / ảnh hưởng | Chặn gói | Nguồn | Đề xuất của dev | **Trả lời** |
|---|---|---|---|---|---|---|
| ⚠ Q-023 | Xác nhận HTX là bên kiểm soát dữ liệu hộ/xã viên/thửa do HTX nhập (H-1 phương án b), và cơ quan khuyến nông là bên kiểm soát dữ liệu khuyến nông? | Đổi chủ sau khi có dữ liệu thật phải làm bằng bản ghi bù; hạn trước 15/12 | W10.13, W11.47 | D1 Q7 | (b) | |
| ⚠ Q-024 | Engine điều phối có được nhận `plot_directory`, `cooperative_directory` qua cấp phép có mục đích "điều phối", hay chỉ tổng hợp theo xã qua k? | v4.3 §21.1 trái W8.2; v1.4 §7.7 lại dùng diện tích thửa để tính tấn/ha | W12.02 | D1 Q8 | Nhận khi cấp phép có `purpose_code` điều phối | |
| Q-025 | MKG có chấp nhận ADR-0035 dùng `grant_class = aggregate` thay vì thêm loại vào `ShareKinds`/CHECK như v1.4 §7.7 là cách hiện thực D-5 không? | Tương đương về mục đích | — (ghi nhận) | D2 Q16 | Chấp nhận | |
| Q-026 | Xuất Google Sheet cho Chi cục (ADR-0026, Q-14): bỏ hẳn (giữ liên kết tải 7 ngày) hay chấp nhận đưa số liệu KN ra ngoài lãnh thổ? | RV-15; lệch A4 đang dùng liên kết 7 ngày | W11.09, W10.15 | D1 Q14 (phần) · D3 T4, Q-TT-2 (phần) | Bỏ Google Sheet, giữ liên kết tải | |
| Q-027 | Cấp TW (TTKN QG) là tenant riêng hay nằm trong cây tenant An Giang? | Tenant riêng thì mọi màn TW cần cấp phép `intra_platform` hoặc tập dữ liệu | W11.53, W11.69, W11.77 | D4 Q17 | — | |

### 4.4 MKG

| Mã | Câu hỏi | Bối cảnh / ảnh hưởng | Chặn gói | Nguồn | Đề xuất của dev | **Trả lời** |
|---|---|---|---|---|---|---|
| Q-028 | Migration mkg 000037 đã áp ở môi trường dùng chung nào chưa (staging, UAT)? | Chưa áp thì sửa thẳng `kind` → `key`, gọn hơn | W12.01 | D2 Q1 | — | |
| Q-029 | `max_age` ở danh mục D-2 có thay hẳn `freshness.max_age` của hợp đồng feed trong quy tắc chọn không? | Job 15 phút = PT15M nên quan sát chập chờn Stale | W12.01 | D2 Q2 | Danh mục quyết định chọn; feed chỉ cho cảnh báo vận hành | |
| Q-030 | MKG có chấp nhận `mkg/engine` là module mới thứ hai (ngoài `mkg/workforce`), trái câu "từ năm còn một" ở v1.4 §5? Sổ kế hoạch đề xuất cũng đặt ở đây? | W8.2 đã tạo | W12.16 | D2 Q3 | Chấp nhận | |
| Q-031 | Đầu ra bộ ước tính được ghi sổ thành quan sát `estimated`, hay chỉ tính lúc chụp và ghi trong ảnh chụp? | v1.4 §7.3 quy tắc 1 | W12.08 | D2 Q4 | Chỉ ghi trong ảnh chụp | |
| Q-032 | Khi bên cấp thu hồi cấp quyền (`grant_revoked`): có xóa quan sát `manual`/`file` cùng entity do MKG tự nhập? Lịch sử quan sát của bên đã thu hồi giữ hay xóa? | ADR-0036 ghi "chờ A7" | W12.01 | D2 Q5 | — | |
| Q-033 | Số đo mốc đã quá `max_age` 7 ngày: bộ ước tính vẫn chạy với độ bất định tăng dần, hay coi khóa là thiếu và từ chối chạy? | v1.4 §7.3 #1 | W12.08 | D2 Q6 | — | |
| Q-034 | X% của tự duyệt và các ngưỡng kích hoạt (48 giờ, 4 giờ, nửa ngày): ai đề xuất, ai là người thứ hai duyệt? | D-7, `[KÝ]` | W12.16 | D2 Q7 | — | |
| Q-035 | `migratecheck` có cho DROP/ADD CHECK trên bảng sổ đã phân vùng (thêm `CONSOLIDATION`, nới `route`, `param_kind`), hay phải dùng sổ v2? | C-9 | W12.13 | D2 Q9 | — | |
| Q-036 | Điểm gom là "cơ sở" (`facility.kind = consolidation_point`) trong `mkg/hub`, hay chỉ là nút mạng của `logistics`? | Quyết mô hình gia phả | W12.12 | D2 Q10 | Là cơ sở | |
| Q-037 | Chế độ `roll_estimate`: kg/cuộn bình quân có phiên bản của HTX là tham số có duyệt, hay là ước lượng học tự động `KgPerRollAsOf` được đóng băng theo kỳ? | — | W12.13 | D2 Q11 | — | |
| ⚠ Q-038 | GPS người đếm cuộn đang thu vào sổ không xóa được (`roll_count_location`): tạm dừng ở production tới khi có D-12? Giữ tọa độ trong sổ hay chuyển ra kho có thời hạn? | v4.3 §8, v1.4 D-12; vị trí người lái/người đếm = dữ liệu vị trí cá nhân | W12.19, W12.14 | D2 Q12 | Tạm dừng bằng cờ ngay; tọa độ ra kho có thời hạn | |
| Q-039 | 1.500 công nhân hiện trường có tài khoản đăng nhập không (ADR-23: thiết bị + PIN), hay chỉ trưởng tổ? | Quyết `worker_ref` có tách khỏi subject tài khoản | W12.18 | D2 Q13 | — | |
| Q-040 | Kênh VFT: đẩy qua API nội bộ, gửi tệp mẫu chuẩn, hay cả hai? Kế hoạch tiêu thụ có `valid_from/to` theo ngày hay theo lò? | H-9 | W12.08 | D2 Q14 (phần) | — | |
| Q-041 | Thủy triều và dự báo mưa: nguồn trong nước nào? ADR-0007 có bắt mọi lời gọi ra ngoài đi qua proxy bất kể nội dung không? | v1.4 §7.7 | W12.10 | D2 Q15 | — | |
| Q-042 | Mô phỏng MKG có được chạy ngay trên đồ thị hiện có (`pf_waterway`, chỉ có `waterway_class`), hay chờ thuộc tính cỡ ghe và tĩnh không (W12.09)? | — | W12.10 | D2 Q18 | Chạy ngay với cờ "đồ thị thiếu thuộc tính" | |
| Q-043 | Ngưỡng tiêu chí đạt của D-6 (p95 sửa nhanh, khoảng chênh chi phí chấp nhận được) do ai định trước khi chạy? | — | W12.06 | D2 Q19 | — | |

### 4.5 Khuyến nông chung và mâu thuẫn giữa các BRD

| Mã | Câu hỏi | Bối cảnh / ảnh hưởng | Chặn gói | Nguồn | Đề xuất của dev | **Trả lời** |
|---|---|---|---|---|---|---|
| ⚠ Q-044 | Phạm vi KN ở G1 15/12: chỉ Trồng trọt, hay Trồng trọt + Báo giá vận hành ngày (v4.1 Phụ lục A đòi `cap/forms`, `cap/price` tối thiểu)? | Báo giá ~10 tuần-người; xem 3.4 | W11.71–W11.76 | mới, từ D4 §3.4 | Có Báo giá nếu chạy ≥ 4 luồng | |
| ⚠ Q-045 | Tuần báo cáo: KHTC "Thứ Tư – Thứ Ba, đã chốt, bắt buộc mọi module" và CNTS "Thứ Ba – Thứ Hai, hạn 12:00 Thứ Ba", repo tính tuần từ Thứ Hai (Q-17 tạm). Chốt một lịch duy nhất? KSGM 4 tuần/tháng giữ song song? | Ba BRD cùng ngày 07/10 | W11.03, W11.05, W11.42, W11.62 | D4 Q1 | Thứ Tư – Thứ Ba; KSGM giữ song song | **D8 (CEO 13/10/2026): mỗi phòng giữ lịch tuần riêng** — CNTS Thứ Ba – Thứ Hai (BRD CNTS), KHTC Thứ Tư – Thứ Ba (v2 từ 04/11/2026), phòng khác v2 mặc định; hệ thống tự quy đổi cho KHTC (PFPUB). Xem [giả định D8](gia-dinh/D8.md) |
| Q-046 | Đường CNTS → KHTC: CNTS gửi báo cáo tháng/quý/năm vào hộp "Báo cáo đến" của KHTC (vai khách), trong khi KHTC nói "các phòng không gửi file", chỉ lấy dữ liệu đã chốt + phiếu FN-PN-03. Giữ cả hai hay bỏ hộp "Báo cáo đến"? | CNTS v3.7 không có FN-PN-03 | W11.45, W11.11 | D4 Q2 | Giữ BM-11 gửi để lưu; số liệu chỉ qua phiếu + nguồn | |
| Q-047 | Sửa số kỳ cũ: CNTS cho sửa (trước chốt/khi mở lại) và lũy kế kỳ sau tính lại; KHTC không sửa kỳ đã khóa mà ghi "Điều chỉnh kỳ trước" ở phiếu kỳ sau. Khi KHTC đọc lũy kế CNTS, lũy kế nào là chuẩn? | — | W11.05, W11.63 | D4 Q3 | Lũy kế của nguồn (CNTS) | |
| Q-048 | "Nguồn CNTS đã chốt cho tuần X" là gì, khi CNTS chốt theo biểu × kỳ, Phụ lục 2 nuôi trồng không có chốt, Trồng trọt chốt theo đợt phát hành? | — | W11.10, W11.64 | D4 Q4 | Mọi biểu nguồn của tuần đã chốt | |
| Q-049 | Mã FN/UR trùng giữa các BRD (FN-AD-04.03, FN-AD-04.06, FN-TI-02.0x, UR-XA-21…). BA thêm tiền tố module (`TT-`, `HC-`, `CN-`, `KH-`, `BG-`)? | Truy vết UAT bị lẫn | W11.01 | D4 Q5 · D3 Q-HC-16, C8 | Thêm tiền tố | |
| Q-050 | Một luồng đăng nhập theo v7.7 cho mọi module (email/SĐT, chọn vai trò và phòng ban sau xác thực)? HCTH FN-05 ghi tên đăng nhập + dropdown vai trò; CNTS/Báo giá tham chiếu Trồng trọt v6.3/v6.0 | — | W11.06 | D3 Q-HC-1, C1, C2 · D4 Q6 | Theo v7.7 | |
| ⚠ Q-051 | Tên và mã 6 phòng ban: "Thông tin Tuyên truyền" (Báo giá, KHTC) hay "Thông tin – Truyền thông" (seed, v7.7); thêm mã HCTH; bỏ `phong_ban_5/6` tạm | Q-02 cũ | W11.06 và mọi gói KN mới | D3 Q-TT-5 (Q-02) · D4 Q7 | — | |
| Q-052 | BA sửa lỗi văn bản và bổ sung mục thiếu: Trồng trọt (FN-TI-05, menu FN-AD-04.06, "Mục VI", Mục V.3/VII); HCTH (Mục IX, "7 điểm cần xác nhận", FN-23); Báo giá (Mục VII, người lập, mục sẵn sàng/sao lưu) | — | W11.01 | D3 Q-TT-1, Q-HC-5, C9 · D4 Q21 | — | |
| Q-053 | Phân nhóm địa bàn: CNTS "Khu vực" (huyện cũ, 102 xã), Báo giá "Nhóm vùng" An Giang/Kiên Giang (26 xã), repo có `Regions` (Q-04). Hai cách gom cùng tồn tại? KHTC "tập trung tại các xã" theo cách nào? | — | W11.07, W11.72 | D4 Q10 | Cả hai là thuộc tính riêng | |
| Q-054 | Cấp Tỉnh sửa số gốc: CNTS/Trồng trọt không sửa, chỉ yêu cầu điều chỉnh; Báo giá cho sửa giá, sửa ngày, xóa phiếu. Chấp nhận khác nhau theo phòng? | Repo ghi được dạng hiệu chỉnh + xóa mềm | W11.75 | D4 Q11 | Chấp nhận theo phòng | |
| Q-055 | Quyền Cấp TW khác nhau theo phòng (CNTS chỉ KNQG; KHTC báo cáo đã xuất; Báo giá bảng giá, giá thành; Trồng trọt dashboard + báo cáo xã). Cần quyền theo phòng × chức năng? | `kn_tw` hiện là "Nút xuống dưới ∩ phòng ban" | W11.07, W11.53, W11.69 | D4 Q13 | Có | |
| Q-056 | Ai thiết kế biểu: Cấp Tỉnh (CNTS), Admin (Trồng trọt), Admin + KHTC (KHTC), QTNV (Báo giá)? | `forms.Profile.DefineBy` = Admin KN + `reports_admin` | W11.05 | D4 Q16 | Cấu hình theo Profile từng phòng | |
| Q-057 | Lịch công tác: CNTS có check-in tại địa điểm và người duyệt = "người theo dõi được phân công"; Trồng trọt người duyệt = Trưởng phòng, không check-in. Một quy tắc chung hay theo phòng? | Q-05, Q-16 cũ | W11.46 | D4 Q18 | Theo phòng (tham số) | |
| Q-058 | Gom "đợt điều tra" của CNTS (nhập trong app theo xã) và Báo giá (nhập file Google Form) thành một năng lực (kỳ `adhoc` của `cap/forms`)? | — | W11.50, W11.78 | D4 Q19 | Gom ở mức kỳ; nguồn nhập khác nhau | |
| Q-059 | Danh mục cấp quản lý chung cho các module (Lãnh đạo đơn vị, Trưởng phòng, Phó trưởng phòng, Phụ trách, Chuyên viên – Cán bộ) và 5 nhóm Cấp Tỉnh của CNTS? "Lãnh đạo đơn vị xem mọi phòng ban, chỉ đọc" có áp cho Module 1? | — | W11.07 | D3 Q-HC-3, C3 · D4 §1.1 (X3) | — | |
| Q-060 | Khách hàng có ký nhận các lệch đã quyết nội bộ A1–A5: khóa hẳn sau 3 lần khóa tạm; liên kết kích hoạt thay mật khẩu tạm; liên kết tải 7 ngày thay Google Sheet; vẽ thửa tối thiểu 3 điểm? | Lệch chữ BRD | W11.02 | D3 Q-TT-2 | — | |
| Q-061 | CT/DA trùng giữa CNTS và KHTC: một danh sách chung (KHTC là chủ, CNTS gắn điểm/hộ)? Kết quả thu – chi lấy từ phiếu KHTC hay từ nhật ký NC-15 của CNTS (tránh nhập hai lần)? | — | W11.47, W11.61 | D4 Q15 | KHTC là chủ danh sách; thu – chi từ CNTS | |
| Q-062 | Xuất Word (KHTC Mẫu 1–5, CNTS BM-11, HCTH MS05/06): dùng mẫu .docx gốc của Sở/TTKN làm khung, hay tự dựng bộ ghi? Có cần PDF cho biểu NS? | Repo chưa có bộ ghi .docx | W10.17, W11.26, W11.66 | D3 Q-HC-11 · D4 Q26 | Mẫu .docx gốc làm khung | |

### 4.6 Trồng trọt

| Mã | Câu hỏi | Bối cảnh / ảnh hưởng | Chặn gói | Nguồn | Đề xuất của dev | **Trả lời** |
|---|---|---|---|---|---|---|
| ⚠ Q-063 | Khách gửi tệp Excel 14 mẫu gốc (Q-01) khi nào? | Chặn FN-XA-03.02, FN-TI-02.05, N-14 | W11.08 | D3 Q-TT-5 | — | |
| Q-064 | Vẽ thửa trên Web (FN-XA-03.05) có bắt buộc cho đợt nghiệm thu, hay chấp nhận "Web xem, Mobile vẽ"? | — | W11.02 | D3 Q-TT-3 | Chấp nhận Web xem | |
| Q-065 | Ưu tiên cắt KN sang `cap/forms` native (W11.04) trước hay sau khi khách gửi Q-01? | Đổi client web và mobile | W11.04 | D3 Q-TT-4 | Sau G1 | |
| Q-066 | Các câu Q-03…Q-23 của `kn-v7.7-plan.md` §5 (trừ Q-21) vẫn mở; nhắc khách trả lời cùng đợt | Danh sách ở repo | W11.02 | D3 Q-TT-5 (phần) | — | |

### 4.7 Hành chính – Tổng hợp

| Mã | Câu hỏi | Bối cảnh / ảnh hưởng | Chặn gói | Nguồn | Đề xuất của dev | **Trả lời** |
|---|---|---|---|---|---|---|
| Q-067 | Lãnh đạo các phòng chuyên môn làm việc HCTH trong phiên phòng ban nào: gán thêm phòng ban HCTH, hay menu "Nhân sự của phòng" hiện ở mọi phòng ban theo cấp quản lý? | C4 | W11.07 | D3 Q-HC-2 | Menu hiện theo cấp quản lý | |
| Q-068 | Danh mục đơn vị nội bộ (BGĐ, VPTT, phòng, trạm, trại, "Tăng cường"): dùng danh mục phòng ban (P2) hay danh mục đơn vị riêng của HR? Thứ tự sắp NS01 lấy từ đâu? | C5 | W11.07, W11.27 | D3 Q-HC-4 | Mở rộng `department` với loại `tram`/`trai` | |
| Q-069 | FN-09 cần "ngày xếp bậc hiện tại" và "HS bảo lưu" để tính mốc, nhưng hồ sơ "không lưu quá trình ngạch, bậc". Cho lưu hai trường này? Dữ liệu ban đầu lấy từ cột nào? | C14 | W11.22, W11.25, W11.30 | D3 Q-HC-6 | Lưu trong hồ sơ | |
| ⚠ Q-070 | Bảng hệ số theo ngạch/bậc, thời gian giữ bậc, quy chế NLTH, % TNVK, tuổi nghỉ hưu, số ngày nhắc: Phòng HCTH gửi khi nào (A3)? | Chặn nâng bậc | W11.21, W11.25 | D3 Q-HC-7 (phần) | — | |
| Q-071 | Căn cứ pháp lý để Cấp Xã và Cấp TW xem năm sinh, SĐT của mọi cán bộ trong Contact; có cần đồng ý của chủ thể? Liên hệ Trung ương do ai nhập? | BRD dẫn NĐ 13/2023, đã bị NĐ 356/2025 thay | W11.23 | D3 Q-HC-9 | Mục đích `contact_directory`, có thông báo chủ thể | |
| Q-072 | Contact là năng lực dùng chung cho mọi app (HTX, MKG?) hay chỉ trong KN? Có thay "danh bạ trực" G1 (`pf_extension.staff`)? | — | W11.23 | D3 Q-HC-10 | Dùng chung; thay danh bạ trực | |
| Q-073 | HCTH triển khai trong cùng app KN (một web-ops, menu theo phòng ban) hay app riêng (`app/HC`)? Cùng tenant An Giang? | — | W11.06, W11.29 | D3 Q-HC-12 | Cùng app KN, cùng tenant | |
| Q-074 | Đổi bảng hệ số, hủy quyết định nâng bậc, mở lại đợt đã chốt cho một người: cần duyệt bốn mắt (`cap/workflow`), hay chỉ ghi lý do? | — | W11.25, W11.26 | D3 Q-HC-14 | Chỉ ghi lý do; bốn mắt cho đổi bảng hệ số | |
| Q-075 | "Kỷ luật" lấy từ đâu khi BRD không quản lý kỷ luật? Hình thức hiển thị FN-09: chốt màn danh sách + popup xuất như wireframe Hình 17? | — | W11.25 | D3 Q-HC-7 (phần), Q-HC-17 | Cờ nhập tay khi chuyển đổi; màn danh sách | |

### 4.8 Chăn nuôi – Thủy sản

| Mã | Câu hỏi | Bối cảnh / ảnh hưởng | Chặn gói | Nguồn | Đề xuất của dev | **Trả lời** |
|---|---|---|---|---|---|---|
| Q-076 | Hộ/điểm/lứa CNTS: mở rộng `dom/cooperative` hay module mới `dom/husbandry`? Hộ mô hình CNTS có trùng hộ trong `pf_coop.household` (cùng `household_ref`)? | Gắn với Q-023 | W11.47 | D4 Q14 | Mở rộng `pf/cooperative`; dùng chung hộ | |
| Q-077 | Mẫu sổ nhật ký thủy sản BM-19 ("chờ phòng gửi mẫu sổ") gửi khi nào? | Chặn phần thủy sản của nhật ký | W11.48 | D4 Q23 | — | |
| Q-078 | Phụ lục 2 nuôi trồng (~20 đối tượng): nhập theo tuần hay theo tháng? Có lũy kế nhập tay không? | — | W11.44 | D4 Q24 | — | |
| Q-079 | File chia sẻ Chi cục: kênh nào (email như Trồng trọt FN-TI-02.06, Google Sheet)? Có phải đăng ký như "báo cáo đã gửi cơ quan ngoài" để KHTC mục A.II.3.1 lấy? | Gắn Q-026 | W11.44 | D4 Q25 | Email + liên kết tải; có đăng ký | |
| Q-080 | CNTS mobile: dùng `apps/field-app` cho cán bộ xã và `apps/htx-app` chế độ nông dân (NH) cho nông hộ, hay app riêng? | — | W11.42, W11.55 | D4 Q29 | Dùng hai app hiện có | |

### 4.9 Kế hoạch – Tài chính

| Mã | Câu hỏi | Bối cảnh / ảnh hưởng | Chặn gói | Nguồn | Đề xuất của dev | **Trả lời** |
|---|---|---|---|---|---|---|
| Q-081 | Số lần gửi phiếu KHTC: "không giới hạn" (BR-DL-11, UR-PN-03) hay "tối đa 3, gửi lần 3 tự khóa" (FN-TI-02.03/02.04)? | Mâu thuẫn trong cùng BRD | W11.62, W11.63 | D4 Q8 | Tối đa 3 (tham số) | |

### 4.10 Báo giá nông sản

| Mã | Câu hỏi | Bối cảnh / ảnh hưởng | Chặn gói | Nguồn | Đề xuất của dev | **Trả lời** |
|---|---|---|---|---|---|---|
| Q-082 | Chủ danh mục mặt hàng giá: Admin trong Danh mục KHTC (KHTC FN-AD-04.01) hay Phòng TTTT/QTNV theo thời kỳ (Báo giá FN-AD-04.03)? | — | W11.73, W11.61 | D4 Q9 | Một danh mục ở `cap/price`, KHTC đọc | |
| Q-083 | Báo cáo giá thành cho TW/website có áp ngưỡng k không, k bao nhiêu? BRD hiển thị "Mẫu nhỏ" khi < 3 phiếu; nền tảng đặt k sàn 10 khi rời tenant (ADR-0035) | — | W11.78, W11.77 | D4 Q12 | k = 10 khi rời tenant; trong tenant giữ nhãn "Mẫu nhỏ" | |
| Q-084 | Bổ sung vào BRD Báo giá yêu cầu của KHTC §3.4 (bảng giá đã duyệt, số phiếu theo tuần báo cáo, FN-PN-03). Giá "từ – đến" trong Mẫu 1 A.III là min/max toàn tỉnh (CT-04) hay của xã nào? Ngày chốt chưa duyệt thì xử lý thế nào? | — | W11.10, W11.66 | D4 Q22 | min/max toàn tỉnh; lấy ngày đã duyệt gần nhất | |
| Q-085 | Thống kê phiếu theo CTV là căn cứ thanh toán: cần ký số hoặc khóa tháng? Phiếu in File 2 có cần chữ ký số? | — | W11.77, W11.75 | D4 Q27 | Khóa tháng; không ký số | |
| Q-086 | Lịch báo giá Thứ Hai – Thứ Sáu mặc định, ngày nghỉ, ngày bổ sung đã xác nhận chưa (FN-AD-04.04 BR-04)? Giới hạn mở duyệt 30 ngày? | Báo giá Mục VII thiếu | W11.74, W11.76 | D4 Q28 · D4 Q21 (phần) | — | |

---

## 5. Mâu thuẫn giữa các tài liệu

| # | Loại | Tài liệu A nói | Tài liệu B nói | Ảnh hưởng | Câu hỏi |
|---|---|---|---|---|---|
| 1 | BRD↔BRD | KHTC v2.2 GD-05/BR-DL-01: tuần Thứ Tư – Thứ Ba, "đã chốt, bắt buộc mọi module" | CNTS v3.7: tuần Thứ Ba – Thứ Hai, hạn 12:00 Thứ Ba; repo `period.go` tuần từ Thứ Hai | Lịch kỳ chung | Q-045 |
| 2 | BRD↔BRD | CNTS FN-16: gửi báo cáo cho KHTC vào hộp "Báo cáo đến" | KHTC: các phòng không gửi file; FN-16 CNTS không là nguồn | Có hay không hộp nhận | Q-046 |
| 3 | BRD↔BRD | CNTS: sửa kỳ cũ, lũy kế sau tính lại | KHTC BR-DL-12: không sửa kỳ khóa, điều chỉnh ở kỳ sau | Lũy kế nào chuẩn | Q-047 |
| 4 | BRD (nội bộ) | KHTC BR-DL-11, UR-PN-03: gửi lại không giới hạn | KHTC FN-TI-02.03/04: tối đa 3 lần | Máy trạng thái phiếu | Q-081 |
| 5 | BRD↔BRD | FN-AD-04.03 = phòng ban (TT) = mục lục mẫu (KHTC) = mặt hàng (Báo giá); UR-XA-21 trùng giữa TT và HCTH | — | Truy vết nghiệm thu | Q-049 |
| 6 | BRD↔BRD | Báo giá, KHTC: "Phòng Thông tin Tuyên truyền" | Seed repo, TT v7.7: "Thông tin – Truyền thông" | Seed phòng ban | Q-051 |
| 7 | BRD↔BRD | CNTS tham chiếu TT v6.3; Báo giá v6.0; KHTC v7.7; HCTH FN-05 đăng nhập bằng tên đăng nhập + dropdown vai trò | TT v7.7: email/SĐT, chọn vai trò + phòng ban sau xác thực | Luồng đăng nhập chung | Q-050 |
| 8 | BRD↔BRD | KHTC: danh mục mặt hàng giá do Admin trong Danh mục KHTC | Báo giá: TTTT/QTNV là chủ danh mục theo thời kỳ | Chủ dữ liệu | Q-082 |
| 9 | BRD↔BRD | CNTS, TT: Cấp Tỉnh không sửa số gốc | Báo giá: Cấp Tỉnh sửa giá, ngày, xóa phiếu | Quy tắc hiệu chỉnh | Q-054 |
| 10 | BRD↔BRD | CNTS: Cấp Tỉnh thiết kế biểu; check-in lịch công tác | TT: Admin quản lý mẫu; lịch không check-in, Trưởng phòng duyệt | Profile `cap/forms`, lịch | Q-056, Q-057 |
| 11 | BRD↔BRD | CNTS: CT/DA tự quản; thu – chi từ nhật ký NC-15 | KHTC: Kế hoạch năm có CT VII–XII của CNTS; thu – chi qua phiếu | Nhập hai lần | Q-061 |
| 12 | BRD↔BRD | CNTS "Khu vực" (102 xã) | Báo giá "Nhóm vùng" (26 xã) | Cách gom địa bàn | Q-053 |
| 13 | BRD↔BRD | HCTH, Báo giá: sẵn sàng/sao lưu "thống nhất khi triển khai" hoặc để trống | TT v7.7: ≥ 99,5 %, RPO 24 giờ, RTO 4 giờ | NFR chung | Q-009 |
| 14 | BRD↔BRD | Trồng trọt "Final" | Thực chất là v7.7, không có mục V.3/VII như trích dẫn | Không đổi yêu cầu | Q-052 |
| 15 | BRD↔v4.3 | TT v7.7 D9: phiên 4 giờ/24 giờ, mật khẩu 12/5, MFA chỉ Admin (CNTS NFR cũng "MFA Admin") | v4.3 P-1, P-3, P-5: MFA mọi người dùng không phải nông hộ, 14 ký tự/không trùng 10, 15/5 phút | Cấu hình realm đã khóa bằng kiểm thử | Q-016–Q-018 |
| 16 | BRD↔v4.3 | CNTS, HCTH dẫn NĐ 13/2023 | v4.3 §21: Luật BVDLCN 91/2025 + NĐ 356/2025 thay NĐ 13 | Căn cứ trong BRD lỗi thời; DPIA | Q-005, Q-071 |
| 17 | BRD↔v4.3 | Báo giá: CTV email/SĐT + mật khẩu + PIN; HCTH: liên kết + OTP cho người không có tài khoản | v4.3 P-2/ADR-23: nông hộ Zalo/OTP, người còn lại MFA | Liên kết khách là bề mặt mới, cần ADR | Q-020, Q-021 |
| 18 | BRD↔v4.3/nền tảng | Báo giá: "Mẫu nhỏ" khi < 3 phiếu, vẫn hiển thị | ADR-0035: k sàn 10 khi rời tenant | Báo cáo giá thành cho TW/website | Q-083 |
| 19 | BRD↔v4.3 | TT FN-TI-02.06: tạo Google Sheet cho Chi cục (ADR-0026 đề xuất) | v4.3 RV-15: chuyển dữ liệu ra nước ngoài phải rà | Lệch A4 đang dùng liên kết 7 ngày | Q-026 |
| 20 | BRD↔v4.3 | HCTH lưu dân tộc, tôn giáo, Đảng; CNTS nhật ký có GPS hộ; Báo giá phiếu điều tra có SĐT | v4.3 §21.2: dữ liệu nhạy cảm, vị trí → DPIA | Phải có DPIA trước dữ liệu thật | Q-005 |
| 21 | v4.3↔MKG v1.4 | v4.3 §21.1: dữ liệu HTX tới engine chỉ dạng tổng hợp theo xã qua k (trừ khi HTX cho phép) | v1.4 §7.7: tự đo tấn/ha từ cuộn × kg/cuộn × **diện tích thửa**; W8.2 ánh xạ `plot_directory` | Engine có được dùng dữ liệu mức thửa | Q-024 |
| 22 | v4.3↔MKG v1.4 | v4.3 §6: module `mkg/*` "theo v1.3 mục 5, giữ nguyên", không nêu `mkg/engine` | v1.4 §5: "từ năm module mới còn một (`mkg/workforce`)"; repo đã có `mkg/engine` (W8.2) | Ghi nhận module thứ hai | Q-030 |
| 23 | v4.3↔repo (đánh số) | v4.3 ADR-21…24 (stack Go, hai hệ, MFA, chữ ký số) | Repo ADR-0021…0024 (K8s, Public API, metering, DLP) | Trùng số | Q-010 |
| 24 | MKG v1.4↔repo | v1.4 §7.7: sửa `ShareKinds`/CHECK cho loại chia sẻ tổng hợp | ADR-0035: `grant_class = aggregate` | Tương đương mục đích | Q-025 |
| 25 | MKG v1.4↔repo | v1.4 §4.5 lý do 3: masterdata dùng chung lộ chi phí MKG | Repo: `MasterdataParams.Approved(ctx, tid, …)` tra theo tenant | Lý do sai, kết luận D-1 vẫn đúng | — (ghi nhận) |
| 26 | MKG v1.4↔repo | v1.4 §7.2: `key`, `kind` (epistemic), `max_age` theo danh mục | W8.2: cột `kind` = key, không có epistemic kind, `max_age` theo hợp đồng feed, `unit` chỉ `ha`/`ratio` | Sửa lược đồ | Q-028, Q-029 |
| 27 | MKG v1.4↔repo | v1.4 §6: điểm cân thứ tư `CONSOLIDATION`, định mức hao hụt riêng cho lô đi thẳng | Repo: CHECK chỉ 3 điểm; CHECK cấm mọi trạng thái khác `not_applicable` cho DIRECT | DROP/ADD CHECK hay sổ v2 | Q-035 |
| 28 | MKG v1.4↔repo | v1.4 §5.1: bảng vận hành chỉ giữ `worker_ref`; vị trí người có thời hạn lưu, chỉ trong giờ làm | `crew_event.member_refs`, `driver_ref` = subject tài khoản; `roll_count_location` lưu GPS trong sổ không xóa được | Dữ liệu vị trí đang thu | Q-038, Q-039 |
| 29 | Tài liệu↔repo | `docs/design/mkg.md` §6 RB-3: định danh tài xế/nhân viên ở vault tenant MKG | v1.4 §5.1(2): schema định danh riêng trong cụm `mkg` | Sửa tài liệu (C-6) | — (W10.00) |
| 30 | Tài liệu↔repo | `docs/design/mkg.md` §5.8: engine = mkg 000038; chú thích 000037 dòng 19 ghi ngược | Thực tế engine = 000037, W5.6 = 000038 | Sửa tài liệu, không sửa migration (tổng kiểm) | — (W10.00) |
| 31 | Tài liệu↔repo | Plan W8.4/W8.5 giữ chỗ mkg 000027–000032 | Đã áp tới 000038; `golang-migrate` bỏ qua số nhỏ hơn | Cấp số mới | Q-011 |
| 32 | Tài liệu↔repo | v4.3 ghi thiết kế cũ: mật khẩu "10 ký tự, không trùng 5", vô hiệu 90 ngày | Repo: `length(12) … passwordHistory(5)`, không có vô hiệu | v4.3 mô tả hiện trạng sai | — (ghi chú cho kiến trúc sư) |
| 33 | Tài liệu↔repo | `cau-hoi-ton-dong.md` Q-1: TT200/TT133 | v4.3 §23: Thông tư 99/2025/TT-BTC hiệu lực 01/01/2026 | Cập nhật Q-1 | Q-015 |
| 34 | Tài liệu↔repo | `docs/uat/kn-v7.7-acceptance.md`: menu mobile còn thiếu; đường dẫn `party`/`geo` | Mã: `kn_menu_service.dart:145` đã gọi `/v1/menu`; dữ liệu đã sang `pf_coop` (W3.3); "Báo cáo đột xuất" rơi vào `ComingSoon` mà UAT không ghi | UAT cũ | — (W11.01) |
| 35 | Tài liệu↔repo | H-8 (`cau-hoi-ton-dong.md`): "MKG v1.3 chưa có trong repo" | Đã có v1.4 và v4.3, cả hai chưa vào `docs/` | Cập nhật H-8 | Q-012 |
| 36 | Tài liệu↔repo | ADR-0036 §3: chủ kho quan sát là `mkg/fieldops` hoặc module do v1.3 chỉ định | Repo: `mkg/engine` | Cập nhật ADR | Q-030 |

---

## Phụ lục

Bốn bản phân tích giữ nguyên nội dung. Chỉ hạ cấp tiêu đề để nằm dưới tiêu đề phụ lục. Đường dẫn `analysis/src/…` trong Phụ lục C, D là tệp trích tạm của phiên phân tích, không nằm trong repo.

---

## Phụ lục A — D1 — Đối chiếu Kiến trúc v4.3 với v4.1 và với repo

### D1 — Đối chiếu Kiến trúc v4.3 với v4.1 và với repo

**Ngày:** 09/10/2026 · **Nguồn:** `G:\Shared drives\Development\AgriGreen-Kien-truc-he-thong-v4_3.md` (v4.3, 06/10/2026, 544 dòng, đọc toàn văn) · **Repo:** `C:\Dev\claude-skills-main\Agrigreen_demo_v2` @ `27afd593` (dev = main) — chỉ đọc
**Đối chiếu với:** `docs/kien-truc/AgriGreen-Thiet-ke-kien-truc-he-thong-v4_1.md`, `docs/kien-truc-v4.1-doi-chieu.md`, `docs/kien-truc-v4.1-ke-hoach.md` (W0.1–W9.1), `docs/adr/0027…0038`, `docs/cau-hoi-ton-dong.md`, `docs/roadmap.md`, `docs/dev/modules.md`, và mã/cấu hình liên quan (realm Keycloak, `httpx.RequireMFA`, ADR-0008, runbook sao lưu).

Ký hiệu trạng thái: **Đạt** · **Một phần** · **Xung đột** (mâu thuẫn với điều đã hiện thực/đã chốt) · **Mới** (chưa có gì).

---

#### 0. Ghi chú đọc trước

1. **v4.3 không thay toàn bộ v4.1.** Đầu tài liệu (dòng 6–8): v4.3 hợp nhất và *thay phần tương ứng* của v4.1 và Thiết kế vùng nội bộ MKG v1.3; **v4.1 Phần III (dữ liệu) và Phần VI (tích hợp) còn hiệu lực làm đặc tả chi tiết**, cùng SAD định danh v1.1 và *Thiết kế vùng nội bộ v1.4* mục 2, 4, 7. Do đó toàn bộ W2.x–W6.x (dữ liệu, `k_ref`, chia sẻ, cổng tích hợp) **không bị v4.3 lật lại** — v4.3 chủ yếu thêm lớp pháp lý/an ninh và hai quyết định về hệ nội bộ.
2. **Trùng số ADR.** v4.3 Phần VII đánh số **ADR-21 … ADR-24** theo chuỗi ADR *trong tài liệu kiến trúc* (tiếp nối 20 ADR ở Phụ lục B của v4.1). Repo đã có **ADR-0021 (K8s G4), 0022 (Public API), 0023 (metering), 0024 (DLP)** — khác nghĩa hoàn toàn. v4.3 cũng dẫn "ADR-0020" (đúng là ADR-0020 của repo). Đề xuất cấp số repo ở mục 3 (ADR-0039…0045) và ghi bảng ánh xạ, giống cách đã làm với mã C-x ở `cau-hoi-ton-dong.md` §H.
3. **Tài liệu đi kèm vẫn chưa có trong repo:** SAD định danh v1.1 (sắp thành v1.2), BRD định danh v1.1 (→ v1.2), Thiết kế vùng nội bộ **v1.4** (v4.3 nói phát hành cùng bản này), bộ tài liệu thiết kế CSDL `docs/db/` (DB-02, DB-05) — `docs/db/` **không tồn tại** trong repo. Đây là phần mở rộng của H-8.
4. v4.3 **không đặt mã R-\* mới**; thay vào đó dùng mã phát hiện **RV-01…RV-21** (Phần 0), thay đổi nền tảng **P-1…P-11** (§4), việc phải làm **#1…#17** (§24). Chỉ có một R-* được nhắc là "R-ID mới cho MFA theo nhóm" (ADR-23 Việc 1) — chưa có số.

---

#### 1. Thay đổi v4.1 → v4.3, theo mục

##### 1.1 Phần 0 — Kết quả review (mới hoàn toàn)
21 phát hiện: 4 nghiêm trọng (RV-01 căn cứ ATTT hết hiệu lực; RV-02 MFA chưa phủ; RV-03 hồ sơ cấp độ có thể là điều kiện vận hành G1; RV-04 ranh giới hệ thống thông tin), 8 cao (RV-05…RV-11, RV-13), 6 trung bình (RV-14…RV-19), 3 thấp (RV-12 đã đóng — Odoo; RV-20 luồng song phương treo; RV-21 vùng mạng văn phòng/không dây). §0.2 "Điều không đổi": bốn vùng chủ quyền, lưu trong lãnh thổ, cách ly ở DB, sổ chuỗi băm (đáp TCVN 5.4.2.5), vault tách (5.4.2.7), Keycloak tập trung, nhật ký 13 tháng, không gọi đồng bộ xuyên hệ thống.

##### 1.2 Phần I — Bối cảnh (§1–2)
- **Mới:** mô hình **hai hệ thống thông tin** (Nền tảng — đề xuất cấp độ 3; Hệ nội bộ MKG — đề xuất cấp độ 2) nối qua cổng tích hợp **"+ chữ ký số"**; kế toán pháp định mua ngoài nhận bút toán tổng hợp theo kỳ (§1).
- **Mới:** vai trò dữ liệu của MKG: *bên xử lý* cho dữ liệu của đơn vị khác trên nền tảng; *bên kiểm soát* cho tài khoản nền tảng và cho hệ nội bộ (§1 bảng, chi tiết §21.1).
- **Không đổi:** RB-1…RB-7 (§2); thêm câu áp cho hệ nội bộ: giữ `tenant_id` + RLS dù một đơn vị.

##### 1.3 Phần II — Nền tảng (§3–4)
§3 tóm tắt v4.1 (không đổi). **§4 "Thay đổi do review" P-1…P-11 — tất cả mới:**
| Mã | Nội dung | Căn cứ |
|---|---|---|
| P-1 | MFA cho **mọi người dùng không phải nông hộ** truy cập từ Internet (cán bộ KN, HTX, Cục KTHT, đối tác). App hiện trường: thiết bị đã đăng ký + PIN mở khóa phần cứng = 2 yếu tố | ADR-23, TCVN 5.6.2.4 |
| P-2 | Nông hộ: Zalo/OTP, chỉ dữ liệu của chính hộ; "tài khoản dịch vụ — người thụ hưởng" + đánh giá rủi ro biện minh `[PHÁP CHẾ]` | ADR-23 |
| P-3 | Mật khẩu ≥ 14 ký tự có độ phức tạp khi chưa MFA; quản trị: MFA, đổi 2 tháng, không trùng 10 | TCVN 5.6.2.2 |
| P-4 | Vô hiệu sau **45 ngày** không hoạt động mọi loại tài khoản; nông hộ tự kích hoạt lại bằng OTP | 5.6.2.3 |
| P-5 | Khóa phiên: quản trị **5 phút**, Web nghiệp vụ **15 phút**, app di động khóa sau **5 phút** mở bằng PIN; ghi đánh giá rủi ro cho từng giá trị | 5.5.2.2 |
| P-6 | Tường lửa CSDL trước `pf`, `idp`, `vault` hoặc tương đương: PgBouncer chỉ nhận từ vùng ứng dụng + nhật ký truy vấn vai đặc quyền + cảnh báo truy vấn bất thường | 5.12.2.2 |
| P-7 | EDR trên máy chủ và máy quản trị, nối giám sát tập trung | 5.10.2.4 |
| P-8 | DLP chính thức: mọi đường xuất qua điểm kiểm soát (đã có ở v4.1 §30) — ghi thành phương án trong hồ sơ | 5.12.2.2 |
| P-9 | Chu kỳ 6 tháng: rà quét lỗ hổng, điều chỉnh ngưỡng cảnh báo, rà soát nhật ký, rà soát phân quyền dữ liệu; máy chủ quản lý bản vá tập trung; pentest theo chương trình | 5.7, 5.8, 5.13, 5.4.2.1 |
| P-10 | Kênh tiếp nhận báo cáo lỗ hổng từ bên ngoài | 5.17.2.2 |
| P-11 | Giới hạn kết nối đồng thời theo nguồn và theo dịch vụ, mở rộng sang bề mặt người dùng | 5.12.2.4 e |

##### 1.4 Phần III — Hệ nội bộ MKG (§5–11)
- **ADR-21 (Chấp nhận):** hệ nội bộ giữ **stack Go của nền tảng**, không Odoo (bản nháp v5.0 bị hủy). Kế toán pháp định mua ngoài **kèm kho phụ tùng, nhiên liệu** (§5, ADR-21).
- §5 thành phần: `api-internal` một khối chia module; **năng lực `cap/*` thể hiện nội bộ cùng mã** (workflow, masterdata, forms, export, analytics, import, task, notify, evidence, geo, sync) + lõi `k/ref, k/ledger, k/jobs, k/audit, k/ui-shell` (R-MOD-10 — đã có ở v4.1, nay vẽ cụ thể). Engine Python `lib/optcore` không giữ dữ liệu; ingest cân + GPS → `mkg-ts`; bộ điều hợp feed.
- §6 module `mkg/*` "theo v1.3 mục 5, giữ nguyên": `fieldops` (số cuộn, hỏng hóc — không tạo `mkg/equipment`), **`mkg/workforce`** (1.500+ nhân sự, tách vì dữ liệu cá nhân), `orders` (+ chiều mua), `logistics` (ghe thuê, ghép ghe), `measurement` (**bốn điểm cân** gồm `CONSOLIDATION`), `hub` (gia phả lô), `costing` (ghim `rule_version`), `acctbridge` (một chiều, tháng). Không liệt kê `mkg/engine` (repo đã có từ W8.2).
- §7 quy tắc dữ liệu v4.1 Phần III áp nguyên vẹn (schema/vai riêng, `SET LOCAL ROLE`, sổ cưỡng chế ở DB, `tenant_id`+RLS, không join `mkg`↔`mkg-ts`; `docs/db/` + hai công cụ kiểm).
- **§8 dữ liệu cá nhân nhân sự (mới, chi tiết hơn v4.1):** `worker_ref` bút danh; định danh nhân sự ở schema riêng trong cụm `mkg`, mã hóa mức trường, chỉ mục HMAC, xóa được; **ngân hàng/lương/bảo hiểm không vào hệ vận hành**; vị trí theo máy/đội nhưng máy gắn một người lái = dữ liệu vị trí của người → căn cứ xử lý, giới hạn mục đích, **chỉ thu trong giờ làm việc**; thời hạn lưu sau nghỉ việc do pháp chế.
- §9: Keycloak + Access dùng chung lớp định danh; MFA theo ADR-23; danh mục vai hệ nội bộ ở BRD định danh IV.C.5; **phân tách nhiệm vụ ở ba chỗ có tiền qua `cap/workflow`, ràng buộc ở DB**; đọc dữ liệu nhân sự cũng ghi nhật ký.
- §10: kiểm thử tải đồng bộ 1.500 người trước Phase K.
- **§11 hạ tầng (mới):** thêm **vùng mạng nội bộ** (văn phòng MKG) và **vùng không dây** (hub) — RV-21.

##### 1.5 Phần IV — Kết nối hai hệ (§12–15)
- §12: MKG là đối tác đầu tiên (giữ v4.1 §37). **Mới:** cổng tích hợp là **ranh giới giữa hai hệ thống thông tin theo NĐ 331/2026** — phải mô tả trong hồ sơ cấp độ của cả hai.
- §13 bảng luồng **thêm cột "Chữ ký số"**: bắt buộc cho *Số cuộn ghi nhận* (MKG→HTX), *Xác nhận số cuộn của HTX* (HTX→MKG), *Tồn và kế hoạch tiêu thụ VFT* (với nghiệm thu); "nên có" cho lịch gom; không bắt buộc cho feed tổng hợp/tham chiếu.
- §14 bộ điều hợp feed (khớp v4.1 §41.4).
- §15: luồng song phương vẫn "ADR còn treo" (= C-12/H-2); **thêm điều kiện: luồng song phương mang dữ liệu thương mại phải có chữ ký số** (ADR-24); quyết định "khoản thanh toán của hộ" đi cùng.

##### 1.6 Phần V — An ninh mạng theo cấp độ (§16–20, mới hoàn toàn; thay v4.1 §29–30 phần căn cứ)
- §16 căn cứ mới: **Luật An ninh mạng 116/2025/QH15** (hiệu lực 01/07/2026, thay Luật ATTTM 2015 và Luật ANM 2018); **NĐ 331/2026/NĐ-CP** (19/08/2026; 5 cấp độ, thủ tục hồ sơ, thay khung NĐ 85/2016); **TCVN 14423:2026** (24/07/2026; thay TCVN 14423:2025 và **TCVN 11930:2017** — chính là bảng "Nhóm TCVN 11930" ở v4.1 §30). Thẩm quyền cấp độ 3: đơn vị chuyên trách ANM của chủ quản thẩm định, chủ quản phê duyệt → **MKG phải có đơn vị chuyên trách ANM**.
- §17 xác định cấp độ: ngưỡng (theo bản tóm tắt, `[PHÁP CHẾ]`) cấp độ 2 khi < 100.000 chủ thể DLCN cơ bản **hoặc** < 10.000 chủ thể dữ liệu nhạy cảm. Ước: nền tảng Phase K ~8.000 hộ (cấp 2), Phase 1 ~110–140 nghìn hộ (cấp 3) → **khuyến nghị khai cấp độ 3 ngay**, cái giá: yêu cầu tổ chức (bộ phận độc lập, nghiệm thu độc lập, pentest) **phải có trước G1**. Hệ nội bộ cấp độ 2 + áp thêm MFA, nhật ký tập trung, EDR, 3-2-1.
- §18 bảng đối chiếu TCVN 14423:2026 mục 5.1–5.18 + Phụ lục A: **0/19 đạt trọn, 16 một phần, 3 chưa có** (5.9 trình duyệt/thư, 5.14 nhân sự, 5.18 pentest).
- §19 tổ chức tối thiểu: đơn vị chuyên trách ANM (MKG), bộ phận bảo vệ ANM **không thuộc ICS**, tách vận hành/quản trị hệ thống, đơn vị độc lập giám sát nghiệm thu, nhân sự bảo vệ DLCN (NĐ 356/2025), người giữ tài khoản khẩn cấp.
- §20 thủ tục 4 bước; **RV-03**: nếu G1 có dữ liệu thật = "đưa vào sử dụng" thì 4 bước phải xong trước **15/12/2026**; phương án (a) G1 chạy dữ liệu tổng hợp/phạm vi người dùng hạn chế có thỏa thuận, (b) lùi mốc dữ liệu thật.

##### 1.7 Phần VI — Luật về dữ liệu (§21–23, mới hoàn toàn)
- §21 **Luật BVDLCN 91/2025/QH15** (hiệu lực 01/01/2026) + **NĐ 356/2025** (thay NĐ 13/2023).
  - §21.1 vai trò (đề xuất, chờ pháp chế): MKG **kiểm soát** tài khoản nền tảng, Kênh nông hộ, nhân sự MKG; **HTX là bên kiểm soát dữ liệu nông hộ/xã viên/thửa do HTX nhập**, MKG là bên xử lý (cần hợp đồng xử lý với từng HTX); **cơ quan khuyến nông là bên kiểm soát dữ liệu khuyến nông**. Hệ quả: MKG **không được dùng dữ liệu HTX cho mục đích của MKG, kể cả engine**, ngoài phạm vi HTX cho phép → dữ liệu tới engine chỉ ở **dạng tổng hợp theo xã qua ngưỡng k** (căn cứ thứ hai bên cạnh ADR-0020).
  - §21.2 nghĩa vụ: hồ sơ đánh giá tác động (2 hồ sơ — Chưa có), nhân sự bảo vệ DLCN (Chưa có), đồng ý có phiên bản (Đạt — áp tương tự cho nhân sự), quyền chủ thể (Một phần — FN-ID-08), xóa được (Đạt), thông báo vi phạm (RV-18 — thời hạn chưa chốt), dữ liệu nhạy cảm theo NĐ 356 gồm "theo dõi hành vi trên không gian mạng" (nhật ký hành vi có thể thuộc), dữ liệu vị trí (RV-14), chuyển ra nước ngoài (RV-15: MRV, phòng dữ liệu VFT, SaaS nước ngoài trong CI/theo dõi lỗi).
- §22 **Luật Dữ liệu 60/2024/QH15**: dữ liệu quan trọng/cốt lõi (danh mục Thủ tướng) — RV-17; chuyển xuyên biên giới kể cả dữ liệu không cá nhân; dữ liệu của cơ quan nhà nước — RV-16 chủ quản App KN (MKG hay TTKNQG).
- §23 văn bản khác: pháp luật kế toán (thời hạn lưu chứng từ), hóa đơn điện tử, **giao dịch điện tử** (giá trị pháp lý xác nhận số cuộn, biên bản nghiệm thu → ADR-24), **Thông tư 99/2025/TT-BTC** (chế độ kế toán DN mới, hiệu lực 01/01/2026 — tiêu chí chọn phần mềm kế toán).

##### 1.8 Phần VII — ADR mới
| ADR v4.3 | Trạng thái | Nội dung |
|---|---|---|
| ADR-21 | **Chấp nhận** (CEO, 06/10/2026) | Hệ nội bộ dùng stack Go của nền tảng, không Odoo CE/EE; kế toán + kho phụ tùng/nhiên liệu mua ngoài. Việc: kế hoạch năng lực Go + **người review mã thứ hai**; chọn phần mềm kế toán (TT 99, HĐĐT, lương mùa vụ, kho phụ tùng); đóng mục Odoo của v5.0 |
| ADR-22 | Đề xuất | **Hai hệ thống thông tin** (3 và 2), ranh giới là cổng tích hợp; chỉ đúng khi tách biệt thật: không kết nối DB, **không dùng chung tài khoản quản trị**, phân vùng mạng riêng, chỉ nói chuyện qua cổng; lớp định danh dùng chung phải khai là một kết nối giữa hai hệ, tài khoản quản trị riêng từng hệ |
| ADR-23 | Đề xuất | MFA theo nhóm (bảng: quản trị passkey/TOTP; app hiện trường = thiết bị đăng ký + PIN; Web = mật khẩu + passkey/TOTP/**xác nhận đẩy**; đối tác người như Web; đối tác máy = khóa API + **mTLS**; nông hộ Zalo/OTP + đánh giá rủi ro, nếu bị từ chối thì Zalo-trên-thiết-bị + OTP) |
| ADR-24 | Đề xuất | **Chữ ký số của tổ chức gửi trên từng lô** cho luồng song phương và nghiệm thu, chứng thư của CA hợp pháp tại VN; HMAC giữ cho xác thực kỹ thuật; luồng tổng hợp/tham chiếu không bắt buộc; HTX nhỏ chưa có chứng thư → **nền tảng ký thay có ủy quyền** `[PHÁP CHẾ]` |

##### 1.9 Phần VIII — Việc phải làm (§24–25) và Phụ lục
17 việc có hạn (tuần này: #1 khái niệm "vận hành chính thức", #2 quyết ADR-22; 2 tuần: #3 đơn vị chuyên trách + nhân sự DLCN; 4 tuần: #4 hồ sơ cấp độ, #5 DPIA, #6 vai trò kiểm soát/xử lý + hợp đồng mẫu, #9 đơn vị giám sát nghiệm thu, #10 điều khoản nộp mã nguồn ICS, #12 thời hạn báo vi phạm, #15 năng lực Go/kế toán; trước G1: #7 ADR-23 + P-1…P-5, #8 pentest, #11 P-6…P-11 hoặc lộ trình trong hồ sơ; G2: #16 ADR-24; trước Phase K: #17 hồ sơ hệ nội bộ). §25 tài liệu phải cập nhật: v4.1 §29–30 (căn cứ), SAD/BRD định danh → v1.2, vùng nội bộ → v1.4, DB-02 (ánh xạ 4 mức TCVN), DB-05 (3-2-1). Phụ lục ghi độ tin cậy nguồn pháp lý (NĐ 331 và NĐ 356 chỉ "trung bình").

---

#### 2. Tác động lên phần đã xây

##### 2.1 Bảng tổng hợp theo thay đổi

| # | Thay đổi v4.3 | Trạng thái repo | Bằng chứng / ghi chú |
|---|---|---|---|
| 1 | RV-01 căn cứ pháp lý mới | **Một phần (tài liệu lỗi thời)** | Chỉ 4 chỗ dẫn luật cũ: `docs/kien-truc/…v4_1.md` §30 ("Nhóm TCVN 11930"), `docs/kien-truc-v4.1-doi-chieu.md` (1), `docs/brd/kn-trong-trot-v7.7.md` (2). Không có trong mã. Bản v4.1 gốc giữ nguyên từng byte (W0.4) → không sửa; cần đưa v4.3 vào `docs/kien-truc/` + ghi chú thay thế |
| 2 | ADR-21 stack Go, không Odoo | **Đạt** | Toàn bộ `internal/mkg/*` (8 module: acctbridge, costing, engine, fieldops, hub, logistics, measurement, orders) là Go, cùng `modreg`/`module.yaml`/vai DB (W1.1, W2.6). Không có gì phải gỡ. Câu hỏi Q-1 (`cau-hoi-ton-dong.md` §D) còn nhắc "TT200/TT133" — phải thêm **TT 99/2025** |
| 3 | ADR-22 hai hệ thống, tách biệt thật | **Một phần / Xung đột tạm thời** | Đã đúng: cụm DB riêng `pg-pf`/`pg-mkg`/`pg-ts`, không đường pf→DB mkg (ADR-0008 bảng luồng F10–F18), namespace `agrigreen-pf` vs `agrigreen-mkg`, cổng tích hợp W5.2–W5.8, MKG đọc feed W5.5, song phương W5.6. **Chưa đạt "không dùng chung hàng đợi, chỉ qua cổng"**: (a) van RabbitMQ — tài khoản mkg được publish lên `ag.events` trên vhost `/` (`deploy/k8s/tests/check_flows.py` dòng 540–547), consumer `htx_collection_valve`, `hub_valve`, `mrv` vẫn chạy; W5.10 chỉ gỡ `htx_collection_valve` + `/v1/partner/*` + `mkg_sync`, **giữ `hub_valve`, `mrv`**; (b) cùng một RabbitMQ, cùng namespace dữ liệu `agrigreen-data`, cùng cụm K8s, cùng `agrigreen-ops` (Argo CD, Prometheus, Grafana), cùng CI; (c) cùng realm Keycloak `agrigreen` cho cả client nền tảng và `web-internal`/`mkg-field-app` — ADR-22 cho phép dùng chung lớp định danh nhưng **đòi tài khoản quản trị riêng từng hệ** — realm hiện có vai quản trị chung (`platform_admin` …). Cần quyết hạ tầng dùng chung nào được khai là "kết nối" và cái nào phải tách |
| 4 | ADR-23 / P-1 MFA cho mọi người dùng không phải nông hộ | **Xung đột với cấu hình đã chốt** | Hiện tại: MFA (TOTP) chỉ cho vai `[MFA]`, chỉ cưỡng chế ở Admin API (`internal/shared/httpx/httpx.go:155` `RequireMFA`; `agrigreen-realm.json` note; ADR-0010). Application API (cán bộ KN, HTX) không đòi MFA. App hiện trường: có sổ thiết bị + thu hồi (`internal/platform/sync/app/devices.go`), PIN 6 số cho ngoại tuyến (realm `session_note`) — **chưa xác nhận khóa gắn vùng bảo mật phần cứng**. Không có passkey/WebAuthn, không có xác nhận đẩy (Keycloak không có sẵn — câu Q3 cũ của SAD). Đối tác máy: HMAC + mTLS tùy chọn (`AG_GATEWAY_MTLS_ADDR`, W5.2) — **khớp** bảng ADR-23 nếu bật `require_mtls` cho mọi đối tác |
| 5 | P-2 nông hộ Zalo/OTP + tự kích hoạt lại | **Một phần** | Kênh nông hộ theo ADR-0010 (Zalo Mini App online); không thấy luồng OTP SMS trong `internal/` (OTP chỉ xuất hiện dạng TOTP). Đánh giá rủi ro: Mới |
| 6 | P-3 mật khẩu 14 ký tự / quản trị đổi 2 tháng, không trùng 10 | **Xung đột** | Realm: `"passwordPolicy": "length(12) and notUsername and passwordHistory(5)"` (v4.3 ghi "10 ký tự, không trùng 5" — cũng lệch với repo). `cmd/api-platform/realm_test.go` khóa giá trị realm. Keycloak đặt chính sách mật khẩu **theo realm**, không theo vai → "quản trị đổi 2 tháng, không trùng 10" cần realm riêng cho quản trị hoặc cùng chính sách cho mọi người. Quyết định phải đi cùng ADR-22 (tài khoản quản trị riêng từng hệ) |
| 7 | P-4 vô hiệu sau 45 ngày | **Mới** | Không có job vô hiệu tài khoản không hoạt động trong `pf/accounts`; v4.3 ghi thiết kế cũ 90 ngày, repo không có cả 90 ngày. Keycloak không có sẵn → cần job dựa sự kiện đăng nhập |
| 8 | P-5 khóa phiên 5/15/5 phút | **Xung đột với quyết định KN v7.7 đã hiện thực** | Realm `ssoSessionIdleTimeout 14400` (4 giờ), client `agrigreen-web-ops` idle 14400, field app 24 giờ + ngoại tuyến 72 giờ — theo BRD KN v7.7 D9 (`docs/brd/kn-v7.7-gap.md:20`, `kn-p1-realm-patch.md:11`) và kiểm thử `realm_test.go`. v4.3 cho phép giữ giá trị khác **nếu có đánh giá rủi ro biện minh** — cần PO + khách KN quyết. App di động khóa 5 phút mở bằng PIN: Mới ở Flutter (`packages/agrigreen_mobile`) |
| 9 | P-6 tường lửa CSDL / tương đương | **Một phần** | NetworkPolicy chỉ cho vùng ứng dụng tới PgBouncer (ADR-0008, `check_flows.py`); vai đăng nhập theo đơn vị (W2.6) giúp quy trách. **Chưa có** nhật ký truy vấn vai đặc quyền (không có `pgaudit`), chưa cảnh báo truy vấn bất thường. Cụm `idp` chưa tồn tại (W7.4) |
| 10 | P-7 EDR | **Mới** (hạ tầng, chờ G-1) | Không có trong chart/IaC |
| 11 | P-8 DLP chính thức | **Phần lớn đạt — cần văn bản hóa + kiểm phủ** | ADR-0024, `pf/metering` `egress_event`, cảnh báo `pf.metering.egress.anomaly_detected`; cổng W5.2 giữ DLP; kênh tệp W5.8 đo trước byte. Cần kiểm phủ cho feed W5.3/W5.5, webhook W5.7 (chỉ cursor), `r_agg` W6.4 (có `agg_run` nhưng không qua `egress_event`), ADR-0026 xuất Google Sheets |
| 12 | P-9 chu kỳ 6 tháng | **Một phần** | Quét dependency/image/SBOM trong CI; rà soát cấp phép 90 ngày (W4.1). Chưa: quét hạ tầng, quản lý bản vá tập trung, lịch rà soát nhật ký/ngưỡng/phân quyền có biên bản. **A8: CI GitHub Actions đang lỗi thanh toán** → bằng chứng CI cho hồ sơ không có |
| 13 | P-10 kênh báo lỗ hổng | **Mới** | Không có `security.txt` hay quy trình |
| 14 | P-11 giới hạn kết nối theo nguồn | **Một phần** | Hạn mức theo đối tác (ADR-0023, W5.4 `429`), `httpx.RateLimitByIP` chỉ cho route công khai không đăng nhập |
| 15 | RV-19 sao lưu 3-2-1 | **Một phần** | `docs/runbooks/backup.md`: pgBackRest S3 trong lãnh thổ có object lock, sao chép sang vùng thứ hai, credential tách. Chưa có **loại phương tiện thứ hai**. Lưu ý H-13: RPO repo 24 h vs v4.1 1 h vẫn chưa chốt |
| 16 | RV-21 / §11 vùng mạng văn phòng + không dây | **Mới (tài liệu/hạ tầng)** | ADR-0008 chỉ có các vùng trong cụm K8s + biên OT |
| 17 | RV-08 / ADR-24 chữ ký số cho song phương và nghiệm thu | **Mới — chạm phần đã xây** | W5.6 (`pf_coop.bilateral_record` có `payload_sha256`, HMAC khóa API, tổng kiểm `bilateral_day.v1`, `contracts/integration/bilateral.go`) **không có chữ ký số**. Ed25519 hiện có (`internal/shared/opsjobs/anchor.go` ký manifest neo; ADR-0019 ký tại nguồn cho thiết bị cân) là chữ ký kỹ thuật, **không phải chữ ký số theo pháp luật giao dịch điện tử**. Bài học W4.1: **không thêm cột vào sổ đã có chuỗi băm** (`to_jsonb` toàn dòng) → chữ ký phải nằm ở sổ phụ. Luồng nghiệm thu VFT → MKG chưa có cổng nhận ở vùng nội bộ (v4.1 §38.3, R-MOD-10) |
| 18 | §13 bảng luồng — "Xác nhận số cuộn HTX → MKG: Cổng → `mkg/fieldops`" | **Đạt về đường đi** | W5.6: HTX xác nhận qua `/v1/bilateral/*` (kênh app), MKG kéo feed cặp bằng job `fieldops_bilateral`. Kênh api/file cho HTX chờ W7.2 |
| 19 | §21.1 HTX là bên kiểm soát dữ liệu hộ/thửa | **Xung đột với giả định đã hiện thực (H-1 a)** | ADR-0009: tenant chương trình khuyến nông là chủ; W3.1 ghi giả định **H-1 (a)** (`docs/design/cooperative.md` §11 mô tả phần đổi nếu (b)). v4.3 §21.1 thực chất chọn **(b)**. Cơ chế W3.3/W3.4/W4.1/W4.2 chạy được cả hai (ADR-0030 §7), nhưng đổi chủ **sau khi có dữ liệu thật phải làm bằng bản ghi bù** → phải quyết trước 15/12/2026. Ngoài ra "dữ liệu khuyến nông — cơ quan khuyến nông kiểm soát" kéo theo hợp đồng xử lý với TTKNQG (RV-16) |
| 20 | §21.1 dữ liệu tới engine chỉ dạng tổng hợp theo xã qua k | **Xung đột với W8.2** | W8.2 (`mkg/engine`, mkg 000037) có bộ ánh xạ feed `cooperative_directory`, **`plot_directory`** (thửa theo HTX) vào kho quan sát của engine. Theo v4.3, dữ liệu mức thửa của HTX chỉ được dùng cho engine nếu cấp phép của HTX cho phép mục đích đó. Phần tổng hợp (`harvest_progress_by_commune`, `collection_volume_by_commune` — ADR-0035, W5.3) **khớp**. W5.5 dùng `plot_directory` cho lập lệnh gom (`mkg/fieldops`) — hợp lệ nếu `purpose_code`/`basis_ref` của cấp phép (W4.1) ghi mục đích này |
| 21 | §8 dữ liệu nhân sự, vị trí người lái | **Mới (bị chặn)** | `mkg/workforce` = W8.4 (chặn: v1.3, pháp chế). Quy tắc "chỉ thu GPS trong giờ làm việc" chạm ingest/`mkg-ts` (ADR-0017, W8.5) và cần dữ liệu ca của `mkg/workforce` |
| 22 | §9 phân tách nhiệm vụ ba chỗ có tiền qua `cap/workflow` | **Một phần** | `pf/workflow` (W6.1, pf 000118) chỉ `zones: [pf]`; trong repo chỉ `pf/kref` có `zones: [pf, mkg]`. Hệ nội bộ chưa có thể hiện `cap/workflow` ở cụm `mkg`; `doi-chieu` R-MOD-10 = LN "chưa có `cap/*` hai thể hiện" |
| 23 | §5 `cap/*` thể hiện nội bộ (11 năng lực) | **Một phần** | Như trên; W6.6 quyết không tách `cap/import/task/export` (khai `hosts`). Cần gói riêng khi bật vùng nội bộ |
| 24 | §6 bốn điểm cân, ghe thuê, mua từ HTX… | **Không đổi so với v4.1** | Vẫn W8.5 (chặn v1.3 → nay v1.4) |
| 25 | §17 nền tảng cấp độ 3 ngay; yêu cầu tổ chức trước G1 | **Mới — ảnh hưởng lịch** | Kế hoạch v4.1 §1 nguyên tắc 5 xếp mọi việc theo "KN nhận dữ liệu thật 15/12/2026"; đường găng W0.1→…→W3.4 đã xong mã, nhưng **RV-03 có thể chặn việc nhận dữ liệu thật** dù mã xong. W3.5 (hợp nhất dữ liệu UAT thật) và H-17 (cửa sổ bảo trì W3.3) chịu ảnh hưởng trực tiếp |
| 26 | §18 5.4 phân loại dữ liệu 4 mức TCVN; DB-02 | **Mới (tài liệu chưa có)** | Không có `docs/db/`, không có thang phân loại trong repo (các khớp "DB-02" trong repo là R-DB-02 — vai DB) |
| 27 | §18 5.8 nhật ký ≥ 3 tháng, rà soát 6 tháng | **Một phần** | G-6 (nhật ký 1 năm, tầng lạnh) còn mở; A10/H-5 Grafana/Loki AGPL chưa quyết |
| 28 | §18 5.17 phát triển an toàn; không bí mật trong mã; kiểm thư viện | **Phần lớn đạt** | Cổng CI: SAST, quét bí mật, SBOM, `allowed-licenses` (RB-4), `archlint`, `migratecheck`, W9.1. Thiếu: điều khoản nộp mã nguồn ICS (hợp đồng) |
| 29 | §18 5.16 ứng phó sự cố; RV-18 thời hạn báo vi phạm | **Một phần** | K-10 (kế hoạch §4) "Hai thời hạn báo cáo sự cố — chưa có trong runbook" — giữ nguyên, nay có căn cứ mới (NĐ 356) |
| 30 | RV-15 chuyển dữ liệu ra nước ngoài | **Một phần — thêm đường v4.3 chưa liệt kê** | Ngoài 3 đường v4.3 nêu (MRV, phòng dữ liệu VFT, SaaS CI/theo dõi lỗi), repo còn: **ADR-0026 xuất Google Sheets** (Q-14 — số liệu KN ra ngoài lãnh thổ), **FCM/Firebase** (F-7, ADR-0015), **GitHub Actions** (A8, CI). Cần đưa vào rà soát |
| 31 | Vai DB, `k_ref`, `pf_coop`, `share_grant`, cổng, `cap/*`, runner, cổng CI (W2.x–W9.1) | **Đạt — không xung đột trực tiếp** | v4.3 §3, §7 xác nhận v4.1 Phần III/VI còn hiệu lực. Cổng W9.1 (cách ly schema, `EXPLAIN` RLS) và W7.1 (migration theo module) là **bằng chứng tốt** cho hồ sơ 5.4/5.17. W4.1 rà soát cấp phép 90 ngày **chặt hơn** yêu cầu 6 tháng của 5.4.2.1 |

##### 2.2 Xung đột với gói đã xong — theo gói

| Gói | Mức | Xung đột / tác động |
|---|---|---|
| **W2.x** (vai DB, `k_ref`) | Không | Khớp §7. `k_ref` ở cụm `mkg` (mkg 000022) nạp qua cổng — đúng nguyên tắc ADR-22. Lưu ý: W2.6 gộp vai `ingest_mkg/ingest_ts/ts_reader` vào `r_measurement` (mất tách đọc/ghi ts) — nên xem lại khi lập hồ sơ cấp độ hệ nội bộ |
| **W3.x** (`pf_coop`) | Trung bình | Giả định H-1 (a) trong W3.1 ngược với §21.1 (HTX kiểm soát). Cơ chế đổi được; dữ liệu thì phải đổi trước 15/12/2026 |
| **W4.x** (`share_grant` v2, đọc tại chỗ) | Thấp | Khớp và có giá trị cho §21.1 (cấp phép = "phạm vi HTX cho phép"). Cần: `purpose_code` có danh mục mục đích khớp DPIA; `basis_ref` trỏ hợp đồng xử lý dữ liệu (không chỉ thỏa thuận chia sẻ C-10) |
| **W5.x** (cổng tích hợp) | Trung bình | (1) Thiếu chữ ký số trên lô song phương (ADR-24) — W5.4/W5.6/W5.8 cần mở rộng phong bì `ingest_batch.v1` và sổ; (2) mTLS đang **tùy chọn** — ADR-23 đòi khóa API + mTLS cho đối tác máy; (3) W5.10 giữ van `hub_valve`, `mrv` qua RabbitMQ — trái điều kiện tách của ADR-22; (4) cổng nhận VFT ở vùng nội bộ (cùng mã) chưa có |
| **W6.x** (`cap/*`) | Thấp | `cap/workflow` chưa có thể hiện ở cụm `mkg` (§9). W6.4 `r_agg` cần đưa vào phương án DLP P-8 |
| **W7.1** (bộ chạy migration) | Không | Không liên quan; là bằng chứng 5.17 |
| **W7.2–W7.4** (chưa làm, chặn SAD v1.1) | Phạm vi tăng | SAD sẽ thành **v1.2** (MFA theo nhóm, mật khẩu, 45 ngày, khóa phiên). W7.2 nên chờ v1.2, không v1.1 |
| **W8.2** (kho quan sát engine) | **Cao** | Ánh xạ `plot_directory`/`cooperative_directory` vào engine trái §21.1 nếu không có cấp phép mục đích "engine" của HTX |
| **W8.3–W8.5** | Phạm vi tăng | Chặn bởi v1.3 → nay **v1.4**; W8.4 thêm quy tắc §8 (không ngân hàng/lương/bảo hiểm; vị trí người lái chỉ trong giờ làm việc; đọc dữ liệu nhân sự ghi nhật ký) |
| **W9.1** (cổng CI) | Không | Bằng chứng tốt; có thể thêm cổng realm (MFA/mật khẩu/phiên) theo ADR-23 |
| **KN P1 realm (A3, D9)** | **Cao** | Phiên 4 giờ/24 giờ, mật khẩu 12/5, MFA chỉ vai `[MFA]` — đều đã chốt với khách KN và khóa bằng `realm_test.go`; v4.3 đòi 15 phút/5 phút, 14 ký tự, MFA cho cán bộ |

---

#### 3. Gói việc đề xuất (W10.x) và ADR

Quy ước như kế hoạch v4.1: một gói một agent, tuần tự; **Khách?** = Không / Sau (làm mã được, bật production chờ) / Chặn. Số migration chưa cấp — cấp theo thứ tự gộp (kế hoạch §1 mục 6; sau W7.1 v2 là mặc định thì quy tắc số nhỏ bị bỏ qua hết hiệu lực).

##### Giai đoạn 10 — Tuân thủ an ninh mạng và dữ liệu (v4.3)

| Gói | Nội dung | Phụ thuộc | CS | Khách? |
|---|---|---|---|---|
| **W10.0** | **Tài liệu:** đưa v4.3 (md, giữ nguyên byte) vào `docs/kien-truc/` cạnh v4.1 + README ghi phạm vi thay thế; `tools/docsql` không đổi (v4.3 không có khối SQL); bảng ánh xạ ADR-21…24 ↔ ADR-0039…0042; bản đối chiếu `docs/kien-truc-v4.3-doi-chieu.md` (từ tài liệu này); thêm mục **I** vào `cau-hoi-ton-dong.md`; sửa Q-1 thêm TT 99/2025; cập nhật `roadmap.md` (G1 có điều kiện RV-03). Viết **ADR-0039** (=ADR-21, Chấp nhận — chỉ ghi lại) | — | S | Không |
| **W10.1** | **ADR-0040 (=ADR-22) + kiểm kê tách biệt hai hệ:** liệt kê mọi điểm dùng chung (RabbitMQ/vhost, namespace `agrigreen-data`, `agrigreen-ops`, Argo CD, realm Keycloak, CI, kho đối tượng, Valkey) → mỗi điểm: "kết nối được khai" hoặc "phải tách"; `check_flows.py` thêm khẳng định "không luồng pf↔mkg ngoài cổng" sau W5.10/W10.2 | W10.0 | M | **Chặn** (#2: CEO + đơn vị chuyên trách + pháp chế) |
| **W10.2** | **Gỡ van RabbitMQ còn lại** (`hub_valve`, `mrv`) sang hợp đồng nạp của cổng (mở rộng W5.10) để không còn hàng đợi dùng chung giữa hai hệ | W5.10, W10.1 | M | Sau (hợp đồng `hub_facility`, MRV) |
| **W10.3** | **ADR-0041 (=ADR-23) MFA theo nhóm — phần Keycloak/Go:** luồng xác thực có điều kiện cho client Web của cán bộ/đối tác người (mật khẩu + TOTP/WebAuthn passkey); `RequireMFA` mở rộng sang Application API cho mọi principal không phải nông hộ (theo loại tài khoản, không theo vai); claim `amr`/`acr` vào `authz.Principal`; `realm_test.go` theo chính sách mới; trang tự đăng ký passkey. Xác nhận đẩy: chỉ khi kiểm chứng được (SPI Keycloak — câu Q3 SAD) | W10.0; SAD v1.2 | L | **Sau** (SAD v1.2; khách KN đồng ý — xung đột D9) |
| **W10.4** | **Thiết bị đăng ký + PIN là hai yếu tố:** khóa ký thiết bị trong Android Keystore/StrongBox (khóa không xuất được), chứng thực khóa khi đăng ký (`pf/sync` devices), PIN mở khóa khóa phần cứng; token app mang `amr` = {hwk, pin}; khóa app sau 5 phút không thao tác (P-5) ở `packages/agrigreen_mobile` | W10.3 | M | Sau (G-5 máy Android giá rẻ) |
| **W10.5** | **Mật khẩu, phiên, vô hiệu tài khoản (P-3, P-4, P-5):** chính sách mật khẩu 14 ký tự + độ phức tạp, `passwordHistory(10)`, quản trị đổi 60 ngày (realm quản trị riêng hoặc chính sách chung — theo W10.1); job `accounts_dormant` vô hiệu sau 45 ngày (sổ `account_event`, lý do `dormant_45d`), báo trước; nông hộ tự kích hoạt lại bằng OTP; giá trị phiên là **tham số có phiên bản + bản đánh giá rủi ro** (không mã cứng) | W10.3 | M | **Sau** (khách KN về phiên 4 h; pháp chế) |
| **W10.6** | **OTP SMS cho nông hộ** qua nhà cung cấp trong lãnh thổ (ADR-0007/0015), dùng cho đăng nhập Kênh nông hộ và kích hoạt lại; đánh giá rủi ro nhóm nông hộ (tài liệu) | W10.5 | M | Sau (nhà cung cấp SMS — Q-03, G-4) |
| **W10.7** | **ADR-0042 (=ADR-24) chữ ký số lô song phương:** sổ phụ `pf_coop.bilateral_signature` (không thêm cột vào `bilateral_record` — bài học W4.1), phong bì `ingest_batch.v1` thêm `signature` (CAdES/PKCS#7 tách rời trên băm chuẩn hóa của lô), kiểm chứng thư theo danh sách CA hợp pháp + OCSP/CRL, lưu bằng chứng kiểm; chế độ **nền tảng ký thay có ủy quyền** cho HTX chưa có chứng thư (sổ ủy quyền); `mkg/fieldops` ký lô gửi; HSM/ký từ xa qua nhà cung cấp trong nước | W5.6, W10.0 | L | **Chặn** (#16: pháp chế — ủy quyền ký thay, chọn CA; kế toán trưởng; C-12) |
| **W10.8** | **Cổng nhận VFT ở vùng nội bộ** (cùng mã `cmd/integration-gateway`, triển khai và dữ liệu riêng — R-MOD-10) + hợp đồng tồn/kế hoạch tiêu thụ/nghiệm thu có chữ ký số bắt buộc | W10.7, W8.5 (`mkg/orders`) | L | Chặn (v1.4, VFT) |
| **W10.9** | **Tường lửa CSDL tương đương (P-6):** `pgaudit` cho vai đặc quyền (`ag_migrator`, `own_*`, `r_agg`, vai quản trị), luồng nhật ký vào giám sát tập trung, luật cảnh báo truy vấn bất thường (khối lượng, giờ, vai); khẳng định NetworkPolicy "chỉ vùng ứng dụng tới PgBouncer" trong `check_flows.py` cho cả `vault`/`idp` | W2.6 | M | Không (mã); Sau (G-1 hạ tầng) |
| **W10.10** | **Phương án DLP chính thức (P-8):** văn bản liệt kê mọi đường xuất (cổng feed/lô/tệp/webhook, `cap/export`, MRV, `r_agg`, Google Sheets ADR-0026, liên kết Chi cục) → đều ghi `egress_event`; bổ sung chỗ thiếu (`r_agg` W6.4, feed W5.3) + kiểm thử phủ | W10.0 | M | Không |
| **W10.11** | **Chu kỳ và kênh (P-9, P-10, P-11):** `/.well-known/security.txt` + quy trình tiếp nhận; lịch rà soát 6 tháng có biên bản (nhật ký, ngưỡng, phân quyền — tận dụng màn rà soát W4.1); giới hạn kết nối đồng thời theo nguồn/dịch vụ cho bề mặt người dùng (`httpx`) | W10.0 | S | Không |
| **W10.12** | **Hạ tầng cấp độ 3 (P-7, RV-19, RV-21, 5.13):** EDR, quản lý bản vá tập trung, nhật ký luồng mạng, bản sao lưu thứ ba trên **phương tiện khác loại**, vùng mạng văn phòng + không dây vào ADR-0008 | W10.1 | L | **Chặn** (G-1 nhà cung cấp, G-2 staging) |
| **W10.13** | **Phân loại dữ liệu 4 mức TCVN (DB-02) + nhãn trong `module.yaml`:** trường `data_class` cho mỗi bảng/`shares`, cổng `modgen -check` buộc có nhãn; điền danh mục `personal_sensitive` theo NĐ 356 (gồm nhật ký hành vi); ánh xạ vào hồ sơ | W10.0 | M | Sau (pháp chế — danh mục nhạy cảm; tài liệu DB-02) |
| **W10.14** | **Vai trò kiểm soát/xử lý trong dữ liệu (RV-13):** nếu chọn H-1 (b) — đổi chủ dữ liệu HTX/hộ/thửa trước 15/12 (theo `docs/design/cooperative.md` §11); `share_grant` `basis_ref` → hợp đồng xử lý; quyền chủ thể với dữ liệu MKG là bên xử lý → chuyển tới HTX (FN-ID-08) | W3.3, W4.1 | M–L | **Chặn** (#6 pháp chế, H-1) |
| **W10.15** | **Sửa W8.2 theo §21.1:** chặn feed mức thửa/danh bạ HTX vào kho quan sát engine trừ khi cấp phép có `purpose_code` engine; kiểm thử "engine chỉ nhận tổng hợp qua k" | W8.2, W4.1 | S | Sau (quyết định PO/pháp chế) |
| **W10.16** | **Thông báo vi phạm (RV-18, K-10):** runbook P1 hai nghĩa vụ, thời hạn, đầu mối (cơ quan chuyên trách ANM, A05, Liên minh ứng phó sự cố quốc gia), mẫu thông báo chủ thể; diễn tập có biên bản | W10.0 | S | **Chặn** (#12 pháp chế) |
| **W10.17** | **`cap/workflow` thể hiện ở cụm `mkg`** (`zones: [pf, mkg]`, schema `mkg_workflow`, vai `r_workflow` cụm mkg) cho ba chỗ có tiền (§9); kiểm thử bốn mắt ở DB | W6.1 | M | Không (mã); dùng thật chờ W8.5 |
| **W10.18** | **Rà dữ liệu ra nước ngoài (RV-15):** kiểm kê MRV package, phòng dữ liệu VFT, CI GitHub, FCM, Google Sheets, theo dõi lỗi; mặc định chỉ tổng hợp không định danh; cổng CI cấm đích ngoài danh mục RB-7 trong cấu hình egress | W10.10 | S | Sau (#13, #14 pháp chế) |
| **W10.19** | **Gói bằng chứng hồ sơ cấp độ:** sinh tự động từ repo (danh mục tài sản phần mềm từ SBOM + `module.yaml` có chủ quản/hạn hỗ trợ; ma trận luồng `check_flows.py`; danh sách cổng CI W9.1; kết quả diễn tập) để ICS lập hồ sơ NĐ 331 | W10.0, A8 (CI chạy lại) | M | Sau (mẫu hồ sơ NĐ 331) |

**Bổ sung các gói cũ:**
- **W5.10** — mở rộng phạm vi bằng W10.2 (van `hub_valve`, `mrv`).
- **W5.2** — đổi mTLS từ tùy chọn sang **bắt buộc cho đối tác máy** khi ADR-0041 được ký (một dòng cấu hình + kiểm thử).
- **W7.2–W7.3** — phụ thuộc đổi từ SAD v1.1 sang **SAD v1.2**.
- **W8.3–W8.5** — phụ thuộc đổi từ v1.3 sang **vùng nội bộ v1.4**; W8.4 thêm quy tắc §8.
- **W3.5** — ghi điều kiện RV-03: chỉ nạp dữ liệu thật khi #1 được trả lời (hoặc chạy phương án (a) dữ liệu tổng hợp).

**ADR đề xuất (số repo):**
| ADR repo | Tương ứng v4.3 | Trạng thái đề xuất |
|---|---|---|
| ADR-0039 | ADR-21 Stack Go cho hệ nội bộ, không Odoo | Chấp nhận (ghi lại) |
| ADR-0040 | ADR-22 Hai hệ thống thông tin, ranh giới là cổng | Đề xuất |
| ADR-0041 | ADR-23 MFA theo nhóm + chính sách mật khẩu/phiên/45 ngày (P-1…P-5) | Đề xuất |
| ADR-0042 | ADR-24 Chữ ký số lô dữ liệu thương mại | Đề xuất; sửa ADR-0034 (song phương), ADR-0033 |
| ADR-0043 | (mới) Vai trò kiểm soát/xử lý dữ liệu cá nhân theo loại dữ liệu; sửa ADR-0009, ADR-0020, ADR-0030 | Đề xuất — đi cùng H-1 |
| ADR-0044 | (mới) Căn cứ an ninh mạng theo Luật ANM 2025 / NĐ 331 / TCVN 14423:2026; cấp độ đề xuất; ghi chú sửa ADR-0008 (vùng văn phòng, không dây), ADR-0024 (DLP chính thức) | Đề xuất |
| ADR-0045 | (mới, tùy) Phân loại dữ liệu 4 mức và nhãn trong `module.yaml` | Đề xuất |

**Thứ tự đề xuất:** W10.0 → (chờ #1, #2) W10.1 → W10.10 → W10.11 → W10.9 → W10.13 → W10.17 → W10.15 → W10.19; nhánh định danh W10.3 → W10.4 → W10.5 → W10.6 (khi SAD v1.2 + khách KN); nhánh chữ ký W10.7 → W10.8 (G2); nhánh hạ tầng W10.12 (G-1); W10.2 sau W5.10; W10.14, W10.16, W10.18 khi pháp chế trả lời. **Làm ngay không cần khách:** W10.0, W10.9 (mã), W10.10, W10.11, W10.17.

---

#### 4. Câu hỏi cho product owner

1. **(#1 v4.3, chặn G1)** Pháp chế đã xác nhận bản thử nghiệm G1 15/12/2026 có dữ liệu thật có phải "đưa vào sử dụng/vận hành chính thức" theo NĐ 331/2026 chưa? Nếu có và hồ sơ cấp độ không kịp: chọn (a) G1 dữ liệu tổng hợp/phạm vi hạn chế có thỏa thuận, hay (b) lùi mốc dữ liệu thật? Ảnh hưởng W3.5 và H-17.
2. **(ADR-22)** Chấp nhận khai **hai hệ thống thông tin**? Nếu có, những hạ tầng dùng chung nào được phép giữ và khai là "kết nối" (cụm K8s, RabbitMQ khác vhost, namespace `agrigreen-data`, Argo CD/Prometheus, realm Keycloak chung, CI) và cái nào phải tách? Có cần realm Keycloak riêng cho quản trị từng hệ?
3. **(P-5 vs KN v7.7 D9)** Giữ phiên Web 4 giờ / app 24 giờ đã chốt với khách KN (kèm đánh giá rủi ro biện minh), hay chuyển sang 15 phút Web / 5 phút app khóa PIN / 5 phút quản trị? Ai trao đổi lại với TTKN An Giang/TTKNQG?
4. **(P-1/ADR-23)** MFA bắt buộc cho **toàn bộ cán bộ KN, HTX, Cục KTHT** trên Web từ G1, hay theo lộ trình ghi trong hồ sơ? Yếu tố ưu tiên: TOTP, passkey, hay xác nhận đẩy (cần SPI Keycloak chưa kiểm chứng)?
5. **(P-3)** Chấp nhận nâng mật khẩu lên 14 ký tự + độ phức tạp + không trùng 10 cho mọi tài khoản (Keycloak đặt theo realm), hay tách realm quản trị để chỉ quản trị đổi mật khẩu 2 tháng?
6. **(P-2/P-4)** Nông hộ: OTP SMS qua nhà cung cấp nào trong lãnh thổ? Đồng ý vô hiệu sau 45 ngày + tự kích hoạt lại bằng OTP cho nông hộ (và vô hiệu 45 ngày cho cán bộ thay vì không có cơ chế như hiện nay)?
7. **(§21.1, H-1)** Xác nhận **HTX là bên kiểm soát** dữ liệu hộ/xã viên/thửa do HTX nhập (= H-1 phương án b) và **cơ quan khuyến nông là bên kiểm soát** dữ liệu khuyến nông? Phải trả lời trước 15/12/2026 vì đổi chủ sau khi có dữ liệu thật phải làm bằng bản ghi bù.
8. **(§21.1 vs W8.2)** Engine điều phối có được nhận dữ liệu mức thửa/danh bạ HTX (`plot_directory`, `cooperative_directory`) qua cấp phép có mục đích "điều phối", hay chỉ tổng hợp theo xã qua k? Nếu chỉ tổng hợp, W8.2 phải gỡ hai bộ ánh xạ.
9. **(ADR-24)** Chọn nhà cung cấp chứng thư số (và HSM/ký từ xa) cho MKG; HTX chưa có chứng thư thì nền tảng **ký thay có ủy quyền** — pháp chế chấp nhận hình thức ủy quyền nào? Hạn G2 có giữ?
10. **(RV-16)** Chủ quản App Khuyến nông là MKG hay TTKNQG? (quyết ai phê duyệt hồ sơ cấp độ và hợp đồng xử lý dữ liệu).
11. **(§19, #3)** Ai là đơn vị chuyên trách an ninh mạng của MKG và nhân sự bảo vệ DLCN; bộ phận bảo vệ ANM độc lập là nội bộ hay dịch vụ thuê ngoài trong lãnh thổ? Đơn vị độc lập giám sát nghiệm thu là ai (#9)?
12. **(ADR-21 Việc 1)** Người review mã thứ hai cho cả nền tảng và hệ nội bộ là ai, từ khi nào? Đội Odoo nội bộ chuyển sang Go hay vai trò khác?
13. **(ADR-21 Việc 2, Q-1)** Phần mềm kế toán pháp định đã chọn chưa — phải hỗ trợ TT 99/2025, HĐĐT, lương mùa vụ, kho phụ tùng/nhiên liệu; định dạng nhận bút toán cho `mkg/acctbridge`?
14. **(RV-15/RV-17)** Có chấp nhận các đường ra nước ngoài hiện có trong repo không: xuất Google Sheets (ADR-0026, Q-14), FCM (F-7), GitHub Actions (A8)? Pháp chế đã kiểm danh mục dữ liệu quan trọng/cốt lõi cho dữ liệu nông nghiệp tổng hợp chưa?
15. **(RV-18, K-10)** Thời hạn thông báo vi phạm DLCN và sự cố ANM đã chốt từ toàn văn NĐ 356 / NĐ 331 chưa?
16. **(RV-19, H-13, RV-21)** Phương tiện thứ hai cho sao lưu 3-2-1 là gì (băng/đĩa ngoại tuyến/kho đối tượng khác loại)? Chốt RPO 1 giờ hay 24 giờ? Vùng mạng văn phòng MKG và Wi-Fi hub do ai thiết kế/vận hành?
17. **(H-8 mở rộng)** Xin bản SAD định danh **v1.2**, BRD định danh v1.2, Thiết kế vùng nội bộ **v1.4**, bộ `docs/db/` (DB-02, DB-05) để đưa vào repo — W7.2–W7.4, W8.3–W8.5, W10.3, W10.13 phụ thuộc.
18. **(Đánh số)** Đồng ý đánh số repo ADR-0039…0042 cho ADR-21…24 của v4.3 (tránh trùng ADR-0021…0024 hiện có), và yêu cầu kiến trúc sư dùng tiền tố khác (vd. "KT-ADR-21") ở bản sau?
19. **(A8)** Sửa thanh toán GitHub Actions — bằng chứng CI là đầu vào của hồ sơ cấp độ (5.17) và gói W10.19.
</content>
</invoke>

---

## Phụ lục B — D2 — Đối chiếu Thiết kế vùng nội bộ MKG v1.4 với repo

### D2 — Đối chiếu "Thiết kế vùng nội bộ MKG v1.4" với repo `Agrigreen_demo_v2`

**Ngày:** 09/10/2026 · **Repo:** `C:\Dev\claude-skills-main\Agrigreen_demo_v2`. HEAD là `27afd593`. Nhánh checkout tên `main`, cùng commit với `dev` theo đề bài. Phân tích chỉ đọc. Không đụng `C:\Users\manht\agwt\*`.
**Nguồn:** `G:\Shared drives\Development\Thiet-ke-chi-tiet-vung-noi-bo-MKG-v1_4.md` (ký hiệu v1.4 §x). Kiến trúc v4.3 Phần III–VI (ký hiệu v4.3 §x) chỉ đọc ở những chỗ v1.4 dẫn tới.
**Ghi chú nền:** v1.4 viết khi chưa đọc được repo. Nó đối chiếu repo ở `c2cfccf` theo lời bản review. Từ đó tới nay repo đã có W5.1–W5.9, W8.1, W8.2 và W4.2. Nhiều chỗ v1.4 ghi "theo bản review" giờ kiểm được trực tiếp, và kết quả kiểm có ở bảng dưới.

Ký hiệu trạng thái: **XONG** · **MỘT PHẦN** · **THIẾU** · **XUNG ĐỘT** (repo làm khác hoặc trái v1.4).

---

#### 1. Trạng thái từng yêu cầu / quyết định

##### 1.1 Ba thành phần, engine ba chế độ, vị trí engine (v1.4 §0, §3, §4.1, §4.5; D-1)

| Yêu cầu v1.4 | Trạng thái | Bằng chứng trong repo | Ghi chú |
|---|---|---|---|
| Ba thành phần: kế toán mua ngoài · hệ vận hành Go một khối · engine Python riêng (§3) | **XONG** (khung) | `internal/mkg/*` có 9 module. `mkg/acctbridge` dùng bộ xuất tệp CSV/XML và bộ giả, chưa chọn nhà cung cấp (`docs/design/mkg.md` §5.7). Engine Python nội bộ **chưa có** | D-11 (chọn phần mềm kế toán) còn mở |
| Một lõi, ba chế độ, dùng chung mô hình và hàm chi phí (§0, §4.1) | **MỘT PHẦN** | `lib/optcore` 1.0.0 (W8.1) gồm `engine`, `flow`, `calendar`, `params`, `select_hubs` (HiGHS), `sensitivity`. Lõi này chỉ là mô hình chọn hub của App SIM | Lõi **chưa có** các phần v1.4 cần: điểm gom, cỡ ghe, cung đường theo cỡ ghe, MILP theo thời gian (T1), Monte Carlo thời tiết, tồn an toàn, T2a/T2b/T3 |
| D-1: cả ba chế độ ở vùng nội bộ, dùng chung gói lõi với `pf/simulation` (§4.5) | **XONG ở mức quyết định, THIẾU ở mức triển khai** | ADR-0036 §2 (Đề xuất, chờ A7) chốt hai triển khai, dữ liệu riêng. `optimizer/` vẫn chỉ phục vụ `pf/simulation`. Chưa có đơn vị triển khai engine ở vùng mkg, chưa có vhost `mkg.internal` cho engine | Lý do 3 ở §4.5 ("masterdata dùng chung lộ chi phí MKG") **không đúng với repo**: `pf/simulation/app/adapters.go` `MasterdataParams.Approved(ctx, tid, …)` tra theo tenant. Kết luận D-1 vẫn đúng nhờ lý do 1, 2 và 4 |
| Chế độ mô phỏng của MKG "làm ngay" (§8) | **THIẾU** | Không có mã mô phỏng nào chạy trên dữ liệu MKG ở vùng mkg | §4.5 nói đường dữ liệu nền tảng → MKG ở §7.7 chặn cả mô phỏng. Thực tế mô phỏng chạy được ngay với tham số `assumed` (§9) và đồ thị đường thủy |
| Mục tiêu thời gian giải (§4.1): giờ / <30 phút / sửa nhanh <2 phút, giải lại <15 phút | **THIẾU** | Không có | Theo §4.2, chỉ chốt sau khi làm thử D-6 |

##### 1.2 Ba tầng, nối T2a–T2b, vòng lặp lập lại kế hoạch (v1.4 §4.2, §4.3; D-6, D-7)

| Yêu cầu | Trạng thái | Bằng chứng | Ghi chú |
|---|---|---|---|
| T1 HiGHS: dòng chảy mạng theo ngày, cuộn 7–14 ngày | **THIẾU** | `optcore.select_hubs` là MILP chọn hub tĩnh, không chỉ số thời gian | Đã có `highspy` qua extra `[milp]`, đã kiểm giấy phép theo ADR-0016 |
| T2a (OR-Tools định tuyến đón–trả), T2b (CP-SAT), T3 (CP-SAT) | **THIẾU** | Repo không có OR-Tools. ADR-0016 chưa có mục OR-Tools. ADR-0036 §4 mới nêu quy trình | D-6 cần một ADR thêm phụ thuộc, theo quy trình ADR-0016 |
| Sức đệm điểm gom là tham số; hai cách nối T2a–T2b (§4.2) | **THIẾU** | Không có thực thể "điểm gom". `mkg_logistics.leg_event.from_kind/to_kind` có giá trị `collection_point`, nhưng đó là điểm gom ruộng, không phải nút trung chuyển ghe | Cần thực thể điểm gom có `buffer_capacity`, `weighing_mode`, năng lực trung chuyển |
| D-6 làm thử T2a/T2b ở cỡ Phase 1, tiêu chí đạt định trước | **THIẾU** | — | Làm được ngay trên dữ liệu tổng hợp. Không phụ thuộc repo, chỉ cần ADR phụ thuộc |
| Vùng đóng băng: lệnh đã giao hoặc khởi hành <2 giờ, engine không đổi (§4.3) | **THIẾU** | Không có thực thể kế hoạch đề xuất hay lệnh chuyến do engine sinh. `trip_current` có trạng thái `planned/departed/…`, `fn_trip_forward` chặn lùi trạng thái | Quy tắc đóng băng đặt được trên `trip_current` (`status <> planned` hoặc `departure_at - now < 2h`) |
| Ngưỡng kích hoạt `[KÝ]`, chính sách tự duyệt `[KÝ]` hai người (§4.3) | **THIẾU** (có sẵn mẫu) | Mỗi module đều có mẫu sổ tham số có phiên bản với người duyệt khác người đề xuất: `mkg_logistics.param_version` (mkg 000015), `mkg_fieldops.param_version` (000012), `mkg_orders`, `mkg_hub`, `mkg_measurement.param_decision` (`CHECK actor_ref <> proposer_ref`) | Dùng lại đúng mẫu đó cho bộ tham số `engine.replan.*` và `engine.autoapprove.*` |
| Tự duyệt khi thỏa đồng thời 5 điều kiện; ghi nhật ký; đo tỷ lệ bị con người đảo ngược (§4.3) | **THIẾU** | — | ADR-0036 §2 có câu "người duyệt hoặc tự duyệt theo năm điều kiện" nhưng chưa có mã. X% chưa có giá trị (D-7) |
| Mọi kế hoạch mới là đề xuất; bản chỉnh tay lưu kèm lý do (§4.3) | **THIẾU** | ADR-0036 §2 có hướng: đề xuất đi vào `mkg/logistics`, `mkg/fieldops` qua API nội bộ | v1.4 §5 nói "kế hoạch có phiên bản: đối chiếu trước khi tạo". Repo chỉ có `leg_event.plan_revision`, tức phiên bản chặng **của từng chuyến**. Chưa có kế hoạch mạng (T1), chưa có đề xuất cấp lô |
| Mô phỏng nuôi điều phối: tồn an toàn, cơ cấu ghe, điểm gom (§4.4) | **THIẾU** | — | Khi làm, ghi kết quả mô phỏng thành **đề xuất tham số chính sách** vào sổ tham số của module chủ (vẫn qua duyệt hai người), không ghi thẳng |

##### 1.3 Module (v1.4 §5)

| Chức năng | Quyết định v1.4 | Trạng thái | Bằng chứng / khoảng trống |
|---|---|---|---|
| Đội, gán đội → mở rộng `fieldops` | Mở rộng | **XONG** (phần đội) | mkg 000012: `crew_event`, `crew_assignment` có năng lực FM-06/07/08 |
| 1.500+ nhân sự → tách `mkg/workforce`; `fieldops` chỉ giữ `worker_ref` | Module mới | **THIẾU** + **XUNG ĐỘT nhỏ** | Không có `mkg/workforce`. `crew_event.member_refs uuid[]` là **subject tài khoản MKG** (chú thích mkg 000012 dòng 7), không phải `worker_ref` bút danh. 1.500 công nhân hiện trường chưa chắc có tài khoản Keycloak, nên tham chiếu này cần đổi sang `worker_ref` |
| 500+ máy cuộn, trạng thái, hỏng hóc → mở rộng `fieldops` | Mở rộng | **THIẾU** | `fieldops` không có thực thể máy cuộn hay hỏng hóc. `mkg_logistics.vessel_event.vehicle_kind ∈ {ghe, sa_lan, truck}`, không có máy cuộn |
| Mua từ HTX → mở rộng `orders` chiều mua | Mở rộng | **XONG** phần lớn | mkg 000016: `PurchaseContract`, `PurchaseOrder`, `GoodsReceipt`, theo tấn hoặc theo cuộn (CT-01..03). Thiếu: nguồn cung hai độ tin cậy cho engine (§1.3: tỷ lệ đội MKG / HTX là tham số) |
| Ghe thuê, đặt chuyến, ghe sẵn sàng → mở rộng `logistics` | Mở rộng | **MỘT PHẦN** | `vessel_event` có danh mục ghe, `rate_type`, `straw_payload_tons`, `owner_ref`. **Thiếu:** cam kết thuê (đặt chuyến, hợp đồng khung so với theo chuyến) và ghe khả dụng theo cỡ theo ngày (khóa quan sát §7.6) |
| Điểm gom, ghép ghe → loại chặng trong `logistics`, nút trộn vào gia phả dùng chung | Không tạo module | **THIẾU** | Không có loại chặng ghép ghe. `trip_event.cargo` là `[{lot_ref, mass_kg, ticket_ref}]` và không có quan hệ N lô → 1 lô |
| Kế hoạch có phiên bản | Đối chiếu trước khi tạo | **Đã đối chiếu:** chỉ có `leg_event.plan_revision` mỗi chuyến, và `pf_simulation` snapshot bất biến ở nền tảng (không dùng được cho MKG) | Đề xuất đặt sổ kế hoạch đề xuất ở `mkg/engine` (§3, gói E5) |
| Cước thuê ngoài | Đặt theo nơi bảng cước đang có | **XONG** (vị trí) | Cước ở `mkg_logistics.param_version` (`rate_vnd_per_t_km`, ghim `param_version` vào chặng) và `vessel_event.rate_*` |
| Bỏ "thay `mkg/fieldops`" | Bỏ | **XONG** | Repo không có `mkg/fleet` |
| Cân, hub, bán, giá thành, cầu nối kế toán | Giữ | **XONG** | `measurement`, `hub`, `orders`, `costing`, `acctbridge` |
| Kho phụ tùng, nhiên liệu → mua | Mua | **XONG** (không xây) | — |
| Module mới `mkg/engine` (repo W8.2) | v1.4 không nêu | **LỆCH có lý do** | v1.4 §5 nói "từ năm module mới còn một (`mkg/workforce`)". W8.2 đã tạo thêm `mkg/engine` làm phía dữ liệu của engine (lý do ở `docs/design/mkg.md` §5.9). v1.4 §7.1 chỉ nói "kho quan sát mới", không nói đặt ở đâu. Cần MKG ghi nhận đây là module thứ hai (Q-3) |

##### 1.4 Dữ liệu cá nhân nhân sự (v1.4 §5.1, D-8, D-12; v4.3 §8, §21.2)

| Quy tắc | Trạng thái | Bằng chứng |
|---|---|---|
| 1. Bảng vận hành chỉ giữ `worker_ref` bút danh | **THIẾU / XUNG ĐỘT** | Như trên: `member_refs` là subject tài khoản. `trip_event.driver_ref`, `actor_ref` ở mọi sổ là subject tài khoản |
| 2. Định danh nhân sự ở schema riêng trong cụm `mkg`, mã hóa mức trường, HMAC, xóa được | **THIẾU** + **XUNG ĐỘT tài liệu** | Không có. `docs/design/mkg.md` §6 dòng RB-3 ghi "tài xế/nhân viên MKG: định danh ở **vault tenant MKG**", trái v1.4 (schema trong cụm `mkg`). Mẫu dùng lại được: `migrations/vault/000002_vault_identity.up.sql` (mã hóa, HMAC). Plan W8.4 giữ chỗ mkg 000027–000028 |
| 3. Không lưu tài khoản ngân hàng, lương, bảo hiểm; bảng công chỉ gồm mã NV kế toán + công + sản lượng | **XONG** theo nghĩa phủ định (không có các trường đó). Xuất bảng công **THIẾU** | `acctbridge` chỉ đẩy bút toán tổng hợp |
| 4. GPS theo máy và đội. **v1.4:** máy gắn một người lái thì vị trí máy là dữ liệu vị trí của người đó; chỉ thu trong giờ làm việc; thời hạn lưu có khai báo | **XUNG ĐỘT** | (a) `mkg_fieldops.roll_count_location` (mkg 000010) lưu `lat/lng/accuracy_m` của **thiết bị người đếm** kèm `actor_ref`, trong **sổ chỉ ghi thêm**, nên không xóa được theo thời hạn lưu. (b) `mkg_logistics.trip_event` có `lat/lng` và `driver_ref`. (c) `ts_fleet` (ts 000004 dự kiến) chưa có. Không chỗ nào giới hạn giờ làm việc hay khai thời hạn lưu |
| 5. Thời hạn lưu sau nghỉ việc do pháp chế chốt | **THIẾU** (chờ pháp chế) | H-10 ở `docs/cau-hoi-ton-dong.md` |

##### 1.5 Truy xuất nguồn gốc và giá thành (v1.4 §6, D-4)

| Yêu cầu | Trạng thái | Bằng chứng / khoảng trống |
|---|---|---|
| Gia phả lô một mô hình; ghép ghe ghi **nút trộn** N nguồn kèm tỷ lệ | **MỘT PHẦN** | Mô hình nguồn có ở `mkg_hub.lot.sources` (jsonb, `[{kind, ref, ratio, mass_kg?}]`, kind ∈ plot/cooperative/roll_count/supplier/lot, mkg 000002) và `batch.inputs/outputs`. Nhưng `lot` gắn với `facility_id NOT NULL`, stream `facility:<id>`; `facility.kind ∈ {hub, warehouse, yard}`. Ghép ghe trên sông không có cơ sở nào để gắn. `hub/port.LotOrigins` là cổng đọc gia phả. `mkg_measurement.lot_collection_current` có UNIQUE theo `collection_order_ref` (1 lệnh ↔ 1 lô, mkg 000010), không biểu diễn được N lệnh → 1 lô ghe lớn |
| Đối soát và **định mức hao hụt riêng** cho lô đi thẳng | **XUNG ĐỘT** | `mkg_measurement.reconciliation` (mkg 000008): `CHECK (route <> 'DIRECT' OR (internal_status='not_applicable' AND commercial_status='not_applicable' AND yield_aggregated AND outcome='direct_aggregate'))`. Bất biến ở `docs/design/mkg.md` §5.2 viết "lô DIRECT không tính hao hụt tách lớp". `loss_norm_version` chỉ có stream `loss_norm:drying`. Phần có sẵn để dùng lại: `roll_check` (cuộn × kg/cuộn so với CUSTOMER_IN) |
| Điểm cân thứ tư `CONSOLIDATION`; `weighing_mode` theo điểm gom ∈ scale/sampling/roll_estimate | **THIẾU** | Có đúng ba điểm (xác nhận lời bản review): `CHECK point IN ('HUB_IN','HUB_OUT','CUSTOMER_IN')` ở `weigh_ticket` (mkg 000007:241), `point IN ('HUB_IN','HUB_OUT')` ở 000007:58/158 và 000009:128/179, orders 000016:508/536, costing 000017:99. `route` chỉ có HUB/DIRECT, kèm `CHECK (route <> 'DIRECT' OR point = 'CUSTOMER_IN')` (000007:278). Phạm vi ảnh hưởng gồm cả `contracts/openapi/mkg-internal.yaml`, `apps/web-internal` (mã sinh), `internal/platform/mrv/app/g3.go` (`mass_basis`) |
| Chế độ `roll_estimate`: kg/cuộn bình quân có phiên bản theo HTX | **MỘT PHẦN** | `fieldops/port.KgPerRollEstimates.KgPerRollAsOf(…htxRef, before, excludeLot)`, nguồn `htx_learned`, có `Samples`. Học tất định nhưng **không có phiên bản**, và chưa công bố sai số |
| Chế độ `sampling`: cỡ mẫu và sai số | **THIẾU** | — |
| Cờ độ tin cậy trên mỗi kết quả giá thành và mỗi nút trộn | **THIẾU** | `costing` ghim `rule_version` nhưng chưa có cờ `mass_confidence` |

##### 1.6 Đầu vào engine (v1.4 §7, D-2, D-3, D-5) — trọng tâm W8.2

###### §7.1 Tách quan sát khỏi tham số chính sách

**XONG về cấu trúc.** Quan sát nằm ở `mkg_engine.observation` (mkg 000037). Tham số chính sách nằm ở các sổ `param_version`/`param_current` của từng module, không gom về một chỗ. **THIẾU:** ảnh chụp đọc cả hai qua port và ghi phiên bản từng sổ và từng bản ghi quan sát đã dùng.

###### §7.2 Bản ghi quan sát — W8.2 so với v1.4

| Trường v1.4 | W8.2 (`mkg_engine.observation`, `engine/port.Observation`) | Đánh giá |
|---|---|---|
| `key` (mã danh mục) | Cột **`kind`** (vd `harvest_share`, `plot`, `waterway_network`), regex `^[a-z][a-z0-9_]{1,47}$` | **XUNG ĐỘT tên.** W8.2 dùng `kind` cho cái v1.4 gọi là `key` |
| `scope` (đối tượng **và thời gian áp dụng**) | `entity_key` (chỉ đối tượng) | **MỘT PHẦN.** Thiếu khoảng thời gian áp dụng (`valid_from`/`valid_to`). Khóa có lịch ngày như kế hoạch tiêu thụ VFT, ghe khả dụng theo ngày, dự báo mưa 12 giờ không biểu diễn được |
| `value`, `unit` | `value numeric` + `unit` với `CHECK unit IN ('ha','ratio')` | **MỘT PHẦN.** Đơn vị bị khóa cứng hai giá trị. Cần thêm t, t/ngày, chiếc, m, mm, giờ… theo danh mục |
| `as_of` (lúc giá trị đúng) | `as_of` = as_of của **trang feed** do cổng trả, không phải của bản ghi. Thêm `EffectiveAsOf` = max(as_of, as_of lần đọc tới đầu feed) | **MỘT PHẦN.** Với feed trạng thái thì hợp lý. Nhưng `harvest_progress_by_commune` có mốc thật của dữ liệu tổng hợp (phiên bản tập dữ liệu), nên `as_of` nên lấy từ phiên bản tập |
| `received_at` | `recorded_at` (DB đặt) | **XONG** (khác tên) |
| `kind` = measured · declared · estimated · assumed · scenario | **Không có** | **THIẾU.** Đây là chỗ lệch lớn nhất. Quy tắc 2 (§7.3, `kind` phân xử khi cùng `as_of`), quy tắc 4 (mặc định `assumed`), §7.4 (bộ ước tính `estimated`, kịch bản `scenario` chỉ ở mô phỏng) đều cần trường này |
| `source` | `source IN ('api','manual','sensor')` | **MỘT PHẦN.** v1.4 §7.4 có các cổng tay, tệp, API, email (sau), bộ ước tính, kịch bản. Thiếu `file`, `estimator`, `scenario`, `email`. `sensor` không có trong v1.4 nhưng dùng được (≈ measured) |
| `source_ref` | `feed`, `contract_version`, `feed_seq`, `event_id`, `feed_cursor` (chỉ cho api) | **MỘT PHẦN.** Cổng tay và tệp cần `source_ref` chung (mã tệp và dòng, mã phiếu nhập) |
| `entered_by` | **Không có** (sổ không có `actor_ref`) | **THIẾU.** Đây cũng là ngoại lệ so với mẫu sổ 8.1, nơi mọi sổ đều có `actor_ref`. Nhập tay bắt buộc phải có |
| `uncertainty` (cùng đơn vị) | `uncertainty` (nửa độ rộng tuyệt đối, ≥ 0) + `uncertainty_basis text[]` | **XONG và vượt yêu cầu** (`domain.Measured`, lan truyền theo cận tuyến tính). Khớp với cách v1.4 §7.3 ví dụ cộng dồn 6 × 750 t |
| `max_age` | `max_age_seconds` **trên từng dòng**, lấy từ `freshness.max_age` của **hợp đồng feed** | **XUNG ĐỘT ngữ nghĩa.** v1.4 §7.6: `max_age` thuộc **danh mục theo khóa** và chốt ở D-2. Hợp đồng feed đặt `max_age` theo độ tươi giao nhận (`harvest_progress_by_commune` = PT15M, `waterway_routes` = PT1H, `plot_directory` = PT6H). Danh mục v1.4 gợi ý "tiến độ gặt: 2 ngày trong vụ", "đồ thị: theo phiên bản". Thêm nữa, job `engine_observation_feed` chạy mỗi 15 phút (`module.go`), đúng bằng PT15M, nên quan sát tiến độ gặt sẽ **chập chờn Stale** giữa hai lượt kéo |
| Chỉ ghi thêm | `shared.apply_ledger`, chuỗi băm, rút = dòng `retracted` | **XONG** |
| (không có trong v1.4) | `payload`, `mapper_version`, `retracted`, `stream_key`, `idem_key` | Giữ lại. `payload` cần cho khóa phi số (danh bạ, cửa sổ gặt) |

Cũng thiếu: phiên bản danh mục (`catalog_version`) ở mức bản ghi hoặc ảnh chụp.

###### §7.3 Quy tắc chọn giá trị

| # | Quy tắc v1.4 | W8.2 | Trạng thái |
|---|---|---|---|
| 1 | Khóa có bộ ước tính: dùng đầu ra bộ ước tính, neo vào số đo hợp lệ mới nhất | Không có bộ ước tính nào | **THIẾU** |
| 2 | Khóa không có bộ ước tính: mới nhất theo `as_of` trong `max_age`; `kind` chỉ phân xử khi cùng `as_of` | `store.Latest`: `DISTINCT ON (entity_key) … ORDER BY as_of DESC, recorded_at DESC, seq DESC` | **MỘT PHẦN.** Chọn mới nhất theo `as_of` đúng. Khi cùng `as_of` thì phân xử bằng `recorded_at`, không bằng `kind`. Nếu dòng mới nhất là `retracted`, entity vào `Missing` dù có thể còn quan sát `manual` hợp lệ cũ hơn (hiện chưa có đường ghi manual nên chưa lộ ra) |
| 3 | Quá `max_age` thì loại khỏi vòng chọn | `app.Classify` tách `Fresh` / `Stale` / `Missing`; Stale không bao giờ lẫn vào Fresh | **XONG** (nhưng `max_age` lấy sai nguồn, xem trên) |
| 4 | Không còn giá trị hợp lệ: khóa bắt buộc thì từ chối chạy và nêu khóa thiếu; khóa tùy chọn thì dùng mặc định `assumed` và ghi cờ | Không có phân loại bắt buộc / tùy chọn, không có mặc định | **THIẾU** |
| 5 | Ảnh chụp ghi cho từng khóa: bản ghi đã dùng, `as_of`, tuổi tại thời điểm chụp, `kind` | Không có ảnh chụp | **THIẾU** |

###### §7.4 Cổng nạp

| Cổng | Trạng thái | Ghi chú |
|---|---|---|
| Nhập tay theo danh mục, bắt buộc `as_of` và đơn vị | **THIẾU** | `port.SourceManual` có hằng nhưng "chưa có đường ghi — W8.3" |
| Tải tệp theo **mẫu chuẩn do MKG phát hành**, có người xác nhận trước khi có hiệu lực | **THIẾU** ở vùng mkg | W5.8 là kênh tệp của **cổng tích hợp nền tảng** (`POST /v1/files/{contract}`), dành cho đối tác gửi vào nền tảng. Mã kiểm tệp (`shared/docfile`, `orders.file.v1`) dùng lại được, nhưng bước "người xác nhận" chưa có |
| API kéo/đẩy, kiểm lược đồ, khóa API, mTLS | **MỘT PHẦN** | Kéo: job `engine_observation_feed` qua `fieldops.NewGatewayDirectoryFeed` (W5.5), có kiểm `ValidateRecord` và `entity_key`. Đẩy: không có. VFT (v1.4 §10.2) đi **"cổng nhận trong vùng nội bộ, không qua nền tảng"**, cái này **THIẾU** |
| Email (sau): DKIM/SPF/DMARC, người xác nhận | **THIẾU** (đúng lộ trình) | — |
| Bộ ước tính: `kind = estimated`, luôn có `uncertainty` | **THIẾU** | — |
| Kịch bản chỉ ở mô phỏng, không lẫn vào kế hoạch và điều phối | **THIẾU** | Cần `kind = scenario` cộng một ràng buộc ở bước chọn: chế độ ≠ mô phỏng thì loại `scenario` |

###### §7.5 Tồn kho VFT (D-3)

**THIẾU toàn bộ.** Repo không có khóa, cổng hay bộ ước tính nào cho tồn VFT. Phần có sẵn để dùng lại:
- Σ khối lượng đã giao từ t0 tới t: phiếu `CUSTOMER_IN` ở `measurement` (`LotMass`) hoặc giao nhận ở `orders` (`Deliveries`).
- Lan truyền bất định: `domain.Measured` (Add, Sub, Widen).
- Mô hình chặn biên [0, sức chứa] kèm cảnh báo khi chạm.

###### §7.6 Danh mục có phiên bản

**THIẾU.**
- Danh mục hiện là map `mappers` cứng trong `engine/domain/mapper.go` (feed → kind), không có phiên bản, đơn vị, phạm vi, `max_age` hay cờ bắt buộc.
- Phía Python có tiền lệ đúng: `optimizer/src/agrigreen_optimizer/snapshot.py` đặt `extra="forbid"` ở mọi cấp và `schema_version: Literal[1]`, nên từ chối phiên bản lạ. Engine MKG nên theo đúng mẫu này với `catalog_version`.
- Optimizer còn có `Params.unapproved` / `ParamValue.approved`, tức tiền lệ "ghi cờ vào kết quả".

###### §7.7 Đường dữ liệu nền tảng → MKG (D-5)

| Đầu vào | Trạng thái | Bằng chứng |
|---|---|---|
| Tiến độ gặt, diện tích theo xã, ngưỡng k | **MỘT PHẦN — mã xong, chờ ký** | ADR-0035 (Đề xuất, chờ A7, K-8). Hợp đồng `contracts/integration/feeds/harvest_progress_by_commune.v1.json` (proposed). Producer `apiclient_feed_build` (W5.3, pf 000126–127) theo quy tắc k, có dòng `province_withheld`. Bộ ánh xạ W8.2 ra `harvest_share`, `harvest_area_remaining`, căn cứ `k_suppressed_residual`. ADR-0020 `ShareKinds` (`internal/platform/party/port/sharing.go:148`) chưa có loại mới. ADR-0035 chọn `grant_class = aggregate` của cấp phép v2 thay cho việc sửa `ShareKinds`/CHECK như v1.4 đề xuất. Cách này **tương đương về mục đích**, cần ghi nhận |
| Sản lượng rơm tấn/ha tự đo từ cuộn × kg/cuộn × diện tích | **MỘT PHẦN** | Đã có đủ dữ kiện: số cuộn (`roll_total_current`), kg/cuộn học theo HTX (`KgPerRollAsOf`), diện tích thửa (`plot_directory`). Chưa có phép tính và khóa chính sách "hệ số tấn/ha" |
| Đồ thị đường thủy | **MỘT PHẦN** | Hợp đồng `waterway_routes.v1` chỉ mang **siêu dữ liệu phiên bản** (ảnh chụp qua URL ký). **Chưa có producer** ở cổng (`docs/design/mkg.md` §5.9). Đoạn luồng `pf_waterway` chỉ có `waterway_class` (pf 000023:36), **không có cỡ ghe tối đa, tĩnh không cầu, mực nước** |
| Tĩnh không cầu (nhập tay, khảo sát) | **THIẾU** | Chưa có thuộc tính đoạn luồng hay khóa quan sát |
| Thủy triều, dự báo mưa theo ô lưới hoặc tâm xã | **THIẾU** | Chưa có nguồn. ADR-0007 (nhà cung cấp trong lãnh thổ) là ràng buộc, cần đọc khi chọn nguồn |

##### 1.7 Lộ trình và quyết định (v1.4 §8, §9, §10)

| # | Quyết định | Trạng thái repo |
|---|---|---|
| D-1 | Mô phỏng ở vùng nội bộ, chung lõi | **XONG** quyết định (ADR-0036, chờ A7); triển khai **THIẾU** |
| D-2 | Danh mục quan sát v1 (khóa, đơn vị, phạm vi, `max_age`, bắt buộc/tùy chọn, phiên bản; quy tắc 7.3) | **THIẾU**. W8.2 làm phần kho nhưng chưa có danh mục |
| D-3 | Bộ ước tính tồn kho, quy độ bất định ra tồn đệm | **THIẾU** |
| D-4 | Lược đồ cân và gia phả: `CONSOLIDATION`, đối soát và định mức cho lô đi thẳng, nút trộn dùng chung | **THIẾU / XUNG ĐỘT** (§1.5) |
| D-5 | Đường dữ liệu nền tảng → MKG | **MỘT PHẦN** (ADR-0035, W5.3, W5.5, W8.2 xong mã; chờ A7, k; thiếu producer `waterway_routes`, `season_harvest_dates`) |
| D-6 | Làm thử T2a/T2b + ADR OR-Tools | **THIẾU** |
| D-7 | Chính sách tự duyệt `[KÝ]` | **THIẾU** (X% chưa có) |
| D-8 | Kho định danh nhân sự | **THIẾU** (W8.4) |
| D-9 | Đồng bộ tài liệu, đưa kiến trúc vào `docs/` qua ADR | **MỘT PHẦN.** `docs/kien-truc/` có v4.1. **Chưa có v4.3 và MKG v1.4** (H-8 ở `cau-hoi-ton-dong.md` vẫn ghi "MKG v1.3 chưa có trong repo"). **Xung đột số ADR:** ADR-21…24 của v4.3 (stack Go, hai hệ thống, MFA, chữ ký số) trùng số với ADR-0021…0024 của repo (Kubernetes, Public API, metering, DLP). Khi đưa vào repo phải đánh số mới (≥ 0039) và ghi bảng ánh xạ |
| D-10 | Năng lực Go, người review thứ hai | Ngoài repo |
| D-11 | Chọn phần mềm kế toán | Ngoài repo. `acctbridge` đã có `LedgerAdapter` chờ |
| D-12 | Căn cứ xử lý, thời hạn lưu vị trí người lái | **XUNG ĐỘT** với dữ liệu đang thu (§1.4 quy tắc 4) |
| §10.1 | Hệ nội bộ là hệ thống thông tin riêng, cấp độ 2 | Kỹ thuật đã tách (`ag_mkg` riêng, R-03/R-05, `check_flows.py`). Hồ sơ cấp độ ngoài repo |
| §10.2 | Feed adapter Go kéo theo cursor, `source = api`, dùng chung thư viện khách của cổng | **XONG** (`engine_observation_feed`, cursor riêng). "Thư viện khách dùng chung" hiện nằm ở `internal/mkg/fieldops/adapter/gatewayfeed.go`, chưa tách thành thư viện `shared` |
| §10.2 | Chữ ký số của tổ chức gửi trên từng lô (số cuộn, xác nhận HTX: **bắt buộc**; lịch gom: nên có) | **THIẾU.** W5.6 (sổ song phương ở pf 000135: `pf_coop.bilateral_record`/`bilateral_pair_event`; mkg 000038 chỉ giữ các bảng gửi, trạng thái feed và bản sao: `mkg_fieldops.bilateral_send`, `bilateral_feed_state`, `bilateral_feed_record`) chỉ có `payload_sha256` và HMAC của cổng. Không có trường chữ ký, chứng thư hay kiểm chữ ký nào trong `contracts/integration/ingest/*.json`, `contracts/integration/bilateral.go`, ADR-0034 |
| §10.2 | Tồn và kế hoạch tiêu thụ VFT qua cổng nhận nội bộ, có chữ ký số | **THIẾU** |

---

#### 2. Cần sửa ở phần đã xây (đề xuất cụ thể)

**C-1 · Lược đồ quan sát W8.2 → v1.4 §7.2.** Migration mới **mkg 000039** (không sửa 000037, vì R-24 dùng tổng kiểm). Theo quy tắc ADD COLUMN của ADR-0012: cột `NULL`, không `DEFAULT`, đăng ký `shared.register_ledger_columns`.
- `epistemic_kind text NULL CHECK (epistemic_kind IN ('measured','declared','estimated','assumed','scenario'))`. Tên cột vật lý không dùng `kind` vì `kind` đã là khóa danh mục.
  - **Đổi tên ở tầng Go/port:** `Observation.Kind` → `Key`, `Query.Kind` → `Key`, hằng `KindHarvestShare…` → `KeyHarvestShare…`. Thêm `Observation.Kind` mới mang giá trị epistemic. Ghi rõ trong `docs/design/mkg.md` §5.9: "cột `kind` = `key` của v1.4".
  - Phương án gọn hơn: nếu 000037 **chưa áp ở môi trường dùng chung nào** (dev/staging), sửa thẳng 000037, đổi `kind` → `key` và thêm cột mới. Xem Q-1.
- `valid_from timestamptz NULL`, `valid_to timestamptz NULL`, cộng `CHECK (valid_to IS NULL OR valid_to > valid_from)`. Đây là phạm vi thời gian của `scope`.
- `source_ref text NULL` và `entered_by uuid NULL` (bắt buộc khi `source IN ('manual','file')`, kiểm bằng CHECK).
- `catalog_version integer NULL`.
- Đổi CHECK `source`: thêm `'file','estimator','scenario'`, để chỗ cho `'email'`.
- Đổi CHECK `unit`: dùng danh sách theo danh mục (`ha`, `ratio`, `t`, `t_per_day`, `count`, `m`, `mm`, `h`), hoặc bỏ CHECK cứng và kiểm theo danh mục ở `app`. Đổi CHECK không làm lệch chuỗi băm.
- Đọc dòng cũ (NULL): suy `epistemic_kind` theo `mapper_version = engine-obs.v1`. Feed tổng hợp/danh bạ → `declared`. Căn cứ `forecast` / `k_suppressed_residual` → `estimated`. Ghi quy tắc này thành `mapper_version = engine-obs.v2` cho dòng mới.

**C-2 · Tách `max_age` giao nhận khỏi `max_age` danh mục.**
- Giữ cột `max_age_seconds` = độ tươi feed (hợp đồng tích hợp), dùng cho cảnh báo `feed stale` của bộ điều hợp.
- Quy tắc chọn §7.3 phải dùng **`max_age` của danh mục** theo khóa (D-2), áp lúc đọc.
- `Classify` nhận thêm `catalogMaxAge`.
- Ngay cả khi chưa có danh mục: đổi chu kỳ job mặc định xuống dưới `max_age` nhỏ nhất (vd 5 phút), hoặc tính Stale theo `max(feed max_age, chu kỳ job + biên)` để không chập chờn.

**C-3 · Phân xử cùng `as_of` theo `kind`** (quy tắc 2). Thứ tự `ORDER BY as_of DESC, kind_rank, recorded_at DESC`, với kind_rank: measured > declared > estimated > assumed, và `scenario` bị loại ngoài mô phỏng. Khi dòng mới nhất là `retracted` của feed: quan sát **từ nguồn khác** (manual/file) còn trong `max_age` vẫn được chọn. Rút chỉ vô hiệu quan sát cùng nguồn. Cần quyết định cho trường hợp `grant_revoked` (Q-5).

**C-4 · Bổ sung `entered_by`/`actor_ref` cho sổ quan sát.** Sổ mẫu 8.1 nào cũng có `actor_ref`. Với `source = api`, đặt subject của tài khoản dịch vụ job. Gộp vào C-1.

**C-5 · Sửa tài liệu và chú thích sai số migration.**
- `docs/design/mkg.md` §5.8 ghi engine = **000038**, thực tế là **000037**.
- Chú thích đầu `migrations/mkg/000037_mkg_engine_observation.up.sql` dòng 19 ghi "Số 000038 … 000037 của W5.6", ngược với thực tế (W5.6 = 000038). Chú thích migration không sửa được mà không đổi tổng kiểm, nên chỉ sửa tài liệu và ghi chú ở ADR-0036.
- ADR-0036 §3 ghi chủ kho là "`mkg/fieldops` (hoặc module do v1.3 chỉ định)". Cần cập nhật: v1.4 không chỉ định, repo chọn `mkg/engine`.

**C-6 · `docs/design/mkg.md` §6 RB-3:** "tài xế/nhân viên MKG: định danh ở vault tenant MKG" → sửa theo v1.4 §5.1(2): schema định danh riêng trong cụm `mkg` (dùng lại mã vault theo R-MOD-10).

**C-7 · Tham chiếu nhân sự.**
- `crew_event.member_refs` (subject tài khoản) phải chuyển sang `worker_ref` khi có W8.4: thêm sổ phụ `crew_member_worker` hoặc thêm cột NULL theo ADR-0012. Không sửa dòng cũ.
- `trip_event.driver_ref` theo cùng hướng.

**C-8 · Dữ liệu vị trí (D-12).** `roll_count_location` là **sổ chỉ ghi thêm**, không xóa được, nên không đặt được thời hạn lưu cho dữ liệu vị trí người. Đề xuất:
- (a) Tạm dừng thu GPS người đếm ở production tới khi có D-12 (cờ tính năng).
- (b) Về đích: tọa độ chính xác lưu ở bảng thường có thời hạn lưu, hoặc cụm `ts` có phân vùng xóa theo hạn. Sổ chỉ giữ `gps_status` và mã ô lưới thô, đủ làm bằng chứng "có mặt tại thửa".
- (c) Áp quy tắc "chỉ thu trong giờ làm việc" ở app hiện trường và `cmd/ingest`.
- Cần ADR, và đụng ADR-0012 vì không xóa được dòng sổ.

**C-9 · Đối soát lô DIRECT.** CHECK ở `mkg_measurement.reconciliation` cấm mọi trạng thái khác `not_applicable` cho DIRECT, nên không có chỗ cho định mức riêng. Không sửa dòng cũ. Thêm `route` mới hoặc `reconciliation` v2 với `direct_status`, `direct_loss_kg`, `direct_loss_norm_version_id`. Thêm stream `loss_norm:direct` cho `loss_norm_version`, và nới `param_kind`. Các CHECK cũ chỉ đổi được bằng DROP/ADD CONSTRAINT, không đụng chuỗi băm. Cần kiểm `migratecheck` có cho phép không (Q-9).

---

#### 3. Gói việc đề xuất (tinh chỉnh W8.3 / W8.4 / W8.5)

Nguyên tắc chia: tách phần **làm được ngay** (không chờ giá trị kinh doanh, theo v1.4 §9) khỏi phần **chờ ký**.

**Rủi ro số migration (mọi gói):** plan giữ chỗ mkg 000027–000032 cho W8.4/W8.5. Nhưng mkg đã áp tới **000038**, và bộ chạy mặc định vẫn là `golang-migrate` một kỳ phát hành (ADR-0038 §4). Bộ chạy này **bỏ qua im lặng** tệp có số nhỏ hơn số đã áp. Vì vậy:
- Hoặc cấp số ≥ 000039,
- hoặc chỉ dùng 000023–000032 sau khi `AG_MIGRATE_RUNNER=v2` thành mặc định.

Đề xuất: cấp số mới ≥ 000039 và bỏ giữ chỗ cũ (Q-8).

| Gói | Nội dung | Phụ thuộc | Migration | Làm ngay? | Cỡ |
|---|---|---|---|---|---|
| **W8.2b · Kho quan sát v2** | C-1…C-4. Đường ghi `manual` (API nội bộ, form theo danh mục, bắt buộc `as_of` + đơn vị). Danh mục v1 dạng tệp hợp đồng `contracts/engine/catalog.v1.json` (khóa, đơn vị, kiểu phạm vi, `max_age`, bắt buộc/tùy chọn theo chế độ và giai đoạn, bộ ước tính, mặc định `assumed`), có `contractcheck`. Port mới `Observations.Select(ctx, tid, catalogVersion, mode, at)` cài quy tắc 7.3 #2–#4. Từ chối khi thiếu khóa bắt buộc (lỗi có danh sách khóa) | W8.2 | mkg 000039 | **Có**. Giá trị `max_age` lấy từ v1.4 §7.6 làm mặc định tạm, gắn `provisional` (D-2 chốt sau) | M |
| **W8.2c · Ảnh chụp đầu vào engine** | Sổ `mkg_engine.input_snapshot` và `input_snapshot_key` (khóa, id quan sát đã dùng, `as_of`, **tuổi lúc chụp**, `kind`, quy tắc chọn, cờ `assumed`/`stale_excluded`/`optional_skipped`) cộng phiên bản các sổ tham số chính sách đã đọc (`param_version` của logistics, hub, fieldops, measurement, costing qua port). Ảnh chụp bất biến ra kho đối tượng kèm SHA-256 (v4.3 §10). Hợp đồng `contracts/engine/snapshot.v1.schema.json` có `catalog_version`. Pydantic `extra="forbid"`, engine từ chối `catalog_version` > mức hỗ trợ | W8.2b | mkg 000040 | **Có** | M |
| **W8.3a · Cổng tệp mẫu chuẩn MKG** | Mẫu CSV/XLSX do MKG phát hành (theo danh mục), tải lên ở api-internal, `staging → người xác nhận → ghi sổ`, `source = file`, `source_ref = tệp#dòng`. Dùng lại `shared/docfile` | W8.2b | mkg 000041 (bảng đệm `observation_intake`) | **Có** | M |
| **W8.3b · VFT: cổng nhận + bộ ước tính tồn (D-3)** | (1) Khóa `plant.stock` (measured, mốc), `plant.consumption_plan` (declared, có `valid_from/to` theo ngày), `plant.yard_capacity`, `plant.outages`. (2) Bộ ước tính Go thuần ở `engine/domain` theo công thức §7.5. Σ CUSTOMER_IN qua `measurement/port`, độ bất định cộng dồn, chặn [0, sức chứa] kèm cảnh báo. (3) Kênh đẩy API nội bộ cho VFT (khóa API + mTLS, cùng mã cổng tích hợp theo R-MOD-10) | W8.2b, W8.2c; kênh và chữ ký chờ thỏa thuận VFT (H-9) | (dùng 000039) | **Bộ ước tính và nhập tay/tệp: có. Kênh VFT: chặn** (thỏa thuận VFT, W8.6) | M |
| **W8.3c · Dịch vụ engine nội bộ (khung)** | Đơn vị triển khai Python thứ hai ở vùng mkg (vhost `mkg.internal`, ADR-0018). Đọc ảnh chụp W8.2c, ghi kết quả có `optcore_version` (phần ADR-0036 W8.1 để lại). `check_flows.py` và NetworkPolicy: không có luồng sang nền tảng | W8.1, W8.2c | — | **Có** | M |
| **W8.3d · Chế độ mô phỏng MKG (D-1)** | Mở rộng `lib/optcore`: nút điểm gom (sức đệm, năng lực trung chuyển), cỡ ghe theo cung đường, MILP T1 theo ngày (HiGHS) dùng làm lõi chung cho kế hoạch cuộn, lịch thu hoạch thật theo vùng, Monte Carlo thời tiết có hạt giống (tất định), tối ưu tồn an toàn. Chạy kịch bản `assumed` theo v1.4 §9.1 (ba mức máy cuộn, ba mức ghe). **Không đổi byte golden** của App SIM: mô-đun mới, không sửa `evaluate` | W8.3c; đồ thị thủy (producer `waterway_routes` + thuộc tính cỡ ghe, tĩnh không: W8.3h) | — | **Có** với dữ liệu giả định. Kết quả thật chờ D-5 và đồ thị | L |
| **W8.3e · Làm thử T2a/T2b (D-6)** | ADR phụ thuộc OR-Tools (Apache-2.0, kiểm wheel nhị phân theo ADR-0016). Bộ sinh dữ liệu tổng hợp cỡ Phase 1 (310–470 chuyến ghe nhỏ, 83–124 chuyến ghe lớn mỗi ngày). Đo p95 sửa nhanh một hành lang, thời gian giải lại đầy đủ, khoảng chênh chi phí. Cả hai chế độ sức đệm. Viết báo cáo đạt/không đạt | ADR mới | — | **Có**. Ưu tiên cao vì là rủi ro kỹ thuật lớn nhất (§4.2) | M–L |
| **W8.3f · Kế hoạch đề xuất, vùng đóng băng, tự duyệt (D-7)** | Sổ `mkg_engine.plan_proposal` (bản đề xuất có phiên bản, phạm vi hành lang, delta chi phí, delta tồn an toàn dự báo, cam kết thuê mới) và `plan_decision` (`human` / `auto`, người duyệt khác người đề xuất, lý do chỉnh tay, `reverted_by`). Bộ kiểm 5 điều kiện đồng thời (thuần, có kiểm thử). Vùng đóng băng kiểm với `logistics/port.Trips` (đang chạy, hoặc khởi hành < 2 giờ). Bộ tham số `engine.replan.v1` và `engine.autoapprove.v1` ở sổ tham số kiểu R-56, đổi cần hai người. Metric "tỷ lệ tự duyệt bị đảo ngược". Áp đề xuất đã duyệt thành chuyến hoặc phân công qua API nội bộ của `logistics`/`fieldops` | W8.3c; W8.5c (chặng ghép ghe) để có lệnh ghép | mkg 000042 | **Mã: có. Bật: chặn** (X% D-7, ký) | L |
| **W8.3g · Kế hoạch cuộn 7–14 ngày + T3 bán tự động** | Chạy hằng ngày trên ảnh chụp thật. T3 CP-SAT gợi ý phân công tổ, tận dụng `crew_assignment.mode = suggested` đã có | W8.3d, W8.3f, W8.5d (máy cuộn) | — | Chặn bởi dữ liệu thật (A7, k) | L |
| **W8.3h · Producer feed + thuộc tính đồ thị** | Producer `waterway_routes`, `season_harvest_dates` ở `apiclient_feed_build`. Thuộc tính đoạn luồng: cỡ ghe tối đa, tĩnh không cầu, ngưỡng mực nước (pf migration). Khóa quan sát tĩnh không (nhập tay) | W5.3 | pf 0001xx | **Có** (phía nền tảng) | M |
| **W8.4 · `mkg/workforce`** | Schema định danh riêng trong cụm mkg (mã hóa mức trường, HMAC, xóa bằng hủy khóa; dùng lại mã vault). `worker_ref` bút danh. Phân ca, chấm công, sản lượng khoán. Xuất bảng công (mã NV kế toán + công + sản lượng). Đọc định danh ghi nhật ký. C-7 | W2.6; pháp chế (H-10, D-8, quy tắc 5) | mkg ≥ 000043 (thay 000027–28) | **Chặn** (pháp chế: trường, căn cứ, hạn lưu). Có thể làm trước khung `worker_ref` và kho định danh rỗng | L |
| **W8.5a · `measurement`: CONSOLIDATION + lô đi thẳng (D-4)** | Điểm cân thứ tư (DROP/ADD CHECK ở các bảng liệt kê §1.5, `route`/`point` mới ở `weigh_ticket`, `weighing_session`, `device`; sự kiện nội bộ `weigh_ticket.recorded` v2; OpenAPI; web-internal). `weighing_mode` theo điểm gom. Chế độ `sampling` (cỡ mẫu, sai số) và `roll_estimate` có phiên bản (đóng băng ước lượng `KgPerRollAsOf` thành phiên bản). Đối soát riêng cho DIRECT (C-9) với `loss_norm:direct`. Cờ độ tin cậy | W8.5b; C-6 (mẫu cân ở điểm gom) | mkg ≥ 000044 | **Lược đồ: có. Định mức: chờ khách** (C-4/C-5/C-6) | L |
| **W8.5b · Gia phả dùng chung cho ghép ghe** | Cho nút trộn không gắn cơ sở: hoặc `facility.kind` thêm `consolidation_point` (điểm gom là cơ sở có `buffer_capacity`, `weighing_mode`) để lô ghe lớn là `lot` có `sources` kind = `lot`, tỷ lệ theo khối lượng hoặc ước theo `weighing_mode`; hoặc tách bảng gia phả chung. **Đề xuất cách thứ nhất**: một mô hình, `hub/port.LotOrigins` không đổi. Nới quan hệ 1 lệnh ↔ 1 lô (`lot_collection_current`) thành N ↔ 1 qua nút trộn. MRV DIRECT dùng tỷ lệ của nút trộn | W8.5c | mkg ≥ 000045 | **Có** | M–L |
| **W8.5c · `logistics`: ghe thuê + chặng ghép ghe** | Cam kết thuê (đặt chuyến, hợp đồng khung so với theo chuyến; là cam kết tiền nên dùng cho điều kiện 3 của tự duyệt). Khóa quan sát "ghe khả dụng theo cỡ/ngày" (nhập tay, tệp). Loại chặng `transshipment` tại điểm gom, gắn N chuyến ghe nhỏ → 1 chuyến ghe lớn, ghi nút trộn qua `hub/port` | W8.2b | mkg ≥ 000046 | **Có** | M |
| **W8.5d · `fieldops`: máy cuộn, hỏng hóc** | Danh mục máy cuộn có phiên bản (500+), gán máy cho tổ, sự kiện hỏng/sửa. Khóa quan sát nội bộ "đội/máy/người khả dụng" (`max_age` 1 ngày) sinh từ sổ, không nhập lại | W8.4 (để gắn người lái) | mkg ≥ 000047 | **Danh mục và hỏng hóc: có. Gắn người lái: chờ W8.4, D-12** | M |
| **W8.5e · `orders`: nguồn cung hai độ tin cậy** | Tỷ lệ đội MKG / HTX, năng suất máy cuộn là tham số chính sách. Mua cuộn HTX đã có | — | — | Có (nhỏ) | S |
| **W8.6 · Chữ ký số (v4.3 ADR-24 → ADR repo mới)** | Phong bì ký tách rời trên lô `ingest_batch.v1` cho `roll_count_statement`, `htx_roll_confirmation` (bắt buộc), `collection_schedule` (nên có), và luồng VFT. Lưu chữ ký + chứng thư vào sổ song phương (cột NULL theo ADR-0012). Kiểm ở cổng. Phương án "ký thay mặt HTX có ủy quyền" | ADR mới (số ≥ 0039), pháp chế, nhà cung cấp chứng thực trong nước | pf + mkg | **Chặn** (pháp chế, CA) | L |
| **W8.7 · Dữ liệu vị trí (D-12)** | C-8 | Pháp chế | mkg + ts | **Chặn** quyết định; cờ tạm dừng làm được ngay | S–M |
| **D-9 · Đồng bộ tài liệu** | Đưa v4.3 và MKG v1.4 vào `docs/kien-truc/`. ADR mới cho v4.3 ADR-21…24 (đánh số ≥ 0039, bảng ánh xạ). Cập nhật ADR-0036 (W8.2 đã chọn `mkg/engine`; H-9 đã có câu trả lời một phần), `cau-hoi-ton-dong.md` H-8/H-9, plan W8.x theo bảng này. Sửa C-5, C-6 | — | — | **Có** | S |

**Thứ tự gợi ý:**
- D-9 → W8.2b → W8.2c → W8.3c, chạy song song với W8.3e (làm thử D-6, độc lập).
- Sau đó W8.3a, W8.3b (bộ ước tính), W8.3h → W8.3d.
- Song song: W8.5c → W8.5b → W8.5a.
- Cuối: W8.3f → W8.3g. W8.4, W8.6, W8.7 khi pháp chế trả lời.

---

#### 4. Câu hỏi mở

1. Migration mkg 000037 đã áp ở môi trường dùng chung nào chưa (staging, UAT)? Nếu chưa thì sửa thẳng để đổi `kind` → `key`, gọn hơn C-1. Nếu rồi thì giữ `kind` vật lý và thêm `epistemic_kind`.
2. `max_age` ở danh mục D-2 có thay hẳn `freshness.max_age` của hợp đồng feed trong quy tắc chọn không? Đề xuất: danh mục quyết định chọn; hợp đồng feed chỉ cho cảnh báo vận hành.
3. MKG có chấp nhận `mkg/engine` là module mới thứ hai, ngoài `mkg/workforce`, trái câu "từ năm còn một" ở §5 không? Nơi đặt sổ kế hoạch đề xuất cũng ở đây.
4. Quy tắc 1 (§7.3): đầu ra bộ ước tính được **ghi sổ** thành quan sát `estimated` hay chỉ tính lúc chụp và ghi trong ảnh chụp? Đề xuất: chỉ ghi trong ảnh chụp, tránh chuỗi ước tính dựa trên ước tính.
5. Khi bên cấp thu hồi cấp quyền (`grant_revoked`), có xóa quan sát `manual`/`file` cùng entity do MKG tự nhập không? Lịch sử quan sát của bên đã thu hồi giữ hay xóa (ADR-0036 ghi "chờ A7")?
6. Bộ ước tính (§7.3 #1) neo vào "số đo hợp lệ mới nhất". Nếu số đo mốc đã quá `max_age` 7 ngày thì bộ ước tính còn chạy với độ bất định tăng dần, hay khóa coi như thiếu và từ chối chạy?
7. X% của tự duyệt, các ngưỡng kích hoạt (48 giờ, 4 giờ, nửa ngày): ai đề xuất và ai là người thứ hai duyệt? (D-7, `[KÝ]`.)
8. Cấp số migration mới ≥ 000039 và bỏ giữ chỗ 000027–000032, hay chờ bộ chạy v2 (ADR-0038) thành mặc định?
9. `migratecheck` có cho phép DROP/ADD CHECK trên bảng sổ đã phân vùng (thêm `CONSOLIDATION`, nới `route`, `param_kind`) không, hay phải dùng sổ v2?
10. Điểm gom có phải là "cơ sở" (`facility.kind = consolidation_point`) trong `mkg/hub` không, hay chỉ là nút mạng của `logistics`? Câu trả lời quyết định W8.5b.
11. Chế độ `roll_estimate`: "kg/cuộn bình quân có phiên bản của HTX" là tham số có duyệt (sổ tham số) hay là ước lượng học tự động `KgPerRollAsOf` được đóng băng theo kỳ?
12. Dữ liệu GPS người đếm cuộn đang thu vào sổ không xóa được (`roll_count_location`): tạm dừng ở production tới khi có D-12? Có giữ tọa độ trong sổ hay chuyển ra kho có thời hạn?
13. `crew_event.member_refs` hiện là subject tài khoản Keycloak. 1.500 công nhân hiện trường có tài khoản đăng nhập không (ADR-23 v4.3: thiết bị đăng ký + PIN), hay chỉ trưởng tổ? Câu trả lời quyết định `worker_ref` có tách khỏi subject tài khoản không.
14. Kênh VFT: VFT đẩy qua API nội bộ, gửi tệp mẫu chuẩn, hay cả hai? Ai cấp chứng thư số cho VFT? Kế hoạch tiêu thụ có `valid_from/to` theo ngày hay theo lò?
15. Thủy triều và dự báo mưa: nguồn trong nước nào? ADR-0007 có bắt mọi lời gọi ra ngoài đi qua proxy bất kể nội dung không (v1.4 §7.7 chưa đọc)?
16. ADR-0035 dùng `grant_class = aggregate` thay vì thêm loại vào `ShareKinds`/CHECK như v1.4 §7.7. MKG có chấp nhận đây là cách hiện thực D-5 không?
17. Đánh số ADR khi đưa v4.3 vào repo: ADR-21…24 của v4.3 trùng số ADR-0021…0024 của repo. Đề xuất đánh số mới kèm bảng ánh xạ trong `docs/kien-truc/`. Ai duyệt?
18. Mô phỏng MKG (W8.3d) có được chạy ngay trên đồ thị đường thủy hiện có (`pf_waterway`, chỉ có `waterway_class`) không, hay chờ thuộc tính cỡ ghe và tĩnh không (W8.3h)?
19. Ngưỡng tiêu chí đạt của D-6 (p95 sửa nhanh, khoảng chênh chi phí chấp nhận được) do ai định trước khi chạy?

---

## Phụ lục C — D3 — KN Trồng trọt (Final) và Hành chính – Tổng hợp v3.0

### D3 — KN Trồng trọt (bản Final so với v7.7) và Hành chính - Tổng hợp v3.0: đối chiếu với mã hiện tại

> Phạm vi: chỉ đọc repo `C:\Dev\claude-skills-main\Agrigreen_demo_v2` @ `27afd593` (HEAD hiện tại; nhánh đang checkout tên `main`, cùng commit với `dev` theo đề bài). Không sửa repo, không đụng worktree `C:\Users\manht\agwt\*`.
> Ngày phân tích: 09/10/2026.

**Nguồn đã trích xuất** (text + bảng dạng dòng, dùng lại được):
- `analysis/src/brd-trongtrot-final.md` ← `BRD_He_thong_Khuyen_nong_Module_Trong_trot_Final.docx` (sửa lần cuối 03/10/2026 14:50, 35 ảnh)
- `analysis/src/brd-hanhchinh-v3.md` ← `BRD_Khuyen_nong_Module_Hanh_chinh_Tong_hop_v3_0.docx` (v3.0, 07/10/2026)
- `analysis/src/cov-tt.txt`: kết quả so khớp từng câu của bản Final với `docs/brd/kn-trong-trot-v7.7.md` (4-gram, ngưỡng 0,30)
- Script: `scripts/ex.py` (docx → md), `scripts/cov.py` (so độ phủ câu)

Trong repo đã đọc: `docs/brd/kn-trong-trot-v7.7.md`, `kn-v7.7-gap.md`, `kn-v7.7-plan.md`, `docs/uat/kn-v7.7-acceptance.md`, `docs/dev/modules.md` (§ cap/workflow, cap/forms, cap/evidence, cap/task), `app/KN/app.yaml`, `module.yaml` của mọi module `pf/*`, commit W3.3 (`8a20a0c8`), W6.2 (`377cdcac`), P1–P8.

---

#### 1. KN Trồng trọt: bản Final so với v7.7

##### 1.1 Kết luận: bản "Final" chính là v7.7, không có thay đổi yêu cầu

| Kiểm tra | Kết quả |
|---|---|
| Trang bìa | "Phiên bản: v7.7 • Ngày: 01/10/2026", người lập Vũ Phương Mai, giống bản repo đã tóm tắt |
| Lịch sử thay đổi | Dừng ở **v7.7** (thêm Hình 3d/3e/3f). Không có dòng v7.8 hay "Final" |
| UR | 40 UR, cùng mã và cùng nội dung (UR-CH-01, UR-TW-01..02, UR-TI-01..07, UR-XA-01..21, UR-AD-01..07, UR-KT-01..02) |
| FN | 25 FN, cùng mã (FN-CH-00.01, FN-TW-01.01..02, FN-TI-02.01..07, FN-XA-03.01..08, FN-AD-04.01..07); mô tả, luồng, quy tắc, tiêu chí chấp nhận khớp |
| Bảng 14 mẫu T01–V06, ma trận phân quyền (Mục V.2), NFR (Mục VI) | Khớp từng dòng (khóa 15 phút/5 lần, PIN 6 số, offline 72 giờ, phiên 4 giờ/24 giờ, nhắc < 1 phút, ≥ 99,5 %, RPO 24 giờ, RTO 4 giờ, "Tháng 7", nhóm lượng giống ≤ 80 / 80–100 / 100–150 / > 150) |
| Mục V.3 (menu theo vai trò), Mục VII (điểm đã xác nhận/còn mở) | **Vẫn không có** trong bản Final, đúng như ghi chú [PT] trong `kn-trong-trot-v7.7.md` |
| So khớp tự động | 187 câu có độ phủ 4-gram < 0,30. Rà tay thì đều là câu diễn đạt lại, chú thích hình, lịch sử phiên bản hoặc mô tả wireframe. Không có quy tắc nghiệp vụ hay tiêu chí chấp nhận mới (`analysis/src/cov-tt.txt`) |
| Ảnh | Docx có 35 ảnh, repo có 25 ảnh (`docs/brd/kn-v7.7-img`). Chênh lệch là ảnh As-Is/To-Be, logo và các Hình a/b. Không đổi yêu cầu |

**Diff yêu cầu: không có UR, FN, quy tắc, màn hình, báo cáo hay vai trò nào mới, bị đổi hoặc bị bỏ.** Tuy vậy, bản Final vẫn còn mấy lỗi văn bản từ v7.7 (cần BA sửa, xem câu hỏi Q-TT-1):
- FN-TI-02.01 B3: "mở FN-TI-05", đúng ra là FN-TI-02.03.
- FN-AD-04.06 "Người dùng / Menu": `Admin · Phòng ban ›  › Mẫu báo cáo` thiếu tên phòng ban.
- NFR Bảo mật ghi "phân quyền theo Mục VI", đúng ra là Mục V.
- FN-CH-00.01 dẫn tới "Mục V.3" nhưng mục này không có.

##### 1.2 Trạng thái từng FN so với mã hiện tại (`27afd593`)

Trạng thái lấy từ `docs/uat/kn-v7.7-acceptance.md` (sửa lần cuối 03/10, commit `6529710d`), đối chiếu lại với mã sau W3.3/W6.2 (09/10). **đạt** = có mã và kiểm thử chứng minh tiêu chí chấp nhận · **một phần** · **thiếu** · **lệch** = mã cố ý khác chữ BRD (theo quyết định nội bộ).

| FN | Trạng thái | Mã chính | Còn thiếu / ghi chú |
|---|---|---|---|
| FN-CH-00.01 Đăng nhập, chọn vai trò & phòng ban | một phần + lệch | `internal/platform/accounts/app/kn_context.go`, `accounts/handler/kn.go` (`/v1/me/contexts`, `/v1/me/context`, header `X-AG-Context`); `party/app/kn_menu.go`, `party/domain/menu.go` (`GET /v1/menu`, kn.menu/v2); web `apps/web-ops/src/app/kn/**`; mobile `apps/field-app/lib/application/kn_context_service.dart`, `kn_menu_service.dart`, `ui/kn/kn_context_picker_screen.dart`; realm `docs/brd/kn-p1-realm-patch.md` | UAT vẫn ghi "P7 vẽ menu mobile từ `GET /v1/menu`" là còn thiếu, nhưng `kn_menu_service.dart:145` đã gọi `/v1/menu` (commit `7d668f48`, 02/10). **UAT chưa cập nhật, cần xác minh.** "Còn n lần thử" cần SPI Keycloak (N-10/X-08). **Lệch (A3):** khóa hẳn sau 3 lần khóa tạm trong 24 giờ, BRD chỉ nói khóa tạm 15 phút |
| FN-TW-01.01 Dashboard TW | một phần (hạ tầng) | `extension/reportcycle/app/dashboard.go`; web `/kn/dashboard` | Chưa kiểm tải 500 người dùng trên staging (`tests/kn77/load_test.go` mới chạy cục bộ) |
| FN-TW-01.02 Báo cáo TW | đạt (cơ chế) | `reportcycle/app/consolidate.go`, `reports/render/formgrid.go`; web `/kn/bao-cao/{tuan,thang,vu,nam}` | Báo cáo năm (Q-07) và số liệu lịch sử để so cùng kỳ năm trước (Q-20) chờ khách |
| FN-TI-02.01 Dashboard tỉnh | đạt | như trên + nút nhắc | — |
| FN-TI-02.02 Phát hành biểu | đạt (cơ chế) | `reportcycle/app/batch.go`; web `/kn/nhiem-vu/phat-hanh` | Quy tắc chia kỳ Q-17 đang tạm; màn chưa nạp Tỉnh giao từ Excel (máy chủ đã nhận `provincial_source`) |
| FN-TI-02.03 Trạng thái nộp, nhắc | đạt (cơ chế) | `reportcycle/app/task.go`, `job.go`; `pf/notify` kiểu `report_*` (pf 000073) | Tham số tự nhắc Q-15 tạm 24 giờ; chưa đo "< 1 phút" trên FCM thật |
| FN-TI-02.04 Rà soát, chốt/mở kỳ | đạt | `reportcycle/app/review.go`, `forms_mirror.go` (ghi song song vào `pf/forms`, W6.2) | Chốt và mở lại ghi song song ảnh chụp `kn.report_period/v1` vào `pf_forms` |
| FN-TI-02.05 Tổng hợp các xã | một phần (chờ khách) | `reportcycle/app/consolidate.go`, `store/consolidation.go` | Chưa đối chiếu Excel xuất với tệp gốc (Q-01) |
| FN-TI-02.06 Chi cục | một phần + lệch | `reports/app/dispatch.go`, `dispatch_link.go`, `reports/dispatch/`; `tests/kn77/dispatch_link_integration_test.go` | **Lệch (A4):** vượt giới hạn đính kèm thì gửi liên kết tải 7 ngày do api-platform phục vụ, chưa tạo Google Sheet (ADR-0026 "Đề xuất", Q-14). Hộp thư Chi cục chờ Q-09 |
| FN-TI-02.07 Duyệt lịch | đạt | `extension/app/schedule.go`, `schedule_job.go`, `domain/schedule.go`, `store/schedule.go` | Người duyệt `truong_phong`; lịch cán bộ tỉnh duyệt chéo `peer` (tạm, Q-05/Q-16) |
| FN-XA-03.01 Công việc cần làm | đạt | `reportcycle/app/task.go`; web `/kn/nhiem-vu/cong-viec`; mobile `ui/kn/kn_tasks_screen.dart` | — |
| FN-XA-03.02 Nhập 14 biểu | một phần (chờ khách) | `reports/app/forms.go` (engine lưới), `reportcycle/store/submission.go`; web `src/kn/grid`; mobile `kn_report_entry_screen.dart` | 12/14 mẫu là mẫu tạm, chờ tệp gốc (Q-01) |
| FN-XA-03.03 HTX & nông hộ | đạt | `party/app/kn_collect.go`, `party/handler/kn_collect.go`; dữ liệu đã sang `pf_coop` qua `cooperativeport` (W3.3) | Đường dẫn mã trong UAT còn tên cũ; logic không đổi |
| FN-XA-03.04 Máy móc HTX | đạt | `mechanization/{app,domain,handler,store}/machine_count.go` | — |
| FN-XA-03.05 Vẽ thửa | một phần | `cooperative/plot/handler/kn.go` (`/v1/kn/plots`, W3.3); mobile `boundary_screen.dart`, `kn_plots_screen.dart` | **Web chỉ có danh sách**: `src/kn/cycle/screens/DataScreens.tsx` `PlotsScreen` kèm `GapNotice` ("vẽ lô mới dùng app di động"). BRD yêu cầu Cấp Xã/Admin vẽ được cả trên Web |
| FN-XA-03.06 Vật tư | đạt | `extension/material/*` (`/v1/kn/materials`) | ĐVT ≤ 4 chữ số lẻ (tạm) |
| FN-XA-03.07 Báo cáo của xã | đạt | `reportcycle/app/lists.go`; web `/kn/bao-cao/cua-xa` | — |
| FN-XA-03.08 Đăng ký lịch | đạt | như FN-TI-02.07; mobile `work_schedule_screen.dart` | — |
| FN-AD-04.01 ĐVHC | đạt (dữ liệu, API) | `masterdata` admin_unit; web `/kn/quan-tri/don-vi-hanh-chinh` | — |
| FN-AD-04.02 Cây 3 cấp | một phần | `party/app/kn_tree.go`, `kn_region.go`; web `/kn/quan-tri/co-cau-to-chuc` | Màn chưa có "di chuyển Nút xã sang tỉnh khác"; Khu vực chờ Q-04 |
| FN-AD-04.03 Phòng ban | một phần | `party/app/kn.go`; web `/kn/quan-tri/phong-ban` | Chưa đếm bản ghi theo `department_code`; tên 6 phòng ban chờ Q-02 (web còn `phong_ban_5`, `phong_ban_6`) |
| FN-AD-04.04 Tài khoản | đạt (UAT); UR-AD-04 một phần | `accounts/app/kn.go`, `kn_log.go`, `kn_vault.go` | Chưa nạp Excel nhiều vai trò; tab Phiên đang hoạt động/Lịch sử đăng nhập chưa đọc sessions/events của Keycloak. **Lệch:** "gửi lại mật khẩu tạm" làm thành liên kết kích hoạt/đặt mật khẩu (gap §4.4, Q-03) |
| FN-AD-04.05 Danh mục nông nghiệp | một phần | `masterdata` catalog (crop/material/production_model/report_type) | Đếm "biểu đang dùng" (`reports/app/form_usage.go` đã có, còn nối); danh mục thật chờ Q-01 |
| FN-AD-04.06 Mẫu báo cáo | một phần (UAT) | `reports/app/forms.go`, `handler/forms.go`, `render/formgrid.go`; web `/kn/phong-ban/[dept]/mau-bao-cao/**`; worker `formimport` | **"Báo cáo đột xuất":** có trong menu (`party/domain/menu.go:109`, route `/kn/bao-cao/dot-xuat`) nhưng **không có trang**, rơi vào `app/kn/[...slug]/page.tsx` → `ComingSoon`. Đây là chỗ thiếu thật, UAT không ghi |
| FN-AD-04.07 Menu các cấp | đạt | `accounts/app/kn_support.go` | — |

**NFR:** N-01 (sẵn sàng), N-02..N-04 (sao lưu/RPO/RTO trên cụm thật), N-05 (500 người dùng), N-06 (3G), N-13 (nhật ký truy cập ≥ 1 năm), N-14 (ca vàng Excel, xuất Word Q-10) chưa đạt. Phần lớn là việc hạ tầng hoặc chờ khách.

##### 1.3 Điểm lệch so với chữ BRD và nợ kiến trúc sau W3.3/W6.2

1. **Hai đường vòng đời báo cáo.** KN v1 (`/v1/extension/report-*`, sổ `pf_extension.*`) vẫn là nguồn chính; `pf/forms` chỉ nhận ghi song song lúc chốt/mở lại (`forms_mirror.go`). Phần "chưa làm" của W6.2 trong `docs/dev/modules.md` gồm: kỳ KN native trên `/v1/forms`, web-ops chuyển sang `/v1/forms`, Chi cục đọc ảnh chụp, gộp engine lưới của `pf/reports` vào kiểu trường `forms.field_types`. **Muốn HCTH (đánh giá, kế hoạch nhân lực) dùng `cap/forms` thì nên làm phần này trước** (xem gói T2).
2. **UAT và gap đã cũ sau W3.3.** Mã HTX, hộ, thửa nay ở `pf/cooperative` (`pf_coop`, pf 000131–000133) nhưng `kn-v7.7-acceptance.md` và `kn-v7.7-gap.md` vẫn trỏ `party`/`geo`. Dòng UR-KT-02/FN-CH-00.01 về menu mobile cũng có vẻ đã cũ.
3. **Các lệch đã có quyết định nội bộ (A1–A5, 03/10):** khóa hẳn sau 3 lần khóa tạm; liên kết kích hoạt thay mật khẩu tạm; liên kết tải 7 ngày thay Google Sheet; tối thiểu 3 điểm khi vẽ thửa. Khách hàng **chưa** ký nhận các quyết định này (Q-TT-2).
4. **`cap/task`:** `extension/reportcycle` (công việc nộp biểu) được cố ý không tính là `cap/task` (`modules.md` §W6.6). Khi HCTH có "phiếu cần làm", tức là có bên dùng thứ hai cho việc cần làm có hạn và nhắc nhở, thì nên xét lại quy tắc ba.

---

#### 2. Hành chính - Tổng hợp v3.0 (Module 2)

##### 2.1 Bản chất module

BRD Module 2 thực chất là **quản lý nhân sự (HRM) của Phòng HCTH TTKN An Giang**: khoảng 494 viên chức cùng cán bộ khuyến nông cấp xã. Module gồm hồ sơ nhân sự tối giản, thông tin vào làm/nghỉ việc, nâng bậc (NLTX, NLTH, TNVK), quyết định nâng bậc, đánh giá xếp loại quý theo Mẫu số 05/06 kèm phiếu đánh giá các cấp, kế hoạch nhân lực năm, 6 biểu báo cáo Sở, danh bạ Contact dùng chung, kho tài liệu nhân sự và phân quyền hồ sơ.

**Ngoài phạm vi (BRD I.3):** văn thư/lưu trữ, liên kết văn phòng điện tử VNPT, tài sản, tài chính, đào tạo bồi dưỡng, quy trình thi đua khen thưởng đầy đủ, hợp đồng lao động, biến động, kỷ luật, Hội đồng, **luồng trình ký/phê duyệt** (ký trên giấy, hệ thống chỉ lưu kết quả), viên chức tự cập nhật hồ sơ, liên thông phần mềm của Sở, nạp Excel trên giao diện (Mekong Green chuyển đổi một lần), tự tạo mẫu báo cáo, cấu hình quy tắc nâng bậc trên giao diện. Vì vậy **không có** module văn bản, công việc, họp hay tài sản để làm.

##### 2.2 Vai trò

| Vai trò BRD | Ánh xạ sang mô hình hiện có (P1) | Ghi chú |
|---|---|---|
| Admin | `admin` (context role), MFA | "Làm được mọi chức năng của Cán bộ HCTH"; menu "Quản trị", "Master data" |
| Cấp Tỉnh - HCTH | `kn_tinh` + phòng ban HCTH (chưa seed) | Phạm vi nhân sự toàn TTKN tỉnh |
| Cấp Tỉnh - lãnh đạo các phòng | `kn_tinh` + `mgmt_level` ∈ {trưởng, phó, phụ trách} của **mọi** đơn vị (phòng, trạm, trại) | **Mới:** phạm vi là người thuộc đơn vị mình và cán bộ xã được phân công quản lý (HS-37). Hiện chỉ có mã `chuyen_vien`, `pho_truong_phong`, `truong_phong` (`accounts/port/kn.go:32-34`); BRD FN-04 dùng "Lãnh đạo đơn vị / Trưởng phòng / Chuyên viên - Cán bộ" và còn có "Phụ trách" |
| Cấp TW | `kn_tw` | Chỉ xem Contact (họ tên, năm sinh, SĐT) |
| Cấp Xã | `kn_xa` | Contact 3 trường; tự đánh giá theo phiếu |
| Viên chức không có tài khoản | **không có** | Điền phiếu qua liên kết kèm OTP (email/SĐT trong hồ sơ), hoặc lãnh đạo phòng nhập thay kèm bản scan |

##### 2.3 Danh mục chức năng (21 FN)

| FN | Tên | UR | Dữ liệu chính | Báo cáo / tích hợp | Vai trò ghi |
|---|---|---|---|---|---|
| FN-01..05 | ĐVHC, cây 3 cấp, phòng ban, tài khoản, đăng nhập | UR-ADM-01..03 | dùng chung Module 1 | — | Admin |
| FN-06 | Dashboard nhân sự | UR-HC-22, UR-LDP-02 | đọc tại ngày chốt | khớp NS01/NS06; < 5 s | — |
| FN-07 | Hồ sơ nhân sự tối giản | UR-HC-01, -11, UR-LDP-02, UR-TECH-03 | HS-01..37 (văn bằng, ngoại ngữ, khen thưởng dạng bảng; đơn vị, chức vụ có ngày hiệu lực), TK-01..06 (dân tộc, tôn giáo, Đảng, quê quán, nơi sinh, TGCM; thẻ riêng chỉ HCTH/Admin xem), NB-04/05 (bậc, hệ số tra bảng) | lọc < 2 s/1.000 hồ sơ; xuất Excel | HCTH, Admin |
| FN-08 | Thông tin làm việc | UR-HC-04 | ngày vào làm, ngày nghỉ, lý do, tệp QĐ; 2 trạng thái | NS01/NS02 | HCTH |
| FN-09 | Danh sách nâng bậc NL01–NL04 | UR-HC-12..15 | job hằng ngày tính mốc; điều kiện (xếp loại 3 năm, kỷ luật, bậc cuối); trạng thái xử lý (Chưa xử lý → Đã đưa vào DS → Đã xuất → Đã có QĐ); chỉ tiêu NLTH; quy đổi thành tích | Xuất **Mẫu số 1 (20 cột), 2 (20 cột), 4 (17 cột)** tự tách theo loại; lưu tệp đã xuất | HCTH |
| FN-10 | Sổ quyết định nâng bậc | UR-HC-16 | số/ngày/cơ quan/tệp; danh sách người; hủy có lý do (khôi phục bậc) | cập nhật FN-07, tệp vào FN-14 | HCTH |
| FN-11 | Kế hoạch nhân lực năm | UR-HC-23, UR-LDP-03 | kỳ năm, kế hoạch phòng (số hiện có/dự kiến nghỉ hưu tự tính), gửi → yêu cầu điều chỉnh → chốt | bảng tổng hợp đơn vị × chỉ tiêu, xuất | lãnh đạo phòng (lập), HCTH (chốt) |
| FN-12 | Biểu báo cáo Sở NS01–NS06 | UR-HC-06..09 | **truy vấn tại ngày chốt** (as-of); quy tắc kiểm chéo V.2.0 mục 1–3 | Excel đúng mẫu Sở (NS01 19 cột, NS04 36 cột, NS06 39 cột); ghi nhật ký khi xuất trường nhạy cảm | HCTH |
| FN-13 | Contact | UR-GEN-01/02, UR-ADM-10, UR-HC-27, UR-QG-02, UR-XA-21 | một bản ghi/người, cán bộ tỉnh đồng bộ từ FN-07, liên hệ ngoài tỉnh do HCTH nhập, thẻ Đơn vị | lọc trường **ở máy chủ** theo vai trò (TW/Xã chỉ nhận 3 trường); bấm số để gọi | HCTH, Admin |
| FN-14 | Kho tài liệu nhân sự | UR-HC-25, UR-ADM-10 | cây thư mục, tệp, phân loại (nhân viên, phòng ban, loại QĐ, năm), quyền theo thư mục | nhật ký upload/xóa/tải; xem trước | HCTH, Admin; lãnh đạo phòng xem |
| FN-15 | Mẫu đánh giá Mẫu số 05/06 | UR-HC-19 | tiêu đề, đối tượng theo chức vụ, 4 mức, 8 cột, phần ký; có phiên bản | xem trước | HCTH |
| FN-21 | Phiếu đánh giá | UR-HC-28, UR-XA-21 | tiêu chí, điểm tối đa, bảng quy đổi điểm → mức; phiên bản; phiếu tự đánh giá + cột cấp quản lý | thông báo trong app và email; **liên kết + OTP** cho người không có tài khoản; xuất Excel | HCTH (thiết kế); mọi người được giao (điền) |
| FN-16 | Đợt đánh giá | UR-HC-20 | danh sách cố định tại ngày chốt; chia Mẫu số 05/06; hạn từng bước; 3 luồng theo nhóm đối tượng; 6 trạng thái/người | tiến độ theo đơn vị × bước; nhắc; gia hạn | HCTH |
| FN-17 | Đề xuất xếp loại | UR-LDP-01, UR-XA-21 | DG-01..12 | nhập cả phòng từ Excel | lãnh đạo phòng, HCTH (Mẫu số 06), Xã (tự đánh giá) |
| FN-18 | Tổng hợp, xuất Mẫu số 05/06, chốt đợt | UR-HC-21 | DG-09 kết quả chính thức; bản đã ký; chốt; mở lại cho một người; kết quả năm → FN-09 | **Word hoặc Excel**; bản ký lưu ở FN-14 | HCTH |
| FN-19 | Danh mục nhân sự | UR-ADM-08 | chức vụ (cờ lãnh đạo, hệ số PCCV), ngạch/CDNN (nhóm tương đương, số bậc, **bảng hệ số có hiệu lực**, thời gian giữ bậc), trình độ, chuyên ngành (nhóm theo trình độ) | nạp Excel | Admin |
| FN-20 | Phân quyền hồ sơ | UR-ADM-09, UR-TECH-03 | ma trận nhóm quyền × phạm vi (toàn Trung tâm/đơn vị mình/bản thân) theo vai trò/người dùng/đơn vị; quyền theo người dùng ưu tiên | nhật ký đổi quyền | Admin |

**NFR riêng của Module 2:** nhật ký **mỗi lần xem chi tiết** hoặc xuất hồ sơ (lưu ≥ 1 năm); xuất NS/NL cho khoảng 500 người < 10 s; đợt đánh giá chịu khoảng 500 lượt nộp trong ngày hạn; xuất Excel/Word/PDF; chỉ tiêu sẵn sàng/sao lưu "thống nhất khi triển khai" (khác v7.7, vốn đã chốt ≥ 99,5 %).

##### 2.4 Hiện trạng trong repo

Đã tìm `hrm|personnel|ngạch|nâng bậc|hcth|employee|hanh_chinh` trong `internal/`, `migrations/`, `apps/web-ops/src`, `docs/`: **không có mã hay tài liệu nào cho HCTH**. Repo chỉ có các nền dùng lại được:
- FN-01..05: P1/P2 của KN v7.7 (ĐVHC, cây 3 cấp, phòng ban, gán vai trò nhiều chiều, ngữ cảnh phiên, menu từ API). Phòng ban HCTH **chưa seed**; web-ops chỉ có `trong_trot`, `chan_nuoi_thuy_san`, `ke_hoach_tai_chinh`, `thong_tin_truyen_thong`, `phong_ban_5`, `phong_ban_6` (`src/kn/cycle/screens/AdminScreens.tsx:23`). Menu Admin của phòng ban khác dùng chung khuôn "báo cáo theo kỳ" (`theo-doi-nop`, `tong-hop`, `mau-bao-cao`, ...), không hợp với HCTH.
- `pf_extension.staff` (danh bạ trực G1, pf 000006): có `display_name` **dạng rõ**, không có năm sinh/SĐT, đã bị ẩn khỏi menu v7.7. Không dùng làm Contact được (RB-3).
- `pf/content` (thư viện tài liệu, có trích docx), `pf/evidence` (`pf_files`, bộ ba chủ thể, `Open(purpose)` có nhật ký sổ), `pf/notify` (in-app, FCM, **SMTP**; **chưa có SMS**), `reports` render xlsx/pdf (**chưa có xuất Word**), `reports/app/dispatch_link.go` (liên kết công khai có hạn, ghi nhật ký từng lần mở; mẫu để làm liên kết phiếu + OTP).

##### 2.5 Ánh xạ sang năng lực v4.x

| Nhu cầu HCTH | Năng lực nên dùng | Mức sẵn có | Phần mới |
|---|---|---|---|
| Đăng nhập, ngữ cảnh, phòng ban HCTH, lãnh đạo phòng | `pf/accounts` (k/access) context + `pf/party` (k/orgtree) department/menu | Có | Seed `hanh_chinh_tong_hop`; mở rộng `mgmt_level` (lãnh đạo đơn vị, phụ trách); **phạm vi "đơn vị mình" (phòng/trạm/trại)** và "cán bộ xã được phân công" (HS-37) thêm vào `KNContext`; menu kn.menu/v3: nhiều khuôn menu theo phòng ban + nhóm "Dùng chung" (Contact) cho mọi vai trò |
| Danh mục nhân sự (FN-19) | `pf/masterdata` (cap/masterdata) catalog + params có phiên bản (duyệt bốn mắt `masterdata.param_version`) | Một phần | Catalog `position`, `civil_grade`, `education_level`, `major` (+ nhóm theo trình độ); **bảng hệ số theo bậc có ngày hiệu lực** (sổ riêng hoặc param có phiên bản); quy tắc nâng bậc, bảng quy đổi thành tích là param có phiên bản |
| Hồ sơ, thông tin làm việc (FN-07/08) | **module domain mới `pf/hrm` (`dom/hrm`)**, PII qua `vault/identity` (RB-3) | Không | Sổ hồ sơ + quá trình có hiệu lực (đơn vị, chức vụ, văn bằng), thẻ TK nhạy cảm (khóa riêng/mục đích riêng), nhật ký xem/xuất (`access_log` dạng sổ như `pf_files.access_log`), truy vấn as-of |
| Contact (FN-13) | **năng lực dùng chung mới** (đề xuất `pf/contact` aka `cap/directory`, hoặc mở rộng `vault/identity` + chiếu) | Không | Một bản ghi/người tham chiếu chủ thể vault; trường lọc theo vai trò ở API; đồng bộ từ `pf/hrm` qua port; liên hệ ngoài tỉnh; thẻ Đơn vị lấy từ `pf/party` |
| Kho tài liệu (FN-14) | `pf/evidence` (cap/evidence) cho tệp + nhật ký; cây thư mục, phân loại, ACL ở `pf/hrm` | Một phần | `evidence.UseResolver("pf/hrm", …)` cho chủ thể `staff`, `decision`, `folder`; mục đích mới (vd `hr_records`); "xóa = ẩn" là sự kiện bù |
| Phiếu đánh giá, đợt, đề xuất, tổng hợp (FN-15..18, 21) | `pf/forms` (cap/forms): template có phiên bản = phiếu/mẫu; period = đợt; submission = phiếu/đề xuất; review; close_snapshot = chốt đợt; profile HCTH | Một phần | `cap/forms` hiện theo **cây đơn vị (lá nộp)** và chỉ có **một bước rà soát bốn mắt**. HCTH cần đơn vị nộp là **người** (đối tượng được đánh giá), **nhiều bước** (tự đánh giá → cấp quản lý → HCTH → tập thể), người nhập thay, khóa từng bước, chấm điểm + bảng quy đổi. Hai cách: (a) mở rộng `cap/forms` với `subject_kind=person` + `steps[]`; (b) `pf/hrm` tự giữ máy trạng thái đợt, chỉ dùng `cap/forms` cho template/phiếu có phiên bản. **Đề xuất (b)** cho đợt đầu, (a) khi có bên dùng thứ hai |
| Kế hoạch nhân lực (FN-11) | `pf/forms` (period năm, lá = đơn vị, review = yêu cầu điều chỉnh, close_snapshot = chốt) | Gần đủ | Profile `hrm_plan`; hierarchy cần nút **đơn vị nội bộ** (phòng/trạm/trại), không phải xã |
| Duyệt, quyết định | `pf/workflow` (cap/workflow) | Có | BRD **không** có luồng trình ký. Chỉ dùng cho: hủy quyết định nâng bậc, mở lại đợt đã chốt cho một người, đổi bảng hệ số (bốn mắt nếu khách muốn, Q-HC-14) |
| Nhắc hạn, thông báo đợt, đến kỳ nâng bậc | `pf/notify` (in-app, email SMTP) + job (`shared/schedule`, `opsjobs`) | Có | Kiểu thông báo mới `hrm_*`; **SMS chưa có** (OTP qua SĐT) |
| Liên kết phiếu + OTP cho người không có tài khoản | mẫu `reports/dispatch_link` + notify email | Không | Năng lực "liên kết khách có OTP" (token một chủ thể, hạn = khóa bước, OTP 6 số, giới hạn thử, nhật ký mỗi lần mở); cần ADR (định danh người ngoài Keycloak, ATTT cấp 3) |
| Xuất NS01–06, NL02–04, MS05/06 | `pf/reports` render xlsx (cap/export đang "hosts") + `export_audit`/`pf/metering` (điểm kiểm soát xuất) | Một phần | Mẫu xuất cố định bố cục Sở (quốc hiệu, hàng số cột, người lập/ký); **renderer Word (.docx)** mới; xuất trường nhạy cảm ghi nhật ký |
| Dashboard nhân sự | `pf/metrics` (cap/analytics) hoặc truy vấn trực tiếp `pf/hrm` | Một phần | Chỉ số tại ngày chốt; khối "Cần chú ý" |
| Việc cần làm (phiếu cần chấm, đến kỳ nâng bậc) | `cap/task` (đang cài trong `pf/extension`) | Có, nhưng chỉ cho KN | Đây là bên dùng thứ hai → xét tách `pf/task` theo quy tắc ba (`modules.md` §W6.6). **Không bắt buộc** ở đợt 1 |
| Phân quyền hồ sơ (FN-20) | **mới**, trong `pf/hrm` (ma trận nhóm quyền × phạm vi) chồng lên `KNContext` | Không | Thứ tự ưu tiên: quyền người dùng > vai trò; chặn gỡ Admin cuối; nhật ký |

##### 2.6 Xung đột giữa BRD HCTH và nền tảng đã làm theo v7.7

| # | HCTH v3.0 | v7.7 / mã hiện tại | Đề xuất |
|---|---|---|---|
| C1 | FN-05: bước 1 nhập **tên đăng nhập** + mật khẩu + **chọn vai trò bằng dropdown**; bước 2 chọn phòng ban | v7.4+: email **hoặc SĐT**, không có ô vai trò; chọn vai trò và phòng ban sau xác thực (`/v1/me/contexts`) | Giữ cách v7.7 cho mọi module (một luồng đăng nhập); BRD HCTH cần sửa FN-05 (Q-HC-1) |
| C2 | Admin: bước 2 hiện "Tất cả phòng ban" | Admin không chọn phòng ban, vào "Tổng quan hệ thống" | Giữ v7.7 |
| C3 | Cấp quản lý: Lãnh đạo đơn vị / Trưởng phòng / Chuyên viên - Cán bộ; BR-04: "Lãnh đạo đơn vị xem mọi phòng ban của đơn vị, chỉ đọc" | `chuyen_vien`, `pho_truong_phong`, `truong_phong`; chưa có quyền đọc chéo phòng ban | Thống nhất danh mục cấp quản lý (thêm `lanh_dao_don_vi`, `phu_trach`); quy tắc đọc chéo cần xác nhận cho cả Module 1 (Q-HC-3) |
| C4 | Lãnh đạo các phòng (thuộc phòng Trồng trọt, Chăn nuôi, …) dùng menu HCTH cho người của phòng mình | Phiên = 1 vai trò + 1 phòng ban; lãnh đạo Phòng Trồng trọt có phiên `trong_trot`, không thấy menu HCTH | Chọn: (i) gán thêm phòng ban HCTH với vai trò "lãnh đạo phòng" (phạm vi = đơn vị mình), hoặc (ii) menu "Nhân sự của phòng" hiện ở mọi phòng ban khi `mgmt_level` ≥ phó phòng (Q-HC-2) |
| C5 | Đơn vị công tác = phòng, trạm, trại, BGĐ, VPTT, "Tăng cường" (dòng NS05); HS-16 "Danh mục đơn vị (FN-19)" | Cây tổ chức chỉ có 3 cấp TTKNQG → tỉnh → xã; phòng ban là danh mục chiều nghiệp vụ, không phải đơn vị chứa người | Cần danh mục **đơn vị nội bộ** của TTKN tỉnh: dùng `department` (P2) mở rộng loại `tram`/`trai`, hoặc catalog HR riêng (Q-HC-4) |
| C6 | Menu nhóm "Điều hành, Nghiệp vụ, Báo cáo" + nhóm Contact "Dùng chung" | v7.7: Điều hành / Báo cáo / Dữ liệu (Admin: Điều hành / Phòng ban / Quản trị hệ thống) | Mở rộng hợp đồng menu (kn.menu/v3) với nhóm tùy phòng ban + nhóm dùng chung |
| C7 | FN-02 BR-02: "Nút xã chỉ chuyển sang Nút tỉnh khác, mang theo tài khoản và dữ liệu" | Cùng quy tắc với v7.7, màn chưa có (FN-AD-04.02 một phần) | Gói T1 |
| C8 | UR-XA-21 (HCTH: phiếu đánh giá, Contact) | UR-XA-21 (Trồng trọt: lịch công tác) | **Trùng mã UR giữa hai BRD.** Cần tiền tố module trong truy vết (vd `HC-UR-XA-21`) |
| C9 | Nhắc tới "mục IX điểm 1", "7 điểm cần xác nhận" (v2.0), "FN-23" trong thứ tự triển khai | Docx v3.0 **không có** Mục IX và không có FN-23 | BA bổ sung (Q-HC-5) |
| C10 | FN-05 có PIN mobile, offline 72 giờ | Module 2 chỉ là Web App (I.3) | Bỏ khỏi phạm vi HCTH |
| C11 | NFR sẵn sàng/sao lưu "thống nhất khi triển khai" | v7.7 đã chốt ≥ 99,5 %, RPO 24 giờ, RTO 4 giờ | Cùng hạ tầng nên lấy theo v7.7 |
| C12 | HS-12 SĐT "hiển thị ở Contact cho mọi vai trò", HS-04 năm sinh hiển thị cho TW/Xã | RB-3: PII ở vault, hiển thị qua mục đích | Contact là **mục đích riêng** (`contact_directory`), trả 3 trường; xem ở mức xã/TW cần căn cứ NĐ 13 (Q-HC-9) |
| C13 | BR-06 FN-07: "khi bậc thay đổi, giá trị mới **ghi đè**"; FN-10 "hủy quyết định khôi phục bậc trước đó" | RB-1: chỉ ghi thêm | Bậc/hệ số hiện hưởng là phép chiếu từ sổ sự kiện (QĐ, sửa lẻ, hủy = sự kiện bù). BRD "không lưu quá trình ngạch bậc" chỉ áp **ở giao diện**; sổ vẫn giữ |
| C14 | FN-09 cần "ngày xếp bậc hiện tại" để tính mốc, trong khi hồ sơ "không lưu quá trình ngạch, bậc" | Không có dữ liệu | Thiếu dữ liệu đầu vào cho cả FN-09: cần trường "thời điểm được xếp bậc" (và HS bảo lưu) trong hồ sơ (Q-HC-6) |

---

#### 3. Đề xuất gói công việc

Quy ước cỡ: **S** ≤ 3 ngày-người · **M** 1–2 tuần · **L** 2–4 tuần · **XL** > 4 tuần (một agent/dev). Migration: số pf cao nhất hiện tại là **000136** (`000136_pf_kref_share_scope`), nên lấy tiếp từ 000137 theo cách cấp số của người tích hợp. Mọi gói theo `docs/dev/g1-conventions.md` (sổ `apply_ledger`, RLS, `problem.*`, tên kiểm thử mang mã FN).

##### 3.1 Trồng trọt (đóng v7.7)

| Gói | Nội dung | Cỡ | Phụ thuộc | Migration | Năng lực |
|---|---|---|---|---|---|
| **T0** Cập nhật tài liệu | UAT/gap sau W3.3/W6.2 (đường dẫn `pf/cooperative`, menu mobile đã có?), sửa lỗi văn bản BRD (§1.1) gửi BA | S | — | — | — |
| **T1** Đóng khoảng trống mã | Màn "Báo cáo đột xuất" (Tỉnh, Xã) trên `src/kn/grid` thay `ComingSoon`; vẽ thửa trên web (chấm điểm, hình tròn, hình vuông, tải KML/GeoJSON, chặn chồng lấn) dùng `/v1/kn/plots`; nạp Tỉnh giao từ Excel ở màn phát hành; di chuyển Nút xã ở màn cây; nạp Excel tài khoản nhiều vai trò; tab Phiên/Lịch sử đăng nhập (Keycloak Admin REST); đếm bản ghi theo phòng ban, "biểu đang dùng"; X-12 (dữ liệu cũ không phòng ban → Trồng trọt); cảnh báo PII ở ô tự do (X-02) | M–L | — | không (trừ chỉ mục nếu cần) | `pf/party`, `pf/accounts`, `pf/cooperative`, `pf/reports` |
| **T2** Cắt sang `cap/forms` native | Kỳ KN native trên `/v1/forms`; engine lưới (bố cục, công thức) của `pf/reports` thành `forms.field_types`; web-ops và field-app chuyển client; Chi cục đọc `close_snapshot`; giữ `/v1/extension/report-*` chỉ đọc một đợt; di chú dữ liệu bằng sự kiện bù (không UPDATE) | XL | W6.2; nên xong trước H6/H7 | có (pf_forms mở rộng kiểu trường; quyền r_extension) | `cap/forms`, `cap/workflow` |
| **T3** 14 mẫu thật | Nạp tệp gốc khi có Q-01 qua "Nạp từ Excel"; ca vàng công thức so `excelize.CalcCellValue` (N-14) | M | Q-01 | không | `pf/reports` (hoặc `cap/forms` sau T2) |
| **T4** Google Sheet (nếu khách chọn) | ADR-0026 từ "Đề xuất" sang "Chấp nhận", bộ chuyển `reports/gsheet` tắt mặc định | M | Q-14 | có (`gsheet_link` nếu chưa có) | `pf/extdata` van, `pf/metering` |
| **T5** Hạ tầng | Kiểm tải 500 người dùng (staging), FCM thật < 1 phút, Android 3G, nhật ký truy cập ≥ 1 năm, diễn tập RPO/RTO | M | cụm staging | — | — |

##### 3.2 Hành chính - Tổng hợp (mới hoàn toàn)

| Gói | Nội dung | Cỡ | Phụ thuộc | Migration | Năng lực v4.x |
|---|---|---|---|---|---|
| **H0** Nền phòng ban và phạm vi | Seed phòng ban HCTH; danh mục **đơn vị nội bộ** (phòng/trạm/trại/BGĐ) theo Q-HC-4; mở rộng `mgmt_level`; `KNContext` thêm `UnitRef` nội bộ + "cán bộ xã được phân công quản lý"; menu kn.menu/v3 (khuôn menu theo phòng ban, nhóm "Dùng chung"); app `HC` (`app/HC/app.yaml`) hoặc mở rộng `app/KN` (Q-HC-12) | M | Q-HC-2/3/4 | có (`pf_party`, `pf_accounts`) | k/orgtree, k/access |
| **H1** Danh mục nhân sự (FN-19) | Catalog chức vụ (cờ lãnh đạo, PCCV), ngạch/CDNN (nhóm tương đương, số bậc, thời gian giữ bậc), **bảng hệ số có hiệu lực** (sổ), trình độ, chuyên ngành + nhóm theo trình độ; danh sách cố định (dân tộc, tôn giáo, LLCT, QLNN, ngoại ngữ, tin học, lý do nghỉ); quy tắc nâng bậc/quy đổi thành tích là param có phiên bản; chuẩn hóa mã ngạch (bỏ khoảng trắng) | M | H0 | có (`pf_masterdata`) | cap/masterdata (+ cap/workflow cho param) |
| **H2** Module `pf/hrm` + hồ sơ (FN-07, FN-08, FN-20) | Schema `pf_hrm`, vai `r_hrm`; sổ hồ sơ (chủ thể vault cho họ tên, ngày sinh, SĐT, email, địa chỉ; TK-01..06 khóa/mục đích riêng), quá trình có hiệu lực (đơn vị, chức vụ, văn bằng, tăng cường), khen thưởng (không tiền), bậc/hệ số là phép chiếu từ sự kiện; thông tin làm việc → trạng thái; truy vấn as-of; nhật ký xem chi tiết/xuất (sổ, ≥ 1 năm); ma trận quyền hồ sơ (FN-20); quy tắc V.2.0 mục 4, 6; mã hồ sơ tự sinh | L | H0, H1 | có (`pf_hrm` + grants + `schema_isolation_exceptions` nếu gọi `pf_workflow`) | **dom/hrm mới**; vault/identity; cap/workflow |
| **H3** Contact (FN-13) | Năng lực dùng chung `pf/contact` (`cap/directory`, `kind: capability`): một bản ghi/người (tham chiếu vault), nguồn = `pf/hrm` (port, module chủ khai) + liên hệ ngoài tỉnh; thẻ Đơn vị từ `pf/party`; lọc trường ở API theo vai trò; nhập Excel liên hệ ngoài tỉnh; ẩn hẳn `pf_extension.staff` (danh bạ trực) hoặc chiếu nó từ Contact | M | H2 (nguồn), H0 | có (`pf_contact`) | **cap/directory mới** (hoặc mở rộng vault/identity, Q-HC-10) |
| **H4** Kho tài liệu (FN-14) | Cây thư mục + phân loại + quyền thư mục trong `pf/hrm`; tệp qua `pf/evidence` (`UseResolver("pf/hrm")`, mục đích `hr_records`); xem trước; "xóa = ẩn" (sự kiện); QĐ nâng bậc và bản ký đánh giá tự vào thư mục | M | H2 | có (`pf_hrm` thư mục; grant `pf_files` không đổi) | cap/evidence |
| **H5** Nâng bậc (FN-09, FN-10) | Job hằng ngày tính mốc (NLTX/TNVK/NLTH, 1–3–6 tháng), điều kiện (xếp loại 3 năm từ H6/dữ liệu chuyển đổi, kỷ luật = cờ chuyển đổi?), trạng thái xử lý, chỉ tiêu NLTH, quy đổi thành tích; dashboard; xuất Mẫu số 1/2/4 (xlsx đúng bố cục, lưu tệp + người xuất); sổ quyết định (chọn người, ngày hưởng, hủy = sự kiện bù) cập nhật bậc | L | H1, H2, H4; (H6 cho xếp loại) | có (`pf_hrm`) | cap/export (render xlsx), cap/notify, shared/schedule |
| **H6** Đánh giá (FN-15, 16, 17, 18, 21) | Mẫu số 05/06 có phiên bản; phiếu (tiêu chí, điểm, bảng quy đổi, phiên bản); đợt (danh sách cố định tại ngày chốt, chia mẫu theo chức vụ, 3 luồng, hạn từng bước, khóa bước, gia hạn); phiếu tự đánh giá → cấp quản lý (HS-37) → đề xuất DG-04..08 → HCTH; yêu cầu điều chỉnh; xuất MS05/06 **Word + Excel**; tải bản ký; chốt (ảnh chụp bất biến); kết quả năm → hồ sơ; **liên kết + OTP** cho người không có tài khoản (email; SMS nếu Q-HC-8); nhắc | XL | H2, H4, H8-word; nên sau T2 | có (`pf_hrm` + có thể `pf_forms`) | cap/forms (template, snapshot), cap/workflow (mở lại sau chốt), cap/notify, **năng lực liên kết khách + OTP mới (ADR)** |
| **H7** Kế hoạch nhân lực (FN-11) | Profile `cap/forms` "hrm_plan" (period năm, lá = đơn vị nội bộ, review = yêu cầu điều chỉnh, chốt = snapshot); số hiện có/dự kiến nghỉ hưu tự tính từ H2 | M | H0, H2, T2 (nếu dùng forms native) | ít (profile) | cap/forms |
| **H8** Báo cáo Sở (FN-12) + Dashboard (FN-06) | NS01–NS06 tại ngày chốt (as-of), quy tắc kiểm chéo V.2.0 mục 1–3 (chặn/cảnh báo), nhấn ô để xem người được đếm, lưu tệp + lịch sử xuất, nhật ký trường nhạy cảm (`export_audit` + `pf/metering`); **renderer .docx** dùng chung (cho MS05/06); dashboard < 5 s | L | H2, H1 | có (sổ tệp xuất) | cap/export, cap/analytics |
| **H9** web-ops HCTH | 30 màn (15 FN × danh sách/chi tiết) + Contact cho mọi vai trò (bản TW/Xã 3 cột), popup thêm/sửa theo quy định VIII.1; giả lập `src/api/mock/hrm*`; Playwright `hc-*.spec.ts` | L | hợp đồng OpenAPI của H2–H8 | — | — |
| **H10** Chuyển đổi dữ liệu ban đầu | Công cụ một lần (`cmd/…` hoặc `formimport`) đọc Excel Thống kê CCVCNLĐ / Biểu mẫu nhân sự / Theo dõi nâng bậc qua `shared/tabular`; chuẩn hóa (ngày nhiều định dạng, bậc bị đổi thành ngày, hệ số dạng chữ, ghi chú trong họ tên, tên phòng); báo cáo dòng lỗi để HCTH xác nhận (A2) | M | H1, H2 | — (ghi qua app) | cap/import (shared/tabular) |
| **H11** Nghiệm thu | `docs/uat/hc-v3.0-acceptance.md` (một dòng/AC), E2E `tests/hc30/…` (mở đợt quý III/2026: 470 người MS05 + 24 người MS06; NLTX tháng 7/2026 4 người), kiểm tải 500 lượt nộp phiếu/ngày | M | tất cả | — | — |

**Thứ tự đề xuất:** H0 → H1 → H2 → (H3, H4 song song) → H8 → H5 → H6 → H7; H9 chạy theo hợp đồng và dùng giả lập từ sớm; H10 trước UAT. T2 nên xong trước H6/H7 để HCTH không phải dựng thêm một vòng đời kỳ song song thứ ba.

**Ước lượng thô:** HCTH khoảng 14–20 tuần-người (H6 và H2 nặng nhất); đóng Trồng trọt khoảng 3–4 tuần-người (T1, T3) cộng T2 khoảng 4–6 tuần-người.

**Rủi ro chính:** (1) PII nhân sự rất dày (ngày sinh, địa chỉ, SĐT, email, dân tộc, tôn giáo, Đảng) nên NĐ 13 và RB-3 đòi mục đích, nhật ký xem và DLP xuất ngay từ thiết kế; (2) liên kết + OTP cho người ngoài Keycloak là bề mặt tấn công mới; (3) quy tắc nâng bậc chưa được cung cấp (A3), có thể chặn H5; (4) thiếu "ngày xếp bậc" (C14).

---

#### 4. Câu hỏi mở

**Trồng trọt**
1. **Q-TT-1** BA sửa 4 lỗi văn bản trong bản Final (FN-TI-05; đường dẫn menu FN-AD-04.06; "Mục VI" trong NFR; Mục V.3/VII không tồn tại) hay chấp nhận bản tóm tắt repo §6/§9 làm nguồn cho menu và các điểm đã chốt?
2. **Q-TT-2** Khách hàng có ký nhận các lệch đã quyết nội bộ A1–A5 không: khóa hẳn sau 3 lần khóa tạm; liên kết kích hoạt thay mật khẩu tạm; liên kết tải 7 ngày thay Google Sheet; vẽ thửa tối thiểu 3 điểm?
3. **Q-TT-3** Vẽ thửa trên Web (FN-XA-03.05) có bắt buộc cho đợt nghiệm thu, hay chấp nhận "Web xem, Mobile vẽ"?
4. **Q-TT-4** Ưu tiên T2 (cắt KN sang `cap/forms` native) trước hay sau khi khách gửi Q-01? T2 đổi client web và mobile.
5. **Q-TT-5** Các câu Q-01…Q-23 của `kn-v7.7-plan.md` §5 vẫn mở (trừ Q-21). Đặc biệt Q-01 (tệp Excel 14 mẫu) đang chặn FN-XA-03.02, FN-TI-02.05, N-14; Q-02 tên 6 phòng ban, nay cần cả mã cho HCTH.

**Hành chính - Tổng hợp**
6. **Q-HC-1** FN-05 trong BRD HCTH (tên đăng nhập + dropdown vai trò ở màn đăng nhập; Admin chọn "Tất cả phòng ban") mâu thuẫn với v7.7. Xác nhận dùng một luồng đăng nhập theo v7.7 cho mọi module?
7. **Q-HC-2** Lãnh đạo các phòng chuyên môn làm việc HCTH (đề xuất xếp loại, kế hoạch phòng, xem hồ sơ phòng mình) trong phiên phòng ban nào: gán thêm phòng ban HCTH, hay menu nhân sự hiện ở mọi phòng ban theo cấp quản lý?
8. **Q-HC-3** Danh mục cấp quản lý chung cho các module là gì (Lãnh đạo đơn vị, Trưởng phòng, Phó trưởng phòng, Phụ trách, Chuyên viên - Cán bộ)? "Lãnh đạo đơn vị xem mọi phòng ban, chỉ đọc" (FN-04 BR-04) có áp cho Module 1 không?
9. **Q-HC-4** Danh mục đơn vị nội bộ (BGĐ, VPTT, các phòng, trạm, trại, "Tăng cường"): dùng danh mục phòng ban (P2) hay danh mục đơn vị riêng của HR? Thứ tự sắp NS01 lấy từ đâu?
10. **Q-HC-5** BRD nhắc "Mục IX điểm 1", "7 điểm cần xác nhận" và "FN-23" nhưng v3.0 không có. Xin danh sách điểm mở hiện hành.
11. **Q-HC-6** FN-09 cần "ngày xếp bậc hiện tại / thời điểm được xếp" và "HS bảo lưu" để tính mốc, nhưng hồ sơ "không lưu quá trình ngạch, bậc". Có cho lưu hai trường này trong hồ sơ không, hay nhập khi lập danh sách? Dữ liệu ban đầu lấy từ cột nào?
12. **Q-HC-7** Bảng hệ số theo ngạch/bậc, thời gian giữ bậc, quy chế NLTH (bảng quy đổi thành tích, tỷ lệ chỉ tiêu), % TNVK khởi điểm/tăng, tuổi nghỉ hưu, số ngày nhắc: Phòng HCTH gửi khi nào (A3)? "Kỷ luật" lấy từ đâu khi BRD không quản lý kỷ luật?
13. **Q-HC-8** OTP gửi qua **SĐT** cần nhà cung cấp SMS trong nước (hiện chưa có). Chấp nhận chỉ email ở đợt 1? Liên kết hết hạn khi khóa bước, vậy có giới hạn số lần mở/thử OTP không?
14. **Q-HC-9** Căn cứ pháp lý (NĐ 13) để Cấp Xã và Cấp TW xem năm sinh, SĐT của mọi cán bộ các cấp trong Contact. Có cần đồng ý của chủ thể không? Liên hệ Trung ương do ai nhập?
15. **Q-HC-10** Contact là module dùng chung cho mọi app (HTX, MKG?) hay chỉ trong app KN? Có thay "danh bạ trực" G1 (`pf_extension.staff`) không?
16. **Q-HC-11** Xuất **Word** (MS05/06, NFR) dùng mẫu .docx của Sở hay tự dựng? Có cần PDF cho biểu NS không?
17. **Q-HC-12** HCTH triển khai trong cùng app KN (một web-ops, menu theo phòng ban) hay app riêng (`app/HC`)? Cùng tenant An Giang?
18. **Q-HC-13** Viên chức không có tài khoản: nguồn email/SĐT để gửi liên kết là HS-12/HS-35 trong hồ sơ. Nếu thiếu cả hai thì chỉ còn đường lãnh đạo phòng nhập thay?
19. **Q-HC-14** Đổi bảng hệ số, hủy quyết định nâng bậc, mở lại đợt đã chốt cho một người có cần duyệt bốn mắt (`cap/workflow`) không, hay chỉ ghi lý do (BRD chỉ nói "có lý do, lưu vết")?
20. **Q-HC-15** Chỉ tiêu sẵn sàng, sao lưu cho Module 2 có lấy theo v7.7 (≥ 99,5 %, RPO 24 giờ, RTO 4 giờ) không?
21. **Q-HC-16** Mã UR trùng giữa hai BRD (UR-XA-21). Đề nghị BA thêm tiền tố module (vd `TT-`, `HC-`) để truy vết UAT không lẫn.
22. **Q-HC-17** "Hình thức hiển thị (báo cáo hay màn danh sách)" của FN-09 đang chờ Phòng HCTH. Chốt màn danh sách + popup xuất như wireframe Hình 17?

---

## Phụ lục D — D4 — KN Chăn nuôi – Thủy sản, Kế hoạch – Tài chính, Báo giá nông sản

### D4 — Phân tích BRD Khuyến nông: Chăn nuôi – Thủy sản, Kế hoạch – Tài chính, Báo giá nông sản

**Ngày:** 09/10/2026 · **Repo:** `C:\Dev\claude-skills-main\Agrigreen_demo_v2` @ `27afd593` (checkout `main`, cùng commit với `dev`) · **Chế độ:** chỉ đọc repo.

**Nguồn đã trích** (python zipfile → văn bản, bảng giữ theo dòng) — `analysis/src/`:

| Tệp trích | BRD | Phiên bản | Ngày |
|---|---|---|---|
| `brd_channuoi.txt` | Module Phòng Chăn nuôi – Thủy sản (CNTS) | v3.7 | 07/10/2026 |
| `brd_taichinh.txt` | Module Phòng Kế hoạch – Tài chính (KHTC) | v2.2 | 07/10/2026 |
| `brd_baogia.txt` | Module Báo giá nông sản (Phòng Thông tin Tuyên truyền, TTTT) | v1.3 | 07/10/2026 |

Hình (wireframe) trong docx không trích; mọi chỗ "Hình n" chỉ có chú thích.

**Tóm tắt kết luận**

| BRD | Xếp loại kiến trúc đề xuất | Lý do chính |
|---|---|---|
| CNTS | **Cấu hình phòng ban `chan_nuoi_thuy_san` của `dom/extension`** (ADR-0027 mục 6) + mở rộng `dom/cooperative` cho hộ/điểm/lứa/nhật ký chăn nuôi – thủy sản | ~70 % là vòng đời biểu theo kỳ đã có ở `extension/reportcycle` + `cap/forms`. Phần mới (đơn vị nộp không phải xã, kỳ ngày, cảnh báo, nhật ký có GPS) là mở rộng năng lực, không phải module phòng ban |
| KHTC | **Cấu hình phòng ban `ke_hoach_tai_chinh` của `dom/extension`** cho kế hoạch năm, kỳ, phiếu; **kích hoạt tách `cap/export` → `pf/export`** cho mục lục mẫu + soạn + xuất Word | Là bộ tổng hợp liên phòng trong cùng tenant; phần "mẫu văn bản có phiên bản + Render" đúng điều kiện (i) tách `cap/export` của W6.6 |
| Báo giá | **Module năng lực mới `cap/price` = `pf/price`** (đã có trong v4.1 §26, ma trận §5.1: KN, HTX ○, MKG, NH) + cấu hình phòng ban `thong_tin_truyen_thong` | Dùng cho ≥ 3 ứng dụng (đạt quy tắc ba R-MOD-09); v4.1 khai sẵn `Series`, `Publish`, `price.published`, "công bố là quyền riêng", cấm float (R-CODE-01) |

---

#### 1. Kiểm kê tính năng và phần tái sử dụng

##### 1.1 BRD CNTS v3.7

**Phạm vi.** Web (mọi vai) + Mobile (Cấp Xã hiện trường; Nông hộ GĐ2). 102 xã/phường/đặc khu gom theo **Khu vực**. 19 biểu BM-01…BM-19, 21 chức năng FN-01…FN-21. GĐ1: FN-01…09, FN-10 (phần cán bộ xã), FN-11…16, FN-21. GĐ2: FN-10 (nông hộ), FN-17…20.

**Vai trò.** Admin; Cấp TW (TTKN QG — **chỉ dự án KNQG**, xác nhận/trả lại Mẫu 07, không thấy số liệu xã); Cấp Tỉnh có 5 nhóm (viên chức, cán bộ tổng hợp, chủ nhiệm CT/DA, Lãnh đạo phòng, Ban Giám đốc "chỉ xem") + cờ "người phụ trách biểu", "người theo dõi lịch", "phụ trách dịch"; Cấp Xã (cán bộ tăng cường, CBKN xã, cán bộ theo dõi điểm, viên chức trực KSGM — phạm vi = xã **hoặc cơ sở giết mổ hoặc hộ**); **Nông hộ** (Mobile, SĐT + OTP, GĐ2); vai khách **"Phòng nhận báo cáo"** (HCTH, KHTC — vào thẳng hộp "Báo cáo đến").

**Yêu cầu người dùng (UR).**

| Nhóm | Mã | Nội dung tóm tắt |
|---|---|---|
| Admin | UR-ADM-01…07 | Tổ chức 3 cấp + ĐVHC (dùng chung); bật phòng CNTS; tài khoản + phạm vi xã/cơ sở GM; vai Nông hộ (SĐT); danh mục CNTS (vật nuôi, giống, vaccine kèm quy trình theo ngày tuổi, bệnh, hóa chất, CSGM + mã mộc dấu, đối tượng thủy sản, nhóm khai thác, chỉ tiêu + ngưỡng, loại mô hình); nhập thay như Cấp Xã; gán Khu vực cho Nút xã; khởi tạo 19 biểu |
| Cấp Xã | UR-XA-01…14 | Việc cần làm; xem lại; lịch trực KSGM; hộ mô hình + ngày tiêm thực tế; lịch công tác tuần + check-in; BM-01 KSGM ngày (UR-XA-06); BM-04 nhập liệu tuần chăn nuôi (07); BM-17 khai thác thủy sản tuần (08); BM-05 công việc/kế hoạch tuần (09); BM-10 dịch bệnh (10); BM-08/09 nhật ký gà/vịt xiêm (11); BM-19 nhật ký thủy sản có GPS, ảnh, offline (12); hỗ trợ nhập (kiểm trước gửi, sao chép kỳ trước, "không phát sinh", Excel BM-04, điều chỉnh) (13); biểu đợt điều tra (14) |
| Cấp Tỉnh | UR-TINH-01…26 | Thiết kế + phát hành biểu (01, 07, 08); theo dõi nộp, nhắc cấu hình được (02, 08); tổng hợp (03); báo cáo tuần/tháng/quý/năm + duyệt (04, 16); lịch công tác + duyệt + check-in (05, 18); trang chủ (06); cấp quyền xã/nông hộ (09); lịch trực KSGM BM-03 (10); lịch tiêm BM-07 (11); rà soát/xác thực nhật ký (12); ngưỡng + cảnh báo (13); KSGM tuần/tháng BM-02 (14); Phụ lục 2 thủy sản BM-18 (15); bản đồ vùng nuôi/dịch (17); CT/DA/điểm trình diễn (19); Mẫu 01/03/04/05/07 KNQG BM-12…16 (20–24); kho văn bản (25); chiến dịch điều tra (26) |
| TW | UR-QG-01 | Xem dự án KNQG An Giang; xác nhận/trả lại Mẫu 07 |
| Nông hộ | UR-NH-01…03 | Nhật ký trên điện thoại (offline); lịch tiêm + nhắc + xác nhận; báo nghi dịch có vị trí, ảnh |
| Kỹ thuật | UR-TECH-01…03 | Android giá rẻ, 3G, offline cho nhập liệu, ảnh, GPS, check-in; GPS + thời điểm thiết bị + ảnh không sửa được; mở phòng bằng master data |

**Thực thể chính.** Cơ sở giết mổ (nhiều mã mộc dấu GS/GC, loại tập trung/nhỏ lẻ); báo cáo KSGM ngày (cơ sở × ngày, gộp ca); nhập liệu tuần xã (CN-01 tổng đàn 7 loài + nhà yến; CN-02 tiêm phòng 9 cột cấu hình theo năm; hóa chất; KSGM; vịt chạy đồng); khai thác thủy sản tuần (cá/tôm/mực/khác, tấn 3 số lẻ); báo cáo công việc tuần 2 phần; ổ dịch (DB-01…13, vị trí, vùng đệm, cập nhật diễn biến có phiên bản); chương trình/dự án/điểm trình diễn/hộ tham gia; lứa nuôi + lần ghi nhật ký (NC-01…16, NT-01…15, hạch toán kinh tế NC-15); lịch trực (cơ sở × viên chức × ngày × N/Đ/NĐ); lịch tiêm (quy trình mũi theo ngày tuổi → ngày dự kiến); cảnh báo; báo cáo phòng BM-11; Phụ lục 2 (khai thác tự động + nuôi trồng nhập tay theo ~20 đối tượng); Mẫu 01/03/04/05/07; đợt điều tra; văn bản.

**Quy tắc then chốt.**
- Vòng đời báo cáo kỳ: 13 chuyển trạng thái (Chưa nhập, Lưu nháp, Trễ hạn, Đã nộp [+ cờ Nộp trễ], Yêu cầu điều chỉnh, Đã xác thực, Đã chốt, **Đã chốt – chưa điều chỉnh**, **Không nộp**, **Chờ mở kỳ**). Mở lại kỳ do LĐ phòng (ủy quyền được cho cán bộ tổng hợp có thời hạn), hết hạn tự chốt lại.
- Hạn/kỳ: BM-01 mỗi ngày một kỳ, hạn 09:00 D+1; ca đêm tính ngày bắt đầu ca. BM-04/BM-17 **tuần Thứ Ba – Thứ Hai, hạn 12:00 Thứ Ba**. KSGM tháng chia 4 tuần (1–7, 8–14, 15–21, 22–cuối), Tỉnh sửa ranh giới từng tháng. BM-05 hai phần, mỗi phần một kỳ.
- Lũy kế = cộng kỳ đã nộp; tuần vắt quý/năm tính theo **ngày cuối tuần (Thứ Hai)**; **sửa kỳ cũ → lũy kế kỳ sau tính lại**; số nộp trễ/sửa sau khi báo cáo phòng đã gửi đưa vào mục "Số liệu tuần trước nộp bổ sung".
- Kiểm soát sửa: lưu cũ → mới từng ô, lý do khi sửa sau hạn/sau xác thực; xung đột hai người → không ghi đè; Cấp Tỉnh **không sửa số gốc**.
- 13 quy tắc liên biểu (vd liều tiêm ≤ tổng đàn theo ánh xạ vaccine–loài: Chặn; KSGM tuần BM-04 = Σ BM-01: Cảnh báo; Phụ lục 2 khai thác = Σ BM-17; Mẫu 07 chỉ gửi khi mọi điểm có Mẫu 05 đã ký).
- 12 cảnh báo tự động (biến động tổng đàn ±15 %, KSGM > 3× TB 28 ngày, khai thác ±50 %, pH ngoài 7,5–8,5, oxy < 4 mg/l, tăng trọng thấp, tỷ lệ chết, vị trí > 200 m hoặc sai số > 50 m, quá hạn ghi, mũi tiêm quá hạn, ổ dịch mới = Khẩn). Vòng đời cảnh báo Mới → Đã giao → Đang xử lý → Đã đóng / Quá hạn; tự đóng khi hết vi phạm; một cảnh báo mỗi (đối tượng, loại, kỳ).
- Báo cáo phòng BM-11: chỉ số đã nộp/xác thực/chốt; tạo được khi tỷ lệ nộp ≥ mức Tỉnh đặt hoặc kỳ đã chốt, ghi "Tổng hợp từ n/N xã"; Đang soạn → Chờ duyệt → Đã duyệt → Đã gửi → Đã nhận; gửi hộp "Báo cáo đến" + email công vụ.
- Lần ghi nhật ký: sửa trong 7 ngày nếu chưa xác thực; đã xác thực chỉ tạo bản điều chỉnh.

**Báo cáo đầu ra.** BM-02 KSGM tuần/tháng (+ dòng điều chỉnh có lý do, file chia sẻ Chi cục có số phiên bản), BM-18 Phụ lục 2, BM-11 tuần/tháng/quý/năm (Word), Mẫu 01/03/04/05/07 (đúng mẫu TTKN QG), kết quả đợt điều tra, trang chủ chỉ số nóng, bản đồ.

**Tích hợp.** Excel/Word/PDF đúng mẫu; gửi báo cáo sang Module KHTC (khi có) hoặc email; file cho Chi cục Chăn nuôi – Thú y (không liên thông); bản đồ nền + ranh giới ĐVHC; SMS/OTP; KML/GeoJSON; sẵn sàng IoT/truy xuất (sau).

**Mobile/offline.** BM-04, nhật ký, báo dịch, check-in offline; giữ thời điểm + vị trí lúc ghi; ảnh ≤ 500 KB; phát hiện vị trí giả lập; bản đến sau chốt kỳ → "Chờ mở kỳ". 3 màn Mobile tối thiểu: Việc cần làm, BM-04, Công việc tuần + check-in.

**Phi chức năng.** < 3 s trên 3G; trang chủ/bản đồ < 5 s với 500 người; cảnh báo ≤ 1 phút sau đồng bộ; ATTT cấp 3, MFA Admin, OTP nông hộ; NĐ 13/2023 (che SĐT).

**Tái sử dụng từ repo.**

| FN CNTS | Đã có | Tham chiếu | Khoảng trống |
|---|---|---|---|
| FN-01…05 (ĐVHC, tổ chức, phòng ban, tài khoản, đăng nhập) | Đủ cơ chế | `pf/masterdata` ĐVHC có hiệu lực (ADR-0032); `pf/party` Departments, Regions, OrgDirectory; `pf/accounts` ngữ cảnh KN (`/v1/me/contexts`, `X-AG-Context`, `Principal{Role, Department, OrgRef, Communes}` — `docs/brd/kn-v7.7-plan.md` P1); menu theo phòng ban `GET /v1/departments/{code}/modules`; seed `internal/platform/party/seeddata/departments_an_giang.json` (`chan_nuoi_thuy_san`, `not_open`, `menu_profile: generic`) | Menu CNTS chưa có (chỉ Trồng trọt định nghĩa trong mã); phạm vi Cấp Xã theo **cơ sở GM / hộ** (hiện chỉ danh sách xã); 5 nhóm trong Cấp Tỉnh (hiện có trường "Cấp quản lý", Q-05); vai khách "Phòng nhận báo cáo"; Nông hộ OTP |
| FN-06 danh mục | Danh mục chung có sổ lịch sử, ngừng dùng, nhập hàng loạt | `internal/platform/masterdata/domain/catalog.go` (`Catalogs` là enum Go, DB chỉ regex `000002`) | Thêm ~10 danh mục; vaccine cần **danh sách mũi theo ngày tuổi** (thuộc tính lồng — có thể cần bảng riêng); ngưỡng mặc định theo chỉ tiêu |
| FN-07 thiết kế + phát hành biểu | Engine lưới mẫu (cột Cố định / Tỉnh giao / Nhập / Công thức, kiểm, dòng tổng), nhập mẫu từ .xlsx, phát hành đợt chia kỳ sinh công việc | `pf/reports` `form/eval.go`, `formimport/xlsx.go`, `cmd/formimport`; `extension/reportcycle/app/batch.go`, `domain/period.go`; `pf/forms` (template phiên bản, period, close_snapshot) | Người định nghĩa biểu là **Cấp Tỉnh** (TT: Admin) → `forms.Profile.DefineBy`; trường GPS/ảnh/bắt buộc theo mẫu; kỳ **ngày**; ranh giới tuần KSGM sửa theo tháng; kiểu tuần Thứ Ba/Thứ Tư; cột thêm/ngừng giữa năm có hiệu lực từ kỳ |
| FN-08 việc cần làm | Có | `extension/reportcycle/app/task.go`, `domain/task.go` (Chưa nhập, Lưu nháp, Đã nộp, Trễ hạn, Yêu cầu điều chỉnh, Đã xác nhận, Đã chốt) | Đơn vị công việc = xã/cơ sở/hộ; thêm trạng thái Không nộp, Đã chốt – chưa điều chỉnh, Chờ mở kỳ |
| FN-09 nhập biểu theo kỳ | Có (Web + đồng bộ offline) | sync entity `report_task`, `form_layout`, `report_submission` (`extension/port/reportcycle.go`) | "Báo không phát sinh", "Sao chép kỳ trước", khóa lạc quan hai người, giải trình theo từng cảnh báo (H09) |
| FN-13 theo dõi, rà soát | Có: theo dõi nộp, nhắc, leo thang, yêu cầu điều chỉnh, chốt/mở lại kỳ (có hạn mở), Freezer ảnh chụp | `reportcycle/app/review.go`, `job.go`, `pf/forms` `Freezer`; `notify/domain/report_cycle.go` | Cảnh báo theo ngưỡng/biến động + vòng đời cảnh báo; ủy quyền mở lại kỳ; nhắc cấu hình theo biểu (số kỳ trễ liên tiếp) |
| FN-12 lịch công tác | Có đăng ký + duyệt + xuất Excel/PDF | `extension/domain/schedule.go`, `app/schedule*.go` | **Check-in GPS**; hai phần kế hoạch/kết quả; người duyệt = "người theo dõi được phân công" |
| FN-15 lịch trực, lịch tiêm | Danh bạ trực + ca (`Shift`) | `extension/domain/roster.go` | Lưới cơ sở × viên chức × ngày N/Đ/NĐ, nạp Excel, cảnh báo xung đột; lịch tiêm sinh từ quy trình — chưa có |
| FN-16 tổng hợp, báo cáo phòng | Tổng hợp có nhật ký ô thay đổi, xuất Excel/PDF đúng mẫu, gửi email Chi cục; bốn mắt | `reportcycle/app/consolidate.go`, `domain/consolidate.go`; `pf/reports` Mailer, `dispatch.*`; `pf/workflow` | BM-02 dòng điều chỉnh; Phụ lục 2 nhập tay một phần; BM-11 có phần lời văn + duyệt + hộp nhận; xuất **Word** (repo chưa có bộ ghi .docx) |
| FN-10 hộ mô hình, nhật ký | Hộ (PII ở vault), nhật ký canh tác, minh chứng mã hóa, log GPS + khoảng cách tới thửa | `pf/cooperative` (`Households`, `Diary`, `HouseholdIdentities`), `pf/evidence` (`Attach`, `Open(purpose)`), `pf/geo` `GpsLog`, `extension/app/gpslog.go` | Điểm nuôi (chuồng/ao/lồng), **lứa nuôi**, nhật ký có chỉ số (trọng lượng, pH, oxy), xác thực bởi chủ nhiệm, phát hiện vị trí giả, hạch toán |
| FN-11 dịch bệnh | Mẫu gần: yêu cầu hỗ trợ có GPS + ảnh + đồng bộ; chỉ đạo khẩn theo vùng | `extension/domain/support.go`, `domain/directive.go` | Thực thể ổ dịch có phiên bản diễn biến; xác nhận báo nghi dịch |
| FN-17 trang chủ | Dashboard KN, `pf/metrics` đọc `_pub` | `extension/app/dashboard.go`; `docs/dev/modules.md` W6.3 | View `_pub` cho số CNTS |
| FN-18 bản đồ | Nhập GeoJSON/KML/SHP, lớp bản đồ, cơ sở (kho/hub/nhà máy) | `geo/domain/features.go`, `g2.go` | **Vẽ vùng** (bán kính, tay), hướng di chuyển, lưu theo ngày; lớp ổ dịch; cơ sở GM có thể là `facility` kind mới |
| FN-19 dự án KNQG | Chỉ mẫu tương tự (tiến độ Đề án theo HTX, sổ vật tư) | `extension/domain/program.go`, `extension/material` | Toàn bộ Mẫu 01/03/04/05/07, ký scan, TW xác nhận |
| FN-20 văn bản | Có | `pf/content` (document, library, sync) | Nhãn hết hiệu lực |
| FN-21 điều tra | Kỳ `adhoc` | `reportcycle/domain/period.go` (`PeriodAdhoc`) | Đợt (Nháp/Đang thu thập/Đã đóng), gia hạn, tách khỏi lũy kế |

##### 1.2 BRD KHTC v2.2

**Phạm vi.** Web. Module KHTC + chức năng "Báo cáo cho KHTC" (FN-PN-03.01…03) **đặt trong module mỗi phòng chuyên môn** (TTTT, Trồng trọt, CNTS, HCTH — giả định GD-01). Cấp Xã không có chức năng.

**Vai trò.** Admin; Cấp TW (xem/tải báo cáo đã xuất); Cấp Tỉnh: cán bộ KHTC (thao tác), lãnh đạo KHTC/các phòng/BGĐ (chỉ xem), viên chức phòng chuyên môn (nhập phiếu trong module phòng).

**UR.** UR-CH-01; UR-TW-01; UR-TI-01…16 (trang chủ; kế hoạch năm có phiên bản theo QĐ; mở kỳ bằng ô chọn; tiến độ phiếu + nguồn module; rà soát phiếu; soạn Mẫu 1 tuần, 2 tháng, 3 quý/6/9 tháng, 4 năm, 5 tiến độ các CT; báo cáo phát sinh; xem nguồn từng số; phiên bản file xuất + mốc dữ liệu; soạn thảo tự do + "Lưu thành mẫu"; số liệu nguồn ngoài; thêm/ngừng CT theo năm); UR-PN-01…04; UR-AD-01…03; UR-KT-01…02.

**Chức năng.** FN-CH-00.01; FN-TW-01.01; FN-TI-02.01…02.11; FN-PN-03.01…03.03; FN-AD-04.01…04.03 (04.02 dùng chung "Quản lý mẫu báo cáo" FN-AD-04.06 của Trồng trọt).

**Thực thể.** Kế hoạch năm (CT → nhóm → hạng mục: ĐVT, khối lượng KH, quy mô, phòng phụ trách, xã/điểm, nguồn KP, nguồn số liệu phiếu/module; 6 tab nội dung theo dõi; phiên bản theo QĐ + ngày hiệu lực); kỳ báo cáo (6 loại); phiếu số liệu (mỗi kỳ × phòng, lần gửi, "Thực hiện trong kỳ", "Điều chỉnh kỳ trước" + kỳ được điều chỉnh + lý do); bản báo cáo (bản soạn, phiên bản xuất, mốc dữ liệu); mục lục mẫu (mục, cách lặp, khuôn câu có biến, nguồn biến, khóa/sửa); số liệu nguồn ngoài (chỉ tiêu × kỳ + nguồn + minh chứng); danh mục KHTC (loại BC, ĐVT, lĩnh vực, nguồn KP, người ký, nơi nhận, căn cứ, **mặt hàng giá cả**).

**Quy tắc.** BR-DL-01…21: tuần báo cáo **Thứ Tư – Thứ Ba**, tháng/quý/năm = các tuần có Thứ Ba thuộc kỳ, "bắt buộc mọi module (đã chốt)"; chỉ lấy nguồn **Đã chốt** + phiếu **Đã gửi**; Tạm tính; mốc dữ liệu khi xuất + "Nguồn đã thay đổi"; lũy kế = Σ thực hiện + Σ điều chỉnh cùng năm; **không sửa kỳ cũ — điều chỉnh ghi ở phiếu kỳ kế tiếp**; năng suất bình quân gia quyền; KH dùng phiên bản hiệu lực ≤ ngày chốt; ưu tiên câu in khi thiếu dữ liệu; đóng/hủy kỳ; ô hành chính sửa được, ô số khóa. Ba máy trạng thái: kỳ (Nháp, Đã mở, Đang thu thập, Trễ hạn, Đủ dữ liệu, Đã đóng, Đã hủy), phiếu (Chưa nhập … Đã gửi lần n, Yêu cầu bổ sung, Đã xác nhận, Đã khóa, Không gửi), bản báo cáo (Chưa soạn, Đang soạn, Đã xuất vN, Nguồn đã thay đổi, Bản cuối).

**Báo cáo.** Mẫu 1–5 (Word đúng NĐ 30/2020, mở được Word 2016+/WPS) + phụ lục Excel; báo cáo phát sinh; tiến độ các CT lọc phòng/CT/kỳ/xã.

**Tích hợp.** Đọc dữ liệu đã chốt của Trồng trọt (Báo cáo tổng hợp các xã), CNTS (tổng đàn, nhà yến, thủy sản khai thác + nuôi trồng, lũy kế), TTTT (bảng giá đã duyệt theo ngày + **số phiếu giá đã thu thập theo tuần báo cáo**), HCTH (phiếu CCHC); không tích hợp văn phòng điện tử (chỉ xuất .docx).

**Mobile/offline.** Không.

**Tái sử dụng từ repo.**

| Nhu cầu KHTC | Đã có | Tham chiếu | Khoảng trống |
|---|---|---|---|
| Kỳ + phát hành + nộp + rà soát + đóng + ảnh chụp | Vòng đời kỳ KN; `cap/forms` có Profile cấu hình cấp, chốt có ảnh chụp băm | `pf/forms` (`docs/dev/modules.md` W6.2), `extension/reportcycle` | Đơn vị lá là **phòng ban** (không phải xã); hủy kỳ; trạng thái "Đủ dữ liệu" phụ thuộc nguồn module; tối đa n lần gửi |
| Lưới phiếu | Engine lưới (Cố định/Tỉnh giao/Nhập/Công thức) | `pf/reports/form`, `formimport` | Kiểu cột "Từ Kế hoạch năm", "Lũy kế kỳ trước"; dòng sinh từ kế hoạch; tab theo CT |
| Rà soát bốn mắt / yêu cầu bổ sung | Có | `pf/workflow` (`forms.submission`) | — |
| Danh mục | Có (enum) | `masterdata/domain/catalog.go` | Danh mục người ký, nơi nhận, căn cứ có "mặc định cho mẫu" |
| Nhập Excel nguồn ngoài | Bộ đọc an toàn | `internal/shared/tabular`; `pf/evidence` cho minh chứng | Luồng hai bước có lưu dòng lỗi (điều kiện tách `cap/import`) |
| Xuất tệp, lưu băm | Khung xuất bất đồng bộ | `pf/reports` export job + `shared/objstore`, `shared/outbox` | **Mẫu văn bản có phiên bản, biến, khuôn câu, lặp, soạn thảo tự do, ghi .docx** — chưa có (chỉ `content/domain/extract.go` đọc docx) |
| Đọc nguồn module | `_pub` + `pf/metrics`; port `reports/port/extensionsrc.Consolidations` | `docs/dev/modules.md` W6.3; `pf/reports` module.yaml | View `_pub` cho tổng hợp đã chốt của từng phòng; trạng thái "đã chốt" của kỳ nguồn |
| Chương trình | Chỉ tiến độ Đề án theo HTX | `extension/domain/program.go` | Kế hoạch năm nhiều CT có phiên bản theo QĐ — mới hoàn toàn |

##### 1.3 BRD Báo giá nông sản v1.3

**Phạm vi.** Web responsive (máy tính + điện thoại), Mobile đăng nhập PIN/offline 72 h; 26 xã báo giá (danh mục, không cố định), 30–40 mặt hàng/xã, 5 nhóm hàng, 2 nhóm vùng (An Giang, Kiên Giang); điều tra giá thành theo vụ (nhập file Google Form/Excel). Đợt 1: vận hành hằng ngày + điều tra. Đợt 2: báo cáo biến động, dashboard vùng, báo cáo giá thành, website.

**Vai trò.** Admin (kỹ thuật, không duyệt khi vận hành); Cấp TW (chỉ xem bảng giá đã công bố, báo cáo, giá thành tổng hợp — không PII); Cấp Tỉnh (Phòng TTTT; cờ **Quản trị nghiệp vụ báo giá**; phân công theo xã; xã chưa phân công ai cũng xử lý); Cấp Xã (CTV báo giá, một cán bộ chính mỗi xã theo ngày hiệu lực).

**UR.** UR-TI-01…17, UR-XA-01…07, UR-TW-01…04, UR-AD-01…02, UR-CH-01.

**Chức năng (28).** FN-CH-00.01; FN-TW-01.01 bảng giá tổng hợp, 01.02 biến động, 01.03 dashboard vùng, 01.04 giá thành; FN-TI-02.01 tiến độ, 02.02 chi tiết, 02.03 cảnh báo + xác minh, 02.04 sửa/xóa phiếu, 02.05 duyệt, 02.06 mở duyệt (≤ 30 ngày), 02.07 website, 02.08 thống kê phiếu theo cán bộ, 02.09 nhập thay (Nộp muộn), 02.10 đợt điều tra, 02.11 nhập file điều tra, 02.12 rà soát + chốt đợt; FN-XA-03.01 nhập giá trước 12:00, 03.02 in phiếu (File 2), 03.03 lịch sử, 03.04 nhắc, 03.05 phản hồi xác minh; FN-AD-04.01 tài khoản, 04.02 xã báo giá, 04.03 mặt hàng theo thời kỳ + gán xã + đề xuất + bộ theo vụ, 04.04 lịch báo giá + ngoại lệ + giờ khóa + hạn phản hồi, 04.05 nhắc, 04.06 ngưỡng.

**Thực thể.** Xã báo giá (↔ mã ĐVHC, nhóm vùng, ngày bắt đầu); phân công cán bộ (xã ↔ CTV theo ngày; Tỉnh ↔ xã); mặt hàng (mã tự sinh, loại+giống/phẩm cấp+ĐVT, Cơ bản/Địa phương, hiệu lực, "thay thế cho"), nhóm hàng, bộ theo vụ, đề xuất; lịch báo giá; phiếu báo giá (xã × ngày; dòng: min, max, ghi chú, "Không có hàng"); cảnh báo biến động + trao đổi xác minh; bảng giá ngày (phiên bản); ngưỡng (mặc định/nhóm/mặt hàng, N ngày, đi ngang 1 %, 40 % điều tra); đợt điều tra, phiếu điều tra (nông hộ — **họ tên, SĐT là PII**), bảng từ đồng nghĩa.

**Quy tắc.** CT-01 P = (min+max)/2; CT-02 giá tham chiếu = P đã duyệt gần nhất trong N=7 ngày báo giá; CT-03 Δ% ≥ ±T (mặt hàng → nhóm → mặc định 30 %); CT-04 tổng hợp min/max/TB cộng, đi ngang |Δ| < 1 %; CT-05 giá thành, lợi nhuận; CT-06 lệch trung vị ≥ 40 % (≥ 5 phiếu). Bốn máy trạng thái (phiếu, cảnh báo, bảng giá ngày, đợt/phiếu điều tra). Khóa 12:00 giờ máy chủ ±1 phút; hạn phản hồi 13:30; không duyệt khi còn cảnh báo; một người duyệt một lúc; mọi thay đổi danh mục áp dụng từ ngày báo giá kế tiếp; mặt hàng đã có giá không đổi ĐVT/quy cách.

**Báo cáo.** Bảng giá tổng hợp (Excel), biến động (Excel/PDF), dashboard vùng, thống kê phiếu theo CTV (căn cứ thanh toán — hệ thống không tính tiền), giá thành theo vụ ("Mẫu nhỏ" khi < 3 phiếu; trung vị), in phiếu File 2.

**Tích hợp.** Website Khuyến nông An Giang (đường dẫn nội bộ; đăng bảng giá — phương án chờ A2); email + thông báo trong app (Zalo OA sau); nhập Google Form/Excel; chuyển ≥ 3 tháng dữ liệu cũ + Hè Thu 2026.

**Mobile/offline.** Nhập trên trình duyệt điện thoại; Mobile PIN + offline đăng nhập; mất mạng khi nhập → giữ trên màn hình (không yêu cầu nhập offline có đồng bộ).

**Tái sử dụng từ repo.** Không có gì về giá nông sản (`cap/price` "Chưa có module" — ADR-0027 bảng mục 2; đối chiếu v4.1 dòng §26 `cap/price` "Không có, chờ yêu cầu (NH, KN)"). Dùng lại: đăng nhập, ngữ cảnh vai + phòng ban, PIN/offline 72 h (`docs/uat/kn-v7.7-acceptance.md` N-09), ĐVHC có hiệu lực (ADR-0032) cho xã báo giá + sắp xếp ĐVHC, `pf/notify` (nhắc theo giờ, email, đẩy), `pf/workflow` (nếu muốn bốn mắt khi duyệt), `pf/metrics` `_pub` (dashboard), `shared/tabular` (nhập file), `vault/identity` (PII nông hộ điều tra), `pf/evidence` (minh chứng nhập thay), cổng `cmd/integration-gateway` + tập dữ liệu dựng sẵn (ADR-0033, ADR-0035) cho website/MKG. Khung xuất `pf/reports`.

---

#### 2. Phù hợp kiến trúc

##### 2.1 CNTS → cấu hình phòng ban của `dom/extension`, cộng mở rộng năng lực

- Theo ADR-0027 mục 6 (X-13) phòng ban không có `module.yaml`, schema, port riêng. CNTS đặt trong `pf/extension` với `department_code = chan_nuoi_thuy_san`; mọi bản ghi mới mang `department_code` (quy tắc 2 của `kn-v7.7-plan.md`).
- Phần "biểu theo kỳ" (BM-01/04/05/17, điều tra) dùng `extension/reportcycle` + engine lưới `pf/reports` + ghi song song `pf/forms` — **nhưng phải tổng quát hóa đơn vị nộp**: hiện công việc = mẫu × **xã** × kỳ; CNTS cần xã | cơ sở GM | hộ/điểm. Đề xuất thêm `unit_kind` + `unit_ref` (sổ phụ, không `ADD COLUMN` vào sổ có dữ liệu) và ưu tiên chuyển kỳ CNTS thẳng sang kỳ native `cap/forms` (mục "Chưa làm" W6.2) thay vì mở rộng sổ cũ.
- Cảnh báo/ngưỡng: dùng cơ chế nhiệm vụ có SLA + leo thang đang cài ở `pf/extension` (`hosts: [cap/task]`): cảnh báo = loại nhiệm vụ có kết quả bắt buộc khi đóng. Đây là bên dùng thứ hai trong cùng module, chưa đủ để tách `pf/task`.
- **Hộ, điểm nuôi, lứa, nhật ký**: chủ duy nhất là `dom/cooperative` (v4.1 §27.1: "chủ duy nhất … nông hộ, thửa, vụ, nhật ký canh tác"). Đề xuất gói con `cooperative/husbandry` trong `pf/cooperative`: `production_site` (chuồng/ao/lồng, tọa độ — tương tự thửa), `batch` (lứa nuôi — tương tự vụ), `husbandry_log` (sổ lần ghi có chỉ số, GPS, ảnh qua `cap/evidence`). Lý do không làm module domain mới: dùng chung hộ (`household_ref` → vault), RLS `p_scope` Nút đã có (pf 000134), nông hộ tự ghi qua app NH (`app/NH/app.yaml` đã ghép `pf/cooperative`). Phương án module mới `dom/husbandry` chỉ đáng khi HTX chăn nuôi/thủy sản cũng dùng (quyết định ở câu hỏi 14).
- Chương trình/dự án/điểm trình diễn/Mẫu KNQG: `dom/extension` (nghiệp vụ khuyến nông), tham chiếu hạng mục Kế hoạch năm của KHTC thay vì tự lập danh sách CT (xem 2.4).
- Ổ dịch + vùng dịch: thực thể ổ dịch thuộc `dom/extension`; hình học vẽ tay lưu theo quy ước v4.1 §26 ("hình học thực thể thuộc module chủ; `cap/geo` chỉ cung cấp phép tính và hiển thị") → `pf_extension` giữ polygon, `pf/geo` cấp buffer, hiển thị lớp.
- Cơ sở giết mổ: danh mục `pf/masterdata` (mã, nhiều mã mộc dấu, xã, loại) hoặc `facility` kind mới của `pf/geo` (đang có kho/hub/nhà máy). Đề xuất danh mục masterdata (có hiệu lực, có sổ) + tọa độ tùy chọn.

##### 2.2 KHTC → cấu hình phòng ban của `dom/extension` + tách `cap/export`

- Kế hoạch năm, kỳ KHTC, phiếu phòng, nguồn ngoài là nghiệp vụ khuyến nông tỉnh → `pf/extension`, `department_code = ke_hoach_tai_chinh`. Phiếu "Báo cáo cho KHTC" hiển thị trong module mọi phòng — chung một chức năng dom/extension, lọc theo phòng phụ trách.
- Kỳ KHTC dùng `cap/forms` với **Profile mới** `kn_khtc`: `Hierarchy` TINH(KHTC) → lá = phòng ban (không phải xã), `PublishBy = CloseBy = ReviewBy = KHTC`. Cần `cap/forms` chấp nhận đơn vị lá là phòng ban (hiện `period` gắn "đơn vị nhận cấp lá" theo cây Nút).
- Đọc nguồn module trong **cùng tenant**: qua view `_pub` của từng nguồn (vd `pf_extension_pub.v1_consolidation_closed` cho Trồng trọt/CNTS, `pf_price_pub.v1_board_daily`), không qua cấp phép chia sẻ và không cần `r_agg`. `pf/reports` hiện còn đọc qua port (mục "Chưa làm" W6.3) — KHTC nên đi thẳng `_pub`.
- Mục lục mẫu + khuôn câu + soạn thảo + xuất .docx có phiên bản và băm: đúng điều kiện (i) tách `cap/export` → module `pf/export` (W6.6). Đề xuất làm luôn khi làm KHTC (Mẫu 1–5 là bên dùng đầu; Mẫu 01–07 KNQG và BM-11 của CNTS là bên dùng thứ hai, thứ ba).

##### 2.3 Báo giá → module năng lực mới `cap/price` (`pf/price`)

- v4.1 §26 định nghĩa sẵn: sở hữu chuỗi giá có hiệu lực, ngưỡng; port `Series`, `Publish`; sự kiện `price.published`; điểm mở rộng "Nguồn giá"; công bố là quyền riêng. Ma trận §5.1: KN ●, HTX ○, MKG ●, NH ● → đạt quy tắc ba.
- Đề xuất: `pf/price` (`kind: capability`, `aka: [cap/price]`, `zones: [pf]`, `enable: per_tenant`, schema `pf_price` + `pf_price_pub`). Giữ: danh mục mặt hàng theo thời kỳ (hoặc dùng `pf/masterdata` — câu hỏi 9), điểm thu (xã báo giá), chuỗi giá, ngưỡng, bảng giá ngày có phiên bản, công bố. **Nguồn giá** là điểm mở rộng: nguồn "phiếu xã hằng ngày" (FN-XA-03.*) và "điều tra giá thành" (FN-TI-02.10–12). Quy trình phiếu xã có thể nằm trong `pf/price` (nguồn nội tại) vì không có module domain nào khác cần nó; cấu hình vai/khóa giờ theo Profile (như `forms.Profile`) để không so sánh tên ứng dụng (R-MOD-04).
- Cấm float (R-CODE-01 nêu đích danh `cap/price`) → `decimal` cho giá, %; thêm `pf/price` vào `floatFreeZones` của `agnofloat`.
- Phòng TTTT là cấu hình phòng ban `thong_tin_truyen_thong` của `dom/extension` cho menu; dữ liệu do `pf/price` sở hữu.
- Website/MKG/NH đọc qua cổng tích hợp: tập dữ liệu dựng sẵn `price_board_daily` (ADR-0033 feed theo cursor, R-INT-05), không DB chung.

##### 2.4 Chủ dữ liệu, PII, chia sẻ, ngưỡng k

| Dữ liệu | Chủ đề xuất | PII / kho định danh | Ghi chú |
|---|---|---|---|
| Hộ mô hình CNTS, nông hộ tự ghi | `pf/cooperative` (`household`, `household_ref` → `vault/identity`) | Họ tên, SĐT, vị trí hộ = dữ liệu cá nhân NĐ 13/2023; che SĐT khi hiển thị; Nông hộ đăng nhập OTP → danh tính ở vault (vai `farmer`) | Câu hỏi H-1 (chủ là chương trình KN hay HTX) chặn bật chia sẻ |
| Điểm nuôi, lứa, nhật ký CN/TS | `pf/cooperative` (gói mới) | Tọa độ điểm = vị trí cá nhân của hộ → ảnh, GPS qua `cap/evidence` (mã hóa theo hộ, `Open(purpose)` có nhật ký) | Bản đồ "hộ định vị" chỉ cho vai có mục đích; Cấp TW không thấy |
| KSGM, nhập liệu tuần, khai thác, dịch bệnh | `pf/extension` (phòng CNTS) | KSGM theo cơ sở: không PII. Ổ dịch có "hộ chăn nuôi, địa chỉ" (DB-04) → `household_ref`, không lưu tên tự do; quét `ContainsPII` như các trường văn bản KN | File Chi cục chỉ số liệu cơ sở |
| Kế hoạch năm, kỳ, phiếu, nguồn ngoài | `pf/extension` (phòng KHTC) | Không PII (người ký là cán bộ — vẫn `pii:"true"` như `Staff.DisplayName`) | Đọc nguồn qua `_pub` cùng tenant |
| Chuỗi giá, bảng giá | `pf/price` | Giá thương lái theo xã: không PII → công bố được | Thống kê phiếu theo CTV có SĐT cán bộ → chỉ Cấp Tỉnh |
| Phiếu điều tra giá thành | `pf/price` (nguồn điều tra) | Họ tên, SĐT nông hộ → `vault/identity` (nhập file tách ngay cột PII sang vault, giữ `household_ref`/băm SĐT để chống trùng); không xuất, không cho TW | Cán bộ điều tra không cần tài khoản → danh sách tên/SĐT cán bộ cũng là PII |

**Chia sẻ.** Toàn bộ ba module chạy trong tenant TTKN An Giang; luồng KHTC ← phòng khác là trong tenant (port/`_pub`), không dùng `share_grant`. Chỉ cần cấp phép khi: (a) Cấp TW là tenant khác (câu hỏi 17) — dùng `intra_platform` (ADR-0030) cho bảng `pf/cooperative:*` hoặc chỉ xuất báo cáo; (b) đối tác ngoài (website, MKG) — `aggregate` hoặc tập dữ liệu công khai qua cổng.

**Ngưỡng k.**
- Bảng giá xã: là giá thị trường, không phải dữ liệu hộ → không áp k; công bố theo quyền riêng của `cap/price`.
- Giá thành điều tra (theo nông hộ): khi phát ngoài tỉnh (TW khác tenant, website, MKG) phải là tập tổng hợp có k theo đơn vị đếm `household` (ADR-0035 §3, k sàn `publicdata.k_min` mặc định 10). BRD chỉ gắn nhãn "Mẫu nhỏ" khi < 3 phiếu và vẫn hiển thị → mâu thuẫn với k sàn khi rời tenant (câu hỏi 12).
- Số liệu CNTS cấp xã cho TW/Sở: tổng theo xã, không theo hộ → không cần k; nhật ký hộ không rời tenant.

---

#### 3. Gói công việc, thứ tự, câu hỏi

Cỡ: **S** ≤ 3 ngày · **M** 1–2 tuần · **L** 2–4 tuần · **XL** > 4 tuần (một kỹ sư, gồm kiểm thử). Migration: cụm `pf`, số tiếp theo từ **pf 000137** (cuối hiện tại 000136), đặt tên `<số>_pf_<module>_*`, chạy được bằng bộ chạy v2 (ADR-0038); sổ có dữ liệu không `ADD COLUMN` → sổ phụ; bảng mới có `p_tenant` + `p_scope` RESTRICTIVE theo `k_ref.node_closure`; bên gọi mới của `pf_workflow.submit/decide` cần `GRANT EXECUTE` cho `r_<module>`.

##### 3.0 Gói dùng chung (làm trước)

| Mã | Gói | Cỡ | Phụ thuộc | Migration |
|---|---|---|---|---|
| X0 | Chốt quyết định: lịch tuần hệ thống, mô hình điều chỉnh số, chủ danh mục mặt hàng, tiền tố mã FN (câu hỏi 1–4, 9) | — | Khách + PO | — |
| X1 | Lịch kỳ hệ thống dùng chung: quy tắc chia kỳ v2 (tuần Thứ Tư–Thứ Ba hoặc theo X0; kỳ ngày; tuần KSGM 4 tuần/tháng sửa ranh giới; tháng/quý/năm theo Thứ Ba cuối) là tham số có phiên bản `pf/masterdata` + hàm thuần trong `reportcycle/domain/period.go` (thay `RuleVersion` tạm Q-17) | M | X0 | Không (tham số); sổ phụ nếu ghi `period_rule` mới vào đợt cũ |
| X2 | Mở 3 phòng ban: sửa seed (`chan_nuoi_thuy_san`, `ke_hoach_tai_chinh`, tên "Thông tin Tuyên truyền" vs "Thông tin - Truyền thông", thêm HCTH — Q-02), định nghĩa menu theo vai cho từng `menu_profile` | S | X0 | Seed dữ liệu; không schema |
| X3 | Thuộc tính phân quyền trong vai: nhóm Cấp Tỉnh (viên chức, tổng hợp, chủ nhiệm, LĐ, BGĐ chỉ xem), cờ QTNV báo giá, "người phụ trách biểu", ủy quyền có thời hạn; phạm vi Cấp Xã theo cơ sở/hộ; vai khách "Phòng nhận báo cáo" (nếu giữ) | M | X2 | Có: sổ phụ cạnh sổ gán vai `pf_party`/`pf_accounts` |
| X4 | `cap/forms` tổng quát đơn vị lá: `unit_kind` (xã, cơ sở, hộ/điểm, **phòng ban**); Profile `kn_cnts`, `kn_khtc`; trạng thái Không nộp, Đã chốt – chưa điều chỉnh, Chờ mở kỳ; hủy kỳ; giới hạn lần gửi | L | X1 | Có: `pf_forms` (sổ phụ đơn vị, sự kiện `cancelled` trong `trg_transition`) |
| X5 | "Báo cáo cho KHTC" FN-PN-03.01–03 dùng chung trong `dom/extension` (danh sách phiếu, form phiếu, tiến độ CT của phòng) — một mã, hiện trong menu mọi phòng | M | X4, KH1 | Có (`pf_extension` phiếu, lần gửi, điều chỉnh kỳ trước) |
| X6 | Hợp đồng nguồn module: view `_pub` "số liệu đã chốt theo tuần báo cáo" cho Trồng trọt (tổng hợp các xã), CNTS (tổng đàn, nhà yến, khai thác, nuôi trồng, lũy kế), Giá (bảng giá đã duyệt, số phiếu); cờ trạng thái chốt của kỳ nguồn | M | X1, X4 | Có: migration công bố `pf_pub_*` |
| X7 | Ghi .docx + mẫu văn bản có phiên bản → tách `cap/export` thành `pf/export` (điều kiện W6.6 (i)); dùng cho KHTC Mẫu 1–5, CNTS BM-11, Mẫu 01–07 | XL | X0 | Có: schema `pf_export` (mẫu, phiên bản, lần xuất + băm), vai `r_export` |

##### 3.1 CNTS

| Mã | Gói | FN/UR | Cỡ | Phụ thuộc | Migration |
|---|---|---|---|---|---|
| CN1 | Danh mục CNTS (~10 danh mục, ngưỡng mặc định, quy trình tiêm theo ngày tuổi, CSGM nhiều mã mộc dấu, ánh xạ vaccine–loài) | FN-06, UR-ADM-04 | M | X2 | Code (enum `catalog.go`); bảng mũi tiêm nếu không để trong `attrs` |
| CN2 | Biểu kỳ cấp Xã BM-01, 04, 05, 17: định nghĩa mẫu, kỳ ngày + tuần mới, hạn 09:00 D+1 / 12:00 Thứ Ba, đơn vị = cơ sở GM cho BM-01, sao chép kỳ trước, không phát sinh, Excel BM-04, khóa lạc quan, giải trình theo cảnh báo; Mobile 3 màn + offline (`report_task`/`report_submission`) | FN-07…09, UR-XA-06…09, 13 | XL | X1, X3, X4, CN1 | Có (`pf_extension`/`pf_forms` sổ phụ) |
| CN3 | Quy tắc liên biểu + 12 cảnh báo + vòng đời cảnh báo (nhiệm vụ SLA của `cap/task` đang cài ở extension) + cấu hình nhắc theo biểu | FN-13, UR-TINH-02, 08, 13 | L | CN2 | Có (bảng cảnh báo/ngưỡng hoặc loại nhiệm vụ) |
| CN4 | Tổng hợp: BM-02 (+ dòng điều chỉnh, file Chi cục có phiên bản), BM-18 Phụ lục 2 (khai thác tự động + nuôi trồng nhập), lũy kế | FN-16, UR-TINH-14, 15 | L | CN2, X6 | Có (dòng điều chỉnh, nuôi trồng) |
| CN5 | Báo cáo phòng BM-11: soạn tự động + phần viết, LĐ duyệt (`cap/workflow`), gửi theo nơi nhận, lưu phiên bản, "Số liệu bổ sung" | FN-16, UR-TINH-16 | L | CN4, X7, câu hỏi 2 | Có |
| CN6 | Lịch: BM-03 lịch trực KSGM (lưới, Excel, cảnh báo), BM-07 lịch tiêm sinh từ quy trình, BM-05/06 lịch công tác + **check-in GPS** (mở rộng `schedule.go` + `GpsLog`) | FN-12, FN-15 | L | CN1, X3 | Có |
| CN7 | Hộ mô hình, điểm trình diễn/điểm nuôi, chương trình/dự án; cấp quyền xã/nông hộ (FN-14) | FN-10 (phần hộ), FN-14, FN-19 (phần CT), UR-XA-04, UR-TINH-09, 19 | L | H-1, câu hỏi 14, KH1 (danh sách CT) | Có: `pf_coop` (site, batch) + `pf_extension` (CT/DA) |
| CN8 | Nhật ký BM-08/09/19: lứa, lần ghi có chỉ số, GPS bất biến + khoảng cách 200 m + giả lập, ảnh `cap/evidence`, offline, chủ nhiệm xác thực/yêu cầu bổ sung, hạch toán NC-15 | FN-10, FN-13, UR-XA-11, 12, UR-TINH-12 | XL | CN7, CN3 | Có: sổ `pf_coop` mới, entity đồng bộ mới |
| CN9 | Dịch bệnh BM-10 (ổ dịch có phiên bản diễn biến) + báo nghi dịch | FN-11, UR-XA-10 | M | CN1, X3 | Có |
| CN10 | Chiến dịch điều tra mở rộng (đợt trên kỳ `adhoc`, tách lũy kế) | FN-21, UR-TINH-26, UR-XA-14 | M | CN2 | Có (nhỏ) |
| CN11 | Báo cáo cho KHTC trong menu CNTS + nguồn module CNTS | GD-01, GD-03 | S | X5, X6 | — |
| CN12 (GĐ2) | Bản đồ vùng nuôi/dịch: vẽ vùng, bán kính, hướng di chuyển, theo ngày, KML/GeoJSON | FN-18, UR-TINH-17 | L | CN9, CN7 | Có (`pf_extension` polygon) |
| CN13 (GĐ2) | Dự án KNQG Mẫu 01/03/04/05/07, ký scan, TW xác nhận | FN-19, UR-TINH-20…24, UR-QG-01 | L | CN7, CN8, X7 | Có |
| CN14 (GĐ2) | Trang chủ phòng + kho văn bản (`pf/content`) | FN-17, FN-20 | M | CN4, X6 | `_pub` |
| CN15 (GĐ2) | Nông hộ Mobile: OTP SMS, nhật ký hộ mình, lịch tiêm + nhắc + xác nhận, báo nghi dịch | UR-NH-01…03 | L | CN8, CN9, vault | Có (nhỏ); cần nhà cung cấp SMS |

##### 3.2 KHTC

| Mã | Gói | FN | Cỡ | Phụ thuộc | Migration |
|---|---|---|---|---|---|
| KH1 | Danh mục KHTC + Kế hoạch năm (CT/nhóm/hạng mục, 6 tab, phòng phụ trách, nguồn số liệu, phiên bản theo QĐ + ngày hiệu lực, thêm/ngừng CT, nhập/xuất Excel, sao chép năm trước) | FN-AD-04.01, FN-TI-02.02 | L | X2 | Có: `pf_extension` sổ kế hoạch + phiên bản |
| KH2 | Kỳ KHTC: mở kỳ bằng ô chọn, sinh phiếu theo phòng, gắn kỳ nguồn, tiến độ phiếu + nguồn, nhắc, đóng/hủy kỳ, 3 máy trạng thái | FN-TI-02.03 | L | X1, X4, KH1 | Có |
| KH3 | Mẫu phiếu (kiểu cột "Từ Kế hoạch năm", "Lũy kế kỳ trước"), nhập phiếu, điều chỉnh kỳ trước, rà soát/so sánh lần gửi | FN-AD-04.02, FN-PN-03.02, FN-TI-02.04 | L | KH2, X5 | Có (`pf_reports` kiểu cột) |
| KH4 | Bộ đọc nguồn module (Trồng trọt, CNTS, Giá) + "Cập nhật số liệu" + mốc dữ liệu + "Nguồn đã thay đổi" + "Nguồn dữ liệu" | BR-DL-02…04 | M | X6 | — |
| KH5 | Mục lục mẫu (mục, lặp, khuôn câu có biến, khóa, xem trước theo tình huống, phát hành, nhân bản) | FN-AD-04.03 | XL | X7, KH1 | Trong `pf_export` |
| KH6 | Màn soạn + Mẫu 1–5 + xuất Word/Excel + Bản cuối | FN-TI-02.05…09 | XL | KH4, KH5 | Có |
| KH7 | Số liệu nguồn ngoài (nhập, Excel hai bước, minh chứng) | FN-TI-02.11 | M | KH2 | Có |
| KH8 | Báo cáo phát sinh + yêu cầu số liệu bổ sung | FN-TI-02.10 | M | KH6, X5 | Có |
| KH9 | Trang chủ tổng hợp + báo cáo đã xuất cho TW/lãnh đạo/phòng | FN-TI-02.01, FN-TW-01.01, FN-PN-03.03 | M | KH6 | `_pub` |

##### 3.3 Báo giá

| Mã | Gói | FN | Cỡ | Phụ thuộc | Migration |
|---|---|---|---|---|---|
| BG1 | Khung module `pf/price`: `module.yaml`, `modgen`, vai `r_price`, `pf_price`/`pf_price_pub`, `agnofloat`, thêm vào `app/KN/app.yaml` (và NH, MKG khi dùng) | — | M | ADR mới (`cap/price`), X0 | Có: schema mới |
| BG2 | Xã báo giá (↔ ĐVHC có hiệu lực, nhóm vùng), phân công CTV/Tỉnh theo ngày hiệu lực, cờ QTNV | FN-AD-04.01, 04.02 | M | BG1, X3 | Có |
| BG3 | Mặt hàng theo thời kỳ, nhóm, gán xã, đề xuất, bộ theo vụ, "thay thế cho" | FN-AD-04.03 | M | BG1, câu hỏi 9 | Có |
| BG4 | Lịch báo giá + ngoại lệ + giờ khóa (job khóa ±1 phút) + hạn phản hồi + nhắc (`pf/notify`) + ngưỡng | FN-AD-04.04…06, FN-XA-03.04 | M | BG1 | Có |
| BG5 | Nhập báo giá (Web responsive), cảnh báo tạm tính, xác minh hai chiều, nhập thay (Nộp muộn), hiệu chỉnh/xóa mềm có lưu gốc, in phiếu File 2 (PDF), lịch sử | FN-XA-03.01…03, 03.05, FN-TI-02.02…04, 02.09 | L | BG2…BG4 | Có |
| BG6 | Duyệt/mở duyệt bảng giá có phiên bản, khóa "đang duyệt", `price.published` | FN-TI-02.05, 02.06 | M | BG5 | Có |
| BG7 | Báo cáo: tiến độ, bảng giá tổng hợp, biến động, dashboard vùng, thống kê phiếu theo CTV | FN-TI-02.01, 02.08, FN-TW-01.01…03 | M | BG6 | `_pub` |
| BG8 | Điều tra giá thành: đợt, nhập file hai bước (ánh xạ cột + từ đồng nghĩa + lưu dòng lỗi — kích hoạt tách `cap/import`?), PII → vault, chống trùng, CT-05/06, rà soát, chốt, báo cáo giá thành | FN-TI-02.10…12, FN-TW-01.04 | XL | BG1, BG3, vault | Có |
| BG9 | Chuyển dữ liệu ≥ 3 tháng báo giá cũ + Hè Thu 2026 | A5 | M | BG5, BG8 | Nhập dữ liệu |
| BG10 | Cấp cho KHTC: số phiếu giá theo tuần báo cáo + bảng giá đã duyệt (view `_pub`) + FN-PN-03 trong menu TTTT | GD-04 | S | X5, X6, BG6 | `_pub` |
| BG11 (Đợt 2) | Website: tập dữ liệu `price_board_daily` qua cổng tích hợp (feed cursor) hoặc xuất file | FN-TI-02.07 | M | BG6, A2 | Hợp đồng `contracts/integration` |

##### 3.4 Thứ tự đề xuất

1. **Tuần 0 — X0** (chốt câu hỏi 1–4, 9, 14, 17 với khách). Không có X0 thì X1, X4, CN2, KH2 phải làm lại.
2. **Đợt A (song song hai nhánh):**
   - Nhánh nền: X1 → X2 → X3 → X4.
   - Nhánh giá (độc lập với nền, chỉ cần X2/X3 nhẹ): BG1 → BG2, BG3, BG4 → BG5 → BG6 → BG7 → BG8 → BG9. Báo giá có hệ thống cũ đang chạy và chỉ số thành công rõ — giao sớm được.
3. **Đợt B — CNTS GĐ1:** CN1 → CN2 → CN3, CN4, CN6, CN9, CN10 → CN5 (cần X7 hoặc tạm xuất Excel/PDF) → CN7 → CN8.
4. **Đợt C — KHTC:** X6, X7 → KH1 → KH2 → X5 (+ CN11, BG10, Trồng trọt) → KH3 → KH4 → KH5 → KH6 → KH7, KH8, KH9. KHTC đi sau vì phụ thuộc nguồn "đã chốt" của mọi phòng và phiếu trong module mọi phòng.
5. **Đợt D — GĐ2/Đợt 2:** CN12, CN13, CN14, CN15, BG11, phần Đợt 2 của BG7.

Mốc G1 (15/12/2026, v4.1 Phụ lục A) yêu cầu KN chạy trên `cap/import`, `cap/forms`, `cap/price` tối thiểu → BG1–BG6 và X4 nên vào trước mốc này.

##### 3.5 Câu hỏi mở (gồm mâu thuẫn giữa các BRD)

**Mâu thuẫn giữa các BRD / với repo**

1. **Tuần báo cáo.** KHTC GD-05/BR-DL-01: tuần **Thứ Tư – Thứ Ba**, "đã chốt, áp dụng bắt buộc mọi module; BRD CNTS cập nhật kỳ tuần BM-04, BM-17". CNTS v3.7 (cùng ngày 07/10) vẫn ghi **Thứ Ba – Thứ Hai**, hạn 12:00 Thứ Ba, tuần vắt quý tính theo **Thứ Hai**. Repo `reportcycle/domain/period.go`: tuần bắt đầu **Thứ Hai**, hạn "Thứ Ba tuần sau 17:00", Q-17 tạm. Báo giá theo ngày nhưng KHTC cần "số phiếu theo tuần báo cáo". Chốt một lịch duy nhất? KSGM 4 tuần/tháng (1–7, 8–14…) có giữ song song không?
2. **Đường CNTS → KHTC.** CNTS: báo cáo tháng/quý/năm (FN-16) **gửi Phòng KHTC** vào hộp "Báo cáo đến", vai khách "Phòng nhận báo cáo". KHTC: "các phòng không gửi file báo cáo cho KHTC", "FN-16 Module CNTS không là nguồn", KHTC chỉ lấy dữ liệu module đã chốt + phiếu FN-PN-03. CNTS v3.7 **không có** FN-PN-03.01–03. Giữ cả hai (BM-11 gửi để lưu, phiếu để tổng hợp) hay bỏ hộp "Báo cáo đến"?
3. **Mô hình sửa số kỳ cũ.** CNTS: sửa kỳ cũ được (trước chốt/khi mở lại), lũy kế kỳ sau tự tính lại. KHTC BR-DL-12: không sửa kỳ đã khóa, ghi "Điều chỉnh kỳ trước" ở phiếu kỳ kế tiếp, điều chỉnh chỉ vào lũy kế. Khi KHTC đọc lũy kế CNTS (Lấy từ module), lũy kế nào là chuẩn — của CNTS (đã tính lại) hay của KHTC?
4. **"Đã chốt" của nguồn.** KHTC cần trạng thái "đã chốt" cho kỳ nguồn theo tuần báo cáo. CNTS chốt theo từng biểu × kỳ (BM-04, BM-17 riêng), Phụ lục 2 nuôi trồng do cán bộ tổng hợp nhập (không có chốt). Trồng trọt chốt theo đợt phát hành. Định nghĩa "nguồn CNTS đã chốt cho tuần X" là gì?
5. **Trùng mã chức năng.** Ba BRD dùng cùng tiền tố với nghĩa khác: FN-AD-04.03 = Danh mục phòng ban (Trồng trọt v7.7) = Mục lục mẫu báo cáo (KHTC) = Danh mục mặt hàng (Báo giá); FN-AD-04.06 = Quản lý mẫu báo cáo (Trồng trọt, KHTC tham chiếu) = Ngưỡng cảnh báo (Báo giá); FN-TI-02.0x, FN-TW-01.0x cũng trùng. CNTS dùng FN-01…21. Đề nghị thêm tiền tố module (vd `TT-`, `CN-`, `KH-`, `BG-`) trước khi đưa vào ma trận nghiệm thu.
6. **Phiên bản BRD Trồng trọt tham chiếu.** CNTS → v6.3 (FN-01…05), Báo giá → v6.0 (FN-01/04/05), KHTC → v7.7 (FN-AD-04.06). Repo có v7.7 (`docs/brd/kn-trong-trot-v7.7.md`). Phần dùng chung theo v7.7 có khác v6.x (vd đăng nhập 2 bước "chọn vai trò rồi phòng ban", Nông hộ OTP) không?
7. **Tên và số phòng ban.** Báo giá/KHTC: "Phòng Thông tin Tuyên truyền"; repo seed và Trồng trọt v7.7: "Phòng Thông tin - Truyền thông". KHTC nêu Phòng **Hành chính tổng hợp** (có BRD riêng `BRD_Khuyen_nong_Module_Hanh_chinh_Tong_hop_v3_0.docx` trong cùng thư mục) — seed chỉ có `phong_ban_5/6` tạm (Q-02).
8. **Số lần gửi phiếu KHTC.** BR-DL-11/UR-PN-03: "không giới hạn số lần gửi lại". FN-TI-02.03/02.04: "Số lần gửi tối đa 3 (cố định)", gửi lần 3 tự khóa. Chọn một.
9. **Chủ danh mục mặt hàng giá.** KHTC FN-AD-04.01: "Mặt hàng giá cả (nhóm hàng, mặt hàng, ĐVT)" trong Danh mục KHTC do Admin quản lý. Báo giá FN-AD-04.03: Phòng TTTT là chủ danh mục, QTNV quản lý theo thời kỳ. Đề xuất: một danh mục ở `cap/price`, KHTC đọc.
10. **Phân nhóm địa bàn.** CNTS: "Khu vực" (huyện cũ, thuộc tính Nút xã, 102 xã). Báo giá: "Nhóm vùng" An Giang / Kiên Giang (26 xã). Repo có `Regions` (Q-04). Hai cách gom cùng tồn tại? Báo cáo KHTC "tập trung tại các xã" theo cách nào?
11. **Cấp Tỉnh sửa số gốc.** CNTS, Trồng trọt: Cấp Tỉnh **không sửa** số gốc của xã, chỉ yêu cầu điều chỉnh. Báo giá FN-TI-02.04: Cấp Tỉnh **sửa giá, sửa ngày phiếu, xóa phiếu**. Chấp nhận khác nhau theo phòng? (Repo ghi được dưới dạng bản ghi hiệu chỉnh + xóa mềm trên sổ chỉ ghi thêm.)
12. **Ngưỡng mẫu nhỏ.** Báo giá: "Mẫu nhỏ" khi < 3 phiếu, vẫn hiển thị số; ngưỡng cờ trung vị cần ≥ 5 phiếu. Nền tảng: k sàn 10 (`publicdata.k_min`) khi rời tenant (ADR-0035). Báo cáo giá thành cho TW/website có áp k không, k bao nhiêu?
13. **Vai Cấp TW khác nhau theo phòng.** CNTS: TW chỉ thấy dự án KNQG, không thấy số liệu xã. KHTC: TW xem báo cáo đã xuất. Báo giá: TW xem bảng giá, giá thành. Trồng trọt: TW xem dashboard + báo cáo xã. Ngữ cảnh `kn_tw` hiện là "Nút xuống dưới ∩ phòng ban" — cần quyền theo phòng × chức năng.
14. **Chủ hộ/điểm/lứa chăn nuôi – thủy sản.** Mở rộng `dom/cooperative` (đề xuất) hay module domain mới `dom/husbandry`? Gắn với H-1 (chủ dữ liệu là chương trình KN hay HTX). Hộ mô hình CNTS có trùng hộ trong `pf_coop.household` của Trồng trọt/HTX không (cùng `household_ref`)?
15. **Chương trình/dự án trùng giữa CNTS và KHTC.** KHTC Kế hoạch năm có CT VII–XII (gia súc, gia cầm, thủy sản…) do CNTS phụ trách, kèm "CT, DA năm trước (thu – chi)", "dự án TW". CNTS UR-TINH-19 tự quản lý CT/DA/điểm (loại KN tỉnh, KNQG, hợp tác). Một danh sách chung (KHTC là chủ, CNTS gắn điểm/hộ)? Kết quả thu – chi: KHTC lấy từ phiếu (phòng nhập lại) trong khi CNTS tính từ nhật ký NC-15 — nhập hai lần.
16. **Ai thiết kế biểu.** CNTS: Cấp Tỉnh tự thiết kế biểu, mẫu nhật ký (UR-TINH-07). Trồng trọt v7.7: Admin quản lý mẫu từ Excel (FN-AD-04.06). KHTC: Admin tạo mẫu phiếu, KHTC cấu hình mục lục. Báo giá: QTNV. `forms.Profile.DefineBy` hiện là Admin KN + `reports_admin`.
17. **Cấp TW là tenant nào?** Nếu TTKN QG là tenant riêng, mọi màn TW (KNQG Mẫu 07, bảng giá, báo cáo đã xuất) cần cấp phép `intra_platform`/tập dữ liệu; nếu cùng cây tenant An Giang thì chỉ là phạm vi Nút.
18. **Lịch công tác.** CNTS: cán bộ xã đăng ký Thứ Năm/Thứ Sáu, có **check-in tại địa điểm**, người duyệt = "người theo dõi được phân công". Trồng trọt v7.7: người duyệt = Trưởng phòng (Q-05), không check-in. Repo `schedule.go` không có check-in. Một quy tắc chung hay theo phòng?
19. **Đợt điều tra hai kiểu.** CNTS FN-21: biểu điều tra nhập trong app, theo xã. Báo giá FN-TI-02.10–12: không có màn nhập, chỉ nhập file Google Form. Có muốn gom thành một năng lực "đợt điều tra" (kỳ `adhoc` của `cap/forms`) cho cả hai?
20. **Đăng nhập Nông hộ/CTV.** CNTS: Nông hộ SĐT + **OTP SMS**. Báo giá: email/SĐT + mật khẩu, PIN 6 số. Trồng trọt v7.7: PIN 6 số, offline 72 h. Repo chưa có OTP SMS (chỉ PIN/Keycloak); cần nhà cung cấp SMS + trần chi phí (v4.1 `cap/notify`).
21. **Báo giá thiếu Mục VII.** Báo giá dẫn "Mục VII (điểm 9)" và "cần xác nhận, Mục VII" (FN-TI-02.06 giới hạn 30 ngày, FN-AD-04.04 lịch T2–T6) nhưng tài liệu kết thúc ở Mục VI; người lập (BA) bỏ trống ở trang phê duyệt; mục "Sẵn sàng, sao lưu" trống.
22. **Báo giá chưa có yêu cầu của KHTC.** KHTC §3.4 yêu cầu BRD Báo giá thêm "bảng giá đã duyệt, số phiếu giá đã thu thập theo tuần báo cáo" và FN-PN-03.01–03 — v1.3 chưa có. Giá "từ – đến" trong Mẫu 1 mục A.III là min/max toàn tỉnh (CT-04) hay của xã nào? "2 ngày lấy giá gần nhất đến ngày chốt" khi ngày chưa duyệt thì sao?

**Câu hỏi nghiệp vụ còn lại**

23. CNTS BM-19 mẫu nhật ký thủy sản chưa có ("Chờ phòng gửi mẫu sổ") — chặn CN8 phần thủy sản.
24. CNTS Phụ lục 2 nuôi trồng (~20 đối tượng): nhập theo tuần hay theo tháng; có lũy kế nhập tay không?
25. CNTS file chia sẻ Chi cục: kênh nào (email như Trồng trọt FN-TI-02.06, Google Sheet)? Có phải đăng ký như "báo cáo đã gửi cơ quan ngoài" để KHTC mục A.II.3.1 lấy không?
26. KHTC xuất .docx "mở và chỉnh sửa được trên Word 2016+ và WPS": chấp nhận bộ ghi .docx tự viết (không có thư viện trong repo) hay dùng mẫu .docx gốc của TTKN làm khung?
27. Báo giá: thống kê phiếu theo CTV là căn cứ thanh toán — cần ký số/khóa tháng không? Phiếu in File 2 có cần chữ ký số?
28. Báo giá ngày nghỉ, ngày bổ sung: lịch T2–T6 mặc định đã xác nhận chưa (FN-AD-04.04 BR-04)?
29. CNTS mobile: dùng `apps/field-app` (KN hiện trường) cho cán bộ xã và `apps/htx-app` chế độ nông dân (NH) cho nông hộ, hay app riêng?
30. ATTT cấp độ 3 + NĐ 13/2023: cần DPIA cho nhật ký có GPS hộ và phiếu điều tra có SĐT nông hộ trước khi nhận dữ liệu thật (G1)?
