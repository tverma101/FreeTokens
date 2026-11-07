import type { Period } from "../lib/types";

type Props = {
  periods: Period[];
  selected: number;
  onSelect: (index: number) => void;
};

export function tabLabel(label: string): string {
  return label === "Last 30 Days" ? "30 Days" : label;
}

export function PeriodTabs({ periods, selected, onSelect }: Props) {
  if (periods.length === 0) return null;

  const move = (event: React.KeyboardEvent<HTMLButtonElement>, index: number) => {
    const last = periods.length - 1;
    let next = index;
    if (event.key === "ArrowRight") next = index === last ? 0 : index + 1;
    else if (event.key === "ArrowLeft") next = index === 0 ? last : index - 1;
    else if (event.key === "Home") next = 0;
    else if (event.key === "End") next = last;
    else return;
    event.preventDefault();
    onSelect(next);
    const tabs = event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>("[role=tab]");
    tabs?.[next]?.focus();
  };

  return (
    <div
      role="tablist"
      aria-label="Usage period"
      className="grid grid-cols-4 gap-0.5 rounded-[10px] bg-panel p-0.5"
    >
      {periods.map((period, index) => {
        const active = index === selected;
        return (
          <button
            key={period.label}
            id={`period-tab-${index}`}
            role="tab"
            type="button"
            aria-selected={active}
            aria-controls="usage-panel"
            tabIndex={active ? 0 : -1}
            onClick={() => onSelect(index)}
            onKeyDown={(event) => move(event, index)}
            className={`h-[30px] rounded-[8px] text-[11.5px] font-medium transition-colors ${
              active ? "bg-raised text-ink" : "text-muted hover:text-ink"
            }`}
          >
            {tabLabel(period.label)}
          </button>
        );
      })}
    </div>
  );
}
