"use client";

// Màn phiếu "Báo cáo cho KHTC" (W11.63 rút gọn):
//  - KhtcFormEntryScreen  — /kn/khtc/phieu/{period_id}: phòng nhập phiếu (FN-PN-03.02). Phòng lấy từ ngữ cảnh phiên; Admin,
//    Phòng KHTC thêm ?department=. Hộp phiếu của phòng (W11.11) dẫn tới đây.
//  - "Phiếu kỳ này" (FN-PN-03.01) là màn của W11.11 (/kn/bao-cao-khtc); /kn/khtc/phieu dùng lại màn đó.
//  - KhtcFormReviewScreen — /kn/khtc/ra-soat/{period_id}?department=: KHTC rà soát (FN-TI-02.04) — yêu cầu bổ sung (bắt
//    buộc nội dung), xác nhận, phiếu trước / tiếp theo. "So sánh lần gửi" để sau G1.
import { useParams, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import { FORM_TONE, createKhtcPeriodApi, vnDate } from "../periods/api";
import { ApiProblem } from "@/api/problem";
import { newIdempotencyKey, outcomeUnknown } from "@/api/idempotency";
import { DetailPage } from "@/components/patterns/detail";
import { useInboxAuthorityKey } from "../inbox/inbox-workspace";
import { INBOX_BASE, parseInboxRoute } from "../inbox/inbox-routes";
import { editorIssues } from "./editor";
import { KhtcFormView } from "./KhtcFormView";
import {
  KHTC_DEPT,
  cloneContent,
  createKhtcFormApi,
  normalizeForSend,
  type KhtcFormContent,
  type KhtcFormSheet,
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
  return <div className="page"><LoadState s={form}>{() => f && c ? <DetailPage
    back={{ href: INBOX_BASE, label: "Báo cáo cho KHTC" }}
    title={`Phiếu ${f.department_name} · ${f.period.name}`}
    badge={<StatusPill tone={FORM_TONE[f.state]}>{f.state_label}</StatusPill>}
    actions={editable ? <>
      <button className="btn" disabled={!!pending} onClick={() => void write("draft")}>Lưu nháp</button>
      <button className="btn btn-primary" disabled={!!pending || sendBlocked} onClick={() => f.supplement ? void write("submit") : setConfirming(true)}>Gửi KHTC</button>
    </> : undefined}
  >
    {act.view}
    <FormHead f={f} />
    {f.draft_at && f.content_source === "draft" ? <p className="mute">Nháp lưu lúc {dt(f.draft_at)}.</p> : null}
    <p role="status">{dirty ? "Có thay đổi chưa lưu." : "Đang xem nội dung từ máy chủ."}</p>
    {!editable ? <p className="mute">Phiếu chỉ đọc trong trạng thái và ngữ cảnh hiện tại.</p> : null}
    {pending ? <section aria-label="Đối chiếu thao tác phiếu" role={act.busy ? "status" : "alert"}>
      <p>{act.busy ? "Đang gửi yêu cầu; nội dung được giữ nguyên." : "Chưa xác định kết quả. Giữ nguyên nội dung và thử lại đúng thao tác này để đối chiếu."}</p>
      <button className="btn" disabled={act.busy} onClick={() => void write(pending.kind)}>Thử lại {pending.kind === "draft" ? "Lưu nháp" : "Gửi KHTC"}</button>
    </section> : null}
    {confirming && !pending && !sendBlocked ? <section role="dialog" aria-label="Xác nhận gửi phiếu" className="alert alert-info">
      <p>Gửi toàn bộ nội dung phiếu {f.department_name} · {f.period.name}, lần {f.attempt + 1}/{f.period.max_submissions || "∞"}? Phiếu đã gửi chỉ sửa khi có yêu cầu bổ sung.</p>
      <button className="btn btn-primary" onClick={() => void write("submit")}>Xác nhận gửi lần {f.attempt + 1}</button>
      <button className="btn" onClick={() => setConfirming(false)}>Tiếp tục sửa</button>
    </section> : null}
    <fieldset disabled={!!pending} style={{ border: 0, margin: 0, padding: 0, minWidth: 0 }} aria-label="Nội dung phiếu">
      <KhtcFormView form={f} content={c} inputIssues={edit ? localIssues : [...f.issues, ...localIssues]} onChange={editable ? next => { if (!pending && !flight.current) { setEdit(next); setConfirming(false); } } : undefined} />
    </fieldset>
  </DetailPage> : null}</LoadState></div>;
}

export function KhtcFormReviewScreen() {
  const { api } = useAuth();
  const { periodId } = useParams<{ periodId: string }>();
  const q = useSearchParams()?.get("department") ?? "";
  const fapi = useMemo(() => (api ? createKhtcFormApi(api) : null), [api]);
  const papi = useMemo(() => (api ? createKhtcPeriodApi(api) : null), [api]);
  const period = useLoad(papi ? () => papi.get(periodId) : null, [
    papi,
    periodId,
  ]);
  const depts = period.data?.departments.map((d) => d.code) ?? [];
  const [dept, setDept] = useState(q);
  const cur = dept || depts[0] || "";
  const form = useLoad(fapi && cur ? () => fapi.get(periodId, cur) : null, [
    fapi,
    periodId,
    cur,
  ]);
  const [note, setNote] = useState("");
  const act = useAction();
  if (!fapi || !papi) return null;
  const idx = depts.indexOf(cur);
  const done = (r: KhtcFormSheet | null) => {
    if (r) {
      setNote("");
      form.reload();
      period.reload();
    }
  };
  return (
    <div className="page">
      <PageHeader
        title="Rà soát phiếu số liệu"
        actions={
          <>
            <button
              className="btn"
              disabled={idx <= 0}
              onClick={() => setDept(depts[idx - 1] ?? "")}
            >
              Phiếu trước
            </button>
            <button
              className="btn"
              disabled={idx < 0 || idx >= depts.length - 1}
              onClick={() => setDept(depts[idx + 1] ?? "")}
            >
              Phiếu tiếp theo
            </button>
          </>
        }
      />
      {act.view}
      <LoadState s={form}>
        {(f) => (
          <>
            <h2>
              Phiếu {f.department_name} · {f.period.name}{" "}
              <StatusPill tone={FORM_TONE[f.state]}>{f.state_label}</StatusPill>
            </h2>
            <FormHead f={f} />
            <section className="card sec">
              <KhtcFormView form={f} content={f.content} />
            </section>
            {f.can_review ? (
              <section className="card sec" aria-label="Rà soát">
                <label className="field" style={{ display: "block" }}>
                  <span>Nội dung yêu cầu bổ sung, điều chỉnh</span>
                  <textarea
                    className="input"
                    rows={3}
                    style={{ width: "100%" }}
                    value={note}
                    onChange={(e) => setNote(e.target.value)}
                  />
                </label>
                <div className="row">
                  <button
                    className="btn btn-primary"
                    disabled={
                      act.busy || !f.can_request_supplement || !note.trim()
                    }
                    title={
                      f.limit_reached
                        ? "Đã gửi đủ số lần tối đa — chỉ xác nhận"
                        : undefined
                    }
                    onClick={async () =>
                      done(
                        await act.run(
                          () => fapi.review(periodId, cur, "returned", note),
                          "Đã gửi yêu cầu bổ sung.",
                        ),
                      )
                    }
                  >
                    Yêu cầu bổ sung
                  </button>
                  <button
                    className="btn"
                    disabled={act.busy || !f.can_confirm}
                    onClick={async () =>
                      done(
                        await act.run(
                          () => fapi.review(periodId, cur, "accepted"),
                          "Đã xác nhận phiếu.",
                        ),
                      )
                    }
                  >
                    Xác nhận
                  </button>
                </div>
              </section>
            ) : null}
          </>
        )}
      </LoadState>
    </div>
  );
}
