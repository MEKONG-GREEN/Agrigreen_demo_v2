import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { PROBLEM_MESSAGES } from "@/api/problem";
import { canSalesAdvance, canTripAdvance, flagLabel, nextMilestone, normalizeVesselCode, TRIP_FLAG_LABEL } from "./g4labels";
import {
  addMoney,
  amountForCount,
  amountForKg,
  applyMoneyPct,
  cents,
  cmpMoney,
  formatDecimalString,
  formatPrice,
  formatVnd,
  isMoney,
  isPrice,
  journalTotals,
  moneyOf,
  netPrice,
  priceDeltaPct,
  shortHash,
  splitCarbon,
} from "./money";
import { base64ToBytes, sha256Hex, sha256Text, textToBase64 } from "./sha256";

describe("tiền VND dạng chuỗi — không qua float (R-10)", () => {
  it("định dạng vi-VN: nhóm nghìn bằng dấu chấm, phần lẻ bằng dấu phẩy, bỏ ,00", () => {
    expect(formatVnd("24813134.93")).toBe("24.813.134,93 ₫");
    expect(formatVnd("1250000.00")).toBe("1.250.000 ₫");
    expect(formatVnd("-300000.50")).toBe("-300.000,5 ₫");
    expect(formatVnd("0.00")).toBe("0 ₫");
    expect(formatVnd(null)).toBe("—");
    expect(formatVnd("abc")).toBe("—");
  });
  it("số rất lớn vẫn chính xác (vượt 2^53)", () => {
    expect(formatVnd("12345678901234567.89")).toBe("12.345.678.901.234.567,89 ₫");
    expect(addMoney("9007199254740993.01", "0.01")).toBe("9007199254740993.02");
  });
  it("đơn giá 4 số lẻ", () => {
    expect(formatPrice("650000.0000", "tấn")).toBe("650.000 ₫/tấn");
    expect(formatPrice("1500.5000", "cuộn")).toBe("1.500,5 ₫/cuộn");
    expect(isPrice("0")).toBe(false);
    expect(isPrice("650000.12345")).toBe(false);
    expect(isPrice("650000.1234")).toBe(true);
  });
  it("cộng, so sánh, 0,1 + 0,2 = 0,3 chính xác", () => {
    expect(addMoney("0.1", "0.2")).toBe("0.30");
    expect(cmpMoney("100.00", "99.99")).toBe(1);
    expect(isMoney("1.234")).toBe(false);
    expect(moneyOf(cents("-12.5"))).toBe("-12.50");
  });
  it("thành tiền theo kg và theo cuộn, làm tròn nửa lên (orders.LineAmount)", () => {
    expect(amountForKg("96800.000", "650000.0000")).toBe("62920000.00");
    expect(amountForKg("1.000", "5.0000")).toBe("0.01"); // 1 kg × 5 đ/t = 0,005 đ → 0,01
    expect(amountForCount(4400, "15000.0000")).toBe("66000000.00");
  });
  it("giá sau chiết khấu và lệch giá bảng", () => {
    expect(netPrice("1500000.0000", "5")).toBe("1425000.0000");
    expect(netPrice("1000.0000", "33.33")).toBe("666.7000");
    expect(priceDeltaPct("1140000.0000", "1200000.0000")).toBe("5.00");
    expect(applyMoneyPct("1000.00", "45")).toBe("450.00");
  });
  it("chia carbon 45 % / 70-15-15, VVB nhận phần dư làm tròn (proto CARBON-SHARE)", () => {
    const s = splitCarbon("100000000.00", "45", "70", "15");
    expect(s).toEqual({ retained: "45000000.00", farmers: "38500000.00", authority: "8250000.00", verifier: "8250000.00" });
    const odd = splitCarbon("100.01", "45", "70", "15");
    expect(addMoney(odd.retained, odd.farmers, odd.authority, odd.verifier)).toBe("100.01");
  });
  it("bút toán cân: Σ Nợ = Σ Có > 0", () => {
    expect(journalTotals([{ side: "debit", amount: "10.00" }, { side: "credit", amount: "10.00" }]).balanced).toBe(true);
    expect(journalTotals([{ side: "debit", amount: "10.00" }, { side: "credit", amount: "9.99" }]).balanced).toBe(false);
    expect(journalTotals([]).balanced).toBe(false);
  });
  it("số thập phân khác (km) và băm rút gọn", () => {
    expect(formatDecimalString("64.250")).toBe("64,25");
    expect(formatDecimalString("1200.000")).toBe("1.200");
    expect(shortHash("a".repeat(64))).toBe("aaaaaaaa…aaaaaa");
  });
});

