"use client";

// FN-AD-04.06 — Chi tiết mẫu (mẫu Detail của P6): thông tin mẫu, lịch sử phiên bản (tạo nháp mới, khôi phục
// phiên bản cũ thành nháp, ngừng dùng phiên bản), gán loại báo cáo, ngừng dùng mẫu (kèm lý do), khung Log.
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { ActionBar, BackLink, DetailLayout, LabelValueBlock, LogPanel } from "@/components/patterns/detail";
import { StatusPill } from "@/components/patterns/list";
import { Loading } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { saveBlob } from "@/lib/files";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import { assignForm, downloadTemplateXlsx, formLog, getForm, newFormVersion, retireForm, retireVersion, revokeAssignment } from "../api";
import { LEVEL_LABEL, REPORT_TYPE_LABEL, type FormReportType, type FormVersion } from "../types";
import { deptName, FormProblem, toLogEntries, VersionPill } from "../ui";
import { formsBase } from "./FormListScreen";

const ORIGIN_LABEL: Record<FormVersion["origin"], string> = { blank: "Mẫu trống", clone: "Nhân bản", restore: "Khôi phục", import: "Nạp Excel", seed: "Bộ 14 mẫu" };

type ReasonAction = { kind: "retire-form" } | { kind: "retire-version"; v: number } | { kind: "revoke"; id: string; label: string };

