<script lang="ts">
  import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";
  import { View, Severity } from "../bindings/github.com/illegalstudio/lazyagent/internal/limits/models";

  interface Props {
    refreshToken?: number;
    closeHint?: string;
  }

  let { refreshToken = 0, closeHint = "esc / l to close" }: Props = $props();
  let loading = $state(true);
  let view = $state<View | null>(null);
  let tab = $state<"summary" | "detailed">("summary");

  $effect(() => {
    refreshToken;
    let cancelled = false;
    loading = true;

    SessionService.GetLimits()
      .then((v) => {
        if (!cancelled) {
          view = v;
          loading = false;
        }
      })
      .catch(() => {
        if (!cancelled) {
          view = new View({ Reports: [], Summary: [], Available: false });
          loading = false;
        }
      });

    return () => {
      cancelled = true;
    };
  });

  function sevText(sev: Severity): string {
    switch (sev) {
      case Severity.SevOK: return "text-activity-writing";
      case Severity.SevInfo: return "text-activity-thinking";
      case Severity.SevWarn: return "text-activity-running";
      case Severity.SevDanger: return "text-activity-spawning";
      default: return "text-text";
    }
  }

  function sevBar(sev: Severity): string {
    switch (sev) {
      case Severity.SevOK: return "bg-activity-writing";
      case Severity.SevInfo: return "bg-activity-thinking";
      case Severity.SevWarn: return "bg-activity-running";
      case Severity.SevDanger: return "bg-activity-spawning";
      default: return "bg-subtext";
    }
  }

  function paceClass(label: string): string {
    if (label === "overutilizing") return "text-activity-spawning";
    if (label === "on track") return "text-activity-writing";
    return "text-subtext";
  }
</script>

<div class="flex flex-col h-full bg-surface">
  <div class="flex items-center gap-2 px-3 py-2 border-b border-border">
    <button
      class="rounded px-2 py-0.5 text-[12px] font-medium {tab === 'summary' ? 'text-accent bg-accent/10' : 'text-subtext hover:text-text'}"
      onclick={() => (tab = "summary")}
    >Summary</button>
    <button
      class="rounded px-2 py-0.5 text-[12px] font-medium {tab === 'detailed' ? 'text-accent bg-accent/10' : 'text-subtext hover:text-text'}"
      onclick={() => (tab = "detailed")}
    >Detailed</button>
    <span class="ml-auto text-[10px] text-subtext">
      r to refresh{#if closeHint} · {closeHint}{/if}
    </span>
  </div>

  <div class="flex-1 overflow-auto p-3">
    {#if loading}
      <div class="text-[13px] text-subtext">Loading limits…</div>
    {:else if !view || !view.Available}
      <div class="text-[13px] text-subtext">No supported agents detected.</div>
    {:else if tab === "summary"}
      <table class="w-full text-[12px]">
        <thead>
          <tr class="text-subtext text-left">
            <th class="font-medium py-1 pr-3">Agent</th>
            <th class="font-medium py-1 pr-3">5h</th>
            <th class="font-medium py-1">Week / Global</th>
          </tr>
        </thead>
        <tbody>
          {#each view.Summary as row}
            <tr class="border-t border-border/50">
              <td class="py-1 pr-3 text-text">{row.Provider}</td>
              <td class="py-1 pr-3 {sevText(row.FiveHour.Severity)}">{row.FiveHour.Text}</td>
              <td class="py-1 {sevText(row.WeekGlobal.Severity)}">{row.WeekGlobal.Text}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {:else}
      <div class="flex flex-col gap-4">
        {#each view.Reports as report}
          <div class="rounded border border-border p-2">
            <div class="text-[13px] font-bold text-accent mb-1">{report.Provider}</div>
            {#each report.Windows as w}
              <div class="mb-2">
                <div class="text-[12px] font-medium text-text">{w.Label} window</div>
                <div class="flex items-center gap-2 text-[11px] text-subtext">
                  <span class="w-20">Used {w.UsedPercent.toFixed(1)}%</span>
                  <div class="flex-1 h-1.5 rounded bg-border overflow-hidden">
                    <div class="h-full {sevBar(w.UsedSeverity)}" style="width: {Math.max(0, Math.min(100, w.UsedPercent))}%"></div>
                  </div>
                </div>
                <div class="text-[11px] text-subtext">Expected {w.ExpectedPercent.toFixed(1)}%</div>
                {#if w.ResetRelative}
                  <div class="text-[11px] text-subtext">Resets {w.ResetRelative} ({w.ResetAbsolute})</div>
                {/if}
                {#if w.PaceKnown}
                  <div class="text-[11px] {paceClass(w.PaceLabel)}">
                    {w.PaceLabel} ({w.PaceRatio.toFixed(2)}× of expected {w.ExpectedPercent.toFixed(1)}%)
                  </div>
                {/if}
              </div>
            {/each}
            {#if report.Source}<div class="text-[10px] text-subtext">{report.Source}</div>{/if}
            {#if report.Note}<div class="text-[10px] text-subtext">{report.Note}</div>{/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>
