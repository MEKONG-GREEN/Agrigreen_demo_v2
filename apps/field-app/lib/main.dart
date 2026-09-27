import 'dart:io';

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

  final platform = AppPlatform(
    config: config,
    store: FlutterSecureStore(),
    clock: const SystemClock(),
    ids: UuidV7Generator(),
    opener: const SqlCipherOpener(),
    dataDir: dataDir,
    apiFactory: (token) =>
        HttpSyncApi(baseUrl: config.apiBaseUrl, accessToken: token),
    camera: ImagePickerCameraSource(),
    location: const GeolocatorLocationSource(),
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
