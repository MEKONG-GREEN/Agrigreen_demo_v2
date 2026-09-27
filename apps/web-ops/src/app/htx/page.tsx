"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { unitLabel } from "@/api/adapters/units";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { useUnits } from "@/lib/useUnits";

function useDebounced<T>(value: T, ms = 300): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

/** GET /v1/cooperatives — HTX/THT trong phạm vi, tìm theo tên, Mã HTX hoặc mã số thuế. */
export default function CooperativesPage() {
  const { api } = useAuth();
  const [q, setQ] = useState("");
  const [includeInactive, setIncludeInactive] = useState(false);
  const dq = useDebounced(q);

  const list = usePagedList(
    (cursor) =>
      unwrap(api.GET("/v1/cooperatives", { params: { query: { q: dq || undefined, include_inactive: includeInactive || undefined, cursor, limit: 20 } } })),
    [api, dq, includeInactive],
  );
  const units = useUnits(api, list.items.map((c) => c.admin_unit_code));

  return (
    <>
      <PageHead title="Quản lý HTX & nông hộ" subtitle="Danh sách HTX, tổ hợp tác trong phạm vi được phân quyền" />
      <div className="card sec">
        <form className="row" role="search" onSubmit={(e) => e.preventDefault()} style={{ marginBottom: 14 }}>
          <div className="field" style={{ flex: "1 1 260px" }}>
            <label htmlFor="htx-q">Tìm HTX</label>
            <input
              id="htx-q"
              className="input"
              type="search"
              placeholder="Tên, Mã HTX hoặc mã số thuế…"
              value={q}
              onChange={(e) => setQ(e.target.value)}
            />
          </div>
          <label className="row" style={{ alignSelf: "flex-end", paddingBottom: 8 }}>
            <input type="checkbox" checked={includeInactive} onChange={(e) => setIncludeInactive(e.target.checked)} />
            Gồm cả HTX đã vô hiệu hoá
          </label>
        </form>

        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Danh sách HTX</caption>
            <thead>
              <tr>
                <th scope="col">HTX / THT</th>
                <th scope="col">Địa bàn</th>
                <th scope="col">Mã số thuế</th>
                <th scope="col" className="num">Diện tích đăng ký (ha)</th>
                <th scope="col">Trạng thái</th>
              </tr>
            </thead>
            <tbody>
              {list.items.map((c) => (
                <tr key={c.ref}>
                  <td>
                    <Link href={`/htx/${c.ref}`} style={{ fontWeight: 600 }}>
                      {c.name}
                    </Link>
                    <div className="mute small">{c.code}</div>
                  </td>
                  <td>{unitLabel(units, c.admin_unit_code)}</td>
                  <td>{c.tax_code ?? "—"}</td>
                  <td className="num">{formatDecimal(c.area_ha, 1)}</td>
                  <td>
                    {c.active ? (
                      <span className="chip c-ok">Đang hoạt động</span>
                    ) : (
                      <span className="chip c-mute" title={c.status_reason ?? undefined}>
                        Đã vô hiệu hoá
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {list.loading ? <Loading /> : null}
        {!list.loading && !list.error && list.items.length === 0 ? (
          <Empty>
            <Icon name="search" /> Không có HTX phù hợp bộ lọc.
          </Empty>
        ) : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </div>
    </>
  );
}
