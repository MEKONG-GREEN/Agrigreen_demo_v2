import type { Reconciliation } from "@/api/types";
import { formatDateTime } from "@/lib/format";
import { formatKg, formatPct } from "@/lib/kg";
import { Chip, Ref } from "./kit";

/** Một lần đối soát: nội bộ (hao hụt dự kiến vs bất thường), thương mại (dung sai), cuộn ↔ khối lượng (RB-6). */
export function ReconView({ r }: { r: Reconciliation }) {
  const i = r.internal;
  const c = r.commercial;
  const k = r.roll_check;
  return (
    <div className="stack" aria-label="Kết quả đối soát">
      <div className="row">
        <b>Kết quả:</b> <Chip status={r.outcome} />
        <span className="small mute">
          {r.route === "DIRECT" ? "Giao thẳng" : "Qua hub"} · {r.pricing_basis === "per_bale" ? "Giá theo cuộn" : "Giá theo tấn"} · dung sai <span className="mono">{r.tolerance_version}</span>
          {r.loss_norm_version ? (
            <>
              {" "}
              · định mức <span className="mono">{r.loss_norm_version}</span>
            </>
          ) : null}{" "}
          · {formatDateTime(r.event_time)}
        </span>
        {r.dispute_id ? (
          <span className="small">
            Tranh chấp <Ref id={r.dispute_id} />
          </span>
        ) : null}
      </div>
      <div className="grid3">
        <div className="panel" aria-label="Hao hụt nội bộ">
          <b>Nội bộ HUB_IN → HUB_OUT</b> <Chip status={i?.status} />
          <dl className="kv small" style={{ marginTop: 8 }}>
            <dt>HUB_IN / HUB_OUT</dt>
            <dd>
              {formatKg(r.hub_in_kg, 3)} / {formatKg(r.hub_out_kg, 3)}
            </dd>
            <dt>Hao hụt thực</dt>
            <dd>{formatKg(i?.loss_kg, 3)}</dd>
            <dt>Định mức sấy</dt>
            <dd>
              {formatPct(i?.loss_norm_pct)} (ẩm {r.hub_in_moisture_pct ?? "—"} %)
            </dd>
            <dt>Dự kiến</dt>
            <dd>{formatKg(i?.expected_loss_kg, 3)}</dd>
            <dt>Bất thường</dt>
            <dd>{formatKg(i?.abnormal_loss_kg, 3)}</dd>
          </dl>
        </div>
        <div className="panel" aria-label="Thương mại">
          <b>Thương mại HUB_OUT → CUSTOMER_IN</b> <Chip status={c?.status} />
          <dl className="kv small" style={{ marginTop: 8 }}>
            <dt>CUSTOMER_IN</dt>
            <dd>{formatKg(r.customer_in_kg, 3)}</dd>
            <dt>Chênh</dt>
            <dd>
              {formatKg(c?.diff_kg, 3)} ({formatPct(c?.diff_pct, true)})
            </dd>
          </dl>
        </div>
        <div className="panel" aria-label="Cuộn và khối lượng">
          <b>Cuộn ↔ khối lượng (RB-6)</b> <Chip status={k?.status} />
          <dl className="kv small" style={{ marginTop: 8 }}>
            <dt>Số cuộn</dt>
            <dd>{k?.rolls?.toLocaleString("vi-VN") ?? "—"}</dd>
            <dt>kg/cuộn ước</dt>
            <dd>{formatKg(k?.kg_per_roll_estimate, 1)}</dd>
            <dt>Ước lượng</dt>
            <dd>{formatKg(k?.estimate_kg)}</dd>
            <dt>Chênh so ước</dt>
            <dd>
              <b>{formatPct(k?.variance_pct, true)}</b>
            </dd>
            <dt>Sản lượng quy đổi</dt>
            <dd>
              {formatKg(r.yield_kg_per_roll, 2)}/cuộn{r.yield_aggregated ? " (gộp)" : ""}
            </dd>
          </dl>
        </div>
      </div>
    </div>
  );
}
