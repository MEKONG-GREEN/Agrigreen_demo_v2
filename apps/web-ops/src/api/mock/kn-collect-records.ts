// ============================================================================
// BACKEND GIẢ LẬP hồ sơ KN riêng HTX / Nông hộ / Tổ hợp tác (KNSPLIT-2-KN-RECORDS #67, application.yaml
// /v1/kn/producer-records, /v1/kn/producer-htx-candidates). Quy tắc như Go knrecords ở mức màn web cần: phạm vi xã của
// ngữ cảnh (Cấp Xã ghi trong xã phụ trách; Cấp Tỉnh, Quốc gia chỉ xem), bắt buộc theo TT-08, SĐT che, trùng tên/chủ hộ
// 409, khóa lạc quan base_version, vô hiệu hóa thay xóa (lý do 10–1000), diện tích thập phân thường. KN không ghi HTX:
// HTX đã công bố (danh sách HTX giả lập của kn-collect) chỉ được đọc để liên kết htx_ref.
// ============================================================================
import { uuidv7 } from "../idempotency";
import { json, problem, type Ctx, type Route } from "./http";
import type { Caller } from "./kn-reports/store";

type Kind = "cooperative" | "household" | "coop_group";
const KIND_LABEL: Record<Kind, string> = { cooperative: "HTX", household: "Nông hộ", coop_group: "Tổ hợp tác" };
interface LogEntry {
  version: number;
  event: "created" | "updated" | "deactivated" | "reactivated";
  at: string;
  actor_ref: string;
  actor_display: string | null;
  status_reason: string | null;
}
interface Rec {
  ref: string;
  kind: Kind;
  commune_code: string;
  name: string | null;
  person: { label: string; masked_phone: string | null };
  phone: string; // chỉ giả lập (so trùng chủ hộ); không trả ra API
  parent_ref: string | null;
  htx_ref: string | null;
  area_ha: string | null;
  tax_code: string | null;
  address: string | null;
  status: "active" | "inactive";
  status_reason: string | null;
  version: number;
  updated_at: string;
  log: LogEntry[];
}
/** HTX đã công bố tối thiểu (từ danh sách HTX giả lập của kn-collect). */
export interface PublishedCoop {
  ref: string;
  name: string;
  commune_code: string;
  active: boolean;
}
type Wrap = (h: (ctx: Ctx, c: Caller) => Response) => (ctx: Ctx) => Response;

const now = () => new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
const mask = (p: string) => `${p.slice(0, 4)}•••${p.slice(-3)}`;
const shortName = (full: string) => {
  const parts = full.trim().split(/\s+/);
  return parts.length < 2 ? full : `${parts[0]} ${parts.slice(1, -1).map((x) => `${x[0]}.`).join(" ")} ${parts.at(-1)![0]}.`.replace(/\s+/g, " ");
};
const nameKey = (s: string) => s.trim().split(/\s+/).join(" ").toLowerCase();
const AREA_RE = /^\d{1,7}(\.\d{1,4})?$/;
const PII_RE = /(?:\+?84|\b0)[\s.-]?(?:\d[\s.-]?){8,9}\d\b|\b\d{12}\b/;

let RECS: Rec[] = [];
let seeded = false;

function entry(event: LogEntry["event"], version: number, reason: string | null = null): LogEntry {
  return { version, event, at: now(), actor_ref: "00000000-0000-7000-8000-00000000c004", actor_display: "Cán bộ xã (giả lập)", status_reason: reason };
}

