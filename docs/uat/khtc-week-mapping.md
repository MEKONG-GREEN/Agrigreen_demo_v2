# Quy đổi kỳ nguồn sang tuần KHTC: ví dụ có số để ký

Gói KHTCQA, 13/10/2026. Tài liệu này dành cho PO và Phòng Kế hoạch – Tài chính (KHTC) đọc, sửa và ký trước khi các gói W11.62 (kỳ KHTC), W11.64 (bộ đọc nguồn) và W11.66 (Mẫu 1–5) viết mã. Mỗi ví dụ dưới đây có bản máy đọc được ở [`tests/golden/khtc/`](../../tests/golden/khtc/) (`cases.json`, `crosswalk.csv`). Mã ca (G01…G06) giống nhau ở hai nơi. Các gói sau dùng bộ dữ liệu này cho kiểm thử tích hợp. Khi PO sửa một con số ở đây thì phải sửa cả trong `cases.json`.

Ma trận nghiệm thu: [khtc-v2.2-acceptance.md](khtc-v2.2-acceptance.md). BRD: KHTC v2.2 (BR-DL-01, 02, 04, 05, 06, 09, 14; Mục II.4).

## 1. Quy tắc (quyết định 13/10/2026)

| # | Quy tắc | Nguồn |
|---|---|---|
| Q1 | Tuần KHTC chạy từ **Thứ Tư đến Thứ Ba**. Tuần 1 là tuần chứa ngày 01/01, và tuần thuộc năm của ngày Thứ Ba cuối tuần. Năm có 52 hoặc 53 tuần. Ví dụ: Tuần 41/2026 = 07/10–13/10/2026, Tuần 45/2026 = 04/11–10/11/2026. | BR-DL-01, Mục II.4; lịch kỳ W11.03 (`DefaultRule`) |
| Q2 | Mỗi phòng giữ lịch kỳ của mình. **Trồng trọt** dùng biểu `reportcycle` cũ với tuần Thứ Tư – Thứ Ba (trùng tuần KHTC). Các đợt phát hành trước 04/11/2026 vẫn dùng tuần thứ Hai – Chủ nhật đánh số ISO (quy tắc v1). **CNTS** dùng biểu riêng với tuần **Thứ Ba – Thứ Hai**. **Báo giá** dùng bảng giá ngày đã duyệt. | QĐ 13/10/2026 |
| Q3 | Mỗi kỳ nguồn được tính **trọn** vào tuần KHTC **chứa ngày cuối** của kỳ nguồn. Không chia số theo ngày. Với bảng giá thì ngày cuối là chính ngày báo giá. | QĐ 13/10/2026 |
| Q4 | **Lũy kế của nguồn là chuẩn.** KHTC không tự cộng lại số của nguồn. Số **trong kỳ** của tuần K = lũy kế nguồn của tuần K − **gốc**. Trong đó **gốc** = lũy kế **đã đóng** của tuần K−1 nếu tuần K−1 đã đóng, và = lũy kế hiện tại của tuần K−1 nếu tuần đó còn mở. Khi nguồn bắt đầu chuỗi lũy kế mới (năm mới, vụ mới) thì gốc = 0. Số trong kỳ có thể âm. | QĐ 13/10/2026; Q-047 |
| Q5 | Lũy kế của một tuần KHTC là lũy kế của kỳ nguồn **mới nhất** quy về tuần đó. Nếu không có kỳ nguồn nào quy về tuần đó thì giữ lũy kế của tuần trước, và số trong kỳ = 0. | Hệ quả của Q3, Q4 |
| Q6 | Tuần KHTC **"đã chốt"** với một nguồn khi **mọi** kỳ nguồn quy về tuần đó đã chốt. Khi nguồn đang mở lại một kỳ, KHTC chỉ thấy ảnh chụp đã chốt gần nhất của kỳ đó (W11.10) và tuần KHTC thành "Tạm tính". | QĐ 13/10/2026; BR-DL-02, 03; Q-048 |
| Q7 | Tuần KHTC đã đóng thì **giữ nguyên số**. Nếu nguồn đổi sau đó, bản đã xuất chuyển sang "Nguồn đã thay đổi" (BR-DL-04), và phần chênh lệch đi vào tuần KHTC đang mở kế tiếp (theo Q4). | QĐ 13/10/2026; BR-DL-04 |
| Q8 | Mẫu 1 mục A.III "giá từ – đến" lấy **min/max toàn tỉnh** của bảng giá **đã duyệt**. Hai cột "Ngày …" là **hai ngày có bảng giá đã duyệt gần nhất, không sau ngày chốt**. Ngày chưa duyệt thì dùng ngày đã duyệt gần nhất. Nếu trong tuần còn ngày báo giá chưa duyệt thì nguồn giá của tuần là "Tạm tính". | QĐ 13/10/2026; Q-084 |
| Q9 | HTX do Trung tâm KN tạo mang nhãn "Chưa xác nhận". Các HTX này **được tính vào tổng**, và mọi tổng đều tách thành đã xác nhận / chưa xác nhận. Khi xã **nhận** HTX thì chỉ nhãn đổi, tổng giữ nguyên. | QĐ 13/10/2026 |
| Q10 | *(Mặc định, chờ PO)* Báo cáo đã gửi trước khi xã nhận HTX hoặc trước khi nguồn đổi thì **không phát hành lại**. | QĐ 13/10/2026 |
| Q11 | *(Mặc định, chờ PO)* Chỉ **cán bộ KHTC phụ trách** kỳ mới được bấm "Cập nhật số liệu". Mỗi lần bấm, hệ thống lưu lịch sử thay đổi: tuần, trường, giá trị cũ → mới, người bấm, thời điểm. | QĐ 13/10/2026 |

