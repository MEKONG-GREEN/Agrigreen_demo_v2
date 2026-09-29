# Agent biên đọc cân — `cmd/edge-agent`

Nhị phân tĩnh (Go, `CGO_ENABLED=0`) chạy trên máy nhỏ đặt cạnh cân tại hub, trong **vùng biên OT** (mục 14, 17; ADR-0019).

Việc của agent:

- đọc đầu cân qua bộ điều hợp;
- gán **bộ đếm đơn điệu** cho từng số đo và ghi vào đệm đĩa (WAL, fsync từng số đo);
- đóng lô ≤ 1000 số đo liên tiếp, ký Ed25519 trên dạng chuẩn `edgewire`;
- đẩy lô qua **mTLS, chỉ đi ra** tới `cmd/ingest`;
- chỉ gỡ số đo khỏi đệm sau khi ingest xác nhận.

Agent không mở cổng nào ra mạng. `/healthz` và `/metrics` chỉ nghe `127.0.0.1`.

> Mẫu cân và giao thức còn chờ khách chốt (C-6). Ba bộ điều hợp dưới đây là bản **tạm, chờ khách**. Chốt C-6 chỉ đổi bộ điều hợp, không đổi hợp đồng dây.

## 1. Build và cài đặt

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o edge-agent ./cmd/edge-agent
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o edge-agent ./cmd/edge-agent
```

Kích thước đo ở G3-1 (go1.27.1): linux/amd64 8,2 MiB, linux/arm64 7,6 MiB (bỏ `-s -w` thì 11,9 / 11,1 MiB). Phụ thuộc ngoài thư viện chuẩn duy nhất là `shopspring/decimal`, kéo vào qua `internal/shared/mass`. `tools/archlint` bảo đảm agent chỉ import thư viện chuẩn, `internal/shared/{clock,ids,mass}` và `edgewire`.

Bố cục trên thiết bị:

```
/opt/edge-agent/slots/a/edge-agent     khe A (bản cài đầu)
/opt/edge-agent/slots/b/               khe B (bản cập nhật)
/opt/edge-agent/slots.json             khe active / stable / trạng thái thử
/opt/edge-agent/active -> slots/a      liên kết do agent quản lý
/opt/edge-agent/stable -> slots/a
/etc/edge-agent/config.json            cấu hình (0640, root:edge)
/var/lib/edge-agent/                   data_dir (0700, edge:edge): state.json, wal/, pending.json, source.key, client.key
```

Lần cài đầu làm tay:

1. Chép nhị phân vào `slots/a/`.
2. Tạo hai liên kết: `ln -s slots/a /opt/edge-agent/active && ln -s slots/a /opt/edge-agent/stable`.
3. Tạo người dùng hệ thống `edge`, thuộc nhóm `dialout` nếu dùng cổng nối tiếp.

Unit systemd `/etc/systemd/system/edge-agent.service`:

```ini
[Unit]
Description=AgriGreen edge-agent (đọc cân)
After=network-online.target
Wants=network-online.target

[Service]
User=edge
# Chỉ khi dùng serial-ascii qua file:/dev/ttyUSB0 — đặt baud/khung (thư viện chuẩn Go không có API serial):
ExecStartPre=+/bin/stty -F /dev/ttyUSB0 9600 cs8 -cstopb -parenb raw -echo
# Luôn chạy bằng bản ĐÃ XÁC NHẬN: đếm lần thử và lùi lại nếu bản mới không khỏe.
ExecStartPre=/opt/edge-agent/stable/edge-agent update guard -config /etc/edge-agent/config.json
ExecStart=/opt/edge-agent/active/edge-agent run -config /etc/edge-agent/config.json
Restart=always
RestartSec=5
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=/var/lib/edge-agent /opt/edge-agent

