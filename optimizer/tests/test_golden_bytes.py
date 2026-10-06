"""Golden byte của phong bì kết quả (W8.1): `process` + `dumps` giữ NGUYÊN BYTE qua lần tách `lib/optcore`.

`golden_bytes.json` được chụp TRƯỚC khi tách (cab0280); chỉ sinh lại khi công thức hoặc
hợp đồng đổi có chủ đích:

    python -m tests.test_golden_bytes --write     # từ optimizer/

So sha256 + độ dài của đúng byte mà worker POST về Internal API.
"""

from __future__ import annotations

import hashlib
import json
import sys
from collections.abc import Iterator
from pathlib import Path
from typing import Any

import pytest

from .paths import EXAMPLE, GOLDEN

MANIFEST = Path(__file__).parent / "golden_bytes.json"
CASES = sorted(p.name for p in GOLDEN.iterdir() if (p / "snapshot.json").exists())


def _has_highspy() -> bool:
    try:
        import highspy  # type: ignore[import-untyped]  # noqa: F401
    except ImportError:
        return False
    return True


def _with_options(raw: bytes, **opts: Any) -> bytes:
    data = json.loads(raw)
    data["options"] = {**data.get("options", {}), **opts}
    return json.dumps(data, ensure_ascii=False, sort_keys=True).encode("utf-8")


def inputs() -> Iterator[tuple[str, bytes]]:
    """(tên, byte ảnh chụp) — byte sinh tất định từ tệp golden và ví dụ hợp đồng."""
    files = {c: (GOLDEN / c / "snapshot.json").read_bytes() for c in CASES}
    files["contract_example"] = EXAMPLE.read_bytes()
    for name, raw in files.items():
        yield f"{name}/as_is", raw
        yield f"{name}/sensitivity_default", _with_options(raw, mode="evaluate", sensitivity_codes=["loss_ratio", "capex_other", "discount_rate_pct"])
        if _has_highspy():
            yield f"{name}/select_hubs_2", _with_options(raw, mode="select_hubs", max_hubs=2, sensitivity_codes=["road_freight_rate"])
    yield "invalid/json", b"{khong phai json"
    yield "invalid/schema", json.dumps({"schema_version": 1, "la": True}).encode("utf-8")


def _entry(raw: bytes) -> dict[str, Any]:
    from agrigreen_optimizer.run import dumps, process

    b = dumps(process(raw))
    return {"sha256": hashlib.sha256(b).hexdigest(), "bytes": len(b)}


@pytest.mark.parametrize("name", [n for n, _ in inputs()])
def test_envelope_bytes_unchanged(name: str) -> None:
    want = json.loads(MANIFEST.read_text(encoding="utf-8"))[name]
    assert _entry(dict(inputs())[name]) == want, f"{name}: byte phong bì kết quả đã đổi"


def test_manifest_covers_every_input() -> None:
    have = set(json.loads(MANIFEST.read_text(encoding="utf-8")))
    if not _has_highspy():
        have = {k for k in have if not k.endswith("/select_hubs_2")}
    assert {n for n, _ in inputs()} == have


if __name__ == "__main__" and "--write" in sys.argv:  # pragma: no cover - công cụ sinh lại
    manifest = {n: _entry(raw) for n, raw in inputs()}
    MANIFEST.write_text(json.dumps(manifest, indent=1, sort_keys=True) + "\n", encoding="utf-8")
    print(f"{len(manifest)} phong bì → {MANIFEST}")
