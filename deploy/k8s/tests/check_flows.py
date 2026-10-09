#!/usr/bin/env python3
"""Kiểm bảng luồng ADR-0008 trên manifest Helm đã render (G4, R-81).

Render chart deploy/k8s/charts/agrigreen (helm template) cho dev/staging/prod rồi:
  1. mô phỏng ngữ nghĩa NetworkPolicy của Kubernetes (Pod bị cô lập khi có policy chọn nó; luồng
     được phép khi CẢ egress ở nguồn VÀ ingress ở đích cho phép) và khẳng định:
       - mọi luồng của bảng ADR-0008 / mục 17 (EXPECTED_ALLOWED) được phép;
       - các luồng cấm (FORBIDDEN) bị chặn: mkg → DB pf, pf → DB mkg/ts, mọi thứ → DB vault trừ
         vault, Internet ra từ DB/vault, Internet vào thẳng ứng dụng/ingest...
  2. kiểm cấu trúc: mặc định từ chối ở mọi namespace, cứng hóa Pod, probe, Secret đúng vùng,
     URL DB đúng vùng, quyền RabbitMQ (không tài khoản nền tảng nào trên mkg.internal),
     max_locks_per_transaction = 256, luật Prometheus trùng bản gốc deploy/observability.
  3. Public API (G5, ADR-0022/0025) trên mọi hồ sơ, kể cả prod-phasek (= values-prod + values-phasek):
     api-public chỉ nhận từ Ingress (F08), chỉ ra PgBouncer pf (F19) + kho đối tượng 443 (F29, khóa chỉ
     đọc pf-public-s3) + DNS + OTLP (+ Valkey/Keycloak/scrape khi cấu hình); hồ sơ gốc TẮT (0 bản sao, không host partner), Phase K BẬT; values-phasek.yaml
     chỉ có cấu hình và số bản sao (R-01, PHASEK_ALLOWED).

Bảng luồng mong đợi ở ĐÂY độc lập với values.yaml (nguồn sinh policy): sửa values mà làm hỏng
một luồng ADR thì kiểm thử hỏng.

Chạy: python deploy/k8s/tests/check_flows.py   (cần helm trên PATH hoặc HELM=<đường dẫn>; PyYAML)
      python deploy/k8s/tests/check_flows.py --rendered prod=out.yaml   (dùng bản đã render)
Thoát 0 khi mọi kiểm đạt.
"""
from __future__ import annotations

import argparse
import ipaddress
import os
import re
import shutil
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path

try:
    import yaml
except ImportError:  # pragma: no cover
    sys.exit("cần PyYAML: python -m pip install --user pyyaml")

ROOT = Path(__file__).resolve().parents[3]
CHART = ROOT / "deploy" / "k8s" / "charts" / "agrigreen"
# Hồ sơ = môi trường + lớp giá trị chồng lên. prod-phasek: Phase K (ADR-0025) chồng lên production.
PROFILES = {
    "dev": ["values-dev.yaml"],
    "staging": ["values-staging.yaml"],
    "prod": ["values-prod.yaml"],
    "prod-phasek": ["values-prod.yaml", "values-phasek.yaml"],
}
ENVS = tuple(PROFILES)


def base_env(env: str) -> str:
    """Môi trường gốc của hồ sơ (prod-phasek → prod)."""
    return env.split("-", 1)[0]


def is_phasek(env: str) -> bool:
    return env.endswith("-phasek")


# R-01: lớp giá trị Phase K chỉ đổi cấu hình và số bản sao. Mọi lá (đường dẫn) của values-phasek.yaml
# phải khớp một mẫu dưới đây — không image, không luồng mạng, không tài nguyên mới.
PHASEK_ALLOWED = (
    r"apps\.[a-z0-9-]+\.replicas",
    r"apps\.[a-z0-9-]+\.(hpa|pdb)\.[a-zA-Z]+",
    r"apps\.[a-z0-9-]+\.env\.AG_[A-Z0-9_]+",
    r"apps\.api-public\.(metrics|oidc)\.enabled",
    r"external\.valkey",
    r"ingress\.optionalHosts\.[a-z0-9-]+\.enabled",
    r"externalSecrets\.secrets\.[a-z]+\.[a-z0-9-]+\.enabled",
)
# Vai DB mà api-public được nhận — W2.6 (ADR-0028): CHỈ vai đăng nhập đơn vị app_public.
PUBLIC_DB_KEYS = {"AG_DB_PASSWORD_APP_PUBLIC"}

NS = {
    "edge": "agrigreen-edge-ingress",
    "pf": "agrigreen-pf",
    "mkg": "agrigreen-mkg",
    "vault": "agrigreen-vault",
    "data": "agrigreen-data",
    "iam": "agrigreen-iam",
    "ops": "agrigreen-ops",
    "pki": "agrigreen-pki",
}
MANAGED = set(NS.values())


# --------------------------------------------------------------------------- mô hình
@dataclass(frozen=True)
class Pod:
    ns: str
    name: str
    labels: tuple  # tuple((k, v), ...)

    def lab(self) -> dict:
        return dict(self.labels)


@dataclass(frozen=True)
class IP:
    addr: str


def pod(ns: str, name: str, **labels) -> Pod:
    return Pod(ns, name, tuple(sorted(labels.items())))


def match_selector(sel: dict | None, labels: dict) -> bool:
    if sel is None:
        return True
    for k, v in (sel.get("matchLabels") or {}).items():
        if labels.get(k) != v:
            return False
    for e in sel.get("matchExpressions") or []:
        k, op, vals = e["key"], e["operator"], e.get("values") or []
        if op == "In" and labels.get(k) not in vals:
            return False
        if op == "NotIn" and labels.get(k) in vals:
            return False
        if op == "Exists" and k not in labels:
            return False
        if op == "DoesNotExist" and k in labels:
            return False
    return True


def peer_matches(peer: dict, policy_ns: str, target, ns_labels: dict) -> bool:
    if "ipBlock" in peer:
        if not isinstance(target, IP):
            return False
        net = ipaddress.ip_network(peer["ipBlock"]["cidr"])
        a = ipaddress.ip_address(target.addr)
        if a not in net:
            return False
        return not any(a in ipaddress.ip_network(x) for x in peer["ipBlock"].get("except") or [])
    if not isinstance(target, Pod):
        return False
    if "namespaceSelector" in peer:
        if not match_selector(peer["namespaceSelector"], ns_labels.get(target.ns, {"kubernetes.io/metadata.name": target.ns})):
            return False
    elif target.ns != policy_ns:
        return False
    if "podSelector" in peer:
        return match_selector(peer["podSelector"], target.lab())
    return True


def port_matches(rule_ports, port: int, proto: str) -> bool:
    if not rule_ports:
        return True
    for p in rule_ports:
        if (p.get("protocol") or "TCP") != proto:
            continue
        if "port" not in p or p["port"] == port:
            return True
        if isinstance(p.get("endPort"), int) and p["port"] <= port <= p["endPort"]:
            return True
    return False


class Model:
    def __init__(self, docs: list[dict]):
        self.policies = [d for d in docs if d.get("kind") == "NetworkPolicy"]
        self.ns_labels = {}
        for d in docs:
            if d.get("kind") == "Namespace":
                labels = dict(d["metadata"].get("labels") or {})
                labels["kubernetes.io/metadata.name"] = d["metadata"]["name"]
                self.ns_labels[d["metadata"]["name"]] = labels

    def _selecting(self, p: Pod, kind: str):
        for np in self.policies:
            if np["metadata"]["namespace"] != p.ns:
                continue
            if kind not in np["spec"].get("policyTypes", ["Ingress"]):
                continue
            if match_selector(np["spec"].get("podSelector") or {}, p.lab()):
                yield np

    def egress_ok(self, src, dst, port: int, proto: str) -> bool:
        if isinstance(src, IP) or src.ns not in MANAGED:
            return True
        pols = list(self._selecting(src, "Egress"))
        if not pols:
            return True
        for np in pols:
            for rule in np["spec"].get("egress") or []:
                peers = rule.get("to")
                if peers is None:
                    ok_peer = True
                else:
                    ok_peer = any(peer_matches(pe, src.ns, dst, self.ns_labels) for pe in peers)
                if ok_peer and port_matches(rule.get("ports"), port, proto):
                    return True
        return False

    def ingress_ok(self, src, dst, port: int, proto: str) -> bool:
        if isinstance(dst, IP) or dst.ns not in MANAGED:
            return True
        pols = list(self._selecting(dst, "Ingress"))
        if not pols:
            return True
        for np in pols:
            for rule in np["spec"].get("ingress") or []:
                peers = rule.get("from")
                if peers is None:
                    ok_peer = True
                else:
                    ok_peer = any(peer_matches(pe, dst.ns, src, self.ns_labels) for pe in peers)
                if ok_peer and port_matches(rule.get("ports"), port, proto):
                    return True
        return False

    def allowed(self, src, dst, port: int, proto: str = "TCP") -> bool:
        return self.egress_ok(src, dst, port, proto) and self.ingress_ok(src, dst, port, proto)


