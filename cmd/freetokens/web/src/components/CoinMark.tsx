type Props = { className?: string };

// The tray icon is the same coin with a token notch, drawn at runtime in Go.
// Reusing it here is the app's one identity motif rather than a generic logo.
export function CoinMark({ className }: Props) {
  return (
    <svg
      viewBox="0 0 20 20"
      aria-hidden="true"
      focusable="false"
      className={className}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
    >
      <circle cx="10" cy="10" r="7.2" />
      <circle cx="10" cy="10" r="4.4" opacity="0.35" />
      <path d="M10 6.9 13.1 10 10 13.1 6.9 10Z" fill="currentColor" stroke="none" />
    </svg>
  );
}
