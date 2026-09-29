"""So kết quả optimizer với kết quả của prototype (đặc tả) trong tests/golden.

Kết quả prototype dùng camelCase và mang tên/toạ độ; optimizer dùng snake_case và
chỉ có mã tham chiếu. Bộ so đổi tên khóa, bỏ các trường không thuộc hợp đồng
optimizer, rồi so số trong dung sai.
"""

from __future__ import annotations

import math
import re
from typing import Any

# Tên khóa prototype → optimizer (sau khi chuyển camel → snake).
RENAME = {
    "hub_id": "hub_ref",
    "htx_id": "cooperative_ref",
    "season_id": "season_key",
    "id": "plant_ref",
}

# Trường không so (lý do trong optimizer/README.md, mục "Golden").
IGNORE = {
    "scenario_id",  # mã kịch bản: cùng giá trị nhưng so riêng
    "status",  # trạng thái kịch bản do Go quản lý
    "parameter_set_version",  # prototype: số nguyên; nền tảng: băm bộ tham số
    "estimate_notice",  # chuỗi có số định dạng vi-VN — so riêng bằng kiểm thử đơn vị
    "unapproved_parameters",  # Go tính (quy tắc chặt hơn: mọi tham số đã dùng)
    "can_be_marked_official",
    "source_label",
    "calendar_sources",
}

_camel = re.compile(r"(?<!^)(?=[A-Z])")


def snake(name: str) -> str:
    return _camel.sub("_", name).lower()


def to_snake(obj: Any) -> Any:
    if isinstance(obj, dict):
        out = {}
        for k, v in obj.items():
            key = snake(k)
            out[RENAME.get(key, key)] = to_snake(v)
        return out
    if isinstance(obj, list):
        return [to_snake(v) for v in obj]
    return obj


def close(a: float, b: float, rel: float, abs_tol: float) -> bool:
    return math.isclose(a, b, rel_tol=rel, abs_tol=abs_tol)


def compare(expected: Any, actual: Any, path: str = "$", rel: float = 1e-9, abs_tol: float = 1.0001, skip: set[str] | None = None) -> list[str]:
    """Trả danh sách khác biệt (rỗng = khớp). Số nguyên đã làm tròn cho phép lệch ±1."""
    skip = skip or set()
    diffs: list[str] = []
    if isinstance(expected, dict):
        if not isinstance(actual, dict):
            return [f"{path}: muốn object, nhận {type(actual).__name__}"]
        for k, v in expected.items():
            p = f"{path}.{k}"
            if k in IGNORE or p in skip or any(p.endswith(s) for s in skip if s.startswith(".")):
                continue
            if k == "warning_count":  # prototype: chuỗi mang tên hiển thị → chỉ so số lượng cảnh báo
                if v != len(actual.get("warnings", [])):
                    diffs.append(f"{path}.warnings: {v} cảnh báo ≠ {len(actual.get('warnings', []))}")
                continue
            if k not in actual:
                diffs.append(f"{p}: thiếu trong kết quả optimizer")
                continue
            diffs.extend(compare(v, actual[k], p, rel, abs_tol, skip))
        return diffs
    if isinstance(expected, list):
        if not isinstance(actual, list) or len(actual) != len(expected):
            return [f"{path}: độ dài {len(expected)} ≠ {len(actual) if isinstance(actual, list) else type(actual).__name__}"]
        for i, (e, a) in enumerate(zip(expected, actual, strict=True)):
            diffs.extend(compare(e, a, f"{path}[{i}]", rel, abs_tol, skip))
        return diffs
    if isinstance(expected, bool) or expected is None or isinstance(expected, str):
        if expected != actual:
            diffs.append(f"{path}: {expected!r} ≠ {actual!r}")
        return diffs
    if isinstance(expected, (int, float)):
        if not isinstance(actual, (int, float)) or isinstance(actual, bool):
            return [f"{path}: {expected!r} ≠ {actual!r}"]
        if not close(float(expected), float(actual), rel, abs_tol):
            diffs.append(f"{path}: {expected!r} ≠ {actual!r}")
    return diffs
