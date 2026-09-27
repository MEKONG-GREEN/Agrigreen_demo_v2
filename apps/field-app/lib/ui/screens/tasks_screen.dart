import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/task_service.dart';
import '../providers.dart';
import '../theme.dart';
import '../widgets/common.dart';

final tasksProvider = FutureProvider.autoDispose<List<FieldTask>>(
  (ref) => servicesOf(ref).tasks.list(),
);

/// Nhiệm vụ hỗ trợ được giao (KN epic J).
class TasksScreen extends ConsumerWidget {
  const TasksScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tasks = ref.watch(tasksProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Nhiệm vụ hỗ trợ')),
      body: tasks.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (list) => list.isEmpty
            ? const Center(
                child: Text(
                  'Chưa có nhiệm vụ nào được giao.',
                  style: TextStyle(fontSize: 17),
                ),
              )
            : ListView.separated(
                itemCount: list.length,
                separatorBuilder: (_, _) => const Divider(height: 1),
                itemBuilder: (_, i) {
                  final t = list[i];
                  return ListTile(
                    tileColor: Colors.white,
                    title: Text(t.title),
                    subtitle: Text(
                      [
                        t.status.label,
                        if (t.dueAt != null) 'Hạn: ${t.dueAt}',
                        if (t.pendingLocal) 'Chờ đồng bộ',
                      ].join(' · '),
                    ),
                    trailing: Icon(
                      t.status == TaskStatus.done
                          ? Icons.check_circle
                          : Icons.chevron_right,
                      color: t.status == TaskStatus.done
                          ? AgColors.g700
                          : AgColors.mute,
                      size: 30,
                    ),
                    onTap: () async {
                      await showModalBottomSheet<void>(
                        context: context,
                        isScrollControlled: true,
                        builder: (_) => _TaskUpdateSheet(task: t),
                      );
                      ref.invalidate(tasksProvider);
                    },
                  );
                },
              ),
      ),
    );
  }
}

class _TaskUpdateSheet extends ConsumerStatefulWidget {
  const _TaskUpdateSheet({required this.task});
  final FieldTask task;

  @override
  ConsumerState<_TaskUpdateSheet> createState() => _TaskUpdateSheetState();
}

class _TaskUpdateSheetState extends ConsumerState<_TaskUpdateSheet> {
  late TaskStatus _status = widget.task.status == TaskStatus.assigned
      ? TaskStatus.accepted
      : widget.task.status;
  final _note = TextEditingController();
  final _result = TextEditingController();

  @override
  void dispose() {
    _note.dispose();
    _result.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final s = ref.read(appControllerProvider).services;
    if (s == null) return;
    try {
      await s.tasks.update(
        taskId: widget.task.id,
        status: _status,
        note: _note.text,
        result: _result.text,
      );
      if (!mounted) return;
      showMessage(context, 'Đã lưu cập nhật. Chờ đồng bộ.');
      Navigator.of(context).pop();
    } on TaskException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.fromLTRB(
        16,
        16,
        16,
        MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            widget.task.title,
            style: Theme.of(context).textTheme.titleLarge,
          ),
          if (widget.task.description != null)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Text(widget.task.description!),
            ),
          const SizedBox(height: 12),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final st in [
                TaskStatus.accepted,
                TaskStatus.inProgress,
                TaskStatus.done,
                TaskStatus.escalated,
              ])
                ChoiceChip(
                  label: Text(st.label, style: const TextStyle(fontSize: 16)),
                  padding: const EdgeInsets.all(10),
                  selected: _status == st,
                  onSelected: (_) => setState(() => _status = st),
                ),
            ],
          ),
          const SizedBox(height: 12),
          if (_status == TaskStatus.done)
            TextField(
              controller: _result,
              maxLines: 2,
              decoration: const InputDecoration(
                labelText: 'Kết quả xử lý (bắt buộc)',
              ),
            ),
          const SizedBox(height: 8),
          TextField(
            controller: _note,
            maxLines: 2,
            decoration: InputDecoration(
              labelText: _status == TaskStatus.escalated
                  ? 'Lý do chuyển tiếp (bắt buộc)'
                  : 'Ghi chú',
            ),
          ),
          const SizedBox(height: 12),
          FilledButton(onPressed: _save, child: const Text('Lưu')),
        ],
      ),
    );
  }
}
