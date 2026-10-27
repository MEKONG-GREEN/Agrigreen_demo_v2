# Đối chiếu 14 mẫu khởi tạo Trồng trọt với 3 tệp Excel gốc — BF-TT-FORMS (#107, PRD UP-04)

Ngày đối chiếu: 27/10/2026. Mã: `internal/kn/extension/module_reports_seeds.go` (seed `kn-v7.7-seed-2026-10-01`) trên `origin/dev` `28d05240`.

## 1. Nguồn

Thư mục `Product team/01. Tai Lieu Khuyen Nong/1.1 Khuyen Nong An Giang/02. TRỒNG TRỌT/`, chỉ đọc. Mở bằng openpyxl (Python `-I`) và `cmd/formimport` (excelize), không chạy macro.

| Tệp | Cập nhật | SHA-256 | Sheet dùng làm mẫu |
|---|---|---|---|
| Báo cáo cây lúa, rau màu hằng tuần.xlsx | 23/10/2026 | `daf068990aa382e3a986ed42051cbdaf8a9e03bd5045f3f59566f001471fb7c0` | T01 `PL 01_Tđộ XG TĐ 2026`, T02 `PL 01B_Tđộ XG lúa mùa 2026`, T03 `PL 02_Phân trà lúa TĐ 2026`, T04 `PL 03_Màu TĐ 2026`, T05 `Tuần 36_PL04_Sâu bệnh hại lúa T`, T06 `Tuần 36_PL05_Sâu bệnh cây trồng`, T07 `PL 07 - Tđộ thu hoạch` |
| Báo cáo cây ăn trái hằng tháng.xlsx | 24/09/2026 | `d4fc4624f6436eed40815b176df1f4c0a7f88560cb19117268bb67fd5a2a14f5` | M01 `Tháng 7` (còn `Tháng 62026`, `Lũy kế (chỉ xem)`, `Cả năm`) |
| Báo cáo áp dụng TBKT, CGH, CCG hằng vụ.xlsx | 24/09/2026 | `277010564981b442595099bc76bd25a33af22ebe4bcf2ee00ed4207f923eeedf` | V01 `PL1 áp dụng 3G3T HT26 ` (có dấu cách cuối), V02 `PL2Áp dụng 1P5G HT26`, V03 `PL3CGH HT26`, V04 `PL4 CCG`, V05 `PL5LUONG GIONG GIEO SA HT26`, V06 `PL6MO HINH HT26` |

Tệp tuần còn các sheet theo tuần (`33_…`, `34-…`, `31_…`, `Tuần 35_…`) cùng bố cục PL04/PL05, sheet `HD_TĐ` (hướng dẫn) và `TỒNG HỢP RAU MÀU_TĐ26` (tổng hợp tỉnh) — không phải mẫu nhập của xã.

## 2. Cách đối chiếu

1. `go run ./cmd/formimport -in <tệp> -sheet <sheet> -code <mã> -out <mã>.json` cho từng sheet (T07 cần `-header-start 3 -data-row 6`). Bộ nạp đọc tiêu đề gộp, chữ cái cột, công thức dòng dữ liệu, cột cố định.
2. Xuất định nghĩa seed (`extension.ReportFormSeeds()`), so số dòng tiêu đề, số cột, ô gộp, cột công thức, cột cố định (freeze), chỉ tiêu (dòng theo đối tượng).
3. Đọc lại tiêu đề từng sheet để xác nhận tên cột và ô "Yêu cầu biểu mẫu" (A1).

## 3. Kết quả theo mẫu

"Seed" = định nghĩa khởi tạo hiện có; "Tệp" = định nghĩa bộ nạp dựng từ sheet gốc. Lệch nêu ở cột cuối.