# --------------------------------------------------------------------------- Pod trong cụm
def pods_from(docs: list[dict]) -> dict[str, Pod]:
    """Pod của chart (Deployment/Job/CronJob) + Pod do operator tạo (CNPG, RabbitMQ) + Pod hệ thống."""
    out: dict[str, Pod] = {}

    def add(key, p):
        out[key] = p

    for d in docs:
        k = d.get("kind")
        md = d.get("metadata", {})
        tmpl = None
        if k == "Deployment":
            tmpl = d["spec"]["template"]
        elif k == "Job":
            tmpl = d["spec"]["template"]
        elif k == "CronJob":
            tmpl = d["spec"]["jobTemplate"]["spec"]["template"]
        if tmpl is not None:
            labels = tmpl["metadata"]["labels"]
            add(f"{md['namespace']}/{labels['app.kubernetes.io/name']}", Pod(md["namespace"], labels["app.kubernetes.io/name"], tuple(sorted(labels.items()))))
        if k == "Cluster" and d["apiVersion"].startswith("postgresql.cnpg.io"):
            n = md["name"]
            add(f"{md['namespace']}/{n}", pod(md["namespace"], n, **{"cnpg.io/cluster": n, "cnpg.io/podRole": "instance", "cnpg.io/instanceName": n + "-1"}))
        if k == "Pooler":
            n = md["name"]
            add(f"{md['namespace']}/{n}", pod(md["namespace"], n, **{"cnpg.io/poolerName": n, "cnpg.io/podRole": "pooler"}))
        if k == "RabbitmqCluster":
            n = md["name"]
            add(f"{md['namespace']}/{n}", pod(md["namespace"], n, **{"app.kubernetes.io/name": n, "app.kubernetes.io/component": "rabbitmq", "app.kubernetes.io/part-of": "rabbitmq"}))
    # Pod ngoài chart (nhãn = systemSelectors trong values.yaml, là giả định cần giữ khi cài chart khác).
    add("edge/traefik", pod(NS["edge"], "traefik", **{"app.kubernetes.io/name": "traefik"}))
    add("iam/keycloak", pod(NS["iam"], "keycloak", **{"app.kubernetes.io/name": "keycloak"}))
    add("ops/prometheus", pod(NS["ops"], "prometheus", **{"app.kubernetes.io/name": "prometheus"}))
    add("ops/grafana", pod(NS["ops"], "grafana", **{"app.kubernetes.io/name": "grafana"}))
    add("cnpg/operator", pod("cnpg-system", "cnpg", **{"app.kubernetes.io/name": "cloudnative-pg"}))
    add("dns", pod("kube-system", "coredns", **{"k8s-app": "kube-dns"}))
    return out


# --------------------------------------------------------------------------- kiểm
@dataclass
class Report:
    fails: list = field(default_factory=list)
    passes: int = 0

    def check(self, ok: bool, msg: str):
        if ok:
            self.passes += 1
        else:
            self.fails.append(msg)


def first_host(cidrs: list[str]) -> str:
    net = ipaddress.ip_network(cidrs[0])
    return str(next(net.hosts())) if net.num_addresses > 1 else str(net.network_address)


