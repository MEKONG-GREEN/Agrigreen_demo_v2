"use client";

import type { User } from "oidc-client-ts";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockAdminFetch, mockFetch } from "@/api/mock/handler";
import { findPersona, MKG_TENANT, mockToken, type MockPersona } from "@/api/mock/personas";
import { config } from "@/config";
import { buildPrincipal, decodeAccessToken, type Principal } from "./principal";
import { getUserManager, safeReturnTo } from "./oidc";
import { tokenStore } from "./tokenStore";

export type AuthStatus = "loading" | "anonymous" | "authenticated";

export interface AuthContextValue {
  status: AuthStatus;
  principal: Principal | null;
  /** Bề mặt ứng dụng của api-internal. */
  api: ApiClient;
  /** Bề mặt quản trị của api-internal (host riêng, MFA bắt buộc). */
  adminApi: ApiClient;
  login: (returnTo?: string) => Promise<void>;
  /** Chỉ chế độ giả lập: đăng nhập bằng persona. */
  loginAsPersona: (personaId: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export const MOCK_KEY = "agi.mock.persona";

function readMockPersona(): string | null {
  try {
    return window.sessionStorage.getItem(MOCK_KEY);
  } catch {
    return null;
  }
}

function mockClaims(p: MockPersona) {
  return { sub: p.actor, name: p.displayName, tenant_id: MKG_TENANT, amr: p.amr, realm_access: { roles: p.roles } };
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>("loading");
  const [principal, setPrincipal] = useState<Principal | null>(null);

  const onUnauthenticated = useCallback(() => {
    tokenStore.set(null);
    setPrincipal(null);
    setStatus("anonymous");
  }, []);

  const clients = useMemo(() => {
    const getAccessToken = tokenStore.get;
    return {
      api: createApiClient({ baseUrl: config.apiBaseUrl, getAccessToken, onUnauthenticated, fetch: config.mock ? mockFetch : undefined }),
      adminApi: createApiClient({ baseUrl: config.adminApiBaseUrl, getAccessToken, onUnauthenticated, fetch: config.mock ? mockAdminFetch : undefined }),
    };
  }, [onUnauthenticated]);

  const establish = useCallback((accessToken: string, mock?: MockPersona) => {
    tokenStore.set(accessToken);
    setPrincipal(buildPrincipal(mock ? mockClaims(mock) : decodeAccessToken(accessToken)));
    setStatus("authenticated");
  }, []);

  useEffect(() => {
    let cancelled = false;
    if (config.mock) {
      const persona = findPersona(readMockPersona());
      queueMicrotask(() => {
        if (cancelled) return;
        if (persona) establish(mockToken(persona.id), persona);
        else setStatus("anonymous");
      });
      return () => {
        cancelled = true;
      };
    }
    const um = getUserManager();
    const onLoaded = (user: User) => {
      if (!cancelled) establish(user.access_token);
    };
    const onExpired = () => {
      if (!cancelled) onUnauthenticated();
    };
    um.events.addUserLoaded(onLoaded);
    um.events.addAccessTokenExpired(onExpired);
    um.events.addUserSignedOut(onExpired);
    const path = window.location.pathname;
    if (!path.startsWith("/login/callback") && !path.startsWith("/login/silent")) {
      (async () => {
        const user = await um.getUser();
        if (user && !user.expired) return onLoaded(user);
        try {
          const renewed = await um.signinSilent();
          if (renewed && !cancelled) return;
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
        // bỏ qua
      }
      establish(mockToken(persona.id), persona);
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
    () => ({ status, principal, api: clients.api, adminApi: clients.adminApi, login, loginAsPersona, logout }),
    [status, principal, clients, login, loginAsPersona, logout],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth phải nằm trong <AuthProvider>");
  return ctx;
}
