import 'dart:io';

import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import 'application/app_services.dart';
import 'core/clock.dart';
import 'core/config.dart';
import 'core/ids.dart';
import 'data/auth/oidc_client.dart';
import 'data/db/sqlcipher_opener.dart';
import 'data/photo/location_source.dart';
import 'data/photo/photo_pipeline.dart';
import 'data/push/firebase_push.dart';
import 'data/push/push_messaging.dart';
import 'data/secure/secure_store.dart';
import 'data/sync/sync_api.dart';
import 'ui/app.dart';
import 'ui/providers.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final config = AppConfig.fromEnvironment();
  // Thư mục riêng của app (không phải bộ nhớ ngoài) — chỉ chứa CSDL SQLCipher
  // và ciphertext ảnh.
  final support = await getApplicationSupportDirectory();
  final dataDir = Directory(p.join(support.path, 'agrigreen'));
  await dataDir.create(recursive: true);

  // Kênh đẩy chỉ bật khi có cấu hình FCM (--dart-define=AG_FCM_*).
  final fcm = firebaseOptionsFrom(config);
  if (fcm != null) {
    FirebaseMessaging.onBackgroundMessage(firebaseBackgroundHandler);
  }

  final platform = AppPlatform(
    config: config,
    store: FlutterSecureStore(),
    clock: const SystemClock(),
    ids: UuidV7Generator(),
    opener: const SqlCipherOpener(),
    dataDir: dataDir,
    apiFactory: (token, deviceId) => HttpSyncApi(
      baseUrl: config.apiBaseUrl,
      accessToken: token,
      deviceId: deviceId,
    ),
    camera: ImagePickerCameraSource(),
    location: const GeolocatorLocationSource(),
    push: fcm == null ? const NoopPushMessaging() : FirebasePushMessaging(fcm),
    notifier: FlutterLocalNotifier(),
  );

  runApp(
    ProviderScope(
      overrides: [
        platformProvider.overrideWithValue(platform),
        oidcProvider.overrideWithValue(AppAuthOidcClient(config)),
      ],
      child: const FieldApp(),
    ),
  );
}
