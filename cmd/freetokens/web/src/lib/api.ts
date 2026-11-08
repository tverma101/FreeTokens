import type { Preferences, Snapshot, UpdateStatus } from "./types";
import { demoSnapshot } from "./demo";

interface Bridge {
  GetSnapshot(): Promise<Snapshot | null>;
  RefreshNow(): Promise<Snapshot | null>;
  OpenDashboard(): Promise<void>;
  GetPreferences(): Promise<Preferences>;
  SaveRefreshMinutes(minutes: number): Promise<void>;
  SetStartAtLogin(on: boolean): Promise<void>;
  CheckForUpdates(): Promise<UpdateStatus>;
  OpenReleases(): Promise<void>;
  RequestHide(): void;
  HideWindow(): void;
  Quit(): void;
}

declare global {
  interface Window {
    go?: { main?: { App?: Bridge } };
    runtime?: {
      EventsOn(name: string, callback: (payload: Snapshot) => void): () => void;
      ClipboardSetText?(text: string): Promise<boolean>;
    };
  }
}

const app = typeof window === "undefined" ? undefined : window.go?.main?.App;

/** True when the popover is running inside the Wails window. */
export const isDesktop = Boolean(app);
export const bridge: Bridge | null = app ?? null;
export const initialSnapshot: Snapshot | null = app ? null : demoSnapshot;

/** copyText puts a short string on the system clipboard from either runtime. */
export async function copyText(text: string): Promise<void> {
  const native = window.runtime?.ClipboardSetText;
  if (native) {
    await native(text);
    return;
  }
  await navigator.clipboard.writeText(text);
}

export function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  return "Something went wrong.";
}
