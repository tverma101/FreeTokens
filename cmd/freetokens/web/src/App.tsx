import { useCallback, useEffect, useMemo, useState } from "react";
import { DailyHours } from "./components/DailyHours";
import { Footer } from "./components/Footer";
import { Header } from "./components/Header";
import { ChevronDownIcon } from "./components/Icons";
import { PeriodTabs, tabLabel } from "./components/PeriodTabs";
import { Settings } from "./components/Settings";
import { ErrorState, LoadingState } from "./components/States";
import { UsageSection } from "./components/UsageSection";
import { bridge, isDesktop } from "./lib/api";
import { formatTokens, formatUsd } from "./lib/format";
import type { ValueMode } from "./lib/types";
import { useNow, useSnapshot } from "./lib/useSnapshot";

const DASHBOARD_URL = "https://codebuff.com/account";

export default function App() {
  const { snapshot, loading, refreshing, loadError, refresh } = useSnapshot();
  const now = useNow();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [selection, setSelection] = useState(3);
  const [expanded, setExpanded] = useState(true);
  const [mode, setMode] = useState<ValueMode>("tokens");

  const periods = snapshot?.periods ?? [];
  const safeSelection = periods.length === 0 ? 0 : Math.min(selection, periods.length - 1);
  const period = periods[safeSelection];

  const warnings = useMemo(() => {
    const all = [snapshot?.quotaError, snapshot?.quota?.warning, snapshot?.historyWarning].filter(
      (warning): warning is string => Boolean(warning),
    );
    return Array.from(new Set(all));
  }, [snapshot]);

  useEffect(() => {
    const off = window.runtime?.EventsOn("open-settings", () => setSettingsOpen(true));
    return () => off?.();
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      if (settingsOpen) {
        setSettingsOpen(false);
        return;
      }
      if (isDesktop) bridge?.HideWindow();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [settingsOpen]);

  useEffect(() => {
    if (!isDesktop) return;
    // A freshly shown window blurs by itself, which would close the popover right after the tray
    // click opens it. Only a blur after the pointer or a key reached the popover counts as a click
    // away, and the next hide starts unarmed again.
    let engaged = false;
    const onEngage = () => {
      engaged = true;
    };
    const onBlur = () => {
      if (!engaged) return;
      engaged = false;
      bridge?.RequestHide();
    };
    const onVisibility = () => {
      if (document.visibilityState === "hidden") engaged = false;
    };
    window.addEventListener("pointermove", onEngage, { passive: true });
    window.addEventListener("pointerdown", onEngage, { passive: true });
    window.addEventListener("keydown", onEngage);
    window.addEventListener("blur", onBlur);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("pointermove", onEngage);
      window.removeEventListener("pointerdown", onEngage);
      window.removeEventListener("keydown", onEngage);
      window.removeEventListener("blur", onBlur);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, []);

  const openDashboard = useCallback(() => {
    if (bridge) {
      void bridge.OpenDashboard().catch(() => {});
      return;
    }
    window.open(DASHBOARD_URL, "_blank", "noopener,noreferrer");
  }, []);

  const quit = useCallback(() => bridge?.Quit(), []);

  return (
    <div className="flex h-screen w-screen p-2">
      <div className="flex h-full w-full min-w-0 flex-col overflow-hidden rounded-[16px] border border-white/10 bg-shell/90 shadow-[0_16px_48px_rgba(0,0,0,0.45)] backdrop-blur-2xl">
        {settingsOpen ? (
          <Settings
            demo={!isDesktop}
            onBack={() => setSettingsOpen(false)}
            onQuit={quit}
          />
        ) : (
          <>
            <Header
              plan={snapshot?.quota?.plan}
              demo={!isDesktop}
              refreshing={refreshing}
              warnings={warnings}
              onRefresh={() => void refresh()}
              onSettings={() => setSettingsOpen(true)}
            />

            {loading && !snapshot ? (
              <LoadingState refreshing={refreshing} />
            ) : loadError && !snapshot ? (
              <ErrorState message={loadError} onRetry={() => void refresh()} />
            ) : snapshot ? (
              <main className="scroll-area min-h-0 flex-1 overflow-y-auto px-4 pb-2">
                {snapshot.quota ? <DailyHours quota={snapshot.quota} now={now} /> : null}

                {periods.length === 0 ? (
                  <div className="mt-3 rounded-[12px] border border-dashed border-line px-4 py-7 text-center">
                    <p className="text-[12px] font-medium text-muted">No token history yet</p>
                    <p className="mt-1 text-[11px] leading-snug text-faint">
                      FreeTokens reads measured Freebuff, Antigravity, Claude, Codex and OpenCode runs
                      on this computer. Run a turn, then refresh.
                    </p>
                  </div>
                ) : expanded ? (
                  <>
                    <div className="mt-3">
                      <PeriodTabs periods={periods} selected={safeSelection} onSelect={setSelection} />
                    </div>
                    {period ? (
                      <UsageSection
                        period={period}
                        selected={safeSelection}
                        mode={mode}
                        onMode={setMode}
                      />
                    ) : null}
                    <button
                      type="button"
                      aria-expanded={true}
                      onClick={() => setExpanded(false)}
                      className="mt-2 flex h-8 w-full items-center justify-center gap-1 text-[10.5px] font-medium text-faint transition-colors hover:text-muted"
                    >
                      Hide model breakdown
                      <ChevronDownIcon className="h-3.5 w-3.5 rotate-180" />
                    </button>
                  </>
                ) : (
                  <>
                    <div className="mt-3 divide-y divide-line/60 overflow-hidden rounded-[12px] bg-panel">
                      {periods.map((item, index) => (
                        <button
                          key={item.label}
                          type="button"
                          aria-pressed={index === safeSelection}
                          aria-label={`Show ${item.label} model breakdown`}
                          onClick={() => {
                            setSelection(index);
                            setExpanded(true);
                          }}
                          className={`flex w-full items-baseline justify-between gap-3 px-3.5 py-2.5 text-left transition-colors hover:bg-raised ${
                            index === safeSelection ? "bg-raised/60" : ""
                          }`}
                        >
                          <span className="text-[12.5px] text-muted">{tabLabel(item.label)}</span>
                          <span className="min-w-0 truncate text-right text-[12.5px] font-medium text-ink tnum">
                            {item.hasData ? (
                              <>
                                {item.costUsd != null
                                  ? formatUsd(item.costUsd, item.costLowerBound)
                                  : "unpriced"}
                                <span className="px-1 text-faint">·</span>
                                {formatTokens(item.tokens)} tokens
                              </>
                            ) : (
                              <span className="text-faint">no usage</span>
                            )}
                          </span>
                        </button>
                      ))}
                    </div>
                    <button
                      type="button"
                      aria-expanded={false}
                      onClick={() => setExpanded(true)}
                      className="mt-2 flex h-8 w-full items-center justify-center gap-1 text-[10.5px] font-medium text-faint transition-colors hover:text-muted"
                    >
                      Show model breakdown
                      <ChevronDownIcon className="h-3.5 w-3.5" />
                    </button>
                  </>
                )}
              </main>
            ) : (
              <LoadingState refreshing={refreshing} />
            )}

            {snapshot ? <Footer snapshot={snapshot} now={now} onOpenDashboard={openDashboard} /> : null}
          </>
        )}
      </div>
    </div>
  );
}
