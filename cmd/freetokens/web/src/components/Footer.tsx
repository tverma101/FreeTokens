import { formatAgo } from "../lib/format";
import type { Snapshot } from "../lib/types";
import { ExternalIcon } from "./Icons";

type Props = {
  snapshot: Snapshot;
  now: number;
  onOpenDashboard: () => void;
};

export function Footer({ snapshot, now, onOpenDashboard }: Props) {
  const partial = (snapshot.periods ?? []).some((period) => period.hasData && period.partial);

  return (
    <footer className="shrink-0 border-t border-line/70 px-4 py-2.5">
      <div className="flex items-baseline justify-between gap-3">
        <p className="text-[10.5px] text-faint">
          FreeTokens · updated {formatAgo(snapshot.refreshedAt, now)}
        </p>
        <p
          className="text-[10.5px] text-faint"
          title={snapshot.sourceNote ?? "Measured usage at public API rates"}
        >
          API-rate estimates
        </p>
      </div>
      {partial ? (
        <p className="mt-1 text-[10.5px] leading-snug text-warn">
          Some runs lack complete receipts, so totals are lower bounds.
        </p>
      ) : null}
      <button
        type="button"
        onClick={onOpenDashboard}
        className="mt-2 flex h-[34px] w-full items-center justify-center gap-1.5 rounded-[9px] bg-raised text-[12px] font-medium text-ink transition-colors hover:bg-[#332f29]"
      >
        Open Freebuff account
        <ExternalIcon className="h-3.5 w-3.5 text-muted" />
      </button>
    </footer>
  );
}
