# ADR-0036: Engine điều phối ba chế độ ở vùng nội bộ MKG; lõi `lib/optcore` dùng chung với mô phỏng nền tảng

**Trạng thái:** Đề xuất — chờ MKG ký (A7); W8.3 chờ tài liệu vùng nội bộ MKG v1.3 (H-8, H-9) · **Ngày:** 04/10/2026 (gói W0.2) · **Phụ lục B v4.1:** mục 19 (mục 20 OR-Tools theo [ADR-0016](0016-giay-phep-python.md)) · **Xung đột:** X-07 · **Bổ sung:** [ADR-0016](0016-giay-phep-python.md) (phạm vi giấy phép mở sang `lib/optcore` và engine nội bộ) · **Thành phần:** `optimizer/` (`agrigreen_optimizer`), `lib/optcore` (mới), `pf/simulation`, `pf/waterway`, engine nội bộ (mới, vùng mkg), kho quan sát (mới, cụm mkg) · **Liên quan:** [ADR-0001](0001-go-ngon-ngu-dich-vu.md), [ADR-0008](0008-phan-vung-mang.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), [ADR-0035](0035-cap-phep-tong-hop-theo-xa-nguong-k.md), kiến trúc v4.1 §6, §27.2, §41.4

## Bối cảnh

Hiện trạng:
- `optimizer/` (Python 3.12, gói `agrigreen_optimizer`: `engine.py`, `flow.py`, `select_hubs.py`, `sensitivity.py`, `calendar.py`, `snapshot.py`, `worker.py`, `consumer.py`) là ngoại lệ ngôn ngữ duy nhất (ADR-0001). Đơn vị triển khai `optimizer` ở **vùng nền tảng**, phục vụ `pf/simulation`: nhận việc qua RabbitMQ, đọc ảnh chụp đầu vào bất biến qua Internal API (mTLS), trả kết quả có băm. HiGHS (`highspy`) cho chế độ chọn hub. Giấy phép theo ADR-0016.
- Vùng mkg chưa có engine, kho quan sát, `mkg/workforce`.

v4.1 §27.2:
- Engine điều phối **một lõi `lib/optcore`, ba chế độ** — mô phỏng (chọn hub, điểm gom, tồn đệm), kế hoạch cuộn 7–14 ngày, điều phối có lập lại kế hoạch — **cả ba ở vùng nội bộ**; dịch vụ Python riêng, không giữ dữ liệu nghiệp vụ.
- Ba tầng: T1 dòng chảy toàn mạng (quy hoạch nguyên, HiGHS), T2a ghe nhỏ bến → điểm gom, T2b ghe lớn điểm gom → nhà máy, T3 phân công đội. Vùng đóng băng; mọi kế hoạch là đề xuất; tự duyệt trong năm điều kiện.
- Đầu vào: quan sát trạng thái ngoài (`kind, source, as_of, max_age, uncertainty`) + tham số chính sách. Quan sát từ nền tảng đến qua feed cổng tích hợp (§41.4).
- `lib/optcore` là **thư viện, không phải module có dữ liệu**, dùng bởi `dom/hubsim` (nền tảng) và engine nội bộ — mã chung, dữ liệu riêng.

X-07 không phải xung đột thật: `dom/hubsim` của v4.1 chính là `pf/simulation` (+ `pf/waterway`) của repo, ở App SIM, vùng nền tảng. Engine nội bộ là **triển khai thứ hai**.

## Quyết định

### 1. `lib/optcore` — thư viện thuần

- Gói Python ở `lib/optcore/` (pyproject riêng), tách từ `agrigreen_optimizer`: mô hình mạng lưới, hàm chi phí, mô hình cung, bộ giải (bọc HiGHS), phân tích độ nhạy.
- **Không** I/O: không DB, không broker, không HTTP, không đọc biến môi trường, không đồng hồ hệ thống (thời điểm là tham số). Đầu vào, đầu ra là cấu trúc dữ liệu kiểu hóa; tất định với cùng đầu vào và hạt giống.
- Tách **không đổi hành vi**: golden test hiện có của optimizer giữ nguyên byte đầu ra (W8.1). `agrigreen_optimizer` thành lớp vỏ: nhận việc, đọc ảnh chụp, gọi `lib/optcore`, ghi kết quả.
- Phiên bản ngữ nghĩa; mỗi kết quả ghi phiên bản `lib/optcore` đã dùng (tái lập).

### 2. Hai triển khai, dữ liệu riêng (R-MOD-10)

