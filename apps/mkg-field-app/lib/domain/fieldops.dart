/// Quy tắc nghiệp vụ đếm cuộn của App hiện trường MKG — kiểm TRƯỚC khi xếp
/// hàng để tổ gom sửa ngay tại ruộng (máy chủ mkg/fieldops kiểm lại, cùng mã
/// lỗi `fieldops.*` — docs/errors.md). Quy tắc lấy từ prototype mekong-green
/// `src/erp/field/service.ts` (FM-03/04/09) và `internal/mkg/fieldops/domain`:
///
/// - FM-03: công đoạn sau chỉ ghi khi công đoạn trước đã có số;
/// - FM-04: số cuộn mỗi công đoạn phải BẰNG công đoạn trước, hoặc ít hơn kèm
///   lý do chênh ≥ 10 ký tự; nhiều hơn bị chặn (rơm không tự sinh ra giữa hai
///   công đoạn — đếm lại hoặc sửa công đoạn trước bằng bản ghi bù);
/// - FM-09: ở ruộng ĐẾM CUỘN, không cân — bắt buộc số cuộn; chỉ ghi kg rơm rời
///   khi số cuộn = 0.
///
/// Không dùng số thực cho khối lượng: kg rơm rời là chuỗi thập phân tối đa 3
/// chữ số (R-10), so sánh theo gam nguyên.
library;

/// Công đoạn đếm theo thứ tự (port.StageBale/StageGather/StageBoatLoad).
enum Stage {
  bale('bale', 'Cuộn tại thửa'),
  gather('gather', 'Tập kết'),
  boatLoad('boat_load', 'Xếp ghe / xe');

  const Stage(this.code, this.label);
  final String code;
  final String label;

  Stage? get previous => index == 0 ? null : Stage.values[index - 1];
  Stage? get next =>
      index + 1 < Stage.values.length ? Stage.values[index + 1] : null;

  static Stage? tryParse(String? code) {
    for (final s in Stage.values) {
      if (s.code == code) return s;
    }
    return null;
  }
}

/// Trạng thái lệnh gom (fieldops domain).
abstract final class OrderStatus {
  static const planned = 'planned';
  static const approved = 'approved';
  static const assigned = 'assigned';
  static const closed = 'closed';
  static const cancelled = 'cancelled';

  static String label(String s) => switch (s) {
    planned => 'Chờ duyệt',
    approved => 'Đã duyệt',
    assigned => 'Đã giao tổ',
    closed => 'Đã đóng',
    cancelled => 'Đã hủy',
    _ => s,
  };

  /// Chỉ đếm cuộn / mở ca trên lệnh đã duyệt (approved / assigned).
  static bool countable(String s) => s == approved || s == assigned;
}

/// Lệnh gom (entity `collection_order` của pull, hoặc `GET /v1/fieldops/orders`).
class CollectionOrder {
  const CollectionOrder({
    required this.orderId,
    required this.status,
    required this.revision,
    required this.htxRef,
    required this.communeCode,
    required this.windowFrom,
    required this.windowTo,
    required this.plotRefs,
    this.crewRef,
    this.createdBy,
  });

  final String orderId;
  final String status;
  final int revision;
  final String htxRef;
  final String communeCode;
  final String windowFrom;
  final String windowTo;
  final List<String> plotRefs;
  final String? crewRef;

  /// Người lập (chỉ có ở `GET /v1/fieldops/orders`) — R-56: không tự duyệt.
  final String? createdBy;

  factory CollectionOrder.fromJson(Map<String, Object?> j) => CollectionOrder(
    orderId: '${j['order_id']}',
    status: '${j['status'] ?? ''}',
    revision: (j['revision'] as num?)?.toInt() ?? 1,
    htxRef: '${j['htx_ref'] ?? ''}',
    communeCode: '${j['commune_code'] ?? ''}',
    windowFrom: '${j['window_from'] ?? ''}',
    windowTo: '${j['window_to'] ?? ''}',
    plotRefs: [for (final p in (j['plot_refs'] as List?) ?? const []) '$p'],
    crewRef: j['crew_ref'] as String?,
    createdBy: j['created_by'] as String?,
  );

