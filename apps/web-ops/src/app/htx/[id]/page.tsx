"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import type { Cooperative, HouseholdSummary } from "@/api/pending/party";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

const CONSENT: Record<HouseholdSummary["consent"], { cls: string; text: string }> = {
  granted: { cls: "c-ok", text: "Đã đồng ý" },
  withdrawn: { cls: "c-bad", text: "Đã rút" },
  none: { cls: "c-mute", text: "Chưa ghi nhận" },
};

function Overview({ c }: { c: Cooperative }) {
  const rows: [string, string][] = [
    ["Mã HTX", c.code],
    ["Loại hình", c.kind === "htx" ? "Hợp tác xã" : "Tổ hợp tác"],
    ["Địa bàn", `${c.commune_name}, ${c.province_name}`],
    ["Ngày thành lập", formatDate(c.founded_on)],
    ["Hộ thành viên", formatInt(c.member_households)],
    ["Diện tích canh tác", `${formatDecimal(c.area_ha, 2)} ha`],
    ["Thửa đã số hoá", formatInt(c.digitized_plots)],
    ["Cán bộ phụ trách", c.extension_officer ?? "Chưa phân công"],
    ["Cập nhật", formatDateTime(c.updated_at)],
  ];
  return (
    <dl style={{ display: "grid", gridTemplateColumns: "minmax(140px, 220px) 1fr", gap: "10px 16px" }}>
      {rows.map(([k, v]) => (
        <div key={k} style={{ display: "contents" }}>
          <dt className="mute">{k}</dt>
          <dd style={{ fontWeight: 600 }}>{v}</dd>
        </div>
      ))}
    </dl>
  );
}

function Households({ id }: { id: string }) {
  const { api } = useAuth();
  const list = usePagedList(
    (cursor) => unwrap(api.GET("/v1/cooperatives/{id}/households", { params: { path: { id }, query: { cursor, limit: 15 } } })),
    [api, id],
  );
  return (
    <>
      <div className="alert alert-info" style={{ marginBottom: 12 }}>
        <Icon name="info" />
        <span>
          Thông tin định danh nông hộ (họ tên, SĐT, CCCD, địa chỉ) được lưu tách trong kho định danh mã hoá. Web vận hành chỉ
          hiển thị <b style={{ display: "inline" }}>nhãn đã che</b>.
        </span>
      </div>
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      <div className="tbl-wrap">
        <table className="tbl">
          <caption className="sr-only">Nông hộ thành viên (nhãn đã che)</caption>
          <thead>
            <tr>
              <th scope="col">Nông hộ (đã che)</th>
              <th scope="col">Xã</th>
              <th scope="col" className="num">Số thửa</th>
              <th scope="col" className="num">Diện tích (ha)</th>
              <th scope="col">Đồng ý chia sẻ dữ liệu</th>
              <th scope="col">Tham gia</th>
            </tr>
          </thead>
          <tbody>
            {list.items.map((h) => (
              <tr key={h.ref}>
                <td style={{ fontFamily: "ui-monospace, Consolas, monospace" }}>{h.masked_label}</td>
                <td>{h.commune_name}</td>
                <td className="num">{h.plots}</td>
                <td className="num">{formatDecimal(h.area_ha, 2)}</td>
                <td>
                  <span className={`chip ${CONSENT[h.consent].cls}`}>{CONSENT[h.consent].text}</span>
                </td>
                <td>{formatDate(h.joined_on)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {list.loading ? <Loading /> : null}
      {!list.loading && list.items.length === 0 && !list.error ? <Empty>Chưa có nông hộ.</Empty> : null}
      <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
    </>
  );
}

function Plots({ id }: { id: string }) {
  const { api } = useAuth();
  const list = usePagedList(
    (cursor) => unwrap(api.GET("/v1/plots", { params: { query: { cooperative_id: id, cursor, limit: 15 } } })),
    [api, id],
  );
  return (
    <>
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      <div className="tbl-wrap">
        <table className="tbl">
          <caption className="sr-only">Thửa ruộng của HTX</caption>
          <thead>
            <tr>
              <th scope="col">Mã thửa</th>
              <th scope="col">Cây trồng</th>
              <th scope="col" className="num">Diện tích (ha)</th>
              <th scope="col">Ranh giới</th>
              <th scope="col">Cập nhật</th>
            </tr>
          </thead>
          <tbody>
            {list.items.map((p) => (
              <tr key={p.id}>
                <td>{p.label}</td>
                <td>{p.crop ?? "—"}</td>
                <td className="num">{formatDecimal(p.area_ha, 2)}</td>
                <td>{p.digitized ? <span className="chip c-ok">Đã số hoá</span> : <span className="chip c-warn">Chưa vẽ</span>}</td>
                <td>{formatDate(p.updated_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {list.loading ? <Loading /> : null}
      <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
    </>
  );
}

export default function CooperativeDetailPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id ?? "";
  const { api } = useAuth();
  const [tab, setTab] = useState("overview");
  const coop = useAsync(() => unwrap(api.GET("/v1/cooperatives/{id}", { params: { path: { id } } })), [api, id]);

  return (
    <>
      <p style={{ marginBottom: 8 }}>
        <Link href="/htx">← Danh sách HTX</Link>
      </p>
      {coop.error ? <ProblemAlert problem={coop.error} onRetry={coop.reload} /> : null}
      {coop.loading && !coop.data ? <Loading /> : null}
      {coop.data ? (
        <>
          <PageHead
            title={coop.data.name}
            subtitle={`${coop.data.code} · ${coop.data.commune_name}, ${coop.data.province_name}`}
            actions={
              coop.data.status === "active" ? <span className="chip c-ok">Đang hoạt động</span> : <span className="chip c-mute">Ngừng hoạt động</span>
            }
          />
          <div className="card sec">
            <Tabs
              label="Thông tin HTX"
              value={tab}
              onChange={setTab}
              tabs={[
                { id: "overview", label: "Tổng quan" },
                { id: "households", label: `Nông hộ (${formatInt(coop.data.member_households)})` },
                { id: "plots", label: "Thửa ruộng" },
              ]}
            >
              {tab === "overview" ? <Overview c={coop.data} /> : null}
              {tab === "households" ? <Households id={id} /> : null}
              {tab === "plots" ? <Plots id={id} /> : null}
            </Tabs>
          </div>
        </>
      ) : null}
    </>
  );
}
