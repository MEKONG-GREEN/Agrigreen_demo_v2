import 'dart:async';

import 'package:agrigreen_mobile/application/auth_flow.dart';
import 'package:agrigreen_mobile/application/push_coordinator.dart';
import 'package:agrigreen_mobile/data/auth/oidc_client.dart';
import 'package:agrigreen_mobile/data/auth/pin_service.dart';
import 'package:agrigreen_mobile/data/auth/session_service.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../application/activation_service.dart';
import '../application/app_services.dart';
import '../domain/cropcycle.dart';
import '../domain/roles.dart';

/// Ghi đè trong `main.dart` (thiết bị) hoặc trong kiểm thử widget.
final platformProvider = Provider<HtxPlatform>(
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

/// Kích hoạt tài khoản — dùng được trước khi đăng nhập.
final activationProvider = Provider<ActivationService>((ref) {
  final pf = ref.watch(platformProvider);
  return ActivationService(oidcIssuer: pf.config.oidcIssuer, opener: pf.dialer);
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

final authFlowProvider = Provider<AuthFlow>(
  (ref) => AuthFlow(
    session: ref.watch(sessionServiceProvider),
    pins: ref.watch(pinServiceProvider),
  ),
);

class AppState {
  const AppState(
    this.view, {
    this.services,
    this.busy = false,
    this.mode = HtxMode.none,
  });
  final AuthView view;
  final HtxServices? services;
  final bool busy;

  /// Trang chủ theo vai (HTX US-LOGIN-01 AC-1).
  final HtxMode mode;

  AppState copyWith({bool? busy, HtxMode? mode}) => AppState(
    view,
    services: services,
    busy: busy ?? this.busy,
    mode: mode ?? this.mode,
  );
}

class AppController extends Notifier<AppState> {
  StreamSubscription<bool>? _conn;
  StreamSubscription<SyncStatus>? _status;
  Timer? _timer;

  @override
  AppState build() {
    ref.onDispose(_stopAutoSync);
    Future.microtask(_init);
    return const AppState(AuthView(AuthStage.loading));
  }

  AuthFlow get _flow => ref.read(authFlowProvider);
  PushCoordinator get _push => ref.read(pushCoordinatorProvider);

  Future<void> _init() async {
    try {
      await _push.start();
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
          await HtxServices.open(
            ref.read(platformProvider),
            accessToken: ref.read(sessionServiceProvider).accessToken,
            onWiped: _onWiped,
          );
      state = AppState(
        v,
        services: services,
        mode: await services.profile.mode(),
      );
      _startAutoSync(services);
      await _push.attach(services.pushHost);
      unawaited(services.sync.syncOnce(force: true));
    } else {
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

  /// Đọc lại vai sau lượt đồng bộ (`/v1/me` đã về).
  Future<void> refreshMode() async {
    final s = state.services;
    if (s == null) return;
    final m = await s.profile.mode();
    if (m != state.mode) state = state.copyWith(mode: m);
  }

  void _onWiped() {
    _stopAutoSync();
    _push.detach();
    state = const AppState(
      AuthView(
        AuthStage.needsOnlineLogin,
        message: 'Thiết bị đã bị thu hồi. Dữ liệu trên máy đã được xóa. Liên hệ quản lý HTX để được cấp lại.',
      ),
    );
  }

  void _startAutoSync(HtxServices s) {
    _stopAutoSync();
    _status = s.sync.statusStream.listen((st) {
      if (st.state == SyncState.idle) unawaited(refreshMode());
    });
    final pf = s.platform;
    // Có mạng trở lại → đồng bộ ngay (HTX US-LOGIN-02 AC-3).
    _conn = pf.onlineChanges?.call().listen((online) {
      if (online) unawaited(s.sync.syncOnce(force: true));
    });
    if (!pf.autoSync) return;
    // Thử lại theo chu kỳ; SyncService tự tôn trọng thời điểm lùi (US-LOG-04 AC-2).
    _timer = Timer.periodic(
      const Duration(minutes: 2),
      (_) => unawaited(s.sync.syncOnce()),
    );
  }

  void _stopAutoSync() {
    unawaited(_conn?.cancel());
    unawaited(_status?.cancel());
    _conn = null;
    _status = null;
    _timer?.cancel();
    _timer = null;
  }
}

final appControllerProvider = NotifierProvider<AppController, AppState>(
  AppController.new,
);

/// Dịch vụ đã mở khóa, dùng trong callback của widget.
extension WidgetServices on WidgetRef {
  HtxServices get services {
    final s = read(appControllerProvider).services;
    if (s == null) throw StateError('Chưa mở khóa');
    return s;
  }
}

HtxServices servicesOf(Ref ref) {
  final s = ref.watch(appControllerProvider).services;
  if (s == null) throw StateError('Chưa mở khóa');
  return s;
}

/// Làm mới khi trạng thái đồng bộ đổi.
final syncStatusStreamProvider = StreamProvider<SyncStatus>((ref) {
  final s = servicesOf(ref);
  return s.sync.statusStream;
});

class HomeSummary {
  const HomeSummary({
    required this.counts,
    required this.status,
    required this.pendingFiles,
    required this.pendingActions,
    required this.plots,
    required this.openSeasons,
    this.advice,
    this.needsSupplement = 0,
    this.reviewPending = 0,
  });
  final OutboxCounts counts;
  final SyncStatus status;
  final int pendingFiles;
  final int pendingActions;
  final int plots;
  final int openSeasons;

  /// Thẻ lời khuyên theo bước SOP hiện tại; null → ẩn (HTX US-WEATHER-02 AC-2).
  final (Season, SopStep)? advice;

  /// Nhật ký bị yêu cầu bổ sung (nông dân).
  final int needsSupplement;

  /// Nhật ký chờ duyệt (quản lý).
  final int reviewPending;

  int get waiting => counts.pending + pendingFiles + pendingActions;
}

final homeSummaryProvider = FutureProvider<HomeSummary>((ref) async {
  ref.watch(syncStatusStreamProvider);
  final s = servicesOf(ref);
  final mode = ref.watch(appControllerProvider.select((a) => a.mode));
  final seasons = await s.crop.seasons();
  return HomeSummary(
    counts: await s.outbox.counts(),
    status: await s.sync.status(),
    pendingFiles: (await s.evidence.pending()).length,
    pendingActions: await s.actions.pendingCount(),
    plots: (await s.crop.plots()).length,
    openSeasons: seasons.where((x) => x.isOpen).length,
    advice: await s.crop.advice(),
    needsSupplement: mode == HtxMode.farmer
        ? (await s.crop.needingSupplement()).length
        : 0,
    reviewPending: mode == HtxMode.manager
        ? (await s.manager.reviewQueue())
              .where((r) => r.decision == null)
              .length
        : 0,
  );
});
