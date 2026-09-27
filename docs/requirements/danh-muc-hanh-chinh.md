# Danh mục đơn vị hành chính ĐBSCL + Tây Ninh (28/09/2026)

Tài liệu mô tả dữ liệu nền đơn vị hành chính của pf/masterdata
([admin_units_dbscl.json](../../internal/platform/masterdata/seeddata/admin_units_dbscl.json), định dạng
`agrigreen.admin_units/v1`). Bộ seed thay bộ minh hoạ cũ (mã tự đặt kiểu `92-X-NINH-KIEU`) bằng mã chính thức.

- Phạm vi: 6 tỉnh/thành sau sắp xếp 2025, gồm 14 tỉnh/thành cũ.
- Mỗi xã/phường/đặc khu mới có mã 5 chữ số, tên và loại hình chính thức.
- Mỗi đơn vị cũ (tỉnh, huyện, xã trước 01/07/2025) là đơn vị đã đóng hiệu lực (`valid_to = 2025-07-01`), có liên kết kế thừa (`successors`).

Bảng tiền thân đầy đủ, mỗi dòng một cặp xã mới ← xã cũ, kèm ghi chú "nhập toàn bộ / nhập một phần / giữ nguyên":
[danh-muc-hanh-chinh-dbscl-2025.csv](danh-muc-hanh-chinh-dbscl-2025.csv) (1.723 dòng).

## 1. Nguồn

Truy xuất ngày **28/09/2026**. Nội dung trang web chỉ được dùng làm dữ liệu.

| # | Nguồn | Dùng cho | Địa chỉ |
|---|---|---|---|
| N1 | Cục Thống kê (Bộ Tài chính), dịch vụ web danh mục ĐVHC `DMDVHC.asmx`, phương thức `DanhMucPhuongXa(DenNgay, Tinh, …)` | Mã, tên, loại hình xã **mới** (`DenNgay = 01/07/2025`). Mã, tên xã **cũ**, mã và tên huyện cũ, tỉnh cũ (`DenNgay = 30/06/2025`) | https://danhmuchanhchinh.nso.gov.vn/DMDVHC.asmx (hướng dẫn: https://danhmuchanhchinh.nso.gov.vn/WebService.aspx) |
| N2 | Cục Thống kê, "Bảng chuyển đổi ĐVHC mới – cũ", sheet "Tổng hợp_không merge" (tệp sửa lần cuối 23/10/2025, 1.146.952 byte) | Quan hệ xã cũ → xã mới, ghi chú nhập toàn bộ hoặc một phần | https://danhmuchanhchinh.nso.gov.vn/TAPTIN/BangChuyendoiĐVHCmoi_cu_final.xlsx |
| N3 | Quyết định 19/2025/QĐ-TTg ngày 30/06/2025 ban hành Bảng danh mục và mã số các ĐVHC Việt Nam, hiệu lực 01/07/2025, thay QĐ 124/2004/QĐ-TTg | Cơ sở pháp lý của mã tỉnh và mã xã mới. N1 là bản điện tử của danh mục này | https://vanban.chinhphu.vn/?docid=214409&pageid=27160 |
| N4 | Nghị quyết 202/2025/QH15 ngày 12/06/2025 về sắp xếp ĐVHC cấp tỉnh | Hợp nhất 14 tỉnh cũ thành 6 tỉnh mới | https://vanban.chinhphu.vn/?pageid=27160&docid=213930 |
| N5 | Các Nghị quyết của UBTVQH ngày 16/06/2025 về sắp xếp ĐVHC cấp xã | Đối chiếu số lượng, loại hình, danh sách đơn vị không sắp xếp, một số khoản mẫu | Bảng dưới |

| Tỉnh mới | Nghị quyết cấp xã | Toàn văn |
|---|---|---|
| An Giang | 1654/NQ-UBTVQH15 | https://xaydungchinhsach.chinhphu.vn/toan-van-nghi-quyet-so-1654-nq-ubtvqh15-sap-xep-cac-dvhc-cap-xa-cua-tinh-an-giang-nam-2025-119250616191813652.htm |
| Cà Mau | 1655/NQ-UBTVQH15 | https://xaydungchinhsach.chinhphu.vn/toan-van-nghi-quyet-so-1655-nq-ubtvqh15-sap-xep-cac-dvhc-cap-xa-cua-tinh-ca-mau-nam-2025-11925061619471695.htm |
| Đồng Tháp | 1663/NQ-UBTVQH15 | https://xaydungchinhsach.chinhphu.vn/toan-van-nghi-quyet-so-1663-nq-ubtvqh15-sap-xep-cac-dvhc-cap-xa-cua-tinh-dong-thap-nam-2025-119250616203652359.htm |
| Cần Thơ | 1668/NQ-UBTVQH15 | https://xaydungchinhsach.chinhphu.vn/toan-van-nghi-quyet-so-1668-nq-ubtvqh15-sap-xep-cac-dvhc-cap-xa-cua-thanh-pho-can-tho-nam-2025-119250616191340674.htm |
| Tây Ninh | 1682/NQ-UBTVQH15 | https://xaydungchinhsach.chinhphu.vn/toan-van-nghi-quyet-so-1682-nq-ubtvqh15-sap-xep-cac-dvhc-cap-xa-cua-tinh-tay-ninh-nam-2025-119250616210257367.htm |
| Vĩnh Long | 1687/NQ-UBTVQH15 | https://xaydungchinhsach.chinhphu.vn/toan-van-nghi-quyet-so-1687-nq-ubtvqh15-sap-xep-cac-dvhc-cap-xa-cua-tinh-vinh-long-nam-2025-11925061621323394.htm |