| | `optimizer` (nền tảng) | Engine nội bộ (vùng mkg) |
|---|---|---|
| Phục vụ | `pf/simulation`, `pf/waterway` — App SIM, kịch bản của chương trình | Hệ vận hành MKG |
| Chế độ | Mô phỏng (như hiện nay) | Mô phỏng mạng MKG, kế hoạch cuộn 7–14 ngày, điều phối lập lại kế hoạch; T1/T2a/T2b/T3 |
| Đầu vào | Ảnh chụp bất biến từ `pf/simulation` | Kho quan sát + tham số chính sách của module mkg |
| Đầu ra | Kết quả mô phỏng vào `pf_simulation` | **Đề xuất** kế hoạch vào `mkg/logistics`, `mkg/fieldops` qua API nội bộ; người duyệt hoặc tự duyệt theo năm điều kiện |
| Broker | vhost `/` (như hiện nay) | vhost `mkg.internal` (ADR-0018) |
| Cụm DB | Không giữ dữ liệu nghiệp vụ | Không giữ dữ liệu nghiệp vụ; kho quan sát thuộc một module mkg |

- Hai triển khai **không** gọi nhau, không chung hàng đợi, không chung ảnh chụp (R-INT-02). Nền tảng không gọi engine MKG; engine MKG không gọi `optimizer`.
- Chế độ mô phỏng của MKG chạy ở vùng nội bộ trên dữ liệu MKG; mô phỏng của App SIM giữ ở nền tảng. Cùng mã, khác dữ liệu.
- Ngôn ngữ: vẫn **một** ngoại lệ Python (ADR-0001), hai đơn vị triển khai.

### 3. Kho quan sát

- Bảng chỉ ghi thêm ở cụm mkg, chủ là `mkg/fieldops` (hoặc module do v1.3 chỉ định): `kind, source (api | manual | sensor), as_of, max_age, uncertainty, payload`.
- Nạp từ bộ điều hợp feed của cổng tích hợp (ADR-0033 §5, W5.5) với `source = api`, `as_of` lấy từ nền tảng. Dữ liệu tổng hợp theo xã có ô bị ẩn dưới k (ADR-0035) → quan sát thiếu, engine dùng ước tính có `uncertainty`.
- Nền tảng ngừng → quan sát cũ dần; quy tắc `max_age` tăng tồn đệm, kế hoạch mang cờ dữ liệu cũ (§41.4). Engine không bao giờ chờ nền tảng.

### 4. Giấy phép

ADR-0016 áp cho `lib/optcore` và engine nội bộ. Thêm bộ giải mới (OR-Tools cho T3, Apache-2.0 — Phụ lục B mục 20) đi theo quy trình của ADR-0016: kiểm `pip-licenses` cả thư viện nhị phân gói kèm, cập nhật `allowed-licenses.txt` nếu cần, ghi vào ADR-0016.

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Engine nội bộ gọi `optimizer` của nền tảng | Gọi đồng bộ xuyên vùng trong đường găng (R-INT-02); dữ liệu MKG đi vào vùng nền tảng |
| Chuyển `optimizer` hiện có sang vùng mkg | App SIM và chương trình mất mô phỏng; dữ liệu chương trình phải sang vùng nội bộ |
| Sao chép mã optimizer thành hai bản | Hai bản lệch nhau; sửa lỗi bộ giải hai lần |
| Viết engine nội bộ bằng Go | Bộ giải và hệ sinh thái tối ưu ở Python; thêm ngôn ngữ không giảm ngoại lệ |
| Engine giữ dữ liệu nghiệp vụ riêng | Hai nguồn sự thật cho lô (trái §27.2 B "một nguồn sự thật") |

## Hệ quả

- W8.1 làm được ngay, không cần khách, không đổi hành vi nền tảng.
- Engine nội bộ (W8.3) bị chặn bởi tài liệu MKG v1.3: mục tiêu thời gian giải, năm điều kiện tự duyệt, kênh dữ liệu VFT (H-9).
- CI thêm job `lib/optcore` (kiểm thử, golden, giấy phép); image `optimizer` và image engine cùng cài một phiên bản `lib/optcore` có ghim.
- Bảng luồng mạng (ADR-0008) thêm đơn vị engine nội bộ ở vùng mkg, không có luồng sang vùng nền tảng trừ qua cổng tích hợp (bộ điều hợp feed).

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| W8.1 | Tách `lib/optcore`, golden test giữ nguyên byte — **xong** (08/10/2026), xem ghi chú dưới | — |
| W5.5 | Bộ điều hợp feed `mkg/fieldops` (nguồn của kho quan sát) — **xong** (09/10/2026) | mkg 000036 (000024 lỗi thời) |
| W8.2 | Kho quan sát — **xong** (09/10/2026), xem ghi chú dưới | mkg 000037 (000026 lỗi thời) |
| W8.3 | Engine nội bộ ba chế độ, T1/T2a/T2b/T3, vùng đóng băng, tự duyệt (chờ v1.3) | — |

**W8.1 (xong).**

