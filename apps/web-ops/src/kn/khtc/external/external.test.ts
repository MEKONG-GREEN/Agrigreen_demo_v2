import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { ApiProblem } from "@/api/problem";
import { createKhtcPeriodApi } from "../periods/api";
import { createReportApi } from "../reports/api";
import { createExternalApi, fmtVn, parseVnNumber, sourceText } from "./api";
const client = () => createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.tinh", getContextId: () => null, fetch: mockFetchNoDelay });
const code = (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => e instanceof ApiProblem ? e.problem.code : String(e));

describe("W11.67 số liệu nguồn ngoài", () => {
    it("đổi số kiểu Việt Nam sang decimal và hiển thị lại", () => {
        expect(parseVnNumber("48.215,5")).toBe("48215.5");
        expect(parseVnNumber("26 400")).toBe("26400");
        expect(parseVnNumber("26.400")).toBe("26400");
        expect(parseVnNumber("12.45")).toBe("12.45");
        expect(parseVnNumber("abc")).toBeNull();
        expect(parseVnNumber("1,23456")).toBeNull();
        expect(fmtVn("48215.5")).toBe("48.215,5");
        expect(fmtVn("-1234")).toBe("-1.234");
        expect(sourceText({ agency: "Cục Thống kê", document: "285/BC-CTK", source_date: "2026-09-28" })).toBe("Cục Thống kê · 285/BC-CTK, 28/09/2026");
    });

    it("nạp hai bước ghi 3 dòng, báo lỗi 1; sửa cần lý do; ô Nhập ngoài vào bản soạn Mẫu 1", async () => {
        const api = client(), periods = createKhtcPeriodApi(api), ext = createExternalApi(api), reports = createReportApi(api);
        const year = new Date().getFullYear() + 7;
        const p = await periods.open({ report_type: "week", year, no: 39, departments: ["trong_trot"] });
        const csv = "Chỉ tiêu,Kỳ,Giá trị,ĐVT,Cơ quan cung cấp,Văn bản / kênh,Ngày\n" +
            `Diện tích lúa Thu Đông,${year}-W39,12450,ha,Cục Thống kê,File tổng hợp,${year}-09-29\n` +
            `Số hộ nuôi tôm càng xanh,${year}-W39,812,hộ,Chi cục Thủy sản,Email,29/09/${year}\n` +
            `Số HTX tham gia chuỗi lúa CLC,${year}-W39,34,HTX,Sở NN&MT,CV 512,${year}-09-29\n` +
            `Giá trị xuất khẩu gạo,${year}-W39,210,triệu USD,,,\n`;
        const staged = await ext.stage(year, { kind: "csv", name: "tuan39.csv", size: csv.length, body: csv, contentType: "text/csv; charset=utf-8" });
        expect([staged.result.valid, staged.result.errors]).toEqual([3, 1]);
        expect(staged.result.rows[3]?.errors.join(" ")).toContain("thiếu nguồn");
        expect(await ext.figures(year, { period_id: p.id })).toHaveLength(0);
        const done = await ext.commit(staged.import_id, false, "");
        expect(done.recorded).toBe(3);
        expect(await code(ext.commit(staged.import_id, false, ""))).toBe("extension.khtc_ext_import_committed");
        const figs = await ext.figures(year, { period_id: p.id });
        const rice = figs.find(f => f.name === "Diện tích lúa Thu Đông")!;
        expect(rice.value).toBe("12450");
        expect(await code(ext.save({ period_id: p.id, indicator_id: rice.indicator_id, value: "12500", agency: "Cục Thống kê", document: "bổ sung", source_date: `${year}-09-30` }))).toBe("extension.khtc_ext_duplicate");
        expect(await code(ext.save({ period_id: p.id, indicator_id: rice.indicator_id, value: "12500", agency: "Cục Thống kê", document: "bổ sung", source_date: `${year}-09-30`, base_version: rice.version, reason: "ngắn" }))).toBe("extension.khtc_ext_invalid");
        const fixed = await ext.save({ period_id: p.id, indicator_id: rice.indicator_id, value: "12500", agency: "Cục Thống kê", document: "bổ sung", source_date: `${year}-09-30`, base_version: rice.version, reason: "Cục Thống kê điều chỉnh số" });
        expect(fixed.version).toBe(rice.version + 1);
        const hist = await ext.history(year, p.id);
        expect(hist[0]).toMatchObject({ old_value: "12450", new_value: "12500", reason: "Cục Thống kê điều chỉnh số" });
        const d = await reports.get(p.id);
        expect(d.content.data.external?.map(x => [x.name, x.value])).toContainEqual(["Diện tích lúa Thu Đông", "12500"]);
        expect(d.content.data.external?.find(x => x.name === "Diện tích lúa Thu Đông")?.agency).toBe("Cục Thống kê");
    });
});