def flow_checks(env: str, docs: list[dict], values: dict, r: Report):
    m = Model(docs)
    pods = pods_from(docs)
    ext = values["external"]
    ipof = {k: IP(first_host(v)) for k, v in ext.items() if v}
    internet = IP("8.8.8.8")
    pf, mkg, vault, data, edge, iam, ops, pki = (NS[z] for z in ("pf", "mkg", "vault", "data", "edge", "iam", "ops", "pki"))
    missing: set[str] = set()

    def g(k):
        if k not in pods:
            missing.add(k)
            return None
        return pods[k]

    # ---- ADR-0008 / mục 17: luồng PHẢI được phép
    allowed = [
        ("Biên → DMZ: Internet → Ingress 443", internet, g("edge/traefik"), 8443),
        ("DMZ → Ứng dụng: Ingress → api-platform", g("edge/traefik"), g(f"{pf}/api-platform"), 8080),
        ("DMZ → Ứng dụng: Ingress → Admin API", g("edge/traefik"), g(f"{pf}/api-platform"), 8082),
        ("DMZ → Ứng dụng: Ingress → web-ops", g("edge/traefik"), g(f"{pf}/web-ops"), 3000),
        ("DMZ → Ứng dụng: Ingress → api-internal", g("edge/traefik"), g(f"{mkg}/api-internal"), 8081),
        ("DMZ → Ứng dụng: Ingress → Admin MKG", g("edge/traefik"), g(f"{mkg}/api-internal"), 8083),
        ("DMZ → Ứng dụng: Ingress → web-internal", g("edge/traefik"), g(f"{mkg}/web-internal"), 3001),
        ("DMZ → Keycloak (OIDC)", g("edge/traefik"), g("iam/keycloak"), 8080),
        ("Ứng dụng → Dữ liệu: api-platform → PgBouncer pf", g(f"{pf}/api-platform"), g(f"{data}/pg-pf-pooler"), 5432),
        ("Ứng dụng → Dữ liệu: worker → PgBouncer pf", g(f"{pf}/worker"), g(f"{data}/pg-pf-pooler"), 5432),
        ("Ứng dụng → Dữ liệu: worker → pf trực tiếp (khóa leader)", g(f"{pf}/worker"), g(f"{data}/pg-pf"), 5432),
        ("Ứng dụng → Dữ liệu: outbox pf → pf trực tiếp", g(f"{pf}/outbox-publisher-pf"), g(f"{data}/pg-pf"), 5432),
        ("Ứng dụng → Dữ liệu: outbox pf → RabbitMQ", g(f"{pf}/outbox-publisher-pf"), g(f"{data}/rabbitmq"), 5672),
        ("Ứng dụng → Dữ liệu: worker → RabbitMQ", g(f"{pf}/worker"), g(f"{data}/rabbitmq"), 5672),
        ("Ứng dụng → Dữ liệu: optimizer → RabbitMQ", g(f"{pf}/optimizer"), g(f"{data}/rabbitmq"), 5672),
        ("Ứng dụng → Dữ liệu: api-platform → RabbitMQ management (DLQ)", g(f"{pf}/api-platform"), g(f"{data}/rabbitmq"), 15672),
        ("Ứng dụng → Dữ liệu: api-internal → PgBouncer mkg", g(f"{mkg}/api-internal"), g(f"{data}/pg-mkg-pooler"), 5432),
        ("Ứng dụng → Dữ liệu: api-internal → mkg trực tiếp (leader hub_capacity_daily)", g(f"{mkg}/api-internal"), g(f"{data}/pg-mkg"), 5432),
        ("Ứng dụng → Dữ liệu: worker-internal → PgBouncer mkg", g(f"{mkg}/worker-internal"), g(f"{data}/pg-mkg-pooler"), 5432),
        ("Ứng dụng → Dữ liệu: ingest → PgBouncer mkg", g(f"{mkg}/ingest"), g(f"{data}/pg-mkg-pooler"), 5432),
        ("Ứng dụng → Dữ liệu: ingest → PgBouncer ts", g(f"{mkg}/ingest"), g(f"{data}/pg-ts-pooler"), 5432),
        ("Ứng dụng → Dữ liệu: outbox mkg → mkg trực tiếp", g(f"{mkg}/outbox-publisher-mkg"), g(f"{data}/pg-mkg"), 5432),
        ("Ứng dụng → Dữ liệu: outbox mkg → ts trực tiếp", g(f"{mkg}/outbox-publisher-mkg"), g(f"{data}/pg-ts"), 5432),
        ("Ứng dụng → Dữ liệu: outbox mkg → RabbitMQ", g(f"{mkg}/outbox-publisher-mkg"), g(f"{data}/rabbitmq"), 5672),
        ("Ứng dụng → Dữ liệu: worker-internal → RabbitMQ", g(f"{mkg}/worker-internal"), g(f"{data}/rabbitmq"), 5672),
        ("Ứng dụng → Định danh: api-platform → identity-vault (mTLS)", g(f"{pf}/api-platform"), g(f"{vault}/identity-vault"), 8090),
        ("Ứng dụng Internal (mTLS): optimizer → api-platform:8084", g(f"{pf}/optimizer"), g(f"{pf}/api-platform"), 8084),
        ("Ứng dụng → Keycloak (JWKS, R-54)", g(f"{pf}/api-platform"), g("iam/keycloak"), 8080),
        ("Ứng dụng → Keycloak: api-internal (client credentials)", g(f"{mkg}/api-internal"), g("iam/keycloak"), 8080),
        ("Tiêu thụ không đặc quyền: api-internal → Application API qua DMZ", g(f"{mkg}/api-internal"), g("edge/traefik"), 8443),
        ("Định danh: identity-vault → PgBouncer vault", g(f"{vault}/identity-vault"), g(f"{vault}/pg-vault-pooler"), 5432),
        ("Định danh: PgBouncer vault → PostgreSQL vault", g(f"{vault}/pg-vault-pooler"), g(f"{vault}/pg-vault"), 5432),
        ("Dữ liệu: PgBouncer pf → PostgreSQL pf", g(f"{data}/pg-pf-pooler"), g(f"{data}/pg-pf"), 5432),
        ("Dữ liệu: PgBouncer ts → PostgreSQL ts", g(f"{data}/pg-ts-pooler"), g(f"{data}/pg-ts"), 5432),
        ("Dữ liệu: sao chép trong cụm mkg", g(f"{data}/pg-mkg"), g(f"{data}/pg-mkg"), 5432),
        ("Quản trị cụm: operator CNPG → PostgreSQL", g("cnpg/operator"), g(f"{data}/pg-pf"), 8000),
        ("Quản trị cụm: operator CNPG → PostgreSQL vault", g("cnpg/operator"), g(f"{vault}/pg-vault"), 8000),
        ("Dữ liệu: Job migrate pf → PostgreSQL pf", g(f"{data}/db-migrate-pf"), g(f"{data}/pg-pf"), 5432),
        ("Định danh: Job migrate vault → PostgreSQL vault", g(f"{vault}/db-migrate-vault"), g(f"{vault}/pg-vault"), 5432),
        ("Định danh: neo chuỗi vault → PostgreSQL vault", g(f"{vault}/anchor-vault"), g(f"{vault}/pg-vault"), 5432),
        ("Neo chuỗi mkg → ts trực tiếp", g(f"{mkg}/anchor-mkg"), g(f"{data}/pg-ts"), 5432),
        ("Quản trị → Dữ liệu: Prometheus → RabbitMQ", g("ops/prometheus"), g(f"{data}/rabbitmq"), 15692),
        ("Quản trị → Dữ liệu: Prometheus → PostgreSQL", g("ops/prometheus"), g(f"{data}/pg-pf"), 9187),
        ("Quản trị → Định danh: Prometheus → PostgreSQL vault", g("ops/prometheus"), g(f"{vault}/pg-vault"), 9187),
        ("Quản trị: sql_exporter → PgBouncer mkg", g(f"{ops}/sql-exporter"), g(f"{data}/pg-mkg-pooler"), 5432),
        ("Quản trị: sql_exporter → PgBouncer ts", g(f"{ops}/sql-exporter"), g(f"{data}/pg-ts-pooler"), 5432),
        ("Ứng dụng → Quản trị: OTLP api-platform", g(f"{pf}/api-platform"), g(f"{ops}/otel-collector"), 4317),
        ("Ứng dụng → Quản trị: OTLP ingest", g(f"{mkg}/ingest"), g(f"{ops}/otel-collector"), 4317),
        ("Dữ liệu: cụm RabbitMQ (Erlang dist)", g(f"{data}/rabbitmq"), g(f"{data}/rabbitmq"), 25672),
        ("Keycloak → DB Keycloak", g("iam/keycloak"), g(f"{data}/pg-iam"), 5432),
    ]
    dns_srcs = ["pf/api-platform", "mkg/ingest", "vault/identity-vault", "data/pg-pf", "ops/otel-collector", "pki/step-ca"]
    for s in dns_srcs:
        zone, name = s.split("/")
        key = f"{NS[zone]}/{name}"
        if key in pods:
            r.check(m.allowed(pods[key], g("dns"), 53, "UDP"), f"[{env}] DNS UDP phải được phép: {s}")
    ext_allowed = [
        ("Biên OT → ingest (VPN + mTLS)", "otVpn", None, f"{mkg}/ingest", 8443),
        ("Quản trị qua VPN → Grafana", "adminVpn", None, "ops/grafana", 3000),
        ("Quản trị qua VPN → CA thiết bị", "adminVpn", None, f"{pki}/step-ca", 9000),
        ("Định danh → KMS", None, "kms", f"{vault}/identity-vault", 443),
        ("Ứng dụng → kho S3 trong lãnh thổ", None, "objectStorage", f"{pf}/api-platform", 443),
        ("Dữ liệu → kho sao lưu trong lãnh thổ", None, "objectStorage", f"{data}/pg-pf", 443),
        ("Định danh → kho sao lưu RIÊNG", None, "objectStorage", f"{vault}/pg-vault", 443),
        ("Neo chuỗi mkg → kho S3", None, "objectStorage", f"{mkg}/anchor-mkg", 443),
        ("Public API → kho S3 (tệp xuất, F29)", None, "objectStorage", f"{pf}/api-public", 443),
        ("worker → SMTP trong lãnh thổ", None, "smtp", f"{pf}/worker", 587),
        ("CNPG → API server", None, "kubeAPI", f"{data}/pg-mkg", 443),
        ("CA thiết bị → HSM", None, "hsm", f"{pki}/step-ca", 1792),
    ]
    for desc, src_ext, dst_ext, podkey, port in ext_allowed:
        if (src_ext or dst_ext) not in ipof or podkey not in pods:
            if base_env(env) == "prod":
                r.check(False, f"[{env}] thiếu dải/Pod cho luồng: {desc}")
            continue
        if src_ext:
            ok = m.allowed(ipof[src_ext], pods[podkey], port)
        else:
            ok = m.allowed(pods[podkey], ipof[dst_ext], port)
        r.check(ok, f"[{env}] ADR-0008 luồng bị chặn: {desc}")
    for desc, s, d, port in allowed:
        if s is None or d is None:
            continue
        r.check(m.allowed(s, d, port), f"[{env}] ADR-0008 luồng bị chặn: {desc} ({port})")

    # ---- luồng CẤM
    forbidden = []
    for a in ("api-internal", "worker-internal", "ingest", "outbox-publisher-mkg", "web-internal", "anchor-mkg"):
        for t in ("pg-pf-pooler", "pg-pf"):
            forbidden.append((f"mkg/{a} → DB pf ({t})", g(f"{mkg}/{a}"), g(f"{data}/{t}"), 5432))
    for a in ("api-platform", "worker", "outbox-publisher-pf", "optimizer", "web-ops"):
        for t in ("pg-mkg-pooler", "pg-mkg", "pg-ts-pooler", "pg-ts"):
            forbidden.append((f"pf/{a} → DB mkg/ts ({t})", g(f"{pf}/{a}"), g(f"{data}/{t}"), 5432))
    vault_db = (g(f"{vault}/pg-vault"), g(f"{vault}/pg-vault-pooler"))
    for s in (f"{pf}/api-platform", f"{pf}/worker", f"{pf}/outbox-publisher-pf", f"{mkg}/api-internal", f"{mkg}/ingest",
              f"{mkg}/outbox-publisher-mkg", f"{data}/pg-pf", f"{data}/rabbitmq", f"{data}/db-migrate-pf", f"{ops}/sql-exporter",
              "edge/traefik", "iam/keycloak", "ops/prometheus"):
        for d in vault_db:
            forbidden.append((f"{s} → DB vault ({d.name})", g(s), d, 5432))
    forbidden += [
        ("Ingress → PgBouncer pf (R-80)", g("edge/traefik"), g(f"{data}/pg-pf-pooler"), 5432),
        ("Ingress → RabbitMQ (R-80)", g("edge/traefik"), g(f"{data}/rabbitmq"), 5672),
        ("Ingress → identity-vault", g("edge/traefik"), g(f"{vault}/identity-vault"), 8090),
        ("Ingress → Internal API 8084", g("edge/traefik"), g(f"{pf}/api-platform"), 8084),
        ("Ingress → ingest", g("edge/traefik"), g(f"{mkg}/ingest"), 8443),
        ("Internet → api-platform (bỏ qua DMZ)", internet, g(f"{pf}/api-platform"), 8080),
        ("Internet → ingest (chỉ Biên OT)", internet, g(f"{mkg}/ingest"), 8443),
        ("Internet → RabbitMQ", internet, g(f"{data}/rabbitmq"), 5672),
        ("Internet → PostgreSQL", internet, g(f"{data}/pg-pf"), 5432),
        ("mkg → api-platform Pod trực tiếp (phải qua DMZ)", g(f"{mkg}/api-internal"), g(f"{pf}/api-platform"), 8080),
        ("mkg → Internal API pf", g(f"{mkg}/api-internal"), g(f"{pf}/api-platform"), 8084),
        ("mkg → identity-vault", g(f"{mkg}/api-internal"), g(f"{vault}/identity-vault"), 8090),
        ("ingest → identity-vault", g(f"{mkg}/ingest"), g(f"{vault}/identity-vault"), 8090),
        ("worker → identity-vault", g(f"{pf}/worker"), g(f"{vault}/identity-vault"), 8090),
        ("web-ops → PgBouncer pf", g(f"{pf}/web-ops"), g(f"{data}/pg-pf-pooler"), 5432),
        ("pf → mkg api-internal (Pod-tới-Pod)", g(f"{pf}/api-platform"), g(f"{mkg}/api-internal"), 8081),
        ("pf → worker-internal", g(f"{pf}/worker"), g(f"{mkg}/worker-internal"), 8086),
        ("Prometheus → identity-vault", g("ops/prometheus"), g(f"{vault}/identity-vault"), 8090),
        ("PostgreSQL pf → PostgreSQL mkg (khác cụm)", g(f"{data}/pg-pf"), g(f"{data}/pg-mkg"), 5432),
        ("PgBouncer mkg → PostgreSQL pf (khác cụm)", g(f"{data}/pg-mkg-pooler"), g(f"{data}/pg-pf"), 5432),
        ("PgBouncer pf → PostgreSQL vault", g(f"{data}/pg-pf-pooler"), g(f"{vault}/pg-vault"), 5432),
        ("Pod vùng nền tảng → CA thiết bị", g(f"{pf}/api-platform"), g(f"{pki}/step-ca"), 9000),
        ("ingest → CA thiết bị", g(f"{mkg}/ingest"), g(f"{pki}/step-ca"), 9000),
    ]
    for desc, s, d, port in forbidden:
        if s is None or d is None:
            continue
        r.check(not m.allowed(s, d, port), f"[{env}] luồng CẤM lại được phép: {desc} ({port})")
    # Internet ra từ vùng Dữ liệu / Định danh / ứng dụng: không đích Internet nào.
    for s in (f"{data}/pg-pf", f"{data}/pg-mkg", f"{data}/pg-ts", f"{data}/rabbitmq", f"{data}/pg-pf-pooler", f"{vault}/pg-vault",
              f"{vault}/identity-vault", f"{mkg}/ingest", f"{pf}/web-ops", f"{pf}/api-platform", f"{mkg}/api-internal", f"{pki}/step-ca"):
        for port in (80, 443, 5432):
            if g(s) is not None:
                r.check(not m.allowed(g(s), internet, port), f"[{env}] ra Internet phải bị chặn: {s} → 8.8.8.8:{port}")
    # dev tắt CA thiết bị (step-ca); môi trường khác phải có đủ Pod.
    if base_env(env) == "dev":
        missing.discard(f"{pki}/step-ca")
    for k in sorted(missing):
        r.check(False, f"[{env}] thiếu Pod {k} trong manifest")
    if "kms" in ipof:
        r.check(not m.allowed(g(f"{pf}/api-platform"), ipof["kms"], 443), f"[{env}] api-platform không được tới KMS")
    if "otVpn" in ipof:
        r.check(not m.allowed(ipof["otVpn"], g(f"{pf}/api-platform"), 8080), f"[{env}] Biên OT chỉ tới ingest")
    if "objectStorage" in ipof:
        r.check(not m.allowed(g(f"{mkg}/ingest"), ipof["objectStorage"], 443), f"[{env}] ingest không cần kho S3")
        r.check(not m.allowed(g(f"{vault}/identity-vault"), ipof["objectStorage"], 443), f"[{env}] identity-vault không ra kho S3")


