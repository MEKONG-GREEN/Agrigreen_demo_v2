## KNSPLIT-2-KN-RECORDS-HTX-READ — Màn Máy móc/Thửa đọc HTX theo hồ sơ KN; chủ dữ liệu khai báo máy, thửa (#162)

- **Nguồn:** #162 (con của #67 KNSPLIT-2-KN-RECORDS). Làn kn (Claude). Lát này chỉ làm **mục 1**; mục 2–3 chờ PO chốt (dưới đây).

### Mục 1 — đã làm: bộ chọn HTX đọc hồ sơ KN riêng

- **Trước:** màn Máy móc (`MachinesScreen`) và Thêm/Sửa thửa (`PlotScreens`) lấy danh sách HTX qua GET legacy `/v1/kn/cooperatives` (`limit` 200, một trang, đọc thẳng pf/party).
- **Sau:** ô chọn HTX (`kn/cycle/screens/HtxRecordPicker.tsx`) đọc **hồ sơ KN loại HTX** `GET /v1/kn/producer-records?kind=cooperative&status=active` (#67):
  - Phân trang **của máy chủ**: mỗi lượt `limit` 50; nút "Tải thêm HTX" gửi `cursor` = `next_cursor` của lượt trước tới khi `next_cursor` null. Ô "Tìm HTX theo tên" gửi `q` lên máy chủ (chờ 250 ms sau khi gõ). Không cắt trang đầu ở client (bài học #67).
  - Phạm vi: máy chủ lọc theo ngữ cảnh phiên KN; web chỉ gửi `commune` đang chọn (Máy móc: bộ lọc Xã/phường; Thửa: xã của thửa).
  - Giá trị chọn là `htx_ref` của hồ sơ (HTX đã công bố) vì `PUT /v1/kn/machine-counts/{ref}` và `/v1/kn/plots` (`cooperative_ref`) vẫn tham chiếu HTX nền tảng (mục 2–3). Hồ sơ chưa liên kết HTX đã công bố hiện trong danh sách nhưng **không chọn được** ("— chưa liên kết HTX đã công bố").
  - Chỉ hiện tên HTX (`display_name`); không hiện họ tên/SĐT người đại diện. Dữ liệu cá nhân, nếu cần, là nhãn che do API trả.
- **Dữ liệu cũ** (khai báo máy, thửa đang tham chiếu HTX nền tảng): đối chiếu qua `GET /v1/kn/producer-records?kind=cooperative&htx_ref=<ref>&status=all&limit=1`.
  - Có hồ sơ KN liên kết → hiện tên hồ sơ (thêm "(ngừng hoạt động)" nếu hồ sơ đã vô hiệu).
  - Không có → hiện "‹tên HTX› — chưa liên kết hồ sơ KN" kèm gợi ý thêm hồ sơ HTX ở Dữ liệu › HTX và liên kết HTX đã công bố. Tên lấy từ dữ liệu khai báo máy (`KNCoopMachines.name`), hoặc với thửa từ publication `GET /v1/kn/producer-htx-candidates?commune=` (chỉ đọc). Vẫn xem/khai báo được như trước; sửa thửa mà không đổi HTX thì không gửi `cooperative_ref`.
  - Màn Máy móc thêm khung "HTX đã khai báo máy" (từ `GET /v1/kn/machine-counts`): tên theo hồ sơ KN liên kết (đọc đủ mọi trang hồ sơ của xã đang lọc, `loadAllRecords`), HTX chưa có hồ sơ liên kết có nhãn "chưa liên kết hồ sơ KN"; bấm để mở khai báo.
- **Quyền giữ nguyên:** thửa chỉ Cấp Xã/Admin hỗ trợ ghi (`usePlotReadOnly`, #104) — Cấp Tỉnh/Quốc gia thấy ô HTX khóa, vẫn thấy tên hồ sơ liên kết, không tải danh sách/tìm. Kiểm khai báo máy #103 (năm SX, số lượng, trùng loại) không đổi.
- **Client:** bỏ `collectApi.cooperatives` — web KN không còn gọi GET `/v1/kn/cooperatives`. Route legacy và mock của nó vẫn còn cho tới #160 (gỡ route, lane:shared) sau #161 (field-app).
- **Không đổi Go/OpenAPI:** dùng nguyên `GET /v1/kn/producer-records` (đã có `q`, `htx_ref`, `cursor`, `limit` 1–200) và `GET /v1/kn/producer-htx-candidates`.
- **Test:** `tt-htx-picker.test.tsx` (quá 50 hồ sơ → tải thêm theo con trỏ tới hồ sơ thứ 120; tìm gửi `q`; hồ sơ chưa liên kết bị khóa; HTX đã khai báo máy/thửa cũ liên kết và chưa liên kết; Cấp Tỉnh chỉ xem; client không còn `cooperatives`), cập nhật `tt-mach`, `tt-plot`, `sweep-tt`, `api/mock/kn-collect.test.ts`.

### Mục 2–3 — CHỜ PO: chủ dữ liệu khai báo máy và thửa

Hiện trạng (không đổi trong lát này):

| Màn | Route | Ghi vào | Khóa tham chiếu |
|---|---|---|---|
| Máy móc HTX — số lượng theo loại (TT-10) | `PUT /v1/kn/machine-counts/{ref}` (`internal/platform/mechanization/handler/machine_count.go`) | `pf_mechanization.coop_machine_count`, `coop_machine_count_event` (module nền mechanization) | `{ref}` = HTX nền tảng (pf_party) |
| Máy móc HTX — năm SX | `PUT /v1/kn/machine-profiles/{ref}` (#119) | hồ sơ KN riêng (pf_extension) | HTX nền tảng |
| Thửa — hình, tên, chủ hộ, HTX (TT-11) | `POST /v1/kn/plots`, `PUT /v1/kn/plots/{ref}`, `POST …/{ref}/boundary` (`internal/platform/cooperative/plot/handler/kn.go`) | `pf_coop` (thửa, ranh giới; module nền cooperative/plot) | `cooperative_ref` = HTX nền tảng (tùy chọn) |
| Thửa — cây trồng | `PUT /v1/kn/plot-profiles/{ref}` (#119) | hồ sơ KN riêng (pf_extension) | thửa |

Field-app (#161) cũng gọi `machine-counts` và `plots`.

**Câu hỏi PO:**

1. **Khai báo số lượng máy theo HTX (TT-10)** là dữ liệu HTX hay dữ liệu KN?
   - *Dữ liệu HTX:* giữ ở `pf_mechanization`, do HTX/MKG ghi. KN chỉ đọc qua publication, nên phải gỡ quyền ghi của KN ở `PUT /v1/kn/machine-counts` (ticket lane:shared). Màn Máy móc của KN thành màn chỉ xem.
   - *Dữ liệu KN:* lập sổ KN riêng (như năm SX #119), khóa theo hồ sơ KN (`producer_ref`) thay cho HTX nền tảng, và chuyển màn web, field-app sang sổ đó. Số đã khai báo ở `pf_mechanization` thì cần chốt: sao chép một lần hay chỉ đọc.
2. **Thửa (`/v1/kn/plots` ghi `pf_coop`)**: cùng câu hỏi. Nếu thửa là dữ liệu KN: sổ thửa KN riêng (hình, chủ hộ ở kho định danh), liên kết hồ sơ KN (HTX/Nông hộ) thay `cooperative_ref` nền tảng, và xử lý chồng lấn với thửa nền tảng. Nếu là dữ liệu HTX: gỡ quyền ghi của KN, KN chỉ đọc thửa qua publication.
3. Trong lúc chờ, có chấp nhận như lát này không: chỉ hồ sơ KN đã liên kết HTX đã công bố mới chọn được để khai báo máy/thửa, còn dữ liệu cũ chưa liên kết vẫn hiện kèm nhãn "chưa liên kết hồ sơ KN"?

Nghiệm thu còn lại của #162 (sau khi PO chốt): ghi quyết định vào file này, và hồi quy "không ghi bảng HTX" cho phần được chuyển sang KN (như `tests/knrecords`).
