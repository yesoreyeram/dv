import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

/** A key cap. Decorative next to a control that already has an accessible name. */
export function Kbd({ className, ...props }: ComponentProps<"kbd">) {
  return (
    <kbd
      className={cn(
        "inline-flex h-5 min-w-5 items-center justify-center rounded-sm border bg-muted px-1 font-mono text-[0.6875rem] leading-none font-medium text-muted-foreground",
        className,
      )}
      {...props}
    />
  );
}
