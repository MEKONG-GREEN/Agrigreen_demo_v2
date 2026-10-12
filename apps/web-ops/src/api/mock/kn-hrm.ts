// ============================================================================
// BACKEND GIẢ LẬP — Danh mục nhân sự (W11.21, dom/hrm, BRD HCTH v3.0 FN-19). Đường dẫn, tham số, hình dạng phản hồi
// theo ĐÚNG contracts/openapi/application.yaml (/v1/hrm/*). Quy tắc mô phỏng (rút gọn từ internal/platform/hrm):
//   - Đọc: Admin; Cấp Tỉnh có ngữ cảnh ở Phòng Hành chính – Tổng hợp. Ghi: Admin. Khác → 403 forbidden.
//   - Mỗi thay đổi là một phiên bản; ngừng sử dụng = phiên bản active=false; mã trùng (kể cả đã ngừng) → 409
//     hrm.item_exists; mã ngạch bỏ khoảng trắng/xuống dòng (AC-03); mã khác về chữ thường.
//   - Bảng hệ số có hiệu lực theo valid_from (AC-02); hệ số tăng dần, ≤ 2 chữ số thập phân.
//   - Dữ liệu mặc định cài sẵn (như sau POST /v1/hrm/defaults), mẫu đánh dấu placeholder (Q-070).
// Dữ liệu là GIẢ LẬP. Trạng thái nằm trong bộ nhớ tab.
// ============================================================================
import type { components } from "../generated/application";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

type S = components["schemas"];
type Item = S["HrmCatalogItem"];
type Scale = S["HrmGradeScale"];
type Rule = S["HrmRule"];
type Attrs = S["HrmCatalogAttrs"];

const KINDS = [
  "position",
  "grade",
  "education_level",
  "major",
  "major_group",
  "ethnicity",
  "religion",
  "political_theory",
  "state_mgmt",
  "foreign_language",
  "language_level",
  "it_level",
  "award_form",
  "discipline_form",
  "change_type",
  "leave_reason",
];
const HCTH = "hanh_chinh_tong_hop";

interface Store {
  items: Map<string, Item[]>; // "<kind>:<code>" → phiên bản
  scales: Map<string, Scale[]>;
  rules: Map<string, Rule[]>;
}

let STORE: Store = seed();

const now = () => new Date().toISOString();
const isDay = (s: unknown): s is string =>
  typeof s === "string" && /^\d{4}-\d{2}-\d{2}$/.test(s);
const vnToday = () =>
  new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);

