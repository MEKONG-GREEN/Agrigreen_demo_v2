# Architecture Decision Records

| # | Quyết định | Trạng thái |
|---|---|---|
| 0001 | [Go là ngôn ngữ dịch vụ](0001-go-ngon-ngu-dich-vu.md) | Chấp nhận |
| 0002 | [RabbitMQ thay vì hàng đợi trong PostgreSQL](0002-rabbitmq.md) | Chấp nhận |
| 0003 | [Event sourcing mức B](0003-event-sourcing-muc-b.md) | Chấp nhận |
| 0004 | [Mã hóa phong bì X25519 theo hộ](0004-ma-hoa-phong-bi-theo-ho.md) | Chấp nhận — hiện thực ở G1 |
| 0005 | [Row-level security, một policy mỗi bảng](0005-rls-mot-policy-moi-bang.md) | Chấp nhận — sửa đổi bởi ADR-0030 (đề xuất) |
| 0006 | [Engine đồng bộ ngoại tuyến và phương án xử lý slot](0006-engine-dong-bo-ngoai-tuyen.md) | Chấp nhận — 30/09/2026 ([triển khai](trien-khai-g1.md)); ghi lệch v4.1 04/10/2026: giữ engine Application API, C-1 không chặn (X-06) |
| 0007 | [Danh mục nhà cung cấp trong lãnh thổ](0007-nha-cung-cap-trong-lanh-tho.md) | Chấp nhận — 30/09/2026 ([triển khai](trien-khai-g1.md)) |
| 0008 | [Phân vùng mạng và bảng luồng](0008-phan-vung-mang.md) | Chấp nhận — hiện thực ở G4 (chart `deploy/k8s`, kiểm `check_flows.py`) |
| 0009 | [Mô hình tenant theo tổ chức sở hữu dữ liệu](0009-mo-hinh-tenant.md) | Chấp nhận — 30/09/2026 ([triển khai](trien-khai-g1.md)); sửa đổi bởi ADR-0030 (đề xuất) |
| 0010 | [Kênh nông hộ: Flutter cho hiện trường, Zalo Mini App chỉ online](0010-kenh-nong-ho.md) | Chấp nhận — 30/09/2026 ([triển khai](trien-khai-g1.md)) |
| 0011 | [Bổ sung module nghiệp vụ cho các app](0011-module-nghiep-vu-cac-app.md) | Chấp nhận — 30/09/2026 ([triển khai](trien-khai-g1.md)) |
| 0012 | [Tính duy nhất trên bảng sổ phân vùng](0012-duy-nhat-tren-bang-so-phan-vung.md) | Chấp nhận; ghi lệch v4.1 04/10/2026: giữ `shared.chain_head`/`shared.ledger_idem` chung (X-09), theo v4.1 hạn idempotency 30 ngày qua W2.1 |
| 0013 | [Quan sát: trace_id từ G0, OTLP từ G1](0013-quan-sat-otlp.md) | Chấp nhận — đã hiện thực OTLP ở G1 |
| 0014 | [Thư viện xuất Excel/PDF và giấy phép phông chữ nhúng](0014-thu-vien-xuat-bao-cao-va-phong-chu.md) | Đề xuất |
| 0015 | [Kênh đẩy thông báo và RB-7](0015-kenh-day-thong-bao-rb7.md) | Đề xuất — đã hiện thực ở pf/notify |
| 0016 | [Giấy phép phụ thuộc Python của optimizer](0016-giay-phep-python.md) | Đề xuất — bổ sung bởi ADR-0036 (đề xuất) |
| 0017 | [Kho chuỗi thời gian — PostgreSQL phân vùng tháng ở cụm `ts`](0017-kho-chuoi-thoi-gian.md) | Đề xuất — hiện thực ở G3-0; ghi lệch v4.1 04/10/2026: không TimescaleDB (TSL trái RB-4, X-05, H-4) |
| 0018 | [Sự kiện nội bộ MKG — vhost RabbitMQ `mkg.internal` (Q-MKG-01)](0018-su-kien-noi-bo-mkg.md) | Đề xuất — hiện thực ở G3-0 |
| 0019 | [Định danh thiết bị cân, ký tại nguồn và đồng hồ](0019-dinh-danh-thiet-bi-va-dong-ho.md) | Đề xuất — hợp đồng dây đóng băng ở G3-0 |
| 0020 | [Liên thông nền tảng ↔ MKG khác tenant bằng cấp quyền chia sẻ (RB-2, Q-MKG-12)](0020-lien-thong-platform-mkg-khac-tenant.md) | Chấp nhận — chủ sản phẩm chốt 29–30/09/2026; hiện thực ở pf 000042; sửa đổi bởi ADR-0030, ADR-0033, ADR-0034 (đề xuất) |
| 0021 | [Hạ tầng Kubernetes G4 — operator, PKI, bí mật và tách vùng bằng cấu hình](0021-ha-tang-kubernetes-g4.md) | Đề xuất — hiện thực ở G4 (`deploy/k8s`, `deploy/iac`) |
| 0022 | [Public API cho bên thứ ba — đơn vị triển khai riêng, khóa API lưu băm, scope, quota](0022-public-api-ben-thu-ba.md) | Đề xuất — nền móng G5-0; sửa đổi bởi ADR-0033, ADR-0035 (đề xuất) |
| 0023 | [`pf/metering` — sổ đo lường sử dụng là chứng từ, bộ đếm hạn mức, bảng kê theo kỳ](0023-so-do-luong-su-dung-metering.md) | Đề xuất — nền móng G5-0 |
| 0024 | [Một điểm kiểm soát xuất dữ liệu (DLP) cho van, Public API, gói MRV, báo cáo](0024-diem-kiem-soat-xuat-du-lieu-dlp.md) | Đề xuất — nền móng G5-0 |
| 0025 | [Hồ sơ cấp phát Phase K — cấu hình, số bản sao, tenant bên thứ ba, đơn vị thẩm định](0025-ho-so-cap-phat-phase-k.md) | Đề xuất — nền móng G5-0; sửa đổi bởi ADR-0033 (đề xuất) |
| 0026 | [Xuất bản báo cáo tổng hợp sang Google Sheets (Báo cáo Chi cục KN v7.7, RB-7)](0026-xuat-google-sheets.md) | Đề xuất — bộ chuyển tắt mặc định, chờ khách Q-14 |
| 0027 | [Bốn tầng module khai trong tệp khai báo, ứng dụng là tổ hợp, gọi trong tiến trình](0027-bon-tang-module-tep-khai-bao.md) | Đề xuất — chờ MKG ký (A7); hiện thực W1.x |
| 0028 | [Một vai đăng nhập mỗi đơn vị triển khai, `SET LOCAL ROLE r_<module>`](0028-vai-dang-nhap-theo-don-vi-trien-khai.md) | Đề xuất — chờ MKG ký (A7); hiện thực W2.3, W2.6 |
| 0029 | [`k_ref` — bản chiếu chỉ đọc của dữ liệu lõi trong mỗi cụm, trễ ≤ 5 s](0029-k-ref-ban-chieu-loi-moi-cum.md) | Đề xuất — chờ MKG ký (A7); hiện thực W2.4, W2.5 |
| 0030 | [Chia sẻ trong vùng nền tảng là đọc tại chỗ có cấp phép, qua RLS `share_scope`](0030-doc-tai-cho-co-cap-phep.md) | Đề xuất — chờ MKG ký (A7), H-1 còn mở; sửa ADR-0005, 0009, 0020; hiện thực W3.4, W4.1, W4.2 |
| 0031 | [Vai tổng hợp `r_agg` và view công bố, ngưỡng k](0031-vai-tong-hop-r-agg.md) | Đề xuất — chờ MKG ký (A7), k `[KÝ]`; hiện thực W6.3, W6.4 |
| 0032 | [Đơn vị hành chính có hiệu lực, ràng buộc loại trừ và bảng kế thừa](0032-don-vi-hanh-chinh-co-hieu-luc-va-ke-thua.md) | Đề xuất — chờ MKG ký (A7), nạp production chờ C-9 (H-16); hiện thực W2.2 |
| 0033 | [Cổng tích hợp `cmd/integration-gateway` — MKG như mọi đối tác, feed cursor, nạp theo lô, webhook mỏng](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md) | Đề xuất — chờ MKG ký (A7, H-3); sửa ADR-0020, 0022, 0025; hiện thực W5.1–W5.10 |
| 0034 | [Luồng song phương — chỉ hai bên của giao dịch thấy](0034-luong-song-phuong.md) | Đề xuất — chờ MKG ký (A7) và chờ C-12 (H-2); sửa ADR-0020; hiện thực W5.6 |
| 0035 | [Cấp phép tổng hợp theo xã có ngưỡng k — tập dữ liệu dựng sẵn](0035-cap-phep-tong-hop-theo-xa-nguong-k.md) | Đề xuất — chờ MKG ký (A7), k `[KÝ]`; sửa ADR-0022; hiện thực W4.1, W5.3, W6.4 |
| 0036 | [Engine ba chế độ ở vùng nội bộ MKG, `lib/optcore` dùng chung](0036-engine-ba-che-do-vung-noi-bo-optcore.md) | Đề xuất — chờ MKG ký (A7); bổ sung ADR-0016; W8.1 ngay, W8.3 chờ MKG v1.3 |
| 0037 | [Tách xác thực và phân quyền, không mạo danh](0037-tach-xac-thuc-va-phan-quyen-khong-mao-danh.md) | Đề xuất — chờ MKG ký (A7); ghi lại KN P1 (đã hiện thực, pf 000060–000061) làm `ResolveContext` ĐTK 1; phần còn lại W7.2 |
| 0038 | [Bộ chạy migration theo module — sổ theo module, thứ tự topo, vai `own_<module>`](0038-bo-chay-migration-theo-module.md) | Đề xuất — hiện thực bước 1 ở W7.1 (sau cờ `AG_MIGRATE_RUNNER=v2`, mặc định v1 một bản phát hành) |
| 0041 | [MFA theo nhóm cho cán bộ KN, phiên giữ theo D9, realm quản trị riêng, vô hiệu tài khoản 45 ngày](0041-mfa-theo-nhom-d24.md) | Chấp nhận — quyết định CEO D24, D25 (13/10/2026), ứng với v4.3 ADR-23/P-1…P-5; hiện thực gói MFA (pf 20261013134254, 20261013142347); sửa ADR-0010 |
| 0049 | [`cap/price` = module năng lực `pf/price`](0049-cap-price-pf-price.md) | Đề xuất — khung hiện thực ở W11.71 (pf 000138); số 0039–0048 dành cho ADR khác của đợt; bị thay thế một phần bởi ADR-0055 (Báo giá là của KN) |
| 0050 | [`dom/hrm` = module nghiệp vụ `pf/hrm`; danh mục nhân sự ở module của chủ](0050-dom-hrm-pf-hrm.md) | Đề xuất — khung + danh mục nhân sự ở W11.21 (pf 000147); dữ liệu thật chờ Q-070 |
| 0051 | [Tách `cap/export` thành module năng lực `pf/export` — mẫu văn bản có phiên bản, bộ ghi .docx tất định, sổ lần xuất có băm, số văn bản tự cấp](0051-cap-export-pf-export-docx.md) | Đề xuất — hiện thực ở W10.17 (pf 20261024090957); dùng thật chờ Q-062 |
| 0052 | [Đồng sở hữu dữ liệu MKG + HTX, neo HTX theo mã số thuế, HTX tạm "Chưa xác nhận" và nhận quyền](0052-dong-so-huu-mkg-htx-neo-mst.md) | Chấp nhận — quyết định CEO D2 (13/10/2026), trả lời H-1/QĐ-2; hiện thực gói D2 (pf 20261013091454) |
| 0053 | [Một hợp đồng `_pub` có phiên bản cho "số liệu đã chốt" — họ `closed_figures/v1`, port KHTC `KHTCSources`, bên đọc qua thành viên vai `_pub`](0053-hop-dong-so-lieu-da-chot-closed-figures.md) | Đề xuất — hiện thực ở PFPUB; D8, D17, D18, D2 đã chốt; Trồng trọt giữ vòng đời báo cáo |
| 0054 | [`cap/directory` = module năng lực `pf/contact` — danh bạ dùng chung, một bản ghi/người ở kho định danh, lọc trường ở API](0054-cap-directory-pf-contact.md) | Đề xuất — hiện thực W11.23 (pf 20261013230912, vault 20261013165158); Q-071, Q-072 theo đề xuất của dev; bị thay thế một phần bởi ADR-0055 (Contact là của KN) |
| 0055 | [Tách hệ thống Khuyến nông — hệ độc lập về logic (`internal/kn/*`, dữ liệu hệ khác chỉ qua `_pub`/data product/lớp tích hợp, client kn-web trong G1, frontend riêng sau G1); tách vật lý tùy chọn khi vận hành cần](0055-tach-he-thong-khuyen-nong.md) | Chốt ranh giới G1 theo PO; đề xuất đối chiếu D2 chờ PO |

