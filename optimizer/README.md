# optimizer (Python 3.12, G2)

Dịch vụ tính mô phỏng chọn Hub cho `pf/simulation`. Đây là ngoại lệ bắt buộc về ngôn ngữ (mục 4): Go không có engine, chỉ giữ kịch bản, ảnh chụp và kết quả.

Từ W8.1 ([ADR-0036](../docs/adr/0036-engine-ba-che-do-vung-noi-bo-optcore.md)), công thức nằm ở lõi dùng chung [`lib/optcore`](../lib/optcore/README.md): mô hình mạng lưới, mô hình cung, hàm chi phí và bộ giải. `agrigreen_optimizer` chỉ còn là lớp vỏ:

- `snapshot` kiểm hợp đồng;
- `run` dựng phong bì kết quả và `ALGORITHM_VERSION`;
- `worker`, `consumer`, `cli` lo Internal API, RabbitMQ và dòng lệnh.

`pyproject.toml` ghim `agrigreen-optcore==<phiên bản>`.

## Quy ước

- Nhận việc qua mẫu bất đồng bộ ở mục 12. Đầu vào là **ảnh chụp bất biến** gồm tham số đã giải, phiên bản mạng, **kết quả định tuyến đã dùng** và `algorithm_version`.
- Chỉ nhận dữ liệu **không định danh** (RB-7): mã tham chiếu UUID, không tên, không toạ độ. Khoảng cách chim bay xét bán kính (FN-05) do Go tính sẵn nên mô hình không cần toạ độ. Mô hình pydantic đặt `extra="forbid"` ở mọi cấp, nên trường lạ bị từ chối.
- Hợp đồng nằm ở `contracts/optimizer/`:
  - `snapshot.v1.schema.json` là đầu vào;
  - `result.v1.schema.json` là phong bì kết quả;
  - `examples/snapshot.example.json` là ảnh chụp do chính Go sinh (`internal/platform/simulation/app`, `AG_UPDATE_EXAMPLES=1`).
- Engine chuyển **công thức** từ prototype `mekong-green/src/erp/simulation` (ENGINE_VERSION 2026.09.24) sang, không chép mã. Chuỗi tính: FN-04 → FN-05 → FN-06 → FN-07 → FN-08 (3 chặng) → FN-09 → FN-10 → FN-11 → FN-12, đối chứng FN-17, mô hình luồng rơm theo ngày 2 năm (lấy năm 2), và FN-18 độ nhạy một tham số mỗi lần.
- Số học khớp V8: dùng `js_round` (làm tròn nửa lên) và `jsum` (cộng tuần tự). Không dùng `round()`/`sum()` của Python, vì Python 3.12 dùng cộng bù sai số.
- Kết quả **không có thời điểm tính**, nên cùng ảnh chụp luôn cho cùng byte. Nộp lặp là idempotent.
- `select_hubs` là chế độ **THỬ NGHIỆM**: MILP vị trí cơ sở rút gọn giải bằng HiGHS (`highspy`, MIT, phụ thuộc tùy chọn `[milp]`). Chế độ này gợi ý tập Hub, rồi chạy lại `evaluate` đầy đủ. Nó bỏ qua trần chở thẳng theo ngày.

## Luồng

1. `POST /v1/simulations` trả 202.
2. `pf.simulation.run.submitted` tới worker Go. Worker dựng ảnh chụp qua `SupplySource`, `Router` và `ParamSet`.
3. `pf.simulation.job.requested` mang `{simulation_id, snapshot_sha256, snapshot_path, result_path}`.
4. Optimizer chạy `agrigreen-optimizer handle-event --event env.json --api https://… --cert --key --ca`. Lệnh này làm ba việc:
   - `GET /internal/v1/simulations/{id}/snapshot` với header `X-Tenant-ID` qua mTLS;
   - kiểm sha256;
   - tính, rồi `POST /internal/v1/simulations/{id}/result`.
5. Go ghi kết quả bất biến và phát `pf.simulation.job.completed`.

## Vòng nhận RabbitMQ (`agrigreen-optimizer consume`)

`consumer.py` bọc `handle_event` — lệnh mặc định của image Docker. Topology **giống hệt** consumer Go (`internal/shared/inbox.DeclareQueue`):

| Thành phần | Giá trị |
|---|---|
| Exchange sự kiện / thư chết | `ag.events` (topic, routing key = event_type) / `ag.dlx` |
| Queue | `pf.simulation.optimizer` (quorum, `x-delivery-limit` 5, thư chết → `ag.dlx` khoá `pf.simulation.optimizer`), bind `pf.simulation.job.requested` |
| DLQ | `pf.simulation.optimizer.dlq` (quorum, không tự xóa — R-62) |

