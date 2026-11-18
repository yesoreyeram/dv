import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { ErrorBoundary } from "./ErrorBoundary";

const SECRET = "boom: token=abc123 at /srv/internal/path.ts:42";
let broken = true;

function Page() {
  if (broken) throw new Error(SECRET);
  return <p>page content</p>;
}

/** A frame with two "routes", keyed like the shell keys the boundary by pathname. */
function Frame() {
  const [route, setRoute] = useState("a");
  return (
    <>
      <button
        type="button"
        onClick={() => {
          setRoute("b");
        }}
      >
        other page
      </button>
      <ErrorBoundary key={route}>{route === "a" ? <Page /> : <p>second page</p>}</ErrorBoundary>
    </>
  );
}

describe("page error boundary (FR-005 AC13, NFR-AVAIL-004)", () => {
  it("contains a render error, shows fixed copy and recovers with Try again", async () => {
    broken = true;
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const user = userEvent.setup();
    render(<Frame />);

    // AC1: the frame around the page still renders.
    expect(screen.getByRole("button", { name: "other page" })).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { level: 1, name: "This page hit a problem" }),
    ).toBeVisible();
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("The rest of dv is still working.");
    // AC2: nothing from the exception reaches the page.
    expect(document.body.textContent).not.toContain("boom");
    expect(document.body.textContent).not.toContain("abc123");
    expect(document.body.textContent).not.toContain("path.ts");
    // AC3: the error is reported to the console for diagnosis.
    expect(consoleError).toHaveBeenCalled();

    // Still broken: Try again shows the fallback again instead of crashing.
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(screen.getByRole("alert")).toBeInTheDocument();

    broken = false;
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(screen.getByText("page content")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("clears the fallback when the route changes (AC4)", async () => {
    broken = true;
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const user = userEvent.setup();
    render(<Frame />);
    expect(screen.getByRole("alert")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "other page" }));
    expect(screen.getByText("second page")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("offers a full reload", async () => {
    broken = true;
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const reload = vi.fn();
    vi.stubGlobal("location", { reload });
    const user = userEvent.setup();
    render(<Frame />);
    await user.click(screen.getByRole("button", { name: "Reload" }));
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("renders children untouched when nothing fails", () => {
    broken = false;
    render(
      <ErrorBoundary>
        <Page />
      </ErrorBoundary>,
    );
    expect(screen.getByText("page content")).toBeInTheDocument();
  });
});