Cách dựng:

- N1 cho danh sách, mã, tên.
- N2 cho quan hệ kế thừa.
- N3–N5 để đối chiếu.
- Hai nguồn N1 và N2 khớp nhau về **tập mã**: 591 mã mới và 1.664 mã cũ, không thiếu, không thừa.

## 2. Số lượng

### 2.1 Tỉnh mới (từ 01/07/2025)

| Mã | Tỉnh/thành mới | Hợp nhất từ (mã cũ) | Tổng cấp xã | Xã | Phường | Đặc khu | Không sắp xếp (giữ nguyên) | Khớp NQ |
|---|---|---|---|---|---|---|---|---|
| 91 | An Giang | An Giang (89), Kiên Giang (91) | **102** | 85 | 14 | 3 | 6: Mỹ Hòa Hưng, Bình Giang, Bình Sơn, Hòn Nghệ, Sơn Hải, Tiên Hải | 1654 ✓ |
| 92 | Cần Thơ | Cần Thơ (92), Sóc Trăng (94), Hậu Giang (93) | **103** | 72 | 31 | 0 | 8: P. Tân Lộc; xã Trường Long, Thạnh Phú, Thới Hưng, Phong Nẫm, Mỹ Phước, Lai Hòa, Vĩnh Hải | 1668 ✓ |
| 82 | Đồng Tháp | Đồng Tháp (87), Tiền Giang (82) | **102** | 82 | 20 | 0 | 0 | 1663 ✓ |
| 86 | Vĩnh Long | Vĩnh Long (86), Bến Tre (83), Trà Vinh (84) | **124** | 105 | 19 | 0 | 4: Hòa Minh, Long Hòa, Long Vĩnh, Đông Hải | 1687 ✓ |
| 96 | Cà Mau | Cà Mau (96), Bạc Liêu (95) | **64** | 55 | 9 | 0 | 1: Hồ Thị Kỷ | 1655 ✓ |
| 80 | Tây Ninh | Tây Ninh (72), Long An (80) | **96** | 82 | 14 | 0 | 0 | 1682 ✓ |
| | **Cộng** | 14 tỉnh cũ | **591** | 481 | 107 | 3 | 19 | |

Xã Thổ Châu (Kiên Giang cũ) được sắp xếp toàn bộ thành Đặc khu Thổ Châu, mã 31105. N2 ghi riêng trường hợp này.

### 2.2 Tỉnh cũ (đến hết 30/06/2025)

| Mã cũ | Tỉnh/thành cũ | → Tỉnh mới | Huyện/quận/thị xã/TP | Xã/phường/thị trấn |
|---|---|---|---|---|
| 89 | An Giang | 91 | 11 | 155 |
| 91 | Kiên Giang | 91 | 15 | 143 |
| 87 | Đồng Tháp | 82 | 12 | 141 |
| 82 | Tiền Giang | 82 | 11 | 164 |
| 86 | Vĩnh Long | 86 | 8 | 102 |
| 83 | Bến Tre | 86 | 9 | 148 |
| 84 | Trà Vinh | 86 | 9 | 104 |
| 92 | Cần Thơ | 92 | 9 | 80 |
| 94 | Sóc Trăng | 92 | 11 | 108 |
| 93 | Hậu Giang | 92 | 8 | 75 |
| 96 | Cà Mau | 96 | 9 | 100 |
| 95 | Bạc Liêu | 96 | 7 | 64 |
| 72 | Tây Ninh | 80 | 9 | 94 |
| 80 | Long An | 80 | 15 | 186 |
| | **Cộng** | | **143** | **1.664** |

Tổng số dòng của tệp seed là 2.418: 14 tỉnh cũ, 6 tỉnh mới, 143 huyện, 1.664 xã cũ, 591 xã mới.

Kiểm thử giữ các số trên:

