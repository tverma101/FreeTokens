import { Fragment, useEffect, useId, useState } from "react";
import { copyText } from "../lib/api";
import { ChevronDownIcon } from "./Icons";
import { formatTokens, formatUsd, percent, share } from "../lib/format";
import type { Grouping, UsageRow } from "../lib/rows";
import { rowValue } from "../lib/rows";
import type { ValueMode } from "../lib/types";

const COLLAPSED_ROWS = 5;

type Props = {
  rows: UsageRow[];
  grouping: Grouping;
  mode: ValueMode;
  total: number;
  periodLabel: string;
  unknownModels?: string[];
  unmatchedVendors?: string[];
};

export function ModelBreakdown({
  rows,
  grouping,
  mode,
  total,
  periodLabel,
  unknownModels,
  unmatchedVendors,
}: Props) {
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState<string | null>(null);
  const listId = useId();

  // Switching period or mode reorders the list, so the fold starts from the top again.
  useEffect(() => setExpanded(false), [periodLabel, mode]);

  useEffect(() => {
    if (copied === null) return;
    const id = window.setTimeout(() => setCopied(null), 1400);
    return () => window.clearTimeout(id);
  }, [copied]);

  const copy = async (name: string) => {
    try {
      await copyText(name);
      setCopied(name);
    } catch {
      // A denied clipboard leaves the label alone rather than claiming a copy happened.
    }
  };

  if (rows.length === 0) return null;

  const sorted = [...rows].sort((a, b) => rowValue(b, mode) - rowValue(a, mode) || a.name.localeCompare(b.name));
  const hidden = sorted.length - COLLAPSED_ROWS;
  const shown = expanded ? sorted : sorted.slice(0, COLLAPSED_ROWS);

  return (
    <div className="mt-4">
      <div className="flex items-baseline justify-between gap-3">
        <h3 className="text-[12px] font-medium text-muted">Breakdown</h3>
        <span className="text-[10.5px] text-faint">share of {mode === "tokens" ? "tokens" : "cost"}</span>
      </div>
      <ul id={listId} className="mt-2 divide-y divide-line/60">
        {shown.map((row) => {
          const value = rowValue(row, mode);
          const costLabel = row.costUsd != null ? formatUsd(row.costUsd, !row.costComplete) : "unpriced";
          const primary = mode === "tokens" ? formatTokens(row.tokens) : costLabel;
          const secondary = mode === "tokens" ? costLabel : `${formatTokens(row.tokens)} tokens`;
          return (
            <li key={row.name} className="py-2">
              <div className="flex items-baseline justify-between gap-3">
                {row.tools ? (
                  <div className="flex min-w-0 items-baseline gap-1.5">
                    <p className="shrink-0 text-[12.5px] font-medium text-ink">{row.name}</p>
                    <p
                      className="min-w-0 truncate text-[10.5px] text-faint"
                      title={`From ${row.tools.join(", ")}`}
                    >
                      {row.tools.join(", ")}
                    </p>
                  </div>
                ) : (
                  <p className="min-w-0 truncate text-[12.5px] font-medium text-ink" title={row.name}>
                    {row.name}
                  </p>
                )}
                <p className="shrink-0 text-[12.5px] font-semibold text-ink tnum">{primary}</p>
              </div>
              <div className="mt-[3px] flex items-baseline justify-between gap-3 text-[10.5px] text-faint tnum">
                <span>{percent(value, total)}</span>
                <span className="truncate">{secondary}</span>
              </div>
              <div className="mt-1.5 h-[5px] overflow-hidden rounded-full bg-track">
                <div
                  className="h-full rounded-full"
                  style={{
                    width: `${value > 0 ? Math.max(1, share(value, total) * 100) : 0}%`,
                    backgroundColor: row.color,
                  }}
                />
              </div>
            </li>
          );
        })}
      </ul>
      {hidden > 0 ? (
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={listId}
          onClick={() => setExpanded((open) => !open)}
          className="mt-1 flex w-full items-center justify-center gap-1.5 rounded-[8px] py-1.5 text-[11px] font-medium text-muted transition-colors hover:bg-raised hover:text-ink"
        >
          <span>
            {expanded
              ? "Show top 5"
              : `Show all ${sorted.length} ${grouping === "provider" ? "providers" : "models"}`}
          </span>
          <ChevronDownIcon className={`h-3 w-3 transition-transform ${expanded ? "rotate-180" : ""}`} />
        </button>
      ) : null}
      {unmatchedVendors && unmatchedVendors.length > 0 ? (
        <p className="mt-2 text-[10.5px] leading-snug text-faint" aria-live="polite">
          Provider unknown:{" "}
          {unmatchedVendors.map((name, index) => (
            <Fragment key={name}>
              {index > 0 ? ", " : null}
              <button
                type="button"
                onClick={() => copy(name)}
                title={`Copy ${name}`}
                aria-label={`Copy model name ${name}`}
                className="cursor-pointer text-left underline decoration-dotted underline-offset-2 transition-colors hover:text-ink"
              >
                {copied === name ? "Copied" : name}
              </button>
            </Fragment>
          ))}
          .
        </p>
      ) : null}
      {unknownModels && unknownModels.length > 0 ? (
        <p className="mt-2 text-[10.5px] leading-snug text-faint">
          Unrecognized models: {unknownModels.join(", ")}. Their usage is counted but not priced.
        </p>
      ) : null}
    </div>
  );
}
