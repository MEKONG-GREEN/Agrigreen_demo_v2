"use client";

// Màn phiếu "Báo cáo cho KHTC" (W11.63 rút gọn):
//  - KhtcFormEntryScreen  — /kn/khtc/phieu/{period_id}: phòng nhập phiếu (FN-PN-03.02). Phòng lấy từ ngữ cảnh phiên; Admin,
//    Phòng KHTC thêm ?department=. Hộp phiếu của phòng (W11.11) dẫn tới đây.
//  - "Phiếu kỳ này" (FN-PN-03.01) là màn của W11.11 (/kn/bao-cao-khtc); /kn/khtc/phieu dùng lại màn đó.
//  - KhtcFormReviewScreen — /kn/khtc/ra-soat/{period_id}?department=: KHTC rà soát (FN-TI-02.04) — yêu cầu bổ sung (bắt
//    buộc nội dung), xác nhận, phiếu trước / tiếp theo. "So sánh lần gửi" để sau G1.
import { useParams, useSearchParams } from "next/navigation";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import { FORM_TONE, createKhtcPeriodApi, vnDate } from "../periods/api";
import { KhtcFormView } from "./KhtcFormView";
import {
  KHTC_DEPT,
  cloneContent,
  createKhtcFormApi,
  type KhtcFormContent,
  type KhtcFormSheet,
} from "./api";

function useDept(): string {
  const { kn } = useAuth();
  const q = useSearchParams()?.get("department");
  const own = kn.context?.department_code ?? "";
  return q || (own === KHTC_DEPT ? "" : own);
}

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
 const {kn}=useAuth();
 return <FormEntry key={JSON.stringify(kn.context)}/>;
}
function FormEntry() {
  const { api } = useAuth();
  const { periodId } = useParams<{ periodId: string }>();
  const dept = useDept();
  const fapi = useMemo(() => (api ? createKhtcFormApi(api) : null), [api]);
  const form = useLoad(fapi && dept ? () => fapi.get(periodId, dept) : null, [
    fapi,
    periodId,
    dept,
  ]);
  // Phiếu mới nhất (phản hồi Lưu nháp / Gửi) và bản sửa cục bộ gắn với đúng phiếu đó.
  const [view, setView] = useState<KhtcFormSheet | null>(null);
  const [edit, setEdit] = useState<{
    of: KhtcFormSheet;
    c: KhtcFormContent;
  } | null>(null);
  const act = useAction();
  if (!fapi) return null;
  if (!dept)
    return <p className="mute">Chọn phòng (?department=) để xem phiếu.</p>;
  const apply = (r: KhtcFormSheet | null) => {
    if (r) {
      setView(r);
      setEdit(null);
    }
  };
  return (
    <div className="page">
      <LoadState s={form}>
        {() => {
          const f =
            view && view.period.id === periodId && view.department === dept
              ? view
              : form.data!;
          const c = edit?.of === f ? edit.c : cloneContent(f.content);
          const setContent = (next: KhtcFormContent) =>
            setEdit({ of: f, c: next });
          return (
            <>
              <PageHeader
                title={`Phiếu ${f.department_name} · ${f.period.name}`}
                actions={
                  <>
                    <StatusPill tone={FORM_TONE[f.state]}>
                      {f.state_label}
                    </StatusPill>
                    {f.editable ? (
                      <>
                        <button
                          className="btn"
                          disabled={act.busy}
                          onClick={async () =>
                            apply(
                              await act.run(
                                () => fapi.draft(periodId, dept, c, f.attempt),
                                "Đã lưu nháp.",
                              ),
                            )
                          }
                        >
                          Lưu nháp
                        </button>
                        <button
                          className="btn btn-primary"
                          disabled={act.busy || !!f.supplement && c.adhoc_request_id!==f.supplement.id}
                          onClick={async () =>
                            apply(
                              await act.run(
                                () => fapi.submit(periodId, dept, c, f.attempt),
                                (r) => `Đã gửi KHTC lần ${r.attempt}.`,
                              ),
                            )
                          }
                        >
                          Gửi KHTC
                        </button>
                      </>
                    ) : null}
                  </>
                }
              />
              {act.view}
              <FormHead f={f} />
              {f.draft_at && f.content_source === "draft" ? (
                <p className="mute">Nháp lưu lúc {dt(f.draft_at)}.</p>
              ) : null}
              {!f.editable && f.state === "submitted" ? (
                <p className="mute">
                  Phiếu đã gửi — chỉ sửa, gửi lại khi KHTC yêu cầu bổ sung.
                </p>
              ) : null}
              <section className="card sec">
                <KhtcFormView
                  form={f}
                  content={c}
                  onChange={f.editable ? setContent : undefined}
                />
              </section>
            </>
          );
        }}
      </LoadState>
    </div>
  );
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
