import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
export type Dashboard = components["schemas"]["KHTCDashboard"];
export interface DashboardQuery {
    year: number;
    quarter: number;
    department?: string;
    field?: string;
}
export function createDashboardApi(api: ApiClient) { return { get: (query: DashboardQuery): Promise<Dashboard> => unwrap(api.GET("/v1/extension/khtc/dashboard", { params: { query } })) }; }