- `TestSeedCountsMatchDoc` ([gazetteer_seed_test.go](../../internal/platform/masterdata/domain/gazetteer_seed_test.go)).
- `TestDescendantsCoverWholeProvince` ([http_integration_test.go](../../internal/platform/masterdata/handler/http_integration_test.go)).

Sửa bảng này thì phải sửa cả hai kiểm thử.

## 3. Ánh xạ vào mô hình

| Đơn vị | `code` | `name` | `parent_code` | Hiệu lực | `successors` |
|---|---|---|---|---|---|
| Tỉnh cũ | 2 chữ số, QĐ 124/2004 | Tên gọn, như bộ cũ (vd "Kiên Giang") | — | 2004-01-01 → 2025-07-01 | Mã tỉnh mới (N4) |
| Tỉnh mới | 2 chữ số, QĐ 19/2025 | Tên gọn | — | từ 2025-07-01 | — |
| Huyện cũ | 3 chữ số (N1) | Tên đủ loại hình, vd "Quận Ninh Kiều" | Tỉnh cũ | 2004-01-01 → 2025-07-01 | Hợp các xã mới nhận địa bàn của các xã trong huyện (1–9 mã) |
| Xã cũ | 5 chữ số (N1) | Tên đủ loại hình (Xã/Phường/Thị trấn) | Huyện cũ | 2004-01-01 → 2025-07-01 | Xã mới theo N2. "Nhập một phần" cho nhiều mã: 1.610 xã có 1 kế thừa, 49 xã có 2, 5 xã có 3 |
| Xã mới | 5 chữ số (N1/N3) | Tên đủ loại hình (Xã/Phường/Đặc khu) | Tỉnh mới | từ 2025-07-01 | — |

Lưu ý:

- **Mã dùng lại.** Cả 591 mã xã mới đều trùng mã của một xã cũ tiền thân, theo nguyên tắc của QĐ 19/2025. Ví dụ 31135 là Phường Tân An (quận Ninh Kiều) đến 30/06/2025, và là Phường Ninh Kiều từ 01/07/2025.
  - Mô hình phân biệt hai phiên bản bằng `(code, valid_from)`, giống mã tỉnh 91 (Kiên Giang → An Giang).
  - Vì vậy `Current(code, at ≥ 01/07/2025)` trả chính đơn vị mới cùng mã.
  - Muốn biết tập kế thừa đầy đủ của đơn vị cũ (kể cả phần "nhập một phần" sang xã khác), đọc phiên bản cũ: `Get(code, at < 01/07/2025).successor_codes`.
  - Ví dụ 29770 và 29771 (thị xã Bình Minh) có kế thừa `[29770, 29771]`.
- **`valid_from = 2004-01-01` là mốc dữ liệu** (QĐ 124/2004/QĐ-TTg), không phải ngày thành lập. Danh mục cũ là ảnh chụp ngày 30/06/2025.
  - Các lần điều chỉnh cấp xã 2004–2025 không được dựng lại, ví dụ đợt 2023–2025 theo NQ 1203/NQ-UBTVQH15 của Vĩnh Long.
  - Tra một ngày trước 30/06/2025 sẽ ra danh mục ngày 30/06/2025.
- **Không lưu tỷ lệ "nhập một phần".** Mô hình chỉ có danh sách kế thừa. Ghi chú nằm trong tệp CSV.
- **Không có thôn/ấp.** N1 có trang thôn ấp, nhưng mô hình không có cấp này.

## 4. Khác biệt giữa các nguồn và cách xử lý

