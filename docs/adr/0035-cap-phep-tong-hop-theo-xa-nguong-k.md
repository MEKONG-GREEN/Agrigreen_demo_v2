# ADR-0035: Loại cấp phép tổng hợp theo xã có ngưỡng k — tập dữ liệu dựng sẵn, quyền ở mức tập

**Trạng thái:** Đề xuất — chờ MKG ký (A7), k `[KÝ]` (K-8) · **Ngày:** 04/10/2026 (gói W0.2) · **Phụ lục B v4.1:** mục 17 · **Sửa đổi:** [ADR-0022](0022-public-api-ben-thu-ba.md) §9 (k tính theo yêu cầu → tính một lần theo phiên bản) · **Thành phần:** `pf/party` (cấp phép v2), `pf/apiclient` (kho feed, tập dữ liệu), `pf/cropcycle` (`dataproduct.go`), job `r_agg`, `contracts/integration/`, `contracts/dataproduct/` · **Liên quan:** [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), [ADR-0031](0031-vai-tong-hop-r-agg.md), [ADR-0032](0032-don-vi-hanh-chinh-co-hieu-luc-va-ke-thua.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), kiến trúc v4.1 §38.3, R-INT-05, R-DB-11

## Bối cảnh

Hiện trạng:
- Sản phẩm dữ liệu `collection_volume` (`contracts/dataproduct/collection_volume.json`): `aggregation: { k_param: publicdata.k_min, k_default: 10, unit: household, suppression: omit }`. Cấp quyền loại `dp_<sản phẩm>` (ADR-0020/0022).
- `cropcycle/app/dataproduct.go` tính **theo mỗi yêu cầu**, trong **một** tenant bên cấp, ẩn nhóm dưới k. `api-public` đọc k của bên cấp tại thời điểm (`kFor`).
- Không có tổng hợp vượt đơn vị (ADR-0031 thêm `r_agg`).

v4.1:
- §38.3: MKG cần **tiến độ gặt, diện tích theo xã**, vài nghìn dòng/ngày, độ tươi ≤ 15 phút trong vụ — "cấp phép, tổng hợp, ngưỡng k".
- R-INT-05: dữ liệu cấp phép là **tập dữ liệu dựng sẵn theo phiên bản**, quyền ở mức tập và phạm vi, không lọc từng hàng cho từng đối tác.
- §39 rủi ro 2 và 5: tính lại cho từng đối tác và lọc từng hàng là hai rủi ro hiệu năng thật.
- Ngưỡng k là `[KÝ]` (K-8, C-4, Q5-3); đang dùng tạm 10.

## Quyết định

### 1. Loại cấp phép `aggregate`

Cấp phép v2 (ADR-0030 §3) thêm `grant_class = aggregate` cạnh `intra_platform`, `partner`:

| Trường | Ý nghĩa với `aggregate` |
|---|---|
| `dataset` | Tên tập dữ liệu trong `contracts/integration/` (vd. `harvest_progress_by_commune`) |
| `scope_admin_units` | Danh sách mã tỉnh hoặc xã **tại phiên bản** (ADR-0032); rỗng = mọi địa bàn trong tập |
| `purpose_code`, `basis_ref`, hạn, bốn mắt, thu hồi | Như cấp phép v2 |

Hai lớp quyền:
- **Đóng góp:** mỗi đơn vị chủ dữ liệu cấp `aggregate` cho **đơn vị vận hành** (ADR-0022 §3) — cho phép dữ liệu của mình được gộp vào tập. ĐTK 1 (H-1 (a)) chỉ có một bản đóng góp từ tenant chương trình.
- **Phát hành:** đơn vị vận hành cấp `aggregate` cho đối tác (MKG, bên thứ ba) trên tập và phạm vi.

Job chỉ gộp dữ liệu của đơn vị có đóng góp đang hiệu lực **tại lúc dựng phiên bản**. Thu hồi đóng góp có hiệu lực từ phiên bản kế tiếp; phiên bản đã phát không bị sửa (RB-1), nhưng ngừng phục vụ ảnh chụp có dữ liệu của đơn vị đã thu hồi sau 30 ngày (hết hạn feed).

