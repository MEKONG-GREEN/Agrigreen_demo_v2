"""Vòng nhận RabbitMQ với broker giả: topology khớp Go, ACK chỉ sau khi POST thành công,
lỗi vĩnh viễn → DLQ, lỗi tạm thời → requeue có lùi dần (GIS US-SIM, mục 12, R-62)."""

from __future__ import annotations

import hashlib
import json
import threading
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from types import SimpleNamespace
from typing import Any

import pytest

from agrigreen_optimizer.consumer import (
    ACK,
    DEAD,
    DEAD_LETTER_EXCHANGE,
    DEFAULT_QUEUE,
    DELIVERY_LIMIT,
    EXCHANGE,
    REQUEUE,
    Config,
    Consumer,
    declare,
)
from agrigreen_optimizer.worker import EVENT_TYPE, InternalAPI, WorkerError

from .paths import GOLDEN

RAW = (GOLDEN / "case1_two_hubs_unapproved" / "snapshot.json").read_bytes()
SHA = hashlib.sha256(RAW).hexdigest()
TENANT = "00000000-0000-7000-a000-000000000001"
SIM = "00000000-0000-7000-9000-000000000001"


@dataclass
class FakeChannel:
    """Broker giả: ghi lại khai báo và ACK/NACK; `deliver` gọi callback như pika."""

    exchanges: dict[str, str] = field(default_factory=dict)
    queues: dict[str, dict[str, Any]] = field(default_factory=dict)
    bindings: list[tuple[str, str, str]] = field(default_factory=list)
    acked: list[int] = field(default_factory=list)
    nacked: list[tuple[int, bool]] = field(default_factory=list)
    callback: Any = None
    prefetch: int = 0
    _tag: int = 0

    def exchange_declare(self, exchange: str, exchange_type: str, durable: bool) -> None:
        assert durable
        self.exchanges[exchange] = exchange_type

    def queue_declare(self, queue: str, durable: bool, arguments: dict[str, Any] | None = None) -> None:
        assert durable
        self.queues[queue] = arguments or {}

    def queue_bind(self, queue: str, exchange: str, routing_key: str) -> None:
        self.bindings.append((queue, exchange, routing_key))

    def basic_qos(self, prefetch_count: int) -> None:
        self.prefetch = prefetch_count

    def basic_consume(self, queue: str, on_message_callback: Any, auto_ack: bool = False) -> None:
        assert not auto_ack, "ACK phải thủ công"
        self.callback = on_message_callback

    def basic_ack(self, delivery_tag: int) -> None:
        self.acked.append(delivery_tag)

    def basic_nack(self, delivery_tag: int, requeue: bool = True) -> None:
        self.nacked.append((delivery_tag, requeue))

    def deliver(self, body: bytes, headers: dict[str, Any] | None = None) -> int:
        self._tag += 1
        self.callback(self, SimpleNamespace(delivery_tag=self._tag), SimpleNamespace(headers=headers), body)
        return self._tag


def envelope(sha: str = SHA, event_type: str = EVENT_TYPE) -> bytes:
    return json.dumps({
        "event_id": "00000000-0000-7000-8000-0000000000e1",
        "event_type": event_type,
        "schema_version": 1,
        "tenant_id": TENANT,
        "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
        "producer": "pf/simulation",
        "payload": {"simulation_id": SIM, "snapshot_sha256": sha,
                    "snapshot_path": f"/internal/v1/simulations/{SIM}/snapshot",
                    "result_path": f"/internal/v1/simulations/{SIM}/result"},
    }).encode()


