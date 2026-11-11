import 'dart:io';

import 'package:agrigreen_field/application/app_services.dart';
import 'package:agrigreen_field/application/kn_context_service.dart';
import 'package:agrigreen_field/application/kn_menu_service.dart';
import 'package:agrigreen_field/ui/app.dart';
import 'package:agrigreen_field/ui/kn/cnts_tasks_screen.dart';
import 'package:agrigreen_field/ui/kn/kn_cooperatives_screen.dart';
import 'package:agrigreen_field/ui/kn/kn_home_screen.dart';
import 'package:agrigreen_field/ui/kn/kn_machines_screen.dart';
import 'package:agrigreen_field/ui/kn/kn_materials_screen.dart';
import 'package:agrigreen_field/ui/kn/kn_plots_screen.dart';
import 'package:agrigreen_field/ui/kn/kn_tasks_screen.dart';
import 'package:agrigreen_field/ui/providers.dart';
import 'package:agrigreen_field/ui/screens/no_access_screen.dart';
import 'package:agrigreen_field/ui/screens/work_schedule_screen.dart';
import 'package:agrigreen_mobile/application/auth_flow.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/ffi_opener.dart';

/// KN-STANDALONE-9 (#270): bản build App KN chỉ còn màn KN theo BRD 2.0.

class _NoCamera implements CameraSource {
  @override
  Future<CapturedImage?> capture() async => null;
}

class _Fixed extends AppController {
  _Fixed(this.initial);
  final AppState initial;
  int signOuts = 0;
  @override
  AppState build() => initial;
  @override
  Future<void> signOut() async => signOuts++;
}

/// 10 màn cũ ngoài BRD 2.0 và trang chủ cũ (audit 07/11, mục 2.4) — kể cả các
/// màn chi tiết đi kèm.
const _legacyScreens = [
  'HomeScreen',
  'PlotsScreen',
  'BoundaryScreen',
  'EvidenceFormScreen',
  'DirectivesScreen',
  'DirectiveDetailScreen',
  'LibraryScreen',
  'DocumentScreen',
  'RosterScreen',
  'FieldReportScreen',
  'TasksScreen',
  'ConsentScreen',
  'NotificationsScreen',
];

/// Route của hệ ngoài (HTX/MRV) và route cũ ngoài BRD 2.0 mà các màn đó gọi.
const _legacyRoutes = [
  '/v1/mrv',
  '/v1/cooperatives',
  '/v1/kn/cooperatives',
  '/v1/cropcycle',
  '/v1/extension/directives',
  '/v1/extension/tasks',
  '/v1/extension/roster',
  '/v1/extension/field-reports',
  '/v1/extension/support-requests',
  '/v1/documents',
];

void main() {
  test(
    'mã App KN không còn lớp màn cũ và không gọi route HTX/MRV/route cũ',
    () {
      final files = Directory('lib')
          .listSync(recursive: true)
          .whereType<File>()
          .where((f) => f.path.endsWith('.dart'))
          .toList();
      expect(files.length, greaterThan(20), reason: 'chạy từ apps/field-app');
      final screen = RegExp('\\b(${_legacyScreens.join('|')})\\b');
      for (final f in files) {
        final src = f.readAsStringSync();
        expect(
          screen.allMatches(src).map((m) => m.group(0)).toSet(),
          isEmpty,
          reason: f.path,
        );
        for (final r in _legacyRoutes) {
          expect(src.contains("'$r"), isFalse, reason: '${f.path} gọi $r');
        }
      }
    },
  );

  test('menu Cấp Xã chỉ mở màn KN; khóa lạ/khóa của màn cũ không mở gì', () {
    const kn = {
      WorkScheduleScreen,
      KnTasksScreen,
      KnCommuneReportsScreen,
      KnCooperativesScreen,
      KnMachinesScreen,
      KnPlotsScreen,
      KnMaterialsScreen,
      CntsTasksScreen,
    };
    KnMenuEntry e(String key) =>
        KnMenuEntry(key: key, label: key, group: 'Điều hành');
    for (final key in KnMenuKey.known) {
      final w = knScreenFor(e(key));
      if (key == KnMenuKey.congViec) {
        expect(w, isNull); // trang mặc định
      } else {
        expect(kn, contains(w.runtimeType), reason: key);
      }
    }
    for (final key in [
      'xa.chi_dao',
      'xa.thu_vien',
      'xa.danh_ba',
      'xa.nhiem_vu.ho_tro',
      'xa.bao_cao.hien_truong',
      'xa.minh_chung_mrv',
      'xa.thong_bao',
      'directives',
      'tasks',
    ]) {
      expect(knScreenFor(e(key)), isNull, reason: key);
    }
  });

  testWidgets(
    'tài khoản không có ngữ cảnh KN → màn "Không có quyền" (không còn '
    'trang chủ cũ), chỉ đăng xuất được',
    (tester) async {
      final dir = (await tester.runAsync(
        () => Directory.systemTemp.createTemp('ag_kn_noaccess_'),
      ))!;
      final pf = AppPlatform(
        config: const AppConfig(
          apiBaseUrl: 'https://api.test',
          oidcIssuer: 'https://id.test',
        ),
        store: MemorySecureStore(),
        clock: const SystemClock(),
        ids: UuidV7Generator(),
        opener: FfiOpener(),
        dataDir: dir,
        apiFactory: (_, _) => FakeServer(),
        camera: _NoCamera(),
        location: const FixedLocationSource(LocationResult(GpsStatus.denied)),
      );
      final s = (await tester.runAsync(
        () => AppServices.open(pf, accessToken: () async => 'tok'),
      ))!;
      final ctl = _Fixed(
        AppState(
          const AuthView(AuthStage.unlocked),
          services: s,
          kn: const KnContextState(KnContextStage.none),
        ),
      );
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            platformProvider.overrideWithValue(pf),
            oidcProvider.overrideWithValue(FakeOidc()),
            appControllerProvider.overrideWith(() => ctl),
          ],
          child: const FieldApp(),
        ),
      );
      await tester.pump();
      expect(find.byType(NoKnAccessScreen), findsOneWidget);
      expect(find.text('Không có quyền'), findsOneWidget);
      expect(find.byType(KnHomeScreen), findsNothing);
      // Không còn ô nào của trang chủ cũ.
      for (final label in [
        'Thửa ruộng',
        'Nhiệm vụ',
        'Báo cáo hiện trường',
        'Minh chứng MRV',
        'Chỉ đạo',
        'Thư viện · Đã lưu',
        'Danh bạ trực',
        'Hàng đợi đồng bộ',
      ]) {
        expect(find.text(label), findsNothing, reason: label);
      }
      expect(find.byType(IconButton), findsNothing);
      await tester.tap(find.byKey(const ValueKey('no-access-signout')));
      await tester.pump();
      expect(ctl.signOuts, 1);

      await tester.pumpWidget(const SizedBox());
      await tester.runAsync(() async {
        await s.close();
        await dir.delete(recursive: true);
      });
    },
  );

  testWidgets('ngoại tuyến chưa chọn được vai trò → màn "Không có quyền" nêu '
      'lý do', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          appControllerProvider.overrideWith(
            () => _Fixed(
              const AppState(
                AuthView(AuthStage.unlocked),
                kn: KnContextState(
                  KnContextStage.none,
                  message: 'Chưa chọn được vai trò khi ngoại tuyến.',
                  offline: true,
                ),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: NoKnAccessScreen()),
      ),
    );
    expect(
      find.text('Chưa chọn được vai trò khi ngoại tuyến.'),
      findsOneWidget,
    );
  });
}
