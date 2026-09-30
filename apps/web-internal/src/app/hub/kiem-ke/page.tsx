"use client";

import { useState } from "react";
import { idem, unwrap } from "@/api/client";
import type { Product, Stocktake } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ActionStatus, Chip, FacilitySelect, Ref, Section, SelfHint, TextField, useAction, useFacilities } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { addKg, formatKg, isKg } from "@/lib/kg";
import { PRODUCT_LABEL, validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

export default function StocktakePage() {
  const { api, principal } = useAuth();
  const facilities = useFacilities();
  const [fid, setFid] = useState("");
  const facility = fid || facilities.data?.[0]?.id || "";
  const data = useAsync(async () => {
    if (!facility) return null;
    const [inv, list] = await Promise.all([
      unwrap(api.GET("/v1/hub/facilities/{id}/inventory", { params: { path: { id: facility } } })),
      unwrap(api.GET("/v1/hub/facilities/{id}/stocktakes", { params: { path: { id: facility } } })),
    ]);
    return { inv, items: (list.items ?? []) as Stocktake[] };
  }, [api, facility]);
  const [counted, setCounted] = useState<Record<string, string>>({});
  const [note, setNote] = useState("");
  const [reason, setReason] = useState("");
  const [decReason, setDecReason] = useState("");
  const act = useAction();

  const products = Array.from(new Set((data.data?.inv.lots ?? []).map((l) => l.product as Product)));
  const book = (p: Product) => addKg("0", ...(data.data?.inv.lots ?? []).filter((l) => l.product === p).map((l) => l.remaining_kg ?? "0"));
  const pending = data.data?.items.find((s) => s.status === "pending");
  const linesOk = products.length > 0 && products.every((p) => isKg(counted[p] ?? ""));
  const canCount = hasRole(principal, ROLES.hubOperator);
  const canDecide = hasRole(principal, ROLES.hubApprover);

  const submit = async () => {
    const r = await act.run(
      () =>
        unwrap(
          api.POST("/v1/hub/facilities/{id}/stocktakes", {
            params: { path: { id: facility }, header: idem() },
            body: {
              lines: products.map((p) => ({ product: p, counted_kg: counted[p]!.trim() })),
              ...(note.trim() ? { note: note.trim() } : {}),
              ...(pending ? { supersedes_id: pending.id, reason: reason.trim() } : {}),
            },
          }),
        ),
      (s) => `Đã ghi phiên kiểm kê — chênh ${formatKg(s.variance_kg, 3)}, chờ người khác duyệt`,
    );
    if (r) {
      setCounted({});
      setReason("");
      data.reload();
    }
  };
  const decide = async (s: Stocktake, decision: "approved" | "rejected") => {
    const r = await act.run(
      () => unwrap(api.POST("/v1/hub/stocktakes/{id}/decision", { params: { path: { id: s.id! }, header: idem() }, body: decision === "rejected" ? { decision, reason: decReason.trim() } : { decision } })),
      (x) => (decision === "approved" ? `Đã duyệt kiểm kê — ${x.adjustments?.length ?? 0} dịch chuyển điều chỉnh` : "Đã từ chối phiên kiểm kê"),
    );
    if (r) data.reload();
  };

  return (
    <div className="stack">
      <PageHead title="Kiểm kê" subtitle="Một lần đếm theo dạng hàng (Q-MKG-06), sổ chụp lúc đếm. Người duyệt (hub_approver) phải KHÁC người đếm; duyệt → dịch chuyển điều chỉnh theo hub.risk.v2." />
      <Section title="Cơ sở">
        <FacilitySelect value={facility} onChange={setFid} facilities={facilities.data ?? []} />
      </Section>
      <ActionStatus a={act} />
      {data.loading ? <Loading /> : null}
      {data.error ? <ProblemAlert problem={data.error} onRetry={data.reload} /> : null}
      {data.data && canCount ? (
        <Section title={pending ? "Đếm lại (thay phiên đang chờ)" : "Ghi lần đếm"}>
          <div className="stack">
            {products.length === 0 ? <Empty>Cơ sở không có tồn.</Empty> : null}
            <div className="grid3">
              {products.map((p) => (
                <TextField key={p} label={`Đếm được — ${PRODUCT_LABEL[p] ?? p} (kg)`} value={counted[p] ?? ""} onChange={(v) => setCounted((c) => ({ ...c, [p]: v }))} hint={`Sổ hiện tại ${formatKg(book(p), 3)}`} inputMode="decimal" />
              ))}
            </div>
            <TextField label="Ghi chú" value={note} onChange={setNote} />
            {pending ? <TextField label="Lý do đếm lại" value={reason} onChange={setReason} hint="Bắt buộc, ít nhất 10 ký tự" /> : null}
            <div className="row">
              <button type="button" className="btn btn-p btn-sm" disabled={!linesOk || act.busy || (!!pending && !validReason(reason))} onClick={() => void submit()}>
                Gửi phiên kiểm kê
              </button>
            </div>
          </div>
        </Section>
      ) : null}
      {data.data ? (
        <Section title="Phiên kiểm kê">
          {data.data.items.length === 0 ? <Empty>Chưa có phiên kiểm kê.</Empty> : null}
          <div className="stack">
            {data.data.items.map((s) => (
              <div key={s.id} className="panel stack" aria-label={`Phiên kiểm kê ${s.id}`}>
                <div className="row">
                  <Chip status={s.status} />
                  <span>
                    Sổ {formatKg(s.book_kg, 3)} · đếm {formatKg(s.counted_kg, 3)} · chênh <b>{formatKg(s.variance_kg, 3)}</b>
                  </span>
                  <span className="small mute">
                    Người đếm <Ref id={s.counted_by} />
                  </span>
                  {s.supersedes_id ? <span className="small mute">thay phiên <Ref id={s.supersedes_id} /></span> : null}
                </div>
                <ul className="list-plain small">
                  {s.lines?.map((l) => (
                    <li key={l.product}>
                      {PRODUCT_LABEL[l.product ?? ""] ?? l.product}: sổ {formatKg(l.book_kg, 3)}, đếm {formatKg(l.counted_kg, 3)}, chênh {formatKg(l.variance_kg, 3)}
                    </li>
                  ))}
                </ul>
                {s.adjustments?.length ? <span className="small">Điều chỉnh: {s.adjustments.map((a) => `${formatKg(a.qty_kg, 3)}`).join(", ")}</span> : null}
                {s.status === "pending" && canDecide ? (
                  <div className="stack" style={{ gap: 6 }}>
                    <SelfHint proposer={s.counted_by} />
                    <TextField label="Lý do từ chối kiểm kê" value={decReason} onChange={setDecReason} hint="Bắt buộc khi từ chối" />
                    <div className="row">
                      <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void decide(s, "approved")}>
                        Duyệt kiểm kê
                      </button>
                      <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(decReason)} onClick={() => void decide(s, "rejected")}>
                        Từ chối kiểm kê
                      </button>
                    </div>
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        </Section>
      ) : null}
    </div>
  );
}
