import { useState } from "react";
import { OTHER_COLOR } from "../lib/colors";
import { formatTokens, formatUsd } from "../lib/format";
import { buildRows, canGroupByProvider, rowValue, type Grouping, type UsageRow } from "../lib/rows";
import type { Period, ValueMode } from "../lib/types";
import { Donut, type DonutSlice } from "./Donut";
import { ModelBreakdown } from "./ModelBreakdown";

type Props = {
  period: Period;
  selected: number;
  mode: ValueMode;
  onMode: (mode: ValueMode) => void;
};

type LegendRow = UsageRow & { value: number };

const DONUT_SLICES = 5;

export function UsageSection({ period, selected, mode, onMode }: Props) {
  const [grouping, setGrouping] = useState<Grouping>("tool");

  const models = period.models ?? [];
  const groupable = canGroupByProvider(period) && models.length > 1;
  const activeGrouping: Grouping = groupable ? grouping : "tool";
  const rows = buildRows(period, activeGrouping);

  const total =
    mode === "tokens" ? period.tokens : (period.costUsd ?? models.reduce((sum, m) => sum + (m.costUsd ?? 0), 0));

  const sources = period.sources ?? [];
  const legendRows: LegendRow[] = rows
    .map((row) => ({ ...row, value: rowValue(row, mode) }))
    .filter((row) => row.value > 0)
    .sort((a, b) => b.value - a.value || a.name.localeCompare(b.name));
  const empty = total <= 0 || legendRows.length === 0;

  const restLabel = activeGrouping === "provider" ? "More providers" : "Other models";
  let slices: DonutSlice[] = legendRows.map((row) => ({ name: row.name, value: row.value, color: row.color }));
  let legend: LegendRow[] = legendRows;
  if (legendRows.length > DONUT_SLICES) {
    const restRows = legendRows.slice(DONUT_SLICES);
    const rest = restRows.reduce((sum, row) => sum + row.value, 0);
    slices = [...slices.slice(0, DONUT_SLICES), { name: restLabel, value: rest, color: OTHER_COLOR }];
    legend = [
      ...legendRows.slice(0, DONUT_SLICES),
      {
        ...restRows[0],
        name: restLabel,
        value: rest,
        color: OTHER_COLOR,
        costUsd: restRows.some((row) => row.costUsd != null) ? restRows.reduce((sum, row) => sum + (row.costUsd ?? 0), 0) : undefined,
        costComplete: restRows.every((row) => row.costComplete),
        tools: undefined,
      },
    ];
  }

  const centerValue = mode === "tokens" ? formatTokens(total) : formatUsd(total, period.costLowerBound);
  const centerLabel = mode === "tokens" ? "tokens" : "estimated";
  const unpricedCount = mode === "cost" ? models.filter((model) => model.costUsd == null).length : 0;
  // Names no provider rule recognized: the fastest way to see which keyword is missing.
  const unmatchedVendors =
    activeGrouping === "provider"
      ? models
          .filter((model) => model.provider === "Other")
          .sort((a, b) => b.tokens - a.tokens)
          .map((model) => model.name)
      : [];

  return (
    <section id="usage-panel" role="tabpanel" aria-labelledby={`period-tab-${selected}`} className="mt-3">
      <div className="flex items-center justify-between gap-3">
        <h2 className="min-w-0 truncate text-[12px] font-medium text-muted">
          {activeGrouping === "provider" ? "Provider mix" : "Model mix"}
        </h2>
        <div className="flex shrink-0 items-center gap-2">
          {groupable ? (
            <div role="group" aria-label="Group usage by" className="flex rounded-[8px] border border-line p-[2px]">
              {(["tool", "provider"] as const).map((option) => {
                const active = grouping === option;
                return (
                  <button
                    key={option}
                    type="button"
                    aria-pressed={active}
                    onClick={() => setGrouping(option)}
                    title={
                      option === "provider"
                        ? "Group by the model's provider, whichever tool ran it"
                        : "Group by the model, with the tool that ran it"
                    }
                    className={`h-[22px] rounded-[6px] px-2 text-[10.5px] font-medium transition-colors ${
                      active ? "bg-raised text-ink" : "text-muted hover:text-ink"
                    }`}
                  >
                    {option === "tool" ? "Tool" : "Provider"}
                  </button>
                );
              })}
            </div>
          ) : null}
          <div role="group" aria-label="Show values as" className="flex rounded-[8px] border border-line p-[2px]">
            {(["tokens", "cost"] as const).map((option) => {
              const active = mode === option;
              return (
                <button
                  key={option}
                  type="button"
                  aria-pressed={active}
                  onClick={() => onMode(option)}
                  className={`h-[22px] rounded-[6px] px-2 text-[10.5px] font-medium transition-colors ${
                    active ? "bg-raised text-ink" : "text-muted hover:text-ink"
                  }`}
                >
                  {option === "tokens" ? "Tokens" : "Cost"}
                </button>
              );
            })}
          </div>
        </div>
      </div>

      {empty ? (
        <div className="mt-3 rounded-[12px] border border-dashed border-line px-4 py-6 text-center">
          <p className="text-[12px] font-medium text-muted">
            {!period.hasData
              ? "No measured usage for this period"
              : mode === "cost"
                ? "No public price for this period's models"
                : "No token usage recorded for this period"}
          </p>
          <p className="mt-1 text-[11px] text-faint">
            {period.hasData && mode === "cost"
              ? "Switch to tokens to see the measured counts."
              : "Run a Freebuff, Antigravity, Claude, Codex or OpenCode turn, then refresh."}
          </p>
        </div>
      ) : (
        <>
          <div className="mt-3 flex items-center gap-4">
            <Donut slices={slices} total={total} centerValue={centerValue} centerLabel={centerLabel} />
            <ul className="min-w-0 flex-1 space-y-[4px]">
              {legend.map((row) => (
                <li key={row.name} className="flex min-w-0 items-center gap-2">
                  <span
                    aria-hidden="true"
                    className="h-[7px] w-[7px] shrink-0 rounded-full"
                    style={{ backgroundColor: row.color }}
                  />
                  <span className="min-w-0 flex-1 truncate text-[11px] text-muted" title={row.name}>
                    {row.name}
                  </span>
                  <span className="shrink-0 text-[11px] font-medium text-ink tnum">
                    {mode === "tokens" ? formatTokens(row.value) : formatUsd(row.value, row.costUsd != null && !row.costComplete)}
                  </span>
                </li>
              ))}
            </ul>
          </div>
          {sources.length > 0 ? (
            <div className="mt-2 flex flex-wrap items-baseline gap-x-3 gap-y-0.5 border-t border-line/70 pt-1.5 text-[10px]">
              <span className="text-faint">By source</span>
              {sources.map((source) => (
                <span key={source.name} className="flex items-baseline gap-1 whitespace-nowrap">
                  <span className="text-muted">{source.name}</span>
                  <span className="font-medium text-ink tnum">{formatTokens(source.tokens)}</span>
                </span>
              ))}
            </div>
          ) : null}
          {unpricedCount > 0 ? (
            <p className="mt-2 text-[10.5px] text-faint">
              {unpricedCount === 1 ? "One model has" : `${unpricedCount} models have`} no public price, so the
              cost total is a lower bound.
            </p>
          ) : null}
          <ModelBreakdown
            rows={rows}
            grouping={activeGrouping}
            mode={mode}
            total={total}
            periodLabel={period.label}
            unknownModels={period.unknownModels}
            unmatchedVendors={unmatchedVendors}
          />
        </>
      )}
    </section>
  );
}
