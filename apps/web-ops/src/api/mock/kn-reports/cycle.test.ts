// Backend giả lập vòng đời báo cáo KN v7.7 (P4) qua HTTP giả lập + client web (src/kn/cycle/api.ts):
// Tỉnh phát hành → Xã nhập/gửi 2 xã → Tỉnh yêu cầu điều chỉnh → Xã gửi lại → "Cập nhật" có nhật ký ô → chốt
// (Hệ thống tổng hợp) → TW xem dashboard chỉ đọc; Chi cục xuất + gửi (202); quy tắc lỗi như Go.
import { beforeEach, describe, expect, it } from "vitest";
import { createCycleApi } from "@/kn/cycle/api";
import { ApiProblem } from "@/api/problem";
import { mockFetchNoDelay } from "../handler";
import { resetMockForms } from "../forms/routes";
import { resetReportCycle } from "./store";

// Thứ Sáu 02/10/2026 10:00 giờ Việt Nam.
let now = Date.parse("2026-10-02T03:00:00Z");

async function clientFor(personaId: string, assignmentId: string) {
  const ctxRes = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: { Authorization: `Bearer mock.${personaId}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-${personaId}-${Math.random()}` },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: "trong_trot" }),
    }),
  );
  const ctx = (await ctxRes.json()) as { context_id: string };
  return createCycleApi({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => ctx.context_id, fetch: mockFetchNoDelay });
}

const code = async (p: Promise<unknown>) => {
  try {
    await p;
    return "ok";
  } catch (e) {
    return e instanceof ApiProblem ? `${e.status} ${e.code}` : String(e);
  }
};

beforeEach(() => {
  now = Date.parse("2026-10-02T03:00:00Z");
  resetMockForms();
  resetReportCycle(() => now);
});