### 2. Tập dữ liệu dựng sẵn

- Hợp đồng tập dữ liệu khai: chiều (`commune_code` tại phiên bản đơn vị hành chính, `season_ref`, ngày), chỉ số, **đơn vị đếm của k** (`household` hoặc `plot` hoặc `cooperative`), quy tắc ẩn, chu kỳ dựng.
- Job dựng (khai trong danh sách job `r_agg`, ADR-0031) chạy theo chu kỳ (trong vụ: 15 phút), ghi **một phiên bản** vào kho feed (ADR-0033 §2): `dataset_version`, `k_used`, `admin_unit_as_of`, `contributors` (số đơn vị, không tên), tổng kiểm.
- Cổng kiểm quyền **một lần mỗi yêu cầu** ở mức (đối tác, tập, phạm vi); lọc phạm vi là chọn phân vùng theo tỉnh/xã, không đánh giá từng hàng.

### 3. Quy tắc k

- Một ô (xã × kỳ) chỉ được phát khi số **đơn vị đếm khác nhau** ≥ k. Ô dưới k bị **bỏ** (`suppression: omit`), cộng vào dòng `withheld` cấp tỉnh **chỉ khi** dòng đó cũng ≥ k.
- Ẩn ô bù (ADR-0031): khi phát cả tổng tỉnh và các xã, ẩn thêm ít nhất một ô để không suy ra ô đã ẩn bằng phép trừ.
- Chống suy ra qua chênh lệch phiên bản: ô đã ẩn trong một mùa vụ giữ trạng thái ẩn cho tới khi vượt **k + 2**, không bật/tắt qua lại quanh ngưỡng giữa các phiên bản 15 phút.
- `k` của job **không nhỏ hơn** k đã ký; job từ chối chạy nếu tham số thấp hơn. Tạm dùng `publicdata.k_min = 10`. **Không bật production** trước khi K-8 được ký.

### 4. Đi kèm sản phẩm dữ liệu cũ

- `/v1/data/collection-volumes` (ADR-0022) giữ **một kỳ** song song, gắn `Deprecation`; sau đó chỉ còn feed `collection_volume_by_commune` dựng sẵn.
- `dp_*` vẫn là tên loại cho sản phẩm không tổng hợp (nếu có); sản phẩm tổng hợp chuyển sang `aggregate`.

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Tính theo yêu cầu, lọc k mỗi lần gọi (hiện trạng) | §39 rủi ro 2: chi phí × số đối tác; kết quả khác nhau giữa hai lời gọi gần nhau, khó đối soát |
| Lọc từng hàng theo đối tác bằng RLS | §39 rủi ro 5; R-INT-05 cấm cho dữ liệu cấp phép |
| Mỗi đơn vị tự tổng hợp rồi cộng | k phải áp sau khi gộp mọi đơn vị (ADR-0031) |
| Gộp ô dưới k lên huyện | Cấp huyện đã bỏ sau sắp xếp 2025; chỉ còn tỉnh → xã |
| Nhiễu ngẫu nhiên (differential privacy) | Đối tác cần số dùng cho điều phối thật; chưa có yêu cầu, khó giải thích cho pháp chế. Xem lại khi có đối tác thứ ba |

## Hệ quả

