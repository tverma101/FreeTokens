function trim(value: number, places: number): string {
  return value
    .toFixed(places)
    .replace(/\.0+$/, "")
    .replace(/(\.\d*?)0+$/, "$1");
}

export function formatTokens(value: number): string {
  const abs = Math.abs(value);
  if (abs >= 1e9) return `${trim(value / 1e9, 2)}B`;
  if (abs >= 1e6) return `${trim(value / 1e6, 2)}M`;
  if (abs >= 1e3) return `${trim(value / 1e3, 1)}K`;
  return Math.round(value).toString();
}

export function formatUsd(value: number, lowerBound = false): string {
  if (value > 0 && value < 0.01) return "<$0.01";
  return `${lowerBound ? "≥" : "~"}$${value.toFixed(2)}`;
}

export function formatMoney(value: number): string {
  return `$${value.toFixed(2)}`;
}

export function formatHours(value: number): string {
  return `${trim(value, 1)} h`;
}

export function formatClockHours(value: number): string {
  return trim(value, 1);
}

export function formatDuration(ms: number): string {
  const minutes = Math.max(0, Math.round(ms / 60000));
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor(minutes / 60) % 24;
  const mins = minutes % 60;
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${String(mins).padStart(2, "0")}m`;
  return `${mins}m`;
}

export function formatReset(resetsAt: string | undefined, now: number): string | null {
  if (!resetsAt) return null;
  const at = Date.parse(resetsAt);
  if (Number.isNaN(at) || at <= now) return null;
  return formatDuration(at - now);
}

export function formatAgo(iso: string, now: number): string {
  const at = Date.parse(iso);
  if (Number.isNaN(at)) return "recently";
  const diff = Math.max(0, now - at);
  if (diff < 60000) return "just now";
  if (diff < 3600000) return `${Math.floor(diff / 60000)} min ago`;
  if (diff < 48 * 3600000) return `${Math.floor(diff / 3600000)} hr ago`;
  return `${Math.floor(diff / 86400000)} days ago`;
}

export function share(part: number, total: number): number {
  if (!Number.isFinite(part) || !Number.isFinite(total) || total <= 0) return 0;
  return Math.min(1, Math.max(0, part / total));
}

export function percent(part: number, total: number): string {
  const value = share(part, total) * 100;
  if (value > 0 && value < 1) return "<1%";
  return `${Math.round(value)}%`;
}
