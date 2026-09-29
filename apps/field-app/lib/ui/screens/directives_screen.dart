import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/directive_service.dart';
import '../providers.dart';
import 'notifications_screen.dart' show unawaitedSync;

final directivesProvider = FutureProvider.autoDispose<List<FieldDirective>>((
  ref,
) {
  ref.watch(syncStatusStreamProvider);
  return servicesOf(ref).directives.list();
});

/// Chỉ đạo từ cấp trên (KN US-TASK-03): đọc và xác nhận đã đọc được khi ngoại
/// tuyến; xác nhận gửi lên khi có mạng.
class DirectivesScreen extends ConsumerWidget {
  const DirectivesScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref.watch(directivesProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Chỉ đạo')),
      body: list.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (items) => items.isEmpty
            ? const Center(
                child: Text(
                  'Chưa có chỉ đạo nào.',
                  style: TextStyle(fontSize: 17),
                ),
              )
            : ListView.separated(
                itemCount: items.length,
                separatorBuilder: (_, _) => const Divider(height: 1),
                itemBuilder: (_, i) {
                  final d = items[i];
                  return ListTile(
                    tileColor: d.acked ? Colors.white : AgColors.amberBg,
                    leading: Icon(
                      d.urgent ? Icons.campaign : Icons.info_outline,
                      color: d.urgent ? AgColors.red : AgColors.g700,
                      size: 32,
                    ),
                    title: Text(
                      d.title,
                      style: TextStyle(
                        fontWeight: d.acked ? FontWeight.w500 : FontWeight.w800,
                      ),
                    ),
                    subtitle: Text(_subtitle(d)),
                    trailing: Icon(
                      d.acked ? Icons.check_circle : Icons.chevron_right,
                      color: d.acked ? AgColors.g700 : AgColors.mute,
                      size: 30,
                    ),
                    onTap: () async {
                      await Navigator.of(context).push(
                        MaterialPageRoute<void>(
                          builder: (_) =>
                              DirectiveDetailScreen(directiveId: d.id),
                        ),
                      );
                      ref.invalidate(directivesProvider);
                    },
                  );
                },
              ),
      ),
    );
  }
}

String _subtitle(FieldDirective d) => [
  if (d.urgent) 'Khẩn',
  'Ban hành ${formatLocal(d.issuedAt)}',
  if (d.ackedAt != null)
    'Đã xác nhận'
  else if (d.pendingAck)
    'Đã xác nhận · chờ đồng bộ'
  else if (d.isTarget)
    'Chưa xác nhận',
].join(' · ');

final _directiveProvider = FutureProvider.autoDispose
    .family<FieldDirective?, String>(
      (ref, id) => servicesOf(ref).directives.byId(id),
    );

class DirectiveDetailScreen extends ConsumerWidget {
  const DirectiveDetailScreen({super.key, required this.directiveId});
  final String directiveId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final d = ref.watch(_directiveProvider(directiveId));
    return Scaffold(
      appBar: AppBar(title: const Text('Chỉ đạo')),
      body: d.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (x) => x == null
            ? const Center(
                child: Padding(
                  padding: EdgeInsets.all(24),
                  child: Text(
                    'Chỉ đạo chưa có trên máy. Hãy đồng bộ khi có mạng.',
                    textAlign: TextAlign.center,
                    style: TextStyle(fontSize: 17),
                  ),
                ),
              )
            : ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  Text(x.title, style: Theme.of(context).textTheme.titleLarge),
                  const SizedBox(height: 6),
                  Text(
                    _subtitle(x),
                    style: const TextStyle(color: AgColors.mute),
                  ),
                  if (x.expiresAt != null)
                    Text(
                      'Hiệu lực đến ${formatLocal(x.expiresAt)}',
                      style: const TextStyle(color: AgColors.mute),
                    ),
                  const SizedBox(height: 16),
                  Text(x.body, style: const TextStyle(fontSize: 18)),
                  const SizedBox(height: 24),
                  if (x.isTarget && !x.acked)
                    FilledButton.icon(
                      icon: const Icon(Icons.done_all),
                      label: const Text('Đã đọc — xác nhận'),
                      onPressed: () async {
                        try {
                          await ref.services.directives.acknowledge(x.id);
                          ref.invalidate(_directiveProvider(directiveId));
                          ref.invalidate(homeSummaryProvider);
                          unawaitedSync(ref);
                          if (context.mounted) {
                            showMessage(
                              context,
                              'Đã xác nhận. Sẽ gửi khi có mạng.',
                            );
                          }
                        } on DirectiveException catch (e) {
                          if (context.mounted) {
                            showMessage(context, e.message, error: true);
                          }
                        }
                      },
                    ),
                ],
              ),
      ),
    );
  }
}
