// ============================================================================
// BACKEND GIẢ LẬP — Báo cáo Sở NS01–NS06 + Dashboard nhân sự (W11.28, dom/hrm, BRD HCTH v3.0 FN-12, FN-06). Đường dẫn,
// tham số, hình dạng phản hồi theo ĐÚNG contracts/openapi/application.yaml (/v1/hrm/reports*, /v1/hrm/report-exports/*,
// /v1/hrm/dashboard*). Quy tắc mô phỏng RÚT GỌN từ internal/platform/hrm/domain/reports.go:
//   - Biểu Sở: cần export = all; biểu có trường thẻ Thống kê BC Sở cần thêm sensitive.view = all.
//   - Dân số tại ngày chốt (phép chiếu của kn-hrm-staff); NS05/NS06 đếm theo đơn vị (cột rút gọn), nhấn ô xem người.
//   - Quy tắc R1 (NS06), R2 (NS05), TK; lỗi chặn → 422 hrm.report_blocked. Sổ xuất trong bộ nhớ tab; tệp tải là tệp giả.
// ============================================================================
import type { components } from "../generated/application";
import { json, problem, type Ctx, type Route } from "./http";
import { hrmStaffInternals as H } from "./kn-hrm-staff";

type S = components["schemas"];
type Snap = S["HrmSnapshot"];
type Staff = ReturnType<typeof H.staff>[number];
type Col = S["HrmReportView"]["columns"][number];

const META: S["HrmReportMeta"][] = [
  {
    code: "NS01",
    name: "Danh sách viên chức hiện có mặt",
    period: "month",
    aggregate: false,
    sensitive_fields: [
      "ethnicity",
      "hometown",
      "revolution_date",
      "party_date",
    ],
    source: "Thống kê CCVCNLĐ — sheet Tháng (19 cột)",
  },
  {
    code: "NS02",
    name: "Danh sách viên chức nghỉ việc",
    period: "month",
    aggregate: false,
    sensitive_fields: [
      "ethnicity",
      "hometown",
      "revolution_date",
      "party_date",
    ],
    source: "Thống kê CCVCNLĐ — sheet NGHỈ",
  },
  {
    code: "NS03",
    name: "Danh sách hợp đồng lao động",
    period: "ad_hoc",
    aggregate: false,
    sensitive_fields: [],
    source: "Thống kê CCVCNLĐ — sheet HĐLĐ-ĐVSN",
  },
  {
    code: "NS04",
    name: "Danh sách trích ngang viên chức",
    period: "ad_hoc",
    aggregate: false,
    sensitive_fields: [
      "ethnicity",
      "religion",
      "revolution_date",
      "party_date",
      "birthplace",
      "hometown",
    ],
    source: "Biểu mẫu nhân sự — sheet Trích ngang (36 cột)",
  },
  {
    code: "NS05",
    name: "Thống kê nhân sự theo trình độ, chuyên ngành",
    period: "month",
    aggregate: true,
    sensitive_fields: ["ethnicity"],
    source: "Biểu mẫu nhân sự — sheet Tổng hợp",
  },
  {
    code: "NS06",
    name: "Báo cáo chất lượng CCVC, HĐLĐ (Phụ lục 2)",
    period: "quarter",
    aggregate: true,
    sensitive_fields: ["ethnicity", "religion", "party_date"],
    source: "Biểu mẫu nhân sự — Sheet1 Phụ lục 2 (39 cột)",
  },
];
const LOGS: S["HrmReportLog"][] = [];
const vnToday = () =>
  new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
const uuid = () => crypto.randomUUID();

/** Đặt lại sổ xuất giả lập (kiểm thử). */
export function resetHrmReports(): void {
  LOGS.length = 0;
}

interface P {
  st: Staff;
  snap: Snap;
}

function period(code: string, at: string): string {
  const [y, m, d] = at.split("-").map(Number) as [number, number, number];
  const p = META.find((x) => x.code === code)!.period;
  if (p === "month") return `THÁNG ${m}/${y}`;
  if (p === "quarter")
    return `QUÝ ${["I", "II", "III", "IV"][Math.floor((m - 1) / 3)]}/${y}`;
  return `NGÀY ${String(d).padStart(2, "0")}/${String(m).padStart(2, "0")}/${y}`;
}