- **ACK chỉ sau khi POST kết quả thành công** (201, hoặc 200 khi nộp lặp). Ảnh chụp không hợp lệ vẫn được nộp với `status = failed` rồi ACK.
- **Lỗi vĩnh viễn** (phong bì hỏng, thiếu trường, sai event_type, băm lệch, HTTP 4xx trừ 408/429): NACK không requeue, thông điệp sang DLQ ngay.
- **Lỗi tạm thời** (mạng/TLS, HTTP 5xx, 408, 429, lỗi bất ngờ): chờ `AG_OPTIMIZER_RETRY_BASE_S × 2^x-delivery-count` giây (tối đa 60) rồi NACK requeue. Quorum queue đếm lần giao, quá 5 lần thì sang DLQ.
- Tính ở luồng phụ. Luồng kết nối giữ heartbeat và nhận ACK/NACK qua `add_callback_threadsafe` (mẫu chuẩn của pika). Mất kết nối thì nối lại, lùi dần tới 30 s. SIGTERM thì dừng; thông điệp đang dở chưa ACK nên broker giao lại, và nộp lặp là idempotent.
- `traceparent` trong header AMQP được chuyển sang Internal API (R-90).

Biến môi trường:

| Biến | Ý nghĩa |
|---|---|
| `AG_AMQP_URL` | `amqp://user:pass@rabbitmq:5672/` (bắt buộc) |
| `AG_INTERNAL_API_URL` | `https://api-platform:8084`, listener Internal của api-platform (bắt buộc) |
| `AG_OPTIMIZER_CLIENT_CERT`, `AG_OPTIMIZER_CLIENT_KEY`, `AG_INTERNAL_CA` | mTLS: chứng thư client CN/SAN `optimizer` và CA của máy chủ |
| `AG_OPTIMIZER_QUEUE` | mặc định `pf.simulation.optimizer` |
| `AG_OPTIMIZER_PREFETCH` | mặc định 1 |
| `AG_OPTIMIZER_RETRY_BASE_S` | mặc định 2 |

Phía api-platform bật listener bằng `AG_INTERNAL_HTTP_ADDR` (`:8084`) cùng `AG_INTERNAL_TLS_CERT`, `AG_INTERNAL_TLS_KEY`, `AG_INTERNAL_CLIENT_CA`. Không đặt thì listener tắt. Đặt địa chỉ mà thiếu tệp nào thì api-platform từ chối khởi động. Ở Docker Compose, chứng thư dev do `scripts/gen-dev-certs.sh` hoặc `.ps1` sinh vào `deploy/compose/.certs/` (gitignore); `gen-dev-secrets` tự gọi script này. Cổng 8084 chỉ mở trong mạng compose.

## Chạy

```powershell
cd optimizer
python -m pip install -e "..\lib\optcore[milp]" -e ".[dev,milp]"   # Python 3.12 (py -3.12); lõi cài từ đường dẫn
python -m pytest                        # 66 kiểm thử (gồm golden byte phong bì, vòng nhận với broker giả); test_select_hubs tự bỏ qua nếu thiếu highspy
agrigreen-optimizer evaluate --snapshot ..\contracts\optimizer\examples\snapshot.example.json --out result.json
```

Không cài gói thì chạy thẳng `python -m pytest`: pytest đọc `pythonpath = ["src", "../lib/optcore/src"]` từ pyproject.

Kiểm giấy phép (RB-4, danh sách `deploy/policies/allowed-licenses.txt`):

```powershell
pip-licenses --packages pydantic pydantic-core annotated-types typing-inspection highspy --allow-only="MIT;MIT License"
pip-licenses --packages pika --allow-only="BSD-3-Clause;BSD License"
```

Phụ thuộc runtime (bản đã kiểm 29/09/2026):

| Gói | Giấy phép | Trạng thái |
|---|---|---|
| pydantic, pydantic-core, annotated-types, typing-inspection | MIT | cho phép |
| highspy (`[milp]`) | MIT | cho phép |
| pika | BSD-3-Clause (pip-licenses báo "BSD License") | cho phép; thuần Python, không có phụ thuộc |
| typing-extensions (phụ thuộc bắt buộc của pydantic) | PSF-2.0 | **chưa** có trong danh sách |
| numpy (phụ thuộc của highspy) | `BSD-3-Clause AND 0BSD AND MIT AND Zlib AND CC0-1.0` | **chưa** đủ: 0BSD, Zlib, CC0-1.0 chưa có trong danh sách |

