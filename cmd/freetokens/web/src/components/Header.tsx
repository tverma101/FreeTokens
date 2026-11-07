import { useEffect, useState } from "react";
import { CoinMark } from "./CoinMark";
import { AlertIcon, GearIcon, RefreshIcon } from "./Icons";

type Props = {
  plan?: string;
  demo: boolean;
  refreshing: boolean;
  warnings: string[];
  onRefresh: () => void;
  onSettings: () => void;
};

export function Header({ plan, demo, refreshing, warnings, onRefresh, onSettings }: Props) {
  const [showWarnings, setShowWarnings] = useState(false);

  useEffect(() => {
    if (warnings.length === 0) setShowWarnings(false);
  }, [warnings.length]);

  return (
    <header className="shrink-0 px-4 pt-3.5">
      <div className="flex items-center gap-2">
        <CoinMark className="h-[19px] w-[19px] shrink-0 text-brand" />
        <h1 className="text-[15px] font-semibold tracking-[-0.01em] text-ink">FreeTokens</h1>
        {plan ? (
          <span
            title="Freebuff plan"
            className="max-w-[76px] truncate rounded-[6px] bg-raised px-1.5 py-0.5 text-[10.5px] font-medium text-muted"
          >
            {plan.toLowerCase()}
          </span>
        ) : null}
        {demo ? (
          <span
            className="rounded-[6px] px-1.5 py-0.5 text-[10.5px] font-medium text-brand-strong"
            title="Preview data shown because this page is running outside the desktop app"
          >
            Preview data
          </span>
        ) : null}
        <div className="ml-auto flex items-center gap-0.5">
          {warnings.length > 0 ? (
            <button
              type="button"
              aria-label={warnings.length === 1 ? "Show warning" : `Show ${warnings.length} warnings`}
              aria-expanded={showWarnings}
              onClick={() => setShowWarnings((open) => !open)}
              className="grid h-8 w-8 place-items-center rounded-[8px] text-warn transition-colors hover:bg-raised"
            >
              <AlertIcon className="h-4 w-4" />
            </button>
          ) : null}
          <button
            type="button"
            aria-label={refreshing ? "Refreshing usage" : "Refresh usage"}
            aria-busy={refreshing}
            disabled={refreshing}
            onClick={onRefresh}
            className="grid h-8 w-8 place-items-center rounded-[8px] text-muted transition-colors hover:bg-raised hover:text-ink disabled:text-faint"
          >
            <RefreshIcon className={`h-4 w-4 ${refreshing ? "motion-safe:animate-spin" : ""}`} />
          </button>
          <button
            type="button"
            aria-label="Settings"
            onClick={onSettings}
            className="grid h-8 w-8 place-items-center rounded-[8px] text-muted transition-colors hover:bg-raised hover:text-ink"
          >
            <GearIcon className="h-4 w-4" />
          </button>
        </div>
      </div>
      {showWarnings && warnings.length > 0 ? (
        <div role="status" className="mt-2.5 space-y-1.5 rounded-[10px] bg-panel px-3 py-2.5">
          {warnings.map((warning) => (
            <p key={warning} className="flex gap-1.5 text-[11.5px] leading-snug text-warn">
              <AlertIcon className="mt-0.5 h-3 w-3 shrink-0" />
              <span>{warning}</span>
            </p>
          ))}
        </div>
      ) : null}
    </header>
  );
}
