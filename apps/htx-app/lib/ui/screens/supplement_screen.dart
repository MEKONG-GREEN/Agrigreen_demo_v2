import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/crop_service.dart';
import '../../domain/cropcycle.dart';
import '../providers.dart';
import '../widgets.dart';

final _householdOfSeason = FutureProvider.autoDispose.family<String?, String>(
  (ref, seasonRef) async =>
      (await servicesOf(ref).crop.seasonByRef(seasonRef))?.householdRef,
);

/// Bổ sung nhật ký khi quản lý HTX yêu cầu (HTX US-LOG-05 AC-3) — bản ghi bù
/// `op: correct`, lý do bắt buộc. GPS và thời gian **khóa** (US-LOG-01 AC-3).
class SupplementScreen extends ConsumerStatefulWidget {
  const SupplementScreen({super.key, required this.log});
  final FarmLogView log;

  @override
  ConsumerState<SupplementScreen> createState() => _SupplementScreenState();
}

class _SupplementScreenState extends ConsumerState<SupplementScreen> {
  late final TextEditingController _note;
  final _reason = TextEditingController();
  List<String> _photos = const [];
  String? _error;

  @override
  void initState() {
    super.initState();
    _note = TextEditingController(text: widget.log.note);
  }

  @override
  void dispose() {
    _note.dispose();
    _reason.dispose();
    super.dispose();
  }

  void _locked() => showMessage(context, msgLockedGpsTime, error: true);

  Future<void> _save() async {
    try {
      await ref.services.crop.supplementLog(
        log: widget.log,
        reason: _reason.text,
        note: _note.text,
        addFileRefs: _photos,
      );
      if (!mounted) return;
      showMessage(context, 'Đã lưu bản bổ sung — Chờ đồng bộ.');
      Navigator.of(context).pop(true);
    } on CropError catch (e) {
      setState(() => _error = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final l = widget.log;
    final hh = ref.watch(_householdOfSeason(l.seasonRef)).value;
    return Scaffold(
      appBar: AppBar(title: const Text('Bổ sung nhật ký')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Text(
            activityLabel(l.activityCode),
            style: Theme.of(context).textTheme.titleLarge,
          ),
          if (l.reviewNote != null && l.reviewNote!.isNotEmpty)
            Card(
              color: AgColors.redBg,
              child: ListTile(
                leading: const Icon(Icons.feedback, color: AgColors.red),
                title: const Text('Quản lý HTX yêu cầu'),
                subtitle: Text(l.reviewNote!),
              ),
            ),
          ListTile(
            key: const ValueKey('locked-time'),
            contentPadding: EdgeInsets.zero,
            leading: const Icon(Icons.lock),
            title: const Text('Thời gian ghi'),
            subtitle: Text(l.eventTime),
            onTap: _locked,
          ),
          ListTile(
            key: const ValueKey('locked-gps'),
            contentPadding: EdgeInsets.zero,
            leading: const Icon(Icons.lock),
            title: const Text('Vị trí GPS'),
            subtitle: Text(
              locationLabel(l.locationStatus, l.gpsSource) ?? 'Đã ghi nhận',
            ),
            onTap: _locked,
          ),
          const SizedBox(height: 8),
          TextField(
            key: const ValueKey('supplement-note'),
            controller: _note,
            maxLines: 3,
            decoration: const InputDecoration(labelText: 'Ghi chú'),
          ),
          const SizedBox(height: 12),
          PhotoCapture(
            householdRef: hh,
            photoIds: _photos,
            onChanged: (p) => setState(() => _photos = p),
            parentEntity: 'farm_log',
          ),
          const SizedBox(height: 12),
          TextField(
            key: const ValueKey('supplement-reason'),
            controller: _reason,
            decoration: const InputDecoration(
              labelText: 'Lý do bổ sung (ít nhất 10 ký tự)',
            ),
          ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                _error!,
                style: const TextStyle(color: AgColors.red, fontSize: 16),
              ),
            ),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: _save,
            icon: const Icon(Icons.save),
            label: const Text('Lưu bản bổ sung'),
          ),
        ],
      ),
    );
  }
}
