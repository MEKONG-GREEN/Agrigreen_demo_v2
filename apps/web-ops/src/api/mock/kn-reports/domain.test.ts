// Bản port TS của reportcycle/domain (backend giả lập) phải cho đúng kết quả của Go: ca vàng do
// TestWebReportCycleFixture sinh (AG_UPDATE_WEB_FIXTURES=1 go test ./internal/platform/extension/reportcycle/domain).
import { describe, expect, it } from "vitest";
import { Decimal } from "@/kn/grid/decimal";
import {
  DEFAULT_CLOSE_AFTER_DAYS,
  DEFAULT_RULE,
  MAX_PERIODS,
  PeriodError,
  STATUS_LABEL,
  defaultDeadline,
  parseRule,
  diff,
  ratio,
  reason,
  ruleTag,
  split,
  taskDisplay,
  taskOpen,
  taskPending,
  yieldOf,
  type Row,
  type Source,
  type StoredStatus,
} from "./domain";
import golden from "./fixtures/rc-golden.json";

interface G {
  status_labels: Record<string, string>;
  default_deadlines: Record<string, { offset_days: number; time: string }>;
  default_deadlines_v2: Record<string, { offset_days: number; time: string }>;
  default_close_after_days: number;
  max_periods: number;
  split: {
    name: string;
    period_type: string;
    from_date: string;
    to_date: string;
    season_label: string;
    rule: string;
    deadline: { offset_days: number; time: string };
    close_after_days: number;
    periods: unknown[];
    error: { Field: string; Msg: string } | null;
    rule_tag: string;
  }[];
  status: { status: StoredStatus; due_at: string; period_closed: boolean; reopened_until: string | null; now: string; display: string; label: string; open: boolean; pending: boolean }[];
  diff: { name: string; prev: Row[]; next: Row[]; prev_src: Record<string, Source>; next_src: Record<string, Source>; formula: string[]; changes: unknown[]; communes: number }[];
  ratio: { a: string; b: string; result: string }[];
  yield: { a: string; b: string; result: string }[];
  reason: { input: string; max: number; out: string; ok: boolean }[];
}
const g = golden as unknown as G;

describe("KN v7.7 P4 — bản port giả lập khớp ca vàng Go (reportcycle/domain)", () => {
  it("FN-TI-02.02: hằng số, hạn mặc định, nhãn trạng thái", () => {
    expect(STATUS_LABEL).toEqual(g.status_labels);
    expect(DEFAULT_CLOSE_AFTER_DAYS).toBe(g.default_close_after_days);
    expect(MAX_PERIODS).toBe(g.max_periods);
    for (const [pt, d] of Object.entries(g.default_deadlines)) expect(defaultDeadline(pt)).toEqual(d);
    for (const [pt, d] of Object.entries(g.default_deadlines_v2)) expect(defaultDeadline(pt, DEFAULT_RULE)).toEqual(d);
  });

  it.each(g.split.map((c) => [c.name, c] as const))("FN-TI-02.02 chia kỳ: %s", (_n, c) => {
    const rule = parseRule(c.rule);
    expect(ruleTag(rule)).toBe(c.rule_tag);
    let got: unknown[] = [];
    let err: { Field: string; Msg: string } | null = null;
    try {
      got = split(c.period_type, c.from_date, c.to_date, c.season_label, rule, c.deadline, c.close_after_days);
    } catch (e) {
      if (!(e instanceof PeriodError)) throw e;
      err = { Field: e.field, Msg: e.msg };
    }
    expect(err).toEqual(c.error);
    expect(got).toEqual(c.periods);
  });

  it("FN-XA-03.01: trạng thái hiển thị, mở, còn phải gửi", () => {
    for (const c of g.status) {
      const s = { status: c.status, dueAt: Date.parse(c.due_at), periodClosed: c.period_closed, reopenedUntil: c.reopened_until ? Date.parse(c.reopened_until) : null };
      const now = Date.parse(c.now);
      expect([taskDisplay(s, now), STATUS_LABEL[taskDisplay(s, now)], taskOpen(s, now), taskPending(s)]).toEqual([c.display, c.label, c.open, c.pending]);
    }
  });

  it.each(g.diff.map((c) => [c.name, c] as const))("FN-TI-02.05 so khác lần tổng hợp: %s", (_n, c) => {
    const [changes, communes] = diff(c.prev, c.next, c.prev_src, c.next_src, new Set(c.formula));
    expect(changes.map((x) => ({ commune: x.commune, row_key: x.row_key, letter: x.letter, old: x.old, new: x.new, kind: x.kind }))).toEqual(c.changes);
    expect(communes).toBe(c.communes);
  });

  it("tỉ lệ % kế hoạch, năng suất bình quân gia quyền, lý do 10–n ký tự", () => {
    for (const c of g.ratio) expect(ratio(Decimal.parse(c.a)!, Decimal.parse(c.b)!).toString()).toBe(c.result);
    for (const c of g.yield) expect(yieldOf(Decimal.parse(c.a)!, Decimal.parse(c.b)!).toString()).toBe(c.result);
    for (const c of g.reason) expect(reason(c.input, c.max)).toEqual([c.out, c.ok]);
  });
});

// D8 (CEO 13/10/2026) — lịch tuần theo phòng ban, như reportcycle/domain.ResolveCalendar.
describe("lịch tuần theo phòng ban (D8)", () => {
  it("CNTS Thứ Ba – Thứ Hai hiệu lực ngay; phòng khác theo lịch chung; tham số theo phòng đè lên", async () => {
    const d = await import("./domain");
    const cnts = d.calendarAt(d.DEPT_CNTS, "2026-10-01");
    expect([cnts.calendar_id, cnts.calendar_version, cnts.calendar_source, d.ruleTag(cnts.rule)]).toEqual([
      "chan_nuoi_thuy_san", 3, "builtin", "kn.period_rule/v2;week_start=tue;week=year;month=week_end;cal=chan_nuoi_thuy_san:v3",
    ]);
    const w = d.split("week", "2026-09-22", "2026-09-28", "", cnts.rule, d.defaultDeadline("week", cnts.rule), 3);
    expect(w.map((p) => p.label)).toEqual(["Tuần 39/2026"]); // BRD CNTS: tuần 39 = 22/09–28/09/2026
    expect(d.calendarAt("ke_hoach_tai_chinh", "2026-11-04").calendar_version).toBe(2);
    expect(d.calendarAt("ke_hoach_tai_chinh", "2026-10-28").calendar_version).toBe(1);
    expect(d.parseRule(d.ruleTag(cnts.rule))).toEqual(cnts.rule);
    d.DEPT_PERIOD_RULES.set("trong_trot", "kn.period_rule/v2;week_start=thu;cal=trong_trot:v4");
    try {
      const tt = d.calendarAt("trong_trot", "2026-01-01");
      expect([tt.calendar_version, tt.calendar_source, tt.rule.week_start]).toEqual([4, "department_param", 4]);
    } finally {
      d.DEPT_PERIOD_RULES.clear();
    }
  });
});
