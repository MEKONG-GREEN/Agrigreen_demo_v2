import 'package:agrigreen_field/application/app_services.dart';
import 'package:agrigreen_field/domain/kn_report.dart';
import 'package:agrigreen_mobile/application/boundary_service.dart';
import 'package:agrigreen_mobile/data/auth/oidc_client.dart';
import 'package:agrigreen_mobile/data/push/push_messaging.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:agrigreen_mobile/domain/geo/boundary.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';

import '../../test/support/kn_fixtures.dart';
import 'mock_api_server.dart';

/// Đăng nhập chế độ kiểm thử: OIDC giả (không có Keycloak), chỉ dùng trong
/// kiểm thử — không có "cửa sau" nào trong mã sản phẩm.
class TestModeOidc implements OidcClient {
  @override
  Future<OidcTokens> signIn() async => OidcTokens(
    accessToken: 'it-access-token',
    refreshToken: 'it-refresh-token',
    accessTokenExpiry: DateTime.now().add(const Duration(hours: 8)),
  );
  @override
  Future<OidcTokens> refresh(String refreshToken) async => signIn();
}

const plotRef = '01920000-0000-7000-8000-0000000000b1';

/// Dữ liệu ban đầu của tenant chỉ-KN: một phân công Cấp Xã (Trồng trọt), menu
/// Cấp Xã, và chiều xuống gồm xã, thửa KN, biểu + công việc báo cáo, thông báo.
/// Kèm bản ghi của nguồn NGOÀI KN (`plot` của HTX, `task`, `directive` cũ) để
/// chứng minh máy không bao giờ kéo chúng về.
void seedServer(MockApiServer s) {
  s
    ..assignments = [assignment('a-1')]
    ..menu = menuJson()
    ..addChange('admin_unit', xaA, {
      'code': xaA,
      'name': 'Xã A',
      'level': 'commune',
    })
    ..addChange('kn_plot', plotRef, {
      'ref': plotRef,
      'code': 'T-001',
      'name': 'Thửa bờ kênh',
      'commune_code': xaA,
      'cooperative_ref': null,
      'status': 'active',
    })
    ..addChange(KnEntity.formLayout, ver1, layoutData())
    ..addChange(KnEntity.reportTask, taskA2, taskData(id: taskA2))
    ..addChange('notification', 'n-001', {
      'id': 'n-001',
      'kind': 'report_task_due',
      'severity': 'normal',
      'title': 'Sắp đến hạn nộp báo cáo tuần',
      'body': 'Tiến độ xuống giống lúa — Xã A',
      'deep_link': '/notifications',
      'subject_ref': null,
      'created_at': DateTime.now().toUtc().toIso8601String(),
      'read_at': null,
    })
    ..addChange('plot', 'htx-plot-1', {'code': 'HTX-01'})
    ..addChange('task', 'task-1', {'task_id': 'task-1'})
    ..addChange('directive', 'd-1', {'directive_id': 'd-1'});
}

/// Chờ tới khi [cond] đúng (tối đa [timeout]); [tick] cho khung hình chạy.
Future<void> waitUntil(
  Future<bool> Function() cond, {
  required Future<void> Function() tick,
  Duration timeout = const Duration(seconds: 30),
  String? reason,
}) async {
  final end = DateTime.now().add(timeout);
  while (!await cond()) {
    if (DateTime.now().isAfter(end)) {
      fail('Hết thời gian chờ: ${reason ?? ''}');
    }
    await tick();
  }
}

List<GeoPoint> _square(double lat, double lon) => [
  GeoPoint(lat, lon),
  GeoPoint(lat, lon + 0.001),
  GeoPoint(lat + 0.001, lon + 0.001),
  GeoPoint(lat + 0.001, lon),
];

