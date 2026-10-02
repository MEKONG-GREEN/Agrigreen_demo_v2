// Bản port TS của reportcycle/domain (backend giả lập) phải cho đúng kết quả của Go: ca vàng do
// TestWebReportCycleFixture sinh (AG_UPDATE_WEB_FIXTURES=1 go test ./internal/platform/extension/reportcycle/domain).
import { describe, expect, it } from "vitest";
import { Decimal } from "@/kn/grid/decimal";
import {
  DEFAULT_CLOSE_AFTER_DAYS,
  MAX_PERIODS,
  PeriodError,
  STATUS_LABEL,
  defaultDeadline,
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
  type WeekNumbering,
} from "./domain";
import golden from "./fixtures/rc-golden.json";

interface G {
  status_labels: Record<string, string>;
  default_deadlines: Record<string, { offset_days: number; time: string }>;
  default_close_after_days: number;
  max_periods: number;
  split: {
    name: string;
    period_type: string;
    from_date: string;
    to_date: string;
    season_label: string;
    week_numbering: WeekNumbering;
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
  });

  it.each(g.split.map((c) => [c.name, c] as const))("FN-TI-02.02 chia kỳ: %s", (_n, c) => {
    expect(ruleTag(c.week_numbering)).toBe(c.rule_tag);
    let got: unknown[] = [];
    let err: { Field: string; Msg: string } | null = null;
    try {
      got = split(c.period_type, c.from_date, c.to_date, c.season_label, c.week_numbering, c.deadline, c.close_after_days);
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
    expect(changes.map(({ source: _s, ...x }) => x)).toEqual(c.changes);
    expect(communes).toBe(c.communes);
  });

  it("tỉ lệ % kế hoạch, năng suất bình quân gia quyền, lý do 10–n ký tự", () => {
    for (const c of g.ratio) expect(ratio(Decimal.parse(c.a)!, Decimal.parse(c.b)!).toString()).toBe(c.result);
    for (const c of g.yield) expect(yieldOf(Decimal.parse(c.a)!, Decimal.parse(c.b)!).toString()).toBe(c.result);
    for (const c of g.reason) expect(reason(c.input, c.max)).toEqual([c.out, c.ok]);
  });
});
