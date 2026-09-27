# App hiện trường AgriGreen (`apps/field-app`)

App Flutter ngoại tuyến cho cán bộ khuyến nông xã, quản lý HTX và nông dân (ADR-0010). Nói chuyện với máy chủ theo `docs/dev/sync-contract.md`. Mã hóa ảnh theo `docs/dev/envelope-format.md` (mục 10.2, ADR-0004).

- Flutter 3.47 / Dart 3.13.
- Android 8.0 (API 26) trở lên.
- Giao diện tiếng Việt, chữ lớn, nút cao ≥ 56 dp.

## Kiến trúc

```
lib/
├── core/          # Clock, UUIDv7, backoff, cấu hình (--dart-define)
├── domain/        # thuần Dart: OutboxRecord, hình học ranh giới (≥4 điểm, tự cắt, diện tích ha, chồng lấn)
├── data/          # hạ tầng
│   ├── db/        # LocalDb + DbOpener (SQLCipher trên máy, FFI trong test), lược đồ, trigger chỉ ghi thêm
│   ├── secure/    # SecureStore (flutter_secure_storage / Android Keystore)
│   ├── outbox/    # hàng đợi chiều lên: seq bền, idem_key, lô, kết quả từng bản ghi, purge 48 giờ
│   ├── reference/ # dữ liệu chiều xuống: upsert/tombstone theo version, cursor, khóa công khai hộ
│   ├── sync/      # HttpSyncApi (hợp đồng), SyncService (tải tệp → push → pull → purge, lùi thử lại)
│   ├── crypto/    # phong bì AES-256-GCM + crypto_box_seal
│   ├── files/     # ciphertext ảnh trên đĩa + bảng evidence_file
│   ├── photo/     # camera, GPS, pipeline chụp → mã hóa
│   └── auth/      # OIDC PKCE, phiên 72 giờ, PIN, đăng ký thiết bị, xóa từ xa
├── application/   # use case: báo cáo hiện trường, ranh giới, nhiệm vụ, luồng đăng nhập, AppServices
└── ui/            # Riverpod (MIT) + màn hình
```

Quản lý trạng thái dùng Riverpod. `AppController` giữ luồng đăng nhập: đăng nhập trực tuyến → đặt PIN → mở khóa. CSDL chỉ mở khi đã mở khóa.

Màn hình:
- đăng nhập;
- PIN (đặt PIN, mở khóa);
- trang chủ: trạng thái đồng bộ, số bản ghi chờ, bị từ chối, xung đột, số nhiệm vụ;
- danh sách thửa;
- vẽ ranh giới trên bản đồ `flutter_map`: chấm điểm, lấy điểm GPS, cảnh báo chồng lấn;
- báo cáo hiện trường / nhật ký canh tác;
- chụp ảnh minh chứng;
- nhiệm vụ hỗ trợ;
- hàng đợi đồng bộ, có nút tạo bản ghi bù.

### Đồng bộ — bảo đảm không mất, không trùng

- **ID và khóa chống trùng.** Mỗi bản ghi có `id` UUIDv7 và `idem_key = dev:<device_id>:<seq>`.
  - `seq` lấy từ bộ đếm lưu trong bảng `meta`. Bộ đếm được tăng **trong cùng giao dịch** với lệnh chèn bản ghi.
  - `seq` không bao giờ tái dùng, kể cả sau khi đã xóa bản ghi cũ.
- **Chỉ ghi thêm.** `op` chỉ có `create` hoặc `correct`.
  - Trigger SQLite chặn `UPDATE` lên nội dung bản ghi.
  - Sửa sai = tạo bản ghi bù có `supersedes_id` và `correction_reason`.
  - Với nhật ký canh tác, GPS và `captured_at` bị khóa (HTX US-LOG-01 AC-3).
- **Gửi theo lô.** Khi lấy lô gửi, máy gán `batch_key` cho các bản ghi và lưu xuống CSDL.
  - Nếu app chết giữa chừng hoặc mất phản hồi, lần sau gửi lại **đúng lô đó**: cùng `Idempotency-Key`, cùng thân từng byte. Máy chủ sẽ phát lại phản hồi cũ.
  - Nếu máy chủ trả `422 idempotency_key_reused`, máy bỏ gán lô và gửi lại trong lô mới. `idem_key` của từng bản ghi giữ nguyên nên máy chủ vẫn chống trùng.
