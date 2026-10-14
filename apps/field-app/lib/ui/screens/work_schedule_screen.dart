import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/work_schedule_service.dart';
import '../../domain/work_schedule.dart';
import '../providers.dart';

/// Dịch vụ lịch công tác (ghi đè được trong kiểm thử widget).
final workScheduleServiceProvider = Provider<WorkScheduleService>(
  (ref) => servicesOf(ref).schedules,
);

/// Lịch công tác tuần (KN v7.7 FN-XA-03.08): danh sách theo ngày, mỗi ngày
/// hai phần Sáng / Chiều (thay lưới tuần rộng cho máy Android màn nhỏ).
/// Sửa được khi ngoại tuyến; "Lưu nháp" / "Gửi duyệt" đồng bộ khi có mạng.
class WorkScheduleScreen extends ConsumerStatefulWidget {
  const WorkScheduleScreen({super.key, this.initialWeek});

  /// Tuần mở sẵn (Thứ Hai); null = tuần kế tiếp.
  final DateTime? initialWeek;

  @override
  ConsumerState<WorkScheduleScreen> createState() => _WorkScheduleScreenState();
}

class _WorkScheduleScreenState extends ConsumerState<WorkScheduleScreen> {
  late DateTime _week;
  WeekSchedule? _view;
  Object? _error;
  bool _busy = false;
  List<WsLogEntry>? _log;
  String? _logError;

  WorkScheduleService get _svc => ref.read(workScheduleServiceProvider);

  @override
  void initState() {
    super.initState();
    _week = mondayOf(widget.initialWeek ?? _svc.defaultWeek());
    _reload();
  }

  Future<void> _reload({bool refresh = true}) =>
      _run(() => _svc.load(_week, refresh: refresh));

