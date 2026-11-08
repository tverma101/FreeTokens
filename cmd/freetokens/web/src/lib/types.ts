export interface Meter {
  label: string;
  used: number;
  limit: number;
  unit: "hours" | "sessions" | "usd" | string;
  resetsAt?: string;
  periodMs?: number;
  detail?: string;
}

export interface Quota {
  status: string;
  plan?: string;
  meters: Meter[];
  hoursBalance?: number;
  monthlySpendUsd?: number;
  activeSessions: string[];
  warning?: string;
}

export interface Variant {
  name: string;
  tokens: number;
  costUsd?: number;
  costComplete: boolean;
}

export interface ModelRow {
  name: string;
  provider?: string;
  source?: string;
  tokens: number;
  costUsd?: number;
  costComplete: boolean;
  variants?: Variant[];
}

export interface SourceRow {
  name: string;
  tokens: number;
}

export interface Period {
  label: string;
  tokens: number;
  costUsd?: number;
  costLowerBound: boolean;
  partial: boolean;
  unknownModels?: string[];
  models: ModelRow[];
  sources?: SourceRow[];
  hasData: boolean;
}

export interface Snapshot {
  refreshedAt: string;
  quota?: Quota;
  quotaError?: string;
  historyWarning?: string;
  periods?: Period[];
  sourceNote?: string;
}

export interface Preferences {
  refreshMinutes: number;
  startAtLogin: boolean;
  version: string;
  platform: string;
}

export interface UpdateStatus {
  current: string;
  latest: string;
  available: boolean;
  devBuild: boolean;
  url?: string;
}

export type ValueMode = "tokens" | "cost";
