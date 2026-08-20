<script lang="ts">
  import { onMount } from "svelte";
  import { limitsRefreshToken } from "./stores";
  import LimitsPage from "./LimitsPage.svelte";

  interface Props {
    onclose: () => void;
  }

  let { onclose }: Props = $props();

  const viewportMargin = 12;
  const preferredWidth = 720;
  const preferredHeight = 680;
  const minimumWidth = 420;
  const minimumHeight = 280;
  let dialogEl = $state<HTMLDivElement | null>(null);
  let dialogWidth = $state(preferredWidth);
  let dialogHeight = $state(preferredHeight);
  let offsetX = $state(0);
  let offsetY = $state(0);
  let dragging = $state(false);
  let resizing = $state(false);
  let pinned = $state(false);
  let dragStartX = 0;
  let dragStartY = 0;
  let dragStartOffsetX = 0;
  let dragStartOffsetY = 0;
  let resizeStartX = 0;
  let resizeStartY = 0;
  let resizeStartWidth = 0;
  let resizeStartHeight = 0;
  let resizeStartOffsetX = 0;
  let resizeStartOffsetY = 0;
  let resizeMaxWidth = 0;
  let resizeMaxHeight = 0;

  function clampOffset(x: number, y: number): { x: number; y: number } {
    if (!dialogEl) return { x, y };

    const { width, height } = dialogEl.getBoundingClientRect();
    const centeredLeft = (window.innerWidth - width) / 2;
    const centeredTop = (window.innerHeight - height) / 2;

    return {
      x: Math.min(
        window.innerWidth - viewportMargin - width - centeredLeft,
        Math.max(viewportMargin - centeredLeft, x),
      ),
      y: Math.min(
        window.innerHeight - viewportMargin - height - centeredTop,
        Math.max(viewportMargin - centeredTop, y),
      ),
    };
  }

  function startDrag(e: PointerEvent) {
    if (pinned || e.button !== 0 || (e.target as Element).closest("button")) return;
    dragging = true;
    dragStartX = e.clientX;
    dragStartY = e.clientY;
    dragStartOffsetX = offsetX;
    dragStartOffsetY = offsetY;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    e.preventDefault();
  }

  function moveDrag(e: PointerEvent) {
    if (!dragging) return;
    const next = clampOffset(
      dragStartOffsetX + e.clientX - dragStartX,
      dragStartOffsetY + e.clientY - dragStartY,
    );
    offsetX = next.x;
    offsetY = next.y;
  }

  function endDrag(e: PointerEvent) {
    if (!dragging) return;
    dragging = false;
    const target = e.currentTarget as HTMLElement;
    if (target.hasPointerCapture(e.pointerId)) target.releasePointerCapture(e.pointerId);
  }

  function keepInViewport() {
    const next = clampOffset(offsetX, offsetY);
    offsetX = next.x;
    offsetY = next.y;
  }

  // The CSS min() guard protects the very first paint. This synchronizes the
  // state with the rendered size on open (and after a host-window resize),
  // then clamps any translated position back inside the containing window.
  function fitDialogToViewport() {
    if (!dialogEl) return;
    const rect = dialogEl.getBoundingClientRect();
    dialogWidth = rect.width;
    dialogHeight = rect.height;
    keepInViewport();
  }

  function startResize(e: PointerEvent) {
    if (e.button !== 0 || !dialogEl) return;
    const rect = dialogEl.getBoundingClientRect();
    resizing = true;
    resizeStartX = e.clientX;
    resizeStartY = e.clientY;
    resizeStartWidth = rect.width;
    resizeStartHeight = rect.height;
    resizeStartOffsetX = offsetX;
    resizeStartOffsetY = offsetY;
    resizeMaxWidth = Math.max(1, window.innerWidth - viewportMargin - rect.left);
    resizeMaxHeight = Math.max(1, window.innerHeight - viewportMargin - rect.top);
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    e.preventDefault();
    e.stopPropagation();
  }

  function moveResize(e: PointerEvent) {
    if (!resizing) return;
    const minWidth = Math.min(minimumWidth, resizeMaxWidth);
    const minHeight = Math.min(minimumHeight, resizeMaxHeight);
    const nextWidth = Math.min(
      resizeMaxWidth,
      Math.max(minWidth, resizeStartWidth + e.clientX - resizeStartX),
    );
    const nextHeight = Math.min(
      resizeMaxHeight,
      Math.max(minHeight, resizeStartHeight + e.clientY - resizeStartY),
    );

    // The dialog is flex-centered, so compensate by half the size delta to
    // keep its top-left corner stationary while the bottom-right handle moves.
    dialogWidth = nextWidth;
    dialogHeight = nextHeight;
    offsetX = resizeStartOffsetX + (nextWidth - resizeStartWidth) / 2;
    offsetY = resizeStartOffsetY + (nextHeight - resizeStartHeight) / 2;
  }

  function endResize(e: PointerEvent) {
    if (!resizing) return;
    resizing = false;
    const target = e.currentTarget as HTMLElement;
    if (target.hasPointerCapture(e.pointerId)) target.releasePointerCapture(e.pointerId);
  }

  function refresh() {
    $limitsRefreshToken += 1;
  }

  function togglePinned() {
    pinned = !pinned;
    if (pinned) dragging = false;
  }

  onMount(fitDialogToViewport);