  bool get countable => OrderStatus.countable(status);

  /// Mã ngắn để đọc qua điện thoại (8 ký tự cuối của UUID).
  String get shortCode => shortRef(orderId);

  /// `dd/MM – dd/MM/yyyy`.
  String get windowLabel => '${_vnDate(windowFrom)} – ${_vnDate(windowTo)}';
}

String shortRef(String uuid) {
  final s = uuid.replaceAll('-', '');
  return s.length <= 8
      ? s.toUpperCase()
      : s.substring(s.length - 8).toUpperCase();
}

String _vnDate(String iso) {
  final p = iso.split('-');
  return p.length == 3 ? '${p[2]}/${p[1]}/${p[0]}' : iso;
}

/// Kg dạng chuỗi thập phân tối đa 3 chữ số → gam nguyên (null nếu sai dạng).
/// Nhận cả dấu phẩy thập phân (bàn phím tiếng Việt).
int? parseKgGrams(String? s) {
  if (s == null) return null;
  final t = s.trim().replaceAll(',', '.');
  final m = RegExp(r'^([0-9]{1,9})(?:\.([0-9]{1,3}))?$').firstMatch(t);
  if (m == null) return null;
  final whole = int.parse(m.group(1)!);
  final frac = (m.group(2) ?? '').padRight(3, '0');
  return whole * 1000 + int.parse(frac);
}

/// Gam nguyên → chuỗi kg chuẩn của API (`"12.5"`, `"80"`).
String formatKg(int grams) {
  final whole = grams ~/ 1000;
  final frac = (grams % 1000)
      .toString()
      .padLeft(3, '0')
      .replaceAll(RegExp(r'0+$'), '');
  return frac.isEmpty ? '$whole' : '$whole.$frac';
}

/// Số hiệu lực của một (thửa, công đoạn).
class StageCount {
  const StageCount({
    required this.rolls,
    this.looseKg,
    this.discrepancyReason,
    this.id,
    this.local = false,
    this.pending = false,
  });

  final int rolls;

  /// kg rơm rời (chuỗi thập phân) — chỉ khi rolls = 0.
  final String? looseKg;
  final String? discrepancyReason;

  /// `roll_count_id` trên máy chủ, hoặc id bản ghi cục bộ (UUIDv7 máy sinh).
  final String? id;

  /// Có từ hàng đợi trên máy (chưa có trong ảnh chụp số của máy chủ).
  final bool local;

  /// Chưa được máy chủ nhận.
  final bool pending;

  bool get loose => rolls == 0 && looseKg != null;
  bool get empty => rolls == 0 && looseKg == null;

  String get label {
    if (loose) return '${looseKg!.replaceAll('.', ',')} kg rơm rời';
    return '$rolls cuộn';
  }
}

/// Lỗi kiểm dữ liệu trước khi lưu; [rule] là mã quy tắc (FM-03/04/09) hoặc
/// mã lỗi máy chủ tương ứng.
class CountRuleError {
  const CountRuleError(this.rule, this.field, this.message);
  final String rule;
  final String field;
  final String message;

  @override
  String toString() => '$rule: $message';
}

const reasonMinLength = 10;
const reasonMaxLength = 500;

String? _checkReason(String? s, {required bool required}) {
  final t = (s ?? '').trim();
  if (t.isEmpty) {
    return required ? 'bắt buộc, ít nhất $reasonMinLength ký tự' : null;
  }
  final n = t.runes.length;
  if (n < reasonMinLength) return 'ít nhất $reasonMinLength ký tự';
  if (n > reasonMaxLength) return 'tối đa $reasonMaxLength ký tự';
  return null;
}

/// So [c] với [prev]: -1 ít hơn, 0 bằng, 1 nhiều hơn; null = khác đơn vị
/// (một bên cuộn, một bên rơm rời).
int? compareCounts(StageCount c, StageCount prev) {
  if (c.empty && prev.empty) return 0;
  if (c.empty) return -1;
  if (prev.empty) return 1;
  if (c.loose != prev.loose) return null;
  if (prev.loose) {
    final a = parseKgGrams(c.looseKg)!;
    final b = parseKgGrams(prev.looseKg)!;
    return a.compareTo(b);
  }
  return c.rolls.compareTo(prev.rolls);
}