| # | Khác biệt | Xử lý |
|---|---|---|
| D1 | N2 ghi sai **tỉnh cũ** ở 2 dòng: 27691 Phường 4 (TP Tân An, huyện 794) ghi "Tỉnh Tiền Giang (82)", đúng là Long An (80). 30781 Xã Tiên Hải (TP Hà Tiên, huyện 900) ghi "Tỉnh An Giang (89)", đúng là Kiên Giang (91) | Lấy tỉnh theo N1 và theo mã huyện. Vì vậy N2 đếm Tiền Giang 165, Long An 185, An Giang 156, Kiên Giang 143 − 1 = 142, còn seed là 164 / 186 / 155 / 143 |
| D2 | Tên xã cũ khác cách viết ở 38 mã. Phần lớn chỉ khác vị trí dấu thanh ("Hòa"/"Hoà", "Thủy"/"Thuỷ", "Hỏa"/"Hoả"). Có vài khác biệt chữ: 25462 "Phường IV"/"Phường 4"; 28807 "Quới Sơn"/"Qưới Sơn"; 29218 "An Qui"/"An Quy"; 29431 "Vinh Kim"/"Vĩnh Kim"; 29857 "Lục Sĩ Thành"/"Lục Sỹ Thành"; 30055 "Mỹ Quí"/"Mỹ Quý"; 30328 "Vĩnh Nguơn"/"Vĩnh Ngươn"; 30475 "Ô Long Vĩ"/"Ô Long Vỹ"; 31231 "Thị trấn Thạnh An"/"Thị trấn Thanh An"; 31729 "Hòa Tú 2"/"Hòa Tú II"; 31843 "Thi trấn"/"Thị trấn" (N2 viết trước, N1 viết sau) | Lấy **nguyên văn N1** (danh mục điện tử của cơ quan quản lý mã). Riêng 31231 (Thạnh An hay Thanh An) chưa có văn bản gốc để đối chiếu, giữ theo N1 và **cần xác minh** |
| D3 | Tên xã mới: 27823 N2 ghi "Xã Mộc Hoá", N1 ghi "Xã Mộc Hóa" | Lấy N1 |
| D4 | N1 viết hoa sai loại hình ở 2 mã: 30688 "Thị Trấn Óc Eo", 32191 "Thị Trấn Năm Căn". Tên huyện ở N2 có dạng "Thị Xã …" | Chuẩn hóa tiền tố loại hình thành "Thị trấn", "Thị xã". Không đổi phần tên riêng |
| D5 | N1 tra theo mã tỉnh ngày 30/06/2025 trả dữ liệu không nhất quán. Mã 80 trả cả Long An và Tây Ninh cũ. Mã 72, 87, 89 trả rỗng; xã của Tây Ninh, Đồng Tháp, An Giang cũ có ở kết quả của mã 80, 82, 91. Mã 83, 84, 93, 94, 95 trả đúng từng tỉnh | Gộp kết quả theo mã 80, 82, 86, 91, 92, 96, 83, 84, 93, 94, 95. Không có mã xã trùng giữa các lần tra. Tỉnh cũ xác định theo trường `TenTinh` |
| D6 | Bộ minh hoạ cũ ghi Kiên Giang (91) → An Giang (91) và Long An (80) → Tây Ninh (80). Bộ này đã khớp N3/N4 | Giữ nguyên mã tỉnh. Không có thay đổi mã tỉnh |

Đối chiếu văn bản gốc (N5), đã kiểm từng tỉnh:

- **Số lượng và loại hình** ở khoản cuối Điều 1 khớp mục 2.1 ở cả 6 NQ.
- **Danh sách đơn vị không sắp xếp** khớp ở An Giang, Cần Thơ, Vĩnh Long, Cà Mau. NQ của Đồng Tháp và Tây Ninh không nêu đơn vị nào.
- **Các khoản mẫu** khớp N2 từng xã tiền thân:
  - Phường Ninh Kiều = Tân An + Thới Bình + Xuân Khánh.
  - Phường Cái Khế = An Hòa + Cái Khế + một phần Bùi Hữu Nghĩa (NQ 1668).
  - Phường Mỹ Tho = Phường 1 + Phường 2 + Tân Long (NQ 1663).
  - Phường Tân An (Tây Ninh) = Phường 7 + Bình Tâm + Nhơn Thạnh Trung + An Vĩnh Ngãi (NQ 1682).
  - Xã Cái Nhum = An Phước + Chánh An + TT Cái Nhum; xã Tân Long Hội = Tân An Hội + Tân Long + Tân Long Hội (NQ 1687).

Chưa đối chiếu toàn văn từng khoản của 6 NQ với N2, tức 591 khoản. N2 là bảng do Cục Thống kê lập từ chính các NQ này.

## 5. Độ đầy đủ

| Tỉnh mới | Xã mới | Xã cũ có kế thừa | Tình trạng |
|---|---|---|---|
| An Giang (91) | 102/102 | 298/298 | Đủ |
| Cần Thơ (92) | 103/103 | 263/263 | Đủ |
| Đồng Tháp (82) | 102/102 | 305/305 | Đủ |
| Vĩnh Long (86) | 124/124 | 354/354 | Đủ |
| Cà Mau (96) | 64/64 | 164/164 | Đủ |
| Tây Ninh (80) | 96/96 | 280/280 | Đủ |

Không có mã tự đặt trong bộ seed. Việc còn mở:

- Xác minh tên 31231 (D2).
- Dựng lịch sử cấp xã trước 30/06/2025 nếu nghiệp vụ cần tra đúng địa giới tại các ngày cũ.

## 6. Cập nhật

- Danh mục thay đổi sau 01/07/2025 (chia tách, đổi tên) nạp qua Admin API `POST /v1/masterdata/admin-units/import`, cùng định dạng: đóng phiên bản đang mở, mở phiên bản mới.
- Không sửa tệp seed cho các thay đổi đó. Tệp seed chỉ phản ánh danh mục ngày 30/06/2025 và 01/07/2025.
- Muốn dựng lại tệp seed từ nguồn thì tra lại N1 với `DenNgay` 30/06/2025 và 01/07/2025, tải N2, rồi áp các quy tắc ở mục 3 và 4.
