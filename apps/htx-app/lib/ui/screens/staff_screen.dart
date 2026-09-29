import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/staff_service.dart';
import '../providers.dart';

final _staffQuery = NotifierProvider.autoDispose<_Query, String>(_Query.new);

class _Query extends Notifier<String> {
  @override
  String build() => '';
  void set(String q) => state = q;
}

final _staffProvider = FutureProvider.autoDispose<List<StaffEntry>>(
  (ref) => servicesOf(ref).staff.search(ref.watch(_staffQuery)),
);

/// Danh bạ cán bộ HTX & gọi nhanh (HTX US-SUPPORT-02). Cần mạng; số điện thoại
/// đọc lúc bấm, không lưu trên máy. Bấm số → chép vào bộ nhớ tạm (AC-1);
/// cán bộ không có số hợp lệ → ẩn nút gọi (AC-2).
class StaffScreen extends ConsumerStatefulWidget {
  const StaffScreen({super.key});

  @override
  ConsumerState<StaffScreen> createState() => _StaffScreenState();
}

class _StaffScreenState extends ConsumerState<StaffScreen> {
  final _q = TextEditingController();

  @override
  void dispose() {
    _q.dispose();
    super.dispose();
  }

  Future<void> _run(Future<void> Function() f) async {
    try {
      await f();
    } on StaffException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    final list = ref.watch(_staffProvider);
    final s = ref.services;
    return Scaffold(
      appBar: AppBar(title: const Text('Danh bạ cán bộ HTX')),
      body: ListView(
        padding: const EdgeInsets.all(12),
        children: [
          TextField(
            key: const ValueKey('staff-search'),
            controller: _q,
            textInputAction: TextInputAction.search,
            decoration: InputDecoration(
              labelText: 'Tìm theo tên hoặc chức vụ',
              suffixIcon: IconButton(
                tooltip: 'Tìm',
                icon: const Icon(Icons.search),
                onPressed: () => ref.read(_staffQuery.notifier).set(_q.text),
              ),
            ),
            onSubmitted: (v) => ref.read(_staffQuery.notifier).set(v),
          ),
          const SizedBox(height: 8),
          ...list.when(
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
                  title: Text(e is StaffException ? e.message : 'Lỗi: $e'),
                ),
              ),
            ],
            data: (items) => [
              if (items.isEmpty) const Text('Không tìm thấy cán bộ.'),
              for (final e in items)
                Card(
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 6),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        ListTile(
                          leading: const Icon(
                            Icons.person,
                            size: 32,
                            color: AgColors.g700,
                          ),
                          title: Text(e.displayName),
                          subtitle: Text(e.title),
                        ),
                        if (e.hasPhone)
                          Padding(
                            padding: const EdgeInsets.symmetric(horizontal: 12),
                            child: Wrap(
                              spacing: 8,
                              runSpacing: 8,
                              children: [
                                FilledButton.icon(
                                  key: ValueKey('call-${e.userRef}'),
                                  icon: const Icon(Icons.call),
                                  label: const Text('Gọi'),
                                  onPressed: () =>
                                      _run(() => s.staff.call(e.userRef)),
                                ),
                                OutlinedButton.icon(
                                  key: ValueKey('copy-${e.userRef}'),
                                  icon: const Icon(Icons.copy),
                                  label: const Text('Chép số'),
                                  onPressed: () => _run(() async {
                                    final phone = await s.staff.phoneOf(
                                      e.userRef,
                                    );
                                    await s.platform.copyText(phone);
                                    if (context.mounted) {
                                      showMessage(
                                        context,
                                        'Đã chép số điện thoại vào bộ nhớ tạm.',
                                      );
                                    }
                                  }),
                                ),
                              ],
                            ),
                          )
                        else
                          const Padding(
                            padding: EdgeInsets.symmetric(horizontal: 16),
                            child: Text(
                              'Chưa có số điện thoại',
                              style: TextStyle(color: AgColors.mute),
                            ),
                          ),
                      ],
                    ),
                  ),
                ),
            ],
          ),
        ],
      ),
    );
  }
}