  Future<void> _run(Future<WeekSchedule> Function() f, {String? done}) async {
    if (!mounted) return;
    setState(() => _busy = true);
    try {
      final v = await f();
      if (!mounted) return;
      setState(() {
        _view = v;
        _error = null;
      });
      if (done != null) showMessage(context, done);
    } on WorkScheduleError catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    } catch (e) {
      if (!mounted) return;
      if (_view == null) {
        setState(() => _error = e);
      } else {
        showMessage(context, 'Lỗi: $e', error: true);
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _goWeek(int deltaWeeks) {
    setState(() {
      _week = _week.add(Duration(days: 7 * deltaWeeks));
      _view = null;
      _log = null;
      _logError = null;
    });
    _reload();
  }

  Future<void> _edit({WsItem? item, DateTime? day, WsSession? session}) async {
    final v = _view;
    if (v == null) return;
    final res = await Navigator.of(context).push<_EditResult>(
      MaterialPageRoute(
        builder: (_) => WorkScheduleItemScreen(
          weekStart: _week,
          item: item,
          day: day,
          session: session,
          communeCodes: v.communeCodes,
          newId: _svc.newItemId,
          readOnly: !v.editable,
        ),
      ),
    );
    if (res == null) return;
    if (res.delete) {
      await _run(
        () => _svc.removeItem(_week, res.item.id),
        done: 'Đã xóa mục.',
      );
    } else {
      final before = _view?.overlapIds ?? const <String>{};
      await _run(() => _svc.upsertItem(_week, res.item));
      final after = _view?.overlapIds ?? const <String>{};
      if (!mounted) return;
      if (after.contains(res.item.id) && !before.contains(res.item.id)) {
        showMessage(
          context,
          'Đã lưu mục trên máy. Cảnh báo: trùng thời gian với mục khác.',
        );
      } else {
        showMessage(context, 'Đã lưu mục trên máy.');
      }
    }
  }

  Future<void> _copyPrevious() async {
    final v = _view;
    if (v == null) return;
    var replace = false;
    if (v.items.isNotEmpty) {
      final ok = await showDialog<bool>(
        context: context,
        builder: (c) => AlertDialog(
          title: const Text('Sao chép tuần trước'),
          content: const Text(
            'Tuần này đã có mục công tác. Thay toàn bộ bằng lịch tuần trước?',
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(c, false),
              child: const Text('Hủy'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(c, true),
              child: const Text('Thay'),
            ),
          ],
        ),
      );
      if (ok != true) return;
      replace = true;
    }
    await _run(
      () => _svc.copyPrevious(_week, replace: replace),
      done: 'Đã sao chép lịch tuần trước (ngày dời 7 ngày).',
    );
  }

  Future<void> _save({required bool submit}) async {
    await _run(() => _svc.save(_week, submit: submit));
    final v = _view;
    if (!mounted || v == null) return;
    if (v.versionConflict || v.syncError != null) return;
    final what = submit ? 'Đã gửi duyệt' : 'Đã lưu nháp';
    showMessage(
      context,
      v.pendingSync ? '$what trên máy. Chờ đồng bộ khi có mạng.' : '$what.',
    );
  }

  Future<void> _loadLog() async {
    final id = _view?.scheduleId;
    if (id == null) return;
    setState(() => _logError = null);
    try {
      final l = await _svc.log(id);
      if (mounted) setState(() => _log = l);
    } on WorkScheduleError catch (e) {
      if (mounted) setState(() => _logError = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final v = _view;
    return Scaffold(
      appBar: AppBar(
        title: const Text('Lịch công tác'),
        actions: [
          IconButton(
            tooltip: 'Tải lại',
            iconSize: 28,
            icon: const Icon(Icons.refresh),
            onPressed: _busy ? null : _reload,
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _reload,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(12, 8, 12, 24),
          children: [
            _WeekPicker(
              week: _week,
              onPrev: _busy ? null : () => _goWeek(-1),
              onNext: _busy ? null : () => _goWeek(1),
            ),
            if (_busy) const LinearProgressIndicator(),
            if (v == null && _error != null)
              Card(
                child: ListTile(
                  leading: const Icon(Icons.error_outline, color: AgColors.red),
                  title: Text('Không mở được lịch: $_error'),
                ),
              ),
            if (v != null) ..._content(v),
          ],
        ),
      ),
    );
  }

  List<Widget> _content(WeekSchedule v) {
    final children = <Widget>[];
    if (v.loadError != null) {
      children.add(_Banner(v.loadError!, color: AgColors.red));
      return children;
    }
    if (v.offline) {
      children.add(
        _Banner(
          v.fetchedAt == null
              ? 'Đang ngoại tuyến — chưa có bản lưu của tuần này trên máy. '
                    'Vẫn thêm mục và lưu được, sẽ gửi khi có mạng.'
              : 'Đang ngoại tuyến — hiển thị bản lưu trên máy lúc '
                    '${formatLocal(v.fetchedAt)}.',
          color: AgColors.amber,
          icon: Icons.cloud_off,
        ),
      );
    }
    if (v.pendingSync) {
      children.add(
        _Banner(
          v.pendingAction == 'submit'
              ? 'Đã gửi duyệt trên máy · chờ đồng bộ.'
              : 'Đã lưu nháp trên máy · chờ đồng bộ.',
          color: AgColors.amber,
          icon: Icons.cloud_upload_outlined,
        ),
      );
    } else if (v.dirty && v.editable && !v.versionConflict) {
      children.add(
        const _Banner(
          'Có thay đổi chưa lưu. Bấm "Lưu nháp" hoặc "Gửi duyệt".',
          color: AgColors.blue,
          icon: Icons.edit_note,
        ),
      );
    }
    if (v.versionConflict) {
      children.add(
        Card(
          color: AgColors.redBg,
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Text(
                  'Lịch đã thay đổi trên máy chủ — tải lại',
                  style: TextStyle(
                    color: AgColors.red,
                    fontWeight: FontWeight.w700,
                    fontSize: 17,
                  ),
                ),
                const SizedBox(height: 4),
                const Text(
                  'Bản trên máy vẫn được giữ. "Tải lại" lấy bản mới nhất trên '
                  'máy chủ (bỏ thay đổi trên máy); "Giữ bản của tôi" đặt bản '
                  'trên máy lên phiên bản mới để gửi lại.',
                ),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    FilledButton.icon(
                      key: const ValueKey('ws-reload'),
                      onPressed: _busy
                          ? null
                          : () => _run(
                              () => _svc.discardLocal(_week),
                              done: 'Đã tải lại lịch từ máy chủ.',
                            ),
                      icon: const Icon(Icons.download),
                      label: const Text('Tải lại'),
                    ),
                    OutlinedButton(
                      key: const ValueKey('ws-keep-mine'),
                      onPressed: _busy
                          ? null
                          : () => _run(
                              () => _svc.keepMine(_week),
                              done: 'Đã giữ bản trên máy. Bấm lưu/gửi lại.',
                            ),
                      child: const Text('Giữ bản của tôi'),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      );
    } else if (v.syncError != null && !v.locked) {
      children.add(
        _Banner(
          'Máy chủ từ chối lần lưu gần nhất (${v.syncError}). Sửa rồi lưu lại.',
          color: AgColors.red,
        ),
      );
    }
    if (v.locked) {
      children.add(
        const _Banner(
          'Tuần đã kết thúc — lịch đã khóa, chỉ xem.',
          color: AgColors.mute,
          icon: Icons.lock_outline,
        ),
      );
    }
    children.add(_StatusCard(view: v));
    if (v.editable) {
      children.add(
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              OutlinedButton.icon(
                key: const ValueKey('ws-copy'),
                onPressed: _busy ? null : _copyPrevious,
                icon: const Icon(Icons.history),
                label: const Text('Sao chép tuần trước'),
              ),
              OutlinedButton.icon(
                key: const ValueKey('ws-draft'),
                onPressed: _busy || !v.canSaveDraft
                    ? null
                    : () => _save(submit: false),
                icon: const Icon(Icons.save_outlined),
                label: const Text('Lưu nháp'),
              ),
              FilledButton.icon(
                key: const ValueKey('ws-submit'),
                onPressed: _busy || !v.canSubmit
                    ? null
                    : () => _save(submit: true),
                icon: const Icon(Icons.send),
                label: const Text('Gửi duyệt'),
              ),
            ],
          ),
        ),
      );
    }
    for (var d = 0; d < 7; d++) {
      final day = _week.add(Duration(days: d));
      children.add(
        _DayCard(
          day: day,
          index: d,
          items: [
            for (final i in v.items)
              if (i.day == day) i,
          ],
          editable: v.editable,
          onAdd: (s) => _edit(day: day, session: s),
          onOpen: (i) => _edit(item: i),
          // W11.46 (CNTS CV-07): check-in tại địa điểm công tác của hôm nay.
          onCheckin: _busy
              ? null
              : (i) => v.canCheckin(i, DateTime.now())
                    ? () => _run(
                        () => _svc.checkin(_week, i),
                        done: 'Đã check-in — vị trí, thời điểm không sửa được.',
                      )
                    : null,
        ),
      );
    }
    if (v.scheduleId != null && v.status != null) {
      children.add(_logCard());
    }
    return children;
  }

  Widget _logCard() => Card(
    child: Padding(
      padding: const EdgeInsets.all(12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Expanded(
                child: Text(
                  'Lịch sử',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
                ),
              ),
              TextButton(
                key: const ValueKey('ws-log'),
                onPressed: _loadLog,
                child: Text(_log == null ? 'Xem lịch sử' : 'Tải lại'),
              ),
            ],
          ),
          if (_logError != null)
            Text(_logError!, style: const TextStyle(color: AgColors.mute)),
          if (_log != null && _log!.isEmpty) const Text('Chưa có sự việc nào.'),
          for (final e in _log ?? const <WsLogEntry>[])
            ListTile(
              dense: true,
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.circle, size: 10, color: AgColors.g600),
              title: Text(
                e.label,
                style: const TextStyle(fontWeight: FontWeight.w600),
              ),
              subtitle: Text(
                [
                  ?e.actor,
                  formatLocal(e.at),
                  if (e.reason != null && e.reason!.isNotEmpty)
                    'Lý do: ${e.reason}',
                ].join(' · '),
              ),
            ),
        ],
      ),
    ),
  );
}

