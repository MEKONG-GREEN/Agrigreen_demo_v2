"use client";

import { idem, unwrap } from "@/api/client";
import type { OrdersParamKey } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ParamEditor, type ParamView } from "@/components/g4kit";
import { Section } from "@/components/kit";
import { PageHead } from "@/components/ui";
import { ORDERS_PARAM_LABEL } from "@/lib/g4labels";

const KEYS = Object.keys(ORDERS_PARAM_LABEL) as OrdersParamKey[];
const UNIT: Record<string, string> = {
  "orders.receipt.po_variance_warn_pct": "%",
  "orders.sales.price_approval_delta_pct": "%",
  "orders.sales.discount_approval_pct": "%",
  "orders.payment_term_days": "ngày",
};

export default function OrdersParamsPage() {
  const { api, principal } = useAuth();
  const load = async (key: string): Promise<ParamView> => {
    const r = await unwrap(api.GET("/v1/orders/params/{key}", { params: { path: { key: key as OrdersParamKey } } }));
    return {
      key,
      value: r.effective_value ?? "",
      version: r.param_version ?? "",
      provisional: !!r.provisional,
      default: r.default_value ?? "",
      note: r.note,
      versions: (r.versions ?? []).map((v) => ({ id: v.version_id!, seq: v.version_seq!, value: v.value!, effective_from: v.effective_from ?? null, status: v.status!, provisional: !!v.provisional, proposed_by: v.proposed_by ?? null, reason: v.proposal_reason ?? null, decided_by: v.decided_by ?? null, decided_at: v.decided_at ?? null, decision_reason: v.decision_reason ?? null })),
    };
  };
  return (
    <div className="stack">
      <PageHead title="Tham số đơn hàng" subtitle="Ngưỡng lệch phiếu nhận ↔ đơn mua, ngưỡng duyệt giá / chiết khấu, hạn thanh toán — có phiên bản, người đặt ≠ người duyệt (sales_approver khác, R-56). Mặc định tạm, chờ khách (Q-5)." />
      <Section title="Tham số">
        <ParamEditor
          keys={KEYS}
          labels={ORDERS_PARAM_LABEL}
          load={load}
          unitHint={(k) => UNIT[k]}
          canPropose={hasRole(principal, ROLES.salesOfficer, ROLES.salesApprover)}
          canDecide={hasRole(principal, ROLES.salesApprover)}
          approverLabel="sales_approver"
          propose={(key, body) => unwrap(api.POST("/v1/orders/params/{key}/versions", { params: { path: { key: key as OrdersParamKey }, header: idem() }, body: { ...body, provisional: true } }))}
          decide={(key, version, decision, reason) => unwrap(api.POST("/v1/orders/params/{key}/versions/{version}/decision", { params: { path: { key: key as OrdersParamKey, version }, header: idem() }, body: { decision, ...(reason ? { reason } : {}) } }))}
        />
      </Section>
    </div>
  );
}
