"""Đường dẫn dữ liệu kiểm thử dùng chung.

Ca golden (đầu vào ảnh chụp + kết quả prototype) thuộc lõi `lib/optcore` từ W8.1;
lớp vỏ optimizer đọc cùng các tệp đó để kiểm phong bì, hợp đồng và vòng nhận.
"""

from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
GOLDEN = ROOT / "lib" / "optcore" / "tests" / "golden"
EXAMPLE = ROOT / "contracts" / "optimizer" / "examples" / "snapshot.example.json"
