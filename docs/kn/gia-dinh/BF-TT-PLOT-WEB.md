## BF-TT-PLOT-WEB — Thửa ruộng: sửa thửa, bắt buộc, diện tích tự tính, chặn chồng lấn trên web (#104)

- **Nguồn:** PRD `kn-bugfix-brd-alignment` TT-11 ("Sửa được thửa; bắt buộc Tên thửa, Chủ hộ, Xã/phường, Cây trồng; diện tích tính từ hình vẽ, không nhập; chặn lưu khi thửa chồng lấn thửa khác"). Phía máy chủ đã gộp: #113 BF-SRV-GUARD (chủ hộ bắt buộc, tên thửa/xã bắt buộc, chồng lấn KN trực tuyến luôn chặn `409 geo.plot_overlap`) và #119 BF-SRV-KN-ATTRS (cây trồng ở hồ sơ KN riêng `/v1/kn/plot-profiles`). Gói này chỉ sửa web, máy chủ giả lập và docs KN. Không đổi API, Go hay migration.
- **Màn mới** (`kn/cycle/screens/PlotScreens.tsx`, chuyển `PlotsScreen` ra khỏi `DataScreens.tsx`):
  - `/kn/du-lieu/ve-thua`: danh sách thêm cột **Cây trồng** (thửa chưa có thì hiện "Chưa khai báo"), cột Thao tác (Sửa; Cấp Quốc gia chỉ Xem), nút **Thêm thửa**. Bỏ cột "Khai báo (ha)" và "Chênh lệch" vì TT-11 không cho nhập diện tích.
  - `/kn/du-lieu/ve-thua/them-moi`: vẽ ranh giới đa giác trên bản đồ (bấm từng đỉnh hoặc nhập tọa độ, dùng `EditorMap`/`DrawSection` của web GIS), các thửa khác của xã hiện làm lớp ngữ cảnh. Bắt buộc Xã/phường, Tên thửa, Chủ hộ, Cây trồng, ranh giới. HTX và Ghi chú không bắt buộc.
  - `/kn/du-lieu/ve-thua/[id]`: sửa Tên thửa, Chủ hộ, Cây trồng, HTX, Ghi chú. Bấm "Sửa ranh giới" để vẽ lại (nạp sẵn các đỉnh cũ), khi đó **Lý do sửa ranh giới** bắt buộc, tối thiểu 10 ký tự như máy chủ. Có khung Log từ `/v1/kn/plots/{ref}/log`. Thửa đã ngừng dùng chỉ xem.
- **Diện tích tự tính:** ô chỉ đọc, tính diện tích trắc địa của hình đang vẽ để xem trước. Máy chủ tính lại trên hình khi lưu. Web không gửi `declared_area_ha`. Hình phải có ít nhất 3 điểm phân biệt, không thẳng hàng, diện tích 0,001 – 1.000 ha (như `geo.area_out_of_range`).
- **Chồng lấn:** máy chủ quyết định và chặn (409 `geo.plot_overlap`). Web hiện thông báo "Thửa chồng lấn … không được lưu" kèm lỗi máy chủ và giữ nguyên trang để vẽ lại. Web không tự tính giao cắt, vì máy chủ có ngưỡng dung sai `geo.overlap_tolerance_pct` và các thửa liền bờ chung cạnh không phải chồng lấn.
- **Thứ tự ghi trong một lượt thao tác** (`useAction`, khóa chống gửi trùng #129; mọi bước bọc `idem` của chính lượt, nên bấm Lưu lại sau mất phản hồi dùng lại khóa cũ cho từng bước và không báo thành công khi bước sau lỗi):
  - Thêm: `POST /v1/kn/plots` → `PUT /v1/kn/plot-profiles/{ref}` (cây trồng). Chồng lấn thì dừng ở bước đầu, không ghi gì.
  - Sửa: `POST /v1/kn/plots/{ref}/boundary` (nếu vẽ lại) → `PUT /v1/kn/plots/{ref}` (chỉ trường đã đổi) → `PUT /v1/kn/plot-profiles/{ref}` (nếu đổi cây trồng). Ranh giới làm trước nên chồng lấn sẽ chặn cả lần lưu.
- **Chủ hộ khi sửa:** tên thật nằm ở kho định danh, web chỉ thấy nhãn che (vd "Nguyễn V. A."). Ô Chủ hộ để trống nghĩa là giữ nguyên chủ hộ hiện tại. Nhập họ tên thì đổi chủ hộ. Ô này chỉ bắt buộc khi thửa chưa có chủ hộ.
- **Xã/phường khi sửa:** chỉ đọc. API sửa thửa không đổi xã, xã đi theo vị trí thửa.
- **Danh mục cây trồng:** `GET /v1/masterdata/catalogs/crop`, chỉ các mục lá đang dùng. Mục nhóm (`is_group = "true"`) không chọn được.
- **Giả định / câu hỏi:**
  - Web vẽ được đa giác. Hình tròn, hình vuông và vẽ ngoại tuyến vẫn ở ứng dụng di động.
  - Danh sách bỏ hai cột diện tích khai báo/chênh lệch. Nếu khách vẫn cần xem số khai báo cũ thì thêm lại cột chỉ đọc.
  - Tạo thửa và đặt cây trồng là hai module, máy chủ không gộp được một giao dịch (#119). Nếu bước cây trồng lỗi, thửa đã tạo vẫn hiện "Chưa khai báo" ở danh sách để bổ sung.
- **Mock:** `api/mock/kn-collect-plots.ts` có danh sách, chi tiết kèm hình, tải GeoJSON, tạo, sửa, sửa ranh giới, log, và `plot-profiles` (GET/PUT). Chồng lấn được giả lập bằng khung bao. Thửa mẫu giữ ref cũ.