function seed(): Store {
  const s: Store = { items: new Map(), scales: new Map(), rules: new Map() };
  const put = (
    kind: string,
    code: string,
    name: string,
    order: number,
    placeholder: boolean,
    attrs: Attrs = {},
  ) =>
    s.items.set(`${kind}:${code}`, [
      {
        kind,
        code,
        name,
        sort_order: order,
        active: true,
        placeholder,
        version: 1,
        attrs,
        recorded_at: "2026-10-01T02:00:00Z",
      },
    ]);
  const grades: [string, string, string, string, boolean][] = [
    ["01.002", "Chuyên viên chính", "CVC", "cong_chuc", false],
    ["01.003", "Chuyên viên", "CV", "cong_chuc", false],
    ["06.031", "Kế toán viên", "CV", "cong_chuc", true],
    ["V.05.02.07", "Kỹ sư hạng III", "CV", "vien_chuc", false],
    ["V.03.09.26", "Khuyến nông viên hạng III", "CV", "vien_chuc", false],
    [
      "V.03.09.27",
      "Kỹ thuật viên khuyến nông hạng IV",
      "CS",
      "vien_chuc",
      true,
    ],
  ];
  grades.forEach(([code, name, eq, gt, ph], i) =>
    put("grade", code, name, (i + 1) * 10, ph, {
      equiv_group: eq as Attrs["equiv_group"],
      grade_type: gt as Attrs["grade_type"],
    }),
  );
  [
    ["giam_doc", "Giám đốc"],
    ["pho_giam_doc", "Phó Giám đốc"],
    ["truong_phong", "Trưởng phòng"],
    ["pho_truong_phong", "Phó Trưởng phòng"],
  ].forEach(([c = "", n = ""], i) =>
    put("position", c, n, (i + 1) * 10, true, { leader: true, mgmt_level: c }),
  );
  put("position", "vien_chuc", "Viên chức", 70, true);
  const levels: [string, string, number][] = [
    ["tien_si", "Tiến sĩ", 60],
    ["thac_si", "Thạc sĩ", 50],
    ["dai_hoc", "Đại học", 40],
    ["cao_dang", "Cao đẳng", 30],
    ["trung_cap", "Trung cấp", 20],
    ["khac", "Khác", 10],
  ];
  levels.forEach(([c, n, r], i) =>
    put("education_level", c, n, (i + 1) * 10, false, { rank: r }),
  );
  const groups: [string, string, string][] = [
    ["thac_si", "ths_nong_hoc", "Nông học"],
    ["thac_si", "ths_khac", "Khác"],
    ["dai_hoc", "dh_tt_bvtv", "Trồng trọt + BVTV"],
    ["dai_hoc", "dh_nong_hoc", "Nông học"],
    ["dai_hoc", "dh_khac", "Khác"],
    ["trung_cap", "tc_trong_trot", "Trồng trọt"],
    ["trung_cap", "tc_khac", "Khác"],
  ];
  groups.forEach(([lv, c, n], i) =>
    put("major_group", c, n, (i + 1) * 10, false, { level: lv }),
  );
  put("major", "trong_trot", "Trồng trọt", 10, true, {
    groups: {
      thac_si: "ths_nong_hoc",
      dai_hoc: "dh_tt_bvtv",
      trung_cap: "tc_trong_trot",
    },
  });
  put("major", "ke_toan", "Kế toán", 20, true, {
    groups: { thac_si: "ths_khac", dai_hoc: "dh_khac", trung_cap: "tc_khac" },
  });
  put("ethnicity", "kinh", "Kinh", 10, false);
  put("ethnicity", "khmer", "Khmer", 20, true, { minority: true });
  put("religion", "khong", "Không", 10, false, { no_religion: true });
  put("political_theory", "cu_nhan", "Cử nhân", 10, false);
  put("political_theory", "cao_cap", "Cao cấp", 20, false);
  put("state_mgmt", "cvc", "Chuyên viên chính", 10, false);
  put("award_form", "bang_khen_ubnd_tinh", "Bằng khen của UBND tỉnh", 10, true);
  put("award_form", "cstd_co_so", "Chiến sĩ thi đua cơ sở", 20, true);
  put("discipline_form", "khien_trach", "Khiển trách", 10, true);
  put("leave_reason", "nghi_huu", "Nghỉ hưu", 10, true);
  const step = (start: number, d: number, n: number) =>
    Array.from({ length: n }, (_, i) =>
      ((start * 100 + d * 100 * i) / 100).toFixed(2),
    );
  const sc = (g: string, cs: string[], hold: number): Scale => ({
    grade_code: g,
    valid_from: "2004-10-01",
    steps: cs.length,
    coefficients: cs,
    hold_months: hold,
    note: "Mẫu theo NĐ 204/2004 — chờ Phòng HCTH xác nhận (Q-070)",
    placeholder: true,
    version: 1,
    recorded_at: "2026-10-01T02:00:00Z",
  });
  s.scales.set("01.002", [sc("01.002", step(4.4, 0.34, 8), 36)]);
  for (const g of ["01.003", "06.031", "V.05.02.07", "V.03.09.26"])
    s.scales.set(g, [sc(g, step(2.34, 0.33, 9), 36)]);
  s.scales.set("V.03.09.27", [sc("V.03.09.27", step(1.86, 0.2, 12), 24)]);
  const rule = (key: string, payload: Record<string, unknown>): Rule => ({
    key,
    valid_from: "2026-01-01",
    payload: payload as Record<string, never>,
    note: "Mẫu — chờ Phòng HCTH gửi quy chế (Q-070)",
    placeholder: true,
    version: 1,
    recorded_at: "2026-10-01T02:00:00Z",
  });
  s.rules.set("promotion", [
    rule("promotion", {
      min_rating: "HTNV",
      rating_years: 3,
      achievement_years: 5,
      discipline_extension_months: { khien_trach: 6 },
      nlth_quota_pct: "10",
      nlth_retirement_months: 12,
      nlth_retirement_within_months: 12,
      tnvk_start_pct: "5",
      tnvk_step_pct: "1",
      tnvk_cycle_months: 12,
      tnvk_first_months: 36,
      remind_months: [1, 3, 6],
      retirement_age_male_months: 738,
      retirement_age_female_months: 684,
    }),
  ]);
  s.rules.set("achievement_conversion", [
    rule("achievement_conversion", {
      rules: [
        { award_code: "bang_khen_ubnd_tinh", months: 12 },
        { award_code: "cstd_co_so", months: 9 },
      ],
    }),
  ]);
  return s;
}

