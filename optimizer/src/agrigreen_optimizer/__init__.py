"""AgriGreen optimizer — engine đánh giá kịch bản chọn Hub (pf/simulation, mục 12).

Ngoại lệ ngôn ngữ bắt buộc (Python 3.12, mục 4). Chỉ nhận ảnh chụp đầu vào KHÔNG
chứa định danh (RB-7): mã tham chiếu UUID, không tên, không toạ độ.
"""

from .engine import ENGINE_VERSION

# Phiên bản thuật toán của optimizer: đổi khi công thức đổi (Go ghim vào ảnh chụp).
ALGORITHM_VERSION = f"agrigreen-optimizer/{ENGINE_VERSION}+py.1"

__all__ = ["ALGORITHM_VERSION", "ENGINE_VERSION"]