ADR 0006, 0007, 0009, 0010, 0011 được chủ sản phẩm chấp nhận ngày 30/09/2026 trong phiên làm việc. Bảng đối chiếu cam kết → mã → kiểm thử ở [trien-khai-g1.md](trien-khai-g1.md).

ADR 0001–0008 là danh sách bắt buộc ở Phụ lục B của tài liệu kiến trúc. ADR 0009–0016 phát sinh khi tổng hợp BRD và khi dựng G0. ADR 0017–0019 là nền của cổng G3 ([g3-conventions](../dev/g3-conventions.md)). ADR-0020 đóng câu hỏi RB-2 (MKG là tenant riêng). ADR-0021 ghi các lựa chọn hạ tầng của cổng G4. ADR 0022–0025 là nền của cổng G5 ([g5-conventions](../dev/g5-conventions.md)): Public API, `pf/metering`, điểm kiểm soát xuất dữ liệu, hồ sơ Phase K. ADR 0027–0031 là năm ADR lõi của kiến trúc v4.1 (gói W0.1, [kế hoạch](../kien-truc-v4.1-ke-hoach.md)), ứng với mục 1–2, 7–10 Phụ lục B; ADR-0030 sửa ADR-0005, 0009, 0020. ADR 0032–0036 là năm ADR tích hợp và engine (gói W0.2), ứng với mục 11, 15–19 Phụ lục B; ADR-0033 sửa ADR-0020, 0022, 0025; ADR-0034 sửa ADR-0020; ADR-0035 sửa ADR-0022; ADR-0036 bổ sung ADR-0016. ADR-0037 (gói W0.3) ứng với mục 14 Phụ lục B. Cùng gói W0.3, ADR-0006, 0012 và 0017 có thêm mục "Đối chiếu v4.1 (04/10/2026)" ghi lệch so với v4.1; phần quyết định gốc không đổi.