- Gói `lib/optcore/` (`agrigreen-optcore` 1.0.0, src layout, `py.typed`, mypy `disallow_untyped_defs`) giữ các mô-đun `engine` (mô hình mạng lưới `Routes`/`Distance`, hàm chi phí, đối chứng không Hub), `calendar`, `flow` (mô hình cung), `params`, `jsmath`, `select_hubs` (bọc HiGHS, `[milp]`) và `sensitivity`. Các mô-đun được chuyển nguyên trạng bằng `git mv`, chỉ thêm chú thích kiểu. Lõi không có phụ thuộc runtime bắt buộc.
- `agrigreen_optimizer` thành lớp vỏ (`snapshot`, `run`, `worker`, `consumer`, `cli`) và ghim `agrigreen-optcore==1.0.0`.
- Không đổi hành vi. Golden byte được chụp ở commit riêng **trước** khi tách: 35 đầu ra lõi trên 5 ca (evaluate, bỏ đối chứng, độ nhạy, lịch thu hoạch, select_hubs) và 20 phong bì kết quả. Sau khi tách, sha256 và độ dài trùng hết. Bản dump đầy đủ trước và sau cũng trùng từng byte (diff -r).
- Phiên bản: mỗi kết quả vẫn ghi `engine_version` (= `optcore.ENGINE_VERSION`). Chưa thêm trường `optcore_version` vào phong bì, vì làm vậy đổi hợp đồng `result.v1` và đổi byte. Việc này để cho W8.3, khi có triển khai thứ hai. Tới lúc đó, phiên bản gói được ghim ở pyproject của optimizer.
- Build: `optimizer/Dockerfile` nhận lõi qua ngữ cảnh đặt tên `optcore` (`docker build --build-context optcore=lib/optcore … optimizer`; compose `additional_contexts`). CI thêm job `optcore` (pytest, mypy, giấy phép highspy). Job `optimizer` cài lõi từ đường dẫn.

**W8.2 (xong, mã).**

- Chủ kho quan sát: **module mới `mkg/engine`** (`kind: domain`, vùng mkg, schema `mkg_engine`, vai nhóm `r_engine` suy từ registry) — §3 cho phép "module do v1.3 chỉ định"; chọn module riêng thay `mkg/fieldops` vì quan sát phủ nhiều miền (tiến độ gặt theo xã, đơn vị hành chính, đồ thị thủy, danh bạ), và engine (dịch vụ Python, không giữ dữ liệu) cần một module Go phía dữ liệu cho W8.3 (tham số chính sách, kế hoạch đề xuất). Phụ thuộc `mkg/fieldops` chỉ qua `fieldops/port` (kiểu bộ điều hợp feed).
- Sổ `mkg_engine.observation` chỉ ghi thêm (`shared.apply_ledger`): `kind, source (api | manual | sensor), as_of, max_age, uncertainty` như §3, cộng `entity_key`, giá trị decimal + đơn vị (giữ số lẻ của nguồn), `uncertainty_basis`, `payload` (bản ghi hợp đồng), nguồn gốc feed (`feed`, `contract_version`, `feed_seq`, `event_id`, `feed_cursor`), `mapper_version`. `uncertainty` là nửa độ rộng tuyệt đối cùng đơn vị giá trị: từ làm tròn của nguồn, lan truyền theo cận tuyến tính (bảo thủ — engine định cỡ tồn đệm). Ô bị ẩn dưới k (ADR-0035) không phát nên vắng quan sát; dòng `province_withheld` mang căn cứ `k_suppressed_residual`.
- Nạp: job `engine_observation_feed` (api-internal) dùng **cùng** bộ điều hợp feed của W5.5 với cursor riêng; mỗi trang kiểm hợp đồng tích hợp như mọi đối tác, ghi sổ + cursor trong một giao dịch, idempotent theo `event_id`. "Quá `max_age`" là phép đọc: `Observations.Latest` tách `Stale` khỏi `Fresh`; `as_of` hiệu lực tiến theo lần đọc tới đầu feed (feed là dòng trạng thái). Nền tảng ngừng → quan sát chuyển dần sang `Stale`, engine không chờ (§41.4).
- Lệch/chưa làm: đường ghi `manual`/`sensor` và chính sách tăng tồn đệm theo `Stale` thuộc W8.3; thu hồi cấp quyền ghi quan sát rút (lịch sử vẫn trong sổ chỉ ghi thêm — khác bản sao W5.5 xóa dòng; quyết định lưu giữ chờ A7); sau 410 đọc lại phần feed còn giữ, chưa dựng lại từ ảnh chụp tập dữ liệu.

## Liên quan

- Kiến trúc v4.1: §6 (`lib/optcore`), §27.2, §41.4, R-INT-02, R-MOD-10, Phụ lục B mục 19, 20.
- Bản đối chiếu: X-07, dòng §6 `lib/optcore`, vùng MKG 27.2.
- Câu hỏi: [cau-hoi-ton-dong.md](../cau-hoi-ton-dong.md) H-8, H-9.
- [ADR-0001](0001-go-ngon-ngu-dich-vu.md), [ADR-0008](0008-phan-vung-mang.md), [ADR-0016](0016-giay-phep-python.md), [ADR-0018](0018-su-kien-noi-bo-mkg.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), [ADR-0035](0035-cap-phep-tong-hop-theo-xa-nguong-k.md); [docs/design/mkg.md](../design/mkg.md).
