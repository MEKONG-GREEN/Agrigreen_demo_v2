# ADR-0019: Định danh thiết bị cân, ký tại nguồn và đồng hồ

**Trạng thái:** Đề xuất · **Ngày:** 30/09/2026 · **Thành phần:** `cmd/edge-agent`, `cmd/ingest`, `internal/mkg/measurement/port` (`DeviceRegistry`), `internal/mkg/measurement/port/edgewire`, `contracts/edge/batch.v1.json`, `ts_weigh.reading_batch`

## Bối cảnh

Mục 14 của tài liệu kiến trúc có các yêu cầu sau cho agent biên:
- mỗi thiết bị một khóa Ed25519 cấp lúc đăng ký;
- lô số đo ký kèm bộ đếm đơn điệu;
- không tin đồng hồ thiết bị làm nguồn thứ tự;
- idempotency theo `(device_id, counter)`;
- đẩy theo lô qua mTLS, chỉ đi ra;
- đệm ≥ 72 giờ, đầy thì ngừng và báo động.

Mẫu cân và giao thức chưa chốt (C-6). Hợp đồng dây phải đóng băng trước để `cmd/ingest`, `mkg/measurement` và `cmd/edge-agent` làm song song ở G3-1.

## Quyết định

1. **Hai danh tính cho mỗi thiết bị, cấp cùng lúc đăng ký:**
   - **Chứng thư mTLS client** do CA nội bộ trong lãnh thổ cấp (dev: `scripts/gen-dev-certs`). CN/SAN = `device_id`. Dùng cho **kênh**: ingest chỉ nhận kết nối từ chứng thư đã duyệt, vùng biên OT qua VPN (mục 17).
   - **Khóa ký nguồn Ed25519**, khóa riêng sinh và giữ trên thiết bị. Dùng cho **dữ liệu**: chữ ký theo lô tồn tại mãi trong `ts_weigh.reading_batch`. Nhờ vậy vẫn xác minh được sau khi chứng thư mTLS hết hạn hoặc bị thu hồi.
2. **Sổ thiết bị đã duyệt** (`mkg/measurement`, interface `DeviceRegistry`).
   - Mỗi `(device_id, epoch)` có một bản ghi: khóa công khai, `key_id`, SHA-256 chứng thư DER, hub/kho, điểm cân, mẫu cân.
   - `scale_supervisor` đề xuất, `device_approver` duyệt, và người duyệt ≠ người đề xuất (R-56).
   - `Resolve(device_id, epoch, cert_sha256)` là nguồn tin duy nhất của ingest. Ingest **không bao giờ** tin khóa hay tenant đi kèm lô (R-16).
3. **Lô = dãy bộ đếm đơn điệu liên tiếp** `counter_from..counter_to` (tối đa 1000 số đo).
   - Bộ đếm bắt đầu từ 1 ở mỗi kỳ đăng ký, là thứ tự **duy nhất** của số đo.
   - Idempotency ở `ts_weigh.reading_batch UNIQUE (device_id, epoch, counter_from)`, cộng trigger đơn điệu: lô mới phải bắt đầu sau lô cuối.
   - Gửi lặp nghĩa là trùng khóa. Ingest so `payload_sha256`: trùng thì trả thành công idempotent, khác thì báo động (thiết bị hỏng hoặc bị giả).
   - Khoảng trống bộ đếm được nhận, nhưng giám sát báo mất lô.
4. **Đồng hồ.**
   - `device_time` (UTC, mili giây) và `ntp_offset_ms` (null khi chưa đồng bộ NTP nội bộ, mục 18) được ký và lưu, nhưng **không bao giờ dùng làm thứ tự**.
   - Thời điểm hệ là `received_at` do DB đặt (R-13), cũng là khóa phân vùng (ADR-0017).
   - Cửa sổ tích phân phiếu cân tính theo **bộ đếm** (`port.Window`).
5. **Mất đĩa, cài lại, thay khóa hay thay chứng thư → kỳ đăng ký mới** (`epoch + 1`), được duyệt lại.
   - Bộ đếm của kỳ cũ **không bao giờ dùng lại**. Kỳ cũ chuyển `superseded`, và lô mới của kỳ cũ bị từ chối (`ErrEpochSuperseded`).
   - Thu hồi (`revoked`) chặn mọi lô mới. Lô đã nhận vẫn giữ nguyên (RB-1).
6. **Đệm tại thiết bị.**
   - Ghi đĩa cục bộ, đủ ≥ 72 giờ ở tốc độ đọc tối đa.
   - Đầy thì **ngừng nhận số đo mới và báo động**, không ghi đè số đo chưa được ingest xác nhận.
   - Chỉ xóa lô khỏi đệm khi ingest đã xác nhận, tức lô đã commit.
   - Độ đầy đệm là chỉ số nghiệp vụ có cảnh báo (mục 19).
7. **Dạng chuẩn và chữ ký** (`contracts/edge/batch.v1.json`, `edgewire`):
   - Ký trên **dạng chuẩn văn bản ASCII/LF cố định**, không ký trên JSON. Dòng đầu `agrigreen.edge.batch.v1` dùng để tách miền.
   - `key_id` = hex 8 byte đầu SHA-256(khóa công khai).
   - `value_kg` là decimal có đúng 3 chữ số thập phân (R-10). Nhờ vậy dạng chuẩn tái dựng được byte-cho-byte từ cột DB.
   - Bộ vector `batch.v1.vectors.json`: 2 lô hợp lệ và 17 ca hỏng có mã lỗi ổn định.
8. **Ranh giới mã.**
   - `cmd/edge-agent` chỉ được import thư viện chuẩn, `internal/shared/{clock,ids,mass}` và `edgewire`. Không pgx, không DB, không module nghiệp vụ.
   - `edgewire` chỉ dùng thư viện chuẩn. `tools/archlint` cưỡng chế cả hai.
   - Cập nhật agent: tải bản đã ký, xác minh chữ ký, lùi lại được (mục 14). Làm ở G3-1 cùng mẫu cân.

## Hệ quả

- Hai lớp độc lập: mTLS chặn kết nối lạ, còn chữ ký nguồn chứng minh nội dung. Lộ một khóa chỉ ảnh hưởng một kỳ của một thiết bị.
- Phiếu cân (R-70) ghi `rule_version` cùng các `reading_batch.id` đã dùng, nên có thể tính lại và xác minh lại khi tranh chấp.
- Còn chờ: mẫu cân và giao thức (C-6). C-6 quyết định bộ điều hợp và thuật toán tích phân `weigh.integrate.v1`, nhưng không đổi hợp đồng dây. CA nội bộ production (G4, PKI trong lãnh thổ).
