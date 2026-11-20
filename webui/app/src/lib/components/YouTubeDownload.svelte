<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import { onDestroy } from 'svelte';
  import { resolve } from '$app/paths';
  import { apiFetch } from '#lib/api.js';
  import { Button } from '@nogon/components/ui/button';

  let { endpoint }: { endpoint: string } = $props();
  let available = $state<boolean | null>(null);
  let busy = $state(false);
  let status = $state('');
  let controller: AbortController | undefined;
  const stream = $derived(`${resolve('/')}api${endpoint}`);

  onDestroy(() => controller?.abort());

  async function request(method: 'HEAD' | 'POST' | 'DELETE') {
    if (busy) return;
    busy = true;
    status = method === 'POST' ? 'Downloading video…' : '';
    controller = new AbortController();
    const signal = controller.signal;
    try {
      const response = await apiFetch(endpoint, { method, signal, redirectOnForbidden: false });
      if (method === 'HEAD' && response.status === 404) {
        available = false;
      } else if (response.ok) {
        available = method !== 'DELETE';
        status = method === 'DELETE' ? 'Downloaded video removed.' : '';
      } else {
        status = (await response.text()).trim() || 'Could not access the local video.';
      }
    } catch {
      if (!signal.aborted) status = 'Could not access the local video.';
    } finally {
      if (!signal.aborted) busy = false;
    }
  }
</script>

<details
  class="my-2 text-sm"
  ontoggle={(event) => {
    if (event.currentTarget.open && available === null) void request('HEAD');
  }}
>
  <summary class="cursor-pointer font-medium">Local YouTube video</summary>
  {#if available}
    <!-- svelte-ignore a11y_media_has_caption (This is a playback of the downloaded original.) -->
    <video
      class="my-2 max-h-[30vh] w-full"
      controls
      preload="none"
      src={stream}
      aria-label="Downloaded YouTube video"
    ></video>
    <div class="flex flex-wrap gap-2">
      <Button size="sm" variant="outline" href={stream} download="youtube.mp4"
        >Save video file</Button
      >
      <Button size="sm" variant="outline" disabled={busy} onclick={() => request('DELETE')}
        >Remove local video</Button
      >
    </div>
  {:else if available === false}
    <Button size="sm" variant="outline" disabled={busy} onclick={() => request('POST')}
      >Download video</Button
    >
  {:else if !busy}
    <Button size="sm" variant="outline" onclick={() => request('HEAD')}>Check local video</Button>
  {/if}
  {#if status}
    <p class="my-2" role="status">{status}</p>
  {/if}
</details>
