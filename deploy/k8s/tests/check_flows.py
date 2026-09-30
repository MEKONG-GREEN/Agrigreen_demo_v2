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
ENVS = ("dev", "staging", "prod")

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
        ("worker → SMTP trong lãnh thổ", None, "smtp", f"{pf}/worker", 587),
        ("CNPG → API server", None, "kubeAPI", f"{data}/pg-mkg", 443),
        ("CA thiết bị → HSM", None, "hsm", f"{pki}/step-ca", 1792),
    ]
    for desc, src_ext, dst_ext, podkey, port in ext_allowed:
        if (src_ext or dst_ext) not in ipof or podkey not in pods:
            if env == "prod":
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
    if env == "dev":
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
    no_probe_ok = {"worker", "outbox-publisher-pf", "outbox-publisher-mkg", "optimizer"}
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
            refs += [e["secretRef"]["name"] for e in c.get("envFrom") or [] if "secretRef" in e]
            for s in refs:
                if ns in forbid_prefix:
                    r.check(not s.startswith(forbid_prefix[ns]), f"[{env}] {ns}/{name}: Secret của vùng khác: {s}")
                if env != "dev":
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
                if env != "dev":
                    r.check((ns, s) in es_names or (ns, s) in cert_names, f"[{env}] {ns}/{name}: volume Secret {s} không có nguồn")
    # PDB cho mọi Deployment nhiều bản; HPA cho api-platform, ingest (prod).
    pdbs = {(d["metadata"]["namespace"], d["metadata"]["name"]) for d in by_kind.get("PodDisruptionBudget", [])}
    for d in by_kind.get("Deployment", []):
        if (d["spec"].get("replicas") or 2) > 1:
            r.check((d["metadata"]["namespace"], d["metadata"]["name"]) in pdbs, f"[{env}] thiếu PDB: {d['metadata']['name']}")
    if env != "dev":
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
    out = subprocess.run([helm_bin(), "template", "agrigreen", str(CHART), "-f", str(CHART / f"values-{env}.yaml")],
                         check=True, capture_output=True, text=True, encoding="utf-8")
    return out.stdout


def merged_values(env: str) -> dict:
    def merge(a, b):
        if isinstance(a, dict) and isinstance(b, dict):
            o = dict(a)
            for k, v in b.items():
                o[k] = merge(a.get(k), v) if k in a else v
            return o
        return b
    base = yaml.safe_load((CHART / "values.yaml").read_text(encoding="utf-8"))
    over = yaml.safe_load((CHART / f"values-{env}.yaml").read_text(encoding="utf-8")) or {}
    return merge(base, over)


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
    for f in r.fails:
        print("HỎNG:", f)
    print(f"check_flows: {r.passes} kiểm đạt, {len(r.fails)} hỏng")
    return 1 if r.fails else 0


if __name__ == "__main__":
    sys.exit(main())
