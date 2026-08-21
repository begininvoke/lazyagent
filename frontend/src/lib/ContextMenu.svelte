<script lang="ts">
  import type { SessionItem } from "./stores";
  import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";

  interface Props {
    session: SessionItem;
    x: number;
    y: number;
    onclose: () => void;
    onrename: (id: string) => void;
  }
  let { session, x, y, onclose, onrename }: Props = $props();

  let menuEl = $state<HTMLDivElement | null>(null);

  // Prefetch on open: WebKit's clipboard API requires transient user
  // activation, which expires across async boundaries. Copying must happen
  // synchronously in the click handler, so the command has to be ready.
  let resumeCommand = $state("");
  $effect(() => {
    resumeCommand = "";
    SessionService.GetSessionDetail(session.sessionId)
      .then((d) => { resumeCommand = d?.resumeCommand ?? ""; })
      .catch(() => {});
  });

  // Keep the menu inside the viewport.
  let pos = $derived.by(() => {
    const w = 190, h = 170;
    return {
      left: Math.min(x, window.innerWidth - w - 8),
      top: Math.min(y, window.innerHeight - h - 8),
    };
  });

  function handleWindowPointer(e: PointerEvent) {
    if (menuEl && !menuEl.contains(e.target as Node)) onclose();
  }
  function handleWindowKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.stopPropagation();
      onclose();
    }
  }

  function resume() {
    SessionService.ResumeInTerminal(session.sessionId).catch(() => {});
    onclose();
  }
  function openEditor() {
    SessionService.OpenInEditor(session.cwd, session.agent).catch(() => {});
    onclose();
  }
  function rename() {
    onrename(session.sessionId);
    onclose();
  }
  function copyResume() {
    if (resumeCommand) navigator.clipboard.writeText(resumeCommand).catch(() => {});
    onclose();
  }
  function copyPath() {
    navigator.clipboard.writeText(session.cwd).catch(() => {});
    onclose();
  }
</script>

<svelte:window onpointerdowncapture={handleWindowPointer} onkeydowncapture={handleWindowKey} />

<div
  bind:this={menuEl}
  class="fixed z-50 w-[190px] rounded-lg border border-surface-active bg-surface shadow-[0_10px_36px_rgba(0,0,0,0.6)] p-1 text-[12px] text-text"
  style="left: {pos.left}px; top: {pos.top}px;"
  role="menu"
>
  <button class="w-full text-left rounded-md px-2.5 py-1.5 hover:bg-accent hover:text-surface" onclick={resume} role="menuitem">Resume in Terminal</button>
  <button class="w-full text-left rounded-md px-2.5 py-1.5 hover:bg-accent hover:text-surface" onclick={openEditor} role="menuitem">Open in editor</button>
  <button class="w-full text-left rounded-md px-2.5 py-1.5 hover:bg-accent hover:text-surface" onclick={rename} role="menuitem">Rename…</button>
  <div class="my-1 border-t border-border"></div>
  <button class="w-full text-left rounded-md px-2.5 py-1.5 hover:bg-accent hover:text-surface" onclick={copyResume} role="menuitem">Copy resume command</button>
  <button class="w-full text-left rounded-md px-2.5 py-1.5 hover:bg-accent hover:text-surface" onclick={copyPath} role="menuitem">Copy project path</button>
</div>