Quy tắc Q3 được kiểm bằng hàm lịch kỳ đã có. Lịch kỳ W11.03 (`reportcycle/domain.PeriodRule.WeekOf`) đủ để dựng bảng quy đổi, không cần hàm mới. Hai kiểm thử thuần đang có:
- `TestKHTCQA_WeekCrosswalkGolden` kiểm từng dòng của `crosswalk.csv`.
- `TestKHTCQA_WeeklySourcesMapOneToOne` chứng minh rằng với mọi lịch tuần của nguồn, mỗi tuần KHTC từ 2026 đến 2031 nhận **đúng một** tuần nguồn. Vì vậy tuần KHTC "trống" chỉ xảy ra khi nguồn không phát hành kỳ.

Phần số học của Q4–Q9 được `khtcgolden.Validate` (`TestKHTCQA_GoldenDatasetConsistent`) kiểm trên số tính tay. Mã KHTC chưa có (W11.62–W11.66).

## 2. Lịch đối chiếu (cuối 2026 – đầu 2027)

| Tuần KHTC (Thứ Tư – Thứ Ba) | Trồng trọt (từ 04/11/2026) | CNTS (Thứ Ba – Thứ Hai) | Trồng trọt đợt cũ v1 (thứ Hai – CN, ISO) |
|---|---|---|---|
| 2026-W45: 04/11–10/11 | 2026-W45: 04/11–10/11 | 2026-W45: 03/11–09/11 | 2026-W45: 02/11–08/11 |
| 2026-W46: 11/11–17/11 | 2026-W46: 11/11–17/11 | 2026-W46: 10/11–16/11 | 2026-W46: 09/11–15/11 |
| 2026-W48: 25/11–01/12 | 2026-W48: 25/11–01/12 | 2026-W48: 24/11–30/11 | 2026-W48: 23/11–29/11 |
| 2026-W52: 23/12–29/12 | 2026-W52: 23/12–29/12 | 2026-W52: 22/12–28/12 | 2026-W52: 21/12–27/12 |
| 2027-W01: 30/12/2026–05/01/2027 | 2027-W01 (trùng) | 2027-W01: 29/12/2026–04/01/2027 | **2026-W53**: 28/12/2026–03/01/2027 |
| 2027-W02: 06/01–12/01/2027 | 2027-W02 (trùng) | 2027-W02: 05/01–11/01/2027 | 2027-W01: 04/01–10/01/2027 |

