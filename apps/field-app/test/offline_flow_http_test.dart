import 'dart:io';

import 'package:agrigreen_field/application/app_services.dart';
import 'package:agrigreen_field/application/kn_context_service.dart';
import 'package:agrigreen_mobile/application/push_coordinator.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/push/push_messaging.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter_test/flutter_test.dart';

import '../integration_test/support/mock_api_server.dart';
import '../integration_test/support/scenario.dart';
import 'support/ffi_opener.dart';

/// Cùng kịch bản với `integration_test/app_flow_test.dart`, chạy trên Dart VM
/// qua HTTP thật tới máy chủ giả `dart:io` (CSDL qua FFI thay SQLCipher). Máy
/// chủ giả là profile chỉ-KN: không có route HTX/MRV (KN-STANDALONE-9 #270).
class _NoCamera implements CameraSource {
  @override
  Future<CapturedImage?> capture() async => null;
}

void main() {
  test('luồng KN trên máy chủ không có route HTX/MRV: ngoại tuyến → có mạng → sync_now → push; chỉ pull nguồn KN; thông báo đẩy (HTTP thật)', () async {
    final server = await MockApiServer.start();
    seedServer(server);
    final dir = await Directory.systemTemp.createTemp('ag_it_');
    final fcm = FakePushMessaging();
    final notes = RecordingLocalNotifier();
    final pf = AppPlatform(
      config: AppConfig(apiBaseUrl: server.baseUrl, oidcIssuer: 'http://x'),
      store: MemorySecureStore(),
      clock: const SystemClock(),
      ids: UuidV7Generator(),
      opener: FfiOpener(),
      dataDir: dir,
      apiFactory: (token, deviceId) => HttpSyncApi(
        baseUrl: server.baseUrl,
        accessToken: token,
        deviceId: deviceId,
        timeout: const Duration(seconds: 5),
      ),
      camera: _NoCamera(),
      location: const FixedLocationSource(
        LocationResult(
          GpsStatus.ok,
          GpsFix(lat: 10.045, lon: 105.77, accuracyM: 5),
        ),
      ),
      push: fcm,
      notifier: notes,
    );
    final s = await AppServices.open(pf, accessToken: () async => 'tok');
    final push = PushCoordinator(messaging: fcm, notifier: notes);
    await push.start();
    await push.attach(s.pushHost);
    try {
      expect((await s.knContext.resolve()).stage, KnContextStage.ready);
      await runOfflineKnScenario(
        s: s,
        server: server,
        fcm: fcm,
        notes: notes,
        tick: () => Future<void>.delayed(const Duration(milliseconds: 50)),
      );
      // Tiền cảnh + đã mở khóa + nội dung đã về → hiện tiêu đề thật.
      expect(notes.shown.last.title, 'Lịch công tác tuần đã được duyệt');
      // Mọi lời gọi đồng bộ đều sau đăng ký thiết bị.
      expect(
        server.requests.indexOf('POST /v1/devices'),
        lessThan(server.requests.indexOf('GET /v1/sync/pull')),
      );
    } finally {
      await push.dispose();
      await s.close();
      await server.close();
      await dir.delete(recursive: true);
    }
  });
}
