# ADR-0018: Sự kiện nội bộ MKG — vhost RabbitMQ `mkg.internal` (Q-MKG-01)

**Trạng thái:** Đề xuất · **Ngày:** 30/09/2026 · **Thành phần:** `shared.outbox.kind` (migration `shared/000004`), `internal/shared/outbox`, `cmd/outbox-publisher`, `cmd/worker-internal`, `tools/contractcheck`, `contracts/events-internal/`, Compose `rabbitmq-init`

## Bối cảnh

- Q-MKG-01 (docs/design/mkg.md §8): exchange `ag.events` dùng chung, nên mọi consumer của nền tảng đều bind được mọi routing key. Vì vậy ở G2, **mọi** sự kiện của vùng mkg lên hàng đợi đều phải là payload van công bố, và `contractcheck` cưỡng chế điều đó.
- G3 cần sự kiện **nội bộ** thật giữa các module mkg (tài liệu kiến trúc mục 13.2):
  - phiếu cân → hub/costing;
  - đối soát → cảnh báo;
  - đếm cuộn → đối chiếu cuộn ↔ khối lượng.

  Các payload này mang đúng thứ không bao giờ được qua van: khối lượng chính xác của lô, dung sai, tranh chấp, số cuộn gắn với nguồn gốc (docs/design/mkg.md §3.1).
- Mục 5 đã tách đơn vị triển khai `worker-internal` khỏi `worker-platform`.

## Quyết định

1. **Vhost riêng `mkg.internal`** trên cùng cụm RabbitMQ (ĐTK 1).
   - Tài khoản riêng của vùng mkg (`mkg_internal`) có quyền configure/write/read trên vhost này.
   - Tài khoản của nền tảng **không có quyền nào** trên vhost này, nên không bind, không đọc, không phát được. Compose `rabbitmq-init` gỡ quyền mà RabbitMQ tự cấp cho người tạo vhost. CI `compose-smoke` kiểm `list_permissions -p mkg.internal` không có tài khoản nền tảng.
   - Trong vhost dùng cùng tên exchange (`ag.events`, `ag.dlx`), quorum queue, DLQ, giới hạn giao 5 lần như vhost nền tảng. Đây là các đối tượng riêng của vhost.
2. **Cột `shared.outbox.kind`** ∈ {`platform`, `internal`}, mặc định `platform`.
   - CHECK `kind = 'platform' OR event_type LIKE 'mkg.%'`.
   - `outbox.Event.Kind` rỗng nghĩa là `KindPlatform`. `outbox.Enqueue` từ chối sự kiện `KindInternal` không phải `mkg.*`.
3. **outbox-publisher định tuyến theo `kind`.**
   - Mỗi `outbox.Publisher` chỉ quét **một** loại (`WHERE kind = $2`) và phát qua broker của đúng vhost đó.
   - Cụm mkg có hai publisher: `AG_AMQP_URL` cho van và `AG_AMQP_INTERNAL_URL` cho sự kiện nội bộ.
   - Nếu thiếu `AG_AMQP_INTERNAL_URL`, sự kiện nội bộ **tồn trên outbox** (metric backlog báo) và không bao giờ rơi sang vhost nền tảng.
   - Lúc khởi động, publisher khai báo hàng đợi của `cmd/worker-internal/assembly.Queues()` trên vhost nội bộ.
4. **Chú thích `//ag:internal-event <tên> v<N>`**, chỉ hợp lệ trong `internal/mkg/**`. `contractcheck` (tools/contractcheck/internal.go) hỏng build khi:
   - chú thích nằm ngoài `internal/mkg`, hoặc trên cùng struct có `//ag:event`/`//ag:publication`;
   - thiếu schema `contracts/events-internal/<tên>.v<N>.json`. Schema kiểm như `contracts/events/`: trường ngoài schema, required, kiểu đệ quy, tương thích ngược `-base`;
   - tên không phải `mkg.*`, hoặc trùng một sự kiện nền tảng ở `contracts/events/`;
   - literal `outbox.Event` trong `internal/mkg/**` có payload `//ag:internal-event` mà thiếu `Kind: outbox.KindInternal`. Không bao giờ phát sự kiện nội bộ lên exchange nền tảng;
   - `Kind: outbox.KindInternal` mà payload không chứng minh được là struct `//ag:internal-event`;
   - `Kind` không phải một trong hai hằng, hoặc gán `e.Kind = …`;
   - dùng `outbox.KindInternal` ngoài `internal/mkg/**`.
5. **`cmd/worker-internal`**: gốc lắp ráp vùng mkg, không import `internal/platform/*` (archlint).
   - Có sổ consumer `assembly.Consumers`, ở G3-0 là rỗng. Consumer dùng inbox cụm mkg (R-61), vai DB theo module, mặc định `worker_internal`.
   - Health ở `/healthz`, `/readyz` (cổng 8086, không publish ra máy).
   - Image có trong ma trận CI `images` và trong Compose.

## Hệ quả

- Van công bố vẫn là cửa duy nhất MKG → nền tảng (mục 5). Sự kiện nội bộ **không** là cửa: nó ở lại vùng mkg về cả mạng (vhost), mã (contractcheck) lẫn dữ liệu (CHECK DB).
- Sự kiện nội bộ đã đóng băng ở G3-0: `mkg.measurement.weigh_ticket.recorded`, `mkg.measurement.reconciliation.completed`, `mkg.fieldops.roll_count.recorded` (v1).
- Tích hợp đồng bộ qua `port/` trong cùng tiến trình vẫn là mặc định. Chỉ dùng sự kiện nội bộ khi cần tách thời gian, tức việc > 2 s hoặc nhiều người nhận.
- Production: vhost và tài khoản do IaC tạo (G4 Helm/OpenTofu). Bí mật `AMQP_INTERNAL_PASSWORD` riêng, và **không** cấp cho dịch vụ nền tảng.
- Kiểm thử: `internal/shared/inbox/kind_integration_test.go` (định tuyến theo `kind`, CHECK `mkg.*`), `tools/contractcheck` testdata ok/bad, `cmd/worker-internal/assembly` (sổ consumer).
