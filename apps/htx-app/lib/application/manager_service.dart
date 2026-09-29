import 'dart:convert';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/dial/dialer.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

import '../domain/cropcycle.dart';
import 'crop_service.dart';
import 'profile_service.dart';

/// Nông hộ của HTX (nhãn đã che — `GET /v1/cooperatives/{ref}/households`).
class HouseholdItem {
  const HouseholdItem({
    required this.ref,
    required this.label,
    this.code,
    this.plots = 0,
  });
  final String ref;
  final String label;
  final String? code;
  final int plots;
}

/// Máy không gọi điện được (HTX US-HH-03 AC-2).
const msgCannotCall = 'Không thể khởi tạo cuộc gọi trên thiết bị này.';

class ManagerException implements Exception {
  ManagerException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Kết quả mở vụ hàng loạt (HTX US-SEASON-02).
class BulkOpenResult {
  const BulkOpenResult({
    required this.opened,
    required this.skipped,
    required this.offline,
  });
  final int opened;

  /// Mã thửa / plot_ref → lý do bỏ qua.
  final Map<String, String> skipped;

  /// true = ngoại tuyến: đã xếp hàng từng thửa, máy chủ kiểm khi đồng bộ.
  final bool offline;
}

/// Số liệu dashboard (HTX US-DASH-01) — đọc từ API, đệm bản gần nhất.
class DashboardData {
  const DashboardData({
    required this.totals,
    required this.byVariety,
    required this.fetchedAt,
    this.stale = false,
  });
  final Map<String, Object?> totals;
  final List<Map<String, Object?>> byVariety;
  final DateTime fetchedAt;

  /// Bản đệm (mất mạng).
  final bool stale;
}

/// Một nhật ký trong hàng chờ duyệt, kèm lệnh duyệt đã xếp hàng (nếu có).
class ReviewItem {
  const ReviewItem({required this.log, this.decision, this.failed});
  final FarmLogView log;

  /// approved | needs_supplement — lệnh đã bấm trên máy (chờ gửi / đã gửi).
  final String? decision;

  /// Mã lỗi khi máy chủ từ chối lệnh duyệt (vd tự duyệt nhật ký của mình).
  final String? failed;
}

/// Bộ chức năng di động của quản lý HTX: hộ, mở vụ hộ / hàng loạt, duyệt
/// nhật ký, dashboard mini. Duyệt là lệnh POST idempotent xếp hàng (làm được
/// khi ngoại tuyến); người ghi nhật ký không tự duyệt được (máy chủ chặn).
class ManagerService {
  ManagerService({
    required SyncApi api,
    required LocalDb db,
    required ActionQueue actions,
    required CropService crop,
    required ProfileService profile,
    required Clock clock,
    required IdGenerator ids,
    Dialer dialer = const UrlLauncherDialer(),
  }) : _dialer = dialer,
       _api = api,
       _db = db,
       _actions = actions,
       _crop = crop,
       _profile = profile,
       _clock = clock,
       _ids = ids;

  final Dialer _dialer;
  final SyncApi _api;
  final LocalDb _db;
  final ActionQueue _actions;
  final CropService _crop;
  final ProfileService _profile;
  final Clock _clock;
  final IdGenerator _ids;

  static const _hhKey = 'htx.coop_households';
  static const _dashKey = 'htx.dashboard';

  /// Nông hộ của HTX: tải khi có mạng (đệm), ngoại tuyến dùng bản đệm; chưa
  /// có đệm → suy từ thửa đã đồng bộ.
  Future<List<HouseholdItem>> households({bool refresh = true}) async {
    final plots = await _crop.plots();
    final counts = <String, int>{};
    for (final p in plots) {
      if (p.householdRef != null) {
        counts[p.householdRef!] = (counts[p.householdRef!] ?? 0) + 1;
      }
    }
    final coop = await _profile.cooperativeRef();
    if (refresh && coop != null) {
      try {
        final items = <Map<String, Object?>>[];
        String? cursor;
        for (var page = 0; page < 20; page++) {
          final j = await _api.getJson(
            '/v1/cooperatives/$coop/households',
            query: {'limit': '200', 'cursor': ?cursor},
          );
          for (final e in (j['items'] as List?) ?? const []) {
            if (e is Map) {
              items.add({
                'ref': e['ref'] ?? e['household_ref'],
                'label': e['label'],
                'code': e['code'],
              });
            }
          }
          cursor = j['next_cursor'] as String?;
          if (cursor == null || cursor.isEmpty) break;
        }
        await _db.setMeta(_hhKey, jsonEncode(items));
      } on NetworkException {
        // dùng bản đệm
      } on ApiException catch (e) {
        if (e is DeviceRevokedException) rethrow;
      }
    }
    final cached = await _db.getMeta(_hhKey);
    final out = <HouseholdItem>[];
    if (cached != null) {
      for (final e in jsonDecode(cached) as List) {
        final m = (e as Map).cast<String, Object?>();
        final ref = '${m['ref']}';
        out.add(
          HouseholdItem(
            ref: ref,
            label: '${m['label'] ?? 'Hộ ${ref.substring(0, 8)}'}',
            code: m['code'] as String?,
            plots: counts[ref] ?? 0,
          ),
        );
      }
    }
    for (final e in counts.entries) {
      if (out.every((h) => h.ref != e.key)) {
        out.add(
          HouseholdItem(
            ref: e.key,
            label: 'Hộ ${e.key.substring(0, 8)}',
            plots: e.value,
          ),
        );
      }
    }
    return out;
  }

