# ADR-0060: App Khuyến nông là hệ vật lý riêng (APP_KN_v1.0), nền tảng gỡ phần KN

**Trạng thái:** Chấp nhận — PO duyệt kế hoạch PA1 ngày 16/11/2026 (áp dụng mặc định P1–P8); gói #311 PA1-ADR-0058-KN-PHYSICAL (bước 0 của PA1). **Ngày:** 16/11/2026.
**Số ADR:** gói mang tên "ADR-0058" nhưng số 0058 và 0059 đã được nhánh đang mở dùng (`codex/knstandalone261gov` — ADR-0058 KN độc lập theo BRD 2.0, gói #261 đang hoãn; `claude/amazing-ramanujan-og5gx8` — ADR-0059 tách hệ HTX), nên ADR này lấy số 0060.
**Thay thế một phần:** [ADR-0055](0055-tach-he-thong-khuyen-nong.md) — câu Q-KNS-02 và dòng "Binary/realm" của bảng bố cục đích (mục 2). Các phần khác của ADR-0055 không đổi, trong đó giữ Q-KNS-01.
**Căn cứ:** kế hoạch PA1 đã duyệt (kn-handoff `PLAN-KN-PA1-20261116.md`); báo cáo đối chiếu APP_KN_v1.0 `dev@01121e5` với nền tảng `dev@360276fb` (kn-handoff `reports/app-kn-v1-tuong-thich-20261115.md`).
**Liên quan:** [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md) (cổng tích hợp, feed), [ADR-0032](0032-don-vi-hanh-chinh-co-hieu-luc-va-ke-thua.md) (ĐVHC), [ADR-0049](0049-cap-price-pf-price.md) (Báo giá), [ADR-0029](0029-k-ref-ban-chieu-loi-moi-cum.md) (`k_ref`).

## Bối cảnh

ADR-0055 chốt KN là hệ độc lập **về logic** trong cùng binary, cụm và realm với nền tảng (Q-KNS-02). Từ 11/11/2026 có repo `MEKONG-GREEN/APP_KN_v1.0`, một bản App Khuyến nông viết lại riêng. Báo cáo đối chiếu ngày 15/11/2026 kết luận APP_KN **không tương thích** với nền tảng ở mức mã, dữ liệu và danh tính; chỉ tương thích một phần ở hạ tầng (cùng PostgreSQL, Keycloak, RabbitMQ, Go/gin):

- **Mã:** APP_KN là `module agrigreen`, Go 1.25, gin 1.10, golang-jwt v5; nền tảng là `module github.com/MEKONG-GREEN/Agrigreen_demo_v2`, Go 1.26.0 (toolchain go1.26.10), gin 1.12, go-oidc v3. APP_KN có 105 commit, khởi đầu từ gói zip của một repo khác, **0 tệp** import mã nền tảng. Cấu trúc khác hẳn: `internal/k/*` (kernel), `cap/*`, `dom/*` so với `internal/platform/*` (27 module nền) và `internal/kn/{extension,hrm,price,contact}`. Web: `web/kn` Next 15.5 (159 trang) so với `apps/web-ops/src/app/kn` Next 16.4 (233 trang).
- **Dữ liệu:** APP_KN có ba instance PG riêng (`agrigreen`, `vault`, `access`). Hai bên trùng tên schema `k_ref` và `vault` (cùng bảng `k_ref.org`, `admin_unit`, `node_closure`, `share_scope`) nhưng khác cột, và trùng vai DB `app_platform`, `app_worker`, `app_vault`. Vì vậy **không chạy chung một database được**; chung cụm thì được nếu mỗi bên một database và vai DB khác tên. Vault của hai bên khác lược đồ và cách mã hóa: chuyển định danh phải giải mã ở nền tảng rồi mã hóa lại ở APP_KN.
- **Danh tính:** APP_KN có dịch vụ Access riêng. Keycloak chỉ trả `sub`; Access ký token ngữ cảnh (`tenant_id`, `node_id`, `dept`, `perms`, `communes`, `ctx_epoch`…), api-platform kiểm qua JWKS của Access; vai × Nút × phòng × xã nằm ở `k_access.*`. Nền tảng kiểm thẳng token Keycloak realm `agrigreen` (`tenant_id`, `org_ref`, `realm_access.roles`), ngữ cảnh KN ở `pf/accounts` qua header `X-AG-Context`, client `kn-web`, vai `kn_user`, realm quản trị `agrigreen-kn-admin`. Cắm APP_KN vào realm `agrigreen` đòi client `agrigreen-access` có `manage-users` trên **toàn realm**, tức sửa/khóa được cả người dùng HTX/MKG. Bản demo server của APP_KN chưa có Keycloak (chạy `devaccess`).
- **Hợp đồng:** APP_KN không có view `_pub`, data product hay feed; chỉ công bố 2 schema sự kiện (`dom.cooperative.changed`, `khtc-source-snapshot`), tên sự kiện `dom.*`/`cap.*` khác `pf.extension.*`/`pf.price.*`/`pf.forms.*`. API cùng tiền tố `/v1/extension/*`, `/v1/forms/*` nhưng khác hợp đồng.
- **Phụ thuộc ngược:** HTX, MKG và Public API gần như không đọc dữ liệu KN. Hai chỗ vướng: API `/v1/extension/support-requests` mà App HTX gọi nằm trong `pf/extension`, và `pf/metrics` (`r_metrics`, `r_agg`) đọc `pf_extension_pub`, `pf_forms_pub`, `pf_price_pub` cùng view nhiệm vụ cũ. Sự kiện `pf.extension.gps_log` phục vụ App HTX → GIS.
- **Độ phủ:** APP_KN tự chấm 44 Đạt / 35 Lệch / 33 Thiếu / 3 chưa xác minh trên 115 FN (13/11, theo KHTC v2.2); KN trên nền tảng 81 Đã có / 30 Một phần / 2 Chưa có trên 113 FN của BRD 2.0 (07/11). Hai thang chấm khác nhau. APP_KN chưa có app di động/đồng bộ ngoại tuyến; nền tảng có field-app 13 màn KN.

Báo cáo xét ba phương án: PA1 (APP_KN độc lập, nền tảng gỡ KN, liên thông qua lớp tích hợp, cỡ XL chia 4 bước), PA2 (APP_KN gọi API/sự kiện nền tảng, trái D1 "KN tự nhập" và tạo phụ thuộc vận hành chéo), PA3 (gộp APP_KN vào monorepo thay `internal/kn`, thực chất viết lại lần hai, XL+). "Kết nối trực tiếp" theo nghĩa APP_KN dùng chung DB, `pf/accounts` hay module nền thì không làm được nếu không viết lại APP_KN, và trái Q-KNS-01. Chọn APP_KN là chọn **ranh giới vật lý**, nên cần quyết định thay một phần ADR-0055. PO chọn PA1 ngày 16/11/2026.

## Quyết định

1. **App Khuyến nông chạy trên APP_KN_v1.0** (`MEKONG-GREEN/APP_KN_v1.0`, nhánh `dev`) như một hệ độc lập về vật lý:
   - **Database riêng:** cụm PostgreSQL riêng, hoặc cùng cụm nhưng database và vai DB khác tên với nền tảng (P6) — vì `k_ref`, `vault`, `app_platform`… trùng tên.
   - **Keycloak realm riêng `kn`** trên cùng instance Keycloak với nền tảng (P5), để giữ được `sub` khi chuyển tài khoản và không cần `manage-users` trên realm `agrigreen`. Bản triển khai không dùng `devaccess`.
   - **Dịch vụ Access riêng** của APP_KN phát token ngữ cảnh; APP_KN không dùng `pf/accounts`, `X-AG-Context` hay client `kn-web`.
   - **Deploy riêng:** binary, web và lịch phát hành riêng. Việc lấp BRD 2.0 cho KN làm trên repo APP_KN theo quy trình của repo đó (P4).
2. **Không cắm APP_KN vào nền tảng:** không dùng chung database, `pf/accounts` hay module nền của nền tảng.
3. **Nền tảng chuyển dữ liệu KN sang APP_KN rồi gỡ toàn bộ phần KN.** Sau khi gỡ, schema và dữ liệu KN cũ trên nền tảng **giữ chỉ đọc, không DROP** (theo quy tắc gỡ chức năng của repo: gỡ route/màn/handler/quyền, giữ dữ liệu cũ).
4. **Nền tảng dừng tính năng KN mới** (P1): chỉ còn sửa lỗi chặn người dùng, vá bảo mật, và các gói của bước 0, 2, 3, 4 dưới đây.
5. **Thay ADR-0055 ở hai chỗ:**
   - Q-KNS-02 ("đích là độc lập logic, không yêu cầu ranh giới vật lý; binary/realm/cụm riêng chỉ sau G1 khi vận hành cần") → **đích là hệ vật lý riêng APP_KN** như mục 1.
   - Dòng "Binary/realm" của bảng bố cục đích ("api-platform, worker và realm agrigreen chung; realm quản trị agrigreen-kn-admin giữ theo D25") → **binary, worker, Access và realm `kn` của APP_KN**; realm `agrigreen-kn-admin`, client `kn-web` và vai `kn_*` bị gỡ khỏi nền tảng ở bước 4.
6. **Giữ Q-KNS-01:** KN chỉ trao đổi với hệ khác qua lớp tích hợp (hợp đồng, feed, sự kiện công bố); không gọi vào module hệ khác. Với APP_KN điều này nghĩa là mọi liên thông với nền tảng đi qua cổng tích hợp (ADR-0033), không qua DB hay API nội bộ. Q-KNS-03…06 giữ nguyên về nghiệp vụ, áp dụng cho dữ liệu khi chuyển sang APP_KN (một tenant KN mỗi Trung tâm tỉnh; KN không ghi dữ liệu HTX; MRV thuộc HTX/MKG).
7. **Chưa kết nối hệ khác** (quyết định 07/11/2026 về KN chạy độc lập vẫn giữ): pha kết nối làm sau, theo các hợp đồng ở mục "Hợp đồng giữ cho pha kết nối".

## Bốn bước và cổng

| Bước | Nội dung | Repo | Cổng để sang bước sau |
|---|---|---|---|
| 0. Quyết định | ADR này; cập nhật trạng thái ADR-0055; cập nhật tài liệu quy trình của kn-handoff | nền tảng + kn-handoff | ADR gộp vào `dev` nền tảng |
| 1. Đuổi kịp độ phủ | APP_KN lấp BRD 2.0 (113 FN; KHTC v2.3) tới mức tối thiểu bằng KN trên nền tảng; có lời giải di động/ngoại tuyến (P7: viết lại 13 màn KN của field-app để gọi API APP_KN, giữ cơ chế đồng bộ ngoại tuyến của package dùng chung); có Keycloak thật realm `kn` | APP_KN | **Cổng độ phủ:** chấm cùng một bộ test case BRD 2.0 cho cả hai hệ; APP_KN ≥ nền tảng ở mọi FN đang "Đã có" trên nền tảng |
| 2. Tiền đề phía nền tảng | Chuyển `/v1/extension/support-requests` và `pf.extension.gps_log` về chủ HTX; cắt nguồn `pf_extension_pub`, `pf_forms_pub`, `pf_price_pub` và view nhiệm vụ cũ khỏi `pf/metrics` (`r_metrics`, `r_agg`) | nền tảng | Test HTX/MKG/metrics xanh khi module KN tắt |
| 3. Chuyển dữ liệu | Công cụ ETL nền tảng → APP_KN theo tenant/Trung tâm: tài khoản và vai (`pf/accounts` + realm `agrigreen` → Access + realm `kn`), cây tổ chức, ĐVHC, danh mục, HTX/nông hộ/thửa/máy, dữ liệu 5 phòng; định danh trong vault giải mã và mã hóa lại theo khóa APP_KN; sổ chỉ ghi thêm nhập dưới dạng bản ghi mở đầu có dẫn nguồn. Diễn tập trên bản chụp | APP_KN (công cụ) + nền tảng (xuất) | Diễn tập: đếm dòng và tổng theo Trung tâm khớp 100%; người dùng thử đăng nhập được |
| 4. Cutover và gỡ KN | Nền tảng đặt KN chỉ đọc → chạy ETL thật → mở APP_KN cho người dùng → gỡ khỏi nền tảng: `internal/kn/*`, import ở `cmd/api-platform/assembly` và `cmd/worker`, route `/v1/kn/*`, `kn-menu.json`, `apps/web-ops/src/app/kn`, màn KN của field-app, client `kn-web`, vai `kn_*`, realm `agrigreen-kn-admin` | cả hai | Sau gỡ: CI nền tảng xanh, HTX/MKG không đổi hành vi; schema và dữ liệu KN cũ giữ chỉ đọc, không DROP |

Bước 1 và bước 2 chạy song song. Bước 3 chỉ chạy thật khi bước 1 qua cổng hoặc gần qua (công cụ ETL viết được trước). Không cutover trước khi qua cổng độ phủ.

## Hợp đồng giữ cho pha kết nối

Nền tảng giữ các hợp đồng feed sau (thư mục `contracts/integration/feeds/`, qua cổng tích hợp ADR-0033) để APP_KN liên thông sau này bằng adapter, không đọc bảng nội bộ:

| Feed | Nguồn phía nền tảng | Dùng cho APP_KN |
|---|---|---|
| `cooperative_directory` v1 | `pf/party`, `pf/cooperative` (HTX) | APP_KN đọc danh mục HTX khi bật liên thông; hiện APP_KN tự quản HTX (D1) |
| `plot_directory` v1 | `pf/cooperative` (HTX) | APP_KN đọc thửa HTX khi bật liên thông |
| `admin_units` v1 | `pf/masterdata` | APP_KN đọc ĐVHC chuẩn thay bản tự nhập khi bật liên thông |
| `price_board_daily` v1 | `pf/price` (KN, sẽ gỡ) | Tên và lược đồ feed giữ cho website Khuyến nông; sau cutover nguồn chuyển sang bảng giá công bố của APP_KN (`cap/price`) phát qua lớp tích hợp. Feed đang `proposed`, chờ A2 |

Ba feed còn ở trạng thái `proposed` theo ADR-0033; ADR này không đổi trạng thái, lược đồ hay quyền của chúng, chỉ cam kết không gỡ chúng cùng phần KN. Bước 4 không được xóa các tệp hợp đồng này.

## Hệ quả

**Phía nền tảng (`Agrigreen_demo_v2`):**
- Không nhận gói tính năng KN mới; các gói tách KN theo ADR-0055/PLAN-KN-STANDALONE (#261–#284, #306, #308…) hoãn, không gộp.
- Làm bước 2 (support-requests, `gps_log` về HTX; cắt nguồn KN khỏi `pf/metrics`), xuất dữ liệu cho bước 3, đặt KN chỉ đọc và gỡ KN ở bước 4.
- Các cổng ranh giới hiện có (`knboundary`, archlint) vẫn chạy tới khi gỡ xong; baseline chỉ giảm.
- Giữ bốn hợp đồng feed ở trên và schema/dữ liệu KN cũ chỉ đọc.

**Phía APP_KN (`APP_KN_v1.0`):**
- Cập nhật KHTC lên v2.3 (gỡ FN-TI-02.11 nguồn ngoài), lấp các FN Lệch/Thiếu của BRD 2.0.
- Dựng triển khai có Keycloak realm `kn`, bỏ `devaccess`; database/vai DB không trùng tên với nền tảng.
- Viết lại màn KN của field-app gọi API APP_KN; viết công cụ ETL; về sau viết adapter đọc các feed ở trên.

**Rủi ro:**
- **Dữ liệu:** hai lược đồ khác hẳn; sổ chỉ ghi thêm và chuỗi băm ở cả hai phía; vault phải mã hóa lại. Giảm bằng diễn tập trên bản chụp, đếm theo Trung tâm, giữ dữ liệu nền tảng chỉ đọc để đối chiếu. Chưa đếm được khối lượng dữ liệu KN thật, nên chưa ước được thời gian ETL.
- **Danh tính:** tài khoản, vai, Nút, xã phụ trách đang ở `pf/accounts`. Giảm bằng realm `kn` cùng instance Keycloak (giữ `sub`) và danh sách người dùng chuyển kèm báo cáo đối chiếu.
- **Độ phủ:** APP_KN đang thấp hơn và chưa có di động/ngoại tuyến; giảm bằng cổng độ phủ ở bước 1.
- **Hai nơi cùng sửa KN:** giảm bằng việc dừng tính năng KN trên nền tảng.

## Trạng thái

- Bước 0: ADR này (gói #311). Các bước 1–4 chưa bắt đầu ở thời điểm viết; gói phía nền tảng đã mở: #312 vá Go 1.26.11 (P8, không thuộc PA1), #313 chuyển support-requests và `gps_log` về HTX, #314 cắt nguồn KN khỏi `pf/metrics`.
- Việc phía APP_KN (bước 1, 3) mở trên repo APP_KN theo quy trình của repo đó.
