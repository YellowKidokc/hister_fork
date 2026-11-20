<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import { onDestroy } from 'svelte';
  import { apiFetch } from '#lib/api.js';
  import { Button } from '@nogon/components/ui/button';

  let { endpoint }: { endpoint: string } = $props();
  let imageURL = $state('');
  let busy = $state(false);
  let status = $state('');
  let controller: AbortController | undefined;

  function clearImage() {
    if (imageURL) URL.revokeObjectURL(imageURL);
    imageURL = '';
  }

  onDestroy(() => {
    controller?.abort();
    clearImage();
  });

  async function request(method: 'GET' | 'DELETE') {
    if (busy) return;
    busy = true;
    status = '';
    controller = new AbortController();
    const signal = controller.signal;
    try {
      const response = await apiFetch(endpoint, { method, signal, redirectOnForbidden: false });
      if (response.status === 404 && method === 'GET') {
        status = 'Use the browser extension to save a screenshot of this page.';
        return;
      }
      if (!response.ok) throw new Error();
      clearImage();
      if (method === 'GET') {
        const blob = await response.blob();
        if (!signal.aborted) imageURL = URL.createObjectURL(blob);
      } else {
        status = 'Visual archive deleted.';
      }
    } catch {
      if (!signal.aborted) status = 'Could not access the visual archive.';
    } finally {
      if (!signal.aborted) busy = false;
    }
  }
</script>

<details
  class="my-2 text-sm"
  ontoggle={(event) => {
    if (event.currentTarget.open && !imageURL) void request('GET');
  }}
>
  <summary class="cursor-pointer font-medium">Visual archive</summary>
  {#if imageURL}
    <div class="my-2 flex flex-wrap gap-2">
      <Button size="sm" variant="outline" href={imageURL} target="_blank" rel="noopener noreferrer"
        >Open saved screenshot</Button
      >
      <Button size="sm" variant="outline" href={imageURL} download="nogon-visual-archive.jpg"
        >Download screenshot</Button
      >
      <Button size="sm" variant="outline" disabled={busy} onclick={() => request('DELETE')}
        >Delete screenshot</Button
      >
    </div>
    <img
      class="mt-1 max-h-[30vh] max-w-full rounded object-contain"
      src={imageURL}
      alt="Screenshot saved from this page"
    />
  {/if}
  {#if busy || status}
    <p class="my-2" role="status">{busy ? 'Loading visual archive…' : status}</p>
  {/if}
</details>