- **Kết quả từng bản ghi:**

  | Kết quả | Máy làm gì |
  |---|---|
  | `accepted` / `duplicate` | Đánh dấu đã gửi. Giữ 48 giờ rồi xóa. |
  | `conflict` | Coi là đã gửi. Hiển thị trong hàng đợi. Giữ 48 giờ rồi xóa. |
  | `rejected` | Giữ lại, hiển thị lỗi, người dùng tạo bản ghi bù. Khi bản bù được nhận, bản gốc chuyển `superseded` và được xóa sau 48 giờ. |
  | Không có trong phản hồi | Vẫn chờ, gửi ở lô sau. |

- **Lùi thử lại.** Khi lỗi mạng hoặc lỗi 5xx/429/409-in-progress, máy chờ lâu dần theo lũy thừa: 5 giây × 2ⁿ, tối đa 30 phút, có jitter.
  - Số lần lỗi và thời điểm thử lại lưu trong CSDL, nên sống qua lần khởi động lại.
  - Có mạng trở lại (`connectivity_plus`) thì đồng bộ ngay.
- **Pull.**
  - Mỗi trang và con trỏ mới được áp **trong một giao dịch**. App chết giữa trang thì lần sau pull lại từ con trỏ cũ. Áp lặp vô hại vì máy so `version`.
  - `tombstone` ẩn bản ghi và bỏ dữ liệu khỏi máy.
  - Nông hộ chỉ giữ các trường nhãn: `label`, `display_label`, `code`, `cooperative_id`, `admin_unit_code`, `status`. Họ tên, SĐT, CCCD, địa chỉ bị loại trước khi ghi (mục 10.4).
- **Ảnh minh chứng.** Luồng xử lý:
  1. Chụp ảnh.
  2. Gắn GPS và thời gian vào metadata.
  3. Mã hóa phong bì **ngay** bằng khóa công khai của hộ.
  4. Chỉ ghi ciphertext xuống đĩa.
  5. `POST /v1/files` với `Idempotency-Key = file:<id>`.
  6. Trong **một giao dịch**: đánh dấu đã tải và tạo bản ghi `evidence` tham chiếu `file_ref` vào hàng đợi.
  7. Xóa ciphertext cục bộ.

## Lệnh

Chạy trong PowerShell tại gốc worktree:

```powershell
. .\scripts\dev-env.ps1          # đưa flutter vào PATH
cd apps\field-app
flutter pub get
flutter analyze                  # phải: No issues found!
flutter test                     # Dart VM; CSDL qua sqflite_common_ffi
```

Chạy trên máy/emulator Android (cần Android SDK):

```powershell
flutter run --dart-define=AG_API_BASE=https://api.dev.agrigreen.vn `
            --dart-define=AG_OIDC_ISSUER=https://id.dev.agrigreen.vn/realms/agrigreen
