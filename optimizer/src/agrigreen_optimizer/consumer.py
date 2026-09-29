"""Vòng nhận `pf.simulation.job.requested` từ RabbitMQ (mục 12) bọc ngoài `handle_event`.

Topology giống hệt consumer Go (`internal/shared/inbox.DeclareQueue`):

- exchange sự kiện `ag.events` (topic, routing key = event_type) và exchange thư chết `ag.dlx`;
- quorum queue tên = tên consumer (mặc định `pf.simulation.optimizer`), `x-delivery-limit` 5,
  thư chết sang `ag.dlx` với routing key = tên consumer;
- DLQ `<consumer>.dlq` (quorum, không bao giờ tự xóa — R-62).

ACK chỉ sau khi POST kết quả thành công (201, hoặc 200 khi nộp lặp — idempotent).
Lỗi vĩnh viễn (phong bì hỏng, băm lệch, HTTP 4xx) → NACK không requeue → DLQ ngay.
Lỗi tạm thời (mạng, HTTP 5xx) → chờ lùi dần rồi NACK requeue; quorum queue đếm
lần giao và chuyển DLQ khi vượt giới hạn.

Tính toán chạy ở luồng phụ; luồng chính giữ kết nối (heartbeat) và nhận ACK/NACK
qua `add_callback_threadsafe` — mẫu chuẩn của pika cho việc dài.

Cấu hình (biến môi trường):

    AG_AMQP_URL                 amqp://user:pass@rabbitmq:5672/   (bắt buộc)
    AG_OPTIMIZER_QUEUE          tên consumer/queue, mặc định pf.simulation.optimizer
    AG_OPTIMIZER_PREFETCH       số thông điệp đang xử lý, mặc định 1
    AG_OPTIMIZER_RETRY_BASE_S   giây chờ trước khi requeue lần đầu (nhân đôi mỗi lần giao, tối đa 60), mặc định 2
    AG_INTERNAL_API_URL         https://api-platform:8084 (Internal API, mTLS)
    AG_OPTIMIZER_CLIENT_CERT, AG_OPTIMIZER_CLIENT_KEY, AG_INTERNAL_CA   mTLS tới Internal API
"""

from __future__ import annotations

import json
import logging
import os
import signal
import sys
import threading
import time
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any, Protocol

from .worker import EVENT_TYPE, InternalAPI, WorkerError, handle_event

EXCHANGE = "ag.events"
DEAD_LETTER_EXCHANGE = "ag.dlx"
DEFAULT_QUEUE = "pf.simulation.optimizer"
DELIVERY_LIMIT = 5  # = inbox.DeliveryLimit của Go

ACK, REQUEUE, DEAD = "ack", "requeue", "dead"

log = logging.getLogger("agrigreen_optimizer.consumer")


class Channel(Protocol):
    """Tập con của pika.adapters.blocking_connection.BlockingChannel mà consumer dùng."""

    def exchange_declare(self, exchange: str, exchange_type: str, durable: bool) -> Any: ...
    def queue_declare(self, queue: str, durable: bool, arguments: dict[str, Any] | None = None) -> Any: ...
    def queue_bind(self, queue: str, exchange: str, routing_key: str) -> Any: ...
    def basic_qos(self, prefetch_count: int) -> Any: ...
    def basic_consume(self, queue: str, on_message_callback: Callable[..., None], auto_ack: bool = False) -> Any: ...
    def basic_ack(self, delivery_tag: int) -> Any: ...
    def basic_nack(self, delivery_tag: int, requeue: bool = True) -> Any: ...


