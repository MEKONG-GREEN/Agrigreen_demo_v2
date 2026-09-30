"use client";

import { useState } from "react";
import { unwrap, unwrapAs } from "@/api/client";
import type { ValveExport } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { FacilitySelect, RefPicker, Section, SelectField, useFacilities } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Empty, Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

function ExportTable({ items }: { items: ValveExport[] }) {
  if (!items.length) return <Empty>Chưa có bản ghi nào đã xuất qua van.</Empty>;
  return (
    <div className="tbl-wrap">
      <table className="tbl" aria-label="Bản ghi van">
        <thead>
          <tr>
            <th>Hợp đồng van</th>
            <th>Sự kiện</th>
            <th className="num">Thứ tự</th>
            <th>Thời điểm</th>
            <th>Nội dung (đúng trường hợp đồng)</th>
          </tr>
        </thead>
        <tbody>
          {items.map((x) => (
            <tr key={x.id}>
              <td className="mono">
                {x.contract} v{x.contract_version}
              </td>
              <td className="mono">{x.event_type}</td>
              <td className="num">{x.seq ?? (x as { revision?: number }).revision}</td>
              <td>{formatDateTime(x.recorded_at)}</td>
              <td>
                <pre className="mono" style={{ whiteSpace: "pre-wrap", margin: 0 }}>
                  {JSON.stringify(x.payload, null, 1)}
                </pre>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function HubExports() {
  const { api } = useAuth();
  const facilities = useFacilities();
  const [fid, setFid] = useState("");
  const [contract, setContract] = useState<"hub_facility" | "hub_capacity">("hub_capacity");
  const facility = fid || facilities.data?.[0]?.id || "";
  const d = useAsync(async () => (facility ? (await unwrap(api.GET("/v1/hub/facilities/{id}/publications", { params: { path: { id: facility }, query: { contract } } }))).items ?? [] : []), [api, facility, contract]);
  return (
    <div className="stack">
      <div className="grid2">
        <FacilitySelect value={facility} onChange={setFid} facilities={facilities.data ?? []} />
        <SelectField label="Hợp đồng van" value={contract} onChange={(v) => setContract(v as typeof contract)} options={[{ value: "hub_capacity", label: "hub_capacity" }, { value: "hub_facility", label: "hub_facility" }]} />
      </div>
      {d.loading ? <Loading /> : d.error ? <ProblemAlert problem={d.error} /> : <ExportTable items={d.data ?? []} />}
    </div>
  );
}

function OrderExports() {
  const { api } = useAuth();
  const orders = useAsync(async () => (await unwrap(api.GET("/v1/fieldops/orders", { params: { query: { limit: 200 } } }))).items ?? [], [api]);
  const [oid, setOid] = useState("");
  const order = oid || orders.data?.find((o) => o.published)?.order_id || "";
  const d = useAsync(async () => (order ? (await unwrap(api.GET("/v1/fieldops/orders/{id}/publications", { params: { path: { id: order } } }))).items ?? [] : []), [api, order]);
  return (
    <div className="stack">
      <SelectField
        label="Lệnh gom"
        value={order}
        onChange={setOid}
        options={[{ value: "", label: "— Chọn lệnh —" }, ...(orders.data ?? []).map((o) => ({ value: o.order_id ?? "", label: `${o.window_from} → ${o.window_to} · ${o.status}${o.published ? " · đã công bố" : ""}` }))]}
      />
      {d.loading ? <Loading /> : d.error ? <ProblemAlert problem={d.error} /> : <ExportTable items={d.data ?? []} />}
    </div>
  );
}

function LotExports() {
  const { api } = useAuth();
  const [lot, setLot] = useState("");
  const d = useAsync(async () => (/^[0-9a-f-]{36}$/i.test(lot) ? (await unwrapAs<{ items: ValveExport[] }>(api.GET("/v1/measurement/lots/{id}/publications", { params: { path: { id: lot } } }))).items : []), [api, lot]);
  return (
    <div className="stack">
      <RefPicker label="Mã lô" kind="lot" value={lot} onChange={setLot} />
      {d.loading ? <Loading /> : d.error ? <ProblemAlert problem={d.error} /> : <ExportTable items={d.data ?? []} />}
    </div>
  );
}

export default function PublicationsPage() {
  const [tab, setTab] = useState("hub");
  return (
    <div className="stack">
      <PageHead
        title="Van công bố"
        subtitle="Nhật ký kiểm toán các bản ghi đã xuất từ vùng MKG sang nền tảng qua van (mặc định từ chối, sổ valve_export). Số cân thô, phiếu cân, tồn theo lô, kg/cuộn KHÔNG BAO GIỜ qua van."
      />
      <Section title="Bản ghi đã xuất">
        <Tabs tabs={[{ id: "hub", label: "Hub (hub_facility, hub_capacity)" }, { id: "order", label: "Lệnh gom (collection_schedule, htx_collection_statement)" }, { id: "lot", label: "MRV theo lô" }]} value={tab} onChange={setTab} label="Nhóm van">
          {tab === "hub" ? <HubExports /> : tab === "order" ? <OrderExports /> : <LotExports />}
        </Tabs>
      </Section>
    </div>
  );
}