/** Đặt lại dữ liệu giả lập (kiểm thử). */
export function resetHrm(): void {
  STORE = seed();
}

function access(ctx: Ctx): { manage: boolean } | Response {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (kc?.role === "admin") return { manage: true };
  if (kc?.role === "kn_tinh" && kc.department_code === HCTH)
    return { manage: false };
  return problem(
    403,
    "forbidden",
    "danh mục nhân sự: chỉ Admin và Phòng Hành chính – Tổng hợp",
  );
}
function writer(ctx: Ctx): Response | null {
  const a = access(ctx);
  if (a instanceof Response) return a;
  return a.manage
    ? null
    : problem(
        403,
        "forbidden",
        "danh mục nhân sự: chỉ Admin được thêm, sửa, ngừng sử dụng (FN-19)",
      );
}
const last = <T>(xs: T[]): T => xs[xs.length - 1]!;
const normCode = (kind: string, c: string) =>
  kind === "grade"
    ? c.replace(/\s+/g, "")
    : c.replace(/\s+/g, "").toLowerCase();
const validCode = (kind: string, c: string) =>
  (kind === "grade"
    ? /^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$/
    : /^[a-z][a-z0-9_]{0,39}$/
  ).test(c);
const current = (kind: string, code: string) => {
  const v = STORE.items.get(`${kind}:${code}`);
  return v ? last(v) : undefined;
};
const activeItem = (kind: string, code: string | undefined) =>
  code ? current(kind, code)?.active === true : false;
const isCoef = (s: unknown): s is string =>
  typeof s === "string" && /^\d{1,2}(\.\d{1,2})?$/.test(s) && Number(s) > 0;

function checkItem(kind: string, b: S["HrmCatalogItemInput"]): Response | null {
  const name = b?.name ?? "";
  if (!name.trim() || name !== name.trim() || name.length > 200)
    return problem(422, "validation_failed", "Tên dài 1–200 ký tự");
  const a = b.attrs ?? {};
  if (kind === "grade" && (!a.equiv_group || !a.grade_type))
    return problem(
      422,
      "validation_failed",
      "Chọn nhóm tương đương và loại ngạch",
    );
  if (kind === "education_level" && !a.rank)
    return problem(422, "validation_failed", "Nhập thứ hạng trình độ");
  if (
    kind === "position" &&
    a.allowance !== undefined &&
    (!a.leader || !isCoef(a.allowance))
  )
    return problem(
      422,
      "validation_failed",
      "Hệ số phụ cấp chỉ cho chức vụ lãnh đạo, tối đa 2 chữ số thập phân",
    );
  if (kind === "major_group" && !activeItem("education_level", a.level))
    return problem(
      422,
      "hrm.item_reference",
      "Tham chiếu tới mục danh mục không có hoặc đã ngừng sử dụng",
      { detail: `trình độ ${a.level ?? ""}` },
    );
  if (kind === "major")
    for (const [lv, g] of Object.entries(a.groups ?? {})) {
      if (
        !activeItem("education_level", lv) ||
        current("major_group", g)?.attrs.level !== lv
      )
        return problem(
          422,
          "hrm.item_reference",
          "Tham chiếu tới mục danh mục không có hoặc đã ngừng sử dụng",
          { detail: `nhóm ${g} / ${lv}` },
        );
    }
  return null;
}