flutter build apk --release --dart-define=...
```

Kiểm thử chính (`test/`):

| Tệp | Nội dung |
|---|---|
| `outbox_test.dart` | UUIDv7/idem_key/seq; crash + mở lại; rollback giữa giao dịch; seq không tái dùng sau purge; trigger chỉ ghi thêm; bản ghi bù; khóa GPS/thời gian |
| `sync_push_test.dart` | lô 100; accepted/duplicate/conflict/rejected; purge 48 giờ; bản bù cho bản bị từ chối; mất phản hồi + crash → gửi lại y nguyên; lùi lũy thừa bền qua khởi động lại; 422; 25 vòng lỗi ngẫu nhiên vẫn **đúng một lần**; gọi chồng nhau |
| `sync_pull_test.dart` | cursor nhiều trang; version/tombstone; loại PII nông hộ; khóa công khai hex/base64; rollback cả trang |
| `auth_test.dart` | phiên 72 giờ; làm mới gia hạn; phát hiện lùi đồng hồ; PIN 6 số; khóa 15 phút sau 5 lần sai; bộ đếm bền; hủy PIN sau 10 lần; luồng đăng nhập |
| `wipe_test.dart` | 403 `sync.device_revoked` → xóa CSDL, ciphertext, toàn bộ kho bí mật; 401 không xóa |
| `envelope_test.dart`, `envelope_vectors_test.dart` | round-trip, sửa tag, sai khóa; vector Go `nacl/box.SealAnonymous` (`test/fixtures/go_nacl_vectors.json`); bộ chung `internal/shared/envelope/testdata/vectors.json` (bỏ qua nếu chưa có) |
| `boundary_test.dart` | ≥4 điểm, thẳng hàng, tự cắt, lõm, diện tích ha (sai lệch <1 %), chồng lấn, GeoJSON, chặn/xác nhận chồng lấn |
| `photo_pipeline_test.dart` | không có bản rõ trên đĩa; tải tệp mất phản hồi + crash → một bản ghi evidence; nhiệm vụ |
| `http_sync_api_test.dart` | hình dạng request/response theo hợp đồng |

## Ghi chú bảo mật

- **CSDL:**
  - SQLCipher (`sqflite_sqlcipher`), khóa ngẫu nhiên 256 bit truyền dạng raw key `x'…'`.
  - Khóa nằm trong `flutter_secure_storage` (Android Keystore). Không bao giờ ở SharedPreferences hay tệp.
  - Bật `PRAGMA secure_delete`.
- **Kho bí mật:**
  - Token OIDC, mốc xác thực trực tuyến, băm PIN (PBKDF2-HMAC-SHA256, muối 16 byte, 60 000 vòng) và bộ đếm sai PIN đều nằm trong kho bí mật.
  - Không lưu PIN rõ.
- **Phiên ngoại tuyến:**
  - Tối đa 72 giờ kể từ lần xác thực trực tuyến cuối (đăng nhập hoặc làm mới token).
  - Đồng hồ máy bị lùi thì coi như hết hạn.
- **PIN:**
  - Sai 5 lần liên tiếp → khóa 15 phút (theo US-ADM).
  - Sai tổng 10 lần → hủy PIN, bắt buộc đăng nhập trực tuyến.
- **Thu hồi thiết bị:**
  - Khi gặp `403 sync.device_revoked`, app xóa kho bí mật trước (khóa token và khóa CSDL), rồi xóa tệp CSDL và thư mục ciphertext.
  - Dữ liệu chưa gửi bị mất. Đây là chủ ý theo mục 15.
- **Ảnh:**
  - Mã hóa ngay khi chụp. DEK và bộ đệm bản rõ bị ghi đè 0 sau khi dùng.
  - Máy không bao giờ có khóa bí mật của hộ.
  - **Hạn chế R-40:** `image_picker` ghi JPEG tạm vào cache; app đọc xong xóa ngay. TODO: dùng package `camera` chụp vào bộ nhớ.
- **GPS là dữ liệu cá nhân:**
  - Tọa độ ảnh chỉ nằm trong metadata phong bì. Bản ghi `evidence` chỉ có `gps_status`.
  - Không ghi log tọa độ.
- **Android:**
  - `allowBackup=false`, `usesCleartextTraffic=false`.
  - OIDC PKCE qua AppAuth (client `agrigreen-field-app`, redirect `vn.agrigreen.field:/oauth2redirect`, `prompt=login`).

## Dependency và giấy phép

Mọi gói thuộc `deploy/policies/allowed-licenses.txt`. Đã quét toàn bộ `pubspec.lock`: 84 BSD, 24 MIT, 8 Apache-2.0, 4 MPL-2.0. Bốn gói MPL-2.0 (`dbus`, `nm`, `geoclue`, `gsettings`) là gói bắc cầu, chỉ dùng trên Linux.

| Gói | Giấy phép | Dùng cho |
|---|---|---|
| flutter_riverpod 3.4 | MIT | quản lý trạng thái |
| sqflite_sqlcipher 3.4 | MIT (SQLCipher Community: BSD-style) | CSDL mã hóa trên máy |
| sqflite_common 2.5 | BSD-2-Clause | API CSDL chung |
| sqflite_common_ffi 2.4 (dev) | BSD-2-Clause | CSDL trong kiểm thử |
| flutter_secure_storage 11.2 | BSD-3-Clause | Keystore |
| uuid 4.6 | MIT | UUIDv7 |
| cryptography 2.9 | Apache-2.0 | AES-256-GCM, PBKDF2 |
| pinenacl 0.6 | MIT | sealed box X25519 |
| crypto 3.0 | BSD-3-Clause | SHA-256 |
| http 1.6 | BSD-3-Clause | HTTP |
| flutter_appauth 12.1 | BSD-3-Clause (AppAuth-Android: Apache-2.0) | OIDC PKCE |
| flutter_map 8.3 | BSD-3-Clause | bản đồ |
| latlong2 0.10 | Apache-2.0 | tọa độ |
| geolocator 14.1 | MIT | GPS |
| image_picker 1.2 | Apache-2.0 + BSD-3-Clause | camera |
| connectivity_plus 7.3 | BSD-3-Clause | phát hiện có mạng |
| path_provider 2.1, path 1.9 | BSD-3-Clause | thư mục dữ liệu |
| flutter_lints 6 (dev) | BSD-3-Clause | lint |

Dữ liệu bản đồ OSM dùng giấy phép ODbL. Máy chủ `tile.openstreetmap.org` chỉ để dev. Không dùng cho sản xuất: phải có máy chủ tile riêng hoặc gói tile ngoại tuyến.

## Cần Android SDK / CI

Không làm được trên máy không có Android SDK:
- `flutter build apk`, `flutter run`;
- kiểm thử tích hợp trên emulator: SQLCipher thật (xác nhận raw key `x'…'` mở được), Keystore, AppAuth với Keycloak, camera/GPS, `connectivity_plus`.

Job CI đề xuất (người tích hợp thêm vào `.github/workflows/ci.yml`):

```yaml
field-app:
  runs-on: ubuntu-latest
  defaults: { run: { working-directory: apps/field-app } }
  steps:
    - uses: actions/checkout@v4
    - uses: subosito/flutter-action@v2
      with: { flutter-version: '3.47.5', channel: stable, cache: true }
    - run: sudo apt-get install -y libsqlite3-dev   # sqflite_common_ffi trên Linux
    - run: flutter pub get --enforce-lockfile
    - run: dart format --output=none --set-exit-if-changed lib test
    - run: flutter analyze --fatal-infos
    - run: flutter test
    - run: flutter build apk --debug                 # cần Android SDK (có sẵn trên ubuntu-latest)
