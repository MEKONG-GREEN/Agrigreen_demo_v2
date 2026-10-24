#!/usr/bin/env python3
"""Bootstrap reference data through authenticated Admin APIs, never SQL or KN->HTX writes.

Provide an operator-owned bearer token FILE, province code and approved GeoJSON
with commune properties.code. Synthetic geometry is only suitable for isolated CI.
"""
import argparse
import hashlib
import json
from pathlib import Path
import urllib.error
import urllib.parse
import urllib.request


class Bootstrap:
    def __init__(self, app_url, admin_url, token, prefix="BOOT"):
        self.app = app_url.rstrip("/")
        self.admin = admin_url.rstrip("/")
        self.token = token
        self.prefix = prefix

    def request(self, method, path, body=None, admin=False, key=None):
        raw = None if body is None else json.dumps(body, ensure_ascii=False).encode("utf-8")
        headers = {"Authorization": "Bearer " + self.token, "Accept": "application/json"}
        if raw is not None:
            headers["Content-Type"] = "application/geo+json" if path.startswith("/v1/geo/admin-boundaries/import") else "application/json"
        if key:
            headers["Idempotency-Key"] = key
        req = urllib.request.Request((self.admin if admin else self.app) + path, data=raw, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=90) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            try:
                code = json.load(error).get("code", "unknown")
            except (ValueError, AttributeError):
                code = "unknown"
            # Never echo request headers, bearer tokens, returned PII or URLs with query secrets.
            raise RuntimeError(f"{method} {path.split('?')[0]}: HTTP {error.code} {code}") from None

    def org(self, kind, code, name, unit="", parent=""):
        body = {"kind": kind, "code": code, "name": name}
        if unit:
            body["admin_unit_code"] = unit
        if parent:
            body["parent_ref"] = parent
        try:
            response = self.request("POST", "/v1/orgs", body, admin=True, key="bootstrap-org-" + code)
            return response["org"]["ref"]
        except RuntimeError as error:
            if "party.org_code_taken" not in str(error):
                raise
            cursor = ""
            seen = set()
            while True:
                query = {"kind": kind, "limit": 200}
                if cursor:
                    query["cursor"] = cursor
                page = self.request("GET", "/v1/orgs?" + urllib.parse.urlencode(query))
                for org in page.get("items", []):
                    if org.get("code") == code:
                        if (org.get("admin_unit_code") or "") != unit or (org.get("parent_ref") or "") != parent:
                            raise RuntimeError("existing bootstrap org has a different unit/parent") from None
                        return org["ref"]
                cursor = page.get("next_cursor") or ""
                if not cursor or cursor in seen:
                    break
                seen.add(cursor)
            raise RuntimeError("existing org not visible; do not create a replacement") from None

    def run(self, province, geojson, source):
        features = geojson.get("features", [])
        if geojson.get("type") != "FeatureCollection" or not features:
            raise ValueError("boundaries must be a nonempty GeoJSON FeatureCollection")
        codes = [str(f.get("properties", {}).get("code", "")) for f in features]
        if any(not code or not code.isdecimal() for code in codes) or len(codes) != len(set(codes)):
            raise ValueError("each commune needs one unique numeric properties.code")
        # Seed gazetteer/catalogs/parameters first; all follow-up reads are tenant scoped.
        self.request("POST", "/v1/masterdata/seed", {}, admin=True, key="bootstrap-masterdata-v1")
        province_unit = self.request("GET", "/v1/masterdata/admin-units/" + province)
        province_unit = province_unit.get("unit", province_unit)
        if province_unit.get("level") != "province":
            raise ValueError("province code is not a current province")
        units = []
        for code in codes:
            unit = self.request("GET", "/v1/masterdata/admin-units/" + code)
            unit = unit.get("unit", unit)
            if unit.get("level") != "commune" or unit.get("parent_code") != province:
                raise ValueError("boundary commune does not belong to selected province")
            units.append(unit)
        # Validate geometry before creating the organization tree. Real geometries are
        # supplied by the operator; no rectangular fallbacks or copied HTX records.
        path = "/v1/geo/admin-boundaries/import?" + urllib.parse.urlencode({"source": source})
        digest = hashlib.sha256((source + json.dumps(geojson, sort_keys=True)).encode()).hexdigest()[:32]
        preview = self.request("POST", path + "&dry_run=true", geojson, admin=True, key="bootstrap-boundaries-preview-" + digest)
        if preview.get("rejected") or preview.get("accepted") != len(features):
            raise ValueError("boundary preview rejected one or more communes")
        national = self.org("ttkn_national", self.prefix + "-TTKNQG", "Trung tÃ¢m Khuyáº¿n nÃ´ng Quá»‘c gia")
        provincial = self.org("ttkn_province", self.prefix + "-TTKN-" + province,
                              "Trung tÃ¢m Khuyáº¿n nÃ´ng " + province_unit["name"], province, national)
        teams = []
        for unit in units:
            teams.append(self.org("extension_team", self.prefix + "-TKN-" + unit["code"],
                                  "Tá»• khuyáº¿n nÃ´ng " + unit["name"], unit["code"], provincial))
        applied = self.request("POST", path, geojson, admin=True, key="bootstrap-boundaries-apply-" + digest)
        if applied.get("rejected") or applied.get("accepted") != len(features):
            raise RuntimeError("boundary import incomplete; keep tree and rerun with corrected file")
        for code in codes:
            self.request("GET", "/v1/geo/admin-boundaries/" + code)
        return {"national_ref": national, "province_ref": provincial, "team_refs": teams,
                "commune_boundaries": len(features)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--app-url", default="http://127.0.0.1:8080")
    parser.add_argument("--admin-url", default="http://127.0.0.1:8082")
    parser.add_argument("--token-file", required=True)
    parser.add_argument("--province", required=True)
    parser.add_argument("--boundaries", required=True)
    parser.add_argument("--source", required=True)
    parser.add_argument("--prefix", default="BOOT")
    args = parser.parse_args()
    if not args.province.isdecimal() or not args.prefix.replace("-", "").isalnum():
        parser.error("province/prefix invalid")
    token = Path(args.token_file).read_text(encoding="utf-8").strip()
    if not token:
        parser.error("empty token file")
    geojson = json.loads(Path(args.boundaries).read_text(encoding="utf-8-sig"))
    result = Bootstrap(args.app_url, args.admin_url, token, args.prefix).run(args.province, geojson, args.source)
    print(json.dumps(result, ensure_ascii=False))
    print("--- PASS: reference catalogs, organization tree and supplied commune boundaries")


if __name__ == "__main__":
    main()
