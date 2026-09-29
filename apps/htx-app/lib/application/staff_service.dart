import 'package:agrigreen_mobile/data/dial/dialer.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

/// Một cán bộ HTX trong danh bạ (`GET /v1/farmer/coop-staff`) — không có SĐT.
class StaffEntry {
  const StaffEntry({
    required this.userRef,
    required this.displayName,
    required this.title,
    required this.hasPhone,
  });
  final String userRef;
  final String displayName;
  final String title;

  /// false → ẩn nút gọi nhanh (HTX US-SUPPORT-02 AC-2).
  final bool hasPhone;

  static StaffEntry fromJson(Map<String, Object?> j) => StaffEntry(
    userRef: '${j['user_ref']}',
    displayName: '${j['display_name'] ?? ''}',
    title: '${j['title'] ?? ''}',
    hasPhone: j['has_phone'] == true,
  );
}

class StaffException implements Exception {
  StaffException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Danh bạ cán bộ HTX (HTX US-SUPPORT-02): tra cứu theo tên/chức vụ (trực
/// tuyến), gọi nhanh hoặc copy số. **Số điện thoại không lưu trên máy**: đọc
/// `GET /v1/farmer/coop-staff/{id}/contact` lúc bấm rồi chuyển thẳng cho
/// trình quay số / bộ nhớ tạm (R-04, R-14).
class StaffService {
  StaffService({required SyncApi api, required Dialer dialer})
    : _api = api,
      _dialer = dialer;

  final SyncApi _api;
  final Dialer _dialer;

  Future<List<StaffEntry>> search(String q) async {
    final j = await _guard(
      () => _api.getJson(
        '/v1/farmer/coop-staff',
        query: q.trim().isEmpty ? null : {'q': q.trim()},
      ),
    );
    return [
      for (final e in (j['items'] as List?) ?? const [])
        if (e is Map) StaffEntry.fromJson(e.cast<String, Object?>()),
    ];
  }

  /// SĐT hợp lệ của cán bộ, đọc ngay lúc cần (không đệm).
  Future<String> phoneOf(String userRef) async {
    final j = await _guard(
      () => _api.getJson('/v1/farmer/coop-staff/$userRef/contact'),
    );
    final phone = j['phone'];
    if (phone is! String || phone.replaceAll(RegExp(r'[^\d]'), '').length < 9) {
      throw StaffException('Cán bộ chưa có số điện thoại hợp lệ.');
    }
    return phone.trim();
  }

  static Uri telUri(String phone) =>
      Uri(scheme: 'tel', path: phone.replaceAll(RegExp(r'[^\d+]'), ''));

  Future<void> call(String userRef) async {
    final ok = await _dialer.open(telUri(await phoneOf(userRef)));
    if (!ok) throw StaffException('Máy không mở được ứng dụng gọi điện.');
  }

  static Future<Map<String, Object?>> _guard(
    Future<Map<String, Object?>> Function() f,
  ) async {
    try {
      return await f();
    } on NetworkException {
      throw StaffException('Cần có mạng để tra cứu danh bạ cán bộ HTX.');
    } on ApiException catch (e) {
      throw StaffException(switch (e.code) {
        'accounts.contact_unavailable' =>
          'Cán bộ chưa có số điện thoại hợp lệ.',
        'accounts.staff_not_found' => 'Không tìm thấy cán bộ trong danh bạ.',
        _ => 'Không tải được danh bạ (${e.code}).',
      });
    }
  }
}
