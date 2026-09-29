import 'dart:io';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/auth/oidc_client.dart';
import 'package:agrigreen_mobile/data/db/sqlcipher_opener.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/push/firebase_push.dart';
import 'package:agrigreen_mobile/data/push/push_messaging.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import 'application/app_services.dart';
import 'ui/app.dart';
import 'ui/providers.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final config = AppConfig.fromEnvironment(
    oidcClientId: htxOidcClientId,
    oidcRedirectUrl: htxOidcRedirect,
  );
  // Thư mục riêng của app (không phải bộ nhớ ngoài) — chỉ chứa CSDL SQLCipher
  // và ciphertext ảnh.
  final support = await getApplicationSupportDirectory();
  final dataDir = Directory(p.join(support.path, 'agrigreen_htx'));
  await dataDir.create(recursive: true);

  // Kênh đẩy chỉ bật khi có cấu hình FCM (--dart-define=AG_FCM_*), như app
  // hiện trường: không dùng google-services.json.
  final fcm = firebaseOptionsFrom(config);
  if (fcm != null) {
    FirebaseMessaging.onBackgroundMessage(firebaseBackgroundHandler);
  }

  final platform = HtxPlatform(
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
    copyText: (t) => Clipboard.setData(ClipboardData(text: t)),
    push: fcm == null ? const NoopPushMessaging() : FirebasePushMessaging(fcm),
    notifier: FlutterLocalNotifier(),
    onlineChanges: () => Connectivity().onConnectivityChanged.map(
      (r) => r.any((c) => c != ConnectivityResult.none),
    ),
  );

  runApp(
    ProviderScope(
      overrides: [
        platformProvider.overrideWithValue(platform),
        oidcProvider.overrideWithValue(AppAuthOidcClient(config)),
      ],
      child: const HtxApp(),
    ),
  );
}
