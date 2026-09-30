"use client";

import { useState, type ReactNode } from "react";
import { idem, unwrap } from "@/api/client";
import type { CarbonRevenueResult } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { KV, Money } from "@/components/g4kit";
import { ActionStatus, Provisional, Section, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { PageHead } from "@/components/ui";
import { COSTING_PARAM_LABEL } from "@/lib/g4labels";
import { isPositiveMoney, splitCarbon } from "@/lib/money";
import { useAsync } from "@/lib/useAsync";

const KEYS = ["costing.carbon.retained_pct", "costing.carbon.split.farmers_pct", "costing.carbon.split.authority_pct", "costing.carbon.split.verifier_pct"];

export default function CarbonPage() {
  const { api } = useAuth();
  const params = useAsync(async () => (await unwrap(api.GET("/v1/costing/params"))).items ?? [], [api]);
  const act = useAction();
  const [amount, setAmount] = useState("");
  const [reference, setReference] = useState("");
  const [result, setResult] = useState<CarbonRevenueResult | null>(null);
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
              onClick={() => void act.run(() => unwrap(api.POST("/v1/costing/carbon-revenue", { params: { header: idem() }, body: { amount: amount.trim(), ...(reference.trim() ? { reference: reference.trim() } : {}) } })), (r) => `Đã ghi doanh thu carbon kỳ ${r.period_code} (${r.label})`).then((r) => r && setResult(r))}
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
    </div>
  );
}
