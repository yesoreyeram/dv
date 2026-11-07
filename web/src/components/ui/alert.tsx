import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

export function Alert({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      role="alert"
      className={cn(
        "flex flex-col gap-2 rounded-lg border border-danger/40 bg-danger-soft p-4 text-sm text-foreground",
        className,
      )}
      {...props}
    />
  );
}
