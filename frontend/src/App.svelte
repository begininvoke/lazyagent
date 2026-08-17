<script lang="ts">
  import { onMount } from "svelte";
  import {
    selectedId,
    selectedDetail,
    windowMinutes,
    searching,
    showLimits,
    limitsRefreshToken,
    updateVersion,
    isDetached,
  } from "./lib/stores";
  import {
    loadSessions,
    loadDetail,
    cycleFilter,
    adjustWindow,
    toggleDetach,
    setSearch,
    syncDetachState,
  } from "./lib/actions";
  import PanelView from "./lib/PanelView.svelte";
  import DesktopView from "./lib/DesktopView.svelte";
  import * as SessionService from "./bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";
  import { Events } from "@wailsio/runtime";

  $effect(() => {
    const id = $selectedId;
    if (id) {
      loadDetail(id);
    } else {
      $selectedDetail = null;
    }
  });

  function handleKeydown(e: KeyboardEvent) {
    if ($searching) {
      if (e.key === "Escape") {
        e.preventDefault();
        setSearch("");
        $searching = false;
      }
      return;
    }

    if ($showLimits) {
      if (e.key === "Escape" || e.key === "l" || e.key === "L") {
        e.preventDefault();
        $showLimits = false;
      } else if (e.key === "r" || e.key === "R") {
        e.preventDefault();
        $limitsRefreshToken += 1;
      }
      return;
    }

    if (e.key === "Escape") {
      if ($selectedId !== null) {
        e.preventDefault();
        $selectedId = null;
      }
    } else if (e.key === "/") {
      e.preventDefault();
      $searching = true;
    } else if (e.key === "d") {
      e.preventDefault();
      toggleDetach();
    } else if (e.key === "f") {
      e.preventDefault();
      cycleFilter();
    } else if (e.key === "+" || e.key === "=") {
      e.preventDefault();
      adjustWindow(10);
    } else if (e.key === "-") {
      e.preventDefault();
      adjustWindow(-10);
    } else if (e.key === "l" || e.key === "L") {
      e.preventDefault();
      $showLimits = true;
    }
  }

  onMount(() => {
    loadSessions();

    SessionService.GetWindowMinutes().then((m) => {
      $windowMinutes = m;
    }).catch(() => {});

    Events.On("sessions:updated", () => {
      loadSessions();
      if ($selectedId) loadDetail($selectedId);
    });

    syncDetachState();

    Events.On("detach:changed", () => {
      syncDetachState();
    });

    // Check for updates after a short delay (gives the backend time to fetch)
    setTimeout(async () => {
      try {
        const v = await SessionService.GetUpdateVersion();
        if (v) $updateVersion = v;
      } catch {}
    }, 3000);
  });
</script>

<svelte:window onkeydown={handleKeydown} />

{#if $isDetached}
  <DesktopView />
{:else}
  <PanelView />
{/if}
