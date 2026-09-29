"""Copy golden cases into optimizer/tests/golden, stripping names/coordinates from the prototype output."""
import json
import shutil
import sys
from pathlib import Path

src = Path(sys.argv[1])
dst = Path(sys.argv[2])
STRIP = {"name", "lat", "lng", "scenarioName", "provinceName", "seasonName", "computedAt", "lineage", "notes"}


def clean(o):
    if isinstance(o, dict):
        out = {}
        for k, v in o.items():
            if k in STRIP:
                continue
            if k == "code" and isinstance(v, str) and v.startswith("HTX-"):
                continue
            out[k] = clean(v)
        return out
    if isinstance(o, list):
        return [clean(v) for v in o]
    return o


for case in sorted(p for p in src.iterdir() if p.is_dir()):
    d = dst / case.name
    d.mkdir(parents=True, exist_ok=True)
    snap = json.loads((case / "snapshot.json").read_text(encoding="utf-8"))
    exp = json.loads((case / "expected.json").read_text(encoding="utf-8"))
    exp["result"] = clean(exp["result"])
    if exp.get("sensitivity"):
        exp["sensitivity"] = clean(exp["sensitivity"])
    # Warnings mention hub display names ("Hub test A") — keep only the count.
    exp["result"]["warningCount"] = len(exp["result"].pop("warnings"))
    for h in exp["result"]["hubs"]:
        h["warningCount"] = len(h.pop("warnings"))
        if h.get("flow"):
            h["flow"]["warningCount"] = len(h["flow"].pop("warnings"))
        if h.get("capacityWarning"):
            h["capacityWarning"].pop("message", None)
    f = exp["result"]["flow"]
    f["warningCount"] = len(f.pop("warnings"))
    for fh in f["hubs"]:
        fh["warningCount"] = len(fh.pop("warnings"))
    (d / "snapshot.json").write_text(json.dumps(snap, ensure_ascii=False, indent=1), encoding="utf-8")
    (d / "expected.json").write_text(json.dumps(exp, ensure_ascii=False, indent=1), encoding="utf-8")
    print(case.name)
