import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../data/files/evidence_repository.dart';
import '../../data/photo/photo_pipeline.dart';
import '../../domain/consent.dart';
import '../providers.dart';
import '../theme.dart';
import '../widgets/common.dart';
import 'consent_screen.dart';

final _blockReasonProvider = FutureProvider.autoDispose.family<String?, String>(
  (ref, subjectRef) => servicesOf(ref).photos.blockReason(subjectRef),
);

final _evidenceProvider = FutureProvider.autoDispose
    .family<List<EvidenceFile>, String>(
      (ref, parentId) => servicesOf(ref).evidence.forParent(parentId),
    );

/// Chụp ảnh minh chứng. Ảnh được mã hóa ngay bằng khóa công khai của hộ; máy
/// không xem lại được ảnh sau khi lưu (chỉ có ciphertext).
class PhotoCaptureScreen extends ConsumerStatefulWidget {
  const PhotoCaptureScreen({
    super.key,
    required this.subjectRef,
    required this.parentEntity,
    required this.parentId,
  });
  final String subjectRef;
  final String parentEntity;
  final String parentId;

  @override
  ConsumerState<PhotoCaptureScreen> createState() => _PhotoCaptureScreenState();
}

class _PhotoCaptureScreenState extends ConsumerState<PhotoCaptureScreen> {
  bool _busy = false;

  Future<void> _capture() async {
    final s = ref.read(appControllerProvider).services;
    if (s == null) return;
    setState(() => _busy = true);
    try {
      final out = await s.photos.capture(
        subjectRef: widget.subjectRef,
        parentEntity: widget.parentEntity,
        parentId: widget.parentId,
      );
      if (!mounted || out == null) return;
      showMessage(
        context,
        'Đã mã hóa và lưu ảnh (${out.gpsStatus.label}). Chờ tải lên.',
      );
      ref.invalidate(_evidenceProvider(widget.parentId));
    } on PhotoPipelineException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    } on ConsentRequiredException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
      ref.invalidate(_blockReasonProvider(widget.subjectRef));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final files = ref.watch(_evidenceProvider(widget.parentId));
    final block = ref.watch(_blockReasonProvider(widget.subjectRef));
    final reason = block.value;
    final needsConsent =
        reason != null &&
        reason == ConsentRequiredException(widget.subjectRef).message;
    return Scaffold(
      appBar: AppBar(title: const Text('Ảnh minh chứng')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (reason != null)
            Card(
              color: AgColors.amberBg,
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Row(
                      children: [
                        const Icon(Icons.block, color: AgColors.amber),
                        const SizedBox(width: 8),
                        Expanded(
                          child: Text(
                            reason,
                            style: const TextStyle(fontSize: 16),
                          ),
                        ),
                      ],
                    ),
                    if (needsConsent) ...[
                      const SizedBox(height: 8),
                      FilledButton.icon(
                        icon: const Icon(Icons.how_to_reg),
                        label: const Text('Ghi nhận đồng ý của nông hộ'),
                        onPressed: () async {
                          await Navigator.of(context).push(
                            MaterialPageRoute<bool>(
                              builder: (_) =>
                                  ConsentScreen(subjectRef: widget.subjectRef),
                            ),
                          );
                          ref.invalidate(
                            _blockReasonProvider(widget.subjectRef),
                          );
                        },
                      ),
                    ],
                  ],
                ),
              ),
            ),
          const SizedBox(height: 8),
          FilledButton.icon(
            onPressed: _busy || reason != null || block.isLoading
                ? null
                : _capture,
            icon: const Icon(Icons.photo_camera, size: 28),
            label: const Text('Chụp ảnh'),
          ),
          const SizedBox(height: 8),
          const Text(
            'Ảnh được đóng dấu thời gian và tọa độ GPS rồi mã hóa ngay trong bộ nhớ. '
            'Chỉ người có thẩm quyền mới mở được ảnh trên hệ thống.',
            style: TextStyle(color: AgColors.mute, fontSize: 15),
          ),
          const SizedBox(height: 16),
          files.when(
            loading: () => const LinearProgressIndicator(),
            error: (e, _) => Text('Lỗi: $e'),
            data: (list) => Column(
              children: [
                for (final f in list)
                  Card(
                    child: ListTile(
                      leading: const Icon(Icons.lock, color: AgColors.g700),
                      title: Text('Ảnh lúc ${f.meta['captured_at']}'),
                      subtitle: Text(
                        '${f.meta['gps'] == null ? 'Thiếu GPS' : 'Có GPS'} · '
                        '${(f.sizeBytes / 1024).round()} KB'
                        '${f.lastError != null ? ' · Lỗi: ${f.lastError}' : ''}',
                      ),
                      trailing: Text(
                        switch (f.status) {
                          'uploaded' => 'Đã tải lên',
                          'failed' => 'Lỗi',
                          _ => 'Chờ tải lên',
                        },
                        style: TextStyle(
                          fontWeight: FontWeight.w600,
                          color: f.status == 'uploaded'
                              ? AgColors.g700
                              : f.status == 'failed'
                              ? AgColors.red
                              : AgColors.amber,
                        ),
                      ),
                    ),
                  ),
              ],
            ),
          ),
          const SizedBox(height: 16),
          OutlinedButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('Xong'),
          ),
        ],
      ),
    );
  }
}
