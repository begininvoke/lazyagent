<script lang="ts">
  import {
    sessions, selectedId, activeCount, windowMinutes, activityFilter,
    searchQuery, searching, showLimits, limitsRefreshToken, updateVersion,
    isPinned, cardDensity, detailWidth,
  } from "./stores";
  import type { CardDensity } from "./stores";
  import {
    cycleFilter, adjustWindow, toggleDetach, togglePin, setSearch,
  } from "./actions";
  import SessionCard from "./SessionCard.svelte";
  import DetailPanel from "./DetailPanel.svelte";
  import LimitsPage from "./LimitsPage.svelte";
  import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";

  const densities: CardDensity[] = ["compact", "rich", "live"];
  const minCardWidth: Record<CardDensity, number> = { compact: 220, rich: 260, live: 300 };

  function pickDensity(d: CardDensity) {
    $cardDensity = d;
    SessionService.SetCardDensity(d).catch(() => {});
  }

  function select(id: string) {
    $selectedId = $selectedId === id ? null : id;
  }

  // Detail panel resize: drag the handle on the panel's left edge.
  // Pointer capture keeps move/up events on the handle even when the
  // pointer leaves it; the width persists once, on release.
  let resizing = $state(false);
  let resizeStartX = 0;
  let resizeStartWidth = 0;

  function startResize(e: PointerEvent) {
    resizing = true;
    resizeStartX = e.clientX;
    resizeStartWidth = $detailWidth;
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
  }

  function moveResize(e: PointerEvent) {
    if (!resizing) return;
    const max = Math.floor(window.innerWidth * 0.6);
    $detailWidth = Math.min(max, Math.max(300, resizeStartWidth + (resizeStartX - e.clientX)));
  }

  function endResize() {
    if (!resizing) return;
    resizing = false;
    SessionService.SetDetailWidth($detailWidth).catch(() => {});
  }

  function resetWidth() {
    $detailWidth = 400;
    SessionService.SetDetailWidth(400).catch(() => {});
  }

  // Grid j/k navigation while the detail panel is open or closed.
  function handleKeydown(e: KeyboardEvent) {
    const tag = (e.target as HTMLElement)?.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA") return;
    if ($showLimits) return;
    const list = $sessions;
    if (!list.length) return;
    const idx = list.findIndex((s) => s.sessionId === $selectedId);
    if (e.key === "j" || e.key === "ArrowDown") {
      e.preventDefault();
      $selectedId = list[Math.min(idx + 1, list.length - 1)].sessionId;
    } else if (e.key === "k" || e.key === "ArrowUp") {
      e.preventDefault();
      $selectedId = list[Math.max(idx - 1, 0)].sessionId;
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="flex flex-col h-screen bg-[#11111b]">
  <!-- Toolbar -->
  <header class="flex items-center justify-between px-4 py-2 bg-surface border-b border-border drag-region shrink-0">
    <div class="flex items-center gap-2 no-drag">
      <h1 class="text-[14px] font-bold text-accent">lazyagent</h1>
      <span class="text-[11px] text-subtext">{$activeCount} active</span>
    </div>
    <div class="flex items-center gap-2.5 no-drag">
      <div class="flex rounded-md bg-surface-hover overflow-hidden">
        {#each densities as d}
          <button
            class="px-2 py-0.5 text-[10.5px] {$cardDensity === d ? 'bg-accent text-surface font-semibold' : 'text-subtext hover:text-text'}"
            onclick={() => pickDensity(d)}
          >{d}</button>
        {/each}
      </div>
      <button
        class="rounded px-1.5 py-0.5 text-[11px] font-medium {$showLimits ? 'text-accent bg-accent/10' : 'text-subtext hover:text-text'}"
        onclick={() => ($showLimits = !$showLimits)}
        title="Show limits (l)"
      >limits</button>
      {#if $activityFilter}
        <button
          class="rounded px-1.5 py-0.5 text-[11px] font-medium text-accent bg-accent/10 hover:bg-accent/20"
          onclick={cycleFilter}
        >{$activityFilter}</button>
      {/if}
      <span class="text-[11px] text-subtext">{$windowMinutes}m</span>
      <button class="text-subtext hover:text-text text-[14px] leading-none" onclick={() => adjustWindow(-10)} title="Decrease time window">−</button>
      <button class="text-subtext hover:text-text text-[14px] leading-none" onclick={() => adjustWindow(10)} title="Increase time window">+</button>
      <button
        class="leading-none text-[11px] font-medium rounded px-1 py-0.5 {$isPinned ? 'text-accent bg-accent/10' : 'text-subtext hover:text-text'}"
        onclick={togglePin}
        title={$isPinned ? "Unpin from top" : "Pin on top"}
      >pin</button>
      <button
        class="text-subtext hover:text-text text-[14px] leading-none"
        onclick={toggleDetach}
        title="Attach to tray (d)"
      >⤡</button>
    </div>
  </header>

  <!-- Search bar -->
  {#if $searching}
    <div class="px-4 py-1.5 bg-surface border-b border-border shrink-0">
      <input
        type="text"
        class="w-full bg-transparent text-text text-[13px] outline-none placeholder-subtext"
        placeholder="Search sessions..."
        value={$searchQuery}
        oninput={(e) => setSearch((e.target as HTMLInputElement).value)}
      />
    </div>
  {/if}

  <!-- Content -->
  <div class="flex-1 flex min-h-0">
    {#if $showLimits}
      <div class="flex-1 overflow-hidden bg-surface">
        <LimitsPage refreshToken={$limitsRefreshToken} />
      </div>
    {:else}
      <div class="flex-1 min-w-0 overflow-y-auto p-3">
        {#if $sessions.length}
          <div
            class="grid gap-3"
            style="grid-template-columns: repeat(auto-fill, minmax({minCardWidth[$cardDensity]}px, 1fr));"
          >
            {#each $sessions as session (session.sessionId)}
              <SessionCard
                {session}
                density={$cardDensity}
                selected={session.sessionId === $selectedId}
                onselect={select}
              />
            {/each}
          </div>
        {:else}
          <div class="flex items-center justify-center h-full text-subtext text-sm">
            No sessions found
          </div>
        {/if}
      </div>
      {#if $selectedId}
        <div
          class="relative shrink-0 min-h-0 max-w-[60vw]"
          style="width: {$detailWidth}px; min-width: 300px;"
        >
          <div
            class="absolute left-0 top-0 bottom-0 w-[5px] z-10 cursor-col-resize hover:bg-accent/30 {resizing ? 'bg-accent/30' : ''}"
            role="separator"
            aria-orientation="vertical"
            title="Drag to resize, double-click to reset"
            onpointerdown={startResize}
            onpointermove={moveResize}
            onpointerup={endResize}
            onpointercancel={endResize}
            ondblclick={resetWidth}
          ></div>
          <DetailPanel />
        </div>
      {/if}
    {/if}
  </div>

  <!-- Footer -->
  <footer class="px-3 py-1 bg-surface border-t border-border shrink-0">
    {#if $updateVersion}
      <div class="flex items-center justify-center gap-1 text-[10px] text-accent pb-0.5">
        <span>↑ lazyagent {$updateVersion} available —</span>
        <button class="underline hover:text-text cursor-pointer" onclick={() => SessionService.OpenReleases()}>releases</button>
      </div>
    {/if}
    <div class="flex items-center justify-center gap-3 text-[10px] text-subtext">
      <span><kbd class="text-text/60">j/k</kbd> navigate</span>
      <span><kbd class="text-text/60">/</kbd> search</span>
      <span><kbd class="text-text/60">f</kbd> filter</span>
      <span><kbd class="text-text/60">l</kbd> limits</span>
      <span><kbd class="text-text/60">+/−</kbd> window</span>
      <span><kbd class="text-text/60">d</kbd> attach</span>
      <span><kbd class="text-text/60">esc</kbd> close</span>
    </div>
  </footer>
</div>
