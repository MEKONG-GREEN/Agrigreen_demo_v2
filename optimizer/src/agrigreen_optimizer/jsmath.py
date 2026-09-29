"""Số học khớp từng bit với prototype TypeScript (đặc tả).

Prototype chạy trên V8: `Math.round` làm tròn nửa lên về phía +∞, `reduce`
cộng dồn trái sang phải. Python 3.12 `round()` làm tròn kiểu ngân hàng và
`sum()` trên float dùng cộng bù Neumaier — cả hai cho kết quả khác prototype ở
chữ số cuối, nên engine KHÔNG dùng chúng cho số liệu kết quả.
"""

from __future__ import annotations

import math
from collections.abc import Iterable


def js_round(value: float, digits: int = 0) -> float:
    """`Math.round(value * 10**digits) / 10**digits` như prototype."""
    factor = 10**digits
    return math.floor(value * factor + 0.5) / factor


def jsum(values: Iterable[float]) -> float:
    """`values.reduce((a, v) => a + v, 0)` — cộng tuần tự, không bù sai số."""
    total = 0.0
    for v in values:
        total += v
    return total


def ceil_int(value: float) -> int:
    return int(math.ceil(value))


def num(value: float) -> float | int:
    """Chuẩn hóa để xuất JSON: số nguyên hiển thị không có phần thập phân."""
    if isinstance(value, bool):
        return value
    if isinstance(value, int):
        return value
    if math.isfinite(value) and value == math.floor(value) and abs(value) < 2**53:
        return int(value)
    return value


def _group(int_part: str) -> str:
    out = []
    while len(int_part) > 3:
        out.insert(0, int_part[-3:])
        int_part = int_part[:-3]
    out.insert(0, int_part)
    return ".".join(out)


def fmt_vi(value: float, max_fraction: int = 0) -> str:
    """`toLocaleString('vi-VN', {maximumFractionDigits})`: nhóm nghìn bằng '.', thập phân bằng ','."""
    neg = value < 0
    v = js_round(abs(value), max_fraction)
    if max_fraction == 0:
        s = _group(str(int(v)))
    else:
        text = f"{v:.{max_fraction}f}".rstrip("0").rstrip(".")
        if "." in text:
            ip, fp = text.split(".")
            s = _group(ip) + "," + fp
        else:
            s = _group(text)
    return ("-" if neg and s not in ("0",) else "") + s


def fmt_tons(value: float) -> str:
    return fmt_vi(js_round(value))


def fmt_num(value: float) -> str:
    return fmt_vi(value, 1)


def fmt_pct(value: float) -> str:
    # toFixed(1) của JS làm tròn theo biểu diễn thập phân chính xác; đủ gần cho chuỗi cảnh báo.
    return f"{js_round(value, 1):.1f}%"
