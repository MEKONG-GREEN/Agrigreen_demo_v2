"use client";

import type { User } from "oidc-client-ts";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createAdminApiClient, createApiClient, unwrap, type AdminApiClient, type ApiClient } from "@/api/client";
import { mockAdminFetch, mockFetch } from "@/api/mock/handler";
import { mockKnLogin, type MockLoginResult } from "@/api/mock/kn";
import { findPersona, mockToken, type MockPersona } from "@/api/mock/personas";
import { ApiProblem } from "@/api/problem";
import type { Me } from "@/api/types";
import { config } from "@/config";
import { createKnApi } from "@/kn/client";
import { createCollectApi, createCycleApi, createKnAdminApi, supportReasonStore, type CollectApi, type CycleApi, type KnAdminApi } from "@/kn/cycle/api";
import type { KnApi, MenuBadges, MenuTree, RoleAssignment, SelectContextRequest, SessionContext } from "@/kn/contract";
import { contextStore, readSelection, writeSelection } from "@/kn/contextStore";
import { autoSelection, isValidSelection } from "@/kn/session";
import { buildPrincipal, decodeAccessToken, type AccessTokenClaims, type Principal } from "./principal";
import { getUserManager, safeReturnTo } from "./oidc";
import { tokenStore } from "./tokenStore";

export type AuthStatus = "loading" | "anonymous" | "authenticated";

/**
 * Phiên KN v7.7 (FN-CH-00.01): "legacy" = tài khoản không có vai trò KN hoặc backend chưa có P1
 * (menu cũ, đăng nhập một vai như hiện nay); "choose" = cần màn Chọn vai trò và phòng ban;
 * "active" = đã có ngữ cảnh (vai trò + phòng ban), menu theo cấu hình.
 */
export type KnStatus = "legacy" | "choose" | "active";

export interface KnSessionState {
  status: KnStatus;
  assignments: RoleAssignment[];
  context: SessionContext | null;
  menu: MenuTree | null;
  badges: MenuBadges | null;
  /** Chế độ hỗ trợ (FN-AD-04.07): đang thay cho ai — nhãn hiển thị trên dải vàng, lý do gửi kèm thao tác ghi. */
  support?: { target: string; reason: string } | null;
}

const KN_LEGACY: KnSessionState = { status: "legacy", assignments: [], context: null, menu: null, badges: null };