class _WeekPicker extends StatelessWidget {
  const _WeekPicker({required this.week, this.onPrev, this.onNext});
  final DateTime week;
  final VoidCallback? onPrev;
  final VoidCallback? onNext;

  @override
  Widget build(BuildContext context) => Row(
    children: [
      IconButton(
        tooltip: 'Tuần trước',
        iconSize: 32,
        onPressed: onPrev,
        icon: const Icon(Icons.chevron_left),
      ),
      Expanded(
        child: Text(
          'Tuần ${weekLabel(week)}',
          textAlign: TextAlign.center,
          style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
        ),
      ),
      IconButton(
        tooltip: 'Tuần sau',
        iconSize: 32,
        onPressed: onNext,
        icon: const Icon(Icons.chevron_right),
      ),
    ],
  );
}

class _Banner extends StatelessWidget {
  const _Banner(this.text, {required this.color, this.icon});
  final String text;
  final Color color;
  final IconData? icon;

  @override
  Widget build(BuildContext context) => Card(
    color: color.withValues(alpha: 0.10),
    child: Padding(
      padding: const EdgeInsets.all(12),
      child: Row(
        children: [
          Icon(icon ?? Icons.info_outline, color: color),
          const SizedBox(width: 8),
          Expanded(child: Text(text)),
        ],
      ),
    ),
  );
}