function unitName(code: string): string {
  return (
    H.units().find((u) => u.code === code)?.name ?? (code || "Chưa xếp đơn vị")
  );
}

function age(birth: string, at: string): number | null {
  if (!birth) return null;
  const [by, bm = 1, bd = 1] = birth.split("-").map(Number) as number[];
  const [y, m, d] = at.split("-").map(Number) as [number, number, number];
  let a = y - by!;
  if (m < bm! || (m === bm && d < bd!)) a--;
  return a;
}

function topDegree(st: Staff, at: string): string {
  const rank = [
    "khac",
    "trung_cap",
    "cao_dang",
    "dai_hoc",
    "thac_si",
    "tien_si",
  ];
  let best = "";
  for (const e of st.events)
    if (
      e.kind === "degree" &&
      !e.void &&
      e.valid_from <= at &&
      rank.indexOf(e.payload.level ?? "") > rank.indexOf(best)
    )
      best = e.payload.level ?? "";
  return best;
}

function population(at: string): P[] {
  return H.staff().map((st) => ({ st, snap: H.project(st.events, at) }));
}

type Built = {
  view: S["HrmReportView"];
  cells: Record<string, string[]>;
  people: string[];
};

function build(code: string, at: string): Built {
  const meta = META.find((x) => x.code === code)!;
  const pop = population(at).filter((p) => p.st.type !== "can_bo_xa");
  const brief = (p: P): S["HrmPersonBrief"] => ({
    id: p.st.id,
    code: p.st.code,
    type: p.st.type,
    name: p.st.profile.full_name,
    unit: p.snap.unit,
    unit_name: unitName(p.snap.unit),
    position: p.snap.position,
  });
  const month = at.slice(0, 7);
  const sel =
    code === "NS02"
      ? pop.filter(
          (p) =>
            p.st.type === "vien_chuc" &&
            p.snap.status === "left" &&
            (p.snap.ended_on ?? "").slice(0, 7) === month,
        )
      : code === "NS03"
        ? pop.filter(
            (p) => p.st.type === "hop_dong" && p.snap.status === "active",
          )
        : code === "NS06"
          ? pop.filter((p) => p.snap.status === "active")
          : pop.filter(
              (p) => p.st.type === "vien_chuc" && p.snap.status === "active",
            );
  sel.sort(
    (a, b) =>
      a.snap.unit.localeCompare(b.snap.unit) ||
      a.st.code.localeCompare(b.st.code),
  );
  const rows: S["HrmReportView"]["rows"] = [];
  const cells: Record<string, string[]> = {};
  const checks: S["HrmReportView"]["checks"] = [];
  let cols: Col[];
  const c = (
    key: string,
    label: string,
    group = "",
    numeric = false,
    sensitive = false,
  ): Col => ({ key, label, group, numeric, sensitive });
  if (!meta.aggregate) {
    cols = [
      c("stt", "STT"),
      c("ho_ten", "Họ và tên"),
      c("ns_nam", "Nam", "Ngày sinh"),
      c("ns_nu", "Nữ", "Ngày sinh"),
      c("chuc_vu", "Chức vụ, chức danh"),
      c("phong_ban", "Phòng, ban, đơn vị công tác"),
    ];
    if (meta.sensitive_fields.length)
      cols.push(
        c("dan_toc", "Dân tộc", "", false, true),
        c("que_quan", "Quê quán", "", false, true),
      );
    cols.push(c("ngach", "Ngạch/CDNN"), c("ghi_chu", "Ghi chú"));
    rows.push({
      kind: "group",
      key: "",
      cells: cols.map((_, i) => (i === 1 ? "I. TRUNG TÂM KHUYẾN NÔNG" : "")),
    });
    let cur = "\0";
    sel.forEach((p, i) => {
      if (p.snap.unit !== cur) {
        cur = p.snap.unit;
        const n = sel.filter((x) => x.snap.unit === cur).length;
        rows.push({
          kind: "unit",
          key: cur,
          cells: cols.map((_, j) =>
            j === 1
              ? `${unitName(cur)} (${n} ${code === "NS03" ? "HĐ" : "VC"})`
              : "",
          ),
        });
      }
      const g = p.st.profile.gender;
      const v: Record<string, string> = {
        stt: String(i + 1),
        ho_ten: p.st.profile.full_name,
        ns_nam: g === "nam" ? p.st.profile.birth_date : "",
        ns_nu: g === "nu" ? p.st.profile.birth_date : "",
        chuc_vu: p.snap.title || p.snap.position,
        phong_ban: unitName(p.snap.unit),
        dan_toc: p.st.sensitive.ethnicity ?? "",
        que_quan: p.st.sensitive.hometown ?? "",
        ngach: p.snap.grade,
        ghi_chu:
          code === "NS02" ? `${p.snap.leave_reason} từ ${p.snap.ended_on}` : "",
      };
      rows.push({
        kind: "person",
        key: "",
        staff_id: p.st.id,
        cells: cols.map((x) => v[x.key] ?? ""),
      });
    });
  } else {
    cols = [
      c("stt", "STT"),
      c("don_vi", "Đơn vị"),
      c("tong", "Tổng số", "", true),
      c("nu", "Nữ", "Trong đó", true),
      c("dtts", "DTTS", "Trong đó", true, true),
    ];
    const levels = [
      ["tien_si", "TS"],
      ["thac_si", "ThS"],
      ["dai_hoc", "ĐH"],
      ["cao_dang", "CĐ"],
      ["trung_cap", "TC"],
      ["con_lai", "Còn lại"],
    ];
    for (const [k, l] of levels)
      cols.push(c(`cm:${k}`, l!, "Chuyên môn", true));
    const ages = [
      ["age_le30", "Dưới 30"],
      ["age_31_40", "31–40"],
      ["age_41_50", "41–50"],
      ["age_ge51", "Từ 51"],
    ];
    if (code === "NS06")
      for (const [k, l] of ages) cols.push(c(k!, l!, "Độ tuổi", true));
    const add = (row: string, col: string, id: string) => {
      for (const r of [row, "_center"]) (cells[`${r}|${col}`] ??= []).push(id);
    };
    const noAge: string[] = [];
    for (const p of sel) {
      const r = p.snap.unit;
      add(r, "tong", p.st.id);
      if (p.st.profile.gender === "nu") add(r, "nu", p.st.id);
      if (p.st.sensitive.ethnicity && p.st.sensitive.ethnicity !== "kinh")
        add(r, "dtts", p.st.id);
      const d = topDegree(p.st, at);
      add(r, `cm:${levels.some(([k]) => k === d) ? d : "con_lai"}`, p.st.id);
      if (code === "NS06") {
        const a = age(p.st.profile.birth_date, at);
        if (a === null) noAge.push(p.st.id);
        else
          add(
            r,
            a <= 30
              ? "age_le30"
              : a <= 40
                ? "age_31_40"
                : a <= 50
                  ? "age_41_50"
                  : "age_ge51",
            p.st.id,
          );
      }
    }
    const units = [
      ...new Set([
        ...H.units().map((u) => u.code),
        ...sel.map((p) => p.snap.unit),
      ]),
    ];
    [
      { code: "_center", name: code === "NS05" ? "TRUNG TÂM" : "TỔNG CỘNG" },
      ...units.map((u) => ({ code: u, name: unitName(u) })),
    ].forEach((u, i) => {
      rows.push({
        kind: i === 0 ? "total" : "unit",
        key: u.code,
        cells: cols.map((x, j) =>
          j === 0
            ? i
              ? String(i)
              : ""
            : j === 1
              ? u.name
              : String(cells[`${u.code}|${x.key}`]?.length ?? 0),
        ),
      });
    });
    const total = cells["_center|tong"]?.length ?? 0;
    if (code === "NS05") {
      const n01 = pop.filter(
        (p) => p.st.type === "vien_chuc" && p.snap.status === "active",
      ).length;
      checks.push({
        rule: "R2",
        level: "block",
        ok: total === n01,
        message: `Tổng số NS05 (${total}) = số người NS01 cùng ngày chốt (${n01})`,
        people: [],
      });
    } else {
      const ageSum = ages.reduce(
        (s, [k]) => s + (cells[`_center|${k}`]?.length ?? 0),
        0,
      );
      checks.push({
        rule: "R1",
        level: "block",
        ok: ageSum === total,
        message:
          "Tổng các nhóm ngạch/hạng, trình độ chuyên môn, độ tuổi = tổng số có mặt",
        people: sel.filter((p) => noAge.includes(p.st.id)).map(brief),
      });
    }
  }
  if (meta.sensitive_fields.length) {
    const miss = sel.filter((p) =>
      ["ethnicity", "religion", "hometown", "revolution_date"].some(
        (k) => !p.st.sensitive[k],
      ),
    );
    checks.push({
      rule: "TK",
      level: "warn",
      ok: miss.length === 0,
      message: `${miss.length} người thiếu dữ liệu thẻ Thống kê BC Sở (dân tộc, tôn giáo, quê quán, ngày TGCM)`,
      people: miss.map(brief),
    });
  }
  const status = checks.some((x) => !x.ok && x.level === "block")
    ? "blocked"
    : checks.some((x) => !x.ok)
      ? "warning"
      : "ready";
  return {
    view: {
      report: meta,
      title: meta.name.toUpperCase(),
      period: period(code, at),
      as_of: at,
      status,
      columns: cols,
      rows,
      checks,
      people: sel.length,
    },
    cells,
    people: sel.map((p) => p.st.id),
  };
}

