import 'dart:convert';
import 'dart:io';

import 'package:agrigreen_mobile/domain/mrv.dart';
import 'package:flutter_test/flutter_test.dart';

/// Payload `evidence` so với fixture JSON rút từ quy tắc
/// kiểm của máy chủ Go (máy chủ là hợp đồng).
Map<String, Object?> _fixture(String name) =>
    (jsonDecode(File('test/fixtures/contract/$name').readAsStringSync()) as Map)
        .cast<String, Object?>();

/// So sánh sau khi mã hóa JSON (phân biệt "5" với 5, null với thiếu khóa).
void expectSameJson(Object? actual, Object? expected) =>
    expect(jsonDecode(jsonEncode(actual)), jsonDecode(jsonEncode(expected)));

MrvCatalog _catalog(String type) => MrvCatalog.fromJson(
  ((_fixture('mrv_catalogs_g1.json')['catalogs']! as List)
          .cast<Map<String, Object?>>())
      .firstWhere((c) => c['evidence_type'] == type),
);

const _plot = '01920000-0000-7000-8000-0000000000b1';
const _hh = '01920000-0000-7000-8000-0000000000a1';
const _f1 = '01920000-0000-7000-8000-0000000000f1';
const _f2 = '01920000-0000-7000-8000-0000000000f2';
const _sha1 =
    '3f0a9a1c5a4b8e2d7c6f1e0d9b8a7c6e5f4d3c2b1a0918273645546372819a0b';
const _sha2 =
    '0000000000000000000000000000000000000000000000000000000000000abc';

MrvEvidenceDraft _awd({
  Map<String, Object>? values,
  List<MrvFileRef>? files,
  MrvGps? gps = const MrvGps('10.0452', '105.7701', '4.5'),
  String? season = 'DX',
  String? plot = _plot,
}) => MrvEvidenceDraft(
  evidenceType: 'awd_log',
  catalogVersion: 1,
  adminUnitCode: '31117',
  plotRef: plot,
  householdRef: _hh,
  seasonCode: season,
  values:
      values ??
      {
        'stage': 'de_nhanh',
        'awd_water_level_cm': '-12.5',
        'awd_dry_days': 5,
        'field_surface': 'am',
        'note': 'Ống quan trắc số 2',
      },
  fileRefs: files ?? const [MrvFileRef(_f1, _sha1)],
  gps: gps,
);

