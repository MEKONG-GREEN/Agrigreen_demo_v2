"""Điểm vào dòng lệnh.

    agrigreen-optimizer evaluate --snapshot snapshot.json --out result.json
    agrigreen-optimizer handle-event --event envelope.json --api https://api-platform-internal:8083 \
        --cert optimizer.crt --key optimizer.key --ca ca.crt

`-` là stdin/stdout. Mã thoát: 0 = xong, 2 = ảnh chụp không hợp lệ/tính hỏng (kết
quả vẫn được ghi, status = failed), 1 = lỗi hạ tầng (worker).
"""

from __future__ import annotations

import argparse
import json
import os
import sys

from .run import dumps, process
from .worker import InternalAPI, WorkerError, handle_event


def _read(path: str) -> bytes:
    if path == "-":
        return sys.stdin.buffer.read()
    with open(path, "rb") as f:
        return f.read()


def _write(path: str, data: bytes) -> None:
    if path == "-":
        sys.stdout.buffer.write(data + b"\n")
        return
    with open(path, "wb") as f:
        f.write(data)


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="agrigreen-optimizer")
    sub = ap.add_subparsers(dest="cmd", required=True)
    ev = sub.add_parser("evaluate", help="đọc ảnh chụp JSON, ghi phong bì kết quả JSON")
    ev.add_argument("--snapshot", required=True)
    ev.add_argument("--out", default="-")
    he = sub.add_parser("handle-event", help="xử lý một phong bì pf.simulation.job.requested qua Internal API")
    he.add_argument("--event", required=True)
    he.add_argument("--api", default=os.environ.get("AG_INTERNAL_API_URL", ""))
    he.add_argument("--cert", default=os.environ.get("AG_OPTIMIZER_CLIENT_CERT"))
    he.add_argument("--key", default=os.environ.get("AG_OPTIMIZER_CLIENT_KEY"))
    he.add_argument("--ca", default=os.environ.get("AG_INTERNAL_CA"))
    args = ap.parse_args(argv)

    if args.cmd == "evaluate":
        env = process(_read(args.snapshot))
        _write(args.out, dumps(env))
        return 0 if env["status"] == "completed" else 2
    if not args.api:
        print("thiếu --api hoặc AG_INTERNAL_API_URL", file=sys.stderr)
        return 1
    try:
        out = handle_event(json.loads(_read(args.event)), InternalAPI(args.api, args.cert, args.key, args.ca))
    except (WorkerError, KeyError, json.JSONDecodeError) as exc:
        print(f"optimizer: {exc}", file=sys.stderr)
        return 1
    print(json.dumps({"status": out.get("status")}, ensure_ascii=False))
    return 0


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())
