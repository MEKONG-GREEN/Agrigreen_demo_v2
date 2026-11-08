"use client";
import { useEffect, useMemo, useSyncExternalStore } from "react";
import { dt, LoadState, useLoad } from "@/kn/cycle/common";
import type { ReportApi } from "./api";
import { createReportSaveSession244 } from "./save-operation244";

export function useReportSave244(api: ReportApi, id: string, actor: string, scope: string) {
    const operation = useMemo(() => createReportSaveSession244(api, id, actor, scope, () => typeof window === "undefined" ? null : window.sessionStorage), [api, id, actor, scope]);
    const state = useSyncExternalStore(operation.subscribe, operation.getSnapshot, operation.getServerSnapshot);
    useEffect(() => {
        operation.load();
        if (operation.getSnapshot().pending) void operation.reconcile().catch(() => { /* The retained operation remains unresolved; the explicit read can be retried. */ });
    }, [operation]);
    return { operation, state };
}
export function ReportSaveStatus244({ save, check, busy }: { save: ReturnType<typeof useReportSave244>; check: () => void; busy: boolean }) {
    return <>
        {save.state.pending && <div role="alert" className="alert alert-warn"><p>Lần lưu trước chưa được đối chiếu. Các thao tác thay đổi báo cáo tạm khóa.</p><button className="btn" disabled={busy} onClick={check}>Đối chiếu lần lưu</button></div>}
        {save.state.error && <p role="alert">{save.state.error}</p>}
        {save.state.receipt?.applied && <p role="status">Đã đối chiếu lần lưu phiên bản {save.state.receipt.applied.version} · {dt(save.state.receipt.applied.at)}.</p>}
    </>;
}
export function ReportDraftAudit244({ api, id, labels }: { api: ReportApi; id: string; labels: Record<string, string> }) {
    const audit = useLoad(() => api.saveAudit(id), [api, id]);
    return <details><summary>Lịch sử lưu nháp</summary><p>Tối đa 50 lần lưu có ghi nhận. Các bản cũ chưa có ghi nhận thao tác riêng.</p><button className="btn" onClick={audit.reload}>Tải lại lịch sử lưu nháp</button><LoadState s={audit}>{items => <ul>{items.map(item => item.applied && <li key={item.operation_id}><strong>Phiên bản {item.applied.version}</strong> · {dt(item.applied.at)} · Người lưu {item.applied.actor_ref}
        <ul>{item.applied.changes.map(change => <li key={change.field}>{labels[change.field] ?? change.field}: <span style={{ whiteSpace: "pre-wrap" }}>{change.before || "(trống)"}</span>{change.before_default ? " (mặc định)" : ""} → <span style={{ whiteSpace: "pre-wrap" }}>{change.after || "(trống)"}</span>{change.after_default ? " (mặc định)" : ""}</li>)}{!item.applied.changes.length && <li>Giữ nguyên các ô nhập.</li>}</ul>
    </li>)}{!items.length && <li>Chưa có ghi nhận lưu nháp.</li>}</ul>}</LoadState></details>;
}