function reportCaller(ctx: Ctx, code?: string) {
  const c = H.caller(ctx);
  if (c instanceof Response) return c;
  if (c.m.export !== "all")
    return problem(
      403,
      "forbidden",
      "báo cáo Sở: cần quyền xuất phạm vi toàn Trung tâm (FN-12, FN-20)",
    );
  const meta = code ? META.find((x) => x.code === code) : undefined;
  if (code && !meta)
    return problem(
      404,
      "hrm.report_unknown",
      "Không có biểu báo cáo Sở này (NS01–NS06)",
    );
  if (meta?.sensitive_fields.length && c.m["sensitive.view"] !== "all")
    return problem(
      403,
      "forbidden",
      `biểu ${code} có trường thẻ Thống kê BC Sở: cần quyền sensitive.view (BR-04)`,
    );
  return c;
}

function log(
  c: { ref: string },
  l: Partial<S["HrmReportLog"]> &
    Pick<S["HrmReportLog"], "action" | "report" | "as_of">,
): S["HrmReportLog"] {
  const meta = META.find((x) => x.code === l.report)!;
  const row: S["HrmReportLog"] = {
    id: uuid(),
    period: period(l.report, l.as_of),
    item_count: 0,
    sensitive_fields: meta.sensitive_fields,
    file_name: "",
    sha256: "",
    byte_size: 0,
    check_status: "",
    checks: [],
    actor: c.ref,
    recorded_at: new Date().toISOString(),
    export_id: null,
    drift: null,
    params: {},
    ...l,
  };
  LOGS.unshift(row);
  return row;
}

