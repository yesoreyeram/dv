import { describe, expect, it, vi } from "vitest";
import { t } from "@/i18n/messages";
import { ApiError, fetchJSON, listEndpoints } from "./api";
import { createQueryClient } from "./queryClient";
import { applyTheme, getInitialTheme, persistTheme, THEME_STORAGE_KEY } from "./theme";
import { cn } from "./utils";

describe("cn", () => {
  it("merges conflicting tailwind classes", () => {
    expect(cn("px-2", false, "px-4", ["text-sm"])).toBe("px-4 text-sm");
  });
});

describe("t", () => {
  it("interpolates variables", () => {
    expect(t("planned.badge", { milestone: "M9" })).toBe("Planned for milestone M9");
    expect(t("nav.home")).toBe("Home");
  });
});

describe("fetchJSON", () => {
  it("returns parsed JSON for 2xx and accepted statuses", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(new Response('{"a":1}', { status: 503 }))),
    );
    await expect(fetchJSON("/x", { acceptStatuses: [503] })).resolves.toEqual({ a: 1 });
  });

  it("throws ApiError otherwise", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(new Response("{}", { status: 500 }))),
    );
    const err = await fetchJSON("/x").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(500);
  });

  it("passes the abort signal and same-origin credentials", async () => {
    const fn = vi.fn(() => Promise.resolve(new Response("{}")));
    vi.stubGlobal("fetch", fn);
    const ac = new AbortController();
    await fetchJSON("/x", { signal: ac.signal });
    expect(fn).toHaveBeenCalledWith(
      "/x",
      expect.objectContaining({ signal: ac.signal, credentials: "same-origin" }),
    );
  });
});

describe("listEndpoints", () => {
  it("handles operations without summaries", () => {
    expect(
      listEndpoints({
        openapi: "3.1.0",
        info: { title: "", version: "" },
        paths: { "/a": { delete: {} } },
      }),
    ).toEqual([{ method: "DELETE", path: "/a", summary: "" }]);
  });
});

describe("theme", () => {
  it("prefers stored theme, then OS preference", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "dark");
    expect(getInitialTheme()).toBe("dark");
    window.localStorage.setItem(THEME_STORAGE_KEY, "bogus");
    vi.spyOn(window, "matchMedia").mockReturnValue({ matches: true } as MediaQueryList);
    expect(getInitialTheme()).toBe("dark");
  });

  it("survives unavailable storage", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied");
    });
    expect(getInitialTheme()).toBe("light");
    expect(() => {
      persistTheme("dark");
    }).not.toThrow();
  });

  it("applies the theme to the root element", () => {
    applyTheme("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
  });
});

describe("queryClient", () => {
  it("caps retries with backoff", () => {
    const qc = createQueryClient();
    const opts = qc.getDefaultOptions().queries;
    expect(opts?.retry).toBe(2);
    const delay = opts?.retryDelay as (n: number) => number;
    expect(delay(1)).toBe(2000);
    expect(delay(10)).toBe(10_000);
  });
});
