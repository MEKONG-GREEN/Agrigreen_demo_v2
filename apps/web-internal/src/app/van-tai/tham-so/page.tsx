"use client";

import { idem, unwrap } from "@/api/client";
import type { LogisticsParamKey } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ParamEditor, type ParamView } from "@/components/g4kit";
import { Section } from "@/components/kit";
import { PageHead } from "@/components/ui";
import { LOGISTICS_PARAM_LABEL } from "@/lib/g4labels";

const KEYS = Object.keys(LOGISTICS_PARAM_LABEL) as LogisticsParamKey[];
const UNIT: Record<string, string> = {
  "logistics.rate.road_vnd_per_t_km": "đ/t·km",
  "logistics.rate.waterway_vnd_per_t_km": "đ/t·km",
  "logistics.mode.road_feasible_km": "km",
  "logistics.vessel.straw_payload_tons": "t",
  "logistics.vessel.expiry_warn_days": "ngày",
  "logistics.trip.unweighed_alert_hours": "giờ",
};

export default function LogisticsParamsPage() {
  const { api, principal } = useAuth();
  const load = async (key: string): Promise<ParamView> => {
    const r = await unwrap(api.GET("/v1/logistics/params/{key}", { params: { path: { key: key as LogisticsParamKey } } }));
    return {
      key,
      value: r.effective?.value ?? r.default ?? "",
      version: r.effective?.param_version ?? "",
      provisional: !!r.effective?.provisional,
      default: r.default ?? "",
      versions: (r.versions ?? []).map((v) => ({ id: v.version_id!, seq: v.version_seq!, value: v.value!, effective_from: v.effective_from ?? null, status: v.status!, provisional: !!v.provisional, proposed_by: v.proposed_by ?? null, reason: v.proposal_reason ?? null, decided_by: v.decided_by ?? null, decided_at: v.decided_at ?? null, decision_reason: v.decision_reason ?? null })),
    };
  };
  return (
    <div className="stack">
      <PageHead title="Tham số cước và vận tải" subtitle="Tham số có phiên bản, áp theo thời điểm sự việc. Điều phối đề xuất — kế toán trưởng KHÁC người đề xuất duyệt (R-56). Mặc định #0 tạm theo prototype, chờ khách (Q-7)." />
      <Section title="Tham số">
        <ParamEditor
          keys={KEYS}
          labels={LOGISTICS_PARAM_LABEL}
          load={load}
          unitHint={(k) => UNIT[k]}
          canPropose={hasRole(principal, ROLES.dispatcher)}
          canDecide={hasRole(principal, ROLES.chiefAccountant)}
          approverLabel="kế toán trưởng"
          propose={(key, body) => unwrap(api.POST("/v1/logistics/params/{key}/versions", { params: { path: { key: key as LogisticsParamKey }, header: idem() }, body }))}
          decide={(key, version, decision, reason) => unwrap(api.POST("/v1/logistics/params/{key}/versions/{version}/decision", { params: { path: { key: key as LogisticsParamKey, version }, header: idem() }, body: { decision, ...(reason ? { reason } : {}) } }))}
        />
      </Section>
    </div>
  );
}