[Install]
WantedBy=multi-user.target
```

Đồng hồ hệ điều hành do chrony hoặc systemd-timesyncd đồng bộ về NTP nội bộ (mục 18). Agent chỉ **đo** độ lệch qua SNTP để ghi `ntp_offset_ms` vào lô, không chỉnh đồng hồ.

## 2. Cấu hình

Cấu hình là một tệp JSON. Khóa lạ bị từ chối.

```json
{
  "device_id": "scale-hub01-in",
  "data_dir": "/var/lib/edge-agent",
  "adapter": {
    "type": "serial-ascii",
    "serial_ascii": { "transport": "file:/dev/ttyUSB0", "include_raw": true }
  },
  "buffer":  { "max_rate_hz": 10, "hours": 72 },
  "batch":   { "max_readings": 1000, "max_wait": "2s" },
  "ingest":  {
    "url": "https://ingest.mkg.internal:8443/v1/edge/batches",
    "ca_file": "/etc/edge-agent/ca.crt",
    "cert_file": "/etc/edge-agent/client.crt",
    "cert_key_file": "/var/lib/edge-agent/client.key"
  },
  "ntp":     { "server": "ntp.mkg.internal", "interval": "5m", "max_age": "30m" },
  "health":  { "listen": "127.0.0.1:9108", "stale_reading": "60s" },
  "update":  { "root": "/opt/edge-agent", "release_public_key": "<64 hex>", "healthy_after": "2m", "max_attempts": 3 }
}
```

### Bộ điều hợp (`adapter.type`)

| Loại | Dùng khi | Cấu hình chính |
|---|---|---|
| `simulator` | e2e, R-95, diễn tập khi chưa có cân | `seed` (tất định), `rate_hz`, `load_min_kg`/`load_max_kg` hoặc `loads_kg: ["96800.000"]`, `empty/settle/stable/release_samples`, `noise_g` |
| `serial-ascii` | đầu cân phát dòng ASCII (RS-232/485) | `transport` (`file:/dev/ttyUSB0` hoặc `tcp://host:port`), `pattern` (RE2, nhóm `value`, tùy chọn `unit`, `stable`), `stable_values`, `delimiter`, `poll_command`/`poll_interval`, `max_frame_bytes`, `include_raw` |
| `modbus-tcp` | PLC hoặc đầu cân có Modbus TCP | `address`, `unit_id`, `function` (`holding`/`input`), `weight_register`, `weight_words` (1/2), `word_order`, `unsigned`, `decimals`, `unit`, `stable_register`/`stable_mask`/`stable_invert` |

Ghi chú cho từng bộ điều hợp:

- **serial-ascii.** Mẫu mặc định khớp dạng `ST,GS,+001250.5kg`. Thư viện chuẩn Go không đặt được baud/parity, nên có hai cách chạy:
  - đặt baud/parity bằng `stty` (ExecStartPre ở mục 1), rồi agent mở tệp thiết bị;
  - dùng cầu nối serial-over-TCP (ser2net hoặc bộ chuyển RS-485 → Ethernet) với `tcp://`. Đây cũng là chế độ kiểm thử.
- **modbus-tcp.** Không khai `stable_register` thì agent coi số đo là ổn định khi `stable_samples` lần đọc liên tiếp bằng nhau.
- **Đơn vị.** Mọi bộ điều hợp quy đổi về `value_kg` có đúng 3 chữ số thập phân (R-10), không dùng float. `unit` chỉ ghi lại đơn vị đầu cân đang hiển thị.

### Định cỡ đệm

Công thức: `max_rate_hz × hours × 3600 × kích thước ước tính một số đo`.

Kích thước ước tính một số đo là 128 byte không raw. Có raw thì cộng `10 + 4·⌈max_frame_bytes/3⌉`.

Ví dụ ở 10 Hz, 72 giờ, không raw: khoảng 332 MB. Có raw 64 byte: khoảng 586 MB.

