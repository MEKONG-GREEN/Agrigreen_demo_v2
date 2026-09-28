import 'dart:convert';
import 'dart:typed_data';

import 'package:agrigreen_field/application/app_services.dart';
import 'package:agrigreen_field/application/boundary_service.dart';
import 'package:agrigreen_field/application/consent_service.dart';
import 'package:agrigreen_field/data/auth/oidc_client.dart';
import 'package:agrigreen_field/data/crypto/envelope.dart';
import 'package:agrigreen_field/data/photo/photo_pipeline.dart';
import 'package:agrigreen_field/domain/mrv.dart';
import 'package:agrigreen_field/data/push/push_messaging.dart';
import 'package:agrigreen_field/data/sync/sync_service.dart';
import 'package:agrigreen_field/domain/geo/boundary.dart';
import 'package:agrigreen_field/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image/image.dart' as img;
import 'package:pinenacl/x25519.dart' as nacl;

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

/// Camera giả: JPEG thật tạo trong bộ nhớ (không chạm đĩa).
class GeneratedJpegCamera implements CameraSource {
  @override
  Future<CapturedImage?> capture() async {
    final im = img.Image(width: 800, height: 600);
    img.fill(im, color: img.ColorRgb8(30, 150, 70));
    return CapturedImage(img.encodeJpg(im, quality: 90), 'image/jpeg');
  }
}

const householdRef = '01920000-0000-7000-8000-0000000000a1';
const plotRef = '01920000-0000-7000-8000-0000000000b1';

/// Dữ liệu chiều xuống ban đầu: xã, thửa, khóa công khai hộ, thông báo.
nacl.PrivateKey seedServer(MockApiServer s) {
  final sk = nacl.PrivateKey.generate();
  s.mrvCatalogs['awd_log'] = awdLogCatalog;
  s.masterdata['season'] = [
    {'catalog': 'season', 'code': 'DX', 'name': 'Đông Xuân', 'attrs': {}},
  ];
  s
    ..addChange('admin_unit', '31117', {
      'code': '31117',
      'name': 'Phường Ninh Kiều',
      'level': 'commune',
    })
    ..addChange('plot', plotRef, {
      'ref': plotRef,
      'code': 'T-001',
      'name': 'Thửa bờ kênh',
      'commune_code': '31117',
      'cooperative_ref': null,
      'household_ref': householdRef,
      'status': 'active',
    })
    ..addChange('pubkey', householdRef, {
      'ref': householdRef,
      'area_code': '31117',
      'public_key': base64.encode(sk.publicKey.asTypedList),
    })
    ..addChange('notification', 'n-001', {
      'id': 'n-001',
      'kind': 'document',
      'severity': 'normal',
      'title': 'Tài liệu mới: phòng trừ rầy nâu',
      'body': 'Đã có hướng dẫn mới',
      'deep_link': '/documents/doc-1',
      'subject_ref': null,
      'created_at': DateTime.now().toUtc().toIso8601String(),
      'read_at': null,
    });
  return sk;
}

/// Danh mục mẫu `awd_log` (mrv/app/seed_g1.json) đã duyệt, dạng API.
const awdLogCatalog = <String, Object?>{
  'id': '01920000-0000-7000-8000-00000000ca01',
  'evidence_type': 'awd_log',
  'version': 1,
  'status': 'approved',
  'provisional': true,
  'source_note': 'Danh mục mẫu G1 — TẠM',
  'fields': [
    {
      'key': 'plot_ref',
      'label_vi': 'Thửa',
      'data_type': 'text',
      'required': true,
      'position': 1,
      'reserved': true,
    },
    {
      'key': 'season',
      'label_vi': 'Vụ',
      'data_type': 'text',
      'required': true,
      'position': 2,
      'reserved': true,
    },
    {
      'key': 'stage',
      'label_vi': 'Giai đoạn',
      'data_type': 'enum',
      'required': true,
      'enum_values': ['ma', 'de_nhanh', 'lam_dong', 'tro_bong', 'chin'],
      'position': 3,
    },
    {
      'key': 'awd_water_level_cm',
      'label_vi': 'Mực nước',
      'data_type': 'decimal',
      'unit': 'cm',
      'required': true,
      'min': '-30',
      'max': '30',
      'max_scale': 1,
      'position': 4,
    },
    {
      'key': 'awd_dry_days',
      'label_vi': 'Số ngày khô',
      'data_type': 'integer',
      'required': true,
      'min': '0',
      'max': '60',
      'position': 5,
    },
    {
      'key': 'photo',
      'label_vi': 'Ảnh',
      'data_type': 'photo',
      'required': true,
      'min': '1',
      'max': '10',
      'position': 6,
      'reserved': true,
    },
    {
      'key': 'gps',
      'label_vi': 'Toạ độ',
      'data_type': 'geo_point',
      'required': true,
      'position': 7,
      'reserved': true,
    },
  ],
};

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

