import { describe, expect, it } from "vitest";
import { csvCell, parseCsvText, toCsvText } from "./csv";

describe("CSV RFC 4180", () => {
  it("bọc nháy ô có dấu phẩy / nháy kép / xuống dòng, nhân đôi nháy", () => {
    expect(csvCell("1,5")).toBe('"1,5"');
    expect(csvCell('a"b')).toBe('"a""b"');
    expect(csvCell("dòng 1\ndòng 2")).toBe('"dòng 1\ndòng 2"');
    expect(csvCell("Phân urê")).toBe("Phân urê");
    expect(csvCell(null)).toBe("");
  });

  it("ghi rồi đọc lại giữ nguyên ô", () => {
    const rows = [
      ["Vật tư", "Số lượng", "Ghi chú"],
      ["Phân urê", "1,5", 'Đợt "1", sáng\r\nchiều'],
    ];
    expect(parseCsvText("﻿" + toCsvText(rows))).toEqual(rows);
  });

  it("bỏ dòng trống, nhận LF lẫn CRLF", () => {
    expect(parseCsvText("a,b\n\nc,\"d,e\"\r\n")).toEqual([
      ["a", "b"],
      ["c", "d,e"],
    ]);
  });
});