</script>

<svelte:window onresize={fitDialogToViewport} />

<div class="pointer-events-none fixed inset-0 z-30 flex items-center justify-center p-3">
  <div
    bind:this={dialogEl}
    class="pointer-events-auto relative flex flex-col overflow-hidden rounded-xl border border-surface-active bg-surface shadow-[0_20px_60px_rgba(0,0,0,0.72)]"
    class:select-none={dragging || resizing}
    style="width: min({dialogWidth}px, calc(100vw - {viewportMargin * 2}px)); height: min({dialogHeight}px, calc(100vh - {viewportMargin * 2}px)); transform: translate({offsetX}px, {offsetY}px);"
    role="dialog"
    aria-labelledby="limits-modal-title"
    tabindex="-1"
  >
    <header
      class="flex shrink-0 items-center gap-2 border-b border-border bg-[#181825] px-3 py-2 {pinned ? 'cursor-default' : dragging ? 'cursor-grabbing' : 'cursor-move'}"
      role="toolbar"
      aria-label="Limits window controls"
      tabindex="-1"
      onpointerdown={startDrag}
      onpointermove={moveDrag}
      onpointerup={endDrag}
      onpointercancel={endDrag}
    >
      <svg viewBox="0 0 24 24" class="h-3.5 w-3.5 shrink-0 text-subtext" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M5 9h14M5 15h14" />
      </svg>
      <h2 id="limits-modal-title" class="text-[13px] font-bold text-text">Limits</h2>
      <span class="text-[10px] text-subtext">{pinned ? "pinned in place" : "drag to move"}</span>

      <div class="ml-auto flex items-center gap-1.5">
        <button
          class="rounded-lg border border-border bg-[#11111b] px-2 py-1 text-[11px] text-subtext hover:text-text"
          onclick={refresh}
          title="Refresh limits (r)"
        >
          <span class="flex items-center gap-1.5">
            <svg viewBox="0 0 24 24" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/></svg>
            Refresh
          </span>
        </button>
        <button
          class="rounded-lg border px-2 py-1 {pinned ? 'border-accent bg-accent text-surface' : 'border-border bg-[#11111b] text-subtext hover:text-text'}"
          onclick={togglePinned}
          title={pinned ? "Unpin dialog to move it" : "Pin dialog in place"}
          aria-pressed={pinned}
        >
          <svg viewBox="0 0 24 24" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1z"/></svg>
        </button>
        <button
          class="rounded-lg border border-border bg-[#11111b] px-2 py-1 text-[13px] leading-none text-subtext hover:text-text"
          onclick={onclose}
          title="Close limits"
        >✕</button>
      </div>
    </header>

    <div class="min-h-0 flex-1">
      <LimitsPage refreshToken={$limitsRefreshToken} closeHint="close with ×" />
    </div>

    <button
      class="absolute bottom-0 right-0 z-10 flex h-5 w-5 cursor-nwse-resize items-end justify-end p-1 text-border hover:text-accent"
      aria-label="Resize limits dialog"
      title="Drag to resize"
      onpointerdown={startResize}
      onpointermove={moveResize}
      onpointerup={endResize}
      onpointercancel={endResize}
    >
      <svg viewBox="0 0 12 12" class="h-3 w-3" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
        <path d="M4 10 10 4M7 10l3-3" />
      </svg>
    </button>
  </div>
</div>
