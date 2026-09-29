"use client";

import { useState, type ChangeEvent, type FormEvent } from "react";
import { ATTR_LABEL } from "@/admin/catalogs";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { CatalogImportResult, CatalogName } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { ResultList } from "@/htx/common";
import { toCsv } from "@/htx/rules";
import { readUpload, saveBlob, type UploadFile } from "@/lib/files";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

const IMPORT_STATUS = { created: "Đã tạo", skipped: "Bỏ qua (đã có)", rejected: "Lỗi" } as const;

/** Nhập danh mục hàng loạt từ Excel/CSV (Admin POST /v1/masterdata/catalog-imports, HTX US-CAT-02): dòng lỗi kèm lý do ngay trên màn hình. */
function CatalogImport({ onDone }: { onDone: () => void }) {
  const { adminApi } = useAuth();
  const [file, setFile] = useState<UploadFile | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<CatalogImportResult | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);
  const ok = !!file && (file.kind === "xlsx" || file.kind === "csv");
  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setResult(null);
    setProblem(null);
    setFile(f ? await readUpload(f) : null);
    setKey(newIdempotencyKey());
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!file || !ok) return;
    setBusy(true);
    setProblem(null);
    try {
      const res = await unwrap(
        adminApi.POST("/v1/masterdata/catalog-imports", {
          params: { header: { "Idempotency-Key": key } },
          body: file.body as unknown as string,
          bodySerializer: (b: unknown) => b as BodyInit,
          headers: { "Content-Type": file.contentType },
        }),
      );
      setResult(res);
      onDone();
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  const template = () =>
    saveBlob(
      new Blob([toCsv([["catalog", "code", "name", "growth_days", "expected_yield_t_per_ha"], ["rice_variety", "OM9582", "OM9582", "95", "6.8"]])], { type: "text/csv;charset=utf-8" }),
      "mau-nhap-danh-muc.csv",
    );
  return (
    <form className="panel stack" style={{ gap: 8, marginBottom: 16 }} onSubmit={(e) => void submit(e)} aria-label="Nhập danh mục từ tệp">
      <b>Nhập danh mục hàng loạt (.xlsx hoặc CSV, tối đa 500 dòng)</b>
      <span className="small mute">Cột catalog (mã danh mục, vd rice_variety), code, name; cột khác là thuộc tính (vd growth_days). Dòng hợp lệ được tạo trong một lần nhập, dòng lỗi không chặn dòng khác.</span>
      <div className="row">
        <input type="file" aria-label="Tệp danh mục" accept=".xlsx,.csv" onChange={(e) => void onFile(e)} />
        <button type="submit" className="btn btn-p btn-sm" disabled={!ok || busy}>
          {busy ? "Đang nhập…" : "Nhập"}
        </button>
        <button type="button" className="btn btn-o btn-sm" onClick={template}>
          <Icon name="download" size={15} />
          Tải tệp mẫu
        </button>
      </div>
      {file && !ok ? <span className="err small">Chỉ nhận .xlsx hoặc CSV UTF-8 (không nhận .xls cũ).</span> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {result ? (
        <>
          <p role="status">
            <b>
              {result.created} dòng đã tạo · {result.skipped} bỏ qua · {result.rejected} dòng lỗi (tổng {result.total})
            </b>
          </p>
          <ResultList
            caption="Kết quả nhập danh mục theo dòng"
            rows={result.rows.map((r) => ({
              key: String(r.row),
              label: `Dòng ${r.row}${r.code ? ` · ${r.catalog ?? ""}/${r.code}` : ""}`,
              ok: r.status !== "rejected",
              status: IMPORT_STATUS[r.status],
              reason: r.reason,
            }))}
          />
        </>
      ) : null}
    </form>
  );
}

/** Danh mục dùng chung — Admin API GET /v1/masterdata/catalogs (G-06) + /{catalog}/items (MFA bắt buộc). */
export default function CatalogsPage() {
  const { adminApi } = useAuth();
  const catalogs = useAsync(() => unwrap(adminApi.GET("/v1/masterdata/catalogs")), [adminApi]);
  const [picked, setPicked] = useState<CatalogName | null>(null);
  const current = (picked ?? catalogs.data?.items[0]?.catalog ?? "rice_variety") as CatalogName;
  const setCurrent = setPicked;
  const [includeInactive, setIncludeInactive] = useState(true);
  const [showImport, setShowImport] = useState(false);

  const items = usePagedList(
    (cursor) =>
      unwrap(
        adminApi.GET("/v1/masterdata/catalogs/{catalog}/items", {
          params: { path: { catalog: current }, query: { include_inactive: includeInactive, cursor, limit: 50 } },
        }),
      ),
    [adminApi, current, includeInactive],
  );
  const meta = catalogs.data?.items.find((c) => c.catalog === current);

  return (
    <>
      <PageHead
        title="Danh mục dùng chung"
        subtitle="Giống lúa, khâu sản xuất, chủng loại máy, mùa vụ, sâu bệnh… dùng chung cho mọi ứng dụng"
        actions={
          <button type="button" className="btn btn-o btn-sm" aria-expanded={showImport} onClick={() => setShowImport((v) => !v)}>
            <Icon name="upload" size={15} />
            Nhập từ Excel
          </button>
        }
      />
      {showImport ? (
        <CatalogImport
          onDone={() => {
            catalogs.reload();
            items.reload();
          }}
        />
      ) : null}
      <div className="alert alert-info" style={{ marginBottom: 16 }}>
        <Icon name="info" />
        <span>
          Mục danh mục không bị xoá — chỉ chuyển sang “Ngừng dùng” (kèm lý do) để giữ lịch sử tra cứu. Màn hình này đọc qua Admin API
          (máy chủ quản trị riêng, bắt buộc xác thực hai yếu tố).
        </span>
      </div>
      <MfaNotice />
      {catalogs.error ? <ProblemAlert problem={catalogs.error} onRetry={catalogs.reload} /> : null}
      <div className="grid2" style={{ gridTemplateColumns: "minmax(220px, 280px) minmax(0, 1fr)", alignItems: "start" }}>
        <nav className="card sec" aria-label="Chọn danh mục">
          {catalogs.loading && !catalogs.data ? <Loading /> : null}
          <ul style={{ listStyle: "none" }} className="stack" role="list">
            {(catalogs.data?.items ?? []).map((c) => (
              <li key={c.catalog} style={{ margin: 0 }}>
                <button
                  type="button"
                  className="persona"
                  aria-current={c.catalog === current ? "true" : undefined}
                  style={c.catalog === current ? { borderColor: "var(--g600)", background: "var(--g50)" } : undefined}
                  onClick={() => setCurrent(c.catalog as CatalogName)}
                >
                  <b>{c.name}</b>
                  <span className="mute small">
                    <code>{c.catalog}</code> · {c.item_count} mục{c.inactive_count ? ` (${c.inactive_count} ngừng dùng)` : ""}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </nav>
        <section className="card sec" aria-labelledby="cat-title">
          <div className="sec-h">
            <div>
              <h2 id="cat-title">{meta?.name ?? current}</h2>
              {meta && meta.attrs_schema.length > 0 ? (
                <span className="mute small">Thuộc tính: {meta.attrs_schema.map((a) => ATTR_LABEL[a] ?? a).join(", ")}</span>
              ) : null}
            </div>
            <label className="row small">
              <input type="checkbox" checked={includeInactive} onChange={(e) => setIncludeInactive(e.target.checked)} />
              Hiện cả mục ngừng dùng
            </label>
          </div>
          {items.error ? <ProblemAlert problem={items.error} onRetry={items.reload} /> : null}
          {items.items.length > 0 ? (
            <div className="tbl-wrap">
              <table className="tbl">
                <caption className="sr-only">Các mục của danh mục {meta?.name ?? current}</caption>
                <thead>
                  <tr>
                    <th scope="col">Mã</th>
                    <th scope="col">Tên</th>
                    <th scope="col">Thuộc tính</th>
                    <th scope="col">Trạng thái</th>
                    <th scope="col">Cập nhật</th>
                  </tr>
                </thead>
                <tbody>
                  {items.items.map((i) => (
                    <tr key={i.code}>
                      <td>
                        <code>{i.code}</code>
                      </td>
                      <td style={{ fontWeight: 600 }}>{i.name}</td>
                      <td className="small">
                        {Object.entries(i.attrs).length === 0
                          ? "—"
                          : Object.entries(i.attrs).map(([k, v]) => (
                              <span key={k} className="chip c-mute" style={{ marginRight: 4 }} title={k}>
                                {ATTR_LABEL[k] ?? k}: {v}
                              </span>
                            ))}
                      </td>
                      <td>
                        {i.active ? (
                          <span className="chip c-ok">Đang dùng</span>
                        ) : (
                          <>
                            <span className="chip c-mute">Ngừng dùng</span>
                            {i.deactivation_reason ? <div className="mute small">{i.deactivation_reason}</div> : null}
                          </>
                        )}
                      </td>
                      <td className="small">
                        {formatDateTime(i.updated_at)}
                        <div className="mute">Lần sửa {i.revision}</div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
          {items.loading ? <Loading /> : null}
          {!items.loading && !items.error && items.items.length === 0 ? <Empty>Danh mục trống.</Empty> : null}
          <LoadMore hasMore={items.hasMore} loading={items.loading} onClick={items.loadMore} />
        </section>
      </div>
    </>
  );
}
