/// Minh chứng MRV `evidence` — **máy chủ là hợp đồng**:
/// `internal/platform/mrv/app/evidence.go` (`CapturePayload`, `Parse`),
/// `internal/platform/mrv/domain/evidence.go` (`ValidateEnvelope`) và
/// `internal/platform/mrv/domain/catalog.go` (`ValidateRecord`, `checkValue`).
///
/// Biểu mẫu dựng từ **phiên bản danh mục đã duyệt** tải qua
/// `GET /v1/mrv/catalog?evidence_type=` (không có entity đồng bộ); phiên bản
/// được **ghim** vào `catalog_version` của bản ghi.
library;

import 'dart:convert';

/// Loại minh chứng G1 (`mrv/port`): máy chủ không có API liệt kê loại.
const mrvEvidenceTypes = {
  'awd_log': 'Nhật ký AWD tại SPOT',
  'field_checklist': 'Kiểm tra thực địa',
};

enum MrvDataType {
  decimal,
  integer,
  boolean,
  enumType,
  date,
  photo,
  geoPoint,
  text,
}

MrvDataType _parseType(String s) => switch (s) {
  'decimal' => MrvDataType.decimal,
  'integer' => MrvDataType.integer,
  'boolean' => MrvDataType.boolean,
  'enum' => MrvDataType.enumType,
  'date' => MrvDataType.date,
  'photo' => MrvDataType.photo,
  'geo_point' => MrvDataType.geoPoint,
  'text' => MrvDataType.text,
  _ => throw FormatException('data_type lạ: $s'),
};

/// Khóa dành riêng: giá trị lấy từ trường riêng của bản ghi, không nằm trong
/// `values` (`domain.reservedColumn`).
const mrvReservedColumn = {
  'plot_ref': 'plot_ref',
  'season': 'season_code',
  'gps': 'gps',
  'photo': 'file_refs',
};

class MrvField {
  const MrvField({
    required this.key,
    required this.labelVi,
    required this.type,
    this.unit,
    this.required = false,
    this.min,
    this.max,
    this.maxScale,
    this.enumValues = const [],
    this.helpVi,
    this.position = 0,
  });

  final String key;
  final String labelVi;
  final MrvDataType type;
  final String? unit;
  final bool required;

  /// Chuỗi decimal (API trả chuỗi): decimal/integer = giá trị; photo = số ảnh;
  /// text = độ dài.
  final String? min;
  final String? max;
  final int? maxScale;
  final List<String> enumValues;
  final String? helpVi;
  final int position;

  bool get reserved => mrvReservedColumn.containsKey(key);

  factory MrvField.fromJson(Map<String, Object?> j) => MrvField(
    key: j['key']! as String,
    labelVi: (j['label_vi'] ?? j['key']).toString(),
    type: _parseType(j['data_type']! as String),
    unit: _nz(j['unit']),
    required: j['required'] == true,
    min: _nz(j['min']),
    max: _nz(j['max']),
    maxScale: (j['max_scale'] as num?)?.toInt(),
    enumValues: [for (final v in (j['enum_values'] as List?) ?? const []) '$v'],
    helpVi: _nz(j['help_vi']),
    position: (j['position'] as num?)?.toInt() ?? 0,
  );

  static String? _nz(Object? v) => v == null || '$v'.isEmpty ? null : '$v';
}

/// Phiên bản danh mục đã duyệt (`MrvCatalogVersion`).
class MrvCatalog {
  const MrvCatalog({
    required this.evidenceType,
    required this.version,
    required this.fields,
    this.provisional = false,
  });

  final String evidenceType;
  final int version;
  final List<MrvField> fields;
  final bool provisional;

  factory MrvCatalog.fromJson(Map<String, Object?> j) {
    final fields = [
      for (final f in (j['fields'] as List?) ?? const [])
        MrvField.fromJson((f as Map).cast<String, Object?>()),
    ]..sort((a, b) => a.position.compareTo(b.position));
    return MrvCatalog(
      evidenceType: j['evidence_type']! as String,
      version: (j['version']! as num).toInt(),
      fields: fields,
      provisional: j['provisional'] == true,
    );
  }

  MrvField? field(String key) {
    for (final f in fields) {
      if (f.key == key) return f;
    }
    return null;
  }

