"use client";

// Màn phiếu "Báo cáo cho KHTC" (W11.63 rút gọn):
//  - KhtcFormEntryScreen  — /kn/khtc/phieu/{period_id}: phòng nhập phiếu (FN-PN-03.02). Phòng lấy từ ngữ cảnh phiên; Admin,
//    Phòng KHTC thêm ?department=. Hộp phiếu của phòng (W11.11) dẫn tới đây.
//  - "Phiếu kỳ này" (FN-PN-03.01) là màn của W11.11 (/kn/bao-cao-khtc); /kn/khtc/phieu dùng lại màn đó.
//  - KhtcFormReviewScreen — /kn/khtc/ra-soat/{period_id}?department=: KHTC rà soát (FN-TI-02.04) — yêu cầu bổ sung (bắt
//    buộc nội dung), xác nhận, phiếu trước / tiếp theo. Lịch sử và so sánh nội dung đã gửi có trang chỉ đọc riêng (#242).
import { useParams, useSearchParams, useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import { FORM_TONE, createKhtcPeriodApi, vnDate } from "../periods/api";
import { ApiProblem } from "@/api/problem";
import { newIdempotencyKey, outcomeUnknown } from "@/api/idempotency";
import { SelectField, TextField } from "@/components/form";
import reviewStyles from "./KhtcFormReview.module.css";
import { DetailPage } from "@/components/patterns/detail";
import { useInboxAuthorityKey } from "../inbox/inbox-workspace";
import { INBOX_BASE, parseInboxRoute } from "../inbox/inbox-routes";
import { editorIssues } from "./editor";
import styles from "./KhtcFormEntry.module.css";
import { KhtcFormView } from "./KhtcFormView";
import { historyHref } from "./submission-history";
import {
  KHTC_DEPT,
  cloneContent,
  createKhtcFormApi,
  normalizeForSend,
  type KhtcFormContent,
  type KhtcFormSheet,
  type KhtcFormReviewInput,
} from "./api";

function FormHead({ f }: { f: KhtcFormSheet }) {
  return (
    <>
      <p className="mute" aria-label="Thanh kỳ">
        {vnDate(f.period.from_date)} – {vnDate(f.period.to_date)} · Hạn nộp{" "}
        {dt(f.period.due_at)}
        {f.overdue ? " (đã quá hạn)" : ""} · Lần gửi {f.attempt}/
        {f.period.max_submissions || "∞"}
        {f.limit_reached ? " — đã gửi đủ số lần" : ""}
        {f.plan
          ? ` · Kế hoạch năm phiên bản ${f.plan.version}${f.plan.decision_no ? ` (QĐ ${f.plan.decision_no})` : ""}`
          : ""}
        {` · Mẫu ${f.template.code} v${f.template.version}`}
      </p>
      {f.request_note ? (
        <div
          className="alert alert-warn"
          role="alert"
          aria-label="Yêu cầu bổ sung"
        >
          <strong>Yêu cầu bổ sung:</strong> {f.request_note}
        </div>
      ) : null}
      {f.period.terminal === "closed" ? (
        <p className="mute">
          Kỳ đã đóng — sai số liệu điều chỉnh ở phiếu kỳ kế tiếp (BR-DL-12).
        </p>
      ) : null}
    </>
  );
}

export function KhtcFormEntryScreen() {
  const auth = useAuth();
  const authority = useInboxAuthorityKey();
  const { periodId } = useParams<{ periodId: string }>();
  const query = useSearchParams();
  const route = parseInboxRoute(`${INBOX_BASE}/phieu/${periodId}`, new URLSearchParams(query.toString()));
  const dept = route?.department || (auth.kn.context?.department_code !== KHTC_DEPT ? auth.kn.context?.department_code : "") || "";
  if (!route) return <p role="alert">Đường dẫn phiếu không hợp lệ. Kiểm tra mã kỳ và phòng.</p>;
  if (auth.status !== "authenticated" || !auth.principal || auth.kn.status !== "active" || !auth.kn.context)
    return <p role="alert">Chọn ngữ cảnh làm việc để đọc phiếu.</p>;
  if (!dept) return <p role="alert">Chọn phòng để đọc phiếu.</p>;
  // A committed route/authority change starts a new lifetime, including A → B → A.
  return <FormEntry key={`${authority}:${periodId}:${query}`} periodId={periodId} dept={dept} />;
}

type PendingFormWrite = { kind: "draft" | "submit"; sheet: KhtcFormContent; attempt: number; key: string };

function FormEntry({ periodId, dept }: { periodId: string; dept: string }) {
  const { api, kn } = useAuth();
  const fapi = useMemo(() => createKhtcFormApi(api), [api]);
  const checkResource = (value: KhtcFormSheet) => {
    if (value.period.id !== periodId || value.department !== dept) throw new Error("Mismatched form resource");
    return value;
  };
  const form = useLoad(() => fapi.get(periodId, dept).then(value => {
    try { return checkResource(value); }
    catch { throw new ApiProblem({ type: "about:blank", title: "Không đọc được phiếu", code: "extension.out_of_scope", status: 403 }); }
  }), [fapi, periodId, dept]);
  const [view, setView] = useState<KhtcFormSheet | null>(null);
  const [edit, setEdit] = useState<KhtcFormContent | null>(null);
  const [pending, setPending] = useState<PendingFormWrite | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [needsReload, setNeedsReload] = useState(false);
  const flight = useRef(false);
  const act = useAction();
  const f = view ?? form.data;
  const c = edit ?? (f ? cloneContent(f.content) : null);
  const dirty = !!edit && !!f && JSON.stringify(normalizeForSend(edit)) !== JSON.stringify(normalizeForSend(f.content));
  useEffect(() => {
    if (!dirty && !pending) return;
    const guard = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [dirty, pending]);
  const editable = !!f?.editable && !kn.context?.read_only && !f.period.terminal
    && (!!f.supplement || (kn.context?.role === "kn_tinh" && kn.context.department_code === dept));
  const localIssues = f && c ? editorIssues(f, c, true) : [];
  const invalid = localIssues.some(issue => issue.severity === "error");
  const sendBlocked = !editable || invalid || !!f?.limit_reached || !!f?.supplement && c?.adhoc_request_id !== f.supplement.id;

  const write = async (kind: PendingFormWrite["kind"]) => {
    if (!f || !c || !editable || needsReload || flight.current || (pending && pending.kind !== kind) || (!pending && kind === "submit" && sendBlocked)) return;
    const operation = pending ?? { kind, sheet: cloneContent(c), attempt: f.attempt, key: newIdempotencyKey() };
    flight.current = true;
    setPending(operation); setConfirming(false);
    try {
      await act.run(async () => {
        try {
          const response = checkResource(await fapi[kind](periodId, dept, operation.sheet, operation.attempt, operation.key));
          setView(response); setEdit(null); setPending(null);
          return response;
        } catch (error) {
          if (error instanceof ApiProblem && !outcomeUnknown({ status: error.status, code: error.code })) {
            setPending(null);
            if ([401, 403, 409].includes(error.status)) setNeedsReload(true);
          }
          throw error;
        }
      }, kind === "draft" ? "Đã lưu nháp." : response => `Đã gửi KHTC lần ${response.attempt}.`);
    } finally { flight.current = false; }
  };

  if (needsReload) return <section role="alert"><p>Quyền hoặc phiên bản phiếu đã thay đổi. Tải lại để đọc trạng thái hiện tại trước khi sửa.</p><button className="btn" onClick={() => window.location.reload()}>Tải lại phiếu</button></section>;
  return <div className={`page ${styles.entry}`}><LoadState s={form}>{() => f && c ? <DetailPage
    back={{ href: INBOX_BASE, label: "Báo cáo cho KHTC" }}
    title={`Phiếu ${f.department_name} · ${f.period.name}`}
    badge={<StatusPill tone={FORM_TONE[f.state]}>{f.state_label}</StatusPill>}
    actions={editable ? <>
      <button className="btn" disabled={!!pending} onClick={() => void write("draft")}>Lưu nháp</button>
      <button className="btn btn-p" disabled={!!pending || sendBlocked} onClick={() => f.supplement ? void write("submit") : setConfirming(true)}>Gửi KHTC</button>
    </> : undefined}
  >
    {act.view}
    <FormHead f={f} />
    {f.draft_at && f.content_source === "draft" ? <p className="mute">Nháp lưu lúc {dt(f.draft_at)}.</p> : null}
    <p role="status">{dirty ? "Có thay đổi chưa lưu." : "Nội dung đã đồng bộ."}</p>
    {!editable ? <p className="mute">Phiếu chỉ đọc trong trạng thái và ngữ cảnh hiện tại.</p> : null}
    {pending ? <section aria-label="Đối chiếu thao tác phiếu" role={act.busy ? "status" : "alert"}>
      <p>{act.busy ? "Đang gửi yêu cầu; nội dung được giữ nguyên." : "Chưa xác định kết quả. Giữ nguyên nội dung và thử lại đúng thao tác này để đối chiếu."}</p>
      <button className="btn" disabled={act.busy} onClick={() => void write(pending.kind)}>Thử lại {pending.kind === "draft" ? "Lưu nháp" : "Gửi KHTC"}</button>
    </section> : null}
    {confirming && !pending && !sendBlocked ? <section role="dialog" aria-label="Xác nhận gửi phiếu" className="alert alert-info">
      <p>Gửi toàn bộ nội dung phiếu {f.department_name} · {f.period.name}, lần {f.attempt + 1}/{f.period.max_submissions || "∞"}? Phiếu đã gửi chỉ sửa khi có yêu cầu bổ sung.</p>
      <button className="btn btn-p" onClick={() => void write("submit")}>Xác nhận gửi lần {f.attempt + 1}</button>
      <button className="btn" onClick={() => setConfirming(false)}>Tiếp tục sửa</button>
    </section> : null}
    <fieldset disabled={!!pending} style={{ border: 0, margin: 0, padding: 0, minWidth: 0 }} aria-label="Nội dung phiếu">
      <KhtcFormView form={f} content={c} inputIssues={edit ? localIssues : [...f.issues, ...localIssues]} onChange={editable ? next => { if (!pending && !flight.current) { setEdit(next); setConfirming(false); } } : undefined} />
    </fieldset>
  </DetailPage> : null}</LoadState></div>;
}

const REVIEW_DEPARTMENTS = new Set(["trong_trot", "chan_nuoi_thuy_san", "thong_tin_truyen_thong", "hanh_chinh_tong_hop", "ke_hoach_tai_chinh"]);
const REVIEW_UUID = /^(?!00000000-0000-0000-0000-000000000000$)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function KhtcFormReviewScreen() {
  const auth = useAuth();
  const { periodId } = useParams<{ periodId: string }>();
  const query = useSearchParams()?.toString() ?? "";
  const authority = useInboxAuthorityKey();
  const params = new URLSearchParams(query);
  const dept = params.get("department") ?? "";
  if (typeof periodId !== "string" || !REVIEW_UUID.test(periodId)
    || [...params.keys()].some(key => key !== "department" || params.getAll(key).length !== 1)
    || params.has("department") && !REVIEW_DEPARTMENTS.has(dept)) return <p role="alert">Đường dẫn rà soát không hợp lệ.</p>;
  if (auth.status !== "authenticated" || !auth.principal || auth.kn.status !== "active" || !auth.kn.context)
    return <p role="alert">Chọn ngữ cảnh làm việc để đọc phiếu.</p>;
  return <ReviewPeriod key={`${authority}:${periodId}:${query}`} periodId={periodId} requestedDepartment={dept} />;
}

function ReviewPeriod({ periodId, requestedDepartment }: { periodId: string; requestedDepartment: string }) {
  const { api } = useAuth();
  const papi = useMemo(() => createKhtcPeriodApi(api), [api]);
  const period = useLoad(() => papi.get(periodId).then(value => {
    if (value.id !== periodId || value.departments.some(d => !REVIEW_DEPARTMENTS.has(d.code))
      || new Set(value.departments.map(d => d.code)).size !== value.departments.length)
      throw new ApiProblem({ type: "about:blank", title: "Không đọc được kỳ", code: "extension.out_of_scope", status: 403 });
    return value;
  }), [papi, periodId]);
  const [department, setDepartment] = useState(requestedDepartment);
  const departments = period.data?.departments ?? [];
  const current = department || departments[0]?.code || "";
  return <LoadState s={period}>{() => departments.some(d => d.code === current)
    ? <ReviewSheet key={current} periodId={periodId} department={current} departments={departments} onDepartment={setDepartment} />
    : <p role="alert">Phòng không được giao phiếu trong kỳ này.</p>}</LoadState>;
}

type PendingReview = { input: KhtcFormReviewInput; key: string };
type ReviewNavigation = { department: string } | { back: true } | { history: true };

function ReviewSheet({ periodId, department, departments, onDepartment }: {
  periodId: string; department: string; departments: { code: string; name: string }[]; onDepartment: (dept: string) => void;
}) {
  const { api, kn } = useAuth();
  const router = useRouter();
  const fapi = useMemo(() => createKhtcFormApi(api), [api]);
  const form = useLoad(() => fapi.get(periodId, department).then(value => {
    if (value.period.id !== periodId || value.department !== department)
      throw new ApiProblem({ type: "about:blank", title: "Không đọc được phiếu", code: "extension.out_of_scope", status: 403 });
    return value;
  }), [fapi, periodId, department]);
  const [view, setView] = useState<KhtcFormSheet | null>(null);
  const [note, setNote] = useState("");
  const [noteRequired, setNoteRequired] = useState(false);
  const [confirming, setConfirming] = useState<PendingReview | null>(null);
  const [pending, setPending] = useState<PendingReview | null>(null);
  const [leaving, setLeaving] = useState<ReviewNavigation | null>(null);
  const [needsReload, setNeedsReload] = useState(false);
  const flight = useRef(false);
  const noteSection = useRef<HTMLElement>(null);
  const dialog = useRef<HTMLElement>(null);
  const act = useAction();
  const f = view ?? form.data;
  const latest = f?.submissions[0];
  const normalizedNote = note.trim();
  const noteTooLong = note.length > 4000 || Array.from(normalizedNote).length > 1000;
  const noteError = noteTooLong ? "Tối đa 1000 ký tự."
    : noteRequired && !normalizedNote ? "Bắt buộc nhập nội dung yêu cầu bổ sung." : undefined;
  const dirty = note !== "";
  const backHref = `/kn/khtc/ky-bao-cao/ky/${periodId}`;
  const canReview = !!f?.can_review && f.state === "submitted" && !f.limit_reached && f.attempt < 3 && !f.period.terminal && !kn.context?.read_only
    && (kn.context?.role === "kn_tinh" && kn.context.department_code === KHTC_DEPT || kn.context?.role === "admin" && f.period.report_type === "adhoc")
    && !!latest && REVIEW_UUID.test(latest.id) && Number.isSafeInteger(latest.attempt) && latest.attempt > 0 && latest.attempt === f.attempt && !latest.decision;
  const index = departments.findIndex(d => d.code === department);
  useEffect(() => {
    if (!dirty && !pending) return;
    const guard = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [dirty, pending]);

  useEffect(() => { if (confirming || leaving) dialog.current?.focus(); }, [confirming, leaving]);

  const followNavigation = (target: ReviewNavigation) => {
    if ("back" in target) router.push(backHref);
    else if ("history" in target) router.push(historyHref({ periodId, department }));
    else onDepartment(target.department);
  };
  const navigate = (target: ReviewNavigation) => {
    if (pending || flight.current) return;
    if (dirty) { setConfirming(null); setLeaving(target); return; }
    followNavigation(target);
  };
  const prepare = (decision: KhtcFormReviewInput["decision"]) => {
    if (!canReview || !latest || pending || flight.current || needsReload) return;
    setNoteRequired(decision === "returned");
    if (noteTooLong || decision === "returned" && !normalizedNote) {
      noteSection.current?.querySelector("textarea")?.focus();
      return;
    }
    if (decision === "returned" && !f?.can_request_supplement || decision === "accepted" && !f?.can_confirm) return;
    setLeaving(null);
    setConfirming({ input: { expected_submission_id: latest.id, base_attempt: latest.attempt, decision, note: normalizedNote }, key: newIdempotencyKey() });
  };
  const dispatch = async (operation: PendingReview) => {
    if (flight.current || needsReload || !canReview || !latest
      || operation.input.expected_submission_id !== latest.id || operation.input.base_attempt !== latest.attempt) return;
    flight.current = true;
    setPending(operation); setConfirming(null);
    try {
      await act.run(async () => {
        try {
          const response = await fapi.review(periodId, department, operation.input, operation.key);
          if (response.period.id !== periodId || response.department !== department
            || response.reviewed_submission_id !== operation.input.expected_submission_id
            || response.reviewed_attempt !== operation.input.base_attempt || response.review_decision !== operation.input.decision) {
            setNeedsReload(true);
            throw new ApiProblem({ type: "about:blank", title: "Chưa đối chiếu được kết quả rà soát", code: "extension.review_ack_mismatch", status: 503 });
          }
          setView(response); setNote(""); setPending(null); setNoteRequired(false);
          return response;
        } catch (error) {
          if (error instanceof ApiProblem && !outcomeUnknown({ status: error.status, code: error.code })) {
            setPending(null);
            if ([401, 403, 409].includes(error.status)) setNeedsReload(true);
          }
          throw error;
        }
      }, response => response.review_decision === "returned" ? `Đã gửi yêu cầu bổ sung cho lần gửi ${response.reviewed_attempt}.` : `Đã xác nhận lần gửi ${response.reviewed_attempt}.`);
    } finally { flight.current = false; }
  };

  if (needsReload) return <section role="alert"><p>Chưa đối chiếu được kết quả trong phiên bản hoặc quyền hiện tại. Tải lại phiếu và kiểm tra trạng thái trước khi tiếp tục.</p><button className="btn" onClick={() => window.location.reload()}>Tải lại phiếu</button></section>;
  return <div className={`page ${reviewStyles.review}`} onClickCapture={event => {
    if ((dirty || pending) && event.target instanceof Element && event.target.closest("a.back-link")) { event.preventDefault(); navigate({ back: true }); }
  }}><LoadState s={form}>{() => f ? <DetailPage
    back={{ href: backHref, label: "Thông tin kỳ báo cáo" }} title={`Rà soát phiếu ${f.department_name} · ${f.period.name}`}
    badge={<StatusPill tone={FORM_TONE[f.state]}>{f.state_label}</StatusPill>}
    actions={<>
      <button className="btn" disabled={!!pending || index <= 0} onClick={() => navigate({ department: departments[index - 1]!.code })}>Phiếu trước</button>
      <button className="btn" disabled={!!pending || index < 0 || index >= departments.length - 1} onClick={() => navigate({ department: departments[index + 1]!.code })}>Phiếu tiếp theo</button>
      <button className="btn" disabled={!!pending || !f.submissions.length} onClick={() => navigate({ history: true })}>Lịch sử và so sánh lần gửi</button>
      {canReview ? <>
        <button className="btn btn-p" disabled={!!pending || !f.can_request_supplement} onClick={() => prepare("returned")}>Yêu cầu bổ sung</button>
        <button className="btn" disabled={!!pending || !f.can_confirm} onClick={() => prepare("accepted")}>Xác nhận</button>
      </> : null}
    </>}
  >
    {act.view}
    {confirming && !pending ? <section ref={dialog} tabIndex={-1} role="dialog" aria-label={confirming.input.decision === "returned" ? "Xác nhận yêu cầu bổ sung" : "Xác nhận phiếu số liệu"} className="alert alert-info">
      <p>{confirming.input.decision === "returned" ? "Gửi yêu cầu bổ sung" : "Xác nhận"} phiếu {f.department_name} · {f.period.name}, lần gửi {confirming.input.base_attempt}?</p>
      {confirming.input.note ? <p className={reviewStyles.note}>{confirming.input.note}</p> : null}
      <button className="btn btn-p" onClick={() => void dispatch(confirming)}>{confirming.input.decision === "returned" ? "Gửi yêu cầu bổ sung" : "Xác nhận lần gửi"}</button>
      <button className="btn" onClick={() => setConfirming(null)}>Tiếp tục rà soát</button>
    </section> : null}
    {pending ? <section aria-label="Đối chiếu thao tác rà soát" role={act.busy ? "status" : "alert"}>
      <p>{act.busy ? "Đang gửi yêu cầu rà soát; giữ nguyên lần gửi và ghi chú." : "Chưa xác định kết quả. Giữ nguyên lần gửi, nội dung và thử lại đúng thao tác này để đối chiếu."}</p>
      <button className="btn" disabled={act.busy} onClick={() => void dispatch(pending)}>Thử lại thao tác rà soát</button>
    </section> : null}
    {leaving && !pending ? <section ref={dialog} tabIndex={-1} role="dialog" aria-label="Bỏ ghi chú chưa gửi?" className="alert alert-info">
      <p>Ghi chú của phiếu này chưa được gửi. Bỏ ghi chú để chuyển?</p>
      <button className="btn" onClick={() => followNavigation(leaving)}>Bỏ ghi chú và chuyển</button>
      <button className="btn btn-p" onClick={() => setLeaving(null)}>Giữ ghi chú</button>
    </section> : null}
    <SelectField label="Phòng được giao phiếu" value={department} disabled={!!pending} options={departments.map(d => ({ value: d.code, label: d.name }))} onChange={dept => { if (dept && dept !== department) navigate({ department: dept }); }} />
    <FormHead f={f} />
    {latest ? <p className="mute">Lần gửi đang đọc: {latest.attempt} · Gửi lúc {dt(latest.submitted_at)}.</p> : null}
    <KhtcFormView form={f} content={f.content} />
    {canReview ? <section ref={noteSection} className="sec" aria-label="Rà soát">
      <TextField label="Nội dung yêu cầu bổ sung, điều chỉnh" multiline maxLength={4000} value={note} disabled={!!pending}
        required={noteRequired} error={noteError} hint="Tối đa 1000 ký tự; bắt buộc khi yêu cầu bổ sung."
        onChange={value => { if (!pending && !flight.current) { setNote(value.slice(0, 4001)); setConfirming(null); } }} />
    </section> : <p className="mute">Phiếu chỉ đọc trong trạng thái và ngữ cảnh hiện tại.</p>}
  </DetailPage> : null}</LoadState></div>;
}
