import { useCallback, useEffect, useState } from "react";
import { bridge, errorMessage } from "../lib/api";
import type { Preferences, UpdateStatus } from "../lib/types";
import { BackIcon, CheckIcon, ExternalIcon } from "./Icons";

type Props = {
  demo: boolean;
  onBack: () => void;
  onQuit: () => void;
};

const INTERVALS = [1, 5, 15, 30, 60];

function versionLabel(version: string): string {
  if (!version || version === "dev" || version === "preview") return "development build";
  return `v${version.replace(/^v/, "")}`;
}

export function Settings({ demo, onBack, onQuit }: Props) {
  const [prefs, setPrefs] = useState<Preferences | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [note, setNote] = useState<{ kind: "ok" | "error"; text: string } | null>(null);
  const [update, setUpdate] = useState<UpdateStatus | null>(null);
  const [checking, setChecking] = useState(false);
  const [updateError, setUpdateError] = useState<string | null>(null);

  useEffect(() => {
    if (!bridge) {
      setPrefs({ refreshMinutes: 5, startAtLogin: false, version: "preview", platform: "browser" });
      return;
    }
    bridge
      .GetPreferences()
      .then(setPrefs)
      .catch((error: unknown) => setLoadError(errorMessage(error)));
  }, []);

  const saveMinutes = useCallback(
    async (minutes: number) => {
      if (!bridge || !prefs) return;
      const previous = prefs.refreshMinutes;
      setPrefs({ ...prefs, refreshMinutes: minutes });
      setSaving(true);
      setNote(null);
      try {
        await bridge.SaveRefreshMinutes(minutes);
        setNote({ kind: "ok", text: "Refresh interval saved." });
      } catch (error) {
        setPrefs((current) => (current ? { ...current, refreshMinutes: previous } : current));
        setNote({ kind: "error", text: errorMessage(error) });
      } finally {
        setSaving(false);
      }
    },
    [prefs],
  );

  const saveLogin = useCallback(async () => {
    if (!bridge || !prefs) return;
    const next = !prefs.startAtLogin;
    setPrefs({ ...prefs, startAtLogin: next });
    setSaving(true);
    setNote(null);
    try {
      await bridge.SetStartAtLogin(next);
      setNote({ kind: "ok", text: next ? "FreeTokens will start at login." : "Login item removed." });
    } catch (error) {
      setPrefs((current) => (current ? { ...current, startAtLogin: !next } : current));
      setNote({ kind: "error", text: errorMessage(error) });
    } finally {
      setSaving(false);
    }
  }, [prefs]);

  const checkUpdates = useCallback(async () => {
    if (!bridge) return;
    setChecking(true);
    setUpdateError(null);
    setUpdate(null);
    try {
      setUpdate(await bridge.CheckForUpdates());
    } catch (error) {
      setUpdateError(errorMessage(error));
    } finally {
      setChecking(false);
    }
  }, []);

  const openReleases = useCallback(() => {
    if (bridge) {
      void bridge.OpenReleases().catch(() => {});
      return;
    }
    window.open("https://github.com/tverma101/FreeTokens/releases", "_blank", "noopener,noreferrer");
  }, []);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex shrink-0 items-center gap-2 px-3 pt-3">
        <button
          type="button"
          aria-label="Back to usage"
          onClick={onBack}
          className="grid h-8 w-8 place-items-center rounded-[8px] text-muted transition-colors hover:bg-raised hover:text-ink"
        >
          <BackIcon className="h-4 w-4" />
        </button>
        <h1 className="text-[14px] font-semibold text-ink">Settings</h1>
        <span className="ml-auto pr-1 text-[10.5px] text-faint">
          {prefs ? versionLabel(prefs.version) : ""}
        </span>
      </header>

      <div className="scroll-area mt-1 flex-1 overflow-y-auto px-4 pb-4">
        {loadError ? (
          <p role="alert" className="mt-3 rounded-[10px] bg-panel px-3 py-2.5 text-[11.5px] text-warn">
            {loadError}
          </p>
        ) : null}
        {!prefs && !loadError ? (
          <p role="status" className="mt-4 text-[11.5px] text-faint">
            Reading settings…
          </p>
        ) : null}

        {prefs ? (
          <>
            <section aria-labelledby="refresh-heading" className="mt-3">
              <h2 id="refresh-heading" className="text-[12px] font-medium text-muted">
                Refresh every
              </h2>
              <div role="group" aria-label="Refresh interval" className="mt-2 grid grid-cols-5 gap-1">
                {INTERVALS.map((minutes) => {
                  const active = prefs.refreshMinutes === minutes;
                  return (
                    <button
                      key={minutes}
                      type="button"
                      aria-pressed={active}
                      disabled={demo || saving}
                      onClick={() => void saveMinutes(minutes)}
                      className={`h-[30px] rounded-[8px] text-[10.5px] font-medium transition-colors ${
                        active
                          ? "bg-raised text-ink"
                          : "bg-panel text-muted hover:text-ink disabled:hover:text-muted"
                      } disabled:opacity-60`}
                    >
                      {minutes === 60 ? "1 hr" : `${minutes} min`}
                    </button>
                  );
                })}
              </div>
              <p className="mt-1.5 text-[10.5px] leading-snug text-faint">
                The background refresh uses the same interval as the tray.
              </p>
            </section>

            <section aria-labelledby="login-heading" className="mt-4 border-t border-line/70 pt-3">
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <h2 id="login-heading" className="text-[12px] font-medium text-muted">
                    Start at login
                  </h2>
                  <p className="mt-0.5 text-[10.5px] leading-snug text-faint">
                    Adds the tray icon when you sign in.
                  </p>
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={prefs.startAtLogin}
                  aria-labelledby="login-heading"
                  disabled={demo || saving}
                  onClick={() => void saveLogin()}
                  className={`relative h-[22px] w-[38px] shrink-0 rounded-full transition-colors disabled:opacity-60 ${
                    prefs.startAtLogin ? "bg-brand" : "bg-track"
                  }`}
                >
                  <span
                    className={`absolute top-[3px] h-4 w-4 rounded-full bg-shell transition-[left] motion-reduce:transition-none ${
                      prefs.startAtLogin ? "left-[19px]" : "left-[3px]"
                    }`}
                  />
                </button>
              </div>
            </section>

            <section aria-labelledby="updates-heading" className="mt-4 border-t border-line/70 pt-3">
              <h2 id="updates-heading" className="text-[12px] font-medium text-muted">
                Updates
              </h2>
              <p className="mt-0.5 text-[10.5px] leading-snug text-faint">
                {versionLabel(prefs.version)} · {prefs.platform}
              </p>
              <div className="mt-2 flex gap-1.5">
                <button
                  type="button"
                  disabled={demo || checking}
                  onClick={() => void checkUpdates()}
                  className="h-[30px] rounded-[8px] bg-panel px-2.5 text-[10.5px] font-medium text-muted transition-colors hover:text-ink disabled:opacity-60"
                >
                  {checking ? "Checking…" : "Check for updates"}
                </button>
                <button
                  type="button"
                  onClick={openReleases}
                  className="flex h-[30px] items-center gap-1 rounded-[8px] px-2 text-[10.5px] font-medium text-muted transition-colors hover:text-ink"
                >
                  Releases
                  <ExternalIcon className="h-3 w-3" />
                </button>
              </div>
              {update ? (
                <p className="mt-2 text-[11px] leading-snug text-muted">
                  {update.available ? (
                    <>
                      {update.latest} is available.{" "}
                      <button
                        type="button"
                        onClick={openReleases}
                        className="font-medium text-brand-strong underline underline-offset-2"
                      >
                        Open the release page
                      </button>
                    </>
                  ) : update.devBuild ? (
                    <>Development build. The latest release is {update.latest || "not published yet"}.</>
                  ) : (
                    <>FreeTokens {versionLabel(update.current)} is up to date.</>
                  )}
                </p>
              ) : null}
              {updateError ? (
                <p role="alert" className="mt-2 text-[11px] leading-snug text-warn">
                  {updateError}
                </p>
              ) : null}
              {demo ? (
                <p className="mt-2 text-[10.5px] leading-snug text-faint">
                  These controls work in the desktop app.
                </p>
              ) : null}
            </section>

            {!demo ? (
              <section className="mt-4 border-t border-line/70 pt-3">
                <button
                  type="button"
                  onClick={onQuit}
                  className="h-[32px] w-full rounded-[8px] border border-line text-[11.5px] font-medium text-danger transition-colors hover:bg-panel"
                >
                  Quit FreeTokens
                </button>
              </section>
            ) : null}

            <p aria-live="polite" className="mt-3 min-h-[16px] text-[10.5px] leading-snug">
              {note ? (
                <span
                  className={`inline-flex items-center gap-1 ${note.kind === "ok" ? "text-brand-strong" : "text-warn"}`}
                >
                  {note.kind === "ok" ? <CheckIcon className="h-3 w-3" /> : null}
                  {note.text}
                </span>
              ) : null}
            </p>
          </>
        ) : null}
      </div>
    </div>
  );
}