/// Kịch bản KN dùng chung cho kiểm thử tích hợp (emulator) và kiểm thử HTTP
/// thật trên Dart VM, trên máy chủ giả KHÔNG có route HTX/MRV: đăng ký thiết
/// bị → pull chỉ nguồn KN → mất mạng: vẽ thửa (FN-XA-03.05) + gửi báo cáo tuần
/// (FN-XA-03.02) vào hàng đợi → có mạng lại + `sync_now` → push accepted →
/// thông báo đẩy. Ngữ cảnh phiên KN phải đã chọn xong trước khi gọi.
Future<void> runOfflineKnScenario({
  required AppServices s,
  required MockApiServer server,
  required FakePushMessaging fcm,
  required RecordingLocalNotifier notes,
  required Future<void> Function() tick,
}) async {
  // 1. Đăng ký thiết bị + gắn token đẩy + pull ban đầu.
  await s.sync.syncOnce(force: true);
  await waitUntil(
    () async => server.pushTokens[s.deviceId] == fcm.initialToken,
    tick: tick,
    reason: 'đăng ký thiết bị và token đẩy',
  );
  expect(server.registeredDevices, contains(s.deviceId));
  await waitUntil(
    () async => await s.refs.get('kn_plot', plotRef) != null,
    tick: tick,
    reason: 'pull thửa KN',
  );
  expect(await s.knReports.task(taskA2), isNotNull);
  expect(await s.inbox.unreadCount(), 1);
  // Nguồn ngoài KN không về máy.
  for (final e in ['plot', 'task', 'directive']) {
    expect(await s.refs.count(e), 0, reason: e);
  }

  // 2. Mất mạng: vẽ thửa mới + gửi báo cáo tuần, lưu vào hàng đợi.
  server.offline = true;
  final rec = await s.boundaries.save(
    points: _square(10.0450, 105.7700),
    method: BoundaryMethod.map,
    communeCode: xaA,
    declaredAreaHa: 1.2,
    acknowledgeOverlap: true,
  );
  final entry = (await s.knReports.entry((await s.knReports.task(taskA2))!))!;
  final report = await s.knReports.queue(entry, {
    knCellKey(xaA, 'E'): '3.5',
    knCellKey(xaA, 'H'): '7',
  }, submit: true);
  await s.sync.syncOnce(force: true);
  expect((await s.sync.status()).state, SyncState.offline);
  expect((await s.outbox.byId(rec.id))!.status, OutboxStatus.pending);
  expect((await s.outbox.byId(report.id))!.status, OutboxStatus.pending);
  expect(server.ledger, isEmpty);

  // 3. Có mạng lại; máy chủ gửi {kind: sync_now} → đồng bộ ngay.
  server.offline = false;
  fcm.deliver({'kind': 'sync_now'});
  await waitUntil(
    () async =>
        (await s.outbox.byId(rec.id))?.status == OutboxStatus.sent &&
        (await s.outbox.byId(report.id))?.status == OutboxStatus.sent,
    tick: tick,
    reason: 'push ranh giới và báo cáo sau khi có mạng',
  );
  final wire = server.recordsOf('kn_plot_boundary').single;
  final payload = wire['payload']! as Map;
  expect(payload['commune_code'], xaA);
  expect(payload['declared_area_ha'], '1.2000');
  expect(payload['confirm_overlap'], isTrue);
  expect((payload['geometry']! as Map)['type'], 'Polygon');
  final sub = server.recordsOf('report_submission').single['payload']! as Map;
  expect(sub['task_id'], taskA2);
  expect(sub['kind'], 'submitted');

  // 4. Thông báo đẩy {kind, notification_id} → pull nội dung → thông báo cục bộ.
  // Chờ lượt đồng bộ của sync_now xong hẳn: thông điệp đến giữa lượt đang chạy
  // sẽ dùng chung lượt đó (đã qua bước pull) và chỉ hiện thông báo chung.
  await s.sync.syncOnce(force: true);
  server.addChange('notification', 'n-002', {
    'id': 'n-002',
    'kind': 'work_schedule_reviewed',
    'severity': 'normal',
    'title': 'Lịch công tác tuần đã được duyệt',
    'body': 'Tuần 37',
    'deep_link': '/extension/work-schedules/mine',
    'subject_ref': null,
    'created_at': DateTime.now().toUtc().toIso8601String(),
    'read_at': null,
  });
  final before = notes.shown.length;
  fcm.deliver({'kind': 'work_schedule_reviewed', 'notification_id': 'n-002'});
  await waitUntil(
    () async => notes.shown.length > before,
    tick: tick,
    reason: 'thông báo cục bộ',
  );
  expect(notes.shown.last.payload, '/extension/work-schedules/mine');
  expect(await s.inbox.byId('n-002'), isNotNull);

  // 5. Mọi lượt pull chỉ đòi nguồn KN; không lời gọi nào ra ngoài route KN.
  final kn = knPullEntities.toList()..sort();
  expect(server.pullEntities, isNotEmpty);
  for (final only in server.pullEntities) {
    expect(only, isNotNull, reason: 'App KN không bao giờ pull tất cả');
    expect(kn, containsAll(only!));
  }
  expect(server.pullEntities.first, kn);
  expect(server.notFound, isEmpty);
  expect(
    server.requests.where(
      (r) =>
          r.contains('/v1/mrv') ||
          r.contains('/v1/cooperatives') ||
          r.contains('/v1/cropcycle') ||
          r.contains('/v1/extension/directives') ||
          r.contains('/v1/extension/tasks') ||
          r.contains('/v1/extension/roster') ||
          r.contains('/v1/documents'),
    ),
    isEmpty,
  );
}
