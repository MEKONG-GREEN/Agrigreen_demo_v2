"use client";

import { useState, type ReactNode } from "react";
import { idem, unwrap } from "@/api/client";
import type { CarbonRevenueResult } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { KV, Money, When } from "@/components/g4kit";
import { ActionStatus, Provisional, Ref, Section, SelectField, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { COSTING_PARAM_LABEL } from "@/lib/g4labels";
import { isPositiveMoney, splitCarbon } from "@/lib/money";
import { useAsync } from "@/lib/useAsync";
import { LIST_PAGE_SIZE, usePaged } from "@/lib/usePaged";

const KEYS = ["costing.carbon.retained_pct", "costing.carbon.split.farmers_pct", "costing.carbon.split.authority_pct", "costing.carbon.split.verifier_pct"];

export default function CarbonPage() {
  const { api } = useAuth();
  const params = useAsync(async () => (await unwrap(api.GET("/v1/costing/params"))).items ?? [], [api]);
  const act = useAction();
  const [amount, setAmount] = useState("");
  const [reference, setReference] = useState("");
  const [result, setResult] = useState<CarbonRevenueResult | null>(null);
  const [period, setPeriod] = useState("");
  const periods = useAsync(async () => (await unwrap(api.GET("/v1/costing/periods", { params: { query: { limit: 24 } } }))).items ?? [], [api]);
  const list = usePaged(
    (cursor) => unwrap(api.GET("/v1/costing/carbon-revenue", { params: { query: { limit: LIST_PAGE_SIZE, ...(period ? { period_ref: period } : {}), ...(cursor ? { cursor } : {}) } } })),
    [api, period],
  );
  const val = (k: string) => params.data?.find((p) => p.key === k)?.active?.value ?? "";
  const preview = isPositiveMoney(amount.trim()) && params.data ? splitCarbon(amount.trim(), val(KEYS[0]!), val(KEYS[1]!), val(KEYS[2]!)) : null;
  return (
    <div className="stack">
      <PageHead
        title="Chia doanh thu carbon"
        subtitle="Chỉ chia số tiền doanh thu carbon đã có — không tính tCO2e (pf/mrv, chờ C-1). MKG giữ lại 45 %; 55 % còn lại chia 70 % nông hộ/HTX, 15 % cơ quan, 15 % VVB (VVB nhận phần dư làm tròn). Tỷ lệ là tham số có phiên bản — tạm, chờ khách (Q-4)."
      />
      <Section title="Tỷ lệ đang áp dụng">
        {params.error ? <ProblemAlert problem={params.error} /> : null}
        <KV
          label="Tỷ lệ chia carbon"
          items={KEYS.map((k) => {
            const p = params.data?.find((x) => x.key === k);
            return [COSTING_PARAM_LABEL[k] ?? k, <span key={k} className="row"><b className="mono">{p?.active?.value ?? "…"} %</b> <span className="mono small">{p?.active?.pinned}</span> {p?.active?.seq === 0 ? <Provisional what="Q-4" /> : null}</span>] as [string, ReactNode];
          })}
        />
      </Section>
      <Section title="Ghi doanh thu carbon (kỳ đang mở)">
        <div className="stack">
          <div className="grid2">
            <TextField label="Doanh thu carbon (VND)" value={amount} onChange={setAmount} inputMode="decimal" />
            <TextField label="Tham chiếu" value={reference} onChange={setReference} hint="Số hợp đồng bán tín chỉ / chứng từ (tuỳ chọn)" />
          </div>
          {preview ? (
            <p className="small">
              Xem trước (tính bằng số thập phân): giữ lại <Money v={preview.retained} /> · nông hộ <Money v={preview.farmers} /> · cơ quan <Money v={preview.authority} /> · VVB <Money v={preview.verifier} />
            </p>
          ) : null}
          <ActionStatus a={act} />
          <div className="row">
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={act.busy || !isPositiveMoney(amount.trim())}
              onClick={() => void act.run(() => unwrap(api.POST("/v1/costing/carbon-revenue", { params: { header: idem() }, body: { amount: amount.trim(), ...(reference.trim() ? { reference: reference.trim() } : {}) } })), (r) => `Đã ghi doanh thu carbon kỳ ${r.period_code} (${r.label})`).then((r) => {
                  if (r) {
                    setResult(r);
                    list.reload();
                  }
                })
              }
            >
              Ghi và chia
            </button>
          </div>
          {result ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Phần chia doanh thu carbon">
                <thead>
                  <tr>
                    <th>Bên</th>
                    <th className="num">Số tiền</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td>Doanh thu</td>
                    <td className="num">
                      <Money v={result.revenue_amount} strong />
                    </td>
                  </tr>
                  <tr>
                    <td>MKG giữ lại</td>
                    <td className="num">
                      <Money v={result.retained_amount} />
                    </td>
                  </tr>
                  <tr>
                    <td>Nông hộ / HTX</td>
                    <td className="num">
                      <Money v={result.farmers_amount} />
                    </td>
                  </tr>
                  <tr>
                    <td>Cơ quan</td>
                    <td className="num">
                      <Money v={result.authority_amount} />
                    </td>
                  </tr>
                  <tr>
                    <td>VVB (nhận phần dư)</td>
                    <td className="num">
                      <Money v={result.verifier_amount} />
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          ) : null}
        </div>
      </Section>
      <Section title="Doanh thu carbon đã ghi">
        <div className="stack">
          <SelectField label="Lọc theo kỳ" value={period} onChange={setPeriod} options={[{ value: "", label: "Mọi kỳ" }, ...(periods.data ?? []).map((p) => ({ value: p.id ?? "", label: p.code ?? "" }))]} />
          {list.loading ? <Loading /> : null}
          {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
          {list.items && !list.items.length ? <Empty>Chưa ghi khoản doanh thu carbon nào.</Empty> : null}
          {list.items?.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Doanh thu carbon đã ghi">
                <thead>
                  <tr>
                    <th>Ghi lúc</th>
                    <th>Kỳ</th>
                    <th>Tham chiếu</th>
                    <th className="num">Doanh thu</th>
                    <th className="num">MKG giữ lại</th>
                    <th className="num">Nông hộ / HTX</th>
                    <th className="num">Cơ quan</th>
                    <th className="num">VVB</th>
                    <th>Tỷ lệ</th>
                    <th>Người ghi</th>
                  </tr>
                </thead>
                <tbody>
                  {list.items.map((c) => (
                    <tr key={c.id}>
                      <td>
                        <When at={c.event_time} />
                      </td>
                      <td className="mono">{c.period_code}</td>
                      <td>{c.reference ?? <span className="mute">—</span>}</td>
                      <td className="num">
                        <Money v={c.revenue_amount} strong />
                      </td>
                      <td className="num">
                        <Money v={c.retained_amount} />
                      </td>
                      <td className="num">
                        <Money v={c.farmers_amount} />
                      </td>
                      <td className="num">
                        <Money v={c.authority_amount} />
                      </td>
                      <td className="num">
                        <Money v={c.verifier_amount} />
                      </td>
                      <td className="small">
                        {c.retained_pct ?? "?"} % · {c.farmers_pct ?? "?"}/{c.authority_pct ?? "?"}/{c.verifier_pct ?? "?"} <span className="chip c-warn">{c.label}</span>
                      </td>
                      <td>
                        <Ref id={c.actor_ref} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
          <LoadMore hasMore={list.hasMore} loading={list.loadingMore} onClick={list.loadMore} />
        </div>
      </Section>
    </div>
  );
}
