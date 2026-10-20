# Chuyển đổi dữ liệu nhân sự HCTH từ Excel (`cmd/hrmimport`, W11.30)

BRD HCTH v3.0 không có chức năng "nạp dữ liệu nhân sự ban đầu từ Excel trên giao diện": Mekong Green chuyển đổi một lần
trước khi vận hành, Phòng HCTH cung cấp tệp và xác nhận các dòng lỗi (giả định A2; D3 H10). Lệnh `cmd/hrmimport` làm việc
đó: đọc tệp qua `shared/tabular`, chuẩn hóa, báo cáo theo dòng, rồi ghi **qua Application API `/v1/hrm/staff`** (lớp app
của `pf/hrm`: quyền FN-20, danh mục, bảng hệ số, FN-08, sổ chỉ ghi thêm, dữ liệu cá nhân vào kho định danh). Công cụ không
nối DB, không chạm kho định danh trực tiếp. Giả định chi tiết: [W11.30](../kn/gia-dinh/W11.30.md).

## 1. Chuẩn bị (trước khi nhận tệp)

1. Danh mục nhân sự (FN-19) và **bảng hệ số** của mọi ngạch có trong tệp đã khai báo (Admin → Danh mục nhân sự). Bảng hệ
   số mẫu (NĐ 204/2004, Q-070) chỉ để thử — hệ số trong tệp phải khớp bảng, nếu không dòng bị báo lỗi.
2. Đơn vị nội bộ (W11.07) đã khai báo; hồ sơ nhân sự đang bật (`AG_HRM_RECORDS=1`, Q-005).
3. Tài khoản chạy lệnh: Cấp Tỉnh – HCTH hoặc Admin, có `profile.edit` phạm vi toàn Trung tâm và `sensitive.view` (thẻ
   Thống kê BC Sở). Mọi hồ sơ tạo ra ghi người tạo là tài khoản này.
4. Gửi HCTH tệp mẫu:

   ```powershell
   go run ./cmd/hrmimport -template mau-nhan-su.xlsx
   ```

   Sheet đầu `Nhan su` (định dạng Văn bản — Excel không bỏ số 0 đầu SĐT, không đổi bậc "3/9" thành ngày), sheet
   `Huong dan` (cột, khóa ánh xạ, bắt buộc, định dạng), sheet `Vi du` (hai dòng tổng hợp).

## 2. Ánh xạ cột khi HCTH gửi tệp gốc (Q-069)

Nếu HCTH gửi tệp cũ (Thống kê CCVCNLĐ, Biểu mẫu nhân sự, Theo dõi nâng bậc) thay vì tệp mẫu:

1. Mỗi lần nạp một **sheet** (công cụ đọc sheet đầu tiên): đưa sheet cần nạp lên đầu, xóa dòng tiêu đề biểu/dòng trống phía
   trên sao cho **dòng 1 là tiêu đề cột**; tiêu đề gộp nhiều tầng (vd "Năm sinh" trên "Nam | Nữ") phải làm phẳng thành một
   dòng ("Năm sinh Nam", "Năm sinh Nữ"); ô công thức phải dán giá trị (Paste Values) — tệp có công thức bị từ chối cả tệp.
2. `go run ./cmd/hrmimport -dump-mapping anh-xa.yaml`, sửa `columns` (thêm tiêu đề cột thật), `values` (tên phòng, chức
   vụ, ngạch viết khác danh mục → mã), `defaults` (`type: hop_dong` cho sheet HĐLĐ-ĐVSN; `position: vien_chuc` nếu tệp để
   trống chức vụ; `as_of`: ngày chốt của tệp, vd `2026-08-31`).
3. Lưu `anh-xa.yaml` cùng tệp nguồn trong hồ sơ dự án (không đưa vào repo — tệp nguồn có dữ liệu cá nhân).

Tên/mã trùng danh mục và tên/mã đơn vị nội bộ tự khớp (không phân biệt hoa thường, dấu, dấu câu; "Phòng HC-TH" = mã
`hcth`; "Trồng trọt" = "Phòng Trồng trọt").

## 3. Chạy thử — không ghi

```powershell
$env:AG_HRM_IMPORT_API = "https://<api>/v1"
$env:AG_HRM_IMPORT_TOKEN = "<access token của tài khoản HCTH/Admin>"   # không truyền qua cờ dòng lệnh
$env:AG_HRM_IMPORT_CONTEXT = "<context_id của POST /v1/me/context>"    # vai Cấp Tỉnh – HCTH hoặc Admin
go run ./cmd/hrmimport -in nhan-su.xlsx -mapping anh-xa.yaml -as-of 2026-08-31 -report bao-cao-chay-thu.xlsx
```

