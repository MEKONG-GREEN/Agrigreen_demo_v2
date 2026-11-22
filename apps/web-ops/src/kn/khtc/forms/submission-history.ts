import { unwrap, type ApiClient } from "@/api/client";
import { ApiProblem } from "@/api/problem";
import type { components } from "@/api/generated/application";

type S = components["schemas"];
export type SubmissionHeader = S["KHTCFormSubmissionHeader"];
export type SubmissionHistory = S["KHTCFormSubmissionHistory"];
export type SubmissionSnapshot = S["KHTCFormSubmissionSnapshot"];
export type HistoryRoute = { periodId: string; department: string; submissionId?: string; compareWith?: string };

const UUID = /^(?!00000000-0000-0000-0000-000000000000$)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const DEPARTMENTS = new Set(["trong_trot", "chan_nuoi_thuy_san", "thong_tin_truyen_thong", "hanh_chinh_tong_hop", "ke_hoach_tai_chinh"]);
const validID = (value: unknown): value is string => typeof value === "string" && value.length === 36 && UUID.test(value);
// Recorded actor references are display values; resource and route IDs remain UUIDs.
const validActorRef = (value: unknown): value is string => typeof value === "string" && value.trim().length > 0;

export function parseHistoryRoute(period: unknown, submission: unknown, query: URLSearchParams): HistoryRoute | null {
  if (!validID(period) || submission !== undefined && !validID(submission)
    || [...query.keys()].some(key => !["department", "compare_with"].includes(key) || query.getAll(key).length !== 1)) return null;
  const department = query.get("department") ?? "", compare = query.get("compare_with");
  if (!DEPARTMENTS.has(department) || compare !== null && (!submission || !validID(compare) || compare.toLowerCase() === submission.toLowerCase())) return null;
  return { periodId: period.toLowerCase(), department, ...(submission ? { submissionId: submission.toLowerCase() } : {}), ...(compare ? { compareWith: compare.toLowerCase() } : {}) };
}

export function historyHref(route: HistoryRoute): string {
  const query = new URLSearchParams({ department: route.department });
  if (route.compareWith) query.set("compare_with", route.compareWith);
  return `/kn/khtc/ra-soat/${route.periodId}/lan-gui${route.submissionId ? `/${route.submissionId}` : ""}?${query}`;
}

function guard(ok: unknown): asserts ok {
  if (!ok) throw new ApiProblem({ type: "about:blank", title: "Không đối chiếu được lịch sử phiếu trong ngữ cảnh hiện tại", code: "extension.out_of_scope", status: 403 });
}
function checkHeader(sub: SubmissionHeader) {
  guard(sub && validID(sub.id) && Number.isSafeInteger(sub.attempt) && sub.attempt > 0 && validActorRef(sub.submitter_ref)
    && typeof sub.submitted_at === "string" && Number.isFinite(Date.parse(sub.submitted_at))
    && ["", "accepted", "returned"].includes(sub.decision));
  guard(sub.decision === "" ? sub.reviewer_ref === null && sub.reviewed_at === null && !sub.review_note
    : validActorRef(sub.reviewer_ref) && typeof sub.reviewed_at === "string" && Number.isFinite(Date.parse(sub.reviewed_at)));
}