/// Kiểm một số trước khi xếp hàng.
///
/// [rollsText]/[looseKgText] là chuỗi người dùng nhập. [previous] = số hiệu
/// lực của công đoạn trước (null = chưa có). [correction] = bản ghi bù (cho
/// phép về 0 khi nhập nhầm thửa; bắt buộc [correctionReason] ≥ 10 ký tự và
/// công đoạn sau [next] vẫn phải thỏa FM-04 với giá trị mới).
///
/// Trả danh sách lỗi (rỗng = hợp lệ) và số đã chuẩn hóa.
(List<CountRuleError>, StageCount?) checkCount({
  required Stage stage,
  required String rollsText,
  String? looseKgText,
  String? discrepancyReason,
  StageCount? previous,
  bool correction = false,
  String? correctionReason,
  StageCount? next,
}) {
  final errs = <CountRuleError>[];
  final rt = rollsText.trim();
  final rolls = int.tryParse(rt);
  if (rt.isEmpty || rolls == null) {
    errs.add(
      const CountRuleError(
        'FM-09',
        'rolls',
        'Bắt buộc nhập số cuộn (ở ruộng đếm cuộn, không cân). Rơm rời: nhập 0 cuộn và số kg.',
      ),
    );
    return (errs, null);
  }
  if (rolls < 0 || rolls > 1000000) {
    errs.add(
      const CountRuleError('FM-09', 'rolls', 'Số cuộn từ 0 đến 1.000.000.'),
    );
    return (errs, null);
  }
  final kgRaw = (looseKgText ?? '').trim();
  String? looseKg;
  if (kgRaw.isNotEmpty) {
    if (rolls != 0) {
      errs.add(
        const CountRuleError(
          'FM-09',
          'loose_kg',
          'Chỉ ghi kg rơm rời khi số cuộn = 0 — có cuộn thì chỉ đếm cuộn.',
        ),
      );
    } else {
      final g = parseKgGrams(kgRaw);
      if (g == null || g <= 0) {
        errs.add(
          const CountRuleError(
            'FM-09',
            'loose_kg',
            'Kg rơm rời phải là số > 0, tối đa 3 chữ số thập phân.',
          ),
        );
      } else {
        looseKg = formatKg(g);
      }
    }
  }
  if (!correction && rolls == 0 && kgRaw.isEmpty) {
    errs.add(
      const CountRuleError(
        'FM-09',
        'rolls',
        'Số cuộn phải > 0 (hoặc 0 cuộn kèm kg rơm rời).',
      ),
    );
  }
  final reasonErr = _checkReason(discrepancyReason, required: false);
  if (reasonErr != null) {
    errs.add(
      CountRuleError('FM-04', 'discrepancy_reason', 'Lý do chênh: $reasonErr.'),
    );
  }
  if (correction) {
    final cr = _checkReason(correctionReason, required: true);
    if (cr != null) {
      errs.add(CountRuleError('RB-1', 'correction_reason', 'Lý do sửa: $cr.'));
    }
  }
  if (errs.isNotEmpty) return (errs, null);

  final dr = (discrepancyReason ?? '').trim();
  final c = StageCount(
    rolls: rolls,
    looseKg: looseKg,
    discrepancyReason: dr.isEmpty ? null : dr,
  );
  final prevStage = stage.previous;
  if (prevStage != null) {
    if (previous == null) {
      errs.add(
        CountRuleError(
          'FM-03',
          'stage',
          '"${prevStage.label}" chưa có số — ghi công đoạn trước rồi mới ghi "${stage.label}".',
        ),
      );
    } else {
      final d = compareCounts(c, previous);
      if (d == null) {
        errs.add(
          CountRuleError(
            'FM-04',
            'rolls',
            '"${prevStage.label}" ghi ${previous.loose ? 'rơm rời (kg)' : 'số cuộn'} thì "${stage.label}" cũng phải ghi như vậy.',
          ),
        );
      } else if (d > 0) {
        errs.add(
          CountRuleError(
            'FM-04',
            'rolls',
            '${c.label} nhiều hơn "${prevStage.label}" (${previous.label}) — rơm không tự sinh ra giữa hai công đoạn. Đếm lại, hoặc sửa công đoạn trước.',
          ),
        );
      } else if (d < 0 && dr.isEmpty) {
        errs.add(
          CountRuleError(
            'FM-04',
            'discrepancy_reason',
            'Ít hơn "${prevStage.label}" (${previous.label}) — bắt buộc ghi lý do chênh (ít nhất $reasonMinLength ký tự).',
          ),
        );
      }
    }
  }
  if (correction && next != null) {
    final d = compareCounts(next, c);
    final nextStage = stage.next!;
    if (d == null || d > 0) {
      errs.add(
        CountRuleError(
          'FM-04',
          'rolls',
          '"${nextStage.label}" đang là ${next.label}, nhiều hơn giá trị sửa — sửa "${nextStage.label}" trước.',
        ),
      );
    } else if (d < 0 && (next.discrepancyReason ?? '').trim().isEmpty) {
      errs.add(
        CountRuleError(
          'FM-04',
          'rolls',
          '"${nextStage.label}" (${next.label}) sẽ ít hơn giá trị sửa mà chưa có lý do chênh — sửa "${nextStage.label}" kèm lý do trước.',
        ),
      );
    }
  }
  return (errs, errs.isEmpty ? c : null);
}

