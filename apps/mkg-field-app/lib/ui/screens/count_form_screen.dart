import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/field_service.dart';
import '../../domain/fieldops.dart';
import '../providers.dart';

/// Ghi số (bản gốc) hoặc sửa số (bản ghi bù) một công đoạn của một thửa.
/// Kiểm FM-03/FM-04/FM-09 ngay trên máy; lưu vào hàng đợi, gửi khi có mạng.
class CountFormScreen extends ConsumerStatefulWidget {
  const CountFormScreen({
    super.key,
    required this.order,
    required this.plotRef,
    required this.stage,
    this.correction = false,
  });
  final CollectionOrder order;
  final String plotRef;
  final Stage stage;
  final bool correction;

  @override
  ConsumerState<CountFormScreen> createState() => _CountFormScreenState();
}

class _CountFormScreenState extends ConsumerState<CountFormScreen> {
  final _rolls = TextEditingController();
  final _kg = TextEditingController();
  final _reason = TextEditingController();
  final _fix = TextEditingController();
  final _vessel = TextEditingController();
  List<CountRuleError> _errors = const [];
  OrderBoard? _board;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    ref.services.field.board(widget.order).then((b) {
      if (!mounted) return;
      setState(() => _board = b);
      final cur = b.count(widget.plotRef, widget.stage);
      if (widget.correction && cur != null) {
        _rolls.text = '${cur.rolls}';
        _kg.text = cur.looseKg ?? '';
        _reason.text = cur.discrepancyReason ?? '';
      }
    });
  }

  @override
  void dispose() {
    _rolls.dispose();
    _kg.dispose();
    _reason.dispose();
    _fix.dispose();
    _vessel.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    setState(() {
      _saving = true;
      _errors = const [];
    });
    final f = ref.services.field;
    try {
      final CountOutcome out = widget.correction
          ? await f.correctCount(
              order: widget.order,
              plotRef: widget.plotRef,
              stage: widget.stage,
              rollsText: _rolls.text,
              looseKgText: _kg.text,
              discrepancyReason: _reason.text,
              correctionReason: _fix.text,
              vesselCode: _vessel.text,
            )
          : await f.recordCount(
              order: widget.order,
              plotRef: widget.plotRef,
              stage: widget.stage,
              rollsText: _rolls.text,
              looseKgText: _kg.text,
              discrepancyReason: _reason.text,
              vesselCode: _vessel.text,
            );
      if (!mounted) return;
      showMessage(
        context,
        out.gps == GpsStatus.ok
            ? 'Đã lưu — Chờ đồng bộ.'
            : 'Đã lưu (${out.gps.label}) — Chờ đồng bộ.',
      );
      Navigator.of(context).pop(true);
    } on FieldError catch (e) {
      setState(
        () => _errors = e.errors.isEmpty
            ? [CountRuleError('', '', e.message)]
            : e.errors,
      );
    } on OutboxException catch (e) {
      setState(() => _errors = [CountRuleError('', '', e.message)]);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final prev = widget.stage.previous;
    final prevCount = prev == null ? null : _board?.count(widget.plotRef, prev);
    return Scaffold(
      appBar: AppBar(title: Text(widget.correction ? 'Sửa số' : 'Ghi số')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Text(
            '${widget.stage.label} · Thửa ${shortRef(widget.plotRef)}',
            style: Theme.of(context).textTheme.titleMedium,
          ),
          if (prev != null)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Text(
                '${prev.label}: ${prevCount?.label ?? 'chưa có số'} — số công '
                'đoạn này phải bằng, hoặc ít hơn kèm lý do (FM-04).',
                key: const ValueKey('previous-hint'),
                style: const TextStyle(color: AgColors.mute, fontSize: 15),
              ),
            ),
          const SizedBox(height: 16),
          TextField(
            key: const ValueKey('rolls'),
            controller: _rolls,
            keyboardType: TextInputType.number,
            decoration: const InputDecoration(labelText: 'Số cuộn (bắt buộc)'),
          ),
          const SizedBox(height: 12),
          TextField(
            key: const ValueKey('loose-kg'),
            controller: _kg,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            decoration: const InputDecoration(
              labelText: 'Kg rơm rời (chỉ khi 0 cuộn)',
            ),
          ),
          const SizedBox(height: 12),
          TextField(
            key: const ValueKey('discrepancy'),
            controller: _reason,
            maxLines: 2,
            decoration: const InputDecoration(
              labelText: 'Lý do chênh (khi ít hơn công đoạn trước)',
            ),
          ),
          if (widget.stage.takesVesselCode) ...[
            const SizedBox(height: 12),
            TextField(
              key: const ValueKey('vessel-code'),
              controller: _vessel,
              textCapitalization: TextCapitalization.characters,
              maxLength: 64,
              decoration: InputDecoration(
                labelText: 'Số hiệu ghe / sà lan (không bắt buộc)',
                helperText: widget.correction
                    ? 'Để trống = giữ số hiệu đã ghi. Ví dụ: CT-12345'
                    : 'Ví dụ: CT-12345 — bỏ khoảng trắng, tự viết hoa.',
              ),
            ),
          ],
          if (widget.correction) ...[
            const SizedBox(height: 12),
            TextField(
              key: const ValueKey('correction-reason'),
              controller: _fix,
              maxLines: 2,
              decoration: const InputDecoration(
                labelText: 'Lý do sửa (bắt buộc, ≥ 10 ký tự)',
              ),
            ),
          ],
          if (_errors.isNotEmpty)
            Container(
              key: const ValueKey('count-errors'),
              margin: const EdgeInsets.only(top: 12),
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: AgColors.redBg,
                borderRadius: BorderRadius.circular(12),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  for (final e in _errors)
                    Text(
                      e.rule.isEmpty ? e.message : '${e.rule}: ${e.message}',
                      style: const TextStyle(color: AgColors.red, fontSize: 15),
                    ),
                ],
              ),
            ),
          const SizedBox(height: 20),
          FilledButton.icon(
            onPressed: _saving ? null : _save,
            icon: const Icon(Icons.save),
            label: const Text('Lưu'),
          ),
          const SizedBox(height: 8),
          const Text(
            'GPS được đóng dấu nếu có; không có GPS vẫn lưu được.',
            style: TextStyle(color: AgColors.mute, fontSize: 14),
          ),
        ],
      ),
    );
  }
}
