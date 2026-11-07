export type DonutSlice = { name: string; value: number; color: string };

type Props = {
  slices: DonutSlice[];
  total: number;
  centerValue: string;
  centerLabel: string;
};

const SIZE = 112;
const RADIUS = 46;
const STROKE = 13;
const CIRC = 2 * Math.PI * RADIUS;

export function Donut({ slices, total, centerValue, centerLabel }: Props) {
  const visible = slices.filter((slice) => slice.value > 0 && Number.isFinite(slice.value));
  let cursor = 0;

  return (
    <div className="relative grid h-[112px] w-[112px] shrink-0 place-items-center">
      <svg
        width={SIZE}
        height={SIZE}
        viewBox={`0 0 ${SIZE} ${SIZE}`}
        aria-hidden="true"
        focusable="false"
        className="-rotate-90"
      >
        <circle cx={SIZE / 2} cy={SIZE / 2} r={RADIUS} fill="none" stroke="var(--color-track)" strokeWidth={STROKE} />
        {visible.map((slice) => {
          const fraction = total > 0 ? slice.value / total : 0;
          const length = Math.max(0, fraction * CIRC - 2);
          const offset = cursor;
          cursor += fraction * CIRC;
          return (
            <circle
              key={slice.name}
              cx={SIZE / 2}
              cy={SIZE / 2}
              r={RADIUS}
              fill="none"
              stroke={slice.color}
              strokeWidth={STROKE}
              strokeDasharray={`${length} ${CIRC - length}`}
              strokeDashoffset={-offset}
            >
              <title>{slice.name}</title>
            </circle>
          );
        })}
      </svg>
      <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
        <span className="max-w-[80px] truncate text-[18px] leading-none font-semibold tracking-[-0.02em] text-ink tnum">
          {centerValue}
        </span>
        <span className="mt-1 text-[10px] text-faint">{centerLabel}</span>
      </div>
    </div>
  );
}
