# optimizer (Python 3.12, G2)

Dịch vụ tính mô phỏng chọn Hub cho `pf/simulation`. Đây là ngoại lệ bắt buộc về ngôn ngữ (mục 4): Go không có engine, chỉ giữ kịch bản, ảnh chụp và kết quả.

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

Việc nhận từ RabbitMQ (ACK sau khi POST thành công) là lớp bọc ngoài `handle-event`. Lớp này chưa làm ở đợt 1 (xem báo cáo tích hợp).

## Chạy

```powershell
cd optimizer
python -m pip install -e ".[dev,milp]"   # Python 3.12 (py -3.12)
python -m pytest                        # 45 kiểm thử; test_select_hubs tự bỏ qua nếu thiếu highspy
agrigreen-optimizer evaluate --snapshot ..\contracts\optimizer\examples\snapshot.example.json --out result.json
```

Không cài gói thì chạy `set PYTHONPATH=src` rồi `python -m pytest` (pytest đọc `pythonpath = src` từ pyproject).

Kiểm giấy phép (RB-4, danh sách `deploy/policies/allowed-licenses.txt`):

```powershell
pip-licenses --packages pydantic pydantic-core annotated-types typing-inspection highspy --allow-only="MIT;MIT License"
```

`typing_extensions` là phụ thuộc bắt buộc của pydantic, giấy phép **PSF-2.0**, không có trong danh sách cho phép. `numpy` là phụ thuộc của highspy, giấy phép gộp `BSD-3-Clause AND 0BSD AND MIT AND Zlib AND CC0-1.0`. Người tích hợp cần một ADR cho hai gói này hoặc bổ sung chúng vào danh sách cho phép.

Docker (non-root, slim): `docker build -t agrigreen/optimizer optimizer`.

## Golden: tái lập prototype

`tests/golden/case*/` chứa 5 ca:

- `snapshot.json` là đầu vào đúng định dạng hợp đồng;
- `expected.json` là `ScenarioResult` của prototype, đã bỏ tên và toạ độ.

Các ca được sinh bằng cách **chạy chính prototype** trên seed tất định của `tests/simulation.test.ts` và `scripts/demo.ts`:

```powershell
$env:MEKONG_GREEN_DIR = "C:\Users\manht\Downloads\claude-skills-main\mekong-green"
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
