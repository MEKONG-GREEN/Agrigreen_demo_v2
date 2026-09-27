"use client";

import { useState } from "react";
import { ATTR_LABEL, CATALOGS } from "@/admin/catalogs";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import type { CatalogName } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";

/** Danh mục dùng chung — Admin API GET /v1/masterdata/catalogs/{catalog}/items (MFA bắt buộc). */
export default function CatalogsPage() {
  const { adminApi, principal } = useAuth();
  const [current, setCurrent] = useState<CatalogName>("rice_variety");
  const [includeInactive, setIncludeInactive] = useState(true);

  const items = usePagedList(
    (cursor) =>
      unwrap(
        adminApi.GET("/v1/masterdata/catalogs/{catalog}/items", {
          params: { path: { catalog: current }, query: { include_inactive: includeInactive, cursor, limit: 50 } },
        }),
      ),
    [adminApi, current, includeInactive],
  );
  const meta = CATALOGS.find((c) => c.catalog === current);

  return (
    <>
      <PageHead title="Danh mục dùng chung" subtitle="Giống lúa, khâu sản xuất, chủng loại máy, mùa vụ, sâu bệnh… dùng chung cho mọi ứng dụng" />
      <div className="alert alert-info" style={{ marginBottom: 16 }}>
        <Icon name="info" />
        <span>
          Mục danh mục không bị xoá — chỉ chuyển sang “Ngừng dùng” (kèm lý do) để giữ lịch sử tra cứu. Màn hình này đọc qua Admin API
          (máy chủ quản trị riêng, bắt buộc xác thực hai yếu tố).
        </span>
      </div>
      {principal && !principal.mfa ? (
        <div className="alert alert-warn" style={{ marginBottom: 16 }} role="status">
          <Icon name="alert" />
          <span>Phiên hiện tại chưa qua xác thực hai yếu tố — Admin API sẽ từ chối (mfa_required).</span>
        </div>
      ) : null}
      <div className="grid2" style={{ gridTemplateColumns: "minmax(220px, 280px) minmax(0, 1fr)", alignItems: "start" }}>
        <nav className="card sec" aria-label="Chọn danh mục">
          <ul style={{ listStyle: "none" }} className="stack" role="list">
            {CATALOGS.map((c) => (
              <li key={c.catalog} style={{ margin: 0 }}>
                <button
                  type="button"
                  className="persona"
                  aria-current={c.catalog === current ? "true" : undefined}
                  style={c.catalog === current ? { borderColor: "var(--g600)", background: "var(--g50)" } : undefined}
                  onClick={() => setCurrent(c.catalog)}
                >
                  <b>{c.name}</b>
                  <span className="mute small">
                    <code>{c.catalog}</code>
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
              {meta ? <span className="mute small">Thuộc tính: {meta.hint}</span> : null}
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
