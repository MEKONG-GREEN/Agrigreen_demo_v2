import '../core/ids.dart';
import '../data/dial/dialer.dart';
import '../data/sync/sync_api.dart';

/// Một dòng danh bạ trực (`GET /v1/extension/roster`, KN US-DIR-02).
class RosterEntry {
  const RosterEntry({
    required this.actorRef,
    required this.displayName,
    required this.roleTitle,
    required this.unitCode,
    required this.dutyStatus,
    required this.statusSource,
    this.until,
  });

  final String actorRef;
  final String displayName;
  final String roleTitle;
  final String unitCode;

  /// on_duty | off_duty | unknown
  final String dutyStatus;

  /// manual | shift | none
  final String statusSource;
  final DateTime? until;

  bool get onDuty => dutyStatus == 'on_duty';

  String get dutyLabel => switch (dutyStatus) {
    'on_duty' => 'Đang trực',
    'off_duty' => 'Tạm nghỉ',
    _ => 'Không xác định',
  };

  static RosterEntry fromJson(Map<String, Object?> j) => RosterEntry(
    actorRef: j['actor_ref']! as String,
    displayName: (j['display_name'] as String?) ?? '',
    roleTitle: (j['role_title'] as String?) ?? '',
    unitCode: (j['unit_code'] as String?) ?? '',
    dutyStatus: (j['duty_status'] as String?) ?? 'unknown',
    statusSource: (j['status_source'] as String?) ?? 'none',
    until: j['until'] is String
        ? DateTime.tryParse(j['until']! as String)
        : null,
  );
}

class Roster {
  const Roster({
    required this.items,
    required this.fallback,
    required this.onDuty,
  });
  final List<RosterEntry> items;

  /// Không ai đang trực ở xã → cán bộ TTKN tỉnh phụ trách.
  final List<RosterEntry> fallback;
  final int onDuty;
}

class RosterException implements Exception {
  RosterException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Danh bạ trực trên app hiện trường: tra cứu (trực tuyến — không đồng bộ
/// xuống máy), gọi/nhắn 1 chạm và tự khai trạng thái trực (US-DIR-01/02).
///
/// Số điện thoại KHÔNG lưu trên máy: đọc `GET /v1/extension/roster/{id}/contact`
/// lúc bấm rồi chuyển thẳng cho ứng dụng gọi/nhắn tin (R-04, R-14).
class RosterService {
  RosterService({
    required SyncApi api,
    required Dialer dialer,
    required IdGenerator ids,
  }) : _api = api,
       _dialer = dialer,
       _ids = ids;

  final SyncApi _api;
  final Dialer _dialer;
  final IdGenerator _ids;

  static List<RosterEntry> _list(Object? v) => [
    if (v is List)
      for (final e in v)
        if (e is Map) RosterEntry.fromJson(e.cast<String, Object?>()),
  ];

  /// [unit] bỏ trống = toàn bộ phạm vi của người dùng.
  Future<Roster> load({String? unit}) async {
    final j = await _guard(
      () => _api.getJson(
        '/v1/extension/roster',
        query: unit == null || unit.isEmpty ? null : {'unit': unit},
      ),
    );
    final items = _list(j['items'])
      ..sort((a, b) => (a.onDuty ? 0 : 1).compareTo(b.onDuty ? 0 : 1));
    return Roster(
      items: items,
      fallback: _list(j['fallback']),
      onDuty: (j['on_duty'] as num?)?.toInt() ?? 0,
    );
  }

  /// URI gọi/nhắn: bỏ khoảng trắng, dấu chấm, gạch; giữ dấu +.
  static Uri dialUri(String scheme, String phone) =>
      Uri(scheme: scheme, path: phone.replaceAll(RegExp(r'[^\d+]'), ''));

  /// Một chạm: tra số rồi mở ứng dụng gọi ([sms] = false) hoặc nhắn tin.
  Future<void> contact(String actorRef, {bool sms = false}) async {
    final j = await _guard(
      () => _api.getJson('/v1/extension/roster/$actorRef/contact'),
    );
    final phone = j['phone'];
    if (phone is! String || phone.trim().isEmpty) {
      throw RosterException('Cán bộ chưa khai số điện thoại liên hệ.');
    }
    final ok = await _dialer.open(dialUri(sms ? 'sms' : 'tel', phone));
    if (!ok) {
      throw RosterException('Máy không mở được ứng dụng gọi điện/nhắn tin.');
    }
  }

  /// Tự khai "Đang trực" / "Tạm nghỉ"; trả thời điểm hết hạn.
  Future<DateTime?> setStatus({required bool onDuty}) async {
    final j = await _guard(
      () => _api.postAction(
        path: '/v1/extension/roster/status',
        idempotencyKey: 'duty:${_ids.newId()}',
        body: {'status': onDuty ? 'on_duty' : 'off_duty'},
      ),
    );
    final v = j['valid_until'];
    return v is String ? DateTime.tryParse(v) : null;
  }

  static Future<Map<String, Object?>> _guard(
    Future<Map<String, Object?>> Function() f,
  ) async {
    try {
      return await f();
    } on NetworkException {
      throw RosterException('Cần có mạng để dùng danh bạ trực.');
    } on ApiException catch (e) {
      throw RosterException(switch (e.code) {
        'extension.contact_unavailable' =>
          'Cán bộ chưa khai số điện thoại liên hệ.',
        'extension.staff_not_found' =>
          'Bạn hoặc cán bộ này chưa có trong danh bạ trực.',
        'extension.out_of_scope' => 'Địa bàn ngoài phạm vi phụ trách.',
        _ => 'Không tải được danh bạ trực (${e.code}).',
      });
    }
  }
}
