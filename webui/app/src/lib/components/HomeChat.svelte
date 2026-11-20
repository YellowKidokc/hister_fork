<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import { onDestroy } from 'svelte';
  import { Sparkles } from '@lucide/svelte';
  import { Button } from '@nogon/components/ui/button';
  import { Input } from '@nogon/components/ui/input';
  import { apiFetch } from '#lib/api.js';
  import { buildPreviewUrl } from '#lib/preview.svelte.js';
  import { publicConfig } from '#lib/stores.js';

  interface Source {
    id: string;
    title: string;
    url: string;
  }

  interface Turn {
    question: string;
    answer: string;
    sources: Source[];
  }

  let question = $state('');
  let search = $state('');
  let turns = $state<Turn[]>([]);
  let pending = $state(false);
  let problem = $state('');
  let controller: AbortController | undefined;

  onDestroy(() => controller?.abort());

  async function send(event: SubmitEvent) {
    event.preventDefault();
    if (pending || !question.trim()) return;

    const trimmed = question.trim();
    const messages = [
      ...turns.slice(-4).flatMap((turn) => [
        { role: 'user', content: turn.question },
        { role: 'assistant', content: turn.answer },
      ]),
      { role: 'user', content: trimmed },
    ];
    problem = '';
    pending = true;
    controller = new AbortController();
    const signal = controller.signal;
    try {
      const response = await apiFetch('/ai/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ messages, search: search.trim() }),
        signal,
        redirectOnForbidden: false,
      });
      if (!response.ok) {
        problem = (await response.text()).trim() || 'Could not get an answer.';
        return;
      }
      const result: { answer: string; sources: Source[] } = await response.json();
      if (signal.aborted) return;
      turns = [...turns, { question: trimmed, ...result }];
      question = '';
    } catch {
      if (!signal.aborted) problem = 'Could not get an answer.';
    } finally {
      if (!signal.aborted) pending = false;
    }
  }

  function reset() {
    controller?.abort();
    turns = [];
    question = '';
    pending = false;
    problem = '';
  }
</script>

{#if $publicConfig?.aiSummaryEnabled && $publicConfig.canWrite}
  <section
    class="border-brutal-border bg-card-surface w-full max-w-[900px] shrink-0 border-[3px] shadow-[4px_4px_0_var(--brutal-shadow)]"
    aria-labelledby="home-chat-title"
  >
    <div
      class="border-brutal-border flex items-center justify-between gap-2 border-b-[3px] px-4 py-3 md:px-5"
    >
      <h2 id="home-chat-title" class="font-outfit flex items-center gap-2 text-lg font-extrabold">
        <Sparkles class="text-nogon-indigo size-4" aria-hidden="true" />
        Ask AI
      </h2>
      {#if turns.length > 0 || pending}
        <Button size="sm" variant="ghost" onclick={reset}>Clear chat</Button>
      {/if}
    </div>
    {#if turns.length > 0}
      <ol
        class="max-h-80 space-y-4 overflow-y-auto px-4 pt-4 text-sm md:px-5"
        aria-label="Conversation"
        aria-live="polite"
      >
        {#each turns as turn}
          <li>
            <p class="font-semibold break-words whitespace-pre-wrap">{turn.question}</p>
            <p class="mt-1 break-words whitespace-pre-wrap">{turn.answer}</p>
            {#if turn.sources.length > 0}
              <ol class="mt-2 list-decimal space-y-1 pl-5" aria-label="Indexed sources">
                {#each turn.sources as source}
                  <li class="break-words">
                    <a
                      class="text-nogon-indigo underline"
                      href={buildPreviewUrl(source.url, source.title, undefined, source.id)}
                    >
                      {source.title || source.url}
                    </a>
                  </li>
                {/each}
              </ol>
            {/if}
          </li>
        {/each}
      </ol>
    {/if}
    <form class="grid gap-3 p-4 text-sm md:p-5" onsubmit={send}>
      <label class="grid gap-1">
        Question
        <textarea
          class="border-border-brand-muted rounded border p-2"
          bind:value={question}
          rows="2"
          maxlength="2000"
          disabled={pending}
          required></textarea>
      </label>
      <label class="grid gap-1">
        Search terms for indexed sources (optional)
        <Input bind:value={search} maxlength="128" disabled={pending} />
      </label>
      <p class="text-text-brand-muted">
        Sending shares the question and any matching excerpts with your configured AI service.
      </p>
      <Button
        class="justify-self-start"
        size="sm"
        type="submit"
        disabled={pending || !question.trim()}
      >
        {pending ? 'Waiting for an answer…' : 'Send question'}
      </Button>
      {#if problem}<p role="status">{problem}</p>{/if}
    </form>
  </section>
{/if}
