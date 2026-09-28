import { describe, expect, it } from "vitest";
import { dialUri } from "./dial";

describe("dialUri (US-DIR-02)", () => {
  it("bỏ ký tự phân cách, giữ dấu +", () => {
    expect(dialUri("tel", "0292 3812.345")).toBe("tel:02923812345");
    expect(dialUri("sms", "+84-912-345-678")).toBe("sms:+84912345678");
  });
});