Mỗi tuần CNTS có 1 ngày nằm ở tuần KHTC trước: ngày Thứ Ba đầu tuần. Mỗi tuần v1 có 2 ngày nằm ở tuần KHTC trước: thứ Hai và thứ Ba.

## 3. Các ca

Số viết theo kiểu Việt Nam: dấu chấm ngăn hàng nghìn, dấu phẩy thập phân. Trong `cases.json`, số là chuỗi decimal (dấu chấm thập phân).

### G01: tuần nguồn vắt qua hai tuần KHTC (CNTS)

Chỉ tiêu: sản lượng thủy sản, lũy kế từ đầu năm (Mẫu 1 A.I.3), đơn vị tấn. Thời điểm xem: 18/11/2026 09:00. KHTC đã đóng tuần 44 với lũy kế 4.812,0.

| Kỳ CNTS | Ngày | Lũy kế nguồn | Tuần KHTC (chứa ngày cuối) |
|---|---|---|---|
| 2026-W44 | 27/10–02/11 | 4.812,0 | 2026-W44 (đã đóng) |
| 2026-W45 | 03/11–09/11 | 4.947,5 | 2026-W45 |
| 2026-W46 | 10/11–**16/11** | 5.079,0 | **2026-W46** (ngày 10/11 thuộc W45 nhưng không chia) |

| Tuần KHTC | Lũy kế | Gốc | Trong kỳ | Lũy kế năm KHTC | Đã chốt |
|---|---|---|---|---|---|
| 2026-W45 | 4.947,5 | 4.812,0 (W44 đã đóng) | **135,5** | 4.947,5 | Có |
| 2026-W46 | 5.079,0 | 4.947,5 (W45 hiện tại) | **131,5** | 5.079,0 | Có |

Trồng trọt kỳ 2026-W46 (11/11–17/11) trùng với tuần KHTC 2026-W46 (quy đổi 1–1).

### G02: tuần trống (Trồng trọt)

Chỉ tiêu: diện tích xuống giống lúa Thu Đông, lũy kế vụ (Mẫu 1 A.I.1), đơn vị ha. Cấp Tỉnh phát hành đợt A cho tuần 45–47 và đợt B từ tuần 49. Không có kỳ nào kết thúc trong tuần KHTC 48 (25/11–01/12). KHTC đã đóng tuần 47 với lũy kế 3.050.

| Tuần KHTC | Kỳ nguồn | Lũy kế | Gốc | Trong kỳ | Đã chốt |
|---|---|---|---|---|---|
| 2026-W48 | (không có) | 3.050 (giữ của W47) | 3.050 | **0** | Có (không có kỳ nguồn nào chưa chốt) |
| 2026-W49 | 2026-W49 | 3.230 | 3.050 | **180** | Có |

Mẫu 1 tuần 48, mục A.I.1, in câu "không phát sinh" (BR-DL-14 mức 3). Không in "Phòng Trồng trọt chưa chốt số liệu".

### G02B: giá Mẫu 1 khi có ngày chưa duyệt (Báo giá)

Mặt hàng: Dưa leo (MH00001), đồng/kg. Có 3 xã báo giá: XBG0001, XBG0002, XBG0003. Ngày chốt tuần 48 là Thứ Ba 01/12/2026.

| Ngày báo giá | Trạng thái bảng giá lúc 01/12 16:00 | XBG0001 | XBG0002 | XBG0003 | Số phiếu |
|---|---|---|---|---|---|
| 25/11 | Đã duyệt | 8.000–9.500 | 8.500–10.000 | 11.000–13.000 | 3 |
| 26/11 | Đã duyệt | 8.000–9.500 | (không nộp) | 11.500–13.500 | 2 |
| 27/11 | Đã duyệt | 8.000–10.000 | 9.000–11.000 | 12.000–14.000 | 3 |
| 30/11 | Đang duyệt | 8.000–10.000 | 9.000–11.000 | 12.000–13.500 | 3 |
| 01/12 | Chưa duyệt | 8.500–10.500 | 9.000–11.000 | 11.000–13.000 | 3 |