def structure_checks(env: str, docs: list[dict], values: dict, r: Report):
    by_kind: dict[str, list] = {}
    for d in docs:
        by_kind.setdefault(d["kind"], []).append(d)
    # Mặc định từ chối ở mọi namespace của chart.
    nss = {d["metadata"]["name"] for d in by_kind.get("Namespace", [])}
    r.check(nss == MANAGED, f"[{env}] namespace: {sorted(nss)}")
    for ns in MANAGED:
        dd = [p for p in by_kind.get("NetworkPolicy", []) if p["metadata"]["namespace"] == ns and p["metadata"]["name"] == "default-deny-all"]
        ok = len(dd) == 1 and dd[0]["spec"]["podSelector"] == {} and set(dd[0]["spec"]["policyTypes"]) == {"Ingress", "Egress"} \
            and not dd[0]["spec"].get("ingress") and not dd[0]["spec"].get("egress")
        r.check(ok, f"[{env}] thiếu default-deny-all (Ingress+Egress) ở {ns}")
    # Mọi luồng trong values có hàng ADR.
    for f in values["networkPolicy"]["flows"]:
        r.check(bool(f.get("adr")), f"[{env}] luồng {f['id']} thiếu adr")

    es_names = {(d["metadata"]["namespace"], d["spec"]["target"]["name"]) for d in by_kind.get("ExternalSecret", [])}
    cert_names = {(d["metadata"]["namespace"], d["spec"]["secretName"]) for d in by_kind.get("Certificate", [])}
    cnpg_secrets = {(d["metadata"]["namespace"], d["metadata"]["name"] + "-superuser") for d in by_kind.get("Cluster", [])}
    forbid_prefix = {
        NS["pf"]: ("mkg-", "ts-", "vault-", "pg-", "rmq-", "amqp-mkg"),
        NS["mkg"]: ("pf-", "vault-", "pg-", "rmq-", "amqp-pf"),
        NS["vault"]: ("pf-", "mkg-", "ts-", "amqp-", "rmq-"),
    }
    forbid_db = {
        NS["pf"]: ("pg-mkg", "pg-ts", "pg-vault"),
        NS["mkg"]: ("pg-pf", "pg-vault"),
        NS["vault"]: ("pg-pf", "pg-mkg", "pg-ts"),
    }
    no_probe_ok = {"optimizer"}  # worker, outbox-publisher: AG_HEALTH_ADDR (cmd/internal/healthsrv, G4)
    workloads = []
    for d in by_kind.get("Deployment", []):
        workloads.append((d, d["spec"]["template"]["spec"]))
    for d in by_kind.get("Job", []):
        workloads.append((d, d["spec"]["template"]["spec"]))
    for d in by_kind.get("CronJob", []):
        workloads.append((d, d["spec"]["jobTemplate"]["spec"]["template"]["spec"]))
    for d, spec in workloads:
        ns, name = d["metadata"]["namespace"], d["metadata"]["name"]
        sc = spec.get("securityContext") or {}
        r.check(sc.get("runAsNonRoot") is True and (sc.get("seccompProfile") or {}).get("type") == "RuntimeDefault", f"[{env}] {ns}/{name}: Pod phải runAsNonRoot + seccomp")
        r.check(spec.get("automountServiceAccountToken") is False, f"[{env}] {ns}/{name}: không gắn token ServiceAccount")
        for c in (spec.get("initContainers") or []) + spec["containers"]:
            csc = c.get("securityContext") or {}
            r.check(csc.get("readOnlyRootFilesystem") is True and csc.get("allowPrivilegeEscalation") is False
                    and "ALL" in ((csc.get("capabilities") or {}).get("drop") or []), f"[{env}] {ns}/{name}/{c['name']}: cứng hóa container")
            res = c.get("resources") or {}
            r.check(bool(res.get("requests")) and bool(res.get("limits")), f"[{env}] {ns}/{name}/{c['name']}: thiếu requests/limits")
            refs = [e["valueFrom"]["secretKeyRef"]["name"] for e in c.get("env") or [] if "valueFrom" in e and "secretKeyRef" in e["valueFrom"]]
            refs += [e["secretRef"]["name"] for e in c.get("envFrom") or [] if "secretRef" in e and not e["secretRef"].get("optional")]
            optional = [e["secretRef"]["name"] for e in c.get("envFrom") or [] if "secretRef" in e and e["secretRef"].get("optional")]
            for s in optional:  # Secret chỉ có khi một hồ sơ bật (vd pepper ở Phase K): vẫn phải đúng vùng
                if ns in forbid_prefix:
                    r.check(not s.startswith(forbid_prefix[ns]), f"[{env}] {ns}/{name}: Secret của vùng khác: {s}")
            # Đơn vị tắt bằng cấu hình (replicas 0, vd api-public ở ĐTK 1): Secret của nó chỉ có khi hồ sơ bật.
            off = d["kind"] == "Deployment" and d["spec"].get("replicas") == 0
            for s in refs:
                if ns in forbid_prefix:
                    r.check(not s.startswith(forbid_prefix[ns]), f"[{env}] {ns}/{name}: Secret của vùng khác: {s}")
                if base_env(env) != "dev" and not off:
                    r.check((ns, s) in es_names or (ns, s) in cnpg_secrets, f"[{env}] {ns}/{name}: Secret {s} không có nguồn ExternalSecret/CNPG trong cùng namespace")
            for e in c.get("env") or []:
                v = str(e.get("value", ""))
                for bad in forbid_db.get(ns, ()):
                    r.check(bad + "-" not in v, f"[{env}] {ns}/{name}: {e['name']} trỏ tới DB vùng khác ({bad})")
            if d["kind"] == "Deployment" and name not in no_probe_ok:
                r.check("readinessProbe" in c and "livenessProbe" in c, f"[{env}] {ns}/{name}: thiếu readiness/liveness")
        for v in spec.get("volumes") or []:
            if "secret" in v:
                s = v["secret"]["secretName"]
                if ns in forbid_prefix:
                    r.check(not s.startswith(forbid_prefix[ns]), f"[{env}] {ns}/{name}: gắn Secret vùng khác {s}")
                # optional: Secret chỉ có khi một tính năng bật (vd mTLS của integration-gateway, W5.2).
                if base_env(env) != "dev" and not v["secret"].get("optional"):
                    r.check((ns, s) in es_names or (ns, s) in cert_names, f"[{env}] {ns}/{name}: volume Secret {s} không có nguồn")
    # PDB cho mọi Deployment nhiều bản; HPA cho api-platform, ingest (prod).
    pdbs = {(d["metadata"]["namespace"], d["metadata"]["name"]) for d in by_kind.get("PodDisruptionBudget", [])}
    for d in by_kind.get("Deployment", []):
        if (d["spec"].get("replicas") or 2) > 1:
            r.check((d["metadata"]["namespace"], d["metadata"]["name"]) in pdbs, f"[{env}] thiếu PDB: {d['metadata']['name']}")
    if base_env(env) != "dev":
        hpas = {d["metadata"]["name"] for d in by_kind.get("HorizontalPodAutoscaler", [])}
        r.check({"api-platform", "ingest"} <= hpas, f"[{env}] HPA cho api-platform/ingest: {hpas}")
    # PostgreSQL: max_locks_per_transaction = 256; cụm vault ở namespace Định danh.
    for d in by_kind.get("Cluster", []):
        if not d["apiVersion"].startswith("postgresql.cnpg.io"):
            continue
        r.check(d["spec"]["postgresql"]["parameters"].get("max_locks_per_transaction") == "256", f"[{env}] {d['metadata']['name']}: max_locks_per_transaction")
        if d["metadata"]["name"] == "pg-vault":
            r.check(d["metadata"]["namespace"] == NS["vault"], f"[{env}] pg-vault phải ở {NS['vault']}")
    # RabbitMQ: không tài khoản nền tảng nào có quyền trên mkg.internal; van chỉ ghi ag.events.
    users = {d["metadata"]["name"]: d for d in by_kind.get("User", [])}
    for p in by_kind.get("Permission", []):
        owner = p["metadata"]["labels"].get("agrigreen.vn/owner-zone")
        u = p["spec"]["userReference"]["name"]
        r.check(u in users, f"[{env}] Permission {p['metadata']['name']} trỏ tới User thiếu")
        if p["spec"]["vhost"] == "mkg.internal":
            r.check(owner == "mkg", f"[{env}] tài khoản vùng {owner} có quyền trên mkg.internal: {u}")
        if owner == "mkg" and p["spec"]["vhost"] == "/":
            perm = p["spec"]["permissions"]
            r.check(perm["read"] == "^$" and perm["write"] == r"^ag\.events$", f"[{env}] tài khoản mkg trên vhost '/' chỉ được phát lên ag.events: {u}")
    pf_users = [n for n, d in users.items() if d["metadata"]["labels"].get("agrigreen.vn/owner-zone") == "pf"]
    r.check(len(pf_users) >= 3, f"[{env}] thiếu tài khoản RabbitMQ nền tảng: {pf_users}")
    r.check(any(d["spec"]["name"] == "mkg.internal" for d in by_kind.get("Vhost", [])), f"[{env}] thiếu vhost mkg.internal")
    # Sao lưu (KN v7.7 NFR): mọi Stanza giữ >= 30 ngày phục hồi — theo ngày (fullType time), không theo số bản.
    for st in by_kind.get("Stanza", []):
        for repo in st["spec"]["stanzaConfiguration"].get("s3Repositories", []):
            rp = repo.get("retentionPolicy", {})
            r.check(rp.get("fullType") == "time" and int(rp.get("full", 0)) >= 30,
                    f"[{env}] Stanza {st['metadata']['name']}: retention {rp} chưa bảo đảm >= 30 ngày (cần fullType time, full >= 30)")
    # Luật Prometheus: bản trong chart trùng bản gốc; PrometheusRule mang đúng nhóm luật.
    src = ROOT / "deploy" / "observability" / "prometheus"
    rules = {d["metadata"]["name"]: d for d in by_kind.get("PrometheusRule", [])}
    for f in sorted(src.glob("*.rules.yml")):
        copy = CHART / "files" / "prometheus" / f.name
        r.check(copy.exists() and copy.read_bytes() == f.read_bytes(), f"bản sao luật lệch nguồn: {copy} (chép lại từ {f})")
        name = f.name[: -len(".rules.yml")]
        want = yaml.safe_load(f.read_text(encoding="utf-8"))["groups"]
        r.check(name in rules and rules[name]["spec"]["groups"] == want, f"[{env}] PrometheusRule {name} không khớp {f.name}")
    for f in sorted((ROOT / "deploy" / "observability" / "sql-exporter").glob("*.yml")):
        copy = CHART / "files" / "sql-exporter" / f.name
        r.check(copy.exists() and copy.read_bytes() == f.read_bytes(), f"bản sao collector lệch nguồn: {copy}")