export interface AuthContextValue {
  status: AuthStatus;
  principal: Principal | null;
  /** /v1/me chưa có hoặc lỗi → phạm vi chưa xác định (chỉ hiện menu theo vai). */
  scopeWarning: string | null;
  /** Application API (api.<domain>). */
  api: ApiClient;
  /** Admin API (host riêng, MFA bắt buộc): danh mục, tham số, cây tổ chức, nhập nông hộ. */
  adminApi: AdminApiClient;
  /** Hợp đồng tạm KN v7.7 (ngữ cảnh phiên, menu theo cấu hình). */
  knApi: KnApi;
  /** Vòng đời báo cáo KN v7.7 (P4) — Application API. */
  cycleApi: CycleApi;
  /** Quản trị KN v7.7 (P1/P2) — Admin API. */
  knAdminApi: KnAdminApi;
  /** Thu thập dữ liệu Cấp Xã KN v7.7 (P7 /v1/kn/*). */
  collectApi: CollectApi;
  kn: KnSessionState;
  /** FN-AD-04.07: vào menu vai trò khác với ngữ cảnh hỗ trợ (POST /v1/accounts/support-sessions). */
  enterSupport: (ctx: SessionContext, target: string, reason: string) => Promise<void>;
  /** Thoát chế độ hỗ trợ → về ngữ cảnh Admin. */
  exitSupport: () => Promise<void>;
  /** Sửa lý do của các thao tác ghi tiếp theo trong chế độ hỗ trợ. */
  setSupportReason: (reason: string) => void;
  /** Chọn/đổi vai trò + phòng ban (không cần đăng nhập lại). Trả ngữ cảnh mới (default_route). */
  selectContext: (sel: SelectContextRequest) => Promise<SessionContext>;
  /** Chuyển hướng tới Keycloak; `loginHint` = email/SĐT người dùng gõ ở thẻ Đăng nhập. */
  login: (returnTo?: string, loginHint?: string) => Promise<void>;
  /** Chỉ chế độ giả lập: đăng nhập bằng email/SĐT + mật khẩu thử (tài khoản KN v7.7). */
  loginWithPassword: (identifier: string, password: string) => Promise<MockLoginResult>;
  /** Chỉ chế độ giả lập: đăng nhập bằng persona. */
  loginAsPersona: (personaId: string) => Promise<void>;
  /** Đổi mật khẩu: Keycloak kc_action=UPDATE_PASSWORD (chế độ giả lập: không làm gì). */
  changePassword: () => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

const MOCK_KEY = "ag.mock.persona";

function readMockPersona(): string | null {
  try {
    return window.sessionStorage.getItem(MOCK_KEY);
  } catch {
    return null;
  }
}

function writeMockPersona(id: string | null): void {
  try {
    if (id) window.sessionStorage.setItem(MOCK_KEY, id);
    else window.sessionStorage.removeItem(MOCK_KEY);
  } catch {
    // bỏ qua: chỉ mất khả năng nhớ persona khi tải lại
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>("loading");
  const [principal, setPrincipal] = useState<Principal | null>(null);
  const [scopeWarning, setScopeWarning] = useState<string | null>(null);
  const [kn, setKn] = useState<KnSessionState>(KN_LEGACY);
  const claimsRef = useRef<AccessTokenClaims>({});

  const onUnauthenticated = useCallback(() => {
    tokenStore.set(null);
    contextStore.set(null);
    setPrincipal(null);
    setKn(KN_LEGACY);
    setStatus("anonymous");
  }, []);

  const clients = useMemo(() => {
    const getAccessToken = tokenStore.get;
    const getContextId = contextStore.get;
    return {
      api: createApiClient({ baseUrl: config.apiBaseUrl, getAccessToken, getContextId, onUnauthenticated, fetch: config.mock ? mockFetch : undefined }),
      adminApi: createAdminApiClient({
        baseUrl: config.adminApiBaseUrl,
        getAccessToken,
        getContextId,
        onUnauthenticated,
        fetch: config.mock ? mockAdminFetch : undefined,
      }),
      knApi: createKnApi({ baseUrl: config.apiBaseUrl, getAccessToken, getContextId, fetch: config.mock ? mockFetch : undefined }),
      cycleApi: createCycleApi({ baseUrl: config.apiBaseUrl, getAccessToken, getContextId, fetch: config.mock ? mockFetch : undefined }),
      collectApi: createCollectApi({ baseUrl: config.apiBaseUrl, getAccessToken, getContextId, fetch: config.mock ? mockFetch : undefined }),
      knAdminApi: createKnAdminApi({ baseUrl: config.adminApiBaseUrl, getAccessToken, getContextId, fetch: config.mock ? mockAdminFetch : undefined }),
    };
  }, [onUnauthenticated]);

  /** GET /v1/me → Principal (gọi lại sau khi đổi ngữ cảnh: vai và phạm vi theo lần gán đã chọn). */
  const loadPrincipal = useCallback(async () => {
    let me: Me | null = null;
    try {
      me = await unwrap(clients.api.GET("/v1/me"));
      setScopeWarning(null);
    } catch (e) {
      const p = e instanceof ApiProblem ? e : null;
      setScopeWarning(
        `Chưa lấy được phạm vi dữ liệu (GET /v1/me${p ? `: ${p.message}${p.traceId ? ` — mã tra cứu ${p.traceId}` : ""}` : ""}). Menu chỉ hiển thị theo vai; dữ liệu vẫn được máy chủ lọc theo phạm vi.`,
      );
    }
    const p = buildPrincipal(claimsRef.current, me);
    setPrincipal(p);
    return p;
  }, [clients]);

  /** POST /v1/me/context → header X-AG-Context cho mọi lời gọi sau; tải menu + số đếm của ngữ cảnh. */
  const activate = useCallback(
    async (sel: SelectContextRequest, assignments: RoleAssignment[]): Promise<SessionContext> => {
      const ctx = await clients.knApi.selectContext(sel);
      contextStore.set(ctx.context_id);
      writeSelection(sel);
      const [menu, badges] = await Promise.all([
        clients.knApi.menu(),
        clients.knApi.badges().catch((): MenuBadges => ({ counts: {} })), // số đếm lỗi: ẩn số, không chặn menu
      ]);
      await loadPrincipal();
      setKn({ status: "active", assignments, context: ctx, menu, badges });
      return ctx;
    },
    [clients, loadPrincipal],
  );

  /** Ngữ cảnh KN sau đăng nhập. Backend chưa có P1 (404/lỗi) → giữ đăng nhập một vai như hiện nay. */
  const resolveKn = useCallback(async () => {
    if (config.surface === "legacy") return setKn(KN_LEGACY);
    let assignments: RoleAssignment[];
    try {
      assignments = (await clients.knApi.listContexts()).assignments ?? [];
    } catch {
      return setKn(KN_LEGACY);
    }
    if (!assignments.length) return setKn(KN_LEGACY);
    const stored = readSelection();
    const sel = isValidSelection(assignments, stored) ? stored : autoSelection(assignments);
    if (sel) {
      try {
        await activate(sel, assignments);
        return;
      } catch {
        writeSelection(null); // lựa chọn cũ không còn hợp lệ → cho chọn lại
      }
    }
    setKn({ status: "choose", assignments, context: null, menu: null, badges: null });
  }, [clients, activate]);

  const establish = useCallback(
    async (accessToken: string, mock?: MockPersona) => {
      tokenStore.set(accessToken);
      contextStore.set(null);
      claimsRef.current = mock
        ? { sub: mock.me.actor_ref, name: mock.displayName, amr: mock.amr, realm_access: { roles: mock.me.roles } }
        : decodeAccessToken(accessToken);
      await loadPrincipal();
      await resolveKn();
      setStatus("authenticated");
    },
    [loadPrincipal, resolveKn],
  );

  useEffect(() => {
    let cancelled = false;
    if (config.mock) {
      void (async () => {
        const persona = findPersona(readMockPersona());
        if (persona) await establish(mockToken(persona.id), persona);
        else if (!cancelled) setStatus("anonymous");
      })();
      return () => {
        cancelled = true;
      };
    }
    const um = getUserManager();
    const onLoaded = (user: User) => {
      if (!cancelled) void establish(user.access_token);
    };
    const onExpired = () => {
      if (!cancelled) onUnauthenticated();
    };
    um.events.addUserLoaded(onLoaded);
    um.events.addAccessTokenExpired(onExpired);
    um.events.addUserSignedOut(onExpired);

    // Trang callback/silent tự xử lý; nơi khác thử khôi phục phiên SSO im lặng.
    const path = window.location.pathname;
    if (!path.startsWith("/login/callback") && !path.startsWith("/login/silent")) {
      (async () => {
        const user = await um.getUser();
        if (user && !user.expired) return onLoaded(user);
        try {
          const renewed = await um.signinSilent();
          if (renewed && !cancelled) return; // userLoaded đã kích hoạt establish
        } catch {
          // không có phiên SSO → cần đăng nhập
        }
        if (!cancelled) setStatus("anonymous");
      })();
    }
    return () => {
      cancelled = true;
      um.events.removeUserLoaded(onLoaded);
      um.events.removeAccessTokenExpired(onExpired);
      um.events.removeUserSignedOut(onExpired);
    };
  }, [establish, onUnauthenticated]);

  const login = useCallback(async (returnTo?: string, loginHint?: string) => {
    if (config.mock) return;
    const hint = loginHint?.trim();
    await getUserManager().signinRedirect({
      state: { returnTo: safeReturnTo(returnTo) },
      ...(hint ? { login_hint: hint } : {}),
    });
  }, []);

  const loginAsPersona = useCallback(
    async (personaId: string) => {
      const persona = findPersona(personaId);
      if (!config.mock || !persona) return;
      writeMockPersona(persona.id);
      writeSelection(null);
      setStatus("loading");
      await establish(mockToken(persona.id), persona);
    },
    [establish],
  );

  const loginWithPassword = useCallback(
    async (identifier: string, password: string): Promise<MockLoginResult> => {
      if (!config.mock) return { ok: false, error: "invalid_credentials", remaining: 0 };
      const r = mockKnLogin(identifier, password);
      if (r.ok) await loginAsPersona(r.personaId);
      return r;
    },
    [loginAsPersona],
  );

  const selectContext = useCallback(
    (sel: SelectContextRequest) => {
      supportReasonStore.set(null);
      return activate(sel, kn.assignments);
    },
    [activate, kn.assignments],
  );

  const enterSupport = useCallback(
    async (ctx: SessionContext, target: string, reason: string) => {
      contextStore.set(ctx.context_id);
      supportReasonStore.set(reason);
      const [menu, badges] = await Promise.all([clients.knApi.menu(), clients.knApi.badges().catch((): MenuBadges => ({ counts: {} }))]);
      await loadPrincipal();
      setKn((k) => ({ status: "active", assignments: k.assignments, context: ctx, menu, badges, support: { target, reason } }));
    },
    [clients, loadPrincipal],
  );

  const exitSupport = useCallback(async () => {
    const id = kn.context?.support_session_id;
    if (id) await clients.knAdminApi.endSupport(id).catch(() => undefined);
    supportReasonStore.set(null);
    contextStore.set(null); // ngữ cảnh hỗ trợ đã hết hiệu lực — không gửi kèm khi chọn lại ngữ cảnh Admin
    const sel = readSelection() ?? autoSelection(kn.assignments);
    if (sel) await activate(sel, kn.assignments);
  }, [clients, kn.context, kn.assignments, activate]);

  const setSupportReason = useCallback((reason: string) => {
    supportReasonStore.set(reason);
    setKn((k) => (k.support ? { ...k, support: { ...k.support, reason } } : k));
  }, []);

  const changePassword = useCallback(async () => {
    if (config.mock) return;
    await getUserManager().signinRedirect({ extraQueryParams: { kc_action: "UPDATE_PASSWORD" }, state: { returnTo: safeReturnTo(window.location.pathname) } });
  }, []);

  const logout = useCallback(async () => {
    supportReasonStore.set(null);
    tokenStore.set(null);
    contextStore.set(null);
    writeSelection(null);
    setPrincipal(null);
    setKn(KN_LEGACY);
    setStatus("anonymous");
    if (config.mock) {
      writeMockPersona(null);
      return;
    }
    const um = getUserManager();
    const user = await um.getUser();
    await um.removeUser();
    await um.signoutRedirect(user?.id_token ? { id_token_hint: user.id_token } : undefined);
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      status,
      principal,
      scopeWarning,
      api: clients.api,
      adminApi: clients.adminApi,
      knApi: clients.knApi,
      cycleApi: clients.cycleApi,
      knAdminApi: clients.knAdminApi,
      collectApi: clients.collectApi,
      kn,
      enterSupport,
      exitSupport,
      setSupportReason,
      selectContext,
      login,
      loginWithPassword,
      loginAsPersona,
      changePassword,
      logout,
    }),
    [status, principal, scopeWarning, clients, kn, enterSupport, exitSupport, setSupportReason, selectContext, login, loginWithPassword, loginAsPersona, changePassword, logout],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth phải nằm trong <AuthProvider>");
  return ctx;
}

/** Cho kiểm thử/Storybook: cung cấp context cố định. */
export const AuthContextForTests = AuthContext;