- MKG nhận tiến độ gặt theo xã qua feed giống mọi đối tác; không đọc dữ liệu cấp thửa.
- Xã ít hộ (dưới k) không xuất hiện; điều phối MKG phải dùng ước tính riêng ở những xã đó (`uncertainty` trong kho quan sát, ADR-0036).
- Mỗi phiên bản ghi `k_used` và phiên bản đơn vị hành chính, nên đối soát và kiểm toán tái lập được.
- Cấp phép thêm một lớp (`aggregate`); rà soát quyền định kỳ (R-SEC-04) bao gồm.

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| W4.1 | Cấp phép v2 có `grant_class = aggregate`, `dataset`, `scope_admin_units` — W4.1 (pf 000120) mới làm lớp `intra_platform`/`partner`; lớp `aggregate` chưa giao gói (H-19) | pf 000120 (000098 cũ bỏ) |
| W5.1 | Hợp đồng `harvest_progress_by_commune`, `collection_volume_by_commune` | — |
| W5.3 | Job dựng tập dữ liệu, phiên bản, `k_used`, ẩn ô bù, trễ ẩn k + 2 | pf 000126–000127 |
| W6.4 | Job khai trong danh sách `r_agg` khi vượt đơn vị; bật production chờ K-8 | pf 000129 (000109 cũ bỏ) |

**W5.1 (08/10/2026):** `harvest_progress_by_commune`, `collection_volume_by_commune` có `aggregation` (`k_param: publicdata.k_min`, `k_default: 10` tạm, `k_status`, `unit: household`, `suppression: omit`, `complementary_suppression: true`, `hysteresis: 2`), `pending: [A7, C-4, K-8]`; phong bì ghi `dataset.k_used`, `contractcheck` bắt mẫu `k_used < k_default`. k vẫn là tham số — ký K-8 không đổi hợp đồng.

**W5.3 (09/10/2026):** job `apiclient_feed_build` (cmd/worker, `pf/apiclient`) dựng `harvest_progress_by_commune`, `collection_volume_by_commune` từ ô THÔ của `pf/cropcycle` (port `apiclient/port.AggregateSource`, mã nông hộ chỉ để đếm phân biệt trong bộ nhớ) của mọi tenant đóng góp, rồi áp §3 một lần trong `apiclient/domain.ApplyK`: ẩn sơ cấp < k; dòng `province_withheld` = HỢP đơn vị của các ô ẩn, phát khi ≥ k; ẩn ô bù khi withheld chỉ gom một ô (ẩn thêm ô phát nhỏ nhất, không còn ô thì ẩn withheld); trễ ẩn: ô đã ẩn (bảng `dataset_cell_state`) chỉ phát lại khi ≥ k + `hysteresis`. k = `publicdata.k_min` của tenant đơn vị vận hành, chưa đặt → `k_default`; nhỏ hơn `k_default` → từ chối (`integration.k_below_floor`). Mỗi phiên bản (`pf_apiclient.dataset_version`, bất biến, pf 000127) ghi `k_used`, `hysteresis`, `contributors` (số), `admin_unit_as_of`, tổng kiểm = SHA-256 ảnh chụp NDJSON. **Ghi lệch:** lớp quyền "Đóng góp" (§1) chưa có ở `pf/party` — danh sách đóng góp tạm lấy từ `AG_INTEGRATION_CONTRIBUTORS`; lớp "Phát hành" kiểm bằng scope `feed:<tên>:read` của đăng ký sản phẩm (bốn mắt) thay cho cấp phép `aggregate`; ô của xã không còn hiệu lực tại ngày dựng bị bỏ (cuộn theo kế thừa ở W6.4); `/v1/data/collection-volumes` chưa gắn `Deprecation` (W5.5).

## Liên quan

- Kiến trúc v4.1: §4 (van dữ liệu công, k), §22.2 (R-DB-11), §38.3, §39, R-INT-05, Phụ lục B mục 17, Phụ lục C C-4.
- Kế hoạch: §4 K-8. Câu hỏi: [cau-hoi-ton-dong.md](../cau-hoi-ton-dong.md) Q5-3, H-1, H-14.
- [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md), [ADR-0022](0022-public-api-ben-thu-ba.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), [ADR-0031](0031-vai-tong-hop-r-agg.md), [ADR-0032](0032-don-vi-hanh-chinh-co-hieu-luc-va-ke-thua.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), [ADR-0036](0036-engine-ba-che-do-vung-noi-bo-optcore.md).