# --------------------------------------------------------------------------- Public API (G5)
def public_api_checks(env: str, docs: list[dict], values: dict, r: Report):
    """ADR-0022 / ADR-0025: api-public chỉ nhận từ Ingress (F08), chỉ ra PgBouncer pf (F19) + kho đối tượng
    443 (F29) + DNS/OTLP (+ Valkey/Keycloak/scrape khi cấu hình); hồ sơ gốc TẮT, hồ sơ Phase K BẬT chỉ bằng
    cấu hình."""
    pf, data, vault, mkg, ops = NS["pf"], NS["data"], NS["vault"], NS["mkg"], NS["ops"]
    phasek = is_phasek(env)
    m = Model(docs)
    pods = pods_from(docs)
    ap = pods.get(f"{pf}/api-public")
    r.check(ap is not None, f"[{env}] thiếu Deployment api-public (image có ở mọi hồ sơ, chỉ số bản sao đổi)")
    if ap is None:
        return
    app_vals = values["apps"]["api-public"]
    metrics_on = bool((app_vals.get("metrics") or {}).get("enabled"))
    oidc_on = bool((app_vals.get("oidc") or {}).get("enabled"))
    internet = IP("8.8.8.8")

    def p(k):
        return pods.get(k)

    # ---- luồng phải được phép
    must = [
        ("F08 Ingress → api-public:8085", p("edge/traefik"), ap, 8085),
        ("F19 api-public → PgBouncer pf", ap, p(f"{data}/pg-pf-pooler"), 5432),
        ("F50 api-public → OTLP", ap, p(f"{ops}/otel-collector"), 4317),
    ]
    for desc, s, d, port in must:
        r.check(s is not None and d is not None and m.allowed(s, d, port), f"[{env}] Public API luồng bị chặn: {desc}")
    r.check(m.allowed(ap, p("dns"), 53, "UDP"), f"[{env}] Public API: DNS bị chặn")

    # ---- api-public CHỈ nhận từ Ingress, CHỈ cổng 8085
    srcs = [internet] + [x for x in (
        p(f"{pf}/api-platform"), p(f"{pf}/worker"), p(f"{pf}/web-ops"), p(f"{pf}/optimizer"), p(f"{pf}/outbox-publisher-pf"),
        p(f"{mkg}/api-internal"), p(f"{mkg}/ingest"), p(f"{mkg}/worker-internal"), p(f"{vault}/identity-vault"),
        p(f"{data}/rabbitmq"), p(f"{data}/pg-pf"), p(f"{data}/pg-pf-pooler"), p("iam/keycloak"), p("ops/prometheus"),
        p(f"{ops}/otel-collector")) if x is not None]
    for s in srcs:
        r.check(not m.allowed(s, ap, 8085), f"[{env}] luồng CẤM: {getattr(s, 'name', getattr(s, 'addr', s))} → api-public:8085 (chỉ Ingress, F08)")
    for port in (8080, 8082, 8084, 9464):
        if port == 9464 and metrics_on:
            continue
        r.check(not m.allowed(p("edge/traefik"), ap, port), f"[{env}] luồng CẤM: Ingress → api-public:{port}")
    prom_ok = m.allowed(p("ops/prometheus"), ap, 9464)
    r.check(prom_ok == metrics_on, f"[{env}] Prometheus → api-public:9464 phải {'mở' if metrics_on else 'đóng'} theo apps.api-public.metrics.enabled (F27)")

    # ---- api-public KHÔNG ra: vault, MQ, DB mkg/ts/vault, PostgreSQL pf trực tiếp, ứng dụng khác, Internet
    banned = [
        ("identity-vault", p(f"{vault}/identity-vault"), 8090),
        ("RabbitMQ AMQP", p(f"{data}/rabbitmq"), 5672),
        ("RabbitMQ management", p(f"{data}/rabbitmq"), 15672),
        ("PostgreSQL pf trực tiếp (chỉ qua PgBouncer)", p(f"{data}/pg-pf"), 5432),
        ("api-platform", p(f"{pf}/api-platform"), 8080),
        ("Admin API", p(f"{pf}/api-platform"), 8082),
        ("Internal API", p(f"{pf}/api-platform"), 8084),
        ("api-internal (MKG)", p(f"{mkg}/api-internal"), 8081),
        ("ingest", p(f"{mkg}/ingest"), 8091),
    ]
    for t in ("pg-mkg-pooler", "pg-mkg", "pg-ts-pooler", "pg-ts", "pg-iam"):
        banned.append((f"DB {t}", p(f"{data}/{t}"), 5432))
    for t in ("pg-vault-pooler", "pg-vault"):
        banned.append((f"DB vault {t}", p(f"{vault}/{t}"), 5432))
    if not oidc_on:
        banned.append(("Keycloak (chỉ khi apps.api-public.oidc.enabled)", p("iam/keycloak"), 8080))
    for desc, d, port in banned:
        if d is not None:
            r.check(not m.allowed(ap, d, port), f"[{env}] luồng CẤM: api-public → {desc} ({port})")
    if oidc_on:
        r.check(m.allowed(ap, p("iam/keycloak"), 8080), f"[{env}] F28 api-public → Keycloak bị chặn dù oidc.enabled")
    for port in (80, 443, 5432, 6379):
        r.check(not m.allowed(ap, internet, port), f"[{env}] ra Internet phải bị chặn: api-public → 8.8.8.8:{port}")
    obj_nets = [ipaddress.ip_network(c, strict=False) for c in values["external"].get("objectStorage") or []]
    for key, cidrs in values["external"].items():
        if not cidrs or key in ("internet", "valkey"):
            continue
        tgt = IP(first_host(cidrs))
        in_obj = any(ipaddress.ip_address(tgt.addr) in n for n in obj_nets)  # dev: dải ngoài chồng nhau (10.0.0.0/8)
        for port in (443, 587, 6443):
            if port == 443 and in_obj:
                # F29: phát luồng tệp xuất (ADR-0022 mục 8) — chỉ 443, chỉ kho đối tượng trong lãnh thổ.
                r.check(m.allowed(ap, tgt, 443), f"[{env}] F29 api-public → kho đối tượng {tgt.addr}:443 bị chặn")
                continue
            r.check(not m.allowed(ap, tgt, port), f"[{env}] api-public không được ra dải ngoài {key} ({tgt.addr}:{port})")
    vk = values["external"].get("valkey") or []
    if vk:
        r.check(m.allowed(ap, IP(first_host(vk)), 6379), f"[{env}] F26 api-public → Valkey bị chặn dù external.valkey có dải")
    r.check(not m.allowed(ap, IP("192.0.2.99"), 6379) or bool(vk), f"[{env}] api-public → :6379 mở khi chưa cấu hình Valkey")

    # ---- cấu trúc: hồ sơ gốc TẮT, Phase K BẬT
    def find(kind, ns, name):
        return next((d for d in docs if d.get("kind") == kind and d["metadata"].get("namespace") == ns and d["metadata"]["name"] == name), None)

    dep = find("Deployment", pf, "api-public")
    c = dep["spec"]["template"]["spec"]["containers"][0]
    envs = {e["name"]: e.get("value") for e in c.get("env") or []}
    env_from = [e["secretRef"]["name"] for e in c.get("envFrom") or [] if "secretRef" in e]
    hpa = find("HorizontalPodAutoscaler", pf, "api-public")
    pdb = find("PodDisruptionBudget", pf, "api-public")
    ingresses = [d for d in docs if d.get("kind") == "Ingress"]
    rules = [(ing, rule) for ing in ingresses for rule in ing["spec"]["rules"]]
    partner = [(ing, rule) for ing, rule in rules if rule["host"].startswith("partner.")]
    mws = {d["metadata"]["name"]: d for d in docs if d.get("kind") == "Middleware" and d["metadata"]["namespace"] == pf}
    es = {(d["metadata"]["namespace"], d["spec"]["target"]["name"]): d for d in docs if d.get("kind") == "ExternalSecret"}
    # api-public chỉ qua host partner.<domain>; host partner chỉ tới api-public:8085 (không bao giờ api-platform).
    for ing, rule in rules:
        for path in rule["http"]["paths"]:
            svc = path["backend"]["service"]
            if svc["name"] == "api-public":
                r.check(rule["host"].startswith("partner."), f"[{env}] api-public lộ qua host khác partner: {rule['host']}")
            if rule["host"].startswith("partner."):
                r.check(svc["name"] == "api-public" and svc["port"]["number"] == 8085 and ing["metadata"]["namespace"] == pf,
                        f"[{env}] host partner.<domain> phải chỉ tới api-public:8085 (ADR-0025): {svc}")
    for name in ("api-platform", "worker", "api-public"):
        d = find("Deployment", pf, name)
        if d is None:
            continue
        mode = {e["name"]: e.get("value") for e in d["spec"]["template"]["spec"]["containers"][0].get("env") or []}.get("AG_EGRESS_MODE")
        want = "enforce" if phasek else "observe"
        r.check(mode == want, f"[{env}] {name}: AG_EGRESS_MODE={mode}, mong đợi {want} (ADR-0024/0025)")
    # app_agg (ADR-0031 §3, gói FC): mật khẩu vai đăng nhập riêng của job tổng hợp chỉ ở Secret pf-db-agg, chỉ worker gắn.
    for d in docs:
        if d.get("kind") not in ("Deployment", "StatefulSet", "CronJob", "Job"):
            continue
        spec = d["spec"]["jobTemplate"]["spec"] if d["kind"] == "CronJob" else d["spec"]
        for ctr in spec["template"]["spec"].get("containers") or []:
            refs = [e["secretRef"]["name"] for e in ctr.get("envFrom") or [] if "secretRef" in e]
            if "pf-db-agg" in refs:
                r.check(d["kind"] == "Deployment" and d["metadata"]["name"] == "worker",
                        f"[{env}] pf-db-agg (app_agg) chỉ cho worker: {d['kind']}/{d['metadata']['name']}")
    agg_es = es.get((pf, "pf-db-agg"))
    if base_env(env) != "dev":
        r.check(agg_es is not None, f"[{env}] thiếu ExternalSecret pf-db-agg (app_agg)")
    if agg_es is not None:
        keys = {x["secretKey"] for x in agg_es["spec"].get("data") or []}
        r.check(keys == {"AG_DB_PASSWORD_APP_AGG"}, f"[{env}] pf-db-agg chỉ chứa AG_DB_PASSWORD_APP_AGG: {sorted(keys)}")
    pg_pf = es.get((data, "pg-pf-roles"))
    if pg_pf is not None:
        r.check("AG_DB_PASSWORD_APP_AGG" in {x["secretKey"] for x in pg_pf["spec"].get("data") or []},
                f"[{env}] pg-pf-roles thiếu AG_DB_PASSWORD_APP_AGG cho bootstrap app_agg")
    # Pepper chỉ ở vùng pf; vai DB của api-public đúng tập.
    for (ns, name) in es:
        if "apikey-pepper" in name:
            r.check(ns == pf, f"[{env}] pepper khóa API ngoài vùng pf: {ns}/{name}")
    pub = es.get((pf, "pf-public-db-roles"))
    if base_env(env) != "dev":
        r.check(pub is not None, f"[{env}] thiếu ExternalSecret pf-public-db-roles")
    if pub is not None:
        keys = {x["secretKey"] for x in pub["spec"].get("data") or []}
        r.check(keys == PUBLIC_DB_KEYS, f"[{env}] pf-public-db-roles phải đúng vai của api-public: {sorted(keys ^ PUBLIC_DB_KEYS)}")
    pg_roles = es.get((data, "pg-pf-roles"))
    if pg_roles is not None:
        keys = {x["secretKey"] for x in pg_roles["spec"].get("data") or []}
        need = {"AG_DB_PASSWORD_APP_PUBLIC", "AG_DB_PASSWORD_PUBLIC_API", "AG_DB_PASSWORD_APICLIENT_APP", "AG_DB_PASSWORD_METERING_APP"}
        r.check(need <= keys, f"[{env}] pg-pf-roles thiếu vai G5 cho bootstrap: {sorted(need - keys)}")
    r.check(set(env_from) >= {"pf-public-db-roles", "pf-apikey-pepper"}, f"[{env}] api-public thiếu envFrom pf-public-db-roles/pf-apikey-pepper")
    r.check(not any(s in env_from for s in ("pf-db-roles", "pf-app", "pf-s3", "pf-anchor")), f"[{env}] api-public nhận Secret rộng hơn cần: {env_from}")
    # Khóa S3: chỉ khóa CHỈ ĐỌC riêng pf-public-s3 (không khóa ghi pf-s3 của api-platform/worker).
    key_refs = {e["valueFrom"]["secretKeyRef"]["name"] for e in c.get("env") or [] if "secretKeyRef" in (e.get("valueFrom") or {})}
    r.check(key_refs <= {"pf-public-s3"}, f"[{env}] api-public chỉ nhận khóa S3 chỉ đọc pf-public-s3: {sorted(key_refs)}")
    r.check(envs.get("AG_S3_ENDPOINT") and envs.get("AG_S3_BUCKET"), f"[{env}] api-public thiếu AG_S3_ENDPOINT/AG_S3_BUCKET (tải tệp xuất, ADR-0022 mục 8)")

    if not phasek:
        r.check(dep["spec"].get("replicas") == 0, f"[{env}] ĐTK 1/G4: api-public phải 0 bản sao, đang {dep['spec'].get('replicas')}")
        r.check(hpa is None, f"[{env}] ĐTK 1/G4: không HPA cho api-public (HPA ép ≥ 1 bản sao)")
        r.check(envs.get("AG_PUBLIC_API_ENABLED") != "1", f"[{env}] ĐTK 1/G4: AG_PUBLIC_API_ENABLED không được là 1")
        r.check(not partner, f"[{env}] ĐTK 1/G4: không có host partner.<domain>")
        r.check(not mws, f"[{env}] ĐTK 1/G4: không Middleware Public API")
        r.check((pf, "pf-apikey-pepper") not in es and (pf, "agrigreen-partner-tls") not in es, f"[{env}] ĐTK 1/G4: pepper/TLS partner chỉ có ở Phase K")
        r.check((pf, "pf-public-s3") not in es, f"[{env}] ĐTK 1/G4: khóa S3 của api-public chỉ có ở Phase K")
        return
    replicas = dep["spec"].get("replicas")
    r.check(replicas == 2 or (hpa is not None and hpa["spec"]["minReplicas"] >= 2), f"[{env}] Phase K: api-public 2 bản sao (HA), đang {replicas}")
    r.check(pdb is not None and pdb["spec"].get("minAvailable", 0) >= 1, f"[{env}] Phase K: PDB api-public minAvailable ≥ 1")
    r.check(envs.get("AG_PUBLIC_API_ENABLED") == "1", f"[{env}] Phase K: AG_PUBLIC_API_ENABLED=1")
    r.check("AG_PUBLIC_OPERATOR_TENANT" in envs and envs.get("AG_PUBLIC_HTTP_ADDR") == ":8085", f"[{env}] Phase K: thiếu AG_PUBLIC_OPERATOR_TENANT / AG_PUBLIC_HTTP_ADDR")
    r.check(len(partner) == 1, f"[{env}] Phase K: đúng một host partner.<domain>, có {len(partner)}")
    if partner:
        ing, rule = partner[0]
        r.check(rule["host"] == f"partner.{values['global']['domain']}", f"[{env}] host partner sai: {rule['host']}")
        tls = ing["spec"].get("tls") or []
        r.check(any(rule["host"] in t.get("hosts", []) and t.get("secretName") == "agrigreen-partner-tls" for t in tls), f"[{env}] Phase K: partner thiếu TLS agrigreen-partner-tls")
        r.check(base_env(env) == "dev" or (pf, "agrigreen-partner-tls") in es, f"[{env}] Phase K: TLS partner không có ExternalSecret ở {pf}")
        ann = ing["metadata"].get("annotations") or {}
        refs = (ann.get("traefik.ingress.kubernetes.io/router.middlewares") or "").split(",")
        r.check(ann.get("traefik.ingress.kubernetes.io/router.entrypoints") == "websecure", f"[{env}] Phase K: partner chỉ qua websecure (TLS)")
        rl = next((mws[n] for n in mws if f"{pf}-{n}@kubernetescrd" in refs and "rateLimit" in mws[n]["spec"]), None)
        bl = next((mws[n] for n in mws if f"{pf}-{n}@kubernetescrd" in refs and "buffering" in mws[n]["spec"]), None)
        r.check(rl is not None and rl["spec"]["rateLimit"].get("average", 0) > 0, f"[{env}] Phase K: partner thiếu giới hạn theo IP (R-55)")
        r.check(bl is not None and 0 < bl["spec"]["buffering"].get("maxRequestBodyBytes", 0) <= 1048576, f"[{env}] Phase K: partner thiếu giới hạn thân 1 MiB")
    r.check(base_env(env) == "dev" or (pf, "pf-apikey-pepper") in es, f"[{env}] Phase K: thiếu ExternalSecret pepper khóa API")
    r.check(base_env(env) == "dev" or (pf, "pf-public-s3") in es, f"[{env}] Phase K: thiếu ExternalSecret pf-public-s3 (khóa S3 chỉ đọc của api-public)")


