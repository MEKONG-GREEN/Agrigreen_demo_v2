"use client";
// Màn quản trị `cap/export` (W10.17): danh sách mẫu văn bản (phiên bản đăng ký, đã ghi sổ, lệch băm) và lịch sử lần
// xuất .docx của đơn vị (số văn bản, băm SHA-256, tải lại nguyên tệp). Chỉ đọc — sửa mẫu là gói W11.65.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { formatBytes, saveBlob } from "@/lib/files";
import { createExportApi, shortHash, type ExportApi } from "./api";

const when = (s?: string | null) => (s ? new Date(s).toLocaleString("vi-VN", { timeZone: "Asia/Ho_Chi_Minh" }) : "—");

export function ExportAdminScreen() {
  const { api } = useAuth();
  const ex = useMemo(() => (api ? createExportApi(api) : null), [api]);
  return (
    <>
      <PageHeader title="Mẫu văn bản & lịch sử xuất">
        <p>Mẫu Word có phiên bản; mỗi lần xuất lưu nguyên tệp kèm mã băm SHA-256 và số văn bản đã dùng.</p>
      </PageHeader>
      {ex ? <Body ex={ex} /> : null}
    </>
  );
}

function Body({ ex }: { ex: ExportApi }) {
  const [template, setTemplate] = useState("");
  const [error, setError] = useState<ReturnType<typeof toApiProblem> | null>(null);
  const templates = useAsync(() => ex.templates(), [ex]);
  const runs = useAsync(() => ex.runs({ template: template || undefined, limit: 100 }), [ex, template]);
  const problem = error ?? templates.error ?? runs.error;
  return (
    <>
      {problem ? <ProblemAlert problem={problem} onRetry={() => { setError(null); templates.reload(); runs.reload(); }} /> : null}
      <section className="card sec" aria-label="Mẫu văn bản">
        <h2>Mẫu văn bản</h2>
        {templates.loading ? <p>Đang tải…</p> : (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Khóa</th><th>Tên mẫu</th><th>Module</th><th>Phiên bản</th><th>Mục</th><th>Biến / danh sách</th></tr></thead>
              <tbody>
                {(templates.data ?? []).map((t) => (
                  <tr key={t.key}>
                    <td><code>{t.key}</code></td>
                    <td>{t.title}</td>
                    <td>{t.owner}</td>
                    <td>
                      {t.versions.map((v) => (
                        <div key={v.version} title={v.hash}>
                          v{v.version}{" "}
                          {v.drift ? <StatusPill tone="bad">Lệch nội dung</StatusPill> : v.recorded_at ? <StatusPill tone="ok">Đã dùng {when(v.recorded_at)}</StatusPill> : <StatusPill>Chưa dùng</StatusPill>}
                          {!v.registered ? <small> · không còn trong mã</small> : null}
                        </div>
                      ))}
                    </td>
                    <td>{t.sections.length}</td>
                    <td>{t.vars.length} / {t.lists.length}</td>
                  </tr>
                ))}
                {!templates.data?.length ? <tr><td colSpan={6}>Chưa có mẫu.</td></tr> : null}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <section className="card sec" aria-label="Lịch sử xuất">
        <h2>Lịch sử xuất</h2>
        <label>Mẫu{" "}
          <select value={template} onChange={(e) => setTemplate(e.target.value)}>
            <option value="">Tất cả</option>
            {(templates.data ?? []).map((t) => <option key={t.key} value={t.key}>{t.title}</option>)}
          </select>
        </label>
        {runs.loading ? <p>Đang tải…</p> : (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Thời điểm</th><th>Đối tượng</th><th>Mẫu</th><th>Số văn bản</th><th>Tệp</th><th>SHA-256</th><th></th></tr></thead>
              <tbody>
                {(runs.data ?? []).map((r) => (
                  <tr key={r.id}>
                    <td>{when(r.at)}</td>
                    <td><code>{r.subject}</code> · lần {r.seq}</td>
                    <td>{r.template} v{r.template_version}</td>
                    <td>{r.doc_number || "—"}{r.doc_number_auto ? <small> (tự cấp)</small> : null}</td>
                    <td>{r.file_name} · {formatBytes(r.size)}</td>
                    <td title={r.body_hash}><code>{shortHash(r.body_hash)}</code></td>
                    <td>
                      <button className="btn" onClick={() => { void ex.download(r.id).then((f) => saveBlob(f.blob, f.name), (e: unknown) => setError(toApiProblem(e))); }}>Tải lại</button>
                    </td>
                  </tr>
                ))}
                {!runs.data?.length ? <tr><td colSpan={7}>Chưa có lần xuất.</td></tr> : null}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </>
  );
}
