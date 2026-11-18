import { RefreshCw, RotateCcw } from "lucide-react";
import { Component, type ReactNode } from "react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { t } from "@/i18n/messages";

/**
 * Page-level error boundary (FR-005 AC13, NFR-AVAIL-004). A render error in one page leaves
 * the frame usable. The fallback shows fixed copy only; React reports the error itself to
 * the browser console, and nothing is sent anywhere.
 */
export class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  override state = { failed: false };

  static getDerivedStateFromError(): { failed: boolean } {
    return { failed: true };
  }

  override render(): ReactNode {
    if (!this.state.failed) return this.props.children;
    return (
      <>
        <PageHeader title={t("error.title")} />
        <Alert data-testid="page-error">
          <p>{t("error.body")}</p>
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                this.setState({ failed: false });
              }}
            >
              <RotateCcw aria-hidden="true" />
              {t("error.retry")}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                window.location.reload();
              }}
            >
              <RefreshCw aria-hidden="true" />
              {t("error.reload")}
            </Button>
          </div>
        </Alert>
      </>
    );
  }
}