@dataclass(frozen=True)
class Config:
    amqp_url: str
    api: InternalAPI
    queue: str = DEFAULT_QUEUE
    prefetch: int = 1
    retry_base_s: float = 2.0

    @staticmethod
    def from_env(env: dict[str, str] | None = None) -> Config:
        e = os.environ if env is None else env
        url = e.get("AG_AMQP_URL", "")
        api_url = e.get("AG_INTERNAL_API_URL", "")
        if not url or not api_url:
            raise ValueError("thiếu AG_AMQP_URL hoặc AG_INTERNAL_API_URL")
        return Config(
            amqp_url=url,
            api=InternalAPI(
                api_url,
                e.get("AG_OPTIMIZER_CLIENT_CERT") or None,
                e.get("AG_OPTIMIZER_CLIENT_KEY") or None,
                e.get("AG_INTERNAL_CA") or None,
            ),
            queue=e.get("AG_OPTIMIZER_QUEUE") or DEFAULT_QUEUE,
            prefetch=int(e.get("AG_OPTIMIZER_PREFETCH") or 1),
            retry_base_s=float(e.get("AG_OPTIMIZER_RETRY_BASE_S") or 2.0),
        )


def declare(ch: Channel, queue: str) -> None:
    """Khai báo topology như `inbox.DeclareQueue` (Go) — lặp an toàn, tham số phải khớp."""
    ch.exchange_declare(exchange=EXCHANGE, exchange_type="topic", durable=True)
    ch.exchange_declare(exchange=DEAD_LETTER_EXCHANGE, exchange_type="topic", durable=True)
    dlq = queue + ".dlq"
    ch.queue_declare(queue=dlq, durable=True, arguments={"x-queue-type": "quorum"})
    ch.queue_bind(queue=dlq, exchange=DEAD_LETTER_EXCHANGE, routing_key=queue)
    ch.queue_declare(
        queue=queue,
        durable=True,
        arguments={
            "x-queue-type": "quorum",
            "x-delivery-limit": DELIVERY_LIMIT,
            "x-dead-letter-exchange": DEAD_LETTER_EXCHANGE,
            "x-dead-letter-routing-key": queue,
        },
    )
    ch.queue_bind(queue=queue, exchange=EXCHANGE, routing_key=EVENT_TYPE)


def _log(level: int, msg: str, **fields: Any) -> None:
    # Log có cấu trúc, không có dữ liệu cá nhân (chỉ mã tham chiếu).
    log.log(level, json.dumps({"msg": msg, **fields}, ensure_ascii=False))


def _header(props: Any, key: str) -> Any:
    headers = getattr(props, "headers", None) or {}
    return headers.get(key)