def test_topology_matches_go_inbox() -> None:
    ch = FakeChannel()
    declare(ch, DEFAULT_QUEUE)
    assert ch.exchanges == {EXCHANGE: "topic", DEAD_LETTER_EXCHANGE: "topic"}
    assert ch.queues[DEFAULT_QUEUE + ".dlq"] == {"x-queue-type": "quorum"}
    assert ch.queues[DEFAULT_QUEUE] == {
        "x-queue-type": "quorum",
        "x-delivery-limit": DELIVERY_LIMIT,
        "x-dead-letter-exchange": DEAD_LETTER_EXCHANGE,
        "x-dead-letter-routing-key": DEFAULT_QUEUE,
    }
    assert (DEFAULT_QUEUE + ".dlq", DEAD_LETTER_EXCHANGE, DEFAULT_QUEUE) in ch.bindings
    assert (DEFAULT_QUEUE, EXCHANGE, EVENT_TYPE) in ch.bindings
    assert DELIVERY_LIMIT == 5  # = inbox.DeliveryLimit (Go)


# ------------------------------------------------ Internal API giả (HTTP thuần)


class _API(BaseHTTPRequestHandler):
    post_status = 201
    get_status = 200
    posted: list[tuple[str, str | None]] = []

    def log_message(self, *args: object) -> None:
        pass

    def do_GET(self) -> None:  # noqa: N802
        self.send_response(self.get_status)
        self.end_headers()
        if self.get_status == 200:
            self.wfile.write(RAW)

    def do_POST(self) -> None:  # noqa: N802
        self.rfile.read(int(self.headers["Content-Length"]))
        _API.posted.append((self.path, self.headers.get("traceparent")))
        self.send_response(self.post_status)
        self.end_headers()
        replayed = "true" if self.post_status == 200 else "false"
        self.wfile.write(f'{{"status":"completed","replayed":{replayed}}}'.encode())


@pytest.fixture()
def api() -> InternalAPI:
    _API.post_status, _API.get_status = 201, 200
    _API.posted.clear()
    srv = HTTPServer(("127.0.0.1", 0), _API)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    yield InternalAPI(f"http://127.0.0.1:{srv.server_address[1]}", timeout=5)
    srv.shutdown()


def consumer_on(ch: FakeChannel, api: InternalAPI, sleeps: list[float] | None = None) -> Consumer:
    c = Consumer(api, retry_base_s=2.0, sleep=(sleeps.append if sleeps is not None else lambda _s: None))
    declare(ch, DEFAULT_QUEUE)
    ch.basic_consume(queue=DEFAULT_QUEUE, on_message_callback=c.on_message)
    return c


def test_ack_only_after_result_posted(api: InternalAPI) -> None:
    ch = FakeChannel()
    consumer_on(ch, api)
    tp = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
    tag = ch.deliver(envelope(), {"traceparent": tp})
    assert ch.acked == [tag] and not ch.nacked
    assert _API.posted == [(f"/internal/v1/simulations/{SIM}/result", tp)]  # traceparent chuyển tiếp (R-90)


def test_replayed_result_is_acked(api: InternalAPI) -> None:
    _API.post_status = 200  # nộp lặp → 200 replayed (idempotent)
    ch = FakeChannel()
    consumer_on(ch, api)
    tag = ch.deliver(envelope())
    assert ch.acked == [tag]


def test_post_5xx_requeues_with_backoff_not_ack(api: InternalAPI) -> None:
    _API.post_status = 503
    ch, sleeps = FakeChannel(), []
    consumer_on(ch, api, sleeps)
    t1 = ch.deliver(envelope())
    t2 = ch.deliver(envelope(), {"x-delivery-count": 2})
    assert not ch.acked
    assert ch.nacked == [(t1, True), (t2, True)]
    assert sleeps == [2.0, 8.0]  # lùi dần theo x-delivery-count của quorum queue


def test_api_unreachable_requeues() -> None:
    ch = FakeChannel()
    consumer_on(ch, InternalAPI("http://127.0.0.1:9", timeout=1))  # cổng discard: từ chối kết nối
    tag = ch.deliver(envelope())
    assert ch.nacked == [(tag, True)] and not ch.acked