function seed(coops: PublishedCoop[]): void {
  if (seeded) return;
  seeded = true;
  const reps = ["Nguyễn V. B.", "Trần T. C.", "Lê V. D.", "Phạm T. E.", "Võ V. G.", "Đặng T. H.", "Huỳnh V. K."];
  const at = "2026-08-20T01:30:00Z";
  const base = () => ({ status: "active" as const, status_reason: null, version: 1, updated_at: at, log: [{ ...entry("created", 1), at }] });
  // Hồ sơ HTX của KN liên kết đối chiếu HTX đã công bố (htx_ref) — dữ liệu demo liên tục với màn Máy móc/Thửa.
  const htx: Rec[] = coops.map((c, i) => ({
    ref: `01927b3e-0000-7000-8000-0000000e${String(i + 1).padStart(4, "0")}`,
    kind: "cooperative",
    commune_code: c.commune_code,
    name: c.name,
    person: { label: reps[i % reps.length]!, masked_phone: `0913•••${String(100 + i).slice(-3)}` },
    phone: `0913000${String(100 + i).slice(-3)}`,
    parent_ref: null,
    htx_ref: c.ref,
    area_ha: String(120 + i * 35),
    tax_code: null,
    address: null,
    ...base(),
  }));
  const communes = [...new Set(coops.map((c) => c.commune_code))];
  const groups: Rec[] = communes.slice(0, 2).map((cc, i) => ({
    ref: `01927b3e-0000-7000-8000-0000000f${String(i + 1).padStart(4, "0")}`,
    kind: "coop_group",
    commune_code: cc,
    name: ["Tổ hợp tác Rau an toàn", "Tổ hợp tác Lúa chất lượng cao"][i]!,
    person: { label: ["Lê T. M.", "Phan V. N."][i]!, masked_phone: `0914•••${String(200 + i)}` },
    phone: `0914000${200 + i}`,
    parent_ref: null,
    htx_ref: null,
    area_ha: String(15 + i * 10),
    tax_code: null,
    address: null,
    ...base(),
  }));
  const parents = [...htx.slice(0, 3), ...groups];
  const households: Rec[] = parents.flatMap((p, i) =>
    Array.from({ length: 2 }, (_, k) => {
      const phone = `0987${String(100000 + 10 * i + k).slice(-6)}`;
      return {
        ref: `01927b3e-0000-7000-8000-0000001${String(i).padStart(2, "0")}${String(k).padStart(3, "0")}`,
        kind: "household" as const,
        commune_code: p.commune_code,
        name: null,
        person: { label: `Hộ ${k + 1} ${(p.name ?? "").replace(/^(HTX|Tổ hợp tác) /, "")}`, masked_phone: mask(phone) },
        phone,
        parent_ref: p.ref,
        htx_ref: null,
        area_ha: String(1.5 + k),
        tax_code: null,
        address: null,
        ...base(),
      };
    }),
  );
  RECS = [...htx, ...groups, ...households];
}

/** Cho kiểm thử. */
export function resetKnRecords(): void {
  seeded = false;
  RECS = [];
}

const inScope = (c: Caller, commune: string) => c.role !== "kn_xa" || c.communes.includes(commune);
const field = (errors: { field: string; reason: string }[]) => problem(422, "validation_failed", "Dữ liệu không hợp lệ", { errors });
const notFound = () => problem(404, "extension.kn_record_not_found", "Không tìm thấy hồ sơ trong phạm vi phụ trách");
function canWrite(c: Caller): Response | null {
  if (c.role === "kn_tw") return problem(403, "extension.read_only", "Cấp Quốc gia chỉ xem");
  if (c.role !== "kn_xa" && c.role !== "admin") return problem(403, "forbidden", "Chỉ Cấp Xã và Admin được thêm, sửa hồ sơ HTX, Nông hộ, Tổ hợp tác");
  return null;
}
const memberCount = (ref: string) => RECS.filter((r) => r.kind === "household" && r.parent_ref === ref && r.status === "active").length;

function wire(r: Rec, c: Caller, coops: PublishedCoop[]) {
  const parent = r.parent_ref ? RECS.find((p) => p.ref === r.parent_ref && inScope(c, p.commune_code)) : undefined;
  const htx = r.htx_ref ? coops.find((x) => x.ref === r.htx_ref) : undefined;
  return {
    ref: r.ref,
    kind: r.kind,
    kind_label: KIND_LABEL[r.kind],
    commune_code: r.commune_code,
    name: r.name,
    display_name: r.name ?? r.person.label,
    representative: r.kind === "household" ? null : r.person,
    holder: r.kind === "household" ? r.person : null,
    parent_ref: r.parent_ref,
    parent_name: parent?.name ?? null,
    parent_kind: parent?.kind ?? null,
    htx_ref: r.htx_ref,
    htx_name: htx?.name ?? null,
    area_ha: r.area_ha,
    tax_code: r.tax_code,
    address: r.address,
    member_count: r.kind === "household" ? 0 : memberCount(r.ref),
    status: r.status,
    status_reason: r.status_reason,
    version: r.version,
    can_edit: canWrite(c) === null && inScope(c, r.commune_code),
    updated_at: r.updated_at,
  };
}
const detail = (r: Rec, c: Caller, coops: PublishedCoop[]) => ({ ...wire(r, c, coops), log: [...r.log].reverse() });