/// Kịch bản dùng chung cho kiểm thử tích hợp (emulator) và kiểm thử HTTP
/// thật trên Dart VM: đăng ký thiết bị → pull → vẽ ranh giới khi mất mạng →
/// có mạng lại + `sync_now` → push accepted → ảnh minh chứng mã hóa tải lên →
/// thông báo đẩy → đánh dấu đã đọc.
Future<void> runOfflineFieldScenario({
  required AppServices s,
  required MockApiServer server,
  required nacl.PrivateKey householdKey,
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
    () async => await s.refs.publicKeyFor(householdRef) != null,
    tick: tick,
    reason: 'pull khóa công khai hộ',
  );
  expect(await s.refs.get('plot', plotRef), isNotNull);
  expect(await s.inbox.unreadCount(), 1);

  // 2. Mất mạng: vẽ thửa mới, lưu vào hàng đợi.
  server.offline = true;
  final rec = await s.boundaries.save(
    points: _square(10.0450, 105.7700),
    method: BoundaryMethod.map,
    communeCode: '31117',
    declaredAreaHa: 1.2,
    acknowledgeOverlap: true,
  );
  await s.sync.syncOnce(force: true);
  expect((await s.sync.status()).state, SyncState.offline);
  expect((await s.outbox.byId(rec.id))!.status, OutboxStatus.pending);
  expect(server.recordsOf('plot_boundary'), isEmpty);

  // 3. Có mạng lại; máy chủ gửi {kind: sync_now} → đồng bộ ngay.
  server.offline = false;
  fcm.deliver({'kind': 'sync_now'});
  await waitUntil(
    () async => (await s.outbox.byId(rec.id))?.status == OutboxStatus.sent,
    tick: tick,
    reason: 'push ranh giới sau khi có mạng',
  );
  final wire = server.recordsOf('plot_boundary').single;
  final payload = wire['payload']! as Map;
  expect(payload['commune_code'], '31117');
  expect(payload['declared_area_ha'], '1.2000');
  expect(payload['confirm_overlap'], isTrue);
  expect((payload['geometry']! as Map)['type'], 'Polygon');

  // 4. Đồng ý + ảnh minh chứng: mã hóa trong bộ nhớ, tải bản mã lên.
  expect(await s.photos.blockReason(householdRef), contains('mrv.evidence'));
  await s.consents.record(
    subjectRef: householdRef,
    status: ConsentStatus.granted,
  );
  // Danh mục trường MRV đã tải qua API sau lượt pull đầu.
  final cat = await s.catalogs.mrvCatalog('awd_log');
  expect(cat?.version, 1);
  final form = s.mrv.newFormId();
  final shot = await s.mrv.capturePhoto(
    formId: form,
    householdRef: householdRef,
  );
  expect(shot, isNotNull);
  await s.mrv.submit(
    formId: form,
    draft: MrvEvidenceDraft(
      evidenceType: 'awd_log',
      catalogVersion: cat!.version,
      adminUnitCode: '31117',
      plotRef: plotRef,
      householdRef: householdRef,
      seasonCode: 'DX',
      values: const {
        'stage': 'de_nhanh',
        'awd_water_level_cm': '-12.5',
        'awd_dry_days': 4,
      },
      gps: await s.mrv.currentGps(),
    ),
  );
  await s.sync.syncOnce(force: true);
  await waitUntil(
    () async => server.recordsOf('evidence').isNotEmpty,
    tick: tick,
    reason: 'bản ghi evidence',
  );
  final f = server.files[shot!.file.id]!;
  expect(f.idempotencyKey, 'file:${shot.file.id}');
  expect(f.meta['subject_ref'], householdRef);
  expect(f.meta['alg'], Envelope.alg);
  // Máy chủ chỉ có bản mã; mở bằng khóa bí mật của hộ ra JPEG đã đóng dấu.
  expect(f.blob.length > 2 && f.blob[0] == 0xFF && f.blob[1] == 0xD8, isFalse);
  final plain = await EnvelopeCipher.openForTest(
    SealedEnvelope(meta: f.meta, ciphertext: f.blob),
    Uint8List.fromList(householdKey.asTypedList),
  );
  final decoded = img.decodeJpg(plain)!;
  expect(decoded.getPixel(790, 598).g, lessThan(120)); // dải watermark
  final ev = server.recordsOf('evidence').single['payload']! as Map;
  expect(ev['evidence_type'], 'awd_log');
  expect(ev['catalog_version'], 1);
  expect(ev['plot_ref'], plotRef);
  expect(ev['household_ref'], householdRef);
  expect(ev['season_code'], 'DX');
  expect((ev['values']! as Map)['awd_water_level_cm'], '-12.5');
  expect((ev['gps']! as Map)['lat'], isA<String>());
  expect(ev['file_refs'], [
    {'file_ref': shot.file.id, 'sha256': shot.file.meta['sha256_ciphertext']},
  ]);
  expect(server.recordsOf('consent').single['payload'], {
    'subject_ref': householdRef,
    'purpose': 'mrv.evidence',
    'text_version': mrvConsentTextVersion,
    'status': 'granted',
    'channel': 'field_app',
  });

  // 5. Thông báo đẩy {kind, notification_id} → pull nội dung → thông báo cục bộ.
  server.addChange('notification', 'n-002', {
    'id': 'n-002',
    'kind': 'directive',
    'severity': 'urgent',
    'title': 'Chỉ đạo khẩn: phun phòng đạo ôn',
    'body': 'Trong 3 ngày tới',
    'deep_link': '/extension/directives/d-9',
    'subject_ref': null,
    'created_at': DateTime.now().toUtc().toIso8601String(),
    'read_at': null,
  });
  final before = notes.shown.length;
  fcm.deliver({'kind': 'directive', 'notification_id': 'n-002'});
  await waitUntil(
    () async => notes.shown.length > before,
    tick: tick,
    reason: 'thông báo cục bộ',
  );
  final n = notes.shown.last;
  expect(n.payload, '/extension/directives/d-9');
  expect(await s.inbox.byId('n-002'), isNotNull);

  // 6. Đánh dấu đã đọc → lệnh xếp hàng → phát lại lên máy chủ.
  await s.inbox.markRead('n-002');
  await s.sync.syncOnce(force: true);
  await waitUntil(
    () async => server.actions.values.contains('/v1/notifications/n-002/read'),
    tick: tick,
    reason: 'đánh dấu đã đọc',
  );
  expect(await s.actions.pendingCount(), 0);
}