`max_bytes = 0` thì agent tự đặt bằng mức cần + 10 %. Đặt `max_bytes` nhỏ hơn mức cần, hoặc `hours < 72`, thì agent **từ chối khởi động**. Lệnh `edge-agent info` in ra `required_buffer_bytes`.

Agent fsync từng số đo. Trên thẻ SD hoặc eMMC, việc này giới hạn tốc độ ghi ở vài trăm số đo/giây, cao hơn nhiều mức 10 Hz của đầu cân.

## 3. Đăng ký thiết bị (enrol)

```bash
sudo -u edge edge-agent enroll -config /etc/edge-agent/config.json -epoch 1
```

Lệnh này:

- sinh khóa ký nguồn Ed25519 `source.key` (PEM PKCS#8, **0600**, không bao giờ rời thiết bị);
- sinh khóa mTLS ECDSA P-256 `client.key` và CSR `client.csr` (CN = SAN DNS = `device_id`);
- ghi `state.json` với kỳ và bộ đếm bằng 0;
- in ra `device_id`, `epoch`, `key_id`, `public_key_hex`/`_base64`, `csr_sha256`, subject của CSR và nội dung CSR.

Các bước phê duyệt:

1. `scale_supervisor` đề xuất bản ghi sổ thiết bị: `device_id`, `epoch`, khóa công khai, `key_id`, hub/kho, điểm cân, mẫu cân.
2. `device_approver`, **khác người đề xuất** (R-56), duyệt.
3. CA nội bộ ký CSR. Đặt chứng thư vào `ingest.cert_file`.
4. Chạy `edge-agent info` để lấy `cert_sha256` (SHA-256 của DER) và bổ sung vào sổ. Ingest chỉ tin `Resolve(device_id, epoch, cert_sha256)`.
5. Chạy `systemctl enable --now edge-agent`.

## 4. Thay khóa, mất đĩa, cài lại → kỳ mới

Bộ đếm của một kỳ **không bao giờ dùng lại** (ADR-0019 §5).

| Tình huống | Agent làm gì | Việc cần làm |
|---|---|---|
| Thay khóa định kỳ, chứng thư sắp hết hạn | — | Chờ `edge_unacked_readings = 0`, dừng dịch vụ, chạy `enroll -rotate -epoch N+1`, duyệt lại trong sổ (kỳ cũ chuyển `superseded`) |
| Mất `state.json` nhưng còn `wal/` | Từ chối khởi động (`ErrStateLost`) | `enroll -epoch N+1 -abandon-buffer`. Đệm cũ được **lưu trữ** (`*.retired-e…`), không xóa. Số đo trong đó không gửi được, ghi sự cố |
| Mất `wal/` hoặc một đoạn WAL, thiếu số đo so với `state.json` | Từ chối (`ErrCounterGap`) | Như trên. Ingest sẽ báo khoảng trống bộ đếm |
| Mất `source.key` hoặc khóa không khớp `key_id` | Từ chối (`ErrKeyLost`) | `enroll -rotate -epoch N+1 -abandon-buffer` |
| Thay cả đĩa hoặc máy | Chưa đăng ký (`ErrNotEnrolled`) | `enroll -epoch N+1`, dùng số kỳ tiếp theo trong sổ |

`-rotate` từ chối chạy khi đệm còn số đo chưa được xác nhận, trừ khi có thêm `-abandon-buffer`. Khóa, chứng thư, đệm và state cũ đều được đổi tên thành `<tên>.retired-e<kỳ>-<thời điểm>`, không xóa.

## 5. Giao thức đẩy lô (agent ↔ ingest)

Đây là **đề xuất của agent**. `cmd/ingest` (đợt A2) phải khớp; nếu cần đổi thì báo người tích hợp.

- Yêu cầu: `POST <ingest.url>` (mặc định `/v1/edge/batches`), TLS 1.3 kèm chứng thư client, `Content-Type: application/json`.
  - Thân là một lô `agrigreen.edge.batch.v1`.
  - Header `Idempotency-Key: <device_id>/<epoch>/<counter_from>` chỉ để tham khảo. Khóa thật là `(device_id, epoch, counter_from)` trong lô.
- `200`/`201`: lô đã commit, kể cả khi gửi lặp idempotent. Thân phản hồi tùy chọn. Nếu có `counter_to` hoặc `payload_sha256` thì phải khớp lô đã gửi, lệch thì agent bật báo động `payload_conflict`.
- `409`: cùng khóa khác payload, hoặc kỳ bị thay → báo động `payload_conflict`.
- `400`/`413`/`422`/`401`/`403`: → báo động `ingest_rejected`.
- Với cả 409 và các mã từ chối trên, agent **không gỡ đệm** và thử lại ở nhịp `backoff_max`.
- `408`/`425`/`429`/`5xx` và lỗi mạng: lùi lũy thừa có jitter trong khoảng `[exp/2, exp]`, với `exp = min(backoff_max, backoff_base·2^(n−1))`. Agent tôn trọng `Retry-After`.
- Trước lần gửi đầu, lô đã ký được lưu `pending.json` (fsync). Sau sập, agent gửi lại **đúng các byte đó**, nên `payload_sha256` không đổi dù độ lệch NTP đã khác. Ingest trả idempotent, không phải 409.
- Khi được xác nhận, agent làm theo thứ tự: ghi `acked_counter` vào `state.json` (fsync), gỡ các đoạn WAL đã xác nhận hết, xóa `pending.json`.

## 6. Cập nhật có chữ ký và lùi lại

Khóa phát hành là Ed25519, **khác** khóa thiết bị, và giữ ngoài thiết bị (CI hoặc HSM). Tạo một lần:

```bash
edge-agent update keygen -out release.key     # in release_public_key=<hex> → đặt vào update.release_public_key
```

Phát hành một bản:

```bash
edge-agent update sign -key release.key -binary edge-agent-linux-arm64 -version 0.2.0 -os linux -arch arm64 > manifest.json
```

Manifest gồm `version`, `os`, `arch`, `sha256`, `size` và chữ ký trên dạng chuẩn `agrigreen.edge.update.v1`.

Cài trên thiết bị. Tệp có thể lấy từ đĩa hoặc tải `https://` qua cùng client mTLS, chỉ đi ra:

```bash
edge-agent update apply -config /etc/edge-agent/config.json -manifest manifest.json -binary edge-agent-linux-arm64
systemctl restart edge-agent
```

Agent chỉ cài khi các điều kiện sau đều đạt:

- chữ ký của khóa phát hành hợp lệ;
- sha256 và kích thước khớp manifest;
- `os`/`arch` khớp thiết bị;
- phiên bản **mới hơn** bản đang chạy (chống hạ cấp; `-allow-downgrade` khi chủ ý);
- không có bản nào đang thử.

Nhị phân vào khe không chạy, `active` trỏ sang khe đó, `trial = true`. Sau khi khởi động lại:

1. Trước mỗi lần khởi động, `stable/edge-agent update guard` tăng `attempts`.
2. Nếu `attempts` vượt `max_attempts` (bản mới crash hoặc không khỏe), `active` lùi về `stable` và systemd chạy lại bản cũ.
3. Bản mới khỏe liên tục trong `healthy_after` thì tự xác nhận (`stable = active`).
4. Bản mới không khỏe quá `3 × healthy_after` thì tự thoát lỗi, để guard đếm và lùi lại.

Các lệnh tay:

- `edge-agent update status`: xem `slots.json` và lịch sử;
- `update commit`: xác nhận ngay;
- `update rollback`: đang thử thì về `stable`, còn không thì về khe kia.

## 7. Giám sát

Đọc tại chỗ: `curl -s 127.0.0.1:9108/healthz` (JSON, 503 khi không khỏe) và `curl -s 127.0.0.1:9108/metrics`. Hai điểm này dành cho Prometheus node-agent chạy cùng máy, hoặc để đọc tay qua VPN.

| Chỉ số | Cảnh báo đề xuất |
|---|---|
| `edge_buffer_fill_ratio` (mục 19, bắt buộc có cảnh báo) | > 0,5 trong 1 giờ: ingest không nhận. `edge_alarm{reason="buffer_high"}` (≥ `warn_fill_ratio`, mặc định 0,8): P2 |
| `edge_alarm{reason="buffer_full"}`, `edge_readings_refused_total` tăng | P1: **đang mất số đo mới**. Agent không ghi đè số đo chưa xác nhận |
| `edge_alarm{reason="payload_conflict"}` | P1: thiết bị hỏng hoặc bị giả, cách ly và điều tra |
| `edge_alarm{reason="ingest_rejected"}` | P2: chứng thư, sổ thiết bị, kỳ hoặc hợp đồng |
| `edge_last_push_success_timestamp_seconds` | cũ hơn 15 phút |
| `edge_alarm{reason="adapter_stale"}`, `edge_adapter_errors_total` | không có số đo quá `stale_reading`: cáp, đầu cân hoặc cấu hình mẫu |
| `edge_ntp_offset_ms`, `edge_ntp_synced` | \|lệch\| > 1000 ms hoặc chưa đồng bộ. Chỉ là thông tin, không ảnh hưởng thứ tự |
| `edge_unacked_readings`, `edge_last_counter`, `edge_acked_counter` | chênh lệch tăng đều |

## 8. Xử lý sự cố

| Triệu chứng | Kiểm tra |
|---|---|
| Không khởi động: `buffer.max_bytes … nhỏ hơn mức cần` | Tăng dung lượng hoặc bỏ `max_bytes` (tự tính). Không được giảm `hours` dưới 72 |
| `ErrStateLost`, `ErrCounterGap`, `ErrKeyLost`, `WAL hỏng` | Xem mục 4. **Không** sửa tay `state.json` hay xóa đoạn WAL để "chạy được": làm vậy là dùng lại bộ đếm |
| Log `WAL: cắt đuôi dở sau sập` | Bình thường sau mất điện. Số đo cuối chưa fsync bị bỏ, bộ đếm tiếp tục sau số đo đã ghi |
| `ingest 403` hoặc bắt tay TLS lỗi | Chứng thư hết hạn hoặc chưa duyệt, CN/SAN ≠ `device_id`, `cert_sha256` trong sổ chưa cập nhật, VPN |
| `ingest 409` | Có thiết bị khác dùng cùng `device_id`/kỳ, hoặc đệm bị sửa. Dừng agent, giữ nguyên `data_dir`, báo `device_approver` |
| `khung đầu cân: không khớp mẫu` tăng liên tục | So `pattern` với khung thật. Bật `include_raw` và xem `raw` trong `ts_weigh.raw_reading` |
| Modbus `ngoại lệ … mã 2` | Sai địa chỉ thanh ghi (0-based) hoặc sai `function` |
| Đồng hồ thiết bị sai | Chỉ ảnh hưởng `device_time` (thông tin). Thứ tự là bộ đếm, thời điểm hệ là `received_at` của ingest |

Kiểm thử: `go test ./cmd/edge-agent/`. Các nhóm kiểm thử:

- WAL đầy, cắt đuôi sau sập, đoạn hỏng hoặc thiếu;
- bộ đếm bền qua sập, mất đĩa phải sang kỳ mới;
- ranh giới lô, chữ ký khớp `contracts/edge/batch.v1.vectors.json`;
- mTLS, backoff, gửi lại idempotent, gỡ đệm khi xác nhận;
- cập nhật: xác minh, lùi lại, xác nhận;
- bộ mô phỏng tất định, serial qua cầu TCP, Modbus giả, SNTP giả;
- e2e từ mô phỏng tới ingest giả.