export function FormDetailScreen({ dept, id }: { dept: string; id: string }) {
  const { adminApi, api } = useAuth();
  const router = useRouter();
  const base = formsBase(dept);
  const detail = useAsync(() => getForm(adminApi, id), [adminApi, id]);
  const log = useAsync(() => formLog(adminApi, id), [adminApi, id]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [reasonFor, setReasonFor] = useState<ReasonAction | null>(null);
  const [newType, setNewType] = useState<FormReportType | "">("");
  const d = detail.data;
  useBreadcrumbTail(d ? [{ label: `${d.template.code} · ${d.template.name}` }] : []);
  const reload = () => {
    detail.reload();
    log.reload();
  };
  const run = async (fn: () => Promise<unknown>, after?: (r: unknown) => void) => {
    setBusy(true);
    setProblem(null);
    try {
      const r = await fn();
      reload();
      after?.(r);
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };

  if (detail.loading && !d) return <Loading />;
  if (detail.error && !d) return <FormProblem problem={detail.error} />;
  if (!d) return null;
  const t = d.template;
  const versions = d.versions;
  const open = versions.find((v) => v.status === "draft" || v.status === "proposed");
  const latest = versions[0];
  const published = versions.filter((v) => v.status === "published");
  const active = t.status === "active";
  const activeAsg = d.assignments.filter((a) => a.status === "active");
  const download = async (v: FormVersion) => {
    setProblem(null);
    try {
      const f = await downloadTemplateXlsx(api, t.id, v.version, `${t.code}-v${v.version}.xlsx`);
      saveBlob(f.blob, f.name);
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };

  return (
    <>
      <BackLink href={base}>Danh sách mẫu báo cáo</BackLink>
      <div className="pg-head">
        <h1>
          {t.code} · {t.name} {active ? <StatusPill tone="ok">Đang dùng</StatusPill> : <StatusPill tone="bad">Ngừng dùng</StatusPill>}
        </h1>
      </div>
      {active ? (
        <ActionBar>
          {open ? (
            <Link className="btn btn-p btn-sm" href={`${base}/${t.id}/v/${open.version}`}>
              <Icon name="edit" size={15} /> Mở nháp v{open.version}
            </Link>
          ) : (
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={busy}
              onClick={() => void run(() => newFormVersion(adminApi, t.id, {}), (r) => router.push(`${base}/${t.id}/v/${(r as FormVersion).version}`))}
            >
              + Tạo phiên bản mới (nháp)
            </button>
          )}
          {latest ? (
            <button type="button" className="btn btn-o btn-sm" onClick={() => void download(open ?? published[0] ?? latest)}>
              <Icon name="download" size={15} /> Tải mẫu Excel
            </button>
          ) : null}
          <button type="button" className="btn btn-o btn-sm" onClick={() => setReasonFor({ kind: "retire-form" })}>
            Ngừng dùng mẫu
          </button>
        </ActionBar>
      ) : null}
      {problem ? <FormProblem problem={problem} onClose={() => setProblem(null)} /> : null}
      <DetailLayout
        main={
          <>
            <LabelValueBlock
              title="Thông tin mẫu"
              items={[
                { label: "Mã", value: t.code },
                { label: "Tên mẫu", value: t.name },
                { label: "Phòng ban", value: deptName(t.department_code) },
                { label: "Cấp nhập liệu", value: LEVEL_LABEL[t.entry_level] },
                { label: "Loại báo cáo", value: activeAsg.length ? activeAsg.map((a) => REPORT_TYPE_LABEL[a.report_type]).join(", ") : "Chưa gán" },
                { label: "Lý do ngừng dùng", value: t.retired_reason },
                { label: "Tạo lúc", value: formatDateTime(t.created_at) },
                { label: "Cập nhật", value: formatDateTime(t.updated_at) },
              ]}
            />
            <section className="lv-block">
              <h2 id="lich-su-pb">Lịch sử phiên bản</h2>
              <div className="tbl-wrap">
                <table className="dt" aria-labelledby="lich-su-pb">
                  <thead>
                    <tr>
                      <th scope="col">Phiên bản</th>
                      <th scope="col">Trạng thái</th>
                      <th scope="col">Áp dụng từ kỳ</th>
                      <th scope="col">Nguồn</th>
                      <th scope="col" className="num">
                        Số cột
                      </th>
                      <th scope="col">Phát hành</th>
                      <th scope="col">
                        <span className="sr-only">Thao tác</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {versions.map((v) => (
                      <tr key={v.id}>
                        <td>
                          <Link href={`${base}/${t.id}/v/${v.version}`}>v{v.version}</Link>
                        </td>
                        <td>
                          <VersionPill status={v.status} />
                        </td>
                        <td>{v.effective_label ?? v.effective_from ?? "—"}</td>
                        <td>
                          {ORIGIN_LABEL[v.origin]}
                          {v.pending_count ? <span className="mute small"> · {v.pending_count} phần chờ khách</span> : null}
                        </td>
                        <td className="num">{v.column_count}</td>
                        <td className="small">{v.published_at ? formatDateTime(v.published_at) : "—"}</td>
                        <td>
                          <span className="row" style={{ gap: 6 }}>
                            {active && !open && v.version !== latest?.version ? (
                              <button
                                type="button"
                                className="btn btn-o btn-sm"
                                disabled={busy}
                                onClick={() => void run(() => newFormVersion(adminApi, t.id, { from_version: v.version, restore: true }), (r) => router.push(`${base}/${t.id}/v/${(r as FormVersion).version}`))}
                              >
                                Khôi phục thành nháp
                              </button>
                            ) : null}
                            {v.status === "published" ? (
                              <button type="button" className="btn btn-o btn-sm" onClick={() => setReasonFor({ kind: "retire-version", v: v.version })}>
                                Ngừng dùng v{v.version}
                              </button>
                            ) : null}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <p className="fd-hint" style={{ marginTop: 6 }}>
                Phiên bản đã phát hành bất biến; kỳ đã có số liệu giữ phiên bản cũ — sửa mẫu là tạo phiên bản mới áp dụng từ kỳ sau.
              </p>
            </section>
            <section className="lv-block">
              <h2 id="gan-loai">Gán vào loại báo cáo</h2>
              <ul className="fd-list" aria-labelledby="gan-loai">
                {d.assignments.map((a) => (
                  <li key={a.id}>
                    <span>
                      {REPORT_TYPE_LABEL[a.report_type]} · {LEVEL_LABEL[a.entry_level]} · {deptName(a.department_code)}{" "}
                      {a.status === "active" ? <StatusPill tone="ok">Đang gán</StatusPill> : <StatusPill>Đã bỏ gán{a.revoke_reason ? ` — ${a.revoke_reason}` : ""}</StatusPill>}
                    </span>
                    {a.status === "active" && active ? (
                      <button type="button" className="btn btn-o btn-sm" onClick={() => setReasonFor({ kind: "revoke", id: a.id, label: REPORT_TYPE_LABEL[a.report_type] })}>
                        Bỏ gán
                      </button>
                    ) : null}
                  </li>
                ))}
                {!d.assignments.length ? <li className="mute">Chưa gán — mẫu chưa gán loại báo cáo không phát hành được.</li> : null}
              </ul>
              {active ? (
                <div className="row" style={{ marginTop: 10 }}>
                  <label className="sr-only" htmlFor="fd-type">
                    Loại báo cáo cần gán
                  </label>
                  <select id="fd-type" className="select" value={newType} onChange={(e) => setNewType(e.target.value as FormReportType | "")}>
                    <option value="">Chọn loại báo cáo</option>
                    {Object.entries(REPORT_TYPE_LABEL)
                      .filter(([k]) => !activeAsg.some((a) => a.report_type === k))
                      .map(([k, l]) => (
                        <option key={k} value={k}>
                          {l}
                        </option>
                      ))}
                  </select>
                  <button
                    type="button"
                    className="btn btn-o btn-sm"
                    disabled={!newType || busy}
                    onClick={() => void run(() => assignForm(adminApi, t.id, newType as FormReportType), () => setNewType(""))}
                  >
                    Gán mẫu
                  </button>
                </div>
              ) : null}
            </section>
          </>
        }
        log={<LogPanel title="Log" entries={toLogEntries(log.data?.items ?? [])} empty={log.loading ? "Đang tải…" : "Chưa có thay đổi nào."} />}
      />
      {reasonFor ? (
        <ReasonDialog
          title={reasonFor.kind === "retire-form" ? `Ngừng dùng mẫu ${t.code}` : reasonFor.kind === "retire-version" ? `Ngừng dùng ${t.code} v${reasonFor.v}` : `Bỏ gán ${reasonFor.label}`}
          hint={
            reasonFor.kind === "retire-form"
              ? "Không xóa (RB-1): mẫu không tạo nháp, không gán, không phát hành tiếp; số liệu đã có giữ nguyên."
              : reasonFor.kind === "retire-version"
                ? "Công việc đã ghim phiên bản này giữ nguyên; phát hành mới dùng phiên bản khác."
                : "Công việc đã phát hành giữ nguyên."
          }
          onClose={() => setReasonFor(null)}
          onSubmit={async (reason) => {
            const r = reasonFor;
            if (r.kind === "retire-form") await retireForm(adminApi, t.id, reason);
            else if (r.kind === "retire-version") await retireVersion(adminApi, t.id, r.v, reason);
            else await revokeAssignment(adminApi, r.id, reason);
            setReasonFor(null);
            reload();
          }}
        />
      ) : null}
    </>
  );
}

export function ReasonDialog({ title, hint, onClose, onSubmit }: { title: string; hint?: string; onClose: () => void; onSubmit: (reason: string) => Promise<void> }) {
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const ok = Array.from(reason.trim()).length >= 10;
  return (
    <Dialog
      title={title}
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-d btn-sm"
          disabled={!ok || busy}
          onClick={() => {
            setBusy(true);
            setProblem(null);
            onSubmit(reason.trim()).catch((e: unknown) => {
              setProblem(toApiProblem(e));
              setBusy(false);
            });
          }}
        >
          Xác nhận
        </button>
      }
    >
      <div className="field">
        <label htmlFor="fd-reason">Lý do (10–500 ký tự) *</label>
        <textarea id="fd-reason" className="textarea" value={reason} maxLength={500} onChange={(e) => setReason(e.target.value)} />
      </div>
      {hint ? <p className="fd-hint" style={{ marginTop: 8 }}>{hint}</p> : null}
      {problem ? <FormProblem problem={problem} /> : null}
    </Dialog>
  );
}
