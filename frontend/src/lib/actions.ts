import { get } from "svelte/store";
import {
  sessions, selectedDetail, windowMinutes, activityFilter, searchQuery,
  isDetached, isPinned,
} from "./stores";
import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";

export async function loadSessions(): Promise<void> {
  try {
    const items = await SessionService.GetSessions();
    sessions.set(items || []);
  } catch {
    // Dev mode without Go backend
  }
}

export async function loadDetail(id: string): Promise<void> {
  try {
    selectedDetail.set((await SessionService.GetSessionDetail(id)) as any);
  } catch {
    selectedDetail.set(null);
  }
}

const allFilters = ["", "idle", "waiting", "thinking", "compacting", "reading", "writing", "running", "searching", "browsing", "spawning"];

export function cycleFilter(): void {
  const idx = allFilters.indexOf(get(activityFilter));
  const next = allFilters[(idx + 1) % allFilters.length];
  activityFilter.set(next);
  SessionService.SetActivityFilter(next).catch(() => {});
  loadSessions();
}

export function adjustWindow(delta: number): void {
  const next = Math.max(10, Math.min(480, get(windowMinutes) + delta));
  windowMinutes.set(next);
  SessionService.SetWindowMinutes(next).catch(() => {});
  loadSessions();
}

export function toggleDetach(): void {
  if (get(isDetached)) {
    SessionService.Attach().catch(() => {});
  } else {
    SessionService.Detach().catch(() => {});
  }
}

export function togglePin(): void {
  SessionService.TogglePin().catch(() => {});
}

export function setSearch(value: string): void {
  searchQuery.set(value);
  SessionService.SetSearchQuery(value).catch(() => {});
  loadSessions();
}

export function syncDetachState(): void {
  SessionService.IsDetached().then((d) => isDetached.set(d)).catch(() => {});
  SessionService.IsPinned().then((p) => isPinned.set(p)).catch(() => {});
}
