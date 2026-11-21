<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import { apiFetch } from '#lib/api.js';
  import { formatTimestamp } from '#lib/search.js';
  import { Button } from '@nogon/components/ui/button';

  interface Visit {
    id: number;
    visited_at: string;
  }

  let { endpoint }: { endpoint: string } = $props();
  let visits = $state<Visit[]>([]);
  let total = $state<number | null>(null);
  let busy = $state(false);
  let error = $state('');

  async function load() {
    if (busy) return;
    error = '';
    busy = true;
    try {
      const response = await apiFetch(`${endpoint}&limit=30&offset=${visits.length}`, {
        redirectOnForbidden: false,
      });
      if (!response.ok) throw new Error();
      const page: { total: number; visits: Visit[] } = await response.json();
      visits = [...visits, ...page.visits];
      total = page.total;
    } catch {
      error = 'Could not load the visit timeline.';
    } finally {
      busy = false;
    }
  }
</script>

<details
  class="my-2 text-sm"
  ontoggle={(event) => {
    if (event.currentTarget.open && total === null) void load();
  }}
>
  <summary class="cursor-pointer font-medium">Visit timeline</summary>
  {#if total === 0}
    <p class="my-2">No dated visits have been recorded for this page.</p>
  {/if}
  {#if visits.length}
    <ol class="border-border-brand-muted my-2 ml-1 grid gap-1 border-l pl-3">
      {#each visits as visit (visit.id)}
        <li>
          <time datetime={visit.visited_at}
            >{formatTimestamp(Date.parse(visit.visited_at) / 1000)}</time
          >
        </li>
      {/each}
    </ol>
  {/if}
  {#if busy}
    <p role="status">Loading visits…</p>
  {:else if error}
    <p role="status">{error}</p>
    <Button type="button" size="sm" onclick={load}>Retry</Button>
  {:else if total !== null && visits.length < total}
    <Button type="button" size="sm" onclick={load}>Load older visits</Button>
  {/if}
</details>