# --------------------------------------------------------------------------- Cổng tích hợp (W5.2)
def gateway_checks(env: str, docs: list[dict], values: dict, r: Report):
    """ADR-0033 (W5.2): integration-gateway có CÙNG phong bì mạng với api-public — chỉ nhận từ Ingress (F08 :8085,
    F09 mTLS :8443), chỉ ra PgBouncer pf (F19) + kho đối tượng 443 (F29) + DNS/OTLP; cùng vai DB app_public; chứng
    thư mTLS chỉ từ Secret pf-gateway-mtls của vùng pf. 0 bản sao ở mọi hồ sơ cho tới khi chuyển host partner."""
    pf, data, vault, mkg, ops = NS["pf"], NS["data"], NS["vault"], NS["mkg"], NS["ops"]
    m = Model(docs)
    pods = pods_from(docs)
    gw = pods.get(f"{pf}/integration-gateway")
    r.check(gw is not None, f"[{env}] thiếu Deployment integration-gateway (W5.2)")
    if gw is None:
        return
    internet = IP("8.8.8.8")

    def p(k):
        return pods.get(k)

    for desc, s, d, port in [
        ("F08 Ingress → integration-gateway:8085", p("edge/traefik"), gw, 8085),
        ("F09 Ingress → integration-gateway:8443 (mTLS)", p("edge/traefik"), gw, 8443),
        ("F19 integration-gateway → PgBouncer pf", gw, p(f"{data}/pg-pf-pooler"), 5432),
        ("F50 integration-gateway → OTLP", gw, p(f"{ops}/otel-collector"), 4317),
    ]:
        r.check(s is not None and d is not None and m.allowed(s, d, port), f"[{env}] cổng tích hợp: luồng bị chặn: {desc}")
    srcs = [internet] + [x for x in (
        p(f"{pf}/api-platform"), p(f"{pf}/worker"), p(f"{pf}/web-ops"), p(f"{pf}/api-public"), p(f"{mkg}/api-internal"),
        p(f"{mkg}/ingest"), p(f"{mkg}/worker-internal"), p(f"{vault}/identity-vault"), p(f"{data}/rabbitmq"),
        p("iam/keycloak"), p(f"{ops}/otel-collector")) if x is not None]
    for s in srcs:
        for port in (8085, 8443):
            r.check(not m.allowed(s, gw, port), f"[{env}] luồng CẤM: {getattr(s, 'name', getattr(s, 'addr', s))} → integration-gateway:{port} (chỉ Ingress)")
    banned = [
        ("identity-vault", p(f"{vault}/identity-vault"), 8090),
        ("RabbitMQ AMQP", p(f"{data}/rabbitmq"), 5672),
        ("PostgreSQL pf trực tiếp", p(f"{data}/pg-pf"), 5432),
        ("api-platform", p(f"{pf}/api-platform"), 8080),
        ("Admin API", p(f"{pf}/api-platform"), 8082),
        ("api-internal (MKG)", p(f"{mkg}/api-internal"), 8081),
        ("Keycloak", p("iam/keycloak"), 8080),
    ]
    for t in ("pg-mkg-pooler", "pg-mkg", "pg-ts-pooler", "pg-ts", "pg-iam"):
        banned.append((f"DB {t}", p(f"{data}/{t}"), 5432))
    for desc, d, port in banned:
        if d is not None:
            r.check(not m.allowed(gw, d, port), f"[{env}] luồng CẤM: integration-gateway → {desc} ({port})")
    for port in (80, 443, 5432, 6379):
        r.check(not m.allowed(gw, internet, port), f"[{env}] ra Internet phải bị chặn: integration-gateway → 8.8.8.8:{port}")

    dep = next(d for d in docs if d.get("kind") == "Deployment" and d["metadata"].get("namespace") == pf and d["metadata"]["name"] == "integration-gateway")
    spec = dep["spec"]["template"]["spec"]
    c = spec["containers"][0]
    env_from = [e["secretRef"]["name"] for e in c.get("envFrom") or [] if "secretRef" in e]
    r.check(set(env_from) >= {"pf-public-db-roles", "pf-apikey-pepper"}, f"[{env}] integration-gateway thiếu envFrom pf-public-db-roles/pf-apikey-pepper (vai app_public, ADR-0033)")
    r.check(not any(s in env_from for s in ("pf-db-roles", "pf-app", "pf-s3", "pf-anchor")), f"[{env}] integration-gateway nhận Secret rộng hơn cần: {env_from}")
    vols = {v["secret"]["secretName"] for v in spec.get("volumes") or [] if "secret" in v}
    r.check(vols <= {"pf-gateway-mtls"}, f"[{env}] integration-gateway chỉ gắn Secret mTLS pf-gateway-mtls: {sorted(vols)}")
    r.check(dep["spec"].get("replicas") == 0, f"[{env}] W5.2: integration-gateway 0 bản sao cho tới khi chuyển host partner (runbook)")
    for d in docs:
        if d.get("kind") == "ExternalSecret" and d["spec"]["target"]["name"] == "pf-gateway-mtls":
            r.check(d["metadata"]["namespace"] == pf, f"[{env}] Secret mTLS cổng ngoài vùng pf: {d['metadata']['namespace']}")


