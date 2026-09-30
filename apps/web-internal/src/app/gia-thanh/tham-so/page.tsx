"use client";

import { idem, unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ParamEditor, type ParamView } from "@/components/g4kit";
import { Section } from "@/components/kit";
import { PageHead } from "@/components/ui";
import { COSTING_PARAM_LABEL } from "@/lib/g4labels";

const KEYS = Object.keys(COSTING_PARAM_LABEL);
const UNIT: Record<string, string> = {
  "costing.allocation.key": "mass (chờ khách Q-2)",
  "costing.moisture.base_pct": "none (chờ công thức C-4)",
  "costing.rate.hub_processing_vnd_per_t": "đ/t",
  "costing.settlement.payment_term_days": "ngày",
};

export default function CostingParamsPage() {
  const { adminApi, principal } = useAuth();
  // Bề mặt quản trị (MFA): đọc, đề xuất và duyệt đều qua adminApi.
  const load = async (key: string): Promise<ParamView> => {
    const r = await unwrap(adminApi.GET("/v1/costing/params"));
    const p = (r.items ?? []).find((x) => x.key === key);
    return {
      key,
      value: p?.active?.value ?? p?.default ?? "",
      version: p?.active?.pinned ?? "",
      provisional: (p?.active?.seq ?? 0) === 0,
      default: p?.default ?? "",
      note: `${p?.default_source ?? ""} — ${p?.default_label ?? ""}`,
      versions: (p?.versions ?? []).map((v) => ({ id: String(v.seq), seq: v.seq!, value: v.value!, effective_from: v.effective_from ?? null, status: v.decision ?? "pending", provisional: false, proposed_by: v.proposed_by ?? null, reason: v.reason ?? null, decided_by: v.decided_by ?? null, decided_at: v.decided_at ?? null, decision_reason: v.decision_reason ?? null })),
    };
  };
  return (
    <div className="stack">
      <PageHead title="Tham số giá thành" subtitle="Khoá phân bổ, quy đổi ẩm, hao hụt, đơn giá sơ chế, hạn quyết toán, tỷ lệ chia carbon. Kế toán giá thành đề xuất — kế toán trưởng KHÁC duyệt (R-56, CHECK DB); tính lại ghim phiên bản theo thời điểm sự việc." />
      <Section title="Tham số">
        <ParamEditor
          keys={KEYS}
          labels={COSTING_PARAM_LABEL}
          load={load}
          unitHint={(k) => UNIT[k] ?? (k.endsWith("_pct") ? "%" : undefined)}
          canPropose={hasRole(principal, ROLES.costAccountant)}
          canDecide={hasRole(principal, ROLES.chiefAccountant)}
          approverLabel="kế toán trưởng"
          propose={(key, body) => unwrap(adminApi.POST("/v1/costing/params", { params: { header: idem() }, body: { key, value: body.value, reason: body.reason, ...(body.effective_from ? { effective_from: new Date(`${body.effective_from}T00:00:00+07:00`).toISOString() } : {}) } }))}
          decide={(key, seq, decision, reason) => unwrap(adminApi.POST("/v1/costing/params/{key}/versions/{seq}/decision", { params: { path: { key, seq: Number(seq) }, header: idem() }, body: { decision, ...(reason ? { reason } : {}) } }))}
        />
      </Section>
    </div>
  );
}