| Thời điểm | Cột 1 | Cột 2 | Số phiếu giá tuần 48 | Nguồn giá |
|---|---|---|---|---|
| 01/12 16:00 | Ngày 26/11: **8.000 – 13.500** | Ngày 27/11: **8.000 – 14.000** | **8** (3 + 2 + 3, chỉ các bảng đã duyệt) | Tạm tính |
| 02/12 09:00, sau khi duyệt 30/11 và 01/12 và cán bộ phụ trách bấm "Cập nhật số liệu" | Ngày 30/11: **8.000 – 13.500** | Ngày 01/12: **8.500 – 13.000** | **14** | Đã chốt |

Theo hợp đồng W11.10, ngày chưa duyệt không có dòng trong view `pf_price_pub.v1_board_daily`, nên bên đọc tự lùi về ngày đã duyệt gần nhất.

### G03: sửa lùi lũy kế (Trồng trọt)

Chỉ tiêu và đơn vị như G02. KHTC đã đóng tuần 48 với lũy kế 3.050. Tuần 49 và 50 còn mở.

| Bước | Thời điểm | Nguồn W49 | Nguồn W50 | KHTC W49: lũy kế / trong kỳ / chốt | KHTC W50: lũy kế / trong kỳ / chốt |
|---|---|---|---|---|---|
| s1 | 16/12 15:00 | 3.230, đã chốt | 3.400, đã chốt | 3.230 / 180 / Có | 3.400 / 170 / Có |
| s2: Cấp Tỉnh mở lại W49 | 17/12 09:00 | đang mở lại (ảnh chụp đã chốt 3.230) | 3.400, đã chốt | 3.230 / 180 / **Tạm tính** | 3.400 / 170 / Có |
| s3: chốt lại W49 = 3.190, cán bộ phụ trách bấm "Cập nhật số liệu" | 17/12 15:00 | 3.190, đã chốt | 3.400 (ảnh chụp giữ nguyên) | **3.190 / 140** / Có | 3.400 / **210** / Có |

Lịch sử "Cập nhật số liệu" ở bước s3 (người bấm: cán bộ KHTC phụ trách):
- W49 lũy kế 3.230 → 3.190.
- W49 trong kỳ 180 → 140.
- W50 trong kỳ 170 → 210.

Tổng hai tuần không đổi (350 ha).

| Bước | Thời điểm | Tình huống | KHTC W51 |
|---|---|---|---|
| s4 | 23/12 15:00 | KHTC đã đóng W49 (3.190) và W50 (3.400). Nguồn W51 ghi lũy kế 3.350, giảm 50 ha vì xã khai trùng. | Lũy kế 3.350, gốc 3.400, trong kỳ **−50**. Số âm giữ nguyên, Mẫu 1 in "giảm 50 ha (điều chỉnh)". |

### G04: nguồn đổi sau khi kỳ KHTC đã đóng (CNTS)

Chỉ tiêu như G01. KHTC đã đóng và xuất Mẫu 1 tuần 45 (lũy kế 4.947,5) và tuần 46 (lũy kế 5.079,0). Sau đó CNTS mở lại kỳ 2026-W45, sửa lũy kế 4.947,5 → 4.940,0, và tự tính lại kỳ 2026-W46 thành 5.071,5. Kỳ 2026-W47 (17/11–23/11) có lũy kế 5.210,0.

| Tuần KHTC | Lũy kế | Gốc | Trong kỳ | Lũy kế năm | Trạng thái bản đã xuất |
|---|---|---|---|---|---|
| 2026-W45 (đã đóng) | 4.947,5 (giữ) | 4.812,0 | 135,5 | 4.947,5 | **Nguồn đã thay đổi**, không phát hành lại |
| 2026-W46 (đã đóng) | 5.079,0 (giữ) | 4.947,5 | 131,5 | 5.079,0 | **Nguồn đã thay đổi**, không phát hành lại |
| 2026-W47 | 5.210,0 | **5.079,0** (W46 đã đóng) | **131,0** | 5.210,0 = lũy kế nguồn | Đã chốt |

Nếu tính gốc theo số mới của nguồn (5.071,5) thì trong kỳ W47 sẽ là 138,5. Khi đó tổng các tuần KHTC lệch −7,5 tấn so với lũy kế nguồn. Dùng gốc là số đã đóng thì phần sửa đi vào tuần đang mở, và lũy kế KHTC luôn bằng lũy kế nguồn.

