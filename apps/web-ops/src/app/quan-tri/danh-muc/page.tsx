"use client";

import { useState } from "react";
import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { useAsync } from "@/lib/useAsync";

export default function CatalogsPage() {
  const { api } = useAuth();
  const catalogs = useAsync(() => unwrap(api.GET("/v1/catalogs")), [api]);
  const [selected, setSelected] = useState<string | null>(null);
  const [includeInactive, setIncludeInactive] = useState(true);
  const current = selected ?? catalogs.data?.items[0]?.catalog ?? null;

  const items = useAsync(
    () =>
      current
        ? unwrap(api.GET("/v1/catalogs/{catalog}", { params: { path: { catalog: current }, query: { include_inactive: includeInactive } } }))
        : Promise.resolve({ items: [] }),
    [api, current, includeInactive],
  );
  const currentName = catalogs.data?.items.find((c) => c.catalog === current)?.name ?? "";

  return (
    <>
      <PageHead title="Danh mục dùng chung" subtitle="Giống lúa, khâu sản xuất, chủng loại máy, mùa vụ, sâu bệnh… dùng chung cho mọi ứng dụng" />
      <div className="alert alert-info" style={{ marginBottom: 16 }}>
        <Icon name="info" />
        <span>Mục danh mục không bị xoá — chỉ chuyển sang “Ngừng dùng” để giữ lịch sử tra cứu. Thêm/sửa mục đi qua Admin API (cần MFA).</span>
      </div>
      {catalogs.error ? <ProblemAlert problem={catalogs.error} onRetry={catalogs.reload} /> : null}
      <div className="grid2" style={{ gridTemplateColumns: "minmax(220px, 280px) minmax(0, 1fr)", alignItems: "start" }}>
        <nav className="card sec" aria-label="Chọn danh mục">
          {catalogs.loading && !catalogs.data ? <Loading /> : null}
          <ul style={{ listStyle: "none" }} className="stack" role="list">
            {catalogs.data?.items.map((c) => (
              <li key={c.catalog} style={{ margin: 0 }}>
                <button
                  type="button"
                  className="persona"
                  aria-current={c.catalog === current ? "true" : undefined}
                  style={c.catalog === current ? { borderColor: "var(--g600)", background: "var(--g50)" } : undefined}
                  onClick={() => setSelected(c.catalog)}
                >
                  <b>{c.name}</b>
                  <span className="mute small">
                    <code>{c.catalog}</code> · {c.item_count} mục
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </nav>
        <section className="card sec" aria-labelledby="cat-title">
          <div className="sec-h">
            <h2 id="cat-title">{currentName || "Danh mục"}</h2>
            <label className="row small">
              <input type="checkbox" checked={includeInactive} onChange={(e) => setIncludeInactive(e.target.checked)} />
              Hiện cả mục ngừng dùng
            </label>
          </div>
          {items.error ? <ProblemAlert problem={items.error} onRetry={items.reload} /> : null}
          {items.loading ? <Loading /> : null}
          {items.data && items.data.items.length === 0 ? <Empty>Danh mục trống.</Empty> : null}
          {items.data && items.data.items.length > 0 ? (
            <div className="tbl-wrap">
              <table className="tbl">
                <caption className="sr-only">Các mục của danh mục {currentName}</caption>
                <thead>
                  <tr>
                    <th scope="col">Mã</th>
                    <th scope="col">Tên</th>
                    <th scope="col">Thuộc tính</th>
                    <th scope="col">Trạng thái</th>
                  </tr>
                </thead>
                <tbody>
                  {items.data.items.map((i) => (
                    <tr key={i.code}>
                      <td>
                        <code>{i.code}</code>
                      </td>
                      <td style={{ fontWeight: 600 }}>{i.name}</td>
                      <td className="small">
                        {Object.entries(i.attrs).length === 0
                          ? "—"
                          : Object.entries(i.attrs).map(([k, v]) => (
                              <span key={k} className="chip c-mute" style={{ marginRight: 4 }}>
                                {k}: {v}
                              </span>
                            ))}
                      </td>
                      <td>{i.active ? <span className="chip c-ok">Đang dùng</span> : <span className="chip c-mute">Ngừng dùng</span>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </section>
      </div>
    </>
  );
}