/// Lời dễ hiểu cho mã từ chối của máy chủ (docs/errors.md, mkg/fieldops).
String rejectMessage(String? code) => switch (code) {
  'fieldops.previous_stage_missing' =>
    'Công đoạn trước chưa có số trên máy chủ (FM-03).',
  'fieldops.stage_exceeds_previous' => 'Số cuộn nhiều hơn công đoạn trước (FM-04) — đếm lại hoặc sửa công đoạn trước.',
  'fieldops.discrepancy_reason_required' =>
    'Ít hơn công đoạn trước mà thiếu lý do chênh (FM-04).',
  'fieldops.unit_mismatch' => 'Công đoạn trước đếm cuộn thì công đoạn sau cũng đếm cuộn (và ngược lại với rơm rời).',
  'fieldops.next_stage_conflict' =>
    'Bản sửa làm công đoạn sau sai FM-04 — sửa công đoạn sau trước.',
  'fieldops.stage_already_counted' => 'Công đoạn này của thửa đã có số trên máy chủ — dùng "Sửa số" (bản ghi bù).',
  'fieldops.already_corrected' =>
    'Bản ghi đã được sửa ở nơi khác — làm mới số rồi sửa bản mới nhất.',
  'fieldops.order_not_open' =>
    'Lệnh gom chưa duyệt, đã đóng hoặc đã hủy — không ghi số được.',
  'fieldops.order_not_found' => 'Không còn lệnh gom này.',
  'fieldops.roll_count_not_found' => 'Không có bản ghi đếm cuộn cần sửa.',
  'fieldops.plot_not_in_order' => 'Thửa không thuộc lệnh gom.',
  'fieldops.id_reused' => 'Mã bản ghi trùng — liên hệ quản trị.',
  'fieldops.unknown_entity' => 'Loại bản ghi không được hỗ trợ.',
  'fieldops.device_mismatch' => 'Mã thiết bị không khớp — đăng nhập lại.',
  'fieldops.sink_failed' => 'Máy chủ tạm lỗi — máy sẽ tự gửi lại.',
  'self_approval_forbidden' => 'Bạn là người lập (hoặc sửa gần nhất) lệnh gom này — cần người khác duyệt (R-56).',
  'fieldops.order_transition' =>
    'Lệnh gom không còn ở trạng thái cho phép thao tác này.',
  'forbidden' => 'Tài khoản không có quyền thao tác này.',
  'validation_failed' => 'Dữ liệu không hợp lệ.',
  null => 'Bị từ chối.',
  _ => 'Bị từ chối ($code).',
};
