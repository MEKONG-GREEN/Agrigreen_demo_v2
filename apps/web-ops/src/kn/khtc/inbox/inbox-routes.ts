export const INBOX_BASE = "/kn/bao-cao-khtc";
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const departments = new Set(["trong_trot", "chan_nuoi_thuy_san", "thong_tin_truyen_thong", "hanh_chinh_tong_hop", "ke_hoach_tai_chinh"]);
export type InboxRoute = { id: string; department?: string; base: string; history: boolean; view: "current" | "sent" };

export function parseInboxRoute(path: string, query: URLSearchParams): InboxRoute | null {
  const own = /^\/kn\/bao-cao-khtc\/phieu\/([^/]+)(\/lich-su)?$/.exec(path);
  const admin = /^\/kn\/phong-ban\/([^/]+)\/bao-cao-khtc\/phieu\/([^/]+)(\/lich-su)?$/.exec(path);
  if (!own && !admin) return null;
  for (const key of query.keys()) if (!["view", "department"].includes(key) || query.getAll(key).length !== 1) return null;
  const id = (admin ? admin[2] : own![1])!;
  if (!uuid.test(id) || /^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(id)) return null;
  const department = admin?.[1] || query.get("department") || undefined;
  if (department && !departments.has(department)) return null;
  if (admin && query.has("department") && query.get("department") !== department) return null;
  const view = query.get("view") || "current";
  if (view !== "current" && view !== "sent") return null;
  return { id, department, base: admin ? `/kn/phong-ban/${department}/bao-cao-khtc` : INBOX_BASE, history: !!(admin ? admin[3] : own![2]), view };
}
export function inboxHref(base: string, id: string, department: string | undefined, view: "current" | "sent", history = false): string {
  const query = new URLSearchParams({ view });
  if (department && base === INBOX_BASE) query.set("department", department);
  return `${base}/phieu/${id}${history ? "/lich-su" : ""}?${query}`;
}
export function inboxBack(route: InboxRoute): string {
  if (route.base !== INBOX_BASE) return `${route.base}?view=${route.view}`;
  return route.view === "sent" ? `${INBOX_BASE}/da-gui` : INBOX_BASE;
}
