import { useEffect, useRef, type KeyboardEvent, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/utils";

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * Modal dialog (NFR-A11Y-006 AC2): focus moves inside on open, Tab stays inside, Escape and
 * a press outside close it, and focus returns to the opener. The caller makes the rest of
 * the page inert while it is mounted. Rendered in a portal so it sits outside that subtree.
 */
export function Modal({
  onClose,
  label,
  labelledBy,
  initialFocus,
  className,
  children,
  testId,
}: {
  onClose: () => void;
  /** Accessible name; give either this or `labelledBy`. */
  label?: string;
  labelledBy?: string;
  /** Element to focus on open; defaults to the first focusable element in the dialog. */
  initialFocus?: RefObject<HTMLElement | null>;
  className?: string;
  children: ReactNode;
  testId?: string;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const closeRef = useRef(onClose);
  useEffect(() => {
    closeRef.current = onClose;
  });

  useEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const panel = panelRef.current;
    const first = initialFocus?.current ?? panel?.querySelector<HTMLElement>(FOCUSABLE) ?? panel;
    first?.focus();

    const onPointerDown = (e: PointerEvent) => {
      if (e.target instanceof Node && !panel?.contains(e.target)) closeRef.current();
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      // The opener is no longer inert by the time this cleanup runs.
      if (opener?.isConnected) opener.focus();
    };
  }, [initialFocus]);

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      onClose();
      return;
    }
    if (e.key !== "Tab") return;
    const panel = panelRef.current;
    if (!panel) return;
    const items = [...panel.querySelectorAll<HTMLElement>(FOCUSABLE)];
    const first = items[0];
    const last = items[items.length - 1];
    if (!first || !last) {
      e.preventDefault();
      return;
    }
    const active = document.activeElement;
    if (e.shiftKey && (active === first || active === panel)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  };

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-overlay/60 p-4 pt-[12vh] backdrop-blur-[2px]">
      {/* Escape and Tab bubble up from the focusable controls inside the dialog. */}
      {/* eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions */}
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        {...(label ? { "aria-label": label } : {})}
        {...(labelledBy ? { "aria-labelledby": labelledBy } : {})}
        tabIndex={-1}
        onKeyDown={onKeyDown}
        data-testid={testId}
        className={cn(
          "w-full max-w-xl overflow-hidden rounded-lg border bg-popover text-popover-foreground shadow-overlay focus:outline-none",
          className,
        )}
      >
        {children}
      </div>
    </div>,
    document.body,
  );
}