void main() {
  group('evidence = CapturePayload (pf/mrv)', () {
    final fx = _fixture('evidence_payloads.json');

    test('danh mục API → mô hình; khóa dành riêng', () {
      final c = _catalog('awd_log');
      expect(c.version, 1);
      expect(c.provisional, isTrue);
      expect(c.fields.first.key, 'plot_ref');
      expect(c.field('photo')!.reserved, isTrue);
      expect(c.field('awd_water_level_cm')!.maxScale, 1);
      expect(c.field('awd_water_level_cm')!.min, '-30');
    });

    test('awd_log khớp fixture và qua ValidateRecord + ValidateEnvelope', () {
      final d = _awd();
      expect(d.validateEnvelope(), isEmpty);
      expect(_catalog('awd_log').validate(d), isEmpty);
      expectSameJson(d.toPayload(), fx['awd_log']);
      // Đọc lại (lưu biểu mẫu trên máy) không đổi payload.
      expectSameJson(
        MrvEvidenceDraft.fromPayload(d.toPayload()).toPayload(),
        fx['awd_log'],
      );
    });

    test('field_checklist khớp fixture (boolean, date, decimal 2 chữ số lẻ, GPS không sai số)', () {
      final d = MrvEvidenceDraft(
        evidenceType: 'field_checklist',
        catalogVersion: 1,
        adminUnitCode: '31117',
        plotRef: _plot,
        householdRef: _hh,
        seasonCode: 'DX',
        values: const {
          'check_boundary_ok': true,
          'check_interview_done': false,
          'check_water_regime': 'awd',
          'check_estimated_yield_t_ha': '6.75',
          'check_visit_date': '2026-12-01',
        },
        fileRefs: const [MrvFileRef(_f1, _sha1), MrvFileRef(_f2, _sha2)],
        gps: MrvGps.fromDegrees(10.0452, 105.7701),
      );
      expect(_catalog('field_checklist').validate(d), isEmpty);
      expectSameJson(d.toPayload(), fx['field_checklist']);
    });

    test('lỗi như checkValue/checkReserved của máy chủ', () {
      final c = _catalog('awd_log');
      Set<String> errs(MrvEvidenceDraft d) => {
        for (final e in [...d.validateEnvelope(), ...c.validate(d)]) e.field,
      };
      Map<String, Object> vals(Map<String, Object> over) => {
        'stage': 'ma',
        'awd_water_level_cm': '1',
        'awd_dry_days': 1,
        ...over,
      };
      expect(errs(_awd(values: vals({}))), isEmpty);
      // decimal gửi số JSON → cấm (R-10); quá số lẻ; ngoài khoảng.
      expect(errs(_awd(values: vals({'awd_water_level_cm': 12}))), {
        'values.awd_water_level_cm',
      });
      expect(errs(_awd(values: vals({'awd_water_level_cm': '1.25'}))), {
        'values.awd_water_level_cm',
      });
      expect(errs(_awd(values: vals({'awd_water_level_cm': '-30.1'}))), {
        'values.awd_water_level_cm',
      });
      expect(errs(_awd(values: vals({'awd_water_level_cm': '-30'}))), isEmpty);
      expect(errs(_awd(values: vals({'awd_dry_days': '61'}))), {
        'values.awd_dry_days',
      });
      expect(errs(_awd(values: vals({'awd_dry_days': '7'}))), isEmpty);
      expect(errs(_awd(values: vals({'stage': 'lua'}))), {'values.stage'});
      // Thiếu bắt buộc; khóa lạ; khóa dành riêng trong values.
      expect(errs(_awd(values: {'stage': 'ma'})), {
        'values.awd_water_level_cm',
        'values.awd_dry_days',
      });
      expect(errs(_awd(values: vals({'foo': 'x'}))), {'values.foo'});
      expect(errs(_awd(values: vals({'gps': 'x'}))), {'values.gps'});
      // Cột dành riêng bắt buộc.
      expect(errs(_awd(values: vals({}), gps: null)), {'gps'});
      expect(errs(_awd(values: vals({}), season: null)), {'season_code'});
      expect(errs(_awd(values: vals({}), plot: null)), {'plot_ref'});
      expect(errs(_awd(values: vals({}), files: const [])), {'file_refs'});
      // Phong bì: sha256 phải hex thường, không trùng file_ref, GPS trong phạm vi.
      expect(
        errs(_awd(values: vals({}), files: const [MrvFileRef(_f1, 'ABC')])),
        {'file_refs'},
      );
      expect(
        errs(
          _awd(
            values: vals({}),
            files: const [MrvFileRef(_f1, _sha1), MrvFileRef(_f1, _sha1)],
          ),
        ),
        {'file_refs'},
      );
      expect(errs(_awd(values: vals({}), gps: const MrvGps('91', '105'))), {
        'gps',
      });
      // text max 1000 ký tự.
      expect(errs(_awd(values: vals({'note': 'x' * 1001}))), {'values.note'});
    });

    test('so sánh decimal chính xác', () {
      expect(compareDecimal('-30', '-30.0'), 0);
      expect(compareDecimal('-30.1', '-30'), -1);
      expect(compareDecimal('10', '9.99'), 1);
      expect(compareDecimal('0.5', '-0.5'), 1);
      expect(normalizeFixed('10.0452000'), '10.0452');
      expect(MrvGps.fromDegrees(10.0452, 105.7701, 4.5).toJson(), {
        'lat': '10.0452',
        'lon': '105.7701',
        'accuracy_m': '4.5',
      });
    });
  });
}
