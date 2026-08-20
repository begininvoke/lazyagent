<script lang="ts">
  import { selectedId, selectedDetail } from "./stores";
  import SessionDetail from "./SessionDetail.svelte";

  let title = $derived(
    $selectedDetail
      ? $selectedDetail.customName || $selectedDetail.agentName || $selectedDetail.shortName
      : ""
  );
</script>

<div class="flex flex-col h-full border-l border-border bg-surface">
  <div class="flex items-center justify-between px-3 py-1.5 border-b border-border">
    <span class="truncate text-[11px] font-medium text-subtext">{title}</span>
    <button
      class="text-subtext hover:text-text text-[13px] leading-none px-1"
      onclick={() => ($selectedId = null)}
      title="Close detail (esc)"
    >✕</button>
  </div>
  <div class="flex-1 min-h-0 overflow-hidden">
    <SessionDetail />
  </div>
</div>
