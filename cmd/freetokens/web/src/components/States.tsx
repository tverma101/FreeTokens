import { CoinMark } from "./CoinMark";
import { AlertIcon } from "./Icons";

export function LoadingState({ refreshing }: { refreshing: boolean }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 text-center" role="status">
      <CoinMark className="h-8 w-8 text-brand motion-safe:animate-pulse" />
      <p className="mt-3 text-[12.5px] font-medium text-ink">
        {refreshing ? "Refreshing Freebuff usage" : "Reading Freebuff usage"}
      </p>
      <p className="mt-1 text-[11px] leading-snug text-faint">
        Your login and history stay on this device.
      </p>
    </div>
  );
}

export function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 text-center" role="alert">
      <span className="grid h-9 w-9 place-items-center rounded-full bg-panel text-warn">
        <AlertIcon className="h-4 w-4" />
      </span>
      <p className="mt-2.5 text-[12.5px] font-medium text-ink">Could not load usage</p>
      <p className="mt-1 text-[11px] leading-snug text-faint">{message}</p>
      <button
        type="button"
        onClick={onRetry}
        className="mt-3 h-8 rounded-[8px] bg-raised px-3 text-[11.5px] font-medium text-ink transition-colors hover:bg-[#332f29]"
      >
        Try again
      </button>
    </div>
  );
}
