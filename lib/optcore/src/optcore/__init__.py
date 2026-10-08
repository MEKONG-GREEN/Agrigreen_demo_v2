"""optcore — lõi tối ưu dùng chung của AgriGreen (ADR-0036, W8.1).

Thư viện THUẦN: không DB, không broker, không HTTP, không biến môi trường, không đồng hồ
hệ thống. Đầu vào là ảnh chụp đã kiểm hợp đồng (dict theo contracts/optimizer/snapshot.v1),
đầu ra là cấu trúc dữ liệu; cùng đầu vào → cùng kết quả (golden byte).

Thành phần (tách nguyên trạng từ `agrigreen_optimizer`, không đổi hành vi):

- mô hình mạng lưới — `engine.Routes`, `engine.Distance` (khoảng cách đã định tuyến, chọn phương thức);
- mô hình cung — `calendar` (lịch thu hoạch theo ngày), `engine.build_season_supply`, `flow` (luồng rơm theo ngày);
- hàm chi phí — `engine.compute_sizing/compute_capex/compute_opex/build_cost_chain/compute_financials`,
  đối chứng không Hub `engine.compute_no_hub_baseline`; tham số `params`;
- bộ giải — `select_hubs` (MILP HiGHS, tùy chọn `[milp]`), `sensitivity` (FN-18 một-tham-số-một-lần).

Người dùng: `agrigreen_optimizer` (vùng nền tảng, pf/simulation) và engine nội bộ vùng mkg (W8.3).
"""

from .engine import ENGINE_VERSION, SnapshotError, evaluate, normalize_numbers
from .params import SimulationParams, resolve
from .select_hubs import SolverUnavailable, select_hubs
from .sensitivity import run_sensitivity

# Phiên bản ngữ nghĩa của thư viện (pyproject đọc từ đây). Đổi công thức → đổi ENGINE_VERSION
# (được ghi vào mỗi kết quả, `engine_version`) và tăng phiên bản này.
__version__ = "1.0.0"

__all__ = [
    "ENGINE_VERSION",
    "SimulationParams",
    "SnapshotError",
    "SolverUnavailable",
    "__version__",
    "evaluate",
    "normalize_numbers",
    "resolve",
    "run_sensitivity",
    "select_hubs",
]
