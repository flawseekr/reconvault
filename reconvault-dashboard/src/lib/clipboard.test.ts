import { afterEach, describe, expect, it, vi } from "vitest";

import { copyText } from "@/lib/clipboard";

function setSecure(secure: boolean) {
  Object.defineProperty(window, "isSecureContext", { value: secure, configurable: true });
}

describe("copyText", () => {
  afterEach(() => {
    Reflect.deleteProperty(window, "isSecureContext");
    Reflect.deleteProperty(navigator, "clipboard");
  });

  it("uses the Clipboard API in secure contexts", async () => {
    setSecure(true);
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    await expect(copyText("abc")).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith("abc");
  });

  it("falls back to execCommand over plain HTTP", async () => {
    setSecure(false);
    let copied = "";
    const exec = vi.fn(() => {
      copied = (document.activeElement as HTMLTextAreaElement | null)?.value ?? "";
      return true;
    });
    document.execCommand = exec;
    await expect(copyText("plain http value")).resolves.toBe(true);
    expect(exec).toHaveBeenCalledWith("copy");
    expect(copied).toBe("plain http value");
    expect(document.querySelector("textarea")).toBeNull(); // cleaned up
  });

  it("falls back when clipboard permission is denied, and reports failure", async () => {
    setSecure(true);
    Object.defineProperty(navigator, "clipboard", { value: { writeText: vi.fn().mockRejectedValue(new Error("denied")) }, configurable: true });
    document.execCommand = vi.fn(() => false);
    await expect(copyText("x")).resolves.toBe(false);
  });
});
