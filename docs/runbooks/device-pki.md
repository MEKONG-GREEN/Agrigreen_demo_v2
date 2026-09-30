# PKI thiết bị biên — CA thiết bị riêng trong lãnh thổ

Áp dụng cho bề mặt nhận lô của `cmd/ingest` (vùng mkg G3; mục 14, 17, RB-7; ADR-0019). Đăng ký/duyệt kỳ thiết bị trong sổ xem [ingest.md](ingest.md) mục 3–5; cài agent xem [edge-agent.md](edge-agent.md).

## 1. Nguyên tắc

- **Hai CA tách biệt.**
  - **CA dịch vụ nội bộ** ký chứng thư máy chủ (`ingest`, listener Internal của `api-platform`) và client dịch vụ (`optimizer`).
  - **CA thiết bị** chỉ ký chứng thư client của thiết bị biên: CN = SAN DNS = `device_id`, EKU `clientAuth`.
- **Ingest chỉ tin CA thiết bị** cho client auth. `AG_INGEST_CLIENT_CA` chỉ chứa (các) CA thiết bị. Chứng thư dịch vụ nội bộ vì thế **không bắt tay được** với cổng 8443.
- **Kiểm lúc khởi động.** Khi `AG_ENV=production`, hoặc `AG_INGEST_REQUIRE_DEVICE_CA=1` (Compose dev bật), ingest **từ chối khởi động** trong hai trường hợp:
  - một CA trong `AG_INGEST_CLIENT_CA` đã ký chứng thư máy chủ `AG_INGEST_TLS_CERT` (lỗi `ingest: AG_INGEST_CLIENT_CA phải là CA thiết bị riêng…`);
  - tệp chứa chứng thư không phải CA.

  Kiểm thử: `TestRequireSeparateDeviceCA`.
- **Hai lớp tin cậy độc lập.**
  - Chứng thư mTLS chỉ mở **kênh**.
  - Quyền ghi đến từ sổ thiết bị đã duyệt hai người (khóa ký nguồn Ed25519 + `cert_sha256`, R-56).

  Lộ CA thiết bị chưa đủ để ghi số đo: kẻ tấn công vẫn cần một kỳ đã duyệt khớp khóa ký.
- **Trong lãnh thổ (RB-7).** CA gốc, CA trung gian, CRL/OCSP và kho lưu khóa đặt tại hạ tầng trong Việt Nam của MKG/nhà cung cấp đã duyệt ([residency.md](residency.md)). Không dùng CA công cộng hay dịch vụ PKI ngoài lãnh thổ.

## 2. Dev (Local/Compose)

`scripts/gen-dev-certs.sh` / `.ps1` sinh vào `deploy/compose/.certs/` (gitignore):

| Tệp | Ký bởi | Dùng |
|---|---|---|
| `ca.crt/.key` | tự ký — `agrigreen-dev-internal-ca` | ký `api-platform`, `optimizer`, `ingest` (máy chủ) |
| `device-ca.crt/.key` | tự ký — `agrigreen-dev-device-ca` | ký `device-<id>.crt` (client thiết bị) |
| `device-edge-sim-01.crt/.key` | `device-ca` | agent mô phỏng `edge-sim` |

Compose đặt các biến sau cho ingest:
- `AG_INGEST_CLIENT_CA=/certs/device-ca.crt`;
- `AG_INGEST_REQUIRE_DEVICE_CA=1`.

Agent vẫn xác thực máy chủ ingest bằng `ca.crt`.

Tệp cũ (trước G3) không có `device-ca.crt`: script tự sinh lại **toàn bộ**. Chứng thư thiết bị đổi thì `cert_sha256` đổi, nên kỳ trong sổ không còn khớp. Ở dev: `docker compose down -v` hoặc chuyển kỳ theo [ingest.md](ingest.md) mục 2.

## 3. Production — dựng CA thiết bị (một lần, hai người)

1. **CA gốc thiết bị** ngoại tuyến:
   - máy không nối mạng, khóa trong HSM hoặc token FIPS 140-2 mức 2 trở lên, trong két tại trụ sở MKG;
   - thuật toán EC P-256 hoặc P-384, hạn 10 năm, `basicConstraints=critical,CA:TRUE,pathlen:1`, `keyUsage=critical,keyCertSign,cRLSign`;
   - nghi thức dựng khóa có **hai người** (người giữ khóa + người chứng kiến) và biên bản.
2. **CA trung gian thiết bị** (ký hằng ngày):
   - khóa trong HSM của cụm PKI nội bộ trong lãnh thổ;
   - hạn 3–5 năm, `pathlen:0`;
   - `nameConstraints` giới hạn `DNS` theo quy ước `device_id` nếu công cụ CA hỗ trợ;
   - CA gốc ký, rồi CA gốc trở lại ngoại tuyến.
