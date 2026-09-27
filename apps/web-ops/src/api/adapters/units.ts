// pf/masterdata — tra đơn vị hành chính để hiện TÊN cho các mã mà route khác trả về
// (HTX, nhiệm vụ, dashboard chỉ có mã). Kết quả được nhớ theo client, không gọi lặp.
import type { ApiClient } from "../client";
import { unwrap } from "../client";
import { collectPages } from "../pagination";
import { ApiProblem } from "../problem";
import type { AdminUnit } from "../types";

export interface UnitResolver {
  /** Một đơn vị theo mã; không có (404) → null. Lỗi khác ném ApiProblem. */
  get(code: string): Promise<AdminUnit | null>;
  /** Nhiều mã cùng lúc, kèm cả đơn vị cha (để hiện "Xã …, Tỉnh …"). Mã lỗi/không có bị bỏ qua. */
  many(codes: readonly string[], withParents?: boolean): Promise<Map<string, AdminUnit>>;
}

const resolvers = new WeakMap<ApiClient, UnitResolver>();

export function unitResolver(api: ApiClient): UnitResolver {
  const existing = resolvers.get(api);
  if (existing) return existing;
  const cache = new Map<string, Promise<AdminUnit | null>>();

  const get = (code: string): Promise<AdminUnit | null> => {
    let p = cache.get(code);
    if (!p) {
      p = unwrap(api.GET("/v1/masterdata/admin-units/{code}", { params: { path: { code } } })).catch((e: unknown) => {
        if (e instanceof ApiProblem && e.status === 404) return null;
        cache.delete(code); // lỗi tạm thời: lần sau thử lại
        throw e;
      });
      cache.set(code, p);
    }
    return p;
  };

  const many = async (codes: readonly string[], withParents = true): Promise<Map<string, AdminUnit>> => {
    const out = new Map<string, AdminUnit>();
    const load = async (list: string[]) => {
      const settled = await Promise.allSettled(list.map((c) => get(c)));
      for (const r of settled) if (r.status === "fulfilled" && r.value) out.set(r.value.code, r.value);
    };
    await load(Array.from(new Set(codes.filter(Boolean))));
    if (withParents) {
      const parents = Array.from(out.values())
        .map((u) => u.parent_code)
        .filter((c): c is string => !!c && !out.has(c));
      await load(Array.from(new Set(parents)));
    }
    return out;
  };

  const r: UnitResolver = { get, many };
  resolvers.set(api, r);
  return r;
}

/** Đơn vị con trực tiếp có hiệu lực hôm nay (parent bỏ trống → các tỉnh). Gom mọi trang cursor. */
export function listChildUnits(api: ApiClient, parent?: string | null): Promise<AdminUnit[]> {
  return collectPages(
    (cursor) =>
      unwrap(api.GET("/v1/masterdata/admin-units", { params: { query: { parent: parent ?? undefined, cursor, limit: 200 } } })),
    5000,
  );
}

/** "Xã A, Đồng Tháp" từ bảng tra; thiếu tên thì hiện mã. */
export function unitLabel(units: ReadonlyMap<string, AdminUnit>, code: string | null | undefined, withParent = true): string {
  if (!code) return "—";
  const u = units.get(code);
  if (!u) return code;
  const parent = withParent && u.parent_code ? units.get(u.parent_code) : undefined;
  return parent ? `${u.name}, ${parent.name}` : u.name;
}