describe("SHA-256 kiểm ảnh chụp định tuyến nguyên văn (L-03)", () => {
  it("véc-tơ chuẩn FIPS 180-4", () => {
    expect(sha256Text("")).toBe("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
    expect(sha256Text("abc")).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    expect(sha256Text("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq")).toBe("248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1");
  });
  it("base64 ↔ byte UTF-8 giữ nguyên văn", () => {
    const raw = '{"distance_km":"64.250","ghi_chú":"kênh Xà No"}';
    expect(sha256Hex(base64ToBytes(textToBase64(raw)))).toBe(sha256Text(raw));
  });
});

describe("trạng thái chỉ tiến và quy tắc hiển thị G4", () => {
  it("chuyến L-02: mốc từng bậc, hoàn tất khi đã xuất phát, huỷ trước khi đến", () => {
    expect(canTripAdvance("planned", "departed")).toBe(true);
    expect(canTripAdvance("planned", "arrived")).toBe(false);
    expect(canTripAdvance("departed", "completed")).toBe(true);
    expect(canTripAdvance("planned", "completed")).toBe(false);
    expect(canTripAdvance("arrived", "cancelled")).toBe(false);
    expect(canTripAdvance("completed", "cancelled")).toBe(false);
    expect(nextMilestone("planned")).toBe("departed");
    expect(nextMilestone("arrived")).toBeNull();
  });
  it("đơn bán chỉ tiến, huỷ chỉ khi nháp / đã xác nhận", () => {
    expect(canSalesAdvance("confirmed", "delivered")).toBe(true);
    expect(canSalesAdvance("delivered", "delivering")).toBe(false);
    expect(canSalesAdvance("delivering", "cancelled")).toBe(false);
    expect(canSalesAdvance("draft", "cancelled")).toBe(true);
  });
  it("số hiệu ghe chuẩn hoá (GH-01) và nhãn cờ", () => {
    expect(normalizeVesselCode(" tg - 12 345 ")).toBe("TG-12345");
    expect(flagLabel("payload_exceeded")).toMatch(/1,1/);
    expect(flagLabel("cờ_lạ")).toBe("cờ_lạ");
  });
});

describe("nhãn mã lỗi: mọi mã logistics / orders / costing / acctbridge trong docs/errors.md", () => {
  const md = readFileSync(resolve(process.cwd(), "../../docs/errors.md"), "utf8");
  const codes = Array.from(new Set(Array.from(md.matchAll(/^\| \d{3} \| `((?:logistics|orders|costing|acctbridge)\.[a-z_]+)`/gm), (m) => m[1]!)));
  it("docs/errors.md có mã G4", () => {
    expect(codes.length).toBeGreaterThan(50);
  });
  it.each(codes)("%s có thông báo tiếng Việt", (code) => {
    expect(PROBLEM_MESSAGES[code]).toBeTruthy();
  });
  it("mọi cờ cảnh báo logistics trong docs/errors.md có nhãn", () => {
    const sec = md.slice(md.indexOf("### mkg/logistics"), md.indexOf("### mkg/orders"));
    const line = sec.split("\n").find((l) => l.startsWith("Định tuyến **không bao giờ**")) ?? "";
    const flags = Array.from(line.matchAll(/`([a-z_]+)`/g), (m) => m[1]!).filter((f) => !f.includes("haversine") && f !== "waterway_routes" && f !== "warnings" && f !== "flags");
    expect(flags.length).toBeGreaterThan(8);
    for (const f of flags) expect(TRIP_FLAG_LABEL[f], f).toBeTruthy();
  });
});