```

## Việc còn lại (TODO)

- **Watermark:** in chữ GPS và thời gian lên ảnh (`NoopWatermarker`). Metadata đã có.
- **Bản đồ ngoại tuyến (US-MAP-03):** gói tile theo địa bàn, ví dụ MBTiles. Tự chuyển sang bản đồ ngoại tuyến khi mất mạng.
- **Chụp ảnh:** dùng `camera` thay `image_picker` để bản rõ không chạm đĩa.
- **Đồng bộ nền:** WorkManager khi app đóng.
- **Màn hình đồng ý** (`consent`): thực thể đã hỗ trợ trong hàng đợi.
- **Ranh giới:** tải tệp KML/GeoJSON (US-PLOT-02); vẽ hình tròn/vuông.
- **Bản ghi bù sau khi purge:** bản gốc đã bị xóa khỏi máy sau 48 giờ. Muốn sửa phải lấy bản gốc từ máy chủ (cần API đọc).
- **Chống chụp màn hình:** `FLAG_SECURE`; khóa khi app ra nền.
- **Ký bản phát hành:** keystore từ secret CI.

## Điểm chưa rõ trong hợp đồng

1. **Mã hóa chuỗi khóa công khai** trong `pubkey.data`: chưa nói hex hay base64, cũng chưa nói tên trường. App nhận cả hai, tên `public_key` và `subject_ref`.
2. **Phần `meta` của `POST /v1/files`:** chưa rõ là trường văn bản hay phần tệp. App gửi dạng trường văn bản (không filename); `blob` là phần tệp.
3. **Bản ghi bù cho bản bị `rejected`:** máy chủ chưa từng ghi bản gốc, nên `supersedes_id` trỏ tới một id máy chủ không có. Cần chốt: máy chủ chấp nhận `correct` như vậy, hay app phải gửi `create` mới?
4. **Mã lỗi thu hồi:** app đọc `code` trong problem+json (theo `internal/shared/problem`). `403` với mã khác không xóa dữ liệu.
5. **Payload từng `entity`:** chưa có schema. App đang dùng:
   - `plot_boundary`: `plot_id`, `geometry` (GeoJSON), `area_ha_client` (chuỗi 4 chữ số thập phân), `method`, `overlap_*`;
   - `field_report`: `activity`, `gps`, `gps_status`, `captured_at`, `machinery_ids`;
   - `task_update`: `task_id`, `status`, `note`, `result`;
   - `evidence`: `file_ref`, `parent_*`, `sha256_ciphertext`.
6. **Định dạng `vectors.json`:** chưa quy định tên trường. Đề xuất theo `test/fixtures/go_nacl_vectors.json`: `{v, alg, vectors:[{name, recipient_sk_hex, recipient_pk_hex, plaintext_hex, dek_hex, nonce_hex, ciphertext_hex (ct‖tag), wrapped_dek_hex, sha256_ciphertext}]}`. Test đọc được nhiều biến thể tên.
7. **`Idempotency-Key` của đăng ký thiết bị** (`devreg:<device_id>`) và tệp (`file:<id>`) do app tự đặt. Hai khóa này hợp regex `[A-Za-z0-9._:-]{8,200}` của máy chủ.
8. **Thời hạn khóa idempotency:** máy chủ giữ khóa 48 giờ. Nếu lô gửi lại sau hơn 48 giờ, máy chủ coi là yêu cầu mới. Vẫn an toàn vì chống trùng theo `idem_key`.
