import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/collection_service.dart';
import '../providers.dart';

class _CollectionData {
  const _CollectionData(this.schedules, this.statements);
  final List<CollectionSchedule> schedules;
  final List<CollectionStatement> statements;
}

final _collectionProvider = FutureProvider.autoDispose<_CollectionData>((
  ref,
) async {
  final s = servicesOf(ref);
  return _CollectionData(
    await s.collection.schedules(),
    await s.collection.statements(),
  );
});

/// Thu gom rơm MKG (G3-2, RB-6 chiều mua): lịch gom và bảng kê số cuộn MKG
/// đã gom trên từng lô; quản lý HTX xác nhận hoặc tranh chấp (số cuộn + lý
/// do). Mất mạng → phản hồi "Đang chờ gửi", tự gửi khi có mạng.
class CollectionScreen extends ConsumerWidget {
  const CollectionScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final data = ref.watch(_collectionProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Thu gom rơm MKG')),
      body: data.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) => ListView(
          padding: const EdgeInsets.all(16),
          children: [
            Text('Lịch gom', style: Theme.of(context).textTheme.titleLarge),
            if (d.schedules.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 8),
                child: Text('Chưa có lịch gom của MKG.'),
              ),
            for (final s in d.schedules)
              Card(
                child: ListTile(
                  leading: const Icon(Icons.event),
                  title: Text('${s.windowFrom} → ${s.windowTo}'),
                  subtitle: Text('Xã ${s.communeCode} · ${s.statusLabel}'),
                ),
              ),
            const SizedBox(height: 16),
            Text(
              'Bảng kê số cuộn',
              style: Theme.of(context).textTheme.titleLarge,
            ),
            if (d.statements.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 8),
                child: Text('Chưa có bảng kê số cuộn.'),
              ),
            for (final st in d.statements)
              Card(
                key: ValueKey('stmt-${st.ref}'),
                child: ListTile(
                  title: Text('Lô ${st.plotCode} · ${st.rolls} cuộn'),
                  subtitle: Text(
                    'Gom ngày ${st.collectedOn} · bản ${st.revision}\n'
                    '${st.stateLabel}'
                    '${st.rejected != null ? '\nBị từ chối: ${st.rejected}' : ''}',
                  ),
                  isThreeLine: true,
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () async {
                    final done = await Navigator.of(context).push<bool>(
                      MaterialPageRoute(
                        builder: (_) => CollectionRespondScreen(statement: st),
                      ),
                    );
                    if (done == true) ref.invalidate(_collectionProvider);
                  },
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// Xác nhận / tranh chấp một bảng kê.
class CollectionRespondScreen extends ConsumerStatefulWidget {
  const CollectionRespondScreen({super.key, required this.statement});
  final CollectionStatement statement;

  @override
  ConsumerState<CollectionRespondScreen> createState() =>
      _CollectionRespondScreenState();
}

class _CollectionRespondScreenState
    extends ConsumerState<CollectionRespondScreen> {
  String _outcome = 'confirmed';
  final _rolls = TextEditingController();
  final _reason = TextEditingController();
  String? _error;
  bool _sending = false;

  @override
  void dispose() {
    _rolls.dispose();
    _reason.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    final st = widget.statement;
    setState(() => _sending = true);
    try {
      await ref.services.collection.respond(
        st,
        outcome: _outcome,
        htxRolls: int.tryParse(_rolls.text.trim()),
        reason: _reason.text,
      );
      if (!mounted) return;
      showMessage(context, 'Đã lưu phản hồi — Đang chờ gửi.');
      Navigator.of(context).pop(true);
    } on CollectionError catch (e) {
      setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final st = widget.statement;
    return Scaffold(
      appBar: AppBar(title: Text('Bảng kê lô ${st.plotCode}')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Text(
            'MKG ghi ${st.rolls} cuộn, gom ngày ${st.collectedOn} (bản ${st.revision}).',
            style: const TextStyle(fontSize: 17),
          ),
          const SizedBox(height: 12),
          RadioGroup<String>(
            groupValue: _outcome,
            onChanged: (v) => setState(() => _outcome = v ?? 'confirmed'),
            child: const Column(
              children: [
                RadioListTile<String>(
                  value: 'confirmed',
                  title: Text('Xác nhận đúng số MKG'),
                ),
                RadioListTile<String>(
                  value: 'disputed',
                  title: Text('Tranh chấp — HTX đếm khác'),
                ),
              ],
            ),
          ),
          if (_outcome == 'disputed')
            TextField(
              key: const ValueKey('collection-rolls'),
              controller: _rolls,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(
                labelText: 'Số cuộn HTX đếm được',
              ),
            ),
          TextField(
            key: const ValueKey('collection-reason'),
            controller: _reason,
            maxLines: 3,
            decoration: const InputDecoration(labelText: 'Lý do'),
          ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                _error!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
          const SizedBox(height: 16),
          FilledButton(
            onPressed: _sending ? null : _send,
            child: Text(
              _outcome == 'confirmed' ? 'Gửi xác nhận' : 'Gửi tranh chấp',
            ),
          ),
        ],
      ),
    );
  }
}
