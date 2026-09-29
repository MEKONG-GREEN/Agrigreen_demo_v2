import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/task_service.dart';
import '../providers.dart';

final tasksProvider = FutureProvider.autoDispose<List<FieldTask>>(
  (ref) => servicesOf(ref).tasks.list(),
);

/// Nhịp mỗi phút để đếm ngược SLA.
final minuteTickProvider = StreamProvider.autoDispose<DateTime>((ref) async* {
  final clock = ref.watch(platformProvider).clock;
  yield clock.now();
  yield* Stream.periodic(const Duration(minutes: 1), (_) => clock.now());
});

Color slaColor(SlaLevel l) => switch (l) {
  SlaLevel.overdue => AgColors.red,
  SlaLevel.soon => AgColors.amber,
  SlaLevel.ok => AgColors.g700,
  SlaLevel.none => AgColors.mute,
};

/// Nhiệm vụ hỗ trợ được giao (KN epic J) — đếm ngược SLA theo hạn xử lý.
class TasksScreen extends ConsumerStatefulWidget {
  const TasksScreen({super.key, this.highlightTaskId});

  /// Mở ngay nhiệm vụ này (từ thông báo "nhiệm vụ leo thang").
  final String? highlightTaskId;

  @override
  ConsumerState<TasksScreen> createState() => _TasksScreenState();
}

class _TasksScreenState extends ConsumerState<TasksScreen> {
  bool _opened = false;

  Future<void> _open(FieldTask t) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (_) => _TaskUpdateSheet(task: t),
    );
    ref.invalidate(tasksProvider);
  }

  @override
  Widget build(BuildContext context) {
    final tasks = ref.watch(tasksProvider);
    final now =
        ref.watch(minuteTickProvider).value ??
        ref.read(platformProvider).clock.now();
    final hl = widget.highlightTaskId;
    if (!_opened && hl != null && tasks.hasValue) {
      _opened = true;
      final t = tasks.value!.where((t) => t.id == hl).firstOrNull;
      if (t != null) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) _open(t);
        });
      }
    }
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
                  final sla = t.sla(now);
                  return ListTile(
                    tileColor: t.id == hl ? AgColors.g100 : Colors.white,
                    title: Text(t.title),
                    subtitle: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          [
                            t.status.label,
                            if (t.dueAt != null)
                              'Hạn: ${formatLocal(DateTime.tryParse(t.dueAt!))}',
                            if (t.pendingLocal) 'Chờ đồng bộ',
                          ].join(' · '),
                        ),
                        if (sla.level != SlaLevel.none)
                          Row(
                            children: [
                              Icon(
                                sla.level == SlaLevel.overdue
                                    ? Icons.alarm_off
                                    : Icons.timer_outlined,
                                size: 18,
                                color: slaColor(sla.level),
                              ),
                              const SizedBox(width: 4),
                              Text(
                                sla.label,
                                style: TextStyle(
                                  color: slaColor(sla.level),
                                  fontWeight: FontWeight.w700,
                                ),
                              ),
                            ],
                          ),
                      ],
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
                    onTap: () => _open(t),
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