  /// Kiểm như `CatalogVersion.ValidateRecord` + phần `ValidateEnvelope` phụ
  /// thuộc bản ghi. Trả lỗi theo trường (`values.<khóa>`, `plot_ref`,
  /// `season_code`, `gps`, `file_refs`); rỗng = hợp lệ.
  List<MrvFieldError> validate(MrvEvidenceDraft d) {
    final errs = <MrvFieldError>[];
    void add(String f, String r) => errs.add(MrvFieldError(f, r));
    final unknown = <String>[];
    for (final k in d.values.keys) {
      if (mrvReservedColumn.containsKey(k)) {
        add(
          'values.$k',
          'khóa dành riêng — gửi ở trường ${mrvReservedColumn[k]}',
        );
        continue;
      }
      if (field(k) == null) unknown.add(k);
    }
    unknown.sort();
    for (final k in unknown) {
      add(
        'values.$k',
        'không có trong danh mục $evidenceType phiên bản $version',
      );
    }
    final files = {for (final f in d.fileRefs) f.fileRef};
    for (final f in fields) {
      if (f.reserved) {
        final col = mrvReservedColumn[f.key]!;
        final present = switch (f.key) {
          'plot_ref' => d.plotRef != null,
          'season' => d.seasonCode != null,
          'gps' => d.gps != null,
          _ => d.fileRefs.isNotEmpty,
        };
        if (!present) {
          if (f.required) add(col, 'bắt buộc (${f.labelVi})');
          continue;
        }
        if (f.key == 'photo') {
          final r = _count(f, d.fileRefs.length, 'ảnh');
          if (r != null) add(col, r);
        }
        continue;
      }
      final v = d.values[f.key];
      if (v == null) {
        if (f.required) add('values.${f.key}', 'bắt buộc (${f.labelVi})');
        continue;
      }
      final r = _checkValue(f, v, files);
      if (r != null) add('values.${f.key}', r);
    }
    return errs;
  }
}

class MrvFieldError {
  const MrvFieldError(this.field, this.reason);
  final String field;
  final String reason;
  @override
  String toString() => '$field: $reason';
}

// Regex của catalog.go.
final _decimalRe = RegExp(r'^-?[0-9]{1,16}(\.[0-9]{1,8})?$');
final _integerRe = RegExp(r'^-?[0-9]{1,15}$');
final _unitCodeRe = RegExp(r'^[0-9A-Z][0-9A-Z.-]{0,39}$');
final _evidenceTypeRe = RegExp(r'^[a-z][a-z0-9_]{2,39}$');
final _sha256Re = RegExp(r'^[0-9a-f]{64}$');
final _dateRe = RegExp(r'^[0-9]{4}-[0-9]{2}-[0-9]{2}$');

/// So sánh hai chuỗi decimal chính xác (không qua double).
int compareDecimal(String a, String b) {
  (bool, String, String) split(String s) {
    final neg = s.startsWith('-');
    final t = neg ? s.substring(1) : s;
    final i = t.indexOf('.');
    var ip = (i < 0 ? t : t.substring(0, i)).replaceFirst(RegExp(r'^0+'), '');
    final fp = (i < 0 ? '' : t.substring(i + 1)).replaceFirst(
      RegExp(r'0+$'),
      '',
    );
    if (ip.isEmpty) ip = '0';
    return (neg && (ip != '0' || fp.isNotEmpty), ip, fp);
  }

  final (na, ia, fa) = split(a);
  final (nb, ib, fb) = split(b);
  if (na != nb) return na ? -1 : 1;
  int mag() {
    if (ia.length != ib.length) return ia.length.compareTo(ib.length);
    final c = ia.compareTo(ib);
    if (c != 0) return c;
    final w = fa.length > fb.length ? fa.length : fb.length;
    return fa.padRight(w, '0').compareTo(fb.padRight(w, '0'));
  }

  final m = mag();
  return na ? -m : m;
}

String? _range(MrvField f, String v) {
  final unit = f.unit == null ? '' : ' ${f.unit}';
  if (f.min != null && compareDecimal(v, f.min!) < 0) {
    return 'phải ≥ ${f.min}$unit';
  }
  if (f.max != null && compareDecimal(v, f.max!) > 0) {
    return 'phải ≤ ${f.max}$unit';
  }
  return null;
}

String? _count(MrvField f, int n, String what) {
  if (f.min != null && compareDecimal('$n', f.min!) < 0) {
    return 'cần tối thiểu ${f.min} $what';
  }
  if (f.max != null && compareDecimal('$n', f.max!) > 0) {
    return 'tối đa ${f.max} $what';
  }
  return null;
}

