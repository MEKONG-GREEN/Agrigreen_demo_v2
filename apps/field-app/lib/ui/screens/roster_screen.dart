import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/roster_service.dart';
import '../providers.dart';

final rosterProvider = FutureProvider.autoDispose<Roster>(
  (ref) => servicesOf(ref).roster.load(),
);

/// Danh bạ trực (KN US-DIR-01/02): ai đang trực trong địa bàn, gọi/nhắn một
/// chạm; tự khai "Đang trực"/"Tạm nghỉ". Cần mạng — không lưu danh bạ và số
/// điện thoại trên máy.
class RosterScreen extends ConsumerWidget {
  const RosterScreen({super.key});

  Future<void> _run(
    BuildContext context,
    Future<void> Function() f, {
    String? done,
  }) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await f();
      if (done != null) messenger.showSnackBar(SnackBar(content: Text(done)));
    } on RosterException catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(e.message)));
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final roster = ref.watch(rosterProvider);
    final svc = ref.read(appControllerProvider).services!.roster;
    Widget tile(RosterEntry e) => Card(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 6),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            ListTile(
              leading: Icon(
                e.onDuty ? Icons.support_agent : Icons.person_outline,
                color: e.onDuty ? AgColors.g700 : AgColors.mute,
                size: 32,
              ),
              title: Text(
                e.displayName,
                style: const TextStyle(fontWeight: FontWeight.w700),
              ),
              subtitle: Text(
                [
                  e.roleTitle,
                  e.dutyLabel,
                  if (e.until != null) 'đến ${formatLocal(e.until)}',
                ].where((s) => s.isNotEmpty).join(' · '),
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  FilledButton.icon(
                    key: ValueKey('call-${e.actorRef}'),
                    icon: const Icon(Icons.call),
                    label: const Text('Gọi'),
                    onPressed: () =>
                        _run(context, () => svc.contact(e.actorRef)),
                  ),
                  OutlinedButton.icon(
                    key: ValueKey('sms-${e.actorRef}'),
                    icon: const Icon(Icons.sms_outlined),
                    label: const Text('Nhắn tin'),
                    onPressed: () =>
                        _run(context, () => svc.contact(e.actorRef, sms: true)),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );

    return Scaffold(
      appBar: AppBar(title: const Text('Danh bạ trực')),
      body: RefreshIndicator(
        onRefresh: () async => ref.invalidate(rosterProvider),
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            Card(
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    const Text(
                      'Trạng thái của tôi:',
                      style: TextStyle(fontWeight: FontWeight.w700),
                    ),
                    FilledButton(
                      onPressed: () => _run(context, () async {
                        await svc.setStatus(onDuty: true);
                        ref.invalidate(rosterProvider);
                      }, done: 'Đã cập nhật: Đang trực'),
                      child: const Text('Đang trực'),
                    ),
                    OutlinedButton(
                      onPressed: () => _run(context, () async {
                        await svc.setStatus(onDuty: false);
                        ref.invalidate(rosterProvider);
                      }, done: 'Đã cập nhật: Tạm nghỉ'),
                      child: const Text('Tạm nghỉ'),
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 8),
            ...roster.when(
              loading: () => const [
                Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(child: CircularProgressIndicator()),
                ),
              ],
              error: (e, _) => [
                Card(
                  child: ListTile(
                    leading: const Icon(Icons.wifi_off, color: AgColors.amber),
                    title: Text(e is RosterException ? e.message : 'Lỗi: $e'),
                  ),
                ),
              ],
              data: (r) => [
                Text(
                  '${r.onDuty} cán bộ đang trực',
                  style: const TextStyle(
                    fontSize: 17,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 8),
                if (r.items.isEmpty)
                  const Text('Địa bàn chưa có cán bộ trong danh bạ.'),
                ...r.items.map(tile),
                if (r.fallback.isNotEmpty) ...[
                  const SizedBox(height: 12),
                  const Text(
                    'Không ai đang trực — liên hệ cán bộ TTKN tỉnh:',
                    style: TextStyle(fontWeight: FontWeight.w700),
                  ),
                  ...r.fallback.map(tile),
                ],
              ],
            ),
          ],
        ),
      ),
    );
  }
}
