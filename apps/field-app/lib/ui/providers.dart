import 'dart:async';

import 'package:agrigreen_mobile/application/auth_flow.dart';
import 'package:agrigreen_mobile/application/push_coordinator.dart';
import 'package:agrigreen_mobile/data/auth/oidc_client.dart';
import 'package:agrigreen_mobile/data/auth/pin_service.dart';
import 'package:agrigreen_mobile/data/auth/session_service.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../application/app_services.dart';
import '../application/kn_context_service.dart';
import '../domain/kn_context.dart';

/// Ghi đè trong `main.dart` (thiết bị) hoặc trong kiểm thử widget.
final platformProvider = Provider<AppPlatform>(
  (ref) => throw UnimplementedError('platformProvider'),
);
final oidcProvider = Provider<OidcClient>(
  (ref) => throw UnimplementedError('oidcProvider'),
);

final sessionServiceProvider = Provider<SessionService>((ref) {
  final pf = ref.watch(platformProvider);
  return SessionService(
    store: pf.store,
    clock: pf.clock,
    oidc: ref.watch(oidcProvider),
    offlineMax: pf.config.offlineSessionMax,
  );
});

final pinServiceProvider = Provider<PinService>((ref) {
  final pf = ref.watch(platformProvider);
  return PinService(
    store: pf.store,
    clock: pf.clock,
    maxAttempts: pf.config.pinMaxAttempts,
    lockout: pf.config.pinLockout,
    hardLimit: pf.config.pinHardLimit,
  );
});

/// Kênh đẩy sống suốt tiến trình app (kể cả khi đang khóa).
final pushCoordinatorProvider = Provider<PushCoordinator>((ref) {
  final pf = ref.watch(platformProvider);
  final c = PushCoordinator(
    messaging: pf.push,
    notifier: pf.notifier,
    isForeground: () =>
        WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed,
  );
  ref.onDispose(c.dispose);
  return c;
});

/// Đường dẫn trong app chờ mở (từ thông báo được chạm). HomeScreen tiêu thụ.
class PendingDeepLink extends Notifier<String?> {
  @override
  String? build() => null;
  void set(String? link) => state = link;
  String? take() {
    final v = state;
    state = null;
    return v;
  }
}

final pendingDeepLinkProvider = NotifierProvider<PendingDeepLink, String?>(
  PendingDeepLink.new,
);

final authFlowProvider = Provider<AuthFlow>(
  (ref) => AuthFlow(
    session: ref.watch(sessionServiceProvider),
    pins: ref.watch(pinServiceProvider),
  ),
);

class AppState {
  const AppState(this.view, {this.services, this.busy = false, this.kn});
  final AuthView view;
  final AppServices? services;
  final bool busy;

  /// Ngữ cảnh phiên KN v7.7 (null = chưa xác định / không phải tài khoản KN).
  final KnContextState? kn;

  AppState copyWith({
    AuthView? view,
    AppServices? services,
    bool? busy,
    KnContextState? kn,
  }) => AppState(
    view ?? this.view,
    services: services ?? this.services,
    busy: busy ?? this.busy,
    kn: kn ?? this.kn,
  );
}

class AppController extends Notifier<AppState> {
  StreamSubscription<List<ConnectivityResult>>? _conn;
  StreamSubscription<String?>? _taps;
  Timer? _timer;

  @override
  AppState build() {
    ref.onDispose(_stopAutoSync);
    ref.onDispose(() => unawaited(_knSub?.cancel()));
    ref.onDispose(() => unawaited(_taps?.cancel()));
    Future.microtask(_init);
    return const AppState(AuthView(AuthStage.loading));
  }

  AuthFlow get _flow => ref.read(authFlowProvider);
  PushCoordinator get _push => ref.read(pushCoordinatorProvider);

  Future<void> _init() async {
    final push = _push;
    _taps = push.taps.listen(
      (link) => ref
          .read(pendingDeepLinkProvider.notifier)
          .set(link ?? '/notifications'),
    );
    try {
      await push.start();
      final launched = await push.launchPayload();
      if (launched != null) {
        ref.read(pendingDeepLinkProvider.notifier).set(launched);
      }
    } catch (_) {
      // Kênh đẩy lỗi không chặn app (hộp thư vẫn đồng bộ qua pull).
    }
    await _apply(await _flow.start());
  }

  Future<void> _apply(AuthView v) async {
    if (v.stage == AuthStage.unlocked) {
      final existing = state.services;
      final services =
          existing ??
          await AppServices.open(
            ref.read(platformProvider),
            accessToken: ref.read(sessionServiceProvider).accessToken,
            onWiped: _onWiped,
          );
      // KN v7.7 P1: ngữ cảnh phiên trước lượt đồng bộ đầu (pull theo phạm vi
      // ngữ cảnh). Ngoại tuyến → ngữ cảnh lưu trên máy.
      final kn = existing != null && state.kn != null
          ? state.kn!
          : await _resolveKn(services);
      state = AppState(v, services: services, kn: kn);
      _watchKn(services);
      _startAutoSync(services);
      await _push.attach(services.pushHost);
      if (kn.stage != KnContextStage.picker) {
        unawaited(services.sync.syncOnce(force: true));
      }
    } else {
      // Ngoài trạng thái mở khóa không giữ CSDL mở.
      _stopAutoSync();
      _push.detach();
      final old = state.services;
      state = AppState(v);
      await old?.close();
    }
  }

  Future<void> _busy(Future<AuthView> Function() f) async {
    state = state.copyWith(busy: true);
    try {
      await _apply(await f());
    } finally {
      state = state.copyWith(busy: false);
    }
  }

