import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../data/files/evidence_repository.dart';
import '../../data/photo/photo_pipeline.dart';
import '../providers.dart';
import '../theme.dart';
import '../widgets/common.dart';

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
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final files = ref.watch(_evidenceProvider(widget.parentId));
    return Scaffold(
      appBar: AppBar(title: const Text('Ảnh minh chứng')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          FilledButton.icon(
            onPressed: _busy ? null : _capture,
            icon: const Icon(Icons.photo_camera, size: 28),
            label: const Text('Chụp ảnh'),
          ),
          const SizedBox(height: 8),
          const Text(
            'Ảnh gắn mốc thời gian và tọa độ GPS, được mã hóa ngay trên máy. '
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