Hai dòng cuối chờ [ADR-0016](../docs/adr/0016-giay-phep-python.md) (trạng thái "Đề xuất"). Tới khi ADR được chấp nhận, CI chỉ in giấy phép của hai gói này ra, không chặn.

Docker (non-root, slim), từ gốc repo: `docker build --build-context optcore=lib/optcore -t agrigreen/optimizer optimizer`. Lõi đến qua ngữ cảnh đặt tên `optcore` (BuildKit). Compose khai báo ngữ cảnh này bằng `additional_contexts`.

## Golden

**Golden byte (W8.1).** `tests/golden_bytes.json` ghi sha256 và độ dài của 20 phong bì kết quả, đúng byte mà worker POST về: 5 ca golden và ảnh chụp ví dụ của hợp đồng, mỗi ca chạy nguyên trạng, kèm độ nhạy và `select_hubs`; thêm 2 ảnh chụp hỏng. Bản này được chụp trước khi tách `lib/optcore`, ở cab0280. Chỉ sinh lại khi công thức hoặc hợp đồng đổi có chủ đích: `python -m tests.test_golden_bytes --write`. Lõi có golden byte riêng ở `lib/optcore/tests/golden_bytes.json`.

### Tái lập prototype

`lib/optcore/tests/golden/case*/` chứa 5 ca. Từ W8.1 các ca này thuộc lõi; `tests/paths.py` trỏ tới đó. Mỗi ca gồm:

- `snapshot.json` là đầu vào đúng định dạng hợp đồng;
- `expected.json` là `ScenarioResult` của prototype, đã bỏ tên và toạ độ.

Các ca được sinh bằng cách **chạy chính prototype** trên seed tất định của `tests/simulation.test.ts` và `scripts/demo.ts`:

```powershell
$env:MEKONG_GREEN_DIR = "C:\Users\manht\Downloads\claude-skills-main\mekong-green"
cd lib\optcore
node tests\golden\generate\dump_prototype.ts .\out      # Node ≥ 22.6
python tests\golden\generate\prepare.py .\out tests\golden
```

| Ca | Nội dung |
|---|---|
| case1 | 2 Hub, bán kính 40, tham số chưa phê duyệt |
| case2 | 3 Hub, đã phê duyệt, có chiết khấu, kèm FN-18 độ nhạy |
| case3 | 5 Hub, thuê đất, ghi đè đường thủy, baseline nhập tay, vòng đời 12 năm |
| case4 | kho nhỏ |
| case5 | lịch thu hoạch thực tế: DX theo đợt điều tra, HT theo ngày thu hoạch |

**Dung sai:** tương đối 1e-9, tuyệt đối ±1 với số đã làm tròn đến đơn vị. Thực tế mọi số khớp đúng từng giá trị. Lịch thu hoạch khớp tới 1e-12. Chuỗi cảnh báo chỉ so số lượng, vì prototype dùng tên Hub còn optimizer dùng mã Hub.

**Lệch đã biết.** Ba lỗi đầu là lỗi của prototype; optimizer sửa theo ý định rõ ràng của mã:

1. **`field_hub.trips`.** Prototype đọc `flow.boatTripsToHub` trên kết quả của một Hub. Trường này không tồn tại, nên `trips` mất và `vehiclesRequired = null`. Optimizer tính `ceil(tấn qua Hub ÷ #51)`. Golden bỏ qua `vehicles_required` của chặng này.
2. **Sà lan theo Hub.** Câu `SELECT scenario_hubs` của prototype không lấy `barge_payload_tons`, nên luôn dùng #52. Optimizer dùng cấu hình của từng Hub. Golden ghi `barge_payload_tons: null`, tức đúng đầu vào mà prototype thực dùng.
3. **Độ nhạy #10.** Prototype đọc khoảng cách từ cache, nên ghi đè hệ số hiệu chỉnh không có tác dụng. Optimizer tính lại mọi tuyến nguồn `haversine_hieu_chinh` bằng `straight_km × #10`. Golden bỏ dòng `distance_correction` và kiểm riêng rằng swing > 0.
4. **Ngày thu hoạch.** Prototype chỉ dùng ngày thu hoạch **đầu tiên** của mỗi loại vụ khi không có dữ liệu theo đợt. Việc chọn dữ liệu quan sát thuộc `SupplySource`. Bộ sinh golden tái lập đúng hành vi này.
5. **Quy tắc "Chính thức".** Prototype chỉ xét nhóm giả định. Nền tảng chặt hơn: mọi tham số đã dùng phải có phiên bản phê duyệt ở pf/masterdata. Go tính quy tắc này và ghi vào ảnh chụp.