### G05: cuối năm (CNTS), lũy kế về 0 ở năm mới

| Kỳ CNTS | Ngày | Lũy kế nguồn | Chuỗi | Tuần KHTC | Gốc | Trong kỳ | Lũy kế năm KHTC |
|---|---|---|---|---|---|---|---|
| 2026-W51 | 15/12–21/12 | 5.890,0 | 2026 | 2026-W51 (đã đóng) | — | — | — |
| 2026-W52 | 22/12–28/12 | 6.020,0 | 2026 | 2026-W52 | 5.890,0 | 130,0 | 6.020,0 |
| 2027-W01 | 29/12/2026–04/01/2027 | 118,0 | **2027** | 2027-W01 | **0** (chuỗi mới) | 118,0 | 118,0 |
| 2027-W02 | 05/01–11/01/2027 | 240,5 | 2027 | 2027-W02 | 118,0 | 122,5 | 240,5 |

- Năm KHTC 2026 có **52** tuần, số liệu đến 29/12/2026. Mẫu 4 năm 2026 = 6.020,0 tấn.
- Ngày 30/12 và 31/12/2026 thuộc năm KHTC 2027.

### G05B: cuối năm, tuần ISO 53 (Trồng trọt đợt cũ v1)

Đợt Thu Đông phát hành trước 04/11/2026 nên giữ quy tắc v1: tuần thứ Hai – Chủ nhật, đánh số ISO. Chỉ tiêu: diện tích thu hoạch lúa Thu Đông, lũy kế vụ, đơn vị ha.

| Kỳ v1 | Ngày | Lũy kế vụ | Tuần KHTC | Gốc | Trong kỳ | Lũy kế năm KHTC |
|---|---|---|---|---|---|---|
| 2026-W51 | 14/12–20/12 | 5.600 | 2026-W51 (đã đóng) | — | — | — |
| 2026-W52 | 21/12–27/12 | 5.800 | 2026-W52 | 5.600 | 200 | — |
| **2026-W53** | 28/12/2026–03/01/2027 | 6.150 | **2027-W01** | 5.800 | 350 | 350 |
| 2027-W01 | 04/01–10/01/2027 | 6.400 | 2027-W02 | 6.150 | 250 | 600 |

- Không có tuần KHTC 53/2026: tuần ISO 53 rơi vào tuần KHTC 1/2027.
- Chuỗi lũy kế vụ không về 0 ở ranh giới năm. Mẫu 1 in lũy kế vụ 6.150 / 6.400. Phần năm KHTC 2027 tính bằng Σ trong kỳ (350, 600).

### G05C: năm có 53 tuần (chỉ quy đổi)

| Kỳ nguồn | Ngày | Tuần KHTC |
|---|---|---|
| CNTS 2029-W52 | 18/12–24/12/2029 | 2029-W52 (19/12–25/12/2029) |
| CNTS **2029-W53** | 25/12–31/12/2029 | **2030-W01** (26/12/2029–01/01/2030) |
| CNTS 2030-W52 | 24/12–30/12/2030 | **2030-W53** (25/12–31/12/2030) |
| CNTS 2031-W01 | 31/12/2030–06/01/2031 | 2031-W01 (01/01–07/01/2031) |
| Trồng trọt 2030-W53 | 25/12–31/12/2030 | 2030-W53 (trùng) |

- KHTC 2030 có 53 tuần; CNTS 2029 có 53 tuần; năm 2026 theo ISO (v1) có 53 tuần.
- Năm ghi trên nhãn tuần nguồn **có thể khác** năm của tuần KHTC nhận nó. Ví dụ: CNTS 2029-W53 được tính vào năm KHTC 2030 (xem điểm mở M3).

### G06: HTX "Chưa xác nhận" trước và sau khi xã nhận

Số liệu tính tại ngày chốt (Thứ Ba, cuối ngày). HTX05 và HTX06 do Trung tâm KN tạo.

