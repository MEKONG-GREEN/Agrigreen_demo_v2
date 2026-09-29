"""Hợp đồng contracts/optimizer: ảnh chụp và phong bì kết quả khớp JSON Schema; RB-7."""

from __future__ import annotations

import copy
import json
from pathlib import Path

import pytest

from agrigreen_optimizer.run import dumps, process

jsonschema = pytest.importorskip("jsonschema")

ROOT = Path(__file__).resolve().parents[2]
CONTRACTS = ROOT / "contracts" / "optimizer"
GOLDEN = Path(__file__).parent / "golden"
CASES = sorted(p.name for p in GOLDEN.iterdir() if (p / "snapshot.json").exists())


def schema(name: str) -> dict:
    return json.loads((CONTRACTS / name).read_text(encoding="utf-8"))


def snapshot_bytes(case: str) -> bytes:
    return (GOLDEN / case / "snapshot.json").read_bytes()


@pytest.mark.parametrize("case", CASES)
def test_golden_snapshots_match_input_schema(case: str) -> None:
    jsonschema.validate(json.loads(snapshot_bytes(case)), schema("snapshot.v1.schema.json"))


@pytest.mark.parametrize("case", CASES)
def test_result_envelope_matches_output_schema(case: str) -> None:
    env = json.loads(dumps(process(snapshot_bytes(case))))
    assert env["status"] == "completed", env["error"]
    jsonschema.validate(env, schema("result.v1.schema.json"))


def test_platform_example_snapshot_is_accepted() -> None:
    """Ảnh chụp mẫu do chính Go (pf/simulation) sinh ra phải qua schema và tính được (liên ngôn ngữ)."""
    example = CONTRACTS / "examples" / "snapshot.example.json"
    raw = example.read_bytes()
    jsonschema.validate(json.loads(raw), schema("snapshot.v1.schema.json"))
    env = process(raw)
    assert env["status"] == "completed", env["error"]
    jsonschema.validate(json.loads(dumps(env)), schema("result.v1.schema.json"))


@pytest.mark.parametrize(
    "path, field",
    [
        (("cooperatives", 0), "name"),
        (("cooperatives", 0), "lat"),
        (("hubs", 0), "lng"),
        ((), "farmer_phone"),
        (("scenario", "plant"), "name"),
    ],
)
def test_rb7_identifying_fields_are_rejected(path: tuple, field: str) -> None:
    """RB-7: optimizer chỉ nhận dữ liệu không định danh — trường lạ ở mọi cấp bị từ chối (mặc định từ chối)."""
    data = json.loads(snapshot_bytes(CASES[0]))
    target = data
    for p in path:
        target = target[p]
    target[field] = "x"
    raw = json.dumps(data).encode()
    env = process(raw)
    assert env["status"] == "failed"
    assert env["error"]["code"] == "optimizer.snapshot_invalid"
    with pytest.raises(jsonschema.ValidationError):
        jsonschema.validate(data, schema("snapshot.v1.schema.json"))


def test_result_contains_no_names_or_coordinates() -> None:
    env = process(snapshot_bytes(CASES[1]))
    text = json.dumps(env, ensure_ascii=False)
    for key in ('"name"', '"lat"', '"lng"', '"address"', '"phone"'):
        assert key not in text


def test_snapshot_sha256_is_over_received_bytes() -> None:
    import hashlib

    raw = snapshot_bytes(CASES[0])
    env = process(raw)
    assert env["snapshot_sha256"] == hashlib.sha256(raw).hexdigest()
    mutated = copy.deepcopy(json.loads(raw))
    mutated["scenario"]["lifecycle_years"] = 11
    assert process(json.dumps(mutated).encode())["snapshot_sha256"] != env["snapshot_sha256"]