| Mã | Dòng tiêu đề seed/tệp | Cột seed/tệp | Ô gộp seed/tệp | Công thức seed/tệp | Cố định tới seed/tệp | Đánh giá |
|---|---|---|---|---|---|---|
| T01 | 2 / 3 | 15 / 15 | 9 / 10 | 1 / 1 | C / E | **Gần đúng.** Cùng 15 cột: KH tỉnh giao, KHXG, 7 tuần, lũy kế. Lệch: tệp có 3 dòng tiêu đề, cố định tới E; N, O dùng `MAXIFS/MINIFS` trên dòng tiêu đề (tuần trỗ sớm/muộn) — engine không hỗ trợ, seed để cột nhập. Ô A1 có "Yêu cầu biểu mẫu". |
| T02 | 2 / 3 | 18 / 9 | 8 / 7 | 2 / 1 | C / D | **Lệch.** Tệp 9 cột: KHXG, Tuần 1–4, Lũy kế. Seed có 7 tuần + KH tỉnh giao + phân trà (18 cột). |
| T03 | 2 / 5 | 15 / 30 | 9 / 5 | 1 / 0 | C / E | **Lệch.** Tệp có DTKH, DTXG (lấy từ PL 01 bằng tham chiếu sheet), giai đoạn Mạ, Đẻ nhánh, Đòng, Trổ, Chín, Thu hoạch, % DTXG… 30 cột, 5 dòng tiêu đề. Seed còn "Trà 1–5 (chờ khách Q-01)". |
| T04 | 3 / 7 | 226 / 226 | 73 / 72 | 60 / 3 | J / F | **Đúng số cột.** Tệp 7 dòng tiêu đề (nhóm cây × XG/TH); E, G, I là `SUMIF` theo nhãn dòng tiêu đề — engine không hỗ trợ tham chiếu dòng tiêu đề; seed thay bằng 60 cột công thức theo nhóm. Cần rà tên nhóm cây từng khối. |
| T05 | 2 / 8 | 8 / 63 | 6 / 23 | 1 / 0 | D / C | **Khác cách dựng.** Tệp: mỗi xã một dòng, 60 cột = đối tượng sâu bệnh × mức Nhẹ/TB/Nặng (+ dòng tổng `SUM` trên đầu). Seed: dòng theo chỉ tiêu (20 đối tượng × xã), 3 cột mức. Hai cách cho cùng số liệu; giữ cách nào cần PO chốt (xem §5). |
| T06 | 2 / 8 | 8 / 213 | 6 / 92 | 1 / 0 | D / C | **Khác cách dựng** như T05: tệp 210 cột = cây trồng (Khóm, Dừa…) × đối tượng × mức; seed 70 chỉ tiêu dòng. |
| T07 | 3 / 3 | 24 / 43 | 10 / 26 | 12 / 0 | D / — | **Lệch.** Tệp: KHXG + 9 tuần thu hoạch × (Diện tích, NS lúa tươi, NS lúa khô, Sản lượng) + Lũy kế (DT, NS khô, SL). Seed: 5 khối × 4 cột "chờ khách Q-01". Tiêu đề tuần trong tệp là ngày cụ thể (16/6–22/6…) — trong hệ thống là nhãn kỳ. |
| M01 | 2 / 6 | 183 / 183 | 33 / 0 | 60 / 0 | C / D | **Đúng số cột, khác tên.** Tệp 6 dòng tiêu đề, nhóm cây ghi ở ô đầu khối, không gộp ô (chỉ gộp B7:B10 ở dữ liệu); mỗi cây 6 cột: DT hiện có, DT tăng/giảm, Giải thích, DT thu hoạch, NS, SL. Cột "DT hiện có" lấy từ sheet `Tháng 62026` (tham chiếu sheet khác). Seed đặt tên khác ("Trồng mới", "Trồng các năm trước", "Diện tích cho trái"). |
| V01 | 1 / 4 | 7 / 14 | 0 / 2 | 1 / 0 | C / E | **Khác cách dựng.** Tệp là bảng chỉ tiêu (TT, Chỉ tiêu, Mật độ sạ ≤ 150 kg/ha, phân bón Urê/DAP/Kali/NPK, thuốc BVTV…) cấp tỉnh, không theo xã. Seed theo xã. |
| V02 | 1 / 4 | 16 / 106 | 0 / 1 | 0 / 10 | C / — | **Khác cách dựng** như V01 (1 phải 5 giảm): bảng chỉ tiêu × nhiều khối. |
| V03 | 2 / 3 | 31 / 44 | 17 / 25 | 0 / 0 | C / C | **Lệch.** Tệp: tư liệu sản xuất theo loại máy (Trạm bơm, Máy san laser, Máy cày, Máy cấy…) × (số lượng, diện tích…). Seed: 14 cặp "Số lượng/Diện tích (chờ khách Q-01)". |
| V04 | 2 / 3 | 44 / 45 | 5 / 2 | 1 / 1 | D / — | **Gần đúng.** Tệp có thêm DTKH; tên giống cụ thể (Jasmine 85, OM 4218, OM 6073, OM 4900, OM 5451, OM 2514, OM 2517…) thay "Giống 1…". |
| V05 | 2 / 3 | 12 / 13 | 8 / 9 | 5 / 1 | D / D | **Gần đúng.** 4 mức lượng giống × (Diện tích, %). Tệp chỉ có 1 cột công thức (tổng); % trong tệp là số nhập. |
| V06 | 1 / 4 | 9 / 8 | 0 / 6 | 1 / 0 | C / — | **Lệch.** Tệp không có cột Khu vực; mô hình cụ thể: Đề án 1 triệu ha lúa CLC, IPM, IPHM, Cánh đồng lớn, Khác. |