## Số đã giữ chỗ

Số của ADR **chưa viết** mà tài liệu khác đã nhắc tới. Viết ADR thì thêm dòng vào bảng chỉ mục ở trên và xóa dòng ở đây trong cùng commit. `go run ./tools/docscheck` (CI) kiểm: số trong tên tệp `NNNN-*.md` không trùng; bảng chỉ mục khớp đúng các tệp; số giữ chỗ không trùng nhau và không trùng ADR đã có. Gói cần ADR mới lấy số trống lớn nhất + 1 lúc viết; nếu hai gói song song cùng lấy một số, `docscheck` của gói gộp sau hỏng và gói đó đổi sang số trống kế tiếp (ghi chú ở [docs/dev/parallel-packages.md](../dev/parallel-packages.md)).

| # | Dành cho | Nguồn |
|---|---|---|
| 0039 | v4.3 ADR-21 — stack Go cho hệ nội bộ | [đối chiếu BRD KN §3.1](../kn/doi-chieu-brd-kn-2026-10.md#31-quy-ước), W10.00 |
| 0040 | v4.3 ADR-22 — hai hệ thống thông tin | như trên |
| 0042 | v4.3 ADR-24 — chữ ký số trên lô | như trên, W10.07 |
| 0043 | Vai trò kiểm soát/xử lý dữ liệu | như trên |
| 0044 | Căn cứ an ninh mạng, cấp độ | như trên |
| 0045 | Phân loại dữ liệu 4 mức | như trên |
| 0046 | Phụ thuộc OR-Tools | như trên |
| 0047 | Dữ liệu vị trí người lái/người đếm | như trên, W12.19 |
| 0048 | Liên kết khách có OTP | như trên |
