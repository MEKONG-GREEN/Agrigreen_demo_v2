# ADR-0059: Tách hệ thống Hợp tác xã

**Trạng thái:** Đề xuất — phạm vi và sáu quyết định Q-HTX-01…06 đã chốt với người dùng; căn cứ nghiệp vụ là bộ BRD App HTX 11/2026 (mục "Căn cứ BRD"); ba điểm lệch giữa BRD và mã hiện có chờ người dùng chốt (mục "Còn mở"). **Ngày:** 11/11/2026 (HTX-STANDALONE-0-GOV). **Nguồn:** trả lời của người dùng ngày 11/11/2026.

**Liên quan:** [ADR-0055](0055-tach-he-thong-khuyen-nong.md) (mô hình tách KN, áp dụng lại ở đây), [ADR-0052/D2](0052-dong-so-huu-mkg-htx-neo-mst.md), [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), [ADR-0034](0034-luong-song-phuong.md), [thiết kế `pf/cooperative`](../design/cooperative.md).

Số 0058 dành cho ADR "KN độc lập theo BRD 2.0" của gói KN-STANDALONE-0-GOV ([#261](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/261)); ADR này lấy số trống kế tiếp.

## Bối cảnh

Hệ Khuyến nông đã tách thành hệ độc lập về logic (ADR-0055): namespace `internal/kn/*`, cổng `tools/knboundary` với baseline chỉ giảm, phần riêng KN đăng ký vào registry của nền tảng, dữ liệu hệ khác chỉ đọc qua `_pub`/data product. Hệ Hợp tác xã (HTX) chưa có ranh giới tương ứng:

- Mã HTX nằm cạnh nền tảng: `internal/platform/{cooperative,cropcycle,mechanization,mrv}`. Không có cổng nào chặn hệ khác gọi vào hoặc HTX gọi ra.
- `pf/cooperative` còn chứa phần riêng KN (`member/app/kn_household.go`, `member/store/kn_profile.go`, `plot/app/kn.go`). Các gói KN-STANDALONE-5/6/7 đang gỡ dần từ phía KN.
- `app/HTX/app.yaml` phụ thuộc `pf/extension` (yêu cầu hỗ trợ gửi khuyến nông) — một lời gọi trong tiến trình sang hệ KN.
- Màn HTX nằm trong vỏ chung `apps/web-ops` (`src/htx`, `/htx/**`), dùng chung client Keycloak `agrigreen-web-ops`.
- Căn cứ nghiệp vụ trong repo chỉ có prototype `App Hợp tác xã_v5.1.html` và 48 story G2 ([g2-acceptance](../uat/g2-acceptance.md)). Bộ BRD App HTX 11/2026 (mục "Căn cứ BRD") đổi mô hình vai trò và thêm bốn nhóm nghiệp vụ chưa có mã.

## Trả lời của người dùng (11/11/2026)

| Câu hỏi | Quyết định đã chốt |
|---|---|
| Q-HTX-01 Phạm vi | HTX là hệ **độc lập về logic**, theo mô hình KN. Không yêu cầu binary/cụm DB riêng. |
| Q-HTX-02 Tenant | **Giữ tenant lưu ký** theo ADR-0052 (MKG + HTX đồng sở hữu, neo MST). Không chuyển dữ liệu sang tenant mới; không có gói tương đương KNSPLIT-4b. |
| Q-HTX-03 Căn cứ | **Có BRD HTX** (bốn tài liệu, mục "Căn cứ BRD"). BRD là chuẩn cho bảng bề mặt và nghiệm thu; prototype v5.1 và story G2 chỉ để đối chiếu. |
| Q-HTX-04 MRV, cơ giới hóa | Thu thập MRV và máy móc **thuộc hệ HTX**. |
| Q-HTX-05 Web | **Tách** frontend riêng `apps/web-htx`; không dừng ở cổng chặn trong `apps/web-ops`. |
| Q-HTX-06 Người làm | Làn `lane:htx` giao **Claude Code**. |

Không hỏi lại các lựa chọn này.

## Căn cứ BRD

Bộ BRD App HTX nằm ở Drive, thư mục `1IHDoIPJr1cf93OlQEFnHcxwa_9Szo72F` (chưa chép vào `docs/brd/`). Khách hàng: Mekong Green và các HTX cung cấp rơm vùng ĐBSCL.

| Tài liệu | Phiên bản | Chức năng |
|---|---|---|
| Quản trị phân quyền, Hồ sơ HTX, Thống kê báo cáo | v2.0, 05/11/2026 (tệp `_v3.0`, CR-001) | FN-01…15: đăng nhập và chọn vai trò, ĐVHC, HTX, hộ xã viên, vai trò, phạm vi dữ liệu, tài khoản, liên kết và chia sẻ dữ liệu giữa HTX (FN-09), đặt lại mật khẩu qua OTP SMS, hệ số rơm, thống kê vụ ba cấp, lọc và xuất |
| Kế hoạch mùa vụ, Nhật ký sản xuất | Final.0 | FN-01…06: danh mục cây/giống/SOP, danh sách nông hộ, mở vụ và lịch thời vụ, kế hoạch mùa vụ, nhật ký canh tác (offline, ảnh GPS), sản lượng |
| Thời tiết & Thu gom phụ phẩm | Final | FN-TT-01…03 (thời tiết hôm nay, lịch dương/âm, kết nối API), FN-PP-01…05, 07, 08 (danh mục phụ phẩm, bảng giá, dự báo rơm, quy trình mẫu, phiếu thu gom rơm, phiếu cân & kiểm mẫu, báo cáo) |
| Vật tư, Kho & Máy móc cơ giới | v1.0, 03/11/2026 | FN-01…07: danh mục dùng chung, phiếu nhập, phiếu xuất, tồn và kiểm kê, danh sách máy, phiếu dịch vụ máy, báo cáo máy móc |

Tổng 38 chức năng. Các điểm chung của bộ BRD ràng buộc quyết định bên dưới:

- **Ba vai trò cố định:** Admin (Mekong Green), Đại diện HTX, Thành viên HTX; một tài khoản có thể kiêm hai vai và chọn vai sau đăng nhập. **Nông hộ chỉ là dữ liệu, không có tài khoản.**
- **Mỗi HTX là một vùng dữ liệu riêng**, các HTX ngang hàng. Xem dữ liệu HTX khác chỉ khi có yêu cầu chia sẻ đang hiệu lực (FN-09), chỉ đọc, thu hồi được bất cứ lúc nào; Admin không can thiệp. Quyền kiểm ở máy chủ theo mã bản ghi.
- **Ngoài phạm vi theo BRD:** tích hợp hệ nghiệp vụ bên thứ ba (kể cả Khuyến nông), giao dịch/công nợ, điều phối xe (TMS của MKG), cảnh báo/lời khuyên bằng AI. BRD không có MRV, vẽ ranh giới thửa bằng GPS, yêu cầu hỗ trợ gửi khuyến nông hay bề mặt CGH của Cục.
- **Phi chức năng:** offline bắt buộc cho nhật ký, phiếu thu gom, phiếu cân & kiểm mẫu; 100 CCU thí điểm (500 ở BRD mùa vụ); web từ 360px; Android 8+/iOS 13+; chỉ tiếng Việt; NĐ 13/2023.

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
- HTX → KN (yêu cầu hỗ trợ): BRD không có chức năng này và xếp tích hợp Khuyến nông ngoài phạm vi. Gỡ lời gọi port `pf/extension` và bề mặt "yêu cầu hỗ trợ" khỏi app HTX; nếu sau này cần lại thì đi qua outbox, không gọi trong tiến trình.
- HTX ↔ HTX (BRD FN-09): yêu cầu chia sẻ do Đại diện HTX gửi/duyệt/thu hồi, hiện thực trên cơ chế `share_grant` + `p_scope` sẵn có; thu hồi phải mất quyền ngay.
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

Hai nhóm: **tách** (theo mô hình KN) và **dựng** (chức năng BRD chưa có mã).

| Đợt | Gói | Kết quả bắt buộc |
|---|---|---|
| 0 | HTX-STANDALONE-0-GOV | ADR này; làn `lane:htx`; chốt ba điểm ở mục "Còn mở" |
| 0 | HTX-STANDALONE-0-AUDIT | Kiểm kê điểm dính Go/DB/HTTP/web/mobile/realm và bảng đối chiếu 38 FN ↔ mã hiện có (đạt / một phần / chưa có / thừa) tại `docs/htx/`; không chuyển mã |
| 0 | HTX-STANDALONE-0-BOUNDARY | Cổng ranh giới + baseline ban đầu + kiểm thử âm DB |
| 1 | HTX-STANDALONE-1-NAMESPACE | Module HTX ở `internal/htx/*`; modgen/archlint/cổng ranh giới đạt; không migration |
| 1 | HTX-STANDALONE-1-ROLES | Ba vai trò BRD và chọn vai khi kiêm nhiệm (FN-01, 05, 06); phạm vi theo HTX kiểm ở máy chủ; bỏ đăng nhập của nông hộ |
| 2 | HTX-STANDALONE-2-REGISTRY | Menu, thông báo, nguồn báo cáo, vai/ngữ cảnh HTX đăng ký qua registry; phần riêng HTX rời module chung |
| 2 | HTX-STANDALONE-2-SHARE | Liên kết và chia sẻ dữ liệu giữa HTX (FN-09) trên `share_grant` |
| 2 | HTX-STANDALONE-2-SURFACE | `docs/htx/brd-surface.yaml`; gỡ bề mặt ngoài BRD (yêu cầu hỗ trợ KN, tài khoản nông dân, …), test âm 403/404; `htx-import-kn` = 0 |
| 3 | HTX-BRD-PROFILE | Hồ sơ HTX, hộ xã viên (diện tích nhập tay), tài khoản, OTP SMS, hệ số rơm (FN-02…04, 07, 08, 10, 11) |
| 3 | HTX-BRD-SEASON | Đối chiếu và bù Kế hoạch mùa vụ, Nhật ký sản xuất (6 FN) trên `pf/cropcycle` |
| 3 | HTX-BRD-INVENTORY | Module mới vật tư và kho: danh mục, phiếu nhập/xuất, tồn, kiểm kê (FN-01…04 BRD Vật tư) |
| 3 | HTX-BRD-MACHINE | Danh sách máy, phiếu dịch vụ máy, báo cáo máy móc (FN-05…07 BRD Vật tư) |
| 3 | HTX-BRD-WEATHER | Thời tiết, lịch âm, kết nối API (FN-TT-01…03); fail-closed khi thiếu cấu hình nhà cung cấp |
| 3 | HTX-BRD-COLLECT | Danh mục phụ phẩm, bảng giá, dự báo rơm, quy trình mẫu, phiếu thu gom, phiếu cân & kiểm mẫu, báo cáo (FN-PP-*), offline |
| 3 | HTX-BRD-STATS | Thống kê vụ ba cấp, lọc và xuất (FN-12…15) |
| 4 | HTX-STANDALONE-4-WEB | `apps/web-htx` + client; gỡ `/htx/**` khỏi web-ops |
| 4 | HTX-STANDALONE-4-APP | `htx-app` chỉ có màn theo BRD |
| 5 | HTX-STANDALONE-5-PROFILE-CI | Profile Compose chỉ-HTX, seed, job CI `htx-standalone` e2e với backend thật |
| 5 | HTX-STANDALONE-5-UAT-DOCS | Tài liệu nghiệm thu theo 38 FN |

Thứ tự: 0 → 1 → 2 → 3 → 4 → 5; các gói đợt 3 chạy song song, mỗi gói có backend, web và mobile của mình. Màn mới của đợt 3 viết thẳng vào `apps/web-htx` nếu gói 4-WEB dựng khung trước; thứ tự này quyết sau kiểm kê. Gói đợt 2 chờ KN-STANDALONE-5-TT-DATA-MAY ([#306](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/306)), -6 ([#266](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/266)), -7 ([#267](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/267)) và quyết định chủ dữ liệu [#177](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/177), vì cùng đụng ranh giới KN ↔ HTX. Gói 1-NAMESPACE đổi đường dẫn hàng loạt, làm khi ít nhánh đang mở.

## Hệ quả

- Không có di chuyển tenant, nên không có rủi ro sổ append-only và không cần runbook chuyển dữ liệu.
- Phần lớn khối lượng là **dựng mới** (vật tư/kho, máy và phiếu dịch vụ, thời tiết, thu gom phụ phẩm, chia sẻ giữa HTX), không phải tách; mô hình vai trò đổi từ `coop_manager`/`farmer` sang ba vai BRD.
- Thêm một frontend phải build, kiểm và triển khai (`apps/web-htx`); UI kit phải dùng chung được giữa hai app web.
- `pf/mechanization` và `pf/mrv` thuộc HTX nghĩa là bề mặt CGH của Cục và đối chiếu MRV của KN trở thành bên đọc ngoài hệ, cần hợp đồng dữ liệu.
- Các ghép nối hiện hữu là việc phải gỡ, không phải ngoại lệ vĩnh viễn; baseline không được nới để làm xanh.

## Còn mở

Ba điểm BRD lệch với mã hiện có hoặc với trả lời 11/11/2026; chốt trong gói 0-GOV, trước khi lập bảng bề mặt:

1. **MRV và cơ giới hóa.** Q-HTX-04 xếp `pf/mrv`, `pf/mechanization` vào hệ HTX, nhưng BRD không có MRV hay bề mặt CGH; "máy móc" của BRD là danh sách máy và phiếu dịch vụ của từng HTX — nghiệp vụ khác với `pf/mechanization`. Lựa chọn: (a) hai module về namespace HTX nhưng bề mặt nằm ngoài app HTX; (b) để ngoài hệ HTX, máy móc BRD là module mới.
2. **Tài khoản nông dân, thửa GPS, yêu cầu hỗ trợ.** Mã G2 có đăng nhập cho nông hộ, vẽ ranh giới thửa và yêu cầu hỗ trợ gửi KN; BRD không có. Mặc định của ADR này: gỡ khỏi bề mặt app HTX, giữ dữ liệu và mã phía nền tảng (không DROP), như tiêu chí gói gỡ của KN-STANDALONE.
3. **Admin là Mekong Green.** BRD đặt MKG làm Admin của app HTX (xem mọi HTX, thiết lập danh mục). Cần xác nhận Admin là vai trong tenant lưu ký (khớp ADR-0052) chứ không phải tài khoản tenant MKG đọc qua cổng tích hợp.

Ngoài ra: một công cụ `htxboundary` riêng hay tổng quát hóa `knboundary` — quyết ở gói 0-BOUNDARY sau kiểm kê.