const at = (ctx: Ctx) => ctx.url.searchParams.get("at") || vnToday();

export const hrmReportRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/reports$/,
    handler: (ctx) => {
      const c = reportCaller(ctx);
      if (c instanceof Response) return c;
      return json({
        items: META.map((m) => {
          const ex = LOGS.filter(
            (l) => l.report === m.code && l.action === "export",
          );
          return { ...m, exports: ex.length, last_export: ex[0] ?? null };
        }),
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/reports\/([^/]+)$/,
    handler: (ctx) => {
      const c = reportCaller(ctx, ctx.params[0]);
      if (c instanceof Response) return c;
      const b = build(ctx.params[0]!, at(ctx));
      log(c, {
        action: "preview",
        report: ctx.params[0]!,
        as_of: b.view.as_of,
        item_count: b.people.length,
      });
      return json(b.view);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/reports\/([^/]+)\/export$/,
    handler: (ctx) => {
      const c = reportCaller(ctx, ctx.params[0]);
      if (c instanceof Response) return c;
      const body = (ctx.body ?? {}) as {
        as_of?: string;
        preparer_id?: string | null;
        signer_id?: string | null;
      };
      const day = body.as_of || vnToday();
      if (day > vnToday())
        return problem(422, "validation_failed", "ngày chốt sau hôm nay");
      const b = build(ctx.params[0]!, day);
      if (b.view.status === "blocked")
        return problem(
          422,
          "hrm.report_blocked",
          b.view.checks
            .filter((x) => !x.ok && x.level === "block")
            .map((x) => `${x.rule}: ${x.message}`)
            .join(" · "),
        );
      const sha = Array.from(crypto.getRandomValues(new Uint8Array(32)), (x) =>
        x.toString(16).padStart(2, "0"),
      ).join("");
      const l = log(c, {
        action: "export",
        report: ctx.params[0]!,
        as_of: day,
        item_count: b.people.length,
        file_name: `${ctx.params[0]}_${day}.xlsx`,
        sha256: sha,
        byte_size: 4096,
        check_status: b.view.status,
        checks: b.view.checks.map((x) => ({
          rule: x.rule,
          level: x.level,
          ok: x.ok,
          count: x.people.length,
        })),
        params: {
          ...(body.preparer_id ? { preparer: body.preparer_id } : {}),
          ...(body.signer_id ? { signer: body.signer_id } : {}),
        },
      });
      H.audit(c, "export", b.people, "all", {
        report: ctx.params[0]!,
        as_of: day,
      });
      return json(
        {
          id: l.id,
          file_name: l.file_name,
          sha256: sha,
          byte_size: 4096,
          status: b.view.status,
          checks: b.view.checks.map((x) => ({
            rule: x.rule,
            level: x.level,
            ok: x.ok,
            message: x.message,
            count: x.people.length,
          })),
          download: `/v1/hrm/report-exports/${l.id}/file`,
        },
        201,
      );
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/reports\/([^/]+)\/history$/,
    handler: (ctx) => {
      const c = reportCaller(ctx, ctx.params[0]);
      if (c instanceof Response) return c;
      return json({
        items: LOGS.filter(
          (l) =>
            l.report === ctx.params[0] &&
            (l.action === "export" || l.action === "download"),
        ),
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/reports\/([^/]+)\/cells$/,
    handler: (ctx) => {
      const c = reportCaller(ctx, ctx.params[0]);
      if (c instanceof Response) return c;
      if (!META.find((x) => x.code === ctx.params[0])!.aggregate)
        return problem(
          422,
          "validation_failed",
          "chỉ biểu thống kê NS05, NS06 có ô số",
        );
      const day = at(ctx);
      const b = build(ctx.params[0]!, day);
      const ids =
        b.cells[
          `${ctx.url.searchParams.get("row")}|${ctx.url.searchParams.get("col")}`
        ] ?? [];
      const pop = population(day);
      const items = ids
        .map((id) => pop.find((p) => p.st.id === id)!)
        .filter((p) => H.on(c, "profile.view", p.st, p.snap));
      log(c, {
        action: "drill",
        report: ctx.params[0]!,
        as_of: day,
        item_count: items.length,
      });
      return json({
        items: items.map((p) => ({
          id: p.st.id,
          code: p.st.code,
          type: p.st.type,
          name: p.st.profile.full_name,
          unit: p.snap.unit,
          unit_name: unitName(p.snap.unit),
          position: p.snap.position,
        })),
        hidden: ids.length - items.length,
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/report-exports\/([^/]+)\/file$/,
    handler: (ctx) => {
      const ex = LOGS.find(
        (l) => l.id === ctx.params[0] && l.action === "export",
      );
      if (!ex)
        return problem(
          404,
          "hrm.export_not_found",
          "Không có lần xuất báo cáo này",
        );
      const c = reportCaller(ctx, ex.report);
      if (c instanceof Response) return c;
      log(c, {
        action: "download",
        report: ex.report,
        as_of: ex.as_of,
        export_id: ex.id,
        drift: false,
        file_name: ex.file_name,
        sha256: ex.sha256,
        byte_size: ex.byte_size,
        item_count: ex.item_count,
      });
      return new Response(new Blob([`Tệp giả lập ${ex.file_name}`]), {
        status: 200,
        headers: {
          "Content-Type":
            "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
          "Cache-Control": "no-store",
          "Content-Disposition": `attachment; filename="${ex.file_name}"`,
          "X-Export-Drift": "false",
          "X-Export-Sha256": ex.sha256,
        },
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/dashboard(\/members)?$/,
    handler: (ctx) => {
      const c = H.caller(ctx);
      if (c instanceof Response) return c;
      if ((c.m["profile.view"] ?? "none") === "none")
        return problem(
          403,
          "forbidden",
          "hồ sơ nhân sự: không có quyền profile.view (FN-20)",
        );
      const day = at(ctx);
      const pop = population(day).filter((p) =>
        H.on(c, "profile.view", p.st, p.snap),
      );
      const members: Record<string, P[]> = {};
      const put = (k: string, p: P) => (members[k] ??= []).push(p);
      for (const p of pop) {
        if (
          p.snap.started_on &&
          p.snap.started_on.slice(0, 7) === day.slice(0, 7)
        )
          put("joined", p);
        if (
          p.snap.status === "left" &&
          (p.snap.ended_on ?? "").slice(0, 7) === day.slice(0, 7)
        )
          put("left", p);
        if (p.snap.status !== "active") continue;
        put(`type:${p.st.type}`, p);
        if (p.st.type === "vien_chuc") put("total", p);
        if (p.st.type === "can_bo_xa") continue;
        put("staff", p);
        put(`unit:${p.snap.unit}`, p);
        put(`gender:${p.st.profile.gender || "unknown"}`, p);
        const a = age(p.st.profile.birth_date, day);
        put(
          `age:${a === null ? "unknown" : a <= 30 ? "age_le30" : a <= 40 ? "age_31_40" : a <= 50 ? "age_41_50" : "age_ge51"}`,
          p,
        );
        put(`edu:${topDegree(p.st, day) || "none"}`, p);
        put(`grade:${p.snap.grade ? "CV" : "CON_LAI"}`, p);
        const lim = p.st.profile.gender === "nu" ? 57 : 61.5;
        if (a !== null && a >= lim) put("over_retire", p);
        else if (a !== null && a + 0.5 >= lim) put("retire_soon", p);
      }
      if (ctx.params[0]) {
        const metric = ctx.url.searchParams.get("metric") ?? "";
        const list = members[metric] ?? [];
        return json({
          total: list.length,
          items: list
            .slice(0, 50)
            .map((p) => ({
              id: p.st.id,
              code: p.st.code,
              type: p.st.type,
              name: p.st.profile.full_name,
              unit: p.snap.unit,
              unit_name: unitName(p.snap.unit),
              position: p.snap.position,
            })),
        });
      }
      const pick = (prefix: string) =>
        Object.fromEntries(
          Object.entries(members)
            .filter(([k]) => k.startsWith(prefix))
            .map(([k, v]) => [k.slice(prefix.length), v.length]),
        );
      const n = (k: string) => members[k]?.length ?? 0;
      return json({
        as_of: day,
        scope: c.m["profile.view"] ?? "none",
        total: n("total"),
        staff: n("staff"),
        by_type: pick("type:"),
        by_unit: H.units()
          .filter((u) => n(`unit:${u.code}`) > 0)
          .map((u) => ({
            key: u.code,
            name: u.name,
            count: n(`unit:${u.code}`),
          })),
        gender: pick("gender:"),
        age: pick("age:"),
        education: Object.entries(pick("edu:")).map(([k, v]) => ({
          key: k,
          name: k,
          count: v,
        })),
        grade_group: pick("grade:"),
        joined: n("joined"),
        left: n("left"),
        retire_soon: n("retire_soon"),
        over_retire: n("over_retire"),
        pii: true,
        elapsed_ms: 1,
      });
    },
  },
];
