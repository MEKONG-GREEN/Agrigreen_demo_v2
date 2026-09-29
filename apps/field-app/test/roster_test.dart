import 'package:agrigreen_field/application/roster_service.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/dial/dialer.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter_test/flutter_test.dart';

/// API giả: GET theo path → JSON hoặc lỗi; ghi lại lệnh POST.
class _Api implements SyncApi {
  final Map<String, Object> gets = {};
  final List<String> getCalls = [];
  final List<(String, String, Map<String, Object?>?)> posts = [];
  bool offline = false;

  @override
  Future<Map<String, Object?>> getJson(
    String path, {
    Map<String, String>? query,
  }) async {
    if (offline) throw NetworkException('offline');
    final key = query == null ? path : '$path?unit=${query['unit']}';
    getCalls.add(key);
    final r = gets[key];
    if (r is ApiException) throw r;
    if (r == null) throw ApiException(404, 'not_found');
    return r as Map<String, Object?>;
  }

  @override
  Future<Map<String, Object?>> postAction({
    required String path,
    required String idempotencyKey,
    Map<String, Object?>? body,
  }) async {
    if (offline) throw NetworkException('offline');
    posts.add((path, idempotencyKey, body));
    return {
      'status': body?['status'],
      'set_at': '2026-09-29T01:00:00Z',
      'valid_until': '2026-09-29T13:00:00Z',
    };
  }

  @override
  dynamic noSuchMethod(Invocation i) => super.noSuchMethod(i);
}

class _Dialer implements Dialer {
  final List<Uri> opened = [];
  bool ok = true;
  @override
  Future<bool> open(Uri uri) async {
    opened.add(uri);
    return ok;
  }
}

class _Ids implements IdGenerator {
  int n = 0;
  @override
  String newId() => 'id-${++n}';
}

Map<String, Object?> _entry(String actor, String status, String source) => {
  'actor_ref': actor,
  'unit_code': '31135',
  'province_code': '92',
  'role_title': 'Khuyến nông viên xã',
  'display_name': 'KNV $actor',
  'active': true,
  'updated_at': '2026-09-01T00:00:00Z',
  'duty_status': status,
  'status_source': source,
  'until': status == 'on_duty' ? '2026-09-29T10:00:00Z' : null,
};

void main() {
  late _Api api;
  late _Dialer dialer;
  late RosterService svc;

  setUp(() {
    api = _Api();
    dialer = _Dialer();
    svc = RosterService(api: api, dialer: dialer, ids: _Ids());
  });

  group('danh bạ trực (US-DIR-02)', () {
    test(
      'tải danh bạ theo phạm vi, người đang trực lên đầu, có dự phòng cấp tỉnh',
      () async {
        api.gets['/v1/extension/roster'] = {
          'unit': null,
          'on_duty': 1,
          'items': [
            _entry('a', 'unknown', 'none'),
            _entry('b', 'on_duty', 'shift'),
          ],
          'fallback': [_entry('tinh', 'on_duty', 'manual')],
        };
        final r = await svc.load();
        expect(r.onDuty, 1);
        expect(r.items.map((e) => e.actorRef), ['b', 'a']);
        expect(r.items.first.dutyLabel, 'Đang trực');
        expect(r.items.first.until, DateTime.utc(2026, 9, 29, 10));
        expect(r.items.last.dutyLabel, 'Không xác định');
        expect(r.fallback.single.actorRef, 'tinh');
      },
    );

    test(
      'gọi một chạm: tra số lúc bấm rồi mở tel:; nhắn tin mở sms:',
      () async {
        api.gets['/v1/extension/roster/b/contact'] = {
          'actor_ref': 'b',
          'phone': '0912 345.678',
        };
        await svc.contact('b');
        await svc.contact('b', sms: true);
        expect(dialer.opened.map((u) => u.toString()), [
          'tel:0912345678',
          'sms:0912345678',
        ]);
        expect(api.getCalls, [
          '/v1/extension/roster/b/contact',
          '/v1/extension/roster/b/contact',
        ]);
      },
    );

    test('chưa khai số, ngoài phạm vi, mất mạng, máy không có ứng dụng gọi → thông báo rõ', () async {
      api.gets['/v1/extension/roster/x/contact'] = ApiException(
        404,
        'extension.contact_unavailable',
      );
      api.gets['/v1/extension/roster/y/contact'] = ApiException(
        404,
        'extension.staff_not_found',
      );
      await expectLater(
        svc.contact('x'),
        throwsA(
          isA<RosterException>().having(
            (e) => e.message,
            'message',
            contains('chưa khai số'),
          ),
        ),
      );
      await expectLater(svc.contact('y'), throwsA(isA<RosterException>()));
      api.gets['/v1/extension/roster/b/contact'] = {'phone': '0912345678'};
      dialer.ok = false;
      await expectLater(
        svc.contact('b'),
        throwsA(
          isA<RosterException>().having(
            (e) => e.message,
            'message',
            contains('không mở được'),
          ),
        ),
      );
      api.offline = true;
      await expectLater(
        svc.load(),
        throwsA(
          isA<RosterException>().having(
            (e) => e.message,
            'message',
            contains('Cần có mạng'),
          ),
        ),
      );
      expect(dialer.opened, hasLength(1));
    });

    test('dialUri giữ dấu +, bỏ ký tự phân cách', () {
      expect(
        RosterService.dialUri('tel', '+84 (292) 381-2345').toString(),
        'tel:+842923812345',
      );
    });
  });

  test('tự khai trạng thái trực (US-DIR-01): POST /roster/status có Idempotency-Key, trả hạn hiệu lực', () async {
    final until = await svc.setStatus(onDuty: false);
    expect(until, DateTime.utc(2026, 9, 29, 13));
    expect(api.posts.single.$1, '/v1/extension/roster/status');
    expect(api.posts.single.$2, 'duty:id-1');
    expect(api.posts.single.$3, {'status': 'off_duty'});
  });
}