  Future<KnContextState> _resolveKn(AppServices s) async {
    try {
      return await s.knContext.resolve();
    } on Object {
      final cur = await s.knContext.stored();
      return cur == null
          ? const KnContextState(KnContextStage.none)
          : KnContextState(KnContextStage.ready, context: cur, offline: true);
    }
  }

  StreamSubscription<KnContextState>? _knSub;

  void _watchKn(AppServices s) {
    unawaited(_knSub?.cancel());
    _knSub = s.knContext.changes.listen((k) {
      if (state.services == s) state = state.copyWith(kn: k);
    });
  }

  /// Chọn (vai trò, phòng ban) ở màn chọn hoặc "Đổi vai trò/phòng ban" —
  /// không đăng nhập lại. Phạm vi đổi → dữ liệu KN đã pull bị thay (máy chủ
  /// trả `reset: true`), nên đồng bộ đầy đủ ngay.
  Future<void> chooseContext(KnContextChoice c) async {
    final s = state.services;
    if (s == null) return;
    state = state.copyWith(busy: true);
    try {
      final ctx = await s.knContext.switchTo(c);
      state = AppState(
        state.view,
        services: s,
        kn: KnContextState(
          KnContextStage.ready,
          context: ctx,
          choices: state.kn?.choices ?? const [],
        ),
      );
      unawaited(s.sync.syncOnce(force: true));
    } finally {
      state = state.copyWith(busy: false);
    }
  }

  /// Mở màn chọn để đổi vai trò/phòng ban (cần mạng để lấy danh sách mới;
  /// ngoại tuyến dùng danh sách đã lưu).
  Future<List<KnContextChoice>> contextChoices() async {
    final s = state.services;
    if (s == null) return const [];
    try {
      return await s.knContext.fetchChoices();
    } on Object {
      return s.knContext.storedChoices();
    }
  }

  Future<void> loginOnline() => _busy(_flow.loginOnline);
  Future<void> setPin(String pin, String confirm) =>
      _busy(() => _flow.setPin(pin, confirm));
  Future<void> unlock(String pin) => _busy(() => _flow.unlock(pin));

  /// Khóa màn hình: đóng CSDL, về màn PIN.
  Future<void> lock() async {
    _stopAutoSync();
    _push.detach();
    await state.services?.close();
    state = const AppState(AuthView(AuthStage.locked));
  }

  void _onWiped() {
    _stopAutoSync();
    _push.detach();
    state = const AppState(
      AuthView(
        AuthStage.needsOnlineLogin,
        message: 'Thiết bị đã bị thu hồi. Dữ liệu trên máy đã được xóa. Liên hệ quản trị để được cấp lại.',
      ),
    );
  }

  void _startAutoSync(AppServices s) {
    _stopAutoSync();
    // Có mạng trở lại → đồng bộ ngay (HTX US-LOGIN-02 AC-3).
    _conn = Connectivity().onConnectivityChanged.listen((r) {
      if (r.any((c) => c != ConnectivityResult.none)) {
        unawaited(s.sync.syncOnce(force: true));
      }
    });
    // Thử lại theo chu kỳ; SyncService tự tôn trọng thời điểm lùi.
    _timer = Timer.periodic(
      const Duration(minutes: 2),
      (_) => unawaited(s.sync.syncOnce()),
    );
  }

  void _stopAutoSync() {
    unawaited(_conn?.cancel());
    _conn = null;
    _timer?.cancel();
    _timer = null;
  }
}

final appControllerProvider = NotifierProvider<AppController, AppState>(
  AppController.new,
);

/// Dịch vụ đã mở khóa, dùng trong callback của widget.
extension WidgetServices on WidgetRef {
  AppServices get services {
    final s = read(appControllerProvider).services;
    if (s == null) throw StateError('Chưa mở khóa');
    return s;
  }
}

AppServices servicesOf(Ref ref) {
  final s = ref.watch(appControllerProvider).services;
  if (s == null) throw StateError('Chưa mở khóa');
  return s;
}

class HomeSummary {
  const HomeSummary({
    required this.counts,
    required this.status,
    required this.openTasks,
    required this.plots,
    required this.pendingFiles,
    this.unreadNotifications = 0,
    this.unackedDirectives = 0,
    this.pendingActions = 0,
    this.boundaryAttention = 0,
  });
  final OutboxCounts counts;
  final SyncStatus status;
  final int openTasks;
  final int plots;
  final int pendingFiles;
  final int unreadNotifications;
  final int unackedDirectives;
  final int pendingActions;

  /// Ranh giới xung đột/bị từ chối chưa xử lý.
  final int boundaryAttention;
}

/// Làm mới khi trạng thái đồng bộ đổi.
final syncStatusStreamProvider = StreamProvider<SyncStatus>((ref) {
  final s = servicesOf(ref);
  return s.sync.statusStream;
});

final homeSummaryProvider = FutureProvider<HomeSummary>((ref) async {
  ref.watch(syncStatusStreamProvider);
  final s = servicesOf(ref);
  return HomeSummary(
    counts: await s.outbox.counts(),
    status: await s.sync.status(),
    openTasks: await s.tasks.openCount(),
    plots: await s.refs.count(RefEntity.plot),
    pendingFiles: (await s.evidence.pending()).length,
    unreadNotifications: await s.inbox.unreadCount(),
    unackedDirectives: await s.directives.unackedCount(),
    pendingActions: await s.actions.pendingCount(),
    boundaryAttention: (await s.boundaries.needingAttention()).length,
  );
});