type Body = {
  kind?: string;
  commune_code?: string;
  name?: string | null;
  area_ha?: string | number | null;
  tax_code?: string | null;
  address?: string | null;
  representative?: { full_name?: string; phone?: string };
  holder?: { full_name?: string; phone?: string };
  parent_ref?: string | null;
  htx_ref?: string | null;
  base_version?: number;
};

/** Kiểm thân tạo/sửa; trả lỗi hoặc bản ghi đã chuẩn hóa (chưa gắn ref/phiên bản). */
function check(b: Body, c: Caller, coops: PublishedCoop[], kind: Kind, create: boolean, self?: string): Response | Omit<Rec, "ref" | "status" | "status_reason" | "version" | "updated_at" | "log"> {
  const errs: { field: string; reason: string }[] = [];
  const commune = String(b.commune_code ?? "").trim();
  if (!commune) errs.push({ field: "commune_code", reason: "bắt buộc" });
  const name = String(b.name ?? "").trim();
  if (kind === "household") {
    if (name) errs.push({ field: "name", reason: "Nông hộ: tên là họ tên chủ hộ" });
    if (b.tax_code) errs.push({ field: "tax_code", reason: "chỉ dùng cho HTX" });
    if (b.address) errs.push({ field: "address", reason: "Nông hộ: địa chỉ là dữ liệu cá nhân" });
  } else {
    if (!name) errs.push({ field: "name", reason: "bắt buộc" });
    else if ([...name].length > 200) errs.push({ field: "name", reason: "tối đa 200 ký tự" });
    else if (PII_RE.test(name)) errs.push({ field: "name", reason: "không ghi số điện thoại/CCCD trong tên" });
    if (b.tax_code && (kind !== "cooperative" || !/^\d{10}(-\d{3})?$/.test(b.tax_code))) errs.push({ field: "tax_code", reason: "mã số thuế 10 chữ số (chỉ HTX)" });
  }
  const area = b.area_ha === undefined || b.area_ha === null || b.area_ha === "" ? null : String(b.area_ha).trim();
  if (area !== null && (!AREA_RE.test(area) || Number(area) > 1_000_000)) errs.push({ field: "area_ha", reason: "số thập phân ≥ 0, tối đa 4 chữ số lẻ, không quá 1.000.000 ha" });
  const pk = kind === "household" ? "holder" : "representative";
  const p = kind === "household" ? b.holder : b.representative;
  if ((kind === "household" ? b.representative : b.holder) !== undefined) errs.push({ field: kind === "household" ? "representative" : "holder", reason: "sai loại hồ sơ" });
  let phone = "";
  if (p !== undefined || create) {
    const full = String(p?.full_name ?? "").trim();
    phone = String(p?.phone ?? "").replace(/[\s.()-]/g, "");
    if (!full) errs.push({ field: `${pk}.full_name`, reason: "bắt buộc" });
    else if (/\d/.test(full)) errs.push({ field: `${pk}.full_name`, reason: "họ tên không chứa chữ số" });
    if (!phone) errs.push({ field: `${pk}.phone`, reason: "bắt buộc" });
    else if (!/^0[1-9]\d{8}$/.test(phone)) errs.push({ field: `${pk}.phone`, reason: "số điện thoại Việt Nam không hợp lệ (10 chữ số, bắt đầu bằng 0)" });
  }
  if (b.parent_ref && kind !== "household") errs.push({ field: "parent_ref", reason: "chỉ Nông hộ thuộc HTX/Tổ hợp tác" });
  if (b.htx_ref && kind !== "cooperative") errs.push({ field: "htx_ref", reason: "chỉ hồ sơ HTX liên kết HTX đã công bố" });
  if (errs.length) return field(errs);
  if (!inScope(c, commune)) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
  if (b.htx_ref) {
    const h = coops.find((x) => x.ref === b.htx_ref);
    if (!h || !h.active || !inScope(c, h.commune_code)) return field([{ field: "htx_ref", reason: "không có HTX đã công bố, đang hoạt động trong phạm vi" }]);
    if (h.commune_code !== commune) return field([{ field: "htx_ref", reason: "HTX đã công bố ở xã khác" }]);
  }
  if (b.parent_ref) {
    const par = RECS.find((x) => x.ref === b.parent_ref);
    if (!par || par.ref === self || par.kind === "household" || !inScope(c, par.commune_code)) return field([{ field: "parent_ref", reason: "không có HTX/Tổ hợp tác trong phạm vi" }]);
    if (par.status !== "active") return field([{ field: "parent_ref", reason: "HTX/Tổ hợp tác đã ngừng hoạt động" }]);
  }
  const cur = self ? RECS.find((x) => x.ref === self) : undefined;
  const person = p !== undefined ? { label: shortName(String(p.full_name)), masked_phone: mask(phone) } : cur!.person;
  return {
    kind,
    commune_code: commune,
    name: kind === "household" ? null : name,
    person,
    phone: p !== undefined ? phone : cur!.phone,
    parent_ref: b.parent_ref || null,
    htx_ref: b.htx_ref || null,
    area_ha: area,
    tax_code: kind === "cooperative" ? b.tax_code || null : null,
    address: kind === "household" ? null : (b.address ?? "").trim() || null,
  };
}

