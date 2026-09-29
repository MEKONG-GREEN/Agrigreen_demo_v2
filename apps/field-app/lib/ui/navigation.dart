import 'package:agrigreen_mobile/domain/deep_link.dart';
import 'package:flutter/material.dart';

import 'screens/directives_screen.dart';
import 'screens/library_screen.dart';
import 'screens/notifications_screen.dart';
import 'screens/sync_queue_screen.dart';
import 'screens/tasks_screen.dart';

/// Màn hình cho một đường dẫn trong app (từ thông báo).
Widget screenForDeepLink(DeepLink link) => switch (link) {
  DirectiveLink(:final id) => DirectiveDetailScreen(directiveId: id),
  TaskLink(:final id) => TasksScreen(highlightTaskId: id),
  DocumentLink(:final ref) => DocumentScreen(documentRef: ref),
  ConflictLink() => const SyncQueueScreen(),
  InboxLink() => const NotificationsScreen(),
};

Future<void> openDeepLink(BuildContext context, String? raw) =>
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => screenForDeepLink(DeepLink.parse(raw)),
      ),
    );
