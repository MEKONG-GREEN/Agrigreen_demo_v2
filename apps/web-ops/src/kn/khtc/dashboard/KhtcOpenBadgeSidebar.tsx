"use client";

import { useEffect, useState, type ComponentProps } from "react";
import { unwrap, type ApiClient } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import type { MenuBadges } from "@/kn/contract";
import { Sidebar } from "@/shell/Sidebar";
import { menuLinks } from "@/shell/menu";

export const KHTC_OPEN_BADGE = "extension.khtc_periods.open";
type Props = ComponentProps<typeof Sidebar>;

function badges(original: MenuBadges | null | undefined, count: number | null): MenuBadges {
  const counts = { ...original?.counts };
  delete counts[KHTC_OPEN_BADGE];
  if (count !== null) counts[KHTC_OPEN_BADGE] = count;
  return { counts };
}

// The current menu advertises eligibility; the server checks the selected
// session again. A committed authority/route change unmounts the old request.
export function KhtcOpenBadgeSidebar(props: Props) {
  const auth = useAuth();
  const expiresAt = Date.parse(auth.kn?.context?.expires_at ?? "");
  const eligible = Number.isFinite(expiresAt) && auth.status === "authenticated" && !!auth.principal && auth.kn?.status === "active" && !!auth.kn.context && !!auth.api
    && menuLinks(props.menu).some(({ node }) => node.badge_source === KHTC_OPEN_BADGE);
  if (!eligible) return <Sidebar {...props} badges={badges(props.badges, null)} />;
  const identity = JSON.stringify([auth.status, auth.principal, auth.kn.status, auth.kn.context, auth.kn.assignments, auth.kn.support, auth.scopeWarning,
    props.pathname, props.menu.version, props.menu.role, props.menu.department_code]);
  return <ClientBoundary key={identity} api={auth.api} expiresAt={expiresAt} sidebar={props} />;
}

function ClientBoundary({ api, expiresAt, sidebar }: { api: ApiClient; expiresAt: number; sidebar: Props }) {
  const [client, setClient] = useState({ api, epoch: 0 });
  if (client.api !== api) { setClient({ api, epoch: client.epoch + 1 }); return null; }
  return <CountResource key={client.epoch} api={api} expiresAt={expiresAt} sidebar={sidebar} />;
}

async function readCount(api: ApiClient, signal: AbortSignal): Promise<number> {
  const value = await unwrap(api.GET("/v1/extension/khtc/open-period-count", { signal, cache: "no-store" }));
  if (!Number.isSafeInteger(value.open_count) || value.open_count < 0) throw new Error("Số kỳ mở không hợp lệ");
  return value.open_count;
}

function CountResource({ api, expiresAt, sidebar }: { api: ApiClient; expiresAt: number; sidebar: Props }) {
  const [count, setCount] = useState<number | null>(null);
  useEffect(() => {
    if (expiresAt <= Date.now()) return;
    let active = true;
    let sequence = 0;
    let pending: AbortController | null = null;
    const refresh = () => {
      if (Date.now() >= expiresAt) return;
      const current = ++sequence;
      pending?.abort();
      pending = new AbortController();
      void readCount(api, pending.signal).then(value => {
        if (active && current === sequence) setCount(Date.now() < expiresAt ? value : null);
      }, () => {
        if (active && current === sequence) setCount(null);
      });
    };
    let expiryTimer: number | undefined;
    const expire = () => {
      const remaining = expiresAt - Date.now();
      if (remaining <= 0) { ++sequence; pending?.abort(); setCount(null); return; }
      expiryTimer = window.setTimeout(expire, Math.min(remaining, 2_147_483_647));
    };
    expiryTimer = window.setTimeout(expire, Math.min(Math.max(0, expiresAt - Date.now()), 2_147_483_647));
    refresh();
    const interval = window.setInterval(() => { if (document.visibilityState === "visible") refresh(); }, 30_000);
    const visibility = () => { if (document.visibilityState === "visible") refresh(); };
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", visibility);
    return () => {
      active = false;
      ++sequence;
      pending?.abort();
      window.clearInterval(interval);
      window.clearTimeout(expiryTimer);
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", visibility);
    };
  }, [api, expiresAt]);
  return <Sidebar {...sidebar} badges={badges(sidebar.badges, count)} />;
}
