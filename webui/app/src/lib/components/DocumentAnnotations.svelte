<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import { apiFetch } from '#lib/api.js';
  import { Button } from '@nogon/components/ui/button';

  let { endpoint, summaryEndpoint }: { endpoint: string; summaryEndpoint?: string } = $props();

  let loaded = $state(false);
  let busy = $state(false);
  let tags = $state('');
  let notes = $state('');
  let summary = $state('');
  let status = $state('');

  async function load() {
    if (loaded || busy) return;
    busy = true;
    status = '';
    try {
      const response = await apiFetch(endpoint, { redirectOnForbidden: false });
      if (!response.ok) throw new Error();
      const annotation = await response.json();
      tags = (annotation.tags ?? []).join(', ');
      notes = annotation.notes;
      summary = annotation.summary;
      loaded = true;
    } catch {
      status = 'Could not load annotations.';
    } finally {
      busy = false;
    }
  }

  async function generateSummary() {
    if (!summaryEndpoint || busy) return;
    busy = true;
    status = 'Generating a summary…';
    try {
      const response = await apiFetch(summaryEndpoint, {
        method: 'POST',
        redirectOnForbidden: false,
      });
      if (!response.ok) {
        status = (await response.text()).trim() || 'Could not generate a summary.';
        return;
      }
      const draft = await response.json();
      summary = draft.summary;
      status = 'Draft generated. Save annotations to keep it.';
    } catch {
      status = 'Could not generate a summary.';
    } finally {
      busy = false;
    }
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    busy = true;
    status = '';
    try {
      const response = await apiFetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        redirectOnForbidden: false,
        body: JSON.stringify({ tags: tags.split(',').filter((tag) => tag.trim()), notes, summary }),
      });
      if (!response.ok) {
        status =
          response.status === 400 ? (await response.text()).trim() : 'Could not save annotations.';
        return;
      }
      const annotation = await response.json();
      tags = annotation.tags.join(', ');
      status = 'Annotations saved.';
    } catch {
      status = 'Could not save annotations.';
    } finally {
      busy = false;
    }
  }
</script>

<details
  class="border-border-brand-muted my-2 rounded border p-2"
  ontoggle={(event) => {
    if (event.currentTarget.open) void load();
  }}
>
  <summary class="cursor-pointer text-sm font-medium">Tags, notes, and summary</summary>
  {#if loaded}
    <form class="mt-2 grid gap-2 text-sm" onsubmit={save}>
      <label class="grid gap-1">
        Tags (comma-separated)
        <input
          class="border-border-brand-muted rounded border p-2"
          bind:value={tags}
          disabled={busy}
        />
      </label>
      <label class="grid gap-1">
        Notes
        <textarea
          class="border-border-brand-muted rounded border p-2"
          rows="3"
          bind:value={notes}
          disabled={busy}></textarea>
      </label>
      <label class="grid gap-1">
        Summary
        <textarea
          class="border-border-brand-muted rounded border p-2"
          rows="3"
          bind:value={summary}
          disabled={busy}></textarea>
      </label>
      {#if summaryEndpoint}
        <Button
          type="button"
          size="sm"
          variant="outline"
          class="justify-self-start"
          disabled={busy}
          onclick={generateSummary}
        >
          Generate summary with AI
        </Button>
      {/if}
      <Button type="submit" size="sm" class="justify-self-start" disabled={busy}
        >Save annotations</Button
      >
    </form>
  {:else if busy}
    <p class="text-sm">Loading annotations…</p>
  {:else if status}
    <Button type="button" size="sm" onclick={load}>Retry</Button>
  {/if}
  {#if status}
    <p class="mt-2 text-sm" role="status">{status}</p>
  {/if}
</details>
