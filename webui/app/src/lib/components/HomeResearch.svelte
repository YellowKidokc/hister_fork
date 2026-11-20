<script lang="ts">
  import { onMount } from 'svelte';
  import { resolve } from '$app/paths';
  import { apiFetch } from '#lib/api.js';
  import { Tag, Link2, FolderUp, Video } from '@lucide/svelte';

  interface Props {
    canWrite: boolean;
    onSearch: (query: string) => void;
  }

  let { canWrite, onSearch }: Props = $props();

  let groups: { term: string; count: number }[] = $state([]);
  let groupsLoaded = $state(false);

  const colors = [
    'border-nogon-indigo text-nogon-indigo bg-nogon-indigo/10',
    'border-nogon-teal text-nogon-teal bg-nogon-teal/10',
    'border-nogon-coral text-nogon-coral bg-nogon-coral/10',
    'border-nogon-amber text-nogon-amber bg-nogon-amber/10',
  ];

  // The research page is served by the Go server, outside the SvelteKit app.
  const researchPage = `${resolve('/')}research`;

  function labelQuery(label: string): string {
    return /^[\w.-]+$/.test(label) ? `label:${label}` : `label:"${label.replaceAll('"', '')}"`;
  }

  onMount(async () => {
    try {
      const res = await apiFetch('/facets?q=*&size_labels=60', {
        headers: { Accept: 'application/json' },
        redirectOnForbidden: false,
      });
      if (res.ok) {
        const facets = await res.json();
        groups = (facets?.terms?.labels?.terms ?? []).filter(
          (t: { term: string }) => t.term && t.term.trim() !== '',
        );
      }
    } catch (e) {
      console.log('Failed to load groups', e);
    }
    groupsLoaded = true;
  });

  const sources = [
    {
      title: 'Upload a list of links',
      detail: 'Paste links or a .txt, .csv or Excel list',
      href: `${researchPage}#linkimport`,
      icon: Link2,
      color: 'text-nogon-indigo border-nogon-indigo bg-nogon-indigo/5 hover:bg-nogon-indigo/10',
    },
    {
      title: 'Upload files or a folder',
      detail: 'PDFs, Markdown, Word, HTML, code',
      href: `${researchPage}#newjob`,
      icon: FolderUp,
      color: 'text-nogon-teal border-nogon-teal bg-nogon-teal/5 hover:bg-nogon-teal/10',
    },
    {
      title: 'YouTube transcripts',
      detail: 'Channels and playlists via the YouTube scripts',
      href: `${researchPage}#youtube`,
      icon: Video,
      color: 'text-nogon-coral border-nogon-coral bg-nogon-coral/5 hover:bg-nogon-coral/10',
    },
  ];
</script>

{#if groupsLoaded && groups.length > 0}
  <section
    class="flex w-full max-w-[900px] shrink-0 flex-col gap-2"
    aria-labelledby="home-groups-title"
  >
    <h2
      id="home-groups-title"
      class="font-inter text-text-brand-secondary flex items-center gap-2 text-sm font-semibold"
    >
      <Tag class="text-nogon-indigo size-4" />
      Groups
    </h2>
    <div class="flex flex-wrap items-center justify-center gap-2">
      {#each groups as group, i}
        <button
          type="button"
          class="font-inter flex max-w-80 min-w-0 cursor-pointer items-center gap-2 border-[2px] px-3 py-1.5 text-sm font-semibold shadow-[2px_2px_0_var(--brutal-shadow)] transition-colors hover:brightness-110 {colors[
            i % colors.length
          ]}"
          title={`Show everything labelled ${group.term}`}
          onclick={() => onSearch(labelQuery(group.term))}
        >
          <span class="truncate">{group.term}</span>
          <span class="text-text-brand-muted text-xs tabular-nums"
            >{group.count.toLocaleString()}</span
          >
        </button>
      {/each}
    </div>
  </section>
{/if}

{#if canWrite}
  <section
    class="border-brutal-border bg-card-surface w-full max-w-[900px] shrink-0 border-[3px] shadow-[4px_4px_0_var(--brutal-shadow)]"
    aria-labelledby="home-research-title"
  >
    <div class="border-brutal-border border-b-[3px] px-4 py-3 md:px-5">
      <h2 id="home-research-title" class="font-outfit text-text-brand text-lg font-extrabold">
        Start a research job
      </h2>
      <p class="font-inter text-text-brand-secondary text-sm">
        Bring new material in. Give it a label and it shows up as a group above.
      </p>
    </div>
    <div class="grid gap-3 p-4 md:grid-cols-2 md:p-5">
      {#each sources as source}
        <a
          href={source.href}
          data-sveltekit-reload
          class="flex items-center gap-3 border-[2px] px-3 py-3 transition-colors {source.color}"
        >
          <source.icon class="size-5 shrink-0" />
          <span class="min-w-0">
            <span class="font-outfit text-text-brand block font-bold">{source.title}</span>
            <span class="font-inter text-text-brand-muted block text-xs">{source.detail}</span>
          </span>
        </a>
      {/each}
    </div>
  </section>
{/if}
