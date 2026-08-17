<script lang="ts">
  import type { SessionItem, CardDensity } from "./stores";
  import { activityColor, formatCost, timeAgo } from "./stores";
  import Sparkline from "./Sparkline.svelte";
  import ActivityBadge from "./ActivityBadge.svelte";

  interface Props {
    session: SessionItem;
    density: CardDensity;
    selected: boolean;
    onselect: (id: string) => void;
  }
  let { session, density, selected, onselect }: Props = $props();

  let color = $derived(activityColor(session.activity));
  let name = $derived(session.customName || session.agentName || session.shortName);

  const glyphs: Record<string, string> = {
    pi: "π", opencode: "O", kilo: "L", cursor: "C", codex: "X", amp: "A", kimi: "K",
  };
  let glyph = $derived(
    glyphs[session.agent] ?? (session.source === "desktop" ? "D" : "")
  );
</script>

<button
  class="flex flex-col text-left rounded-lg border bg-surface px-3 py-2.5 transition-colors duration-75 no-drag
    {selected ? 'border-accent' : 'border-border hover:border-subtext/50'}"
  onclick={() => onselect(session.sessionId)}
>
  <div class="flex items-center justify-between gap-2 w-full">
    <div class="flex items-center gap-1.5 min-w-0">
      <span
        class="shrink-0 h-2 w-2 rounded-full"
        class:animate-pulse-dot={session.isActive}
        style="background: {color};"
      ></span>
      <span class="truncate text-[13px] font-semibold text-text">
        {#if glyph}<span class="{session.agent === 'pi' ? 'text-activity-spawning' : session.source === 'desktop' ? 'text-accent' : 'text-subtext'} font-normal">{glyph}</span>{/if}
        {name}
      </span>
    </div>
    <div class="shrink-0">
      <ActivityBadge activity={session.activity} isActive={session.isActive} />
    </div>
  </div>

  <div class="mt-1.5">
    <Sparkline data={session.sparklineData} {color} width={140} height={16} />
  </div>

  <div class="mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-0.5 text-[10.5px] text-subtext w-full">
    {#if density !== "compact"}
      {#if session.model}<span class="truncate max-w-[9rem]">{session.model}</span>{/if}
      {#if session.gitBranch}<span class="truncate max-w-[9rem]">⎇ {session.gitBranch}</span>{/if}
    {/if}
    <span class="text-activity-reading">{formatCost(session.costUsd)}</span>
    {#if density === "compact"}
      <span>{timeAgo(session.lastActivity)}</span>
    {:else}
      <span>{session.totalMessages} msg</span>
    {/if}
  </div>

  {#if density !== "compact" && session.currentTool}
    <div class="mt-2 pt-1.5 border-t border-surface-hover w-full flex items-center gap-1.5 text-[10.5px] text-subtext">
      <span>▸</span>
      <code class="bg-surface-hover rounded px-1 py-px text-[10px] text-text">{session.currentTool}</code>
    </div>
  {/if}

  {#if density === "live" && session.lastMessage}
    <div class="mt-1.5 w-full rounded border-l-2 border-border bg-surface/60 px-2 py-1 text-[10.5px] italic leading-snug text-subtext line-clamp-2">
      {session.lastMessage}
    </div>
  {/if}
</button>
