// Backend giả lập vòng đời báo cáo KN v7.7 (P4) qua HTTP giả lập + client web (src/kn/cycle/api.ts):
// Tỉnh phát hành → Xã nhập/gửi 2 xã → Tỉnh yêu cầu điều chỉnh → Xã gửi lại → "Cập nhật" có nhật ký ô → chốt
// (Hệ thống tổng hợp) → TW xem dashboard chỉ đọc; Chi cục xuất + gửi (202); quy tắc lỗi như Go.
import { beforeEach, describe, expect, it } from "vitest";
import { createCycleApi, RawFile } from "@/kn/cycle/api";
import { ApiProblem } from "@/api/problem";
import { mockFetchNoDelay } from "../handler";
import { resetMockForms } from "../forms/routes";
import { KSGM_WEEK_STARTS, defaultPeriod, resetReportCycle } from "./store";

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

  it("đầu tuần 00:00–07:00 giờ VN: kỳ tuần mới đã bắt đầu theo ngày VN (badge 'cần làm' và kỳ mặc định không lùi về tuần trước)", async () => {
    const badges = async () => {
      const ctxRes = await mockFetchNoDelay(
        new Request("http://mock/v1/me/context", {
          method: "POST",
          headers: { Authorization: "Bearer mock.kn-an", "Content-Type": "application/json", "Idempotency-Key": `ctx-an-${Math.random()}` },
          body: JSON.stringify({ assignment_id: "asg-an-xa", department_code: "trong_trot" }),
        }),
      );
      const { context_id } = (await ctxRes.json()) as { context_id: string };
      const res = await mockFetchNoDelay(new Request("http://mock/v1/me/menu-badges", { headers: { Authorization: "Bearer mock.kn-an", "X-AG-Context": context_id } }));
      return ((await res.json()) as { counts: Record<string, number> }).counts["extension.report_tasks.todo"];
    };
    // Thứ Hai 26/10/2026 10:00 giờ VN: tuần hiện tại 26/10–01/11, 2 xã còn T04 (nháp / chưa nhập).
    now = Date.parse("2026-10-26T03:00:00Z");
    resetReportCycle(() => now);
    expect(await badges()).toBe(2);
    // Thứ Hai 26/10/2026 01:00 giờ VN (= Chủ Nhật 25/10 18:00 UTC): kỳ đã bắt đầu, số đếm phải như trên.
    now = Date.parse("2026-10-25T18:00:00Z");
    resetReportCycle(() => now);
    expect(await badges()).toBe(2);
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    const batch = (await tinh.batches()).items[0]!;
    expect(defaultPeriod(batch.id, now)?.from_date).toBe("2026-10-26");
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
    expect([pv.calendar_id, pv.calendar_version, pv.calendar_source]).toEqual(["trong_trot", 1, "legacy"]); // D8
    // W11.03: đợt bắt đầu từ ngày hiệu lực tham số lịch kỳ v2 (04/11/2026) chia theo tuần báo cáo Thứ Tư–Thứ Ba.
    const v2 = await tinh.preview({ ...input, from_date: "2026-11-04", to_date: "2026-11-17" });
    expect(v2.period_rule).toBe("kn.period_rule/v2;week_start=wed;week=year;month=week_end;cal=trong_trot:v2"); // D8: lịch của phòng
    expect([v2.calendar_id, v2.calendar_version, v2.calendar_source]).toEqual(["trong_trot", 2, "global_param"]);
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

describe("W11.05b: kỳ ngày / tuần KSGM / quý ở Phát hành biểu + khóa kỳ lưu khi phát hành", () => {
  it("chọn loại kỳ theo loại báo cáo, ranh giới KSGM theo tham số, khóa kỳ lưu ở kỳ đã phát hành", async () => {
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    const t01 = (await tinh.reportForms("weekly")).items.find((f) => f.code === "T01")!;
    const base = {
      report_type: "weekly" as const,
      template_ids: [t01.template_id],
      scope: { mode: "communes" as const, communes: ["91301"] },
      season_label: "Đợt KSGM",
    };
    // Lịch v1 (trước 04/11/2026) không có kỳ ngày/KSGM/quý.
    expect(await code(tinh.preview({ ...base, period_type: "ksgm_week", from_date: "2026-10-01", to_date: "2026-10-31" }))).toBe("422 validation_failed");
    // Loại kỳ không thuộc loại báo cáo.
    expect(await code(tinh.preview({ ...base, period_type: "quarter", from_date: "2026-11-04", to_date: "2026-12-31" }))).toBe("422 validation_failed");
    // Tuần KSGM: tháng 11 mặc định 1, 8, 15, 22; tháng 12 Cấp Tỉnh sửa ranh giới (tham số extension.ksgm_week_starts:2026-12).
    KSGM_WEEK_STARTS.set("2026-12", "1,10,16,23");
    try {
      const pv = await tinh.preview({ ...base, period_type: "ksgm_week", from_date: "2026-11-04", to_date: "2026-12-31" });
      expect(pv.period_type).toBe("ksgm_week");
      expect(pv.periods.map((p) => [p.period_key, p.range_label])).toEqual([
        ["2026-11-K1", "04/11–07/11/2026"],
        ["2026-11-K2", "08/11–14/11/2026"],
        ["2026-11-K3", "15/11–21/11/2026"],
        ["2026-11-K4", "22/11–30/11/2026"],
        ["2026-12-K1", "01/12–09/12/2026"],
        ["2026-12-K2", "10/12–15/12/2026"],
        ["2026-12-K3", "16/12–22/12/2026"],
        ["2026-12-K4", "23/12–31/12/2026"],
      ]);
      expect(pv.periods[0]!.week_year).toBeNull();
      const { batch, periods } = await tinh.issue({ ...base, period_type: "ksgm_week", from_date: "2026-12-01", to_date: "2026-12-31" });
      expect(batch.period_type).toBe("ksgm_week");
      expect(periods.map((p) => p.period_key)).toEqual(["2026-12-K1", "2026-12-K2", "2026-12-K3", "2026-12-K4"]);
      // Khóa đã lưu không đổi khi tham số đổi về sau.
      KSGM_WEEK_STARTS.set("2026-12", "1,8,15,22");
      const again = await tinh.batch(batch.id);
      expect(again.periods.map((p) => p.period_key)).toEqual(periods.map((p) => p.period_key));
      expect(again.periods[1]).toMatchObject({ period_key: "2026-12-K2", from_date: "2026-12-10", to_date: "2026-12-15", week_year: null });
    } finally {
      KSGM_WEEK_STARTS.clear();
    }
    // Kỳ ngày của báo cáo tuần: hạn 09:00 hôm sau; tuần báo cáo mang week_year.
    const day = await tinh.preview({ ...base, period_type: "day", from_date: "2026-11-04", to_date: "2026-11-06" });
    expect(day.periods.map((p) => [p.period_key, p.label, p.due_at])).toEqual([
      ["2026-11-04", "Ngày 04/11/2026", "2026-11-05T02:00:00Z"],
      ["2026-11-05", "Ngày 05/11/2026", "2026-11-06T02:00:00Z"],
      ["2026-11-06", "Ngày 06/11/2026", "2026-11-07T02:00:00Z"],
    ]);
    const wk = await tinh.preview({ ...base, from_date: "2026-11-04", to_date: "2026-11-10" });
    expect(wk.periods[0]).toMatchObject({ period_key: "2026-W45", iso_week: 45, week_year: 2026 });
    // Báo cáo tháng chia quý.
    const monthly = (await tinh.reportForms("monthly")).items;
    if (monthly.length) {
      const q = await tinh.preview({ ...base, report_type: "monthly", template_ids: [monthly[0]!.template_id], period_type: "quarter", from_date: "2026-11-04", to_date: "2027-03-30" });
      expect(q.periods.map((p) => p.period_key)).toEqual(["2026-Q4", "2027-Q1"]);
    }
  });
});

// BF-TT-FORMS (#107, UP-04): Nhập từ Excel trên màn nhập Cấp Xã — giả lập đọc CSV có hàng chữ cái, không ghi; Cấp Tỉnh không nhập.
describe("Nhập từ Excel (grid-import) giả lập", () => {
  it("đọc dòng dữ liệu sau hàng chữ cái + dòng loại cột, dừng ở Yêu cầu biểu mẫu; tệp sai bố cục → 422; Cấp Tỉnh → 403", async () => {
    const xa = await clientFor("kn-an", "asg-an-xa");
    const t = (await xa.tasks({ status: "" as never })).items[0]!;
    const csv = ["TIẾN ĐỘ", "A,B,C,D,E", "Cố định,Cố định,Cố định,Tỉnh giao,Nhập", "1,Long Xuyên,Mỹ Hòa Hưng,,12.5", "", "2,,Bình Đức,,3", "*Yêu cầu biểu mẫu: x", "9,,Lạ,,1"].join("\n");
    const r = await xa.importGrid(t.id, new RawFile(csv, "text/csv; charset=utf-8"));
    expect(r.rows).toEqual([
      { line: 4, cells: { A: "1", B: "Long Xuyên", C: "Mỹ Hòa Hưng", E: "12.5" } },
      { line: 6, cells: { A: "2", C: "Bình Đức", E: "3" } },
    ]);
    expect(await code(xa.importGrid(t.id, new RawFile("Xã,Diện tích\nA,1\n", "text/csv")))).toBe("422 extension.grid_import_invalid");
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    expect(await code(tinh.importGrid(t.id, new RawFile(csv, "text/csv")))).toMatch(/^403 /);
  });
});
