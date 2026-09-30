import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/field_service.dart';
import '../../domain/fieldops.dart';
import '../providers.dart';

final _reconProvider = FutureProvider.autoDispose<Cached<List<ReconRow>>>(
  (ref) => servicesOf(ref).field.reconciliation(),
);

/// Đối soát chiều mua MKG ↔ HTX theo thửa (RB-6): số cuộn MKG xếp ghe/xe so
/// với số HTX xác nhận — chờ / khớp / lệch.
class ReconciliationScreen extends ConsumerWidget {
  const ReconciliationScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final q = ref.watch(_reconProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Đối soát chiều mua')),
      body: q.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Text('$e', style: const TextStyle(fontSize: 17)),
          ),
        ),
        data: (d) {
          int n(String s) => d.value.where((r) => r.status == s).length;
          return ListView(
            padding: const EdgeInsets.all(12),
            children: [
              if (d.stale)
                Text(
                  'Mất kết nối — số liệu lúc ${formatLocal(d.fetchedAt)}.',
                  style: const TextStyle(color: AgColors.amber),
                ),
              Text(
                'Khớp ${n('match')} · Lệch ${n('mismatch')} · Chờ ${n('pending')}',
                key: const ValueKey('recon-summary'),
                style: const TextStyle(
                  fontSize: 17,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 8),
              if (d.value.isEmpty)
                const Text(
                  'Chưa có thửa nào xếp ghe/xe.',
                  style: TextStyle(fontSize: 17),
                ),
              for (final r in d.value)
                Card(
                  child: ListTile(
                    leading: Icon(
                      switch (r.status) {
                        'match' => Icons.check_circle,
                        'mismatch' => Icons.warning_amber,
                        _ => Icons.hourglass_empty,
                      },
                      color: switch (r.status) {
                        'match' => AgColors.g700,
                        'mismatch' => AgColors.red,
                        _ => AgColors.amber,
                      },
                    ),
                    title: Text(
                      'Thửa ${shortRef(r.plotRef)} · ${ReconRow.statusLabel(r.status)}',
                    ),
                    subtitle: Text(
                      'MKG ${r.mkgRolls} cuộn · HTX ${r.htxRolls == null ? '—' : '${r.htxRolls} cuộn'}'
                      '${r.orderId == null ? '' : ' · lệnh ${shortRef(r.orderId!)}'}',
                    ),
                  ),
                ),
            ],
          );
        },
      ),
    );
  }
}
