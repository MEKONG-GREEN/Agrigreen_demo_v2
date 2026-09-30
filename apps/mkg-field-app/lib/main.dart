import 'dart:io';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/auth/oidc_client.dart';
import 'package:agrigreen_mobile/data/db/sqlcipher_opener.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import 'application/app_services.dart';
import 'ui/app.dart';
import 'ui/providers.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final env = AppConfig.fromEnvironment(
    oidcClientId: mkgOidcClientId,
    oidcRedirectUrl: mkgOidcRedirect,
  );
  // API là api-internal của vùng mkg (không phải Application API nền tảng).
  final config = AppConfig(
    apiBaseUrl: const String.fromEnvironment(
      'AG_MKG_API_BASE',
      defaultValue: 'https://mkg-internal.dev.agrigreen.vn',
    ),
    oidcIssuer: env.oidcIssuer,
    oidcClientId: mkgOidcClientId,
    oidcRedirectUrl: mkgOidcRedirect,
  );
  final support = await getApplicationSupportDirectory();
  final dataDir = Directory(p.join(support.path, 'agrigreen_mkg_field'));
  await dataDir.create(recursive: true);

  final platform = MkgPlatform(
    config: config,
    store: FlutterSecureStore(),
    clock: const SystemClock(),
    ids: UuidV7Generator(),
    opener: const SqlCipherOpener(),
    dataDir: dataDir,
    apiFactory: (token, deviceId) => mkgHttpApi(
      baseUrl: config.apiBaseUrl,
      token: token,
      deviceId: deviceId,
    ),
    location: const GeolocatorLocationSource(),
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
      child: const MkgFieldApp(),
    ),
  );
}
