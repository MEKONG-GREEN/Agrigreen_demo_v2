import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/kn_collection_service.dart';
import '../../domain/kn_context.dart';
import '../providers.dart';

/// Xã phụ trách của ngữ cảnh hiện hành.
final knCommunesProvider = Provider<List<KnCommune>>(
  (ref) => ref.watch(appControllerProvider).kn?.context?.communes ?? const [],
);

String knCommuneName(List<KnCommune> list, String? code) =>
    list.where((c) => c.code == code).firstOrNull?.name ?? (code ?? '');

/// Chạy một lệnh ghi và báo kết quả: đã lưu / đã xếp hàng / lỗi máy chủ.
Future<KnCommandResult?> knRunCommand(
  BuildContext context,
  Future<KnCommandResult> Function() f, {
  String done = 'Đã lưu.',
}) async {
  try {
    final r = await f();
    if (context.mounted) {
      showMessage(
        context,
        r.queued ? 'Mất kết nối — đã xếp hàng, sẽ tự gửi khi có mạng.' : done,
      );
    }
    return r;
  } on KnCommandError catch (e) {
    if (context.mounted) showMessage(context, e.message, error: true);
    return null;
  }
}

/// Hộp thoại nhập lý do (≥ [min] ký tự).
Future<String?> knAskReason(
  BuildContext context, {
  required String title,
  String? body,
  int min = 10,
}) async {
  final ctl = TextEditingController();
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => StatefulBuilder(
      builder: (ctx, set) => AlertDialog(
        title: Text(title),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (body != null) Text(body),
            TextField(
              key: const ValueKey('kn-reason'),
              controller: ctl,
              maxLines: 2,
              onChanged: (_) => set(() {}),
              decoration: InputDecoration(
                labelText: 'Lý do (ít nhất $min ký tự)',
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Hủy'),
          ),
          FilledButton(
            key: const ValueKey('kn-reason-ok'),
            onPressed: ctl.text.trim().length >= min
                ? () => Navigator.pop(ctx, true)
                : null,
            child: const Text('Xác nhận'),
          ),
        ],
      ),
    ),
  );
  final v = ctl.text.trim();
  ctl.dispose();
  return ok == true ? v : null;
}

/// Danh sách phân trang (cursor) tải dần — ListView.builder, nút "Tải thêm".
class KnPagedList extends StatefulWidget {
  const KnPagedList({
    super.key,
    required this.load,
    required this.itemBuilder,
    this.empty = 'Chưa có dữ liệu.',
    this.header,
  });

  final Future<KnPage> Function(String? cursor) load;
  final Widget Function(BuildContext, Map<String, Object?>) itemBuilder;
  final String empty;
  final Widget? header;

  @override
  State<KnPagedList> createState() => KnPagedListState();
}

class KnPagedListState extends State<KnPagedList> {
  final _items = <Map<String, Object?>>[];
  String? _next;
  bool _loading = false;
  bool _cached = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    reload();
  }

  Future<void> reload() async {
    _items.clear();
    _next = null;
    await _more(first: true);
  }

  Future<void> _more({bool first = false}) async {
    if (_loading) return;
    setState(() => _loading = true);
    try {
      final p = await widget.load(first ? null : _next);
      if (!mounted) return;
      setState(() {
        _items.addAll(p.items);
        _next = p.nextCursor;
        _cached = p.fromCache;
        _error = null;
      });
    } on KnCommandError catch (e) {
      if (mounted) setState(() => _error = e.message);
    } on Object {
      if (mounted) {
        setState(() => _error = 'Không tải được — cần kết nối mạng.');
      }
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final extra = (widget.header == null ? 0 : 1) + 1;
    return RefreshIndicator(
      onRefresh: reload,
      child: ListView.builder(
        padding: const EdgeInsets.only(bottom: 96),
        itemCount: _items.length + extra + 1,
        itemBuilder: (ctx, i) {
          if (widget.header != null && i == 0) return widget.header!;
          final j = i - (widget.header == null ? 0 : 1);
          if (j == 0) {
            if (_cached) {
              return const Padding(
                padding: EdgeInsets.all(8),
                child: Text('Ngoại tuyến — đang xem dữ liệu đã lưu trên máy.'),
              );
            }
            if (_error != null) {
              return Padding(
                padding: const EdgeInsets.all(12),
                child: Text(_error!),
              );
            }
            if (_items.isEmpty && !_loading) {
              return Padding(
                padding: const EdgeInsets.all(24),
                child: Text(widget.empty, textAlign: TextAlign.center),
              );
            }
            return const SizedBox.shrink();
          }
          final k = j - 1;
          if (k < _items.length) return widget.itemBuilder(ctx, _items[k]);
          if (_loading) {
            return const Padding(
              padding: EdgeInsets.all(16),
              child: Center(child: CircularProgressIndicator()),
            );
          }
          if (_next != null && _next!.isNotEmpty) {
            return Center(
              child: TextButton(
                key: const ValueKey('kn-load-more'),
                onPressed: _more,
                child: const Text('Tải thêm'),
              ),
            );
          }
          return const SizedBox.shrink();
        },
      ),
    );
  }
}