String? _checkValue(MrvField f, Object v, Set<String> files) {
  switch (f.type) {
    case MrvDataType.decimal:
      if (v is! String) {
        return 'số thập phân phải gửi dạng chuỗi, vd "12.5" (không dùng số thực — R-10)';
      }
      if (!_decimalRe.hasMatch(v)) {
        return 'không phải số thập phân hợp lệ (vd "-3.25")';
      }
      final dot = v.indexOf('.');
      final scale = dot < 0 ? 0 : v.length - dot - 1;
      if (f.maxScale != null && scale > f.maxScale!) {
        return 'tối đa ${f.maxScale} chữ số sau dấu phẩy';
      }
      return _range(f, v);
    case MrvDataType.integer:
      final s = v is int ? '$v' : (v is String ? v : '');
      if (!_integerRe.hasMatch(s)) return 'phải là số nguyên';
      return _range(f, s);
    case MrvDataType.boolean:
      return v is bool ? null : 'phải là true hoặc false';
    case MrvDataType.enumType:
      return v is String && f.enumValues.contains(v)
          ? null
          : 'phải là một trong: ${f.enumValues.join(', ')}';
    case MrvDataType.date:
      return v is String && _dateRe.hasMatch(v) && DateTime.tryParse(v) != null
          ? null
          : 'phải là ngày YYYY-MM-DD';
    case MrvDataType.text:
      if (v is! String) return 'phải là chuỗi';
      final n = v.runes.length;
      if (n > 2000) return 'tối đa 2000 ký tự';
      return _count(f, n, 'ký tự');
    case MrvDataType.photo:
      if (v is! List) return 'phải là danh sách file_ref';
      final seen = <String>{};
      for (final r in v) {
        if (r is! String || !files.contains(r) || !seen.add(r)) {
          return 'file_ref phải có trong file_refs của bản ghi và không trùng: $r';
        }
      }
      if (v.isEmpty && f.required) return 'bắt buộc (${f.labelVi})';
      return _count(f, v.length, 'ảnh');
    case MrvDataType.geoPoint:
      return 'geo_point chỉ dùng cho khóa gps';
  }
}

/// Tham chiếu tệp: `file_ref` + SHA-256 **ciphertext** (`FileRefWire`).
class MrvFileRef {
  const MrvFileRef(this.fileRef, this.sha256);
  final String fileRef;
  final String sha256;
  Map<String, Object?> toJson() => {'file_ref': fileRef, 'sha256': sha256};
}

/// Toạ độ dạng chuỗi decimal (`GPSWire`) — dữ liệu cá nhân khi gắn với hộ.
class MrvGps {
  const MrvGps(this.lat, this.lon, [this.accuracyM]);
  final String lat;
  final String lon;
  final String? accuracyM;

  factory MrvGps.fromDegrees(double lat, double lon, [double? accuracyM]) =>
      MrvGps(
        _fixed(lat, 7),
        _fixed(lon, 7),
        accuracyM == null ? null : _fixed(accuracyM, 2),
      );

  static String _fixed(double v, int digits) =>
      normalizeFixed(v.toStringAsFixed(digits));

  Map<String, Object?> toJson() => {
    'lat': lat,
    'lon': lon,
    'accuracy_m': ?accuracyM,
  };
}

/// "10.0300000" → "10.03"; "5.00" → "5".
String normalizeFixed(String s) {
  if (!s.contains('.')) return s;
  var t = s.replaceFirst(RegExp(r'0+$'), '');
  if (t.endsWith('.')) t = t.substring(0, t.length - 1);
  return t == '-0' ? '0' : t;
}

/// Bản nháp minh chứng: payload đúng `CapturePayload` (id, event_time,
/// device_id, supersedes_id, correction_reason lấy từ phong bì bản ghi đồng bộ).
class MrvEvidenceDraft {
  const MrvEvidenceDraft({
    required this.evidenceType,
    required this.catalogVersion,
    required this.adminUnitCode,
    this.plotRef,
    this.householdRef,
    this.seasonCode,
    this.values = const {},
    this.fileRefs = const [],
    this.gps,
  });

  final String evidenceType;

  /// Phiên bản danh mục đã dùng để dựng biểu mẫu — ghim vào bản ghi.
  final int catalogVersion;
  final String adminUnitCode;
  final String? plotRef;

  /// Có hộ → máy chủ bắt buộc đồng ý `mrv.evidence` (`mrv.consent_missing`).
  final String? householdRef;
  final String? seasonCode;