function conflict(r: Pick<Rec, "kind" | "commune_code" | "name" | "phone">, self?: string): Response | null {
  const others = RECS.filter((x) => x.ref !== self && x.kind === r.kind && x.status === "active");
  if (r.kind === "household") return others.some((x) => x.phone === r.phone) ? problem(409, "extension.kn_record_household_exists", "Chủ hộ đã có hồ sơ Nông hộ đang hoạt động") : null;
  return others.some((x) => x.commune_code === r.commune_code && nameKey(x.name ?? "") === nameKey(r.name ?? ""))
    ? problem(409, "extension.kn_record_name_taken", "Tên đã có trong xã (cùng loại, đang hoạt động)", { errors: [{ field: "name", reason: `trùng tên ${KIND_LABEL[r.kind]} trong xã` }] })
    : null;
}

const UUID = "([0-9a-f-]{36})";
const re = (s: string) => new RegExp(`^${s.replaceAll("{id}", UUID)}$`);
const KINDS: Kind[] = ["cooperative", "household", "coop_group"];

export function knRecordRoutes(getCoops: () => PublishedCoop[], wrap: Wrap): Route[] {
  const w = (h: (ctx: Ctx, c: Caller, coops: PublishedCoop[]) => Response) =>
    wrap((ctx, c) => {
      const coops = getCoops();
      seed(coops);
      return h(ctx, c, coops);
    });
  const find = (c: Caller, ref: string | undefined) => RECS.find((x) => x.ref === ref && inScope(c, x.commune_code));
  return [
    {
      method: "GET",
      pattern: re("/v1/kn/producer-records"),
      handler: w(({ url }, c, coops) => {
        const p = (k: string) => url.searchParams.get(k) ?? "";
        const kind = p("kind") as Kind;
        if (!KINDS.includes(kind)) return field([{ field: "kind", reason: "bắt buộc: cooperative | household | coop_group" }]);
        const commune = p("commune");
        if (commune && !inScope(c, commune)) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
        const st = p("status") || "active";
        const q = p("q").trim().toLowerCase();
        if (kind === "household" && q) return field([{ field: "q", reason: "Nông hộ: lọc theo xã, HTX/THT, trạng thái" }]);
        const items = RECS.filter(
          (x) =>
            x.kind === kind &&
            inScope(c, x.commune_code) &&
            (!commune || x.commune_code === commune) &&
            (st === "all" || x.status === st) &&
            (!q || (x.name ?? "").toLowerCase().includes(q)) &&
            (!p("parent_ref") || x.parent_ref === p("parent_ref")) &&
            (!p("htx_ref") || x.htx_ref === p("htx_ref")),
        );
        return json({ items: items.map((x) => wire(x, c, coops)), next_cursor: null });
      }),
    },
    {
      method: "POST",
      pattern: re("/v1/kn/producer-records"),
      handler: w(({ body }, c, coops) => {
        const deny = canWrite(c);
        if (deny) return deny;
        const b = (body ?? {}) as Body;
        const kind = b.kind as Kind;
        if (!KINDS.includes(kind)) return field([{ field: "kind", reason: "bắt buộc: cooperative | household | coop_group" }]);
        const v = check(b, c, coops, kind, true);
        if (v instanceof Response) return v;
        const dup = conflict(v);
        if (dup) return dup;
        const r: Rec = { ...v, ref: uuidv7(), status: "active", status_reason: null, version: 1, updated_at: now(), log: [entry("created", 1)] };
        RECS.push(r);
        return json(detail(r, c, coops), 201);
      }),
    },
    {
      method: "GET",
      pattern: re("/v1/kn/producer-records/{id}"),
      handler: w(({ params }, c, coops) => {
        const r = find(c, params[0]);
        return r ? json(detail(r, c, coops)) : notFound();
      }),
    },
    {
      method: "POST",
      pattern: re("/v1/kn/producer-records/{id}"),
      handler: w(({ params, body }, c, coops) => {
        const deny = canWrite(c);
        if (deny) return deny;
        const r = find(c, params[0]);
        if (!r) return notFound();
        const b = (body ?? {}) as Body;
        if (b.kind && b.kind !== r.kind) return field([{ field: "kind", reason: "không đổi loại hồ sơ" }]);
        if (r.status !== "active") return problem(409, "extension.kn_record_state_conflict", "Hồ sơ đã ngừng hoạt động — kích hoạt lại trước khi sửa");
        if (b.base_version !== r.version) return problem(409, "extension.kn_record_version_conflict", "Hồ sơ đã được người khác cập nhật — tải lại rồi sửa");
        const v = check(b, c, coops, r.kind, false, r.ref);
        if (v instanceof Response) return v;
        const same = (Object.keys(v) as (keyof typeof v)[]).every((k) => JSON.stringify(v[k]) === JSON.stringify(r[k]));
        if (same) return json(detail(r, c, coops));
        const dup = conflict(v, r.ref);
        if (dup) return dup;
        Object.assign(r, v, { version: r.version + 1, updated_at: now(), status_reason: null });
        r.log.push(entry("updated", r.version));
        return json(detail(r, c, coops));
      }),
    },
    ...(["deactivate", "reactivate"] as const).map((op) => ({
      method: "POST",
      pattern: re(`/v1/kn/producer-records/{id}/${op}`),
      handler: w(({ params, body }, c, coops) => {
        const deny = canWrite(c);
        if (deny) return deny;
        const r = find(c, params[0]);
        if (!r) return notFound();
        const b = (body ?? {}) as { reason?: string; base_version?: number };
        const reason = String(b.reason ?? "").trim();
        if ([...reason].length < 10 || [...reason].length > 1000) return field([{ field: "reason", reason: "bắt buộc, 10–1000 ký tự" }]);
        if (PII_RE.test(reason)) return field([{ field: "reason", reason: "không ghi số điện thoại/CCCD trong lý do" }]);
        if (b.base_version !== r.version) return problem(409, "extension.kn_record_version_conflict", "Hồ sơ đã được người khác cập nhật — tải lại rồi sửa");
        if ((r.status === "inactive") === (op === "deactivate")) return problem(409, "extension.kn_record_state_conflict", "Hồ sơ đã ở trạng thái này");
        if (op === "reactivate") {
          const dup = conflict(r, r.ref);
          if (dup) return dup;
        }
        Object.assign(r, { status: op === "deactivate" ? "inactive" : "active", status_reason: reason, version: r.version + 1, updated_at: now() });
        r.log.push(entry(op === "deactivate" ? "deactivated" : "reactivated", r.version, reason));
        return json(detail(r, c, coops));
      }),
    })),
    {
      method: "GET",
      pattern: re("/v1/kn/producer-htx-candidates"),
      handler: w(({ url }, c, coops) => {
        const commune = url.searchParams.get("commune") ?? "";
        if (commune && !inScope(c, commune)) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
        if (!commune && c.role !== "kn_xa" && c.role !== "kn_tinh") return field([{ field: "commune", reason: "bắt buộc khi phạm vi toàn quốc" }]);
        const items = coops
          .filter((x) => x.active && inScope(c, x.commune_code) && (!commune || x.commune_code === commune))
          .map((x) => ({ htx_ref: x.ref, name: x.name, commune_code: x.commune_code, linked_ref: RECS.find((r) => r.kind === "cooperative" && r.status === "active" && r.htx_ref === x.ref)?.ref ?? null }));
        return json({ items });
      }),
    },
  ];
}
