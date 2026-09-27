"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import type { CooperativeStatus } from "@/api/pending/party";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { useAsync } from "@/lib/useAsync";

function useDebounced<T>(value: T, ms = 300): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

export default function CooperativesPage() {
  const { api, principal } = useAuth();
  const [q, setQ] = useState("");
  const [unit, setUnit] = useState("");
  const [status, setStatus] = useState<"" | CooperativeStatus>("");
  const dq = useDebounced(q);

  const provinces = useAsync(() => unwrap(api.GET("/v1/admin-units", { params: { query: { level: "province" } } })), [api]);
  const list = usePagedList(
    (cursor) =>
      unwrap(
        api.GET("/v1/cooperatives", {
          params: { query: { q: dq || undefined, admin_unit: unit || undefined, status: status || undefined, cursor, limit: 20 } },
        }),
      ),
    [api, dq, unit, status],
  );

  const showProvinceFilter = principal?.level === "tw" || principal?.scopeAll;

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
              placeholder="Tên, mã HTX hoặc xã…"
              value={q}
              onChange={(e) => setQ(e.target.value)}
            />
          </div>
          {showProvinceFilter ? (
            <div className="field">
              <label htmlFor="htx-unit">Tỉnh/thành</label>
              <select id="htx-unit" className="select" value={unit} onChange={(e) => setUnit(e.target.value)}>
                <option value="">Tất cả</option>
                {provinces.data?.items.map((p) => (
                  <option key={p.code} value={p.code}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>
          ) : null}
          <div className="field">
            <label htmlFor="htx-status">Trạng thái</label>
            <select id="htx-status" className="select" value={status} onChange={(e) => setStatus(e.target.value as "" | CooperativeStatus)}>
              <option value="">Tất cả</option>
              <option value="active">Đang hoạt động</option>
              <option value="inactive">Ngừng hoạt động</option>
            </select>
          </div>
        </form>

        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Danh sách HTX</caption>
            <thead>
              <tr>
                <th scope="col">HTX / THT</th>
                <th scope="col">Địa bàn</th>
                <th scope="col" className="num">Hộ thành viên</th>
                <th scope="col" className="num">Diện tích (ha)</th>
                <th scope="col" className="num">Thửa số hoá</th>
                <th scope="col">Trạng thái</th>
              </tr>
            </thead>
            <tbody>
              {list.items.map((c) => (
                <tr key={c.id}>
                  <td>
                    <Link href={`/htx/${c.id}`} style={{ fontWeight: 600 }}>
                      {c.name}
                    </Link>
                    <div className="mute small">{c.code}</div>
                  </td>
                  <td>
                    {c.commune_name}
                    <div className="mute small">{c.province_name}</div>
                  </td>
                  <td className="num">{formatInt(c.member_households)}</td>
                  <td className="num">{formatDecimal(c.area_ha, 1)}</td>
                  <td className="num">{formatInt(c.digitized_plots)}</td>
                  <td>
                    {c.status === "active" ? <span className="chip c-ok">Đang hoạt động</span> : <span className="chip c-mute">Ngừng hoạt động</span>}
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