# --------------------------------------------------------------------------- chạy
def helm_bin() -> str:
    h = os.environ.get("HELM") or shutil.which("helm")
    if not h:
        cand = Path(os.environ.get("LOCALAPPDATA", "")) / "Programs" / "helm" / "helm.exe"
        h = str(cand) if cand.exists() else ""
    if not h:
        sys.exit("không tìm thấy helm (đặt HELM=<đường dẫn> hoặc cài theo docs/runbooks/k8s-deploy.md mục 1)")
    return h


def render(env: str) -> str:
    args = [helm_bin(), "template", "agrigreen", str(CHART)]
    for f in PROFILES[env]:
        args += ["-f", str(CHART / f)]
    out = subprocess.run(args, check=True, capture_output=True, text=True, encoding="utf-8")
    return out.stdout


def merged_values(env: str) -> dict:
    def merge(a, b):
        if isinstance(a, dict) and isinstance(b, dict):
            o = dict(a)
            for k, v in b.items():
                o[k] = merge(a.get(k), v) if k in a else v
            return o
        return b
    out = yaml.safe_load((CHART / "values.yaml").read_text(encoding="utf-8"))
    for f in PROFILES[env]:
        out = merge(out, yaml.safe_load((CHART / f).read_text(encoding="utf-8")) or {})
    return out