export function createSubmissionHistoryApi(api: ApiClient) {
  return {
    async history(periodId: string, department: string): Promise<SubmissionHistory> {
      const data = await unwrap(api.GET("/v1/extension/khtc/periods/{id}/forms/{dept}/submissions", { params: { path: { id: periodId, dept: department } } }));
      guard(data?.period?.id === periodId && data.department === department && Array.isArray(data.items));
      data.items.forEach(checkHeader);
      guard(new Set(data.items.map(sub => sub.id)).size === data.items.length && new Set(data.items.map(sub => sub.attempt)).size === data.items.length);
      return data;
    },
    async submission(periodId: string, department: string, submissionId: string): Promise<SubmissionSnapshot> {
      const data = await unwrap(api.GET("/v1/extension/khtc/periods/{id}/forms/{dept}/submissions/{submissionID}", { params: { path: { id: periodId, dept: department, submissionID: submissionId } } }));
      guard(data?.period?.id === periodId && data.department === department && data.submission?.id === submissionId);
      checkHeader(data.submission);
      guard(Array.isArray(data.rows) && Array.isArray(data.sections) && data.content && typeof data.content.cells === "object"
        && data.content.cells !== null && typeof data.content.texts === "object" && data.content.texts !== null && Array.isArray(data.content.adjustments));
      guard(validID(data.sheet_id) && /^[0-9a-f]{64}$/.test(data.content_hash)
        && (data.plan_version_ref === null ? data.plan === null : validID(data.plan_version_ref)
          && data.plan?.version_id === data.plan_version_ref && Number.isSafeInteger(data.plan.version) && data.plan.version > 0));
      return data;
    },
  };
}

export type SnapshotField = { key: string; label: string; value: string };
const CELL_LABELS = { done: "Thực hiện trong kỳ", status: "Tình trạng", content: "Nội dung, xã/điểm thực hiện", people: "Số người tham gia", budget: "Kinh phí" } as const;

/** Compare stored input strings; never parse decimals or recompute old cumulative/percentage cells. */
export function snapshotFields(snapshot: SubmissionSnapshot): SnapshotField[] {
  const fields: SnapshotField[] = [];
  const add = (path: (string | number)[], label: string, value: string | number | undefined) => {
    if (value !== undefined) fields.push({ key: JSON.stringify(path), label, value: String(value) });
  };
  const rows = new Map(snapshot.rows.map(row => [row.entry_ref, row.name]));
  const sections = new Map(snapshot.sections.map(section => [section.key, section.title]));
  for (const [id, cell] of Object.entries(snapshot.content.cells).sort(([a], [b]) => a.localeCompare(b))) {
    for (const key of Object.keys(CELL_LABELS) as (keyof typeof CELL_LABELS)[]) {
      add(["cell", id, key], `${rows.get(id) || id} · ${CELL_LABELS[key]}`, cell[key]);
    }
  }
  for (const [key, value] of Object.entries(snapshot.content.texts).sort(([a], [b]) => a.localeCompare(b))) add(["text", key], sections.get(key) || key, value);
  snapshot.content.adjustments.forEach((item, index) => {
    const labels = { entry_ref: "Mã hạng mục", period_id: "Mã kỳ", period_name: "Tên kỳ", period_year: "Năm kỳ", amount: "Số điều chỉnh", reason: "Lý do" };
    for (const key of Object.keys(labels) as (keyof typeof labels)[]) add(["adjustment", index, key], `Điều chỉnh dòng ${index + 1} · ${labels[key]}`, item[key]);
  });
  add(["adhoc", "request"], "Mã yêu cầu bổ sung", snapshot.content.adhoc_request_id);
  for (const [key, value] of Object.entries(snapshot.content.adhoc_values ?? {}).sort(([a], [b]) => a.localeCompare(b))) add(["adhoc", "value", key], `Chỉ tiêu bổ sung · ${key}`, value);
  add(["adhoc", "text"], "Lời văn bổ sung", snapshot.content.adhoc_text);
  return fields;
}

export function compareSnapshots(selected: SubmissionSnapshot, other?: SubmissionSnapshot) {
  const current = new Map(snapshotFields(selected).map(field => [field.key, field]));
  const previous = new Map(other ? snapshotFields(other).map(field => [field.key, field]) : []);
  return [...new Set([...current.keys(), ...previous.keys()])].map(key => ({
    key, label: current.get(key)?.label ?? previous.get(key)!.label,
    selected: current.get(key)?.value, other: previous.get(key)?.value,
    changed: !!other && current.get(key)?.value !== previous.get(key)?.value,
  }));
}
