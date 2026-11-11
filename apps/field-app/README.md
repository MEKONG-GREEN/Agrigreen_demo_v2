# App hiện trường AgriGreen (`apps/field-app`)

App Khuyến nông (App KN): app Flutter ngoại tuyến cho cán bộ khuyến nông Cấp Xã (ADR-0010). Bản build chỉ có màn KN theo BRD 2.0 và chỉ gọi route của module KN + hạ tầng KN (KN-STANDALONE-9 #270); quản lý HTX và nông dân dùng `apps/htx-app`. Nói chuyện với máy chủ theo `docs/dev/sync-contract.md`. Mã hóa ảnh theo `docs/dev/envelope-format.md` (mục 10.2, ADR-0004).

- Flutter 3.47 / Dart 3.13.
- Android 8.0 (API 26) trở lên.
- Giao diện tiếng Việt, chữ lớn, nút cao ≥ 56 dp.

## Kiến trúc

Lõi dùng chung với `apps/htx-app` nằm ở package `packages/agrigreen_mobile` (path dependency, docs/dev/g2-conventions.md §1); app này chỉ giữ luồng và màn hình của khuyến nông.

```
packages/agrigreen_mobile/lib/
├── agrigreen_mobile.dart  # thư viện gộp (export toàn bộ lõi); testing.dart: FakeServer, FakeOidc
├── core/          # Clock, UUIDv7, backoff, cấu hình (--dart-define)
├── domain/        # thuần Dart: OutboxRecord, hình học ranh giới, toán tile XYZ, deep link, cổng đồng ý, MRV
├── data/          # hạ tầng
│   ├── db/        # LocalDb + DbOpener (SQLCipher trên máy, FFI trong test), lược đồ + migration, trigger chỉ ghi thêm
│   ├── secure/    # SecureStore (flutter_secure_storage / Android Keystore)
│   ├── outbox/    # hàng đợi chiều lên: seq bền, idem_key, lô, kết quả từng bản ghi, purge 48 giờ
│   ├── reference/ # dữ liệu chiều xuống: upsert/tombstone theo version, cursor, khóa công khai hộ
│   ├── sync/      # HttpSyncApi (hợp đồng, X-Device-Id), SyncService (tải tệp → push → phát lại lệnh → pull → purge)
│   ├── actions/   # ActionQueue: lệnh API idempotent xếp hàng khi ngoại tuyến (đã đọc, xác nhận chỉ đạo, lưu tài liệu)
│   ├── catalog/   # CatalogCache: danh mục masterdata + danh mục trường MRV tải qua API
│   ├── push/      # PushMessaging (FCM chỉ dữ liệu / bản giả), LocalNotifier, PushTokenRegistrar
│   ├── map/       # TileStore: gói bản đồ ngoại tuyến kiểu MBTiles trong SQLCipher
│   ├── crypto/    # phong bì AES-256-GCM + crypto_box_seal
│   ├── files/     # ciphertext ảnh trên đĩa + bảng evidence_file / evidence_form
│   ├── photo/     # camera, GPS, đóng dấu (watermark) trong bộ nhớ, pipeline chụp → mã hóa
│   ├── dial/      # Dialer (tel:/sms:)
│   └── auth/      # OIDC PKCE, phiên 72 giờ, PIN, đăng ký thiết bị, xóa từ xa
├── application/   # AuthFlow, InboxService, PushCoordinator, TilePackService
└── ui/            # chủ đề, widget chung (BigTile, StatusChip, …), OfflineFirstTileProvider

apps/field-app/lib/
├── domain/        # ngữ cảnh phiên KN, báo cáo theo biểu mẫu, biểu kỳ CNTS, lịch công tác
├── application/   # use case KN: ngữ cảnh, menu, báo cáo (TT FN-XA-03.02), biểu kỳ CNTS (FN-08/09), thu thập dữ liệu
│                  # (FN-XA-03.03..03.06), lịch công tác (FN-XA-03.08, CNTS FN-12), AppServices (ghép toàn bộ)
└── ui/            # Riverpod (MIT) + màn hình
```

Quản lý trạng thái dùng Riverpod. `AppController` giữ luồng đăng nhập: đăng nhập trực tuyến → vào thẳng; phiên ngoại tuyến còn hạn (72 giờ) mở thẳng, bảo vệ bằng khóa màn hình của thiết bị. App KN không có PIN (BRD 2.0 FN-CH-00.01, FN-05 — ADR-0057). Đăng xuất đóng CSDL và xóa token.

Màn hình (chỉ màn KN theo BRD 2.0; trang chủ cũ và 10 màn ngoài BRD — thửa/ranh giới HTX, minh chứng MRV, chỉ đạo, thư viện, danh bạ trực, báo cáo hiện trường, nhiệm vụ hỗ trợ, đồng ý, hộp thư — đã gỡ khỏi app, #270):
- đăng nhập; chọn vai trò và phòng ban; **"Không có quyền"** cho tài khoản không có ngữ cảnh KN (chỉ đăng xuất được);
- trang Cấp Xã: menu từ `GET /v1/menu`, "Công việc cần làm", nhập báo cáo theo biểu mẫu, "Báo cáo của xã";
- biểu kỳ CNTS, danh mục CNTS ngoại tuyến;
- thu thập dữ liệu: HTX/nông hộ/tổ hợp tác (hồ sơ KN), máy móc, vẽ thửa ruộng (bản đồ `flutter_map`, gói bản đồ ngoại tuyến), vật tư;
- lịch công tác tuần, check-in/check-out;
- hàng đợi đồng bộ, có nút tạo bản ghi bù và nút "Xác nhận giữ nguyên" cho ranh giới.

### Thông báo và kênh đẩy (pf/notify, ADR-0015)

- Nội dung thông báo = entity đồng bộ `notification` (30 ngày; máy tự bỏ bản cũ hơn) — dùng để hiện thông báo cục bộ; app không còn màn hộp thư.
- **FCM chỉ dữ liệu** sau lớp `PushMessaging` (kiểm thử dùng `FakePushMessaging`, không cần Firebase). Chỉ bật khi có `--dart-define=AG_FCM_PROJECT_ID/AG_FCM_APP_ID/AG_FCM_API_KEY/AG_FCM_SENDER_ID`; không dùng `google-services.json`.
  - `{kind: "sync_now"}` → đồng bộ ngay (US-SYNC-02). Đang khóa → bỏ qua; app đồng bộ khi mở khóa.
  - `sync_now` có thể mang `entities: "kn_plot,report_task"` (KN US-SYNC-02) → pull `GET /v1/sync/pull?entities=…` chỉ các entity đó, giao với danh sách nguồn KN của app (docs/dev/sync-contract.md).
  - `{kind, notification_id}` → pull, rồi hiện thông báo cục bộ (`flutter_local_notifications`). **Tiêu đề "Có thông báo mới"** khi app chưa mở khóa, không ở tiền cảnh, hoặc nội dung chưa về; chỉ hiện tiêu đề thật khi đã mở khóa và đang mở app. `visibility: private` trên Android. Thông điệp khi app tắt/ở nền: CSDL đang khóa nên chỉ hiện thông báo chung.
- **Token đẩy**: `POST /v1/devices/{device_id}/push-token {provider: fcm, token}` ngay sau đăng ký thiết bị (trong `beforeSync`), mỗi lần mở khóa và khi FCM đổi token. Token chỉ nằm trong CSDL SQLCipher; khóa idempotency chứa băm SHA-256 rút gọn của token, không chứa token.

### Ranh giới — hợp đồng pf/geo

- Payload `plot_boundary` theo `BoundaryPayload`: `plot_ref`, `commune_code`, `cooperative_ref`, `household_ref`, `geometry`, `gps_accuracy_m` và `declared_area_ha` (**chuỗi** thập phân), `source = kn_field_app`, `confirm_overlap`.
- Thửa mới: `op: create`, bắt buộc chọn xã. Thửa đã có `boundary_id`: `op: correct` với `supersedes_id = boundary_id`, lý do ≥ 10 ký tự.
- Lệch diện tích khai báo > 5 % hoặc chồng lấn cùng HTX → hỏi xác nhận trước khi lưu → `confirm_overlap: true`.
- Sau phản hồi máy chủ: `conflict` chưa xác nhận hoặc `rejected` mã `geo.overlap_needs_confirm` / `geo.area_deviation_needs_confirm` → trạng thái "Chồng lấn — cần xác nhận" ở danh sách thửa và hàng đợi. Người vẽ xác nhận:
  - `rejected` → bản ghi **mới** `create` cùng nội dung + `confirm_overlap: true` (máy chủ không lưu bản bị từ chối); bản gốc chuyển `superseded` khi bản mới được nhận;
  - `conflict` → bản ghi bù `correct` trỏ tới bản ghi xung đột.
- `rejected` mã `sync.sink_failed`/`internal_error` → giữ `pending`, lùi thử lại (không bắt người dùng tạo bản bù).

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
  - **Chỉ kéo nguồn KN.** Mọi lượt pull gửi `?entities=` = `knPullEntities` (`application/app_services.dart`): `admin_unit`, `catalog_item`, `notification`, `kn_plot`, `report_task`, `form_layout`, `report_submission`, `forms_task`. Không kéo nguồn HTX/MRV/nông dân (`plot`, `season`, `farm_log`, `sop_version`, `mrv_catalog`, `support_request`, `pubkey`) hay nguồn của màn cũ (`task`, `directive`, `document`, `library_item`). `sync_now` có `entities` → giao với danh sách này; trang `reset` nạp lại đúng danh sách này (`SyncService.pullEntities` ở lõi; App HTX/MKG không khai nên vẫn pull tất cả).
  - Mỗi trang và con trỏ mới được áp **trong một giao dịch**. App chết giữa trang thì lần sau pull lại từ con trỏ cũ. Áp lặp vô hại vì máy so `version`.
  - `tombstone` ẩn bản ghi và bỏ dữ liệu khỏi máy.
  - `reset: true` → thay toàn bộ dữ liệu tham chiếu. `400 sync.cursor_invalid` → pull lại từ đầu (một lần).

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

Kiểm thử chính (`test/` của app; tệp đánh dấu *lõi* nằm ở `packages/agrigreen_mobile/test/`):

| Tệp | Nội dung |
|---|---|
| *lõi* `outbox_test.dart` | UUIDv7/idem_key/seq; crash + mở lại; rollback giữa giao dịch; seq không tái dùng sau purge; trigger chỉ ghi thêm; bản ghi bù; khóa GPS/thời gian |
| *lõi* `sync_push_test.dart` | lô 100; accepted/duplicate/conflict/rejected; purge 48 giờ; bản bù cho bản bị từ chối; mất phản hồi + crash → gửi lại y nguyên; lùi lũy thừa bền qua khởi động lại; 422; 25 vòng lỗi ngẫu nhiên vẫn **đúng một lần**; gọi chồng nhau |
| *lõi* `sync_pull_test.dart` | cursor nhiều trang; version/tombstone; loại PII nông hộ; khóa công khai hex/base64; rollback cả trang |
| *lõi* `auth_test.dart` | phiên 72 giờ; làm mới gia hạn; phát hiện lùi đồng hồ; PIN 6 số; khóa 15 phút sau 5 lần sai; bộ đếm bền; hủy PIN sau 10 lần; luồng đăng nhập |
| *lõi* `wipe_test.dart` | 403 `sync.device_revoked` → xóa CSDL, ciphertext, toàn bộ kho bí mật; 401 không xóa |
| *lõi* `envelope_test.dart`, `envelope_vectors_test.dart` | round-trip, sửa tag, sai khóa; vector Go `nacl/box.SealAnonymous` (`packages/agrigreen_mobile/test/fixtures/go_nacl_vectors.json`); bộ chung `internal/shared/envelope/testdata/vectors.json` (bỏ qua nếu chưa có) |
| `boundary_test.dart` | ≥3 điểm (A2, tam giác hợp lệ), thẳng hàng, tự cắt, lõm, diện tích ha (sai lệch <1 %), chồng lấn, GeoJSON, chặn/xác nhận chồng lấn |
| *lõi* `photo_pipeline_test.dart` | không có bản rõ trên đĩa; tải tệp mất phản hồi + crash → một bản ghi evidence |
| *lõi* `big_tile_small_screen_test.dart` | BigTile không tràn trên màn hình nhỏ |
| *lõi* `http_sync_api_test.dart` | hình dạng request/response theo hợp đồng |
| *lõi* `http_contract_g1_test.dart` | `X-Device-Id`; push-token; lệnh xếp hàng; `file_ref`; `reset` |
| `inbox_test.dart` | nội dung thông báo (lõi `InboxService`): chưa đọc, đã đọc ngoại tuyến → phát lại đúng một lần qua crash, đọc hết, 404 bỏ lệnh, bỏ bản > 30 ngày; deep link |
| *lõi* `push_test.dart` | `sync_now`; thông báo cục bộ **không lộ nội dung** khi khóa/nền/chưa về; token khi mở khóa/đổi; không có Firebase; `PushTokenRegistrar` (sau đăng ký, mỗi lần khởi động, lỗi 503 không chặn) |
| `watermark_test.dart` | lõi: watermark có trong ảnh đã giải mã; không JPEG bản rõ trên đĩa; `shredFile` |
| *lõi* `sync_pull_entities_test.dart` | `SyncService.pullEntities`: mọi lượt pull chỉ đòi entity của app; giao với `sync_now.entities`; `reset` không rơi về pull tất cả; không khai → như cũ |
| *lõi* `tiles_pull_test.dart` | toán tile; giới hạn khung/zoom/diện tích/số tile/dung lượng; tải, đọc lại, tải tiếp, lỗi tile, dừng; pubkey theo lượt; `reset`; `cursor_invalid` |
| `payload_contract_test.dart` | lõi `domain/mrv.dart`: `evidence` so với fixture JSON rút từ quy tắc Go (`test/fixtures/contract/`); lỗi theo trường như máy chủ |
| `kn_only_surface_test.dart` | #270: mã app không còn lớp màn cũ và không gọi route HTX/MRV/route cũ; menu chỉ mở màn KN; tài khoản không có ngữ cảnh KN → "Không có quyền" |
| `offline_flow_http_test.dart` | kịch bản KN qua **HTTP thật** tới máy chủ giả `dart:io` **không có route HTX/MRV** (cùng kịch bản với emulator): chỉ pull nguồn KN, không lời gọi nào nhận 404 |
| `integration_test/app_flow_test.dart` | **emulator** (CI `field-app-emulator`): đăng nhập chế độ kiểm thử → trang Cấp Xã → đăng ký thiết bị + token → pull nguồn KN → mất mạng: vẽ thửa + gửi báo cáo tuần → có mạng + `sync_now` → push accepted → thông báo đẩy |

## Ghi chú bảo mật

- **CSDL:**
  - SQLCipher (`sqflite_sqlcipher`), khóa ngẫu nhiên 256 bit truyền dạng raw key `x'…'`.
  - Khóa nằm trong `flutter_secure_storage` (Android Keystore). Không bao giờ ở SharedPreferences hay tệp.
  - Bật `PRAGMA secure_delete`.
- **Kho bí mật:**
  - Token OIDC và mốc xác thực trực tuyến nằm trong kho bí mật. Băm PIN cũ (nếu máy từng đặt PIN trước ADR-0057) giữ nguyên, không còn được đọc.
- **Phiên ngoại tuyến:**
  - Tối đa 72 giờ kể từ lần xác thực trực tuyến cuối (đăng nhập hoặc làm mới token).
  - Đồng hồ máy bị lùi thì coi như hết hạn.
- **Thu hồi thiết bị:**
  - Khi gặp `403 sync.device_revoked`, app xóa kho bí mật trước (khóa token và khóa CSDL), rồi xóa tệp CSDL và thư mục ciphertext.
  - Dữ liệu chưa gửi bị mất. Đây là chủ ý theo mục 15.
- **Ảnh:**
  - Mã hóa ngay khi chụp. DEK và bộ đệm bản rõ bị ghi đè 0 sau khi dùng.
  - Máy không bao giờ có khóa bí mật của hộ.
  - **Watermark** (thời gian + GPS, HTX US-LOG-01 AC-4) vẽ bằng package `image` trong isolate, **trên bộ nhớ**, trước khi mã hóa. Ảnh mã hóa là ảnh đã đóng dấu; không lưu bản chưa đóng dấu. Ảnh không giải mã được → giữ nguyên (metadata phong bì vẫn có GPS/thời gian).
  - **Hạn chế R-40 (rủi ro còn lại):** `image_picker` mở camera hệ thống, plugin buộc ghi JPEG vào cache riêng của app. App đọc vào bộ nhớ rồi **ghi đè 0 toàn bộ tệp và xóa** (`shredFile`). Còn lại: ứng dụng camera hệ thống xử lý bản rõ; bộ nhớ flash (wear-levelling, journal ext4/f2fs) có thể giữ khối cũ; app bị giết đúng lúc giữa chụp và xóa thì tệp còn trong cache (lần chụp sau không dọn lại). Package `camera` không giải quyết được: `takePicture()` cũng ghi tệp tạm; chỉ luồng khung hình YUV là thuần bộ nhớ nhưng độ phân giải xem trước không đạt chất lượng minh chứng. Muốn loại hẳn: plugin native CameraX `OnImageCapturedCallback` (ảnh trong bộ nhớ) — cần Android SDK để phát triển.
- **Thông báo đẩy:** FCM chỉ mang `{kind, notification_id}`; nội dung lấy qua pull. Màn hình khóa chỉ thấy "Có thông báo mới". Token không ghi log.
- **Bản đồ ngoại tuyến:** tile nằm trong CSDL SQLCipher (bảng `map_tile`), bị xóa cùng CSDL khi thu hồi. Tile tải trực tuyến để xem không ghi đệm.
- **Bản debug** cho HTTP rõ tới `127.0.0.1`/`localhost` (network security config chỉ ở `src/debug`) để kiểm thử tích hợp gọi máy chủ giả; bản release vẫn `usesCleartextTraffic=false`.
- **GPS là dữ liệu cá nhân:**
  - Tọa độ ảnh chỉ nằm trong metadata phong bì. Bản ghi `evidence` chỉ có `gps_status`.
  - Không ghi log tọa độ.
- **Android:**
  - `allowBackup=false`, `usesCleartextTraffic=false`.
  - OIDC PKCE qua AppAuth (client `agrigreen-field-app`, redirect `vn.agrigreen.field:/oauth2redirect`, `prompt=login`).

## Dependency và giấy phép

Mọi gói thuộc `deploy/policies/allowed-licenses.txt`. Đã quét toàn bộ `pubspec.lock` (28/09/2026): 102 BSD, 25 MIT, 9 Apache-2.0, 4 MPL-2.0. Bốn gói MPL-2.0 (`dbus`, `nm`, `geoclue`, `gsettings`) là gói bắc cầu, chỉ dùng trên Linux. Gói mới đợt này: `firebase_*`/`_flutterfire_internals`, `flutter_local_notifications*`, `timezone`, `process`, `sync_http`, `flutter_driver`, `integration_test`, `fuchsia_remote_debug_protocol` (BSD), `image` (MIT), `webdriver` (Apache-2.0).

Lưu ý RB-4: thư viện native `com.google.firebase:firebase-messaging` là Apache-2.0 nhưng kéo theo Google Play services (giấy phép Android SDK của Google, không nằm trong danh sách cho phép). Kênh đẩy chỉ bật khi có cấu hình `AG_FCM_*`; cần rà soát pháp lý trước khi phát hành (ADR-0015 đã chấp nhận FCM cho thông điệp không định danh).

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
| firebase_core 4.15, firebase_messaging 16.7 | BSD-3-Clause | FCM chỉ dữ liệu (ADR-0015) |
| flutter_local_notifications 22.3 (+ timezone 0.11) | BSD-3-Clause (timezone: BSD-2-Clause) | thông báo cục bộ |
| image 4.10 | MIT | đóng dấu GPS/thời gian lên ảnh trong bộ nhớ |
| integration_test (dev, SDK) | BSD-3-Clause | kiểm thử trên emulator (kéo theo flutter_driver, webdriver Apache-2.0) |
| url_launcher 6.3 (+ url_launcher_android/ios/…, url_launcher_platform_interface) | BSD-3-Clause | danh bạ trực: mở trình quay số `tel:` / nhắn tin `sms:` (KN US-DIR-02, 29/09/2026) — không cần quyền CALL_PHONE |
| path_provider 2.1, path 1.9 | BSD-3-Clause | thư mục dữ liệu |
| flutter_lints 6 (dev) | BSD-3-Clause | lint |

Dữ liệu bản đồ OSM dùng giấy phép ODbL. Máy chủ `tile.openstreetmap.org` chỉ để dev (chính sách OSM cấm tải hàng loạt — giới hạn gói: ≤ 4 000 tile, ≤ 400 km², zoom 8–18, tổng ≤ 150 MB). Sản xuất: `AG_TILE_URL` trỏ máy chủ tile trong lãnh thổ (ADR-0007).

## Cần Android SDK / CI

Không làm được trên máy không có Android SDK:
- `flutter build apk`, `flutter run`;
- kiểm thử tích hợp trên emulator: SQLCipher thật (xác nhận raw key `x'…'` mở được), Keystore, AppAuth với Keycloak, camera/GPS, `connectivity_plus`.

Job CI `field-app` (mọi PR) và `field-app-emulator` (push lên `main` hoặc chạy tay: `reactivecircus/android-emulator-runner@v2`, API 30, x86_64, `flutter test integration_test`). Chạy trên máy có Android SDK:

```powershell
flutter test integration_test -d emulator-5554
```

Job `field-app` tương đương:

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
    - run: dart format --output=none --set-exit-if-changed lib test integration_test
    - run: flutter analyze --fatal-infos
    - run: flutter test
    - run: flutter build apk --debug                 # cần Android SDK (có sẵn trên ubuntu-latest)
```

## Việc còn lại (TODO)

- **Chụp ảnh không chạm đĩa:** plugin CameraX riêng (xem "Hạn chế R-40").
- **Gói bản đồ theo địa giới xã:** hiện tải theo khung đang xem; nên lấy khung từ `GET /v1/geo/admin-boundaries/{code}`.
- **Đồng bộ nền:** WorkManager khi app đóng (hiện `sync_now` khi app tắt chỉ hiện thông báo chung).
- **Đồng ý ghi ở thiết bị khác:** máy chỉ biết đồng ý ghi trên chính nó; hộ đã đồng ý qua web vẫn phải ghi lại trên máy (bản ghi thêm, vô hại). Cần entity chiều xuống `consent` hoặc cờ trên `household`.
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
8. **Payload `field_report`/`evidence`:** đã theo máy chủ (`ReportPayload`, `mrv.CapturePayload`) — schema và khoảng trống danh mục (không có entity đồng bộ cho danh mục MRV, `catalog_item` chưa có nguồn pull) ghi ở `docs/dev/sync-contract.md`. Báo cáo hiện trường là báo cáo KN FR-01/FR-02, **không** còn "nhật ký canh tác" có GPS (máy chủ không có entity đó — HTX US-LOG-01 thuộc G2). Minh chứng là biểu mẫu theo danh mục đã duyệt, nhiều ảnh; bản ghi chỉ gửi khi mọi ảnh đã tải.
9. **Mã `geo.*` cần xác nhận khi đồng bộ:** Sink pf/geo trả `conflict` cho chồng lấn chưa xác nhận; `geo.overlap_needs_confirm`/`geo.area_deviation_needs_confirm` hiện chỉ thấy ở API trực tuyến. App xử lý cả hai.
10. **Thời hạn khóa idempotency:** máy chủ giữ khóa 48 giờ. Nếu lô gửi lại sau hơn 48 giờ, máy chủ coi là yêu cầu mới. Vẫn an toàn vì chống trùng theo `idem_key`.
