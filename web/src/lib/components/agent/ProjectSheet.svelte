<script lang="ts">
	// A project of the KI-Agent on the phone (PLAN 44, Bild 8): its chats,
	// its instructions, the files the agent made in it and what it
	// remembered for it.
	import Dialog from '#lib/components/Dialog.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import { api } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { ChatInfo, Memory, Project } from '#lib/agent.ts';

	let {
		project,
		chats,
		onopen,
		onedit,
		onnew,
		onclose,
	}: {
		project: Project;
		chats: ChatInfo[];
		onopen: (id: string) => void;
		onedit: () => void;
		onnew: () => void;
		onclose: () => void;
	} = $props();

	type File = { root: string; path: string; chat: string; at: string };
	let tab = $state<'chats' | 'instructions' | 'files' | 'memory'>('chats');
	let files = $state<File[] | null>(null);
	let memory = $state<Memory[] | null>(null);
	const list = $derived(chats.filter((c) => c.project === project.id));

	$effect(() => {
		if (tab === 'files' && files === null)
			void api
				.get<{ files: File[] }>(`/agent/projects/${project.id}/files`)
				.then((r) => (files = r.files))
				.catch(() => (files = []));
		if (tab === 'memory' && memory === null)
			void api
				.get<{ memory: Memory[] }>('/agent/memory')
				.then((r) => (memory = r.memory.filter((m) => m.project === project.id)))
				.catch(() => (memory = []));
	});
	const folder = (p: string) => p.split('/').slice(0, -1).join('/');
</script>

<Dialog title={(project.icon ? project.icon + ' ' : '') + project.name} {onclose}>
	<Tabs
		label={project.name}
		value={tab}
		onchange={(id) => (tab = id)}
		tabs={[
			{ id: 'chats', label: t('agent.chats'), badge: list.length || '' },
			{ id: 'instructions', label: t('agent.sheetInstructions') },
			{ id: 'files', label: t('agent.sheetFiles') },
			{ id: 'memory', label: t('agent.sheetMemory') },
		]}
	/>
	{#if tab === 'chats'}
		<ul class="sheet-list">
			{#each list as c (c.id)}
				<li>
					<button type="button" class="row-btn" onclick={() => onopen(c.id)}
						><strong>{c.title || t('agent.newChat')}</strong><span class="muted"
							>{c.preview ?? ''}</span
						></button
					>
				</li>
			{:else}
				<li class="muted">{t('agent.projectEmpty')}</li>
			{/each}
		</ul>
	{:else if tab === 'instructions'}
		{#if project.instructions}<p class="pre">{project.instructions}</p>{:else}<p class="muted">
				{t('agent.sheetNoInstructions')}
			</p>{/if}
		{#if project.location}<p class="muted">
				{t('agent.projectLocation')}: {project.location}
			</p>{/if}
	{:else if tab === 'files'}
		<ul class="sheet-list">
			{#each files ?? [] as f (f.root + f.path)}
				<li>
					<a
						class="row-btn"
						href={`/files?root=${encodeURIComponent(f.root)}&path=${encodeURIComponent(folder(f.path))}`}
						><strong>{f.path.split('/').pop()}</strong><span class="muted"
							>{f.root.replace(/^share\//, '')} › {f.path} · {formatDateTime(new Date(f.at))}</span
						></a
					>
				</li>
			{:else}
				<li class="muted">{files === null ? t('agent.loading') : t('agent.sheetNoFiles')}</li>
			{/each}
		</ul>
	{:else}
		<ul class="sheet-list">
			{#each memory ?? [] as m (m.id)}
				<li class="mem">{m.text}</li>
			{:else}
				<li class="muted">{memory === null ? t('agent.loading') : t('agent.memoryEmpty')}</li>
			{/each}
		</ul>
	{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onedit}>{t('agent.projectEdit')}</button>
		<button type="button" class="btn btn-primary" onclick={onnew}
			>{t('agent.newChatIn', { name: project.name })}</button
		>
	</div>
</Dialog>

<style>
	.sheet-list {
		list-style: none;
		padding: 0;
		margin: var(--space-3) 0 0;
		display: flex;
		flex-direction: column;
		max-height: 50vh;
		overflow-y: auto;
	}
	.row-btn {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		width: 100%;
		padding: 10px 0;
		border: 0;
		border-bottom: 1px solid var(--border);
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
		text-decoration: none;
	}
	.row-btn .muted {
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.mem {
		padding: 8px 0;
		border-bottom: 1px solid var(--border);
	}
	.pre {
		white-space: pre-wrap;
		margin: var(--space-3) 0;
	}
	.dialog-actions {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
		margin-top: var(--space-3);
	}
</style>