  /// Giá trị không dành riêng: decimal = String, integer = int, boolean = bool,
  /// enum/date/text = String, photo = `List<String>` file_ref.
  final Map<String, Object> values;
  final List<MrvFileRef> fileRefs;
  final MrvGps? gps;

  MrvEvidenceDraft copyWith({List<MrvFileRef>? fileRefs}) => MrvEvidenceDraft(
    evidenceType: evidenceType,
    catalogVersion: catalogVersion,
    adminUnitCode: adminUnitCode,
    plotRef: plotRef,
    householdRef: householdRef,
    seasonCode: seasonCode,
    values: values,
    fileRefs: fileRefs ?? this.fileRefs,
    gps: gps,
  );

  /// Lỗi phần phong bì (`ValidateEnvelope`) không phụ thuộc danh mục.
  List<MrvFieldError> validateEnvelope() {
    final errs = <MrvFieldError>[];
    if (!_evidenceTypeRe.hasMatch(evidenceType)) {
      errs.add(const MrvFieldError('evidence_type', '3–40 ký tự a-z, 0-9, _'));
    }
    if (catalogVersion <= 0) {
      errs.add(
        const MrvFieldError('catalog_version', 'phải là số nguyên dương'),
      );
    }
    if (!_unitCodeRe.hasMatch(adminUnitCode)) {
      errs.add(
        const MrvFieldError(
          'admin_unit_code',
          'mã đơn vị hành chính không hợp lệ',
        ),
      );
    }
    final s = seasonCode;
    if (s != null && (s.runes.isEmpty || s.runes.length > 40)) {
      errs.add(const MrvFieldError('season_code', '1–40 ký tự'));
    }
    if (fileRefs.length > 20) {
      errs.add(const MrvFieldError('file_refs', 'tối đa 20 tệp'));
    }
    final seen = <String>{};
    for (final f in fileRefs) {
      if (f.fileRef.isEmpty || !seen.add(f.fileRef)) {
        errs.add(const MrvFieldError('file_refs', 'file_ref rỗng hoặc trùng'));
      }
      if (!_sha256Re.hasMatch(f.sha256)) {
        errs.add(
          const MrvFieldError(
            'file_refs',
            'sha256 phải là 64 ký tự hex thường (băm ciphertext)',
          ),
        );
      }
    }
    final g = gps;
    if (g != null) {
      final lat = double.tryParse(g.lat), lon = double.tryParse(g.lon);
      final acc = g.accuracyM == null ? 0.0 : double.tryParse(g.accuracyM!);
      if (lat == null ||
          lon == null ||
          acc == null ||
          lat.abs() > 90 ||
          lon.abs() > 180 ||
          acc < 0) {
        errs.add(const MrvFieldError('gps', 'toạ độ ngoài phạm vi hợp lệ'));
      }
    }
    return errs;
  }

  Map<String, Object?> toPayload() => {
    'evidence_type': evidenceType,
    'catalog_version': catalogVersion,
    'admin_unit_code': adminUnitCode,
    'plot_ref': ?plotRef,
    'household_ref': ?householdRef,
    'season_code': ?seasonCode,
    'values': {for (final k in (values.keys.toList()..sort())) k: values[k]},
    'file_refs': [for (final f in fileRefs) f.toJson()],
    'gps': ?gps?.toJson(),
  };

  factory MrvEvidenceDraft.fromPayload(Map<String, Object?> p) {
    final g = p['gps'] as Map?;
    return MrvEvidenceDraft(
      evidenceType: p['evidence_type']! as String,
      catalogVersion: (p['catalog_version']! as num).toInt(),
      adminUnitCode: p['admin_unit_code']! as String,
      plotRef: p['plot_ref'] as String?,
      householdRef: p['household_ref'] as String?,
      seasonCode: p['season_code'] as String?,
      values: {
        for (final e in ((p['values'] as Map?) ?? const {}).entries)
          if (e.value != null) '${e.key}': e.value as Object,
      },
      fileRefs: [
        for (final f in (p['file_refs'] as List?) ?? const [])
          MrvFileRef((f as Map)['file_ref']! as String, f['sha256']! as String),
      ],
      gps: g == null
          ? null
          : MrvGps(
              g['lat']! as String,
              g['lon']! as String,
              g['accuracy_m'] as String?,
            ),
    );
  }

  String encode() => jsonEncode(toPayload());
}