def test_permanent_errors_go_to_dlq(api: InternalAPI) -> None:
    ch = FakeChannel()
    consumer_on(ch, api)
    bad_json = ch.deliver(b"{not json")
    tampered = ch.deliver(envelope(sha="0" * 64))  # băm lệch → không tính, không nộp
    wrong_type = ch.deliver(envelope(event_type="pf.simulation.job.completed"))
    missing = ch.deliver(json.dumps({"event_type": EVENT_TYPE, "tenant_id": TENANT, "payload": {}}).encode())
    assert ch.nacked == [(bad_json, False), (tampered, False), (wrong_type, False), (missing, False)]
    assert not ch.acked and not _API.posted


def test_snapshot_404_is_permanent_500_is_transient(api: InternalAPI) -> None:
    ch = FakeChannel()
    consumer_on(ch, api)
    _API.get_status = 404
    t404 = ch.deliver(envelope())
    _API.get_status = 500
    t500 = ch.deliver(envelope())
    assert ch.nacked == [(t404, False), (t500, True)]


class _Props:
    def __init__(self, headers: dict[str, Any]) -> None:
        self.headers = headers


def test_unexpected_exception_requeues_with_backoff() -> None:
    def boom(*_a: Any) -> dict[str, Any]:
        raise RuntimeError("lỗi lập trình")

    sleeps: list[float] = []
    c = Consumer(InternalAPI("http://x"), handler=boom, retry_base_s=2.0, sleep=sleeps.append)
    assert c.decide(envelope()) == REQUEUE
    # Lỗi bất ngờ cũng lùi như WorkerError tạm thời (không dồn 5 lần giao trong vài mili giây rồi vào DLQ).
    assert c.decide(envelope(), _Props({"x-delivery-count": 2})) == REQUEUE
    assert c.decide(envelope(), _Props({"x-delivery-count": 10})) == REQUEUE
    assert c.decide(envelope(), _Props({"x-delivery-count": "hỏng"})) == REQUEUE
    assert sleeps == [2.0, 8.0, 60.0, 2.0]


def test_threaded_callback_settles_on_connection_thread(api: InternalAPI) -> None:
    """Tính ở luồng phụ, ACK qua `add_callback_threadsafe` (luồng kết nối giữ heartbeat)."""
    ch = FakeChannel()
    c = Consumer(api)
    scheduled: list[Any] = []
    done = threading.Event()

    def schedule(fn: Any) -> None:
        scheduled.append(fn)
        done.set()

    declare(ch, DEFAULT_QUEUE)
    ch.basic_consume(queue=DEFAULT_QUEUE, on_message_callback=c.threaded_callback(schedule))
    tag = ch.deliver(envelope())
    assert done.wait(30)
    assert not ch.acked  # chưa ACK cho tới khi luồng kết nối chạy callback
    scheduled[0]()
    assert ch.acked == [tag]


def test_decide_outcomes_with_stub_handler() -> None:
    calls: list[str | None] = []

    def ok(env: dict[str, Any], _api: InternalAPI, tp: str | None) -> dict[str, Any]:
        calls.append(tp)
        return {"status": "completed"}

    def perm(*_a: Any) -> dict[str, Any]:
        raise WorkerError("x", permanent=True)

    assert Consumer(InternalAPI("http://x"), handler=ok).decide(envelope()) == ACK
    assert Consumer(InternalAPI("http://x"), handler=perm).decide(envelope()) == DEAD
    assert calls == [None]


def test_config_from_env() -> None:
    with pytest.raises(ValueError):
        Config.from_env({})
    cfg = Config.from_env({
        "AG_AMQP_URL": "amqp://u:p@rabbitmq:5672/",
        "AG_INTERNAL_API_URL": "https://api-platform:8084",
        "AG_OPTIMIZER_CLIENT_CERT": "/certs/optimizer.crt",
        "AG_OPTIMIZER_CLIENT_KEY": "/certs/optimizer.key",
        "AG_INTERNAL_CA": "/certs/ca.crt",
        "AG_OPTIMIZER_PREFETCH": "2",
    })
    assert cfg.queue == DEFAULT_QUEUE and cfg.prefetch == 2
    assert cfg.api.cert == "/certs/optimizer.crt" and cfg.api.ca == "/certs/ca.crt"
