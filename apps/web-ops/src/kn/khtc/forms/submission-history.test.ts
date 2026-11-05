import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { compareSnapshots, createSubmissionHistoryApi, historyHref, parseHistoryRoute } from "./submission-history";
import { ACTOR242, DEPT242, FIRST242, ITEM242, PERIOD242, PLAN242, SECOND242, history242, snapshot242 } from "./history242-testdata";

describe("history242 — nguồn HTTP và định danh", () => {
  function apiFor(body: unknown, requests: Request[] = []) {
    return createSubmissionHistoryApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.tinh", getContextId: () => "history242",
      fetch: async request => { requests.push(request); return Response.json(body); } }));
  }
  it("chỉ GET đúng period/department/submission, không gửi mutation hay idempotency key", async () => {
    const requests: Request[] = [], snapshot = snapshot242();
    expect(await apiFor(snapshot, requests).submission(PERIOD242, DEPT242, FIRST242)).toEqual(snapshot);
    expect(requests).toHaveLength(1);
    expect(new URL(requests[0]!.url).pathname).toBe(`/v1/extension/khtc/periods/${PERIOD242}/forms/${DEPT242}/submissions/${FIRST242}`);
    expect(requests[0]!.method).toBe("GET"); expect(requests[0]!.headers.has("Idempotency-Key")).toBe(false);
  });
  it.each(["period", "department", "submission", "sender", "reviewer", "hash", "plan"])("từ chối response sai %s trước khi dựng nội dung", async field => {
    const data = snapshot242();
    if (field === "period") data.period.id = SECOND242;
    if (field === "department") data.department = "hanh_chinh_tong_hop";
    if (field === "submission") data.submission.id = SECOND242;
    if (field === "sender") data.submission.submitter_ref = "";
    if (field === "reviewer") data.submission.reviewer_ref = null;
    if (field === "hash") data.content_hash = "not-a-hash";
    if (field === "plan") data.plan!.version_id = ACTOR242;
    await expect(apiFor(data).submission(PERIOD242, DEPT242, FIRST242)).rejects.toMatchObject({ code: "extension.history_resource_mismatch" });
  });
  it("không nhận header khác phạm vi hoặc trùng định danh/attempt", async () => {
    const data = history242(); data.items.push({ ...data.items[0]! });
    await expect(apiFor(data).history(PERIOD242, DEPT242)).rejects.toMatchObject({ code: "extension.history_resource_mismatch" });
    data.items.pop(); data.department = "hanh_chinh_tong_hop";
    await expect(apiFor(data).history(PERIOD242, DEPT242)).rejects.toMatchObject({ status: 403 });
  });
});

describe("history242 — route và so sánh không sửa nguồn", () => {
  it.each(["1e100000", "1e-100000", "9".repeat(10000), "00000000-0000-0000-0000-000000000000"])("loại mã không hợp lệ trước GET", id => {
    expect(parseHistoryRoute(PERIOD242, id, new URLSearchParams({ department: DEPT242 }))).toBeNull();
  });
  it.each(["department=trong_trot&department=hanh_chinh_tong_hop", "department=unknown", "department=trong_trot&unknown=1", `department=${DEPT242}&compare_with=${FIRST242}`])("query sai không chọn lại tài nguyên: %s", query => {
    expect(parseHistoryRoute(PERIOD242, FIRST242, new URLSearchParams(query))).toBeNull();
  });
  it("deep link roundtrip giữ đúng hai ID và phòng", () => {
    const route = { periodId: PERIOD242, department: DEPT242, submissionId: FIRST242, compareWith: SECOND242 };
    const href = new URL(historyHref(route), "http://mock");
    expect(parseHistoryRoute(PERIOD242, FIRST242, href.searchParams)).toEqual(route);
    expect(parseHistoryRoute(PERIOD242, undefined, href.searchParams)).toBeNull();
  });
  it("đánh dấu thêm/xóa/sửa ô, lời văn và thứ tự điều chỉnh, không mutate hai snapshot", () => {
    const first = snapshot242(), second = snapshot242(SECOND242, "12");
    first.content.texts.old_only = "Lời văn cũ"; second.content.texts.summary = "Lời văn mới";
    second.content.cells[ITEM242]!.budget = "9007199254740993.000001";
    first.content.adjustments = [{ entry_ref: ITEM242, period_id: PLAN242, amount: "-5.000001", reason: "Ghi thừa", period_year: 2030 }];
    const bytes = JSON.stringify([first, second]); const fields = compareSnapshots(first, second);
    expect(fields.find(row => row.key === JSON.stringify(["cell", ITEM242, "done"]))).toMatchObject({ selected: "10", other: "12", changed: true });
    expect(fields.find(row => row.key === JSON.stringify(["cell", ITEM242, "people"]))?.changed).toBe(false);
    expect(fields.find(row => row.key === JSON.stringify(["cell", ITEM242, "budget"]))?.other).toBe("9007199254740993.000001");
    expect(fields.find(row => row.key === JSON.stringify(["text", "old_only"]))).toMatchObject({ selected: "Lời văn cũ", other: undefined, changed: true });
    expect(fields.find(row => row.key === JSON.stringify(["adjustment", 0, "amount"]))).toMatchObject({ selected: "-5.000001", other: undefined, changed: true });
    expect(JSON.stringify([first, second])).toBe(bytes);
  });
  it.each(["1e100000", "1e-100000", "9".repeat(10000)])("giữ giá trị đã lưu dưới dạng chữ, không parse số tự tính", value => {
    const fields = compareSnapshots(snapshot242(FIRST242, value));
    expect(fields.find(row => row.key === JSON.stringify(["cell", ITEM242, "done"]))?.selected).toBe(value);
    expect(fields.every(row => !row.changed)).toBe(true);
  });
});