describe("vòng đời báo cáo giả lập (P4)", () => {
  it("dữ liệu khởi đầu: đợt Thu Đông 2026, kỳ đã qua tự chốt + tổng hợp Hệ thống, dashboard có số", async () => {
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    const batches = (await tinh.batches()).items;
    expect(batches.map((b) => b.season_label)).toEqual(["Thu Đông 2026"]);
    const { periods } = await tinh.batch(batches[0]!.id);
    expect(periods.map((p) => [p.label, p.status])).toEqual([
      ["Tuần 1", "closed"],
      ["Tuần 2", "closed"],
      ["Tuần 3", "open"],
      ["Tuần 4", "open"],
    ]);
    const runs = (await tinh.runs(periods[0]!.id)).items;
    expect(runs.some((r) => r.trigger === "system" && r.actor_display === "Hệ thống")).toBe(true);
    const d = await tinh.dashboard();
    expect(d.empty).toBe(false);
    expect((d.cards.sown_area as { value: string }).value).not.toBeNull();
    const xa = await clientFor("kn-an", "asg-an-xa");
    const todo = await xa.tasks({ status: "" as never });
    expect(todo.items.every((t) => ["91301", "91302"].includes(t.commune_code))).toBe(true);
    expect(todo.counts.todo).toBeGreaterThan(0);
  });

  it("FN-TI-02.02 → 02.05 → TW: phát hành, nhập 2 xã, điều chỉnh, gửi lại, Cập nhật có nhật ký ô, chốt, TW chỉ xem", async () => {
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    const forms = (await tinh.reportForms("weekly")).items;
    const t01 = forms.find((f) => f.code === "T01")!;
    expect(t01.provincial_columns.map((c) => c.letter)).toEqual(["D"]);
    const input = {
      report_type: "weekly" as const,
      template_ids: [t01.template_id],
      scope: { mode: "communes" as const, communes: ["91301", "91302", "91307"] },
      from_date: "2026-09-28",
      to_date: "2026-10-18",
      season_label: "Đợt kiểm thử",
      provincial_values: { [t01.template_id]: { "91301": { D: "126" }, "91302": { D: "290" } } },
    };
    const pv = await tinh.preview(input);
    expect([pv.form_count, pv.commune_count, pv.period_count, pv.task_count]).toEqual([1, 3, 3, 9]);
    expect(pv.communes_without_officer).toEqual(["91307"]);
    expect(pv.periods[0]).toMatchObject({ label: "Tuần 1", range_label: "28/09–04/10/2026", due_at: "2026-10-06T10:00:00Z" });
    expect(pv.period_rule).toBe("kn.period_rule/v1-tam;week=season");
    // W11.03: đợt bắt đầu từ ngày hiệu lực tham số lịch kỳ v2 (04/11/2026) chia theo tuần báo cáo Thứ Tư–Thứ Ba.
    const v2 = await tinh.preview({ ...input, from_date: "2026-11-04", to_date: "2026-11-17" });
    expect(v2.period_rule).toBe("kn.period_rule/v2;week_start=wed;week=year;month=week_end");
    expect(v2.periods.map((p) => [p.label, p.range_label, p.due_at])).toEqual([
      ["Tuần 45/2026", "04/11–10/11/2026", "2026-11-11T05:00:00Z"],
      ["Tuần 46/2026", "11/11–17/11/2026", "2026-11-18T05:00:00Z"],
    ]);
    const { batch, periods } = await tinh.issue(input);
    // Cấp Quốc gia chỉ xem.
    const tw = await clientFor("kn-quan", "asg-quan-tw");
    expect(await code(tw.issue(input))).toBe("403 extension.read_only");

    const xa = await clientFor("kn-an", "asg-an-xa");
    const mine = (await xa.tasks({ batch_id: batch.id, period_id: periods[0]!.id })).items;
    expect(mine.map((t) => [t.commune_code, t.status_label])).toEqual([
      ["91301", "Chưa nhập"],
      ["91302", "Chưa nhập"],
    ]);
    const [t1, t2] = mine as [(typeof mine)[0], (typeof mine)[0]];
    const detail = await xa.task(t1.id);
    expect(detail.layout.open_slots).toEqual([1]);
    expect(detail.provincial_values).toEqual({ D: "126" });
    // Nháp → gửi; khe kỳ khác không nhập được.
    await xa.submit(t1.id, { kind: "draft", rows: [{ input: { F: "20" } }] });
    expect(await code(xa.submit(t1.id, { kind: "submitted", rows: [{ input: { G: "5" } }] }))).toBe("422 extension.submission_invalid");
    const s1 = await xa.submit(t1.id, { kind: "submitted", rows: [{ input: { E: "126", F: "28" } }] });
    expect(s1.task.status).toBe("submitted");
    expect(s1.submission.rows[0]!.cells.M).toBe("28");
    await xa.submit(t2.id, { kind: "submitted", rows: [{ input: { F: "30" } }] });
    expect((await xa.history(t1.id)).items.map((s) => s.kind)).toEqual(["submitted", "draft"]);

    // Tỉnh: trạng thái nộp, nhắc xã chưa có cán bộ → bỏ qua.
    const board = await tinh.status(batch.id, { period_id: periods[0]!.id });
    expect(board.counts).toMatchObject({ submitted: 2, not_started: 1, no_officer: 1 });
    expect(await code(tinh.remind(batch.id, { period_id: periods[0]!.id, all_pending: true, message: "Nộp T01" }))).toBe("422 validation_failed");

    // Cập nhật lần 1: mọi ô Gửi mới / Tự tính.
    const r1 = await tinh.consolidate(periods[0]!.id);
    expect(r1.runs).toHaveLength(1);
    expect((await tinh.consolidate(periods[0]!.id)).message).toBe("Dữ liệu đã mới nhất");
    // Yêu cầu điều chỉnh (lý do bắt buộc) → xã gửi lại.
    expect(await code(tinh.requestAdjustment(t2.id, "ngắn"))).toBe("422 validation_failed");
    await tinh.requestAdjustment(t2.id, "Kiểm tra lại diện tích tuần 1");
    const back = (await xa.tasks({ batch_id: batch.id, status: "adjustment_requested" })).items;
    expect(back.map((t) => [t.commune_code, t.adjustment_reason])).toEqual([["91302", "Kiểm tra lại diện tích tuần 1"]]);
    await xa.submit(t2.id, { kind: "submitted", rows: [{ input: { F: "25" } }] });
    const pend = await tinh.pending(periods[0]!.id);
    expect(pend.items).toMatchObject([{ commune_code: "91302", kind: "adjusted" }]);
    const r2 = await tinh.consolidate(periods[0]!.id);
    const ch = await tinh.runChanges(r2.runs[0]!.id);
    expect(ch.items.map((c) => [c.commune_code, c.letter, c.old, c.new, c.kind])).toEqual([
      ["91302", "F", "30", "25", "adjusted"],
      ["91302", "M", "30", "25", "computed"],
    ]);
    const view = await tinh.consolidation({ period_id: periods[0]!.id });
    expect(view.status_label).toBe("Tạm tính");
    expect(view.changed_cells.length).toBe(2);
    const province = view.totals.find((t) => t.kind === "province")!;
    expect(province.cells.M).toBe("53");
    // Chốt kỳ: Hệ thống tổng hợp; gửi sau chốt bị từ chối; mở lại cho một xã rồi gửi được.
    await tinh.close(periods[0]!.id);
    expect(await code(xa.submit(t1.id, { kind: "submitted", rows: [{ input: { F: "29" } }] }))).toBe("409 extension.period_closed");
    expect(await code(tinh.requestAdjustment(t1.id, "Sau chốt kỳ phải mở lại"))).toBe("409 extension.adjustment_after_close");
    const until = new Date(now + 2 * 86_400_000).toISOString();
    const re = await tinh.reopen(periods[0]!.id, { communes: ["91301"], reason: "Mở lại để sửa số tuần 1", until });
    expect(re.task_ids).toHaveLength(1);
    await xa.submit(t1.id, { kind: "submitted", rows: [{ input: { F: "29" } }] });
    await tinh.close(periods[0]!.id); // chốt lại
    const runs = (await tinh.runs(periods[0]!.id)).items;
    expect(runs[0]).toMatchObject({ trigger: "system" });

    // TW: dashboard theo tỉnh, chỉ đọc.
    expect(await code(tw.dashboard())).toBe("422 validation_failed");
    const d = await tw.dashboard({ province: "91", period_id: periods[0]!.id });
    expect(d.read_only).toBe(true);
    expect((d.cards.sown_area as { value: string }).value).toBe("54");
    expect(await code(tw.consolidate(periods[0]!.id))).toBe("403 extension.read_only");

    // Chi cục: xuất (Google Sheet tắt), gửi 202 → đã gửi.
    expect(await code(tinh.exportDispatch({ period_id: periods[0]!.id, formats: ["gsheet"] }))).toBe("409 reports.gsheet_disabled");
    const exp = await tinh.exportDispatch({ period_id: periods[0]!.id, formats: ["xlsx", "pdf"] });
    expect(exp.files.map((f) => f.file_name)).toEqual(["T01_Tuần 1.xlsx", "T01_Tuần 1.pdf"]);
    const sent = await tinh.send(exp.dispatch.id, {});
    expect(sent.dispatch.status).toBe("sending");
    now += 5000;
    expect((await tinh.dispatch(exp.dispatch.id)).dispatch.status).toBe("sent");
    expect((await tinh.download(exp.dispatch.id, exp.files[0]!.id)).url).toMatch(/^data:application\/vnd/);
  });
});