Bộ nạp báo thêm: công thức tham chiếu sheet khác (T03 lấy DTKH/DTXG từ PL 01), công thức trên dòng tiêu đề (T01, T04), `SUM` theo cột cho dòng tổng (T05, T06, M01, V02) — các trường hợp này để cột nhập, Admin sửa trong trình thiết kế (dòng tổng do mẫu tự tính theo `totals`).

## 4. Màn nhập Cấp Xã (đã làm trong #107)

- Thanh chọn kỳ lùi/tiến (cùng đợt × biểu × xã), có hạn nộp và tên mẫu.
- Header: Lịch sử sửa, Tải mẫu Excel (tệp đúng bố cục của phiên bản mẫu), Nhập từ Excel (máy chủ đọc tệp theo hàng chữ cái cột, web ghép theo tên xã / nhãn chỉ tiêu, chỉ điền ô nhập được của kỳ đang mở), Lưu nháp, Gửi.
- Mỗi xã một tab kèm trạng thái; khung "Yêu cầu biểu mẫu" luôn hiện; chú thích màu ô; bộ lọc Biểu đủ T01–T07, M01, V01–V06 (Công việc cần làm, Rà soát & chốt kỳ).
- Không đổ dữ liệu mẫu: lưới chỉ có giá trị đã lưu của công việc, Tỉnh giao và giá trị kỳ trước (chỉ đọc).

## 5. Đề xuất và việc cần PO chốt

1. **Không sửa seed bằng mã.** Mẫu đã cài cho phòng (SeedForms chạy một lần) không đổi khi sửa seed. Cách đúng theo FN-AD-04.06: Admin nạp từng sheet ở §1 (POST `/v1/reports/forms/import` hoặc `cmd/formimport`) thành **phiên bản nháp mới** của T01…V06, rà trong trình thiết kế (đặt loại cột Tỉnh giao/Kỳ/Công thức, dòng tổng), rồi phát hành. Lệnh mẫu:
   `go run ./cmd/formimport -in "Báo cáo cây lúa, rau màu hằng tuần.xlsx" -sheet "PL 07 - Tđộ thu hoạch" -code T07 -header-start 3 -data-row 6 -out t07.json`
2. **T05, T06 (sâu bệnh):** chọn giữ dòng theo đối tượng (seed, dễ nhập trên di động) hay cột theo đối tượng × mức như tệp (213 cột). Đề xuất giữ seed, xuất Excel theo bố cục tệp.
3. **V01, V02:** tệp là bảng chỉ tiêu cấp tỉnh, không theo xã — xác nhận xã có nhập hai biểu này không.
4. **T01, T04 công thức trên dòng tiêu đề** (`MAXIFS`, `SUMIF` theo nhãn): engine chỉ tính trong dòng; cần PO chấp nhận cột nhập hoặc mở rộng engine (lane:shared, việc riêng).
5. Câu hỏi mở của PRD vẫn còn: tệp tuần cập nhật 23/10 đã là bản cuối cho TT-14 và UP-04 chưa.