def leaf_paths(d, prefix: str = ""):
    if isinstance(d, dict) and d:
        for k, v in d.items():
            yield from leaf_paths(v, f"{prefix}.{k}" if prefix else str(k))
    else:
        yield prefix


def phasek_overlay_checks(r: Report):
    """R-01 / ADR-0025 mục 1: values-phasek.yaml chỉ có cấu hình và số bản sao."""
    over = yaml.safe_load((CHART / "values-phasek.yaml").read_text(encoding="utf-8")) or {}
    paths = list(leaf_paths(over))
    r.check(bool(paths), "values-phasek.yaml rỗng")
    for p in paths:
        r.check(any(re.fullmatch(rx, p) for rx in PHASEK_ALLOWED), f"values-phasek.yaml: '{p}' không phải cấu hình/số bản sao (R-01)")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--rendered", action="append", default=[], help="env=tệp YAML đã render (bỏ qua helm)")
    ap.add_argument("--env", action="append", choices=ENVS, help="chỉ kiểm môi trường này")
    a = ap.parse_args()
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")
    given = dict(x.split("=", 1) for x in a.rendered)
    r = Report()
    for env in a.env or ENVS:
        text = Path(given[env]).read_text(encoding="utf-8") if env in given else render(env)
        docs = [d for d in yaml.safe_load_all(text) if d]
        values = merged_values(env)
        structure_checks(env, docs, values, r)
        flow_checks(env, docs, values, r)
        public_api_checks(env, docs, values, r)
        gateway_checks(env, docs, values, r)
    phasek_overlay_checks(r)
    for f in r.fails:
        print("HỎNG:", f)
    print(f"check_flows: {r.passes} kiểm đạt, {len(r.fails)} hỏng")
    return 1 if r.fails else 0


if __name__ == "__main__":
    sys.exit(main())