Chạy thử chỉ đọc máy chủ (`/hrm/access`, danh mục, đơn vị nội bộ, bảng hệ số). Báo cáo (`.xlsx` hoặc `.csv`) có cột
Dòng, Mã cũ, Trạng thái, Mã hồ sơ, Mức, Cột, Nội dung — **không chứa giá trị của trường dữ liệu cá nhân** (họ tên, SĐT,
ngày sinh, email, địa chỉ, thẻ TK). Mã thoát: 0 không lỗi, 2 còn dòng lỗi, 1 lỗi cả tệp/máy chủ.

| Chuẩn hóa tự động (D3 H10) | Báo trong báo cáo |
|---|---|
| Ngày dd/mm/yyyy, d-m-yyyy, d.m.yyyy, mm/yyyy, yyyy, ô định dạng ngày, số ngày Excel (≥ 5 chữ số) | Cảnh báo khi đọc từ số ngày Excel |
| Bậc "3", "Bậc 3", "3/9", hoặc bị Excel đổi thành ngày (03/09 hay 09/03 → bậc 3/9) | Cảnh báo khi đọc từ ngày; lệch số bậc của ngạch |
| Hệ số dạng chữ "2,67", "HS 2,67"; thiếu bậc → suy bậc từ hệ số | Lỗi khi hệ số không khớp bảng hệ số |
| Ghi chú/số trong họ tên ("… (19 VC)") bị bỏ (BR-05) | Cảnh báo |
| SĐT mất số 0 đầu (lưu dạng số), +84 | Cảnh báo khi thêm số 0 |
| Năm sinh ở cột Nam/Nữ → giới tính | — |
| Trùng SĐT/email trong tệp | Lỗi; trùng họ tên + ngày sinh: cảnh báo |
| Thiếu ngày vào làm | Cảnh báo — hồ sơ "Chưa vào làm", không vào NS01 |
| Thiếu ngày xếp bậc | Cảnh báo — tạm dùng ngày chốt (Q-069) |

Gửi báo cáo cho HCTH; HCTH sửa tệp nguồn (không sửa báo cáo) và gửi lại; lặp đến khi không còn dòng **Lỗi** và HCTH xác
nhận các **Cảnh báo** (A2). Lưu biên bản xác nhận cùng báo cáo cuối.

## 4. Ghi

```powershell
go run ./cmd/hrmimport -in nhan-su.xlsx -mapping anh-xa.yaml -as-of 2026-08-31 -report bao-cao-ghi.xlsx -commit
```

- Còn dòng lỗi thì lệnh **không ghi dòng nào**. Muốn ghi các dòng hợp lệ trước (HCTH đồng ý), thêm `-skip-errors`.
- Ghi tuần tự theo thứ tự dòng — mã hồ sơ (VC-/HD-/XA-) cấp theo thứ tự tệp. Mỗi dòng một `POST /v1/hrm/staff` với
  `Idempotency-Key` = băm tệp + số dòng (không chứa dữ liệu cá nhân).
- Báo cáo ghi: `Đã tạo` + mã hồ sơ; `Đã có` (SĐT đã thuộc một hồ sơ — lần chạy trước hoặc nhập tay — không ghi lại);
  `Lỗi` + mã lỗi máy chủ (`problem.*`, không chứa dữ liệu cá nhân); `Chưa ghi` (dừng do mất kết nối).
- **Chạy lại an toàn**: sau khi sửa dòng lỗi, chạy lại cùng lệnh với tệp đã sửa — dòng đã ghi báo `Đã có`, dòng mới được tạo.

## 5. Sau khi ghi

1. HCTH đối chiếu số người trên màn Hồ sơ nhân sự và NS01 tại ngày chốt với tệp gốc.
2. Dòng `Đã có` mà HCTH không nhận ra (SĐT trùng người khác, hoặc chủ thể kho định danh còn lại sau một lần ghi hỏng):
   tra theo SĐT trên màn Hồ sơ nhân sự, xử lý tay.
3. Sửa sau chuyển đổi làm trên màn hồ sơ (sổ ghi lịch sử sửa). Không xóa hồ sơ (BR-01) — nhầm loại/người thì ghi nghỉ việc
   hoặc sửa theo FN-07/FN-08.
4. Xóa tệp nguồn, báo cáo, token khỏi máy chạy lệnh sau khi HCTH xác nhận; lưu báo cáo cuối theo quy định hồ sơ dự án.

## 6. Kiểm thử

`go test -count=1 -p 1 -v -run 'TestHRMImport_W1130|TestNormalize_W1130|TestTemplate_W1130' ./cmd/hrmimport/` (cần
`AG_TEST_PG_ADMIN_URL`). Tệp mẫu tổng hợp `cmd/hrmimport/testdata/nhan-su-tong-hop.xlsx` và báo cáo vàng
`bao-cao-chay-thu.golden.csv`; ghi lại bằng `-update`.
