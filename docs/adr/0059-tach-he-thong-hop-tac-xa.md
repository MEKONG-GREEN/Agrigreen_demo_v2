# ADR-0059: Tách hệ thống Hợp tác xã

**Trạng thái:** Đề xuất — phạm vi và sáu quyết định Q-HTX-01…06 đã chốt với người dùng; chờ BRD HTX vào repo để lập bảng bề mặt. **Ngày:** 11/11/2026 (HTX-STANDALONE-0-GOV). **Nguồn:** trả lời của người dùng ngày 11/11/2026.

**Liên quan:** [ADR-0055](0055-tach-he-thong-khuyen-nong.md) (mô hình tách KN, áp dụng lại ở đây), [ADR-0052/D2](0052-dong-so-huu-mkg-htx-neo-mst.md), [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), [ADR-0034](0034-luong-song-phuong.md), [thiết kế `pf/cooperative`](../design/cooperative.md).

Số 0058 dành cho ADR "KN độc lập theo BRD 2.0" của gói KN-STANDALONE-0-GOV ([#261](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/261)); ADR này lấy số trống kế tiếp.

## Bối cảnh

Hệ Khuyến nông đã tách thành hệ độc lập về logic (ADR-0055): namespace `internal/kn/*`, cổng `tools/knboundary` với baseline chỉ giảm, phần riêng KN đăng ký vào registry của nền tảng, dữ liệu hệ khác chỉ đọc qua `_pub`/data product. Hệ Hợp tác xã (HTX) chưa có ranh giới tương ứng:

- Mã HTX nằm cạnh nền tảng: `internal/platform/{cooperative,cropcycle,mechanization,mrv}`. Không có cổng nào chặn hệ khác gọi vào hoặc HTX gọi ra.
- `pf/cooperative` còn chứa phần riêng KN (`member/app/kn_household.go`, `member/store/kn_profile.go`, `plot/app/kn.go`). Các gói KN-STANDALONE-5/6/7 đang gỡ dần từ phía KN.
- `app/HTX/app.yaml` phụ thuộc `pf/extension` (yêu cầu hỗ trợ gửi khuyến nông) — một lời gọi trong tiến trình sang hệ KN.
- Màn HTX nằm trong vỏ chung `apps/web-ops` (`src/htx`, `/htx/**`), dùng chung client Keycloak `agrigreen-web-ops`.
- Căn cứ nghiệp vụ trong repo chỉ có prototype `App Hợp tác xã_v5.1.html` và 48 story G2 ([g2-acceptance](../uat/g2-acceptance.md)); BRD HTX chưa có trong `docs/brd/`.

## Trả lời của người dùng (11/11/2026)

| Câu hỏi | Quyết định đã chốt |
|---|---|
| Q-HTX-01 Phạm vi | HTX là hệ **độc lập về logic**, theo mô hình KN. Không yêu cầu binary/cụm DB riêng. |
| Q-HTX-02 Tenant | **Giữ tenant lưu ký** theo ADR-0052 (MKG + HTX đồng sở hữu, neo MST). Không chuyển dữ liệu sang tenant mới; không có gói tương đương KNSPLIT-4b. |
| Q-HTX-03 Căn cứ | **Có BRD HTX.** BRD là chuẩn cho bảng bề mặt và nghiệm thu; prototype v5.1 và story G2 chỉ để đối chiếu. |
| Q-HTX-04 MRV, cơ giới hóa | Thu thập MRV và máy móc **thuộc hệ HTX**. |
| Q-HTX-05 Web | **Tách** frontend riêng `apps/web-htx`; không dừng ở cổng chặn trong `apps/web-ops`. |
| Q-HTX-06 Người làm | Làn `lane:htx` giao **Claude Code**. |

Không hỏi lại các lựa chọn này.

## Quyết định

### 1. Hệ HTX độc lập về logic

- Namespace đích: `internal/htx/{cooperative,cropcycle,mechanization,mrv}`. Giữ tên module `pf/*`, schema (`pf_coop`, `pf_cropcycle`, …), vai DB, tên sổ và chuỗi băm. Không đổi SQL migration đã áp dụng; chuyển namespace không có migration.
- HTX dùng năng lực nền tảng qua port/registry do nền tảng định nghĩa: accounts, party (cây Nút), geo, forms, notify, reports, masterdata, sync, evidence, content, workflow, kho định danh.
- HTX không import module KN (`internal/kn/*`) hay MKG, kể cả port. Nền tảng và hệ khác không import HTX; phần riêng HTX đăng ký vào điểm mở rộng chung.
- Dùng chung binary `api-platform`/`worker`, cụm `pg-pf` và realm vẫn hợp lệ.

### 2. Hạ tầng HTX và hệ ngoài

| Nhóm | Thành phần |
|---|---|
| Module HTX | `pf/cooperative`, `pf/cropcycle`, `pf/mechanization`, `pf/mrv` |
| Hạ tầng dùng chung | `pf/accounts`, `pf/party`, `pf/geo`, `pf/forms`, `pf/notify`, `pf/reports`, `pf/masterdata`, `pf/sync`, `pf/evidence`, `pf/content`, `pf/workflow`, `pf/export`, `pf/metrics`, kho định danh, Keycloak, outbox |
| Hệ ngoài | KN (`internal/kn/*`), MKG (`mkg/*`), Public API, bên thứ ba |

`pf/mechanization` hiện phục vụ cả bề mặt CGH của Cục và route `/v1/kn/*`. Theo Q-HTX-04 module thuộc HTX; bề mặt của hệ khác đọc qua hợp đồng `_pub`/data product, route `/v1/kn/*` đi theo các gói KN-STANDALONE đang mở (không gỡ trùng ở phía HTX).

### 3. Ranh giới dữ liệu

- Tenant và sở hữu giữ nguyên ADR-0052: dữ liệu ở tenant lưu ký, phạm vi theo Nút (`p_tenant` + `p_scope`).
- HTX → hệ khác: chỉ qua hợp đồng `contracts/pubview/pf_cooperative_pub.*` và data product, có `share_grant` hiệu lực với dòng chi tiết. Thêm hợp đồng mới thay vì cấp quyền đọc bảng nội bộ.
- HTX → KN (yêu cầu hỗ trợ): chuyển từ lời gọi port `pf/extension` sang sự kiện qua outbox; KN tự tạo nhiệm vụ ở phía mình. Không biến sự kiện thành lệnh ghi tự động vào dữ liệu KN ngoài hợp đồng đã khai.
- MKG ↔ HTX: giữ luồng song phương ADR-0034 và feed ADR-0033.
- Hệ khác không ghi dữ liệu HTX. Phần riêng KN còn trong `pf/cooperative` rời về KN hoặc bị xóa khi gói KN tương ứng gộp xong.

### 4. Cổng ranh giới

Cổng `htxboundary` (công cụ riêng hoặc cấu hình thứ hai của `tools/knboundary`, quyết ở gói HTX-0-BOUNDARY) với các quy tắc tối thiểu: `htx-import-kn`, `htx-import-mkg`, `import-into-htx`, `htx-sql-foreign`, `foreign-sql-htx`, `foreign-symbol-in-htx`. Baseline sinh một lần từ `dev`, sau đó **chỉ giảm**; nối `make gates` và CI `-base`. Kiểm thử âm DB: vai của hệ khác không có quyền trên schema nội bộ HTX.

### 5. Frontend và client

| Thành phần | Đích |
|---|---|
| Web | `apps/web-htx` (Next.js, cùng UI kit), client Keycloak `agrigreen-web-htx`; chuyển `src/htx` và `/htx/**` khỏi `apps/web-ops`, phiên HTX không vào được vỏ web-ops |
| Mobile | `apps/htx-app` giữ nguyên; bản build chỉ có màn HTX theo BRD |
| Realm | Dùng chung realm `agrigreen`; realm riêng chỉ khi vận hành cần |

### 6. Làn

Thêm `lane:htx` — chủ: Claude Code. Phạm vi ghi: `internal/htx/*`, module mới của HTX, `apps/web-htx`, `apps/htx-app`, `docs/htx/*`. Sửa `AGENTS.md` §1/§3 và `docs/process/two-tool-workflow.md` §2 là NGOẠI LỆ LÀN, do bên còn lại review.

### 7. Gói việc theo thứ tự

| Gói | Kết quả bắt buộc |
|---|---|
| HTX-STANDALONE-0-GOV | ADR này; làn `lane:htx`; BRD HTX vào `docs/brd/` |
| HTX-STANDALONE-0-AUDIT | Kiểm kê điểm dính Go/DB/HTTP/web/mobile/realm tại `docs/htx/tach-he-thong/kiem-ke.md`; không chuyển mã |
| HTX-STANDALONE-0-BOUNDARY | Cổng ranh giới + baseline ban đầu + kiểm thử âm DB |
| HTX-STANDALONE-1-NAMESPACE | Bốn module ở `internal/htx/*`; modgen/archlint/cổng ranh giới đạt; không migration |
| HTX-STANDALONE-2-REGISTRY | Menu, thông báo, nguồn báo cáo, vai/ngữ cảnh HTX đăng ký qua registry; phần riêng HTX rời module chung |
| HTX-STANDALONE-2-DATA | Yêu cầu hỗ trợ qua outbox; `htx-import-kn` = 0; hợp đồng `_pub` bổ sung cho bên đọc ngoài |
| HTX-STANDALONE-3-SURFACE | `docs/htx/brd-surface.yaml` ánh xạ bề mặt → FN của BRD; gỡ mục ngoài BRD, test âm 403/404 |
| HTX-STANDALONE-3-WEB | `apps/web-htx` + client; gỡ `/htx/**` khỏi web-ops |
| HTX-STANDALONE-3-APP | `htx-app` theo BRD; bù story còn thiếu |
| HTX-STANDALONE-4-PROFILE-CI | Profile Compose chỉ-HTX, seed, job CI `htx-standalone` e2e với backend thật |
| HTX-STANDALONE-4-UAT-DOCS | Tài liệu nghiệm thu theo BRD HTX |

Thứ tự: 0 → 1 → 2 → 3 → 4. Gói 2 chờ KN-STANDALONE-5-TT-DATA-MAY ([#306](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/306)), -6 ([#266](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/266)), -7 ([#267](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/267)) và quyết định chủ dữ liệu [#177](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/177), vì cùng đụng ranh giới KN ↔ HTX. Gói 1 đổi đường dẫn hàng loạt, làm khi ít nhánh đang mở.

## Hệ quả

- Không có di chuyển tenant, nên không có rủi ro sổ append-only và không cần runbook chuyển dữ liệu.
- Thêm một frontend phải build, kiểm và triển khai (`apps/web-htx`); UI kit phải dùng chung được giữa hai app web.
- `pf/mechanization` và `pf/mrv` thuộc HTX nghĩa là bề mặt CGH của Cục và đối chiếu MRV của KN trở thành bên đọc ngoài hệ, cần hợp đồng dữ liệu.
- Các ghép nối hiện hữu là việc phải gỡ, không phải ngoại lệ vĩnh viễn; baseline không được nới để làm xanh.

## Còn mở

- Vị trí BRD HTX (tệp nguồn, phiên bản) — cần trước HTX-STANDALONE-3-SURFACE.
- Một công cụ `htxboundary` riêng hay tổng quát hóa `knboundary` — quyết ở gói 0-BOUNDARY sau kiểm kê.
