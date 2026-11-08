import { useCallback, useEffect, useState } from "react";
import { bridge, errorMessage, initialSnapshot, isDesktop } from "./api";
import type { Snapshot } from "./types";

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

export function useSnapshot() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(initialSnapshot);
  const [loading, setLoading] = useState(isDesktop);
  const [refreshing, setRefreshing] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  useEffect(() => {
    if (!bridge) return;
    let alive = true;
    let poll = 0;

    const accept = (next: Snapshot | null) => {
      if (!alive || !next) return;
      setSnapshot(next);
      setLoading(false);
      setLoadError(null);
      if (poll) {
        window.clearInterval(poll);
        poll = 0;
      }
    };

    bridge.GetSnapshot().then(accept).catch((error: unknown) => {
      if (alive) setLoadError(errorMessage(error));
    });

    // The first refresh can outlive the window's first paint; poll until it lands.
    poll = window.setInterval(() => {
      bridge
        ?.GetSnapshot()
        .then(accept)
        .catch(() => {});
    }, 1500);

    const off = window.runtime?.EventsOn("snapshot", accept);
    return () => {
      alive = false;
      if (poll) window.clearInterval(poll);
      off?.();
    };
  }, []);

  const refresh = useCallback(async () => {
    if (!bridge) {
      setRefreshing(true);
      await delay(650);
      setRefreshing(false);
      return;
    }
    setRefreshing(true);
    setLoadError(null);
    try {
      const next = await bridge.RefreshNow();
      if (next) {
        setSnapshot(next);
        setLoading(false);
      }
    } catch (error) {
      setLoadError(errorMessage(error));
    } finally {
      setRefreshing(false);
    }
  }, []);

  return { snapshot, loading, refreshing, loadError, refresh };
}

export function useNow(intervalMs = 30000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);
  return now;
}
