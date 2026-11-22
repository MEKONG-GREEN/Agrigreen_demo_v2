"use client";

import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useMemo } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { SelectField } from "@/components/form";
import { DetailPage, LabelValueBlock, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { DataTable, StatusPill, type Column } from "@/components/patterns/list";
import { LoadState, dt, useLoad } from "@/kn/cycle/common";
import { useInboxAuthorityKey } from "../inbox/inbox-workspace";
import { compareSnapshots, createSubmissionHistoryApi, historyHref, parseHistoryRoute, type HistoryRoute, type SubmissionHeader, type SubmissionHistory, type SubmissionSnapshot } from "./submission-history";
import styles from "./KhtcFormHistory.module.css";

export function KhtcFormHistoryScreen() {
  const auth = useAuth(), authority = useInboxAuthorityKey();
  const params = useParams<{ periodId: string; submissionId?: string }>();
  const query = useSearchParams().toString();
  const route = parseHistoryRoute(params.periodId, params.submissionId, new URLSearchParams(query));
  if (!route) return <p role="alert">Đường dẫn lịch sử gửi không hợp lệ.</p>;
  if (auth.status !== "authenticated" || !auth.principal || auth.kn.status !== "active" || !auth.kn.context) return <p role="alert">Chọn ngữ cảnh làm việc để đọc lịch sử phiếu.</p>;
  if (auth.kn.context.role === "kn_tw" || auth.kn.context.role === "kn_xa") return <p role="alert">Ngữ cảnh này không có quyền đọc lịch sử phiếu các phòng.</p>;
  // Every committed authority/resource/query change starts a fresh lifetime,
  // including A -> B -> A. Old requests have no navigation/download side effect.
  return <HistoryResource key={`${authority}:${params.periodId}:${params.submissionId ?? ""}:${query}`} route={route} />;
}

function HistoryResource({ route }: { route: HistoryRoute }) {
  const { api } = useAuth();
  const client = useMemo(() => createSubmissionHistoryApi(api), [api]);
  const history = useLoad(() => client.history(route.periodId, route.department), [client, route.periodId, route.department]);
  return <LoadState s={history}>{data => route.submissionId
    ? data.items.some(sub => sub.id === route.submissionId) && (!route.compareWith || data.items.some(sub => sub.id === route.compareWith))
      ? <SnapshotResource route={route} history={data} api={client} /> : <p role="alert">Lần gửi không thuộc phiếu đang đọc.</p>
    : <HistoryList route={route} history={data} />}</LoadState>;
}

function decisionLabel(sub: SubmissionHeader) {
  return sub.decision === "accepted" ? "Đã xác nhận" : sub.decision === "returned" ? "Yêu cầu bổ sung" : "Chưa rà soát";
}
function HistoryList({ route, history }: { route: HistoryRoute; history: SubmissionHistory }) {
  const columns: Column<SubmissionHeader>[] = [
    { key: "attempt", header: "Lần gửi", cell: row => <Link className="link-all" href={historyHref({ ...route, submissionId: row.id })}>Lần {row.attempt}</Link> },
    { key: "at", header: "Gửi lúc", cell: row => dt(row.submitted_at) },
    { key: "sender", header: "Người gửi", cell: row => <span className={styles.identity}>{row.submitter_ref}</span> },
    { key: "decision", header: "Rà soát", cell: row => <StatusPill tone={row.decision === "accepted" ? "ok" : row.decision === "returned" ? "warn" : "neutral"}>{decisionLabel(row)}</StatusPill> },
    { key: "reviewer", header: "Người rà soát", cell: row => row.reviewer_ref ? <span className={styles.identity}>{row.reviewer_ref}</span> : "—" },
    { key: "note", header: "Nội dung rà soát", cell: row => <span className={styles.value}>{row.review_note || "—"}</span> },
  ];
  return <div className={`page ${styles.history}`}><DetailPage title="Lịch sử gửi phiếu"
    back={{ href: `/kn/khtc/ra-soat/${route.periodId}?department=${route.department}`, label: "Rà soát phiếu hiện tại" }}>
    <p>{history.department_name} · {history.period.name}</p>
    <p>Chọn lần gửi để đọc nội dung đã niêm phong và so sánh với một lần gửi khác của cùng phiếu.</p>
    <DataTable caption="Các lần gửi phiếu" columns={columns} rows={history.items} rowKey={row => row.id} empty="Phiếu chưa có lần gửi nào." />
  </DetailPage></div>;
}

type HistoryApi = ReturnType<typeof createSubmissionHistoryApi>;
function SnapshotResource({ route, history, api }: { route: HistoryRoute; history: SubmissionHistory; api: HistoryApi }) {
  const snapshot = useLoad(() => api.submission(route.periodId, route.department, route.submissionId!), [api, route]);
  const other = useLoad(route.compareWith ? () => api.submission(route.periodId, route.department, route.compareWith!) : null, [api, route]);
  return <LoadState s={snapshot}>{data => route.compareWith
    ? <LoadState s={other}>{compared => <SnapshotDetail route={route} history={history} data={data} other={compared} />}</LoadState>
    : <SnapshotDetail route={route} history={history} data={data} />}</LoadState>;
}

function submissionLog(sub: SubmissionHeader): LogEntry[] {
  const entries: LogEntry[] = [{ event: `Gửi phiếu lần ${sub.attempt}`, actor_display: sub.submitter_ref, at: sub.submitted_at }];
  if (sub.decision && sub.reviewer_ref && sub.reviewed_at) entries.push({ event: decisionLabel(sub), actor_display: sub.reviewer_ref, at: sub.reviewed_at, reason: sub.review_note });
  return entries;
}

function SnapshotDetail({ route, history, data, other }: { route: HistoryRoute; history: SubmissionHistory; data: SubmissionSnapshot; other?: SubmissionSnapshot }) {
  const router = useRouter();
  const fields = compareSnapshots(data, other);
  const changed = fields.filter(field => field.changed).length;
  const columns: Column<(typeof fields)[number]>[] = [
    { key: "label", header: "Ô nhập / Nội dung", cell: row => row.label },
    { key: "selected", header: `Lần gửi ${data.submission.attempt}`, cell: row => <span className={`${styles.value} ${row.changed ? styles.changed : ""}`}>{row.selected ?? "Không có nội dung"}</span> },
    ...(other ? [{ key: "other", header: `Lần gửi ${other.submission.attempt}`, cell: (row: (typeof fields)[number]) => <span className={`${styles.value} ${row.changed ? styles.changed : ""}`}>{row.other ?? "Không có nội dung"}</span> },
      { key: "changed", header: "Đối chiếu", cell: (row: (typeof fields)[number]) => row.changed ? "Đã thay đổi" : "Giữ nguyên" }] : []),
  ];
  return <div className={`page ${styles.history}`}><DetailPage title={`Lần gửi ${data.submission.attempt}`}
    back={{ href: historyHref({ periodId: route.periodId, department: route.department }), label: "Lịch sử gửi" }}
    badge={<StatusPill tone="neutral">Chỉ đọc</StatusPill>}
    actions={<SelectField label="So sánh với" value={route.compareWith ?? ""} placeholder="Không so sánh"
      options={history.items.filter(sub => sub.id !== route.submissionId).map(sub => ({ value: sub.id, label: `Lần gửi ${sub.attempt} · ${dt(sub.submitted_at)}` }))}
      onChange={value => router.push(historyHref({ ...route, compareWith: value || undefined }))} />}
    log={<LogPanel title="Log gửi và rà soát" entries={submissionLog(data.submission)} />}>
    <LabelValueBlock title="Thông tin lần gửi" items={[
      { label: "Phòng", value: data.department_name },
      { label: "Kỳ báo cáo", value: data.period.name },
      { label: "Gửi lúc", value: dt(data.submission.submitted_at) },
      { label: "Người gửi", value: <span className={styles.identity}>{data.submission.submitter_ref}</span> },
      { label: "Rà soát", value: decisionLabel(data.submission) },
      { label: "Mẫu phiếu", value: `${data.template_code} v${data.template_version}` },
      { label: "Kế hoạch đã dùng", value: data.plan ? `Phiên bản ${data.plan.version}${data.plan.decision_no ? ` · ${data.plan.decision_no}` : ""}` : "Không có kế hoạch gắn với lần gửi" },
    ]} />
    <p className="alert alert-info">Nội dung đã gửi được giữ nguyên. Lũy kế, % kế hoạch và kết quả kiểm tra tại thời điểm gửi chưa có ảnh chụp lịch sử trong màn này.</p>
    {other ? <p role="status">Đối chiếu lần gửi {data.submission.attempt} với lần gửi {other.submission.attempt}: {changed} ô thay đổi. Ô thay đổi được tô vàng; dòng điều chỉnh đối chiếu theo thứ tự đã gửi.</p> : null}
    <DataTable caption={other ? "So sánh nội dung các lần gửi" : "Nội dung đã gửi"} columns={columns} rows={fields} rowKey={row => row.key} empty="Lần gửi không có ô nhập hay lời văn." />
    <p className="mute">Log hiển thị sự kiện gửi và kết quả rà soát đã ghi nhận. Lịch sử lưu nháp chưa có trong khối này.</p>
  </DetailPage></div>;
}
