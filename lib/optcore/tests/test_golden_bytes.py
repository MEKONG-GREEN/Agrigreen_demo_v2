"""Golden byte của lõi (W8.1, ADR-0036): đầu ra engine giữ NGUYÊN BYTE qua lần tách `lib/optcore`.

`golden_bytes.json` được chụp từ `agrigreen_optimizer` TRƯỚC khi tách (cab0280) và chỉ
được sinh lại khi công thức đổi có chủ đích (kèm đổi ENGINE_VERSION):

    python tests/test_golden_bytes.py --write     # từ lib/optcore

Mỗi đầu ra được tuần tự hóa như phong bì kết quả (khóa sắp xếp, UTF-8, không khoảng
trắng) rồi so sha256 + độ dài — tương đương so byte. `--dump DIR` ghi đầu ra đầy đủ để diff.
"""

from __future__ import annotations

import hashlib
import json
import sys
from collections.abc import Callable, Iterator
from pathlib import Path
from typing import Any

import pytest

HERE = Path(__file__).parent
GOLDEN = HERE / "golden"
MANIFEST = HERE / "golden_bytes.json"
CASES = sorted(p.name for p in GOLDEN.iterdir() if (p / "snapshot.json").exists())


def _dumps(obj: Any) -> bytes:
    return json.dumps(obj, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")


def _has_highspy() -> bool:
    try:
        import highspy  # type: ignore[import-untyped]  # noqa: F401
    except ImportError:
        return False
    return True


def outputs(case: str) -> Iterator[tuple[str, Callable[[], Any]]]:
    """(tên, hàm tính) cho mọi đầu ra của một ca — tên ổn định là khóa trong manifest."""
    from optcore import calendar as cal
    from optcore.engine import evaluate, normalize_numbers
    from optcore.params import SENSITIVITY_SUPPORTED, resolve
    from optcore.select_hubs import select_hubs
    from optcore.sensitivity import run_sensitivity

    snap = json.loads((GOLDEN / case / "snapshot.json").read_text(encoding="utf-8"))

    def calendar() -> Any:
        p = resolve(snap["params"])
        got = cal.build(snap["seasons"], snap.get("harvest_observations", {}), p.harvest_window_days)
        return {
            k: {"season_type": c.season_type, "source": c.source, "source_label": c.source_label, "days": [list(d) for d in c.days]}
            for k, c in got.items()
        }

    yield "evaluate", lambda: normalize_numbers(evaluate(snap))
    yield "evaluate_skip_baseline", lambda: normalize_numbers(evaluate(snap, skip_baseline=True))
    yield "sensitivity_default", lambda: normalize_numbers(run_sensitivity(snap, evaluate(snap)))
    yield "sensitivity_all", lambda: normalize_numbers(run_sensitivity(snap, evaluate(snap), (*SENSITIVITY_SUPPORTED, "khong_ton_tai")))
    yield "calendar", lambda: normalize_numbers(calendar())
    if _has_highspy():
        yield "select_hubs_1", lambda: normalize_numbers(select_hubs(snap, 1))
        yield "select_hubs_all", lambda: normalize_numbers(select_hubs(snap, None))


def _entry(fn: Callable[[], Any]) -> dict[str, Any]:
    b = _dumps(fn())
    return {"sha256": hashlib.sha256(b).hexdigest(), "bytes": len(b)}


def _params() -> list[tuple[str, str]]:
    return [(c, n) for c in CASES for n, _ in outputs(c)]


@pytest.mark.parametrize(("case", "name"), _params())
def test_engine_output_bytes_unchanged(case: str, name: str) -> None:
    want = json.loads(MANIFEST.read_text(encoding="utf-8"))[f"{case}/{name}"]
    fn = dict(outputs(case))[name]
    assert _entry(fn) == want, f"{case}/{name}: byte đầu ra lõi đã đổi (xem docstring để diff)"


def test_manifest_covers_every_output() -> None:
    names = {f"{c}/{n}" for c, n in _params()}
    have = set(json.loads(MANIFEST.read_text(encoding="utf-8")))
    if not _has_highspy():
        have = {k for k in have if "/select_hubs" not in k}
    assert names == have


def main(argv: list[str]) -> int:  # pragma: no cover - công cụ sinh lại
    if "--write" in argv:
        manifest = {f"{c}/{n}": _entry(fn) for c in CASES for n, fn in outputs(c)}
        MANIFEST.write_text(json.dumps(manifest, indent=1, sort_keys=True) + "\n", encoding="utf-8")
        print(f"{len(manifest)} đầu ra → {MANIFEST}")
    if "--dump" in argv:
        out = Path(argv[argv.index("--dump") + 1])
        out.mkdir(parents=True, exist_ok=True)
        for c in CASES:
            for n, fn in outputs(c):
                (out / f"{c}.{n}.json").write_bytes(_dumps(fn()))
    return 0


if __name__ == "__main__":  # pragma: no cover
    sys.exit(main(sys.argv[1:]))
