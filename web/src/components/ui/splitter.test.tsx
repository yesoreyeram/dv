import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { Splitter } from "./splitter";

function Harness({ onCommit }: { onCommit: (v: number) => void }) {
  const [value, setValue] = useState(360);
  return (
    <>
      <div id="pane" />
      <Splitter
        value={value}
        min={280}
        max={640}
        step={16}
        defaultValue={360}
        controls="pane"
        label="Resize"
        onChange={setValue}
        onCommit={onCommit}
      />
    </>
  );
}

const setup = () => {
  const onCommit = vi.fn();
  render(<Harness onCommit={onCommit} />);
  const sep = screen.getByRole("separator", { name: "Resize" });
  const now = () => Number(sep.getAttribute("aria-valuenow"));
  return { sep, now, onCommit };
};

describe("Splitter (FR-018 AC8, NFR-A11Y-006 AC4)", () => {
  it("exposes the window-splitter semantics", () => {
    const { sep } = setup();
    expect(sep).toHaveAttribute("aria-orientation", "vertical");
    expect(sep).toHaveAttribute("aria-controls", "pane");
    expect(sep).toHaveAttribute("aria-valuemin", "280");
    expect(sep).toHaveAttribute("aria-valuemax", "640");
    expect(sep).toHaveAttribute("aria-valuenow", "360");
    expect(sep).toHaveAttribute("tabindex", "0");
  });

  it("resizes with the arrow keys, Home, End and Enter, and commits each step", async () => {
    const user = userEvent.setup();
    const { sep, now, onCommit } = setup();
    sep.focus();
    await user.keyboard("{ArrowRight}");
    expect(now()).toBe(376);
    expect(onCommit).toHaveBeenLastCalledWith(376);
    await user.keyboard("{ArrowLeft}{ArrowLeft}");
    expect(now()).toBe(344);
    await user.keyboard("{End}");
    expect(now()).toBe(640);
    await user.keyboard("{ArrowRight}");
    expect(now()).toBe(640);
    await user.keyboard("{Home}");
    expect(now()).toBe(280);
    await user.keyboard("{ArrowLeft}");
    expect(now()).toBe(280);
    await user.keyboard("{Enter}");
    expect(now()).toBe(360);
    expect(onCommit).toHaveBeenLastCalledWith(360);
    const calls = onCommit.mock.calls.length;
    await user.keyboard("a{ArrowUp}{Tab}");
    expect(now()).toBe(360);
    expect(onCommit).toHaveBeenCalledTimes(calls);
  });

  it("mirrors the arrow keys and the drag direction in right-to-left layouts", async () => {
    const user = userEvent.setup();
    const { sep, now } = setup();
    sep.style.direction = "rtl";
    sep.focus();
    await user.keyboard("{ArrowLeft}");
    expect(now()).toBe(376);
    await user.keyboard("{ArrowRight}{ArrowRight}");
    expect(now()).toBe(344);
    fireEvent.pointerDown(sep, { button: 0, clientX: 500, pointerId: 1 });
    fireEvent.pointerMove(sep, { clientX: 460, pointerId: 1 });
    expect(now()).toBe(384);
    fireEvent.pointerUp(sep, { pointerId: 1 });
  });

  it("follows a drag, clamps it and commits once on release", () => {
    const { sep, now, onCommit } = setup();
    fireEvent.pointerMove(sep, { clientX: 900, pointerId: 1 });
    expect(now()).toBe(360);

    fireEvent.pointerDown(sep, { button: 0, clientX: 400, pointerId: 1 });
    expect(sep).toHaveFocus();
    fireEvent.pointerMove(sep, { clientX: 450.4, pointerId: 1 });
    expect(now()).toBe(410);
    fireEvent.pointerMove(sep, { clientX: 2000, pointerId: 1 });
    expect(now()).toBe(640);
    fireEvent.pointerMove(sep, { clientX: -2000, pointerId: 1 });
    expect(now()).toBe(280);
    fireEvent.pointerMove(sep, { clientX: 420, pointerId: 1 });
    expect(onCommit).not.toHaveBeenCalled();
    fireEvent.pointerUp(sep, { pointerId: 1 });
    expect(onCommit).toHaveBeenCalledTimes(1);
    expect(onCommit).toHaveBeenCalledWith(380);

    // After release, moving does nothing; a cancelled drag also commits where it stopped.
    fireEvent.pointerMove(sep, { clientX: 600, pointerId: 1 });
    fireEvent.pointerUp(sep, { pointerId: 1 });
    expect(now()).toBe(380);
    expect(onCommit).toHaveBeenCalledTimes(1);
    fireEvent.pointerDown(sep, { button: 0, clientX: 0, pointerId: 2 });
    fireEvent.pointerMove(sep, { clientX: 20, pointerId: 2 });
    fireEvent.pointerCancel(sep, { pointerId: 2 });
    expect(onCommit).toHaveBeenLastCalledWith(400);
  });

  it("ignores other mouse buttons and captures the pointer when it can", () => {
    const { sep, now } = setup();
    fireEvent.pointerDown(sep, { button: 2, clientX: 400, pointerId: 1 });
    fireEvent.pointerMove(sep, { clientX: 500, pointerId: 1 });
    expect(now()).toBe(360);
    const capture = vi.fn();
    sep.setPointerCapture = capture;
    fireEvent.pointerDown(sep, { button: 0, clientX: 400, pointerId: 7 });
    expect(capture).toHaveBeenCalledWith(7);
    fireEvent.pointerUp(sep, { pointerId: 7 });
  });

  it("restores the default on double click", async () => {
    const user = userEvent.setup();
    const { sep, now, onCommit } = setup();
    sep.focus();
    await user.keyboard("{End}");
    fireEvent.doubleClick(sep);
    expect(now()).toBe(360);
    expect(onCommit).toHaveBeenLastCalledWith(360);
  });
});