  /// false sau khi máy chủ trả 404 `not_found` cho route liên hệ nông hộ
  /// (bản API chưa có `GET /v1/households/{ref}/contact`) → ẩn nút gọi.
  bool get householdCallAvailable => _callAvailable;
  bool _callAvailable = true;

  /// SĐT của nông hộ, đọc từ vault qua API **lúc bấm gọi** (`Cache-Control:
  /// no-store`), không lưu trên máy, không ghi log (R-04, R-14).
  Future<String> householdPhone(String householdRef) async {
    final Map<String, Object?> j;
    try {
      j = await _api.getJson('/v1/households/$householdRef/contact');
    } on NetworkException {
      throw ManagerException('Cần có mạng để lấy số điện thoại nông hộ.');
    } on DeviceRevokedException {
      rethrow;
    } on ApiException catch (e) {
      if (e.status == 404 && e.code == 'not_found') {
        _callAvailable = false;
        throw ManagerException(
          'Máy chủ chưa hỗ trợ gọi nhanh nông hộ. Hãy cập nhật hệ thống.',
        );
      }
      throw ManagerException(switch (e.code) {
        // Cùng quy ước với accounts.contact_unavailable (danh bạ cán bộ).
        final c when c.endsWith('.contact_unavailable') =>
          'Nông hộ chưa có số điện thoại hợp lệ.',
        _ when e.status == 404 => 'Không tìm thấy nông hộ.',
        _ when e.status == 403 => 'Nông hộ không thuộc HTX bạn quản lý.',
        _ => 'Không lấy được số điện thoại (${e.code}).',
      });
    }
    final phone = j['phone'];
    if (phone is! String || phone.replaceAll(RegExp(r'[^\d]'), '').length < 9) {
      throw ManagerException('Nông hộ chưa có số điện thoại hợp lệ.');
    }
    return phone.trim();
  }

  /// Gọi nhanh nông hộ (HTX US-HH-03 AC-1): lấy SĐT rồi mở trình quay số.
  /// Máy không gọi được → [msgCannotCall] (AC-2), không treo màn hình.
  Future<void> callHousehold(String householdRef) async {
    final phone = await householdPhone(householdRef);
    bool ok;
    try {
      ok = await _dialer.open(
        Uri(scheme: 'tel', path: phone.replaceAll(RegExp(r'[^\d+]'), '')),
      );
    } catch (_) {
      ok = false;
    }
    if (!ok) throw ManagerException(msgCannotCall);
  }

  Future<List<PlotItem>> plotsOf(String householdRef) async => [
    for (final p in await _crop.plots())
      if (p.householdRef == householdRef) p,
  ];

  /// Mở vụ hàng loạt (HTX US-SEASON-02): có mạng → `POST
  /// /v1/cropcycle/seasons/bulk-open` (một lần xác nhận, thửa lỗi bị bỏ qua
  /// kèm lý do); mất mạng → xếp hàng `season_open` từng thửa.
  Future<BulkOpenResult> bulkOpen({
    required List<PlotItem> plots,
    required String seasonCode,
    required int seasonYear,
    required String varietyCode,
    required DateTime sowingDate,
  }) async {
    if (plots.isEmpty) {
      throw ManagerException('Vui lòng chọn ít nhất 1 lô ruộng để mở vụ.');
    }
    final err = checkSowingDate(sowingDate, _crop.today());
    if (err != null) throw ManagerException(err);
    try {
      final j = await _api.postAction(
        path: '/v1/cropcycle/seasons/bulk-open',
        idempotencyKey: 'bulk:${_ids.newId()}',
        body: {
          'plot_refs': [for (final p in plots) p.ref],
          'season_code': seasonCode,
          'season_year': seasonYear,
          'variety_code': varietyCode,
          'sowing_date': formatDay(sowingDate),
        },
      );
      final codes = {for (final p in plots) p.ref: p.code};
      final skipped = <String, String>{};
      for (final it in (j['items'] as List?) ?? const []) {
        if (it is Map && it['status'] == 'skipped') {
          final ref = '${it['plot_ref']}';
          skipped[codes[ref] ?? ref] = '${it['reason'] ?? ''}';
        }
      }
      return BulkOpenResult(
        opened: (j['opened'] as num?)?.toInt() ?? plots.length - skipped.length,
        skipped: skipped,
        offline: false,
      );
    } on NetworkException {
      final (opened, skipped) = await _crop.openSeasonsOffline(
        plots: plots,
        seasonCode: seasonCode,
        seasonYear: seasonYear,
        varietyCode: varietyCode,
        sowingDate: sowingDate,
      );
      return BulkOpenResult(opened: opened, skipped: skipped, offline: true);
    } on ApiException catch (e) {
      throw ManagerException(switch (e.code) {
        'cropcycle.out_of_scope' || 'forbidden' =>
          'Nông hộ hoặc lô ruộng này không thuộc phạm vi quản lý của bạn.',
        'cropcycle.sowing_date_out_of_window' => msgSowingWindow,
        _ => 'Không mở vụ được (${e.code}).',
      });
    } on CropError catch (e) {
      throw ManagerException(e.message);
    }
  }

