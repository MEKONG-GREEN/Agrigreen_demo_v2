import 'package:agrigreen_mkg_field/application/app_services.dart';
import 'package:agrigreen_mkg_field/domain/fieldops.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';

import '../integration_test/support/mock_mkg_server.dart';
import 'support/test_env.dart';

/// HTTP thật (HttpSyncApi của lõi, đường dẫn vùng mkg) ↔ api-internal giả
/// trên 127.0.0.1: X-Device-Id, Idempotency-Key theo lô, con trỏ pull.
void main() {
  test('đồng bộ HTTP thật: pull collection_order, push roll_count đúng một lần, cursor lạ → 400 fieldops.cursor_invalid', () async {
    final server = await MockMkgServer.start();
    addTearDown(server.close);
    server.addOrder(orderData());
    final env = TestEnv();
    final pf = env.platform;
    final s = await env.open();
    addTearDown(s.close);
    // Thay API giả bằng HTTP thật tới máy chủ giả.
    final http = mkgHttpApi(
      baseUrl: server.baseUrl,
      token: () async => 'tok',
      deviceId: s.deviceId,
    );
    expect(pf.config.apiBaseUrl, isNotEmpty);
    final page = await http.pull(limit: 100);
    expect(page.changes.single.entity, 'collection_order');
    await s.refs.applyPage(page);
    final o = (await s.field.order(orderId))!;
    final rec = await s.field.recordCount(
      order: o,
      plotRef: plotA,
      stage: Stage.bale,
      rollsText: '12',
    );
    final batch = (await s.outbox.claimBatch(10))!;
    final r1 = await http.push(
      batchKey: batch.batchKey,
      deviceId: s.deviceId,
      records: batch.records,
    );
    // Gửi lại cùng lô (phản hồi bị mất) → phát lại, không ghi sổ lần hai.
    final r2 = await http.push(
      batchKey: batch.batchKey,
      deviceId: s.deviceId,
      records: batch.records,
    );
    expect(r1.single.status, 'accepted');
    expect(r2.single.status, 'accepted');
    expect(server.rollCounts, hasLength(1));
    expect(server.rollCounts.single['id'], rec.record.id);
    expect(server.requests, contains('POST /v1/fieldops/sync/push'));
    // Thiết bị khác header → 403 fieldops.device_mismatch.
    final other = mkgHttpApi(
      baseUrl: server.baseUrl,
      token: () async => 'tok',
      deviceId: 'khac',
    );
    await expectLater(
      other.push(batchKey: 'b2', deviceId: s.deviceId, records: const []),
      throwsA(
        isA<ApiException>().having(
          (e) => e.code,
          'code',
          'fieldops.device_mismatch',
        ),
      ),
    );
    await expectLater(
      http.pull(cursor: 'hỏng', limit: 10),
      throwsA(
        isA<ApiException>().having(
          (e) => e.code,
          'code',
          'fieldops.cursor_invalid',
        ),
      ),
    );
    expect(PushEntity.mkgFieldops, contains(rec.record.entity));
  });
}
