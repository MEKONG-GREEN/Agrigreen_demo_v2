# optcore — lõi tối ưu dùng chung (Python 3.12)

Thư viện thuần của engine AgriGreen, tách nguyên trạng từ `optimizer/` (`agrigreen_optimizer`) ở gói W8.1 theo [ADR-0036](../../docs/adr/0036-engine-ba-che-do-vung-noi-bo-optcore.md). Hai đơn vị triển khai dùng chung mã này, mỗi bên giữ dữ liệu riêng:

- `optimizer` ở vùng nền tảng (`pf/simulation`): lớp vỏ nhận việc, kiểm hợp đồng ảnh chụp, gọi `optcore`, ghi phong bì kết quả.
- Engine nội bộ ở vùng mkg (W8.3): chưa có.

## Ranh giới

- **Không I/O.** Thư viện không đụng DB, broker, HTTP, biến môi trường hay đồng hồ hệ thống. Đầu vào là ảnh chụp đã kiểm hợp đồng (dict theo `contracts/optimizer/snapshot.v1.schema.json`). Đầu ra là dict/dataclass.
- **Tất định.** Cùng đầu vào cho cùng byte đầu ra. Số học khớp V8 qua `jsmath` (`js_round`, `jsum`).
- **Không phụ thuộc runtime bắt buộc.** Gói tùy chọn `[milp]` kéo `highspy` (MIT) cho `select_hubs`.
- Kiểm hợp đồng (pydantic), phong bì kết quả, `ALGORITHM_VERSION` và vòng nhận RabbitMQ thuộc lớp vỏ, không thuộc lõi.

## Thành phần

| Thành phần | Mô-đun |
|---|---|
| Mô hình mạng lưới | `engine.Routes`, `engine.Distance`: khoảng cách đã định tuyến trong ảnh chụp, chọn phương thức đường bộ/thủy |
| Mô hình cung | `calendar` (lịch thu hoạch theo ngày), `engine.build_season_supply`, `flow` (luồng rơm theo ngày, 2 năm) |
| Hàm chi phí | `engine.compute_sizing`, `compute_capex`, `compute_opex`, `build_cost_chain`, `compute_financials`, `compute_no_hub_baseline`; tham số `params` |
| Bộ giải | `select_hubs` (MILP HiGHS, THỬ NGHIỆM), `sensitivity` (FN-18 một-tham-số-một-lần) |
| Điểm vào | `optcore.evaluate`, `select_hubs`, `run_sensitivity`, `normalize_numbers`, `resolve` |

## Phiên bản

- `optcore.__version__` là phiên bản ngữ nghĩa của gói. `optimizer/pyproject.toml` ghim đúng bản này (`agrigreen-optcore==…`), và image của engine nội bộ sẽ ghim cùng bản.
- `ENGINE_VERSION` (hiện `2026.09.24`) là phiên bản công thức, được ghi vào mỗi kết quả (`engine_version`). Đổi công thức thì đổi cả hai và sinh lại golden.

## Kiểm thử

```powershell
cd lib/optcore
python -m pip install -e ".[dev,milp]"
python -m pytest     # golden prototype (test_golden) + golden byte (test_golden_bytes)
python -m mypy       # kiểu: disallow_untyped_defs
```

- `tests/golden/case*/` chứa 5 ca do prototype `mekong-green` sinh. `expected.json` là kết quả prototype. Cách sinh lại và các lệch đã biết ghi ở [optimizer/README.md](../../optimizer/README.md#golden).
- `tests/golden_bytes.json` ghi sha256 và độ dài của 35 đầu ra lõi (evaluate, bỏ đối chứng, độ nhạy mặc định và đủ mọi mã, lịch thu hoạch, select_hubs) trên 5 ca. Bản này được chụp **trước** khi tách, ở cab0280. Sau khi tách, đầu ra phải trùng từng byte.
- Chỉ sinh lại khi công thức đổi có chủ đích: `python -m tests.test_golden_bytes --write`. Dùng `--dump DIR` để ghi đầu ra đầy đủ ra thư mục rồi diff.