const written = (code: string, version: number) => json({ code, version }, 201);

function catalogRoutes(): Route[] {
  return [
    {
      method: "GET",
      pattern: /^\/v1\/hrm\/status$/,
      handler: (ctx) => {
        const a = access(ctx);
        if (a instanceof Response) return a;
        const counts: Record<string, number> = Object.fromEntries(
          KINDS.map((k) => [k, 0]),
        );
        let placeholders = 0;
        for (const vs of STORE.items.values()) {
          const v = last(vs);
          if (!v.active) continue;
          counts[v.kind] = (counts[v.kind] ?? 0) + 1;
          if (v.placeholder) placeholders++;
        }
        for (const vs of STORE.scales.values())
          if (last(vs).placeholder) placeholders++;
        for (const vs of STORE.rules.values())
          if (last(vs).placeholder) placeholders++;
        return json({
          can_manage: a.manage,
          rule_version: "hrm.v1",
          day: vnToday(),
          counts,
          placeholders,
          scales: STORE.scales.size,
          rules: STORE.rules.size,
        });
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/hrm\/defaults$/,
      handler: (ctx) =>
        writer(ctx) ?? json({ items: 0, scales: 0, rules: 0 }, 201),
    },
    {
      method: "GET",
      pattern: /^\/v1\/hrm\/catalogs\/([a-z_]+)$/,
      handler: (ctx) => {
        const kind = ctx.params[0]!;
        if (!KINDS.includes(kind))
          return problem(
            404,
            "hrm.kind_unknown",
            "Không có loại danh mục nhân sự này",
          );
        const a = access(ctx);
        if (a instanceof Response) return a;
        const all = ctx.url.searchParams.get("include_retired") === "true";
        const items = [...STORE.items.values()]
          .map(last)
          .filter((v) => v.kind === kind && (all || v.active))
          .sort(
            (x, y) =>
              x.sort_order - y.sort_order || x.code.localeCompare(y.code),
          );
        return json({ kind, items });
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/hrm\/catalogs\/([a-z_]+)$/,
      handler: (ctx) => {
        const kind = ctx.params[0]!;
        if (!KINDS.includes(kind))
          return problem(
            404,
            "hrm.kind_unknown",
            "Không có loại danh mục nhân sự này",
          );
        const w = writer(ctx);
        if (w) return w;
        const b = ctx.body as S["HrmCatalogItemInput"];
        const code = normCode(kind, b?.code ?? "");
        if (!validCode(kind, code))
          return problem(422, "validation_failed", "Mã không hợp lệ");
        if (STORE.items.has(`${kind}:${code}`))
          return problem(409, "hrm.item_exists", "Mã đã có trong danh mục", {
            detail: `${kind} ${code}`,
          });
        const bad = checkItem(kind, b);
        if (bad) return bad;
        STORE.items.set(`${kind}:${code}`, [
          {
            kind,
            code,
            name: b.name,
            sort_order: b.sort_order ?? 0,
            active: true,
            placeholder: false,
            version: 1,
            attrs: b.attrs ?? {},
            recorded_at: now(),
          },
        ]);
        return written(code, 1);
      },
    },
    {
      method: "GET",
      pattern: /^\/v1\/hrm\/catalogs\/([a-z_]+)\/([^/]+)\/history$/,
      handler: (ctx) => {
        const a = access(ctx);
        if (a instanceof Response) return a;
        const [kind = "", raw = ""] = ctx.params;
        const vs = STORE.items.get(
          `${kind}:${normCode(kind, decodeURIComponent(raw))}`,
        );
        if (!vs)
          return problem(
            404,
            "hrm.item_not_found",
            "Không có mục danh mục này",
          );
        return json({ versions: vs });
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/hrm\/catalogs\/([a-z_]+)\/([^/]+)\/revise$/,
      handler: (ctx) => {
        const w = writer(ctx);
        if (w) return w;
        const [kind = "", raw = ""] = ctx.params;
        const code = normCode(kind, decodeURIComponent(raw));
        const vs = STORE.items.get(`${kind}:${code}`);
        if (!vs)
          return problem(
            404,
            "hrm.item_not_found",
            "Không có mục danh mục này",
          );
        const b = ctx.body as S["HrmCatalogItemInput"];
        if (b?.code)
          return problem(
            422,
            "validation_failed",
            "không đổi được mã — mã ở đường dẫn",
          );
        const active = b?.active ?? true;
        if (active) {
          const bad = checkItem(kind, b);
          if (bad) return bad;
        }
        const v: Item = {
          kind,
          code,
          name: b.name,
          sort_order: b.sort_order ?? 0,
          active,
          placeholder: false,
          version: vs.length + 1,
          attrs: b.attrs ?? {},
          recorded_at: now(),
        };
        vs.push(v);
        return written(code, v.version);
      },
    },
  ];
}

function effectiveScale(grade: string, day: string): Scale | null {
  const vs = (STORE.scales.get(grade) ?? []).filter((s) => s.valid_from <= day);
  if (!vs.length) return null;
  return vs.reduce((a, b) =>
    b.valid_from > a.valid_from ||
    (b.valid_from === a.valid_from && b.version > a.version)
      ? b
      : a,
  );
}

function scaleRoutes(): Route[] {
  return [
    {
      method: "GET",
      pattern: /^\/v1\/hrm\/grade-scales$/,
      handler: (ctx) => {
        const a = access(ctx);
        if (a instanceof Response) return a;
        const at = ctx.url.searchParams.get("at");
        if (at && !isDay(at))
          return problem(
            422,
            "validation_failed",
            "at phải là ngày YYYY-MM-DD",
          );
        const day = at ?? vnToday();
        const grades = [...STORE.items.values()]
          .map(last)
          .filter((v) => v.kind === "grade" && v.active)
          .sort(
            (x, y) =>
              x.sort_order - y.sort_order || x.code.localeCompare(y.code),
          )
          .map((g) => ({ grade: g, scale: effectiveScale(g.code, day) }));
        return json({ day, grades });
      },
    },
    {
      method: "GET",
      pattern: /^\/v1\/hrm\/grade-scales\/([^/]+)\/history$/,
      handler: (ctx) => {
        const a = access(ctx);
        if (a instanceof Response) return a;
        const g = normCode("grade", decodeURIComponent(ctx.params[0]!));
        if (!current("grade", g))
          return problem(
            404,
            "hrm.item_not_found",
            "Không có mục danh mục này",
          );
        const vs = [...(STORE.scales.get(g) ?? [])].sort(
          (x, y) =>
            x.valid_from.localeCompare(y.valid_from) || x.version - y.version,
        );
        return json({ versions: vs });
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/hrm\/grade-scales\/([^/]+)$/,
      handler: (ctx) => {
        const w = writer(ctx);
        if (w) return w;
        const g = normCode("grade", decodeURIComponent(ctx.params[0]!));
        const b = ctx.body as S["HrmGradeScaleInput"];
        if (!isDay(b?.valid_from))
          return problem(
            422,
            "validation_failed",
            "valid_from: ngày YYYY-MM-DD",
          );
        const cs = b.coefficients ?? [];
        if (!cs.length || cs.length > 20 || !cs.every(isCoef))
          return problem(
            422,
            "validation_failed",
            "Hệ số: 1–20 bậc, > 0, tối đa 2 chữ số thập phân",
          );
        for (let i = 1; i < cs.length; i++)
          if (Number(cs[i]) <= Number(cs[i - 1]))
            return problem(
              422,
              "validation_failed",
              `hệ số bậc ${i + 1} phải lớn hơn bậc ${i}`,
            );
        if (!(b.hold_months >= 1 && b.hold_months <= 120))
          return problem(
            422,
            "validation_failed",
            "thời gian giữ bậc 1–120 tháng",
          );
        if (!activeItem("grade", g))
          return problem(
            422,
            "hrm.item_reference",
            "Tham chiếu tới mục danh mục không có hoặc đã ngừng sử dụng",
            { detail: `ngạch ${g}` },
          );
        const vs = STORE.scales.get(g) ?? [];
        const v: Scale = {
          grade_code: g,
          valid_from: b.valid_from,
          steps: cs.length,
          coefficients: cs.map((c) => Number(c).toFixed(2)),
          hold_months: b.hold_months,
          note: b.note ?? "",
          placeholder: false,
          version: vs.length + 1,
          recorded_at: now(),
        };
        STORE.scales.set(g, [...vs, v]);
        return written(g, v.version);
      },
    },
  ];
}

function ruleRoutes(): Route[] {
  const known = (k: string) =>
    k === "promotion" || k === "achievement_conversion";
  return [
    {
      method: "GET",
      pattern: /^\/v1\/hrm\/rules\/([a-z_]+)$/,
      handler: (ctx) => {
        const key = ctx.params[0]!;
        if (!known(key))
          return problem(
            404,
            "hrm.rule_unknown",
            "Không có tham số nhân sự này",
          );
        const a = access(ctx);
        if (a instanceof Response) return a;
        const at = ctx.url.searchParams.get("at");
        if (at && !isDay(at))
          return problem(
            422,
            "validation_failed",
            "at phải là ngày YYYY-MM-DD",
          );
        const day = at ?? vnToday();
        const versions = [...(STORE.rules.get(key) ?? [])].sort(
          (x, y) =>
            x.valid_from.localeCompare(y.valid_from) || x.version - y.version,
        );
        const eff = versions.filter((v) => v.valid_from <= day);
        return json({
          key,
          day,
          current: eff.length ? last(eff) : null,
          versions,
        });
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/hrm\/rules\/([a-z_]+)$/,
      handler: (ctx) => {
        const key = ctx.params[0]!;
        if (!known(key))
          return problem(
            404,
            "hrm.rule_unknown",
            "Không có tham số nhân sự này",
          );
        const w = writer(ctx);
        if (w) return w;
        const b = ctx.body as {
          valid_from?: string;
          payload?: Record<string, unknown>;
          note?: string;
        };
        if (
          !isDay(b?.valid_from) ||
          !b.payload ||
          typeof b.payload !== "object"
        )
          return problem(
            422,
            "validation_failed",
            "Thiếu ngày hiệu lực hoặc nội dung",
          );
        if (key === "achievement_conversion") {
          const rows = (b.payload.rules ?? []) as {
            award_code: string;
            months: number;
          }[];
          if (!rows.length)
            return problem(
              422,
              "validation_failed",
              "bảng quy đổi có 1–50 dòng",
            );
          for (const r of rows) {
            if (!(r.months >= 1 && r.months <= 36))
              return problem(422, "validation_failed", "số tháng quy đổi 1–36");
            if (!activeItem("award_form", r.award_code))
              return problem(
                422,
                "hrm.item_reference",
                "Tham chiếu tới mục danh mục không có hoặc đã ngừng sử dụng",
                { detail: `award_form ${r.award_code}` },
              );
          }
        }
        const vs = STORE.rules.get(key) ?? [];
        const v: Rule = {
          key,
          valid_from: b.valid_from,
          payload: b.payload as Record<string, never>,
          note: b.note ?? "",
          placeholder: false,
          version: vs.length + 1,
          recorded_at: now(),
        };
        STORE.rules.set(key, [...vs, v]);
        return written(key, v.version);
      },
    },
  ];
}

export const hrmRoutes: Route[] = [
  ...catalogRoutes(),
  ...scaleRoutes(),
  ...ruleRoutes(),
];
