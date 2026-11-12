import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

/** Decorative loading placeholder; pair it with a `role="status"` text for screen readers. */
export function Skeleton({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      aria-hidden="true"
      className={cn("animate-pulse rounded-sm bg-muted", className)}
      {...props}
    />
  );
}
