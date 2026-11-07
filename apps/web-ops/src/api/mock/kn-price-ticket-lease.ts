// Expiring mock ownership shares the native wire contract. Only synthetic persona names are stored.
import type { components } from "../generated/application";
import type { Ctx } from "./http";
type Lease = { actor: string; name: string; handles: Record<string, number> };
const prefix = "ag.mock.price-edit-lease237:";
const memory = new Map<string, Lease>();
const key = (ctx: Ctx, ticket: string) => prefix + ctx.persona.me.tenant_id + ":" + ticket;
function save(k: string, lease: Lease | null) {
  if (lease) memory.set(k, lease); else memory.delete(k);
  try { if (lease) localStorage.setItem(k, JSON.stringify(lease)); else localStorage.removeItem(k); } catch { /* in-memory test fallback */ }
}
function read(ctx: Ctx, ticket: string): Lease | null {
  const k = key(ctx, ticket);
  let lease = memory.get(k) ?? null;
  try { const raw = localStorage.getItem(k); lease = raw ? JSON.parse(raw) as Lease : null; } catch { /* in-memory test fallback */ }
  if (!lease) return null;
  lease.handles = Object.fromEntries(Object.entries(lease.handles).filter(([, until]) => until > Date.now()));
  if (!Object.keys(lease.handles).length) { save(k, null); return null; }
  return lease;
}
export function mockLeaseState(ctx: Ctx, ticket: string, handle: string | null = null): components["schemas"]["PriceEditLease"] {
  const lease = read(ctx, ticket), mine = lease?.actor === ctx.persona.me.actor_ref;
  return { held: !!lease, mine, holder_name: lease?.name ?? null, expires_at: lease ? new Date(Math.max(...Object.values(lease.handles))).toISOString() : null, handle_id: mine && handle && lease?.handles[handle] ? handle : null, now: new Date().toISOString() };
}
export function mockHasEditLease(ctx: Ctx, ticket: string, handle: string | undefined): boolean {
  const lease = read(ctx, ticket);
  return !!handle && lease?.actor === ctx.persona.me.actor_ref && (lease.handles[handle] ?? 0) > Date.now();
}
export function mockLeaseOp(ctx: Ctx, ticket: string, handle: string, action: "acquire" | "renew" | "release") {
  const current = read(ctx, ticket), actor = ctx.persona.me.actor_ref;
  if (action === "release") {
    if (current?.actor === actor) { delete current.handles[handle]; save(key(ctx, ticket), current); }
    return mockLeaseState(ctx, ticket);
  }
  if (current && current.actor !== actor) return mockLeaseState(ctx, ticket);
  const lease = current ?? { actor, name: ctx.persona.displayName, handles: {} };
  lease.handles[handle] = Date.now() + 120_000;
  save(key(ctx, ticket), lease);
  return mockLeaseState(ctx, ticket, handle);
}
export function resetPriceEditLeases() {
  memory.clear();
  try { for (const k of Object.keys(localStorage)) if (k.startsWith(prefix)) localStorage.removeItem(k); } catch { /* no browser storage */ }
}