class _Pill extends StatelessWidget {
  const _Pill(this.text, {required this.fg, required this.bg});
  final String text;
  final Color fg;
  final Color bg;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
    decoration: BoxDecoration(
      color: bg,
      borderRadius: BorderRadius.circular(20),
    ),
    child: Text(
      text,
      style: TextStyle(color: fg, fontSize: 14, fontWeight: FontWeight.w700),
    ),
  );
}

class _StatusCard extends StatelessWidget {
  const _StatusCard({required this.view});
  final WeekSchedule view;

  @override
  Widget build(BuildContext context) {
    final v = view;
    final (fg, bg) = switch (v.status) {
      WsStatus.approved => (AgColors.g700, AgColors.g100),
      WsStatus.changesRequested => (AgColors.red, AgColors.redBg),
      WsStatus.pending => (AgColors.amber, AgColors.amberBg),
      _ => (AgColors.blue, const Color(0xFFE6F3F8)),
    };
    final showLate =
        v.late ||
        (v.lateNow &&
            !v.locked &&
            (v.status == null ||
                v.status == WsStatus.draft ||
                v.status == WsStatus.changesRequested));
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Wrap(
              spacing: 8,
              runSpacing: 6,
              children: [
                _Pill(v.statusLabel, fg: fg, bg: bg),
                if (showLate)
                  const _Pill(
                    'Đăng ký muộn',
                    fg: AgColors.red,
                    bg: AgColors.redBg,
                  ),
                if (v.amendment)
                  const _Pill(
                    'Phần điều chỉnh chờ duyệt lại',
                    fg: AgColors.amber,
                    bg: AgColors.amberBg,
                  ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              'Hạn đăng ký: ${vnDateTimeLabel(v.submitDueAt)}',
              style: const TextStyle(color: AgColors.mute),
            ),
            if (v.status == WsStatus.changesRequested &&
                (v.lastReason ?? '').isNotEmpty) ...[
              const SizedBox(height: 6),
              Text(
                'Lý do yêu cầu chỉnh sửa: ${v.lastReason}',
                style: const TextStyle(
                  color: AgColors.red,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
            if (v.amendment && v.approvedItems.isNotEmpty) ...[
              const SizedBox(height: 6),
              Text(
                'Bản đã duyệt (${v.approvedItems.length} mục) vẫn hiệu lực tới '
                'khi duyệt phần điều chỉnh.',
                style: const TextStyle(color: AgColors.mute),
              ),
            ],
            if (v.overlapIds.isNotEmpty) ...[
              const SizedBox(height: 6),
              Text(
                'Cảnh báo: ${v.overlapIds.length} mục trùng thời gian.',
                style: const TextStyle(
                  color: AgColors.amber,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _DayCard extends StatelessWidget {
  const _DayCard({
    required this.day,
    required this.index,
    required this.items,
    required this.editable,
    required this.onAdd,
    required this.onOpen,
    this.onCheckin,
  });
  final DateTime day;
  final int index;
  final List<WsItem> items;
  final bool editable;
  final void Function(WsSession) onAdd;
  final void Function(WsItem) onOpen;
  final VoidCallback? Function(WsItem)? onCheckin;

  @override
  Widget build(BuildContext context) => Card(
    child: Padding(
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            '${weekdayLong[index]} ${dayMonth(day)}',
            style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
          ),
          for (final s in WsSession.values) ...[
            Row(
              children: [
                Expanded(
                  child: Text(
                    s.label,
                    style: const TextStyle(
                      color: AgColors.g700,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                if (editable)
                  TextButton.icon(
                    key: ValueKey('ws-add-${ymd(day)}-${s.code}'),
                    onPressed: () => onAdd(s),
                    icon: const Icon(Icons.add),
                    label: const Text('Thêm'),
                  ),
              ],
            ),
            for (final i in items)
              if (i.session == s)
                _ItemTile(
                  item: i,
                  onTap: () => onOpen(i),
                  onCheckin: onCheckin?.call(i),
                ),
            if (!items.any((i) => i.session == s))
              const Padding(
                padding: EdgeInsets.only(bottom: 6),
                child: Text('—', style: TextStyle(color: AgColors.mute)),
              ),
          ],
        ],
      ),
    ),
  );
}

class _ItemTile extends StatelessWidget {
  const _ItemTile({required this.item, required this.onTap, this.onCheckin});
  final WsItem item;
  final VoidCallback onTap;

  /// Khác null: hiện nút "Check-in" (W11.46).
  final VoidCallback? onCheckin;

  @override
  Widget build(BuildContext context) {
    final i = item;
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Material(
        color: i.conflict ? AgColors.amberBg : AgColors.g100,
        borderRadius: BorderRadius.circular(6),
        clipBehavior: Clip.antiAlias,
        child: DecoratedBox(
          decoration: BoxDecoration(
            border: Border(
              left: BorderSide(
                color: i.conflict ? AgColors.amber : AgColors.g600,
                width: 4,
              ),
            ),
          ),
          child: ListTile(
            key: ValueKey('ws-item-${i.id}'),
            onTap: onTap,
            title: Text(
              i.content,
              style: const TextStyle(fontWeight: FontWeight.w600),
            ),
            subtitle: Text(
              [
                i.timeLabel,
                if (i.placeLabel.isNotEmpty) i.placeLabel,
                if (i.participants != null) 'Phối hợp: ${i.participants}',
                if (i.conflict) 'Trùng thời gian',
                if (i.changed) 'Điều chỉnh',
                if (i.checkin != null) i.checkin!.label,
              ].join(' · '),
            ),
            trailing: onCheckin != null
                ? FilledButton.tonalIcon(
                    key: ValueKey('ws-checkin-${i.id}'),
                    onPressed: onCheckin,
                    icon: const Icon(Icons.my_location),
                    label: const Text('Check-in'),
                  )
                : i.checkin != null
                ? Icon(
                    i.checkin!.far ? Icons.wrong_location : Icons.where_to_vote,
                    color: i.checkin!.far ? AgColors.amber : AgColors.g600,
                  )
                : i.conflict
                ? const Icon(Icons.warning_amber, color: AgColors.amber)
                : null,
          ),
        ),
      ),
    );
  }
}

class _EditResult {
  const _EditResult(this.item, {this.delete = false});
  final WsItem item;
  final bool delete;
}

/// Khung "Mục công tác": ngày, buổi/giờ, nội dung, địa điểm, thành phần phối
/// hợp, ghi chú. Kiểm như máy chủ trước khi trả về.
class WorkScheduleItemScreen extends StatefulWidget {
  const WorkScheduleItemScreen({
    super.key,
    required this.weekStart,
    required this.newId,
    this.item,
    this.day,
    this.session,
    this.communeCodes = const [],
    this.readOnly = false,
  });

  final DateTime weekStart;
  final WsItem? item;
  final DateTime? day;
  final WsSession? session;
  final List<String> communeCodes;
  final String Function() newId;
  final bool readOnly;

  @override
  State<WorkScheduleItemScreen> createState() => _WorkScheduleItemScreenState();
}

class _WorkScheduleItemScreenState extends State<WorkScheduleItemScreen> {
  late DateTime _day;
  late WsSession _session;
  String? _commune;
  late final _start = TextEditingController(text: widget.item?.startTime);
  late final _end = TextEditingController(text: widget.item?.endTime);
  late final _content = TextEditingController(text: widget.item?.content);
  late final _location = TextEditingController(text: widget.item?.location);
  late final _participants = TextEditingController(
    text: widget.item?.participants,
  );
  late final _note = TextEditingController(text: widget.item?.note);
  String? _errorField;
  String? _error;

  @override
  void initState() {
    super.initState();
    _day = widget.item?.day ?? widget.day ?? widget.weekStart;
    _session = widget.item?.session ?? widget.session ?? WsSession.morning;
    _commune = widget.item?.communeCode;
  }

  @override
  void dispose() {
    for (final c in [_start, _end, _content, _location, _participants, _note]) {
      c.dispose();
    }
    super.dispose();
  }

  static String? _t(TextEditingController c) {
    final v = c.text.trim();
    return v.isEmpty ? null : v;
  }

  void _save() {
    final it = WsItem(
      id: widget.item?.id ?? widget.newId(),
      day: _day,
      session: _session,
      startTime: _t(_start),
      endTime: _t(_end),
      content: _content.text.trim(),
      communeCode: _commune,
      cooperativeRef: widget.item?.cooperativeRef,
      location: _t(_location),
      participants: _t(_participants),
      note: _t(_note),
      changed: widget.item?.changed ?? false,
    );
    try {
      validateItem(it, widget.weekStart);
    } on WorkScheduleError catch (e) {
      setState(() {
        _errorField = e.field;
        _error = e.message;
      });
      return;
    }
    Navigator.of(context).pop(_EditResult(it));
  }

  String? _err(String f) => _errorField == f ? _error : null;

  @override
  Widget build(BuildContext context) {
    final ro = widget.readOnly;
    final communes = {...widget.communeCodes, ?_commune}.toList();
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.item == null ? 'Thêm mục công tác' : 'Mục công tác'),
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          DropdownButtonFormField<DateTime>(
            key: const ValueKey('ws-f-day'),
            initialValue: _day,
            decoration: InputDecoration(
              labelText: 'Ngày *',
              errorText: _err('day'),
            ),
            items: [
              for (var d = 0; d < 7; d++)
                DropdownMenuItem(
                  value: widget.weekStart.add(Duration(days: d)),
                  child: Text(
                    '${weekdayLong[d]} '
                    '${dayMonth(widget.weekStart.add(Duration(days: d)))}',
                  ),
                ),
            ],
            onChanged: ro ? null : (d) => setState(() => _day = d ?? _day),
          ),
          const SizedBox(height: 12),
          SegmentedButton<WsSession>(
            segments: [
              for (final s in WsSession.values)
                ButtonSegment(value: s, label: Text(s.label)),
            ],
            selected: {_session},
            onSelectionChanged: ro
                ? null
                : (s) => setState(() => _session = s.first),
          ),
          const SizedBox(height: 12),
          Row(
            children: [
              Expanded(
                child: TextField(
                  key: const ValueKey('ws-f-start'),
                  controller: _start,
                  readOnly: ro,
                  keyboardType: TextInputType.datetime,
                  decoration: InputDecoration(
                    labelText: 'Từ giờ',
                    hintText: '07:30',
                    errorText: _err('start_time'),
                  ),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: TextField(
                  key: const ValueKey('ws-f-end'),
                  controller: _end,
                  readOnly: ro,
                  keyboardType: TextInputType.datetime,
                  decoration: InputDecoration(
                    labelText: 'Đến giờ',
                    hintText: '11:00',
                    errorText: _err('end_time'),
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          TextField(
            key: const ValueKey('ws-f-content'),
            controller: _content,
            readOnly: ro,
            maxLines: 3,
            minLines: 1,
            maxLength: 500,
            decoration: InputDecoration(
              labelText: 'Nội dung công việc *',
              errorText: _err('content'),
            ),
          ),
          if (communes.isNotEmpty) ...[
            DropdownButtonFormField<String?>(
              key: const ValueKey('ws-f-commune'),
              initialValue: _commune,
              decoration: const InputDecoration(labelText: 'Xã/phường'),
              items: [
                const DropdownMenuItem<String?>(child: Text('—')),
                for (final c in communes)
                  DropdownMenuItem<String?>(value: c, child: Text(c)),
              ],
              onChanged: ro ? null : (c) => setState(() => _commune = c),
            ),
            const SizedBox(height: 12),
          ],
          TextField(
            key: const ValueKey('ws-f-location'),
            controller: _location,
            readOnly: ro,
            maxLength: 200,
            decoration: InputDecoration(
              labelText: 'Địa điểm (ấp, cánh đồng, HTX…)',
              errorText: _err('location'),
            ),
          ),
          TextField(
            key: const ValueKey('ws-f-participants'),
            controller: _participants,
            readOnly: ro,
            maxLength: 500,
            decoration: InputDecoration(
              labelText: 'Thành phần phối hợp',
              helperText: 'Không ghi số điện thoại, số CCCD.',
              errorText: _err('participants'),
            ),
          ),
          TextField(
            key: const ValueKey('ws-f-note'),
            controller: _note,
            readOnly: ro,
            maxLength: 1000,
            maxLines: 3,
            minLines: 1,
            decoration: InputDecoration(
              labelText: 'Ghi chú',
              errorText: _err('note'),
            ),
          ),
          if (_error != null &&
              !const {
                'day',
                'start_time',
                'end_time',
                'content',
                'location',
                'participants',
                'note',
              }.contains(_errorField))
            Text(_error!, style: const TextStyle(color: AgColors.red)),
          const SizedBox(height: 16),
          if (!ro) ...[
            FilledButton.icon(
              key: const ValueKey('ws-f-save'),
              onPressed: _save,
              icon: const Icon(Icons.check),
              label: const Text('Lưu mục'),
            ),
            if (widget.item != null) ...[
              const SizedBox(height: 8),
              OutlinedButton.icon(
                key: const ValueKey('ws-f-delete'),
                onPressed: () =>
                    Navigator.of(context)
                        .pop(_EditResult(widget.item!, delete: true)),
                icon: const Icon(Icons.delete_outline),
                label: const Text('Xóa mục'),
              ),
            ],
          ],
        ],
      ),
    );
  }
}