  /// Hàng chờ duyệt (HTX US-LOG-05): nhật ký `pending` đã đồng bộ về máy.
  Future<List<ReviewItem>> reviewQueue() async {
    final latest = <String, ApiAction>{};
    for (final a in await _actions.ofKind(ApiActionKind.farmLogReview)) {
      if (a.target != null) latest[a.target!] = a;
    }
    final out = <ReviewItem>[];
    for (final l in await _crop.logs()) {
      final v = l.view;
      final a = latest[v.id];
      if (v.status != 'pending' && a == null) continue;
      if (l.local != null) continue; // chính máy này vừa ghi (chưa lên máy chủ)
      if (a != null &&
          a.status == ApiActionStatus.done &&
          v.status != 'pending') {
        continue; // đã duyệt xong, pull đã phản ánh
      }
      out.add(
        ReviewItem(
          log: v,
          decision: a == null ? null : '${a.body?['decision']}',
          failed: a?.status == ApiActionStatus.failed ? a?.lastError : null,
        ),
      );
    }
    return out;
  }

  /// Phê duyệt / yêu cầu bổ sung (ghi chú bắt buộc khi yêu cầu bổ sung).
  Future<void> review(
    FarmLogView log, {
    required bool approve,
    String note = '',
  }) async {
    if (!approve && note.trim().isEmpty) {
      throw ManagerException('Nhập nội dung cần bổ sung để gửi cho nông dân.');
    }
    if (note.length > 1000) {
      throw ManagerException('Ghi chú tối đa 1000 ký tự.');
    }
    await _actions.enqueue(
      ApiActionKind.farmLogReview,
      target: log.id,
      body: {
        'decision': approve ? 'approved' : 'needs_supplement',
        if (note.trim().isNotEmpty) 'note': note.trim(),
      },
    );
  }

  /// Dashboard mini (HTX US-DASH-01 AC-3: máy chủ chỉ trả dữ liệu HTX mình).
  Future<DashboardData?> dashboard({
    String? seasonCode,
    int? seasonYear,
  }) async {
    final key = '$_dashKey:${seasonCode ?? ''}:${seasonYear ?? ''}';
    try {
      final j = await _api.getJson(
        '/v1/cropcycle/dashboard',
        query: {
          'season_code': ?seasonCode,
          if (seasonYear != null) 'season_year': '$seasonYear',
        },
      );
      final now = _clock.now();
      await _db.setMeta(
        key,
        jsonEncode({'at': now.millisecondsSinceEpoch, 'data': j}),
      );
      return _parse(j, now, stale: false);
    } on NetworkException {
      // dùng bản đệm
    } on ApiException catch (e) {
      if (e is DeviceRevokedException || e is UnauthenticatedException) {
        rethrow;
      }
      if (!e.retriable) throw ManagerException('Không có quyền xem dashboard.');
    }
    final c = await _db.getMeta(key);
    if (c == null) return null;
    final m = (jsonDecode(c) as Map).cast<String, Object?>();
    return _parse(
      (m['data']! as Map).cast<String, Object?>(),
      DateTime.fromMillisecondsSinceEpoch(m['at']! as int),
      stale: true,
    );
  }

  static DashboardData _parse(
    Map<String, Object?> j,
    DateTime at, {
    required bool stale,
  }) => DashboardData(
    totals: ((j['totals'] as Map?) ?? const {}).cast<String, Object?>(),
    byVariety: [
      for (final e in (j['by_variety'] as List?) ?? const [])
        if (e is Map) e.cast<String, Object?>(),
    ],
    fetchedAt: at,
    stale: stale,
  );
}