class Consumer:
    """Quyết định ACK/NACK cho từng thông điệp; không phụ thuộc pika (kiểm thử bằng broker giả)."""

    def __init__(
        self,
        api: InternalAPI,
        queue: str = DEFAULT_QUEUE,
        handler: Callable[..., dict[str, Any]] = handle_event,
        retry_base_s: float = 2.0,
        sleep: Callable[[float], None] = time.sleep,
    ) -> None:
        self.api, self.queue, self.handler = api, queue, handler
        self.retry_base_s, self.sleep = retry_base_s, sleep

    def decide(self, body: bytes, props: Any = None) -> str:
        try:
            env = json.loads(body)
            if not isinstance(env, dict):
                raise ValueError("phong bì không phải object")
        except (ValueError, UnicodeDecodeError):
            _log(logging.ERROR, "phong bì hỏng, chuyển DLQ", consumer=self.queue, outcome="dead")
            return DEAD
        fields = {"consumer": self.queue, "event": env.get("event_type"), "event_id": env.get("event_id"),
                  "tenant_id": env.get("tenant_id"), "trace_id": env.get("trace_id")}
        try:
            out = self.handler(env, self.api, _header(props, "traceparent"))
        except WorkerError as exc:
            if exc.permanent:
                _log(logging.ERROR, f"lỗi vĩnh viễn, chuyển DLQ: {exc}", outcome="dead", **fields)
                return DEAD
            count = int(_header(props, "x-delivery-count") or 0)
            delay = min(self.retry_base_s * (2**count), 60.0)
            _log(logging.WARNING, f"xử lý thất bại, thử lại sau {delay:g}s: {exc}", outcome="retry", **fields)
            self.sleep(delay)
            return REQUEUE
        except Exception as exc:  # noqa: BLE001 - lỗi bất ngờ: thử lại, quorum queue giới hạn số lần
            _log(logging.ERROR, f"lỗi bất ngờ: {type(exc).__name__}: {exc}", outcome="retry", **fields)
            return REQUEUE
        outcome = "duplicate" if out.get("replayed") else "processed"
        _log(logging.INFO, "sự kiện", outcome=outcome, status=out.get("status"), **fields)
        return ACK

    @staticmethod
    def settle(ch: Channel, delivery_tag: int, decision: str) -> None:
        if decision == ACK:
            ch.basic_ack(delivery_tag=delivery_tag)
        else:
            ch.basic_nack(delivery_tag=delivery_tag, requeue=decision == REQUEUE)

    def on_message(self, ch: Channel, method: Any, props: Any, body: bytes) -> None:
        """Callback đồng bộ (ACK/NACK ngay trên luồng gọi)."""
        self.settle(ch, method.delivery_tag, self.decide(body, props))

    def threaded_callback(self, schedule: Callable[[Callable[[], None]], None]) -> Callable[..., None]:
        """Callback chạy `decide` ở luồng phụ; ACK/NACK được đưa về luồng kết nối qua `schedule`
        (pika: `connection.add_callback_threadsafe`)."""

        def cb(ch: Channel, method: Any, props: Any, body: bytes) -> None:
            def work() -> None:
                decision = self.decide(body, props)
                try:
                    schedule(lambda: self.settle(ch, method.delivery_tag, decision))
                except Exception as exc:  # noqa: BLE001 - kết nối đã đóng: broker tự giao lại (idempotent)
                    _log(logging.WARNING, f"không ACK/NACK được: {type(exc).__name__}", consumer=self.queue, outcome="redeliver")

            threading.Thread(target=work, name="optimizer-job", daemon=True).start()

        return cb


def run(cfg: Config, stop: threading.Event | None = None) -> None:  # pragma: no cover - cần RabbitMQ thật (compose-smoke)
    """Chạy tới khi `stop` được đặt (SIGTERM/SIGINT). Mất kết nối → nối lại với lùi dần."""
    import pika  # BSD-3-Clause; chỉ nạp khi chạy thật

    stop = stop or threading.Event()
    consumer = Consumer(cfg.api, cfg.queue, retry_base_s=cfg.retry_base_s)
    backoff = 1.0
    while not stop.is_set():
        conn = None
        try:
            params = pika.URLParameters(cfg.amqp_url)
            params.client_properties = {"connection_name": "optimizer"}
            conn = pika.BlockingConnection(params)
            ch = conn.channel()
            declare(ch, cfg.queue)
            ch.basic_qos(prefetch_count=cfg.prefetch)
            ch.basic_consume(queue=cfg.queue, on_message_callback=consumer.threaded_callback(conn.add_callback_threadsafe))
            _log(logging.INFO, "lắng nghe", event="startup", consumer=cfg.queue, outcome="ok")
            backoff = 1.0
            while not stop.is_set():
                conn.process_data_events(time_limit=1)
        except pika.exceptions.AMQPError as exc:
            _log(logging.WARNING, f"mất kết nối RabbitMQ: {type(exc).__name__}", consumer=cfg.queue, outcome="reconnect")
            stop.wait(backoff)
            backoff = min(backoff * 2, 30.0)
        finally:
            if conn is not None and conn.is_open:
                try:
                    conn.close()
                except pika.exceptions.AMQPError:
                    pass


def main() -> int:  # pragma: no cover - điểm vào tiến trình
    logging.basicConfig(level=logging.INFO, stream=sys.stdout, format="%(message)s")
    try:
        cfg = Config.from_env()
    except ValueError as exc:
        print(f"optimizer: {exc}", file=sys.stderr)
        return 1
    stop = threading.Event()
    signal.signal(signal.SIGTERM, lambda *_: stop.set())
    signal.signal(signal.SIGINT, lambda *_: stop.set())
    run(cfg, stop)
    return 0
