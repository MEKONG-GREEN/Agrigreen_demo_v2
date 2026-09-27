"use client";

import type { User } from "oidc-client-ts";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { createApiClient, unwrap, type ApiClient } from "@/api/client";
import { mockFetch } from "@/api/mock/handler";
import { findPersona, mockToken } from "@/api/mock/personas";
import type { Me } from "@/api/pending/party";
import { config } from "@/config";
import { buildPrincipal, decodeAccessToken, type Principal } from "./principal";
import { getUserManager, safeReturnTo } from "./oidc";
import { tokenStore } from "./tokenStore";

export type AuthStatus = "loading" | "anonymous" | "authenticated";

export interface AuthContextValue {
  status: AuthStatus;
  principal: Principal | null;
  /** /v1/me chưa có hoặc lỗi → phạm vi chưa xác định (chỉ hiện menu theo vai). */
  scopeWarning: string | null;
  api: ApiClient;
  adminApi: ApiClient;
  login: (returnTo?: string) => Promise<void>;
  /** Chỉ chế độ giả lập: đăng nhập bằng persona. */
  loginAsPersona: (personaId: string) => Promise<void>;
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

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>("loading");
  const [principal, setPrincipal] = useState<Principal | null>(null);
  const [scopeWarning, setScopeWarning] = useState<string | null>(null);

  const onUnauthenticated = useCallback(() => {
    tokenStore.set(null);
    setPrincipal(null);
    setStatus("anonymous");
  }, []);

  const clients = useMemo(() => {
    const getAccessToken = tokenStore.get;
    const fetchImpl = config.mock ? mockFetch : undefined;
    return {
      api: createApiClient({ baseUrl: config.apiBaseUrl, getAccessToken, onUnauthenticated, fetch: fetchImpl }),
      adminApi: createApiClient({ baseUrl: config.adminApiBaseUrl, getAccessToken, onUnauthenticated, fetch: fetchImpl }),
    };
  }, [onUnauthenticated]);

  const establish = useCallback(
    async (accessToken: string, mockAmr?: string[]) => {
      tokenStore.set(accessToken);
      let me: Me | null = null;
      try {
        me = await unwrap(clients.api.GET("/v1/me"));
        setScopeWarning(null);
      } catch {
        setScopeWarning("Chưa lấy được phạm vi dữ liệu (/v1/me). Menu chỉ hiển thị theo vai.");
      }
      const claims = config.mock ? { sub: me?.subject, amr: mockAmr, realm_access: { roles: me?.roles ?? [] } } : decodeAccessToken(accessToken);
      setPrincipal(buildPrincipal(claims, me));
      setStatus("authenticated");
    },
    [clients],
  );

  useEffect(() => {
    let cancelled = false;
    if (config.mock) {
      void (async () => {
        const persona = findPersona(readMockPersona());
        if (persona) await establish(mockToken(persona.id), persona.amr);
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

  const login = useCallback(async (returnTo?: string) => {
    if (config.mock) return;
    await getUserManager().signinRedirect({ state: { returnTo: safeReturnTo(returnTo) } });
  }, []);

  const loginAsPersona = useCallback(
    async (personaId: string) => {
      const persona = findPersona(personaId);
      if (!config.mock || !persona) return;
      try {
        window.sessionStorage.setItem(MOCK_KEY, persona.id);
      } catch {
        // bỏ qua: chỉ mất khả năng nhớ persona khi tải lại
      }
      setStatus("loading");
      await establish(mockToken(persona.id), persona.amr);
    },
    [establish],
  );

  const logout = useCallback(async () => {
    tokenStore.set(null);
    setPrincipal(null);
    setStatus("anonymous");
    if (config.mock) {
      try {
        window.sessionStorage.removeItem(MOCK_KEY);
      } catch {
        // bỏ qua
      }
      return;
    }
    const um = getUserManager();
    const user = await um.getUser();
    await um.removeUser();
    await um.signoutRedirect(user?.id_token ? { id_token_hint: user.id_token } : undefined);
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({ status, principal, scopeWarning, api: clients.api, adminApi: clients.adminApi, login, loginAsPersona, logout }),
    [status, principal, scopeWarning, clients, login, loginAsPersona, logout],
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
