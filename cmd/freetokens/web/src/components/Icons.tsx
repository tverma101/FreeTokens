type IconProps = { className?: string };

const base = {
  "aria-hidden": true,
  focusable: false,
  viewBox: "0 0 16 16",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.4,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

export function RefreshIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className}>
      <path d="M13.2 7.4a5.2 5.2 0 1 1-1.6-3.5" />
      <path d="M13.3 2.6v3.2H10" />
    </svg>
  );
}

export function AlertIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className}>
      <path d="M8 2.6 14 13H2L8 2.6Z" />
      <path d="M8 6.4v3.1" />
      <path d="M8 11.4h.01" />
    </svg>
  );
}

export function GearIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className}>
      <path d="M2.4 4.6h3.2M8.6 4.6h5M2.4 8h7.2M12.6 8h1M2.4 11.4h1.2M6.6 11.4h7" />
      <circle cx="7" cy="4.6" r="1.35" />
      <circle cx="10.8" cy="8" r="1.35" />
      <circle cx="5" cy="11.4" r="1.35" />
    </svg>
  );
}

export function ChevronDownIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className}>
      <path d="m4.2 6.2 3.8 3.8 3.8-3.8" />
    </svg>
  );
}

export function ExternalIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className}>
      <path d="M9.4 2.6h4v4" />
      <path d="m13.4 2.6-5.8 5.8" />
      <path d="M12 9.6v2.8a1 1 0 0 1-1 1H3.7a1 1 0 0 1-1-1V5.1a1 1 0 0 1 1-1h2.8" />
    </svg>
  );
}

export function BackIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className}>
      <path d="M9.8 3.2 5 8l4.8 4.8" />
    </svg>
  );
}

export function CheckIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className}>
      <path d="m3.2 8.4 3 3 6.6-6.8" />
    </svg>
  );
}