3. **Tách khỏi CA dịch vụ.** Tên, khóa, HSM partition và người vận hành khác với CA dịch vụ nội bộ. Không bao giờ ký chứng thư máy chủ hay client dịch vụ bằng CA thiết bị, và ngược lại.
4. **Cấu hình ingest:**
   - `AG_INGEST_CLIENT_CA` = PEM của CA trung gian thiết bị (và CA gốc nếu cần chuỗi đầy đủ);
   - `AG_INGEST_TLS_CERT/KEY` = chứng thư máy chủ do **CA dịch vụ** ký;
   - `AG_ENV=production` bật kiểm tách CA ở mục 1.
5. **CRL/OCSP:**
   - công bố CRL của CA trung gian tại địa chỉ nội bộ trong lãnh thổ, hạn CRL ≤ 7 ngày, cập nhật ngay khi thu hồi;
   - G3 ingest chưa đọc CRL, vì thu hồi có hiệu lực tức thì qua sổ thiết bị (`revoke` → `403 ingest.device_revoked`). CRL phục vụ các bên khác tin CA (VPN, kiểm toán).

## 4. Cấp chứng thư cho thiết bị (mỗi kỳ)

1. Trên thiết bị: `edge-agent enroll -epoch <N>` sinh khóa **trên máy**, CSR và `csr_sha256` ([edge-agent.md](edge-agent.md) mục 3). Khóa riêng không rời thiết bị.
2. CSR tới người vận hành CA qua kênh nội bộ. **Đối chiếu `csr_sha256` ngoài băng** (gọi điện, đọc tại chỗ) — không tin tệp gửi qua email.
3. Người vận hành CA thiết bị ký với các thuộc tính:
   - CN = SAN DNS = `device_id` (regex `^[a-z0-9][a-z0-9._-]{2,63}$`);
   - EKU `clientAuth` **duy nhất**, `keyUsage=critical,digitalSignature`, `basicConstraints=critical,CA:FALSE`;
   - hạn ≤ 825 ngày.

   Ghi sổ cấp phát của CA: serial, `device_id`, kỳ, người ký, người đối chiếu.
4. Đặt chứng thư vào `ingest.cert_file`. `edge-agent info` in `cert_sha256`, rồi đề xuất và duyệt kỳ hai người ([ingest.md](ingest.md) mục 3).

## 5. Thu hồi, thay CA

- **Thiết bị mất, nghi lộ khóa:**
  - thu hồi kỳ trong sổ ([ingest.md](ingest.md) mục 3), có hiệu lực ngay;
  - thu hồi serial ở CA, công bố CRL mới trong 1 giờ.
- **Gia hạn định kỳ:** kỳ mới + chứng thư mới ([ingest.md](ingest.md) mục 4). Không gia hạn chứng thư cũ với cùng khóa.
- **Thay CA trung gian thiết bị** (hết hạn, nghi lộ):
  1. phát hành CA trung gian mới từ CA gốc (nghi thức hai người);
  2. gộp PEM **hai CA thiết bị** vào `AG_INGEST_CLIENT_CA`, khởi động lại ingest. Kiểm tách CA chấp nhận bó nhiều CA thiết bị, miễn là không lẫn CA dịch vụ;
  3. thay khóa từng thiết bị sang chứng thư do CA mới ký;
  4. gỡ CA cũ khỏi bó, khởi động lại ingest, thu hồi CA cũ ở CA gốc.
- **Nghi lộ CA gốc thiết bị:**
  - sự cố P1: dừng cấp mới, dựng CA gốc mới (mục 3), thay toàn bộ chứng thư thiết bị;
  - số đo đã nhận vẫn giữ nguyên (RB-1). Chữ ký nguồn Ed25519 + sổ thiết bị vẫn chứng minh nguồn gốc lô; nếu nghi giả mạo, mở tranh chấp cho các lô liên quan.

## 6. Kiểm tra định kỳ (hằng quý)

- [ ] `AG_INGEST_CLIENT_CA` trên mọi bản ingest chỉ chứa CA thiết bị; `openssl verify -CAfile <client-ca> ingest.crt` phải **thất bại**.
- [ ] Mẫu một chứng thư dịch vụ (vd `optimizer`) bị từ chối ở bắt tay TLS cổng 8443.
- [ ] Sổ cấp phát CA khớp các kỳ `active` của `GET /v1/measurement/devices` (không có chứng thư cấp mà không có kỳ duyệt).
- [ ] CRL còn hạn; khóa CA gốc vẫn ngoại tuyến (biên bản kiểm két).
