import { OTHER_COLOR, modelColor } from "./colors";
import type { ModelRow, Period, ValueMode } from "./types";

export type Grouping = "tool" | "provider";

export type UsageRow = {
  name: string;
  tokens: number;
  costUsd?: number;
  costComplete: boolean;
  tools?: string[];
  color: string;
};

type RawRow = Omit<UsageRow, "color"> & { colorFrom: string };

// canGroupByProvider reports whether the snapshot carries provider names. Older snapshots have
// none, so the control stays hidden rather than listing everything as Other.
export function canGroupByProvider(period: Period): boolean {
  return (period.models ?? []).some((model) => model.provider);
}

// groupByProvider sums models that share a provider, so a Claude Code session running a DeepSeek
// model counts toward DeepSeek rather than toward Claude.
function groupByProvider(models: ModelRow[]): RawRow[] {
  const acc = new Map<
    string,
    {
      tokens: number;
      cost: number;
      priced: boolean;
      complete: boolean;
      tools: Map<string, number>;
      dominant: string;
      dominantTokens: number;
    }
  >();
  for (const model of models) {
    const key = model.provider || "Other";
    const row =
      acc.get(key) ??
      { tokens: 0, cost: 0, priced: false, complete: true, tools: new Map(), dominant: model.name, dominantTokens: -1 };
    row.tokens += model.tokens;
    if (model.costUsd != null) {
      row.cost += model.costUsd;
      row.priced = true;
    }
    row.complete = row.complete && model.costComplete;
    const tool = model.source || "Freebuff";
    row.tools.set(tool, (row.tools.get(tool) ?? 0) + model.tokens);
    if (model.tokens > row.dominantTokens) {
      row.dominant = model.name;
      row.dominantTokens = model.tokens;
    }
    acc.set(key, row);
  }
  return [...acc.entries()].map(([name, row]) => ({
    name,
    tokens: row.tokens,
    costUsd: row.priced ? row.cost : undefined,
    costComplete: row.complete,
    tools: [...row.tools.entries()].sort((a, b) => b[1] - a[1]).map(([tool]) => tool),
    // A provider wears the color of its biggest model, so the same identity keeps one color
    // when the panel switches between Tool and Provider grouping.
    colorFrom: row.dominant,
  }));
}

export function buildRows(period: Period, grouping: Grouping): UsageRow[] {
  const models = period.models ?? [];
  const ranked = [...models].sort((a, b) => b.tokens - a.tokens);
  const modelColors = new Map(ranked.map((model, index) => [model.name, modelColor(index)]));

  const raw: RawRow[] =
    grouping === "provider" && canGroupByProvider(period)
      ? groupByProvider(models)
      : models.map((model) => ({
          name: model.name,
          tokens: model.tokens,
          costUsd: model.costUsd,
          costComplete: model.costComplete,
          colorFrom: model.name,
        }));

  return raw.map(({ colorFrom, ...row }) => ({ ...row, color: modelColors.get(colorFrom) ?? OTHER_COLOR }));
}

export function rowValue(row: UsageRow, mode: ValueMode): number {
  return mode === "tokens" ? row.tokens : (row.costUsd ?? 0);
}