| HTX | Thành viên | Ngày chốt W49 (08/12) | Ngày chốt W50 (15/12) |
|---|---|---|---|
| HTX01–HTX04 | 80 + 75 + 85 + 60 = 300 | Đã xác nhận | Đã xác nhận |
| HTX05 | 70 | Chưa xác nhận | **Đã xác nhận** (xã nhận ngày 10/12) |
| HTX06 | 50 | Chưa xác nhận | Chưa xác nhận |

| Tuần KHTC | Số HTX: tổng = đã xác nhận + chưa xác nhận | Thành viên: tổng = đã + chưa | Ghi chú |
|---|---|---|---|
| 2026-W49 (đã gửi) | 6 = 4 + 2 | 420 = 300 + 120 | Không phát hành lại sau khi xã nhận (Q10) |
| 2026-W50 | 6 = **5 + 1** | 420 = **370 + 50** | Tổng không đổi, chỉ nhãn đổi |

## 4. M1–M7 đã chốt với PO trong chat G1 (15/10/2026)

PO đã xác nhận **chốt cả M1–M7 theo giá trị hiện hành**. Đây là quyết định sản phẩm đã ghi trong chat, không thay cho chữ ký nghiệm thu thực tế của đơn vị. Cột phương án khác chỉ giữ làm lịch sử, không còn là lựa chọn đang chờ.

| # | Điểm | Giá trị đã chốt trong tài liệu và golden | Phương án khác (lịch sử) |
|---|---|---|---|
| M1 | Tuần KHTC không có kỳ nguồn (G02) được coi là "đã chốt" | Có. Tập rỗng thì mọi kỳ đều đã chốt. Mẫu 1 in "không phát sinh". | Coi là "chưa chốt": KHTC phải đóng kỳ theo ngoại lệ BR-DL-15 |
| M2 | Gốc của tuần K khi tuần K−1 đã đóng (G04) | Dùng số đã đóng của K−1. Phần sửa đi vào tuần đang mở, lũy kế KHTC = lũy kế nguồn. | Dùng số mới của nguồn: lũy kế năm KHTC sẽ lệch số đã sửa |
| M3 | Chuỗi lũy kế nguồn không trùng năm KHTC (G05B: vụ vắt qua năm; G05C: CNTS 2029-W53 vào KHTC 2030) | Lũy kế hiển thị là lũy kế của nguồn. Lũy kế năm KHTC = Σ trong kỳ của các tuần KHTC trong năm. | Mẫu 1 A.I.3 "lũy kế từ đầu năm" lấy theo năm của nguồn (CNTS) |
| M4 | Hai cột ngày của Mẫu 1 A.III (G02B) | Hai ngày có bảng giá đã duyệt gần nhất, không sau ngày chốt | Hai ngày báo giá theo lịch gần nhất; ô của ngày chưa duyệt lấy giá ngày đã duyệt gần nhất và ghi chú thích |
| M5 | Ai bấm "Cập nhật số liệu" (Q11) và có phát hành lại không (Q10) | Chỉ cán bộ KHTC phụ trách bấm, lưu lịch sử. Không phát hành lại. | Mọi cán bộ KHTC; tự phát hành lại khi tuần đã gửi bị đổi |
| M6 | Thời điểm tính trạng thái HTX | Trạng thái lúc cuối ngày chốt (Thứ Ba) | Trạng thái lúc KHTC bấm "Cập nhật số liệu" |
| M7 | Hạn nộp phiếu tuần của KHTC | 10:00 Thứ Tư kế tiếp (BRD Mục II.4). W11.62 phải đặt riêng hạn này cho kỳ KHTC, vì hạn mặc định v2 của W11.03 cho kỳ tuần là 12:00. | — |

## 5. Ký xác nhận

| Vai trò | Họ tên | Ý kiến (đồng ý / sửa ca nào) | Ngày ký |
|---|---|---|---|
| PO (chủ sản phẩm) | | | |
| Trưởng phòng Kế hoạch – Tài chính | | | |
| Cán bộ KHTC phụ trách báo cáo tuần | | | |
| Đại diện Phòng Trồng trọt | | | |
| Đại diện Phòng Chăn nuôi – Thủy sản | | | |
| Đại diện Phòng Thông tin – Truyền thông (Báo giá) | | | |
