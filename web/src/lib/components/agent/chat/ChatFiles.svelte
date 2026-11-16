<script lang="ts">
	// The files the agent's steps wrote in this chat, each a card with "In
	// <App> öffnen" when a web app is set for its type (0.95) and "Im
	// Dateimanager öffnen".
	import { onMount } from 'svelte';
	import { appForFile, loadWebApps, webAppById } from '#lib/webapps/webapps.svelte.ts';
	import { openInWebApp } from '#lib/webapps/open.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Entry } from '#lib/agent.ts';

	let { entries }: { entries: Entry[] } = $props();

	// The web app set for a file type, if one is installed (0.95): a
	// document the agent wrote opens there with one tap.
	onMount(() => void loadWebApps().catch(() => {}));
	function fileApp(name: string): { key: string; name: string } | null {
		const key = appForFile(name);
		if (!key?.startsWith('webapp:')) return null;
		const app = webAppById(key.slice(7));
		return app ? { key, name: app.name } : null;
	}
</script>

{#each entries.filter((e) => e.role === 'tool' && e.files?.length) as step (step.id)}
	{#each step.files ?? [] as file (file.root + file.path)}
		{@const name = file.path.split('/').pop() ?? ''}
		{@const app = fileApp(name)}
		<div class="file">
			<span class="doc" aria-hidden="true">{file.path.split('.').pop()?.toUpperCase()}</span>
			<span class="meta">
				<strong>{name}</strong>
				<span class="muted">{file.root.replace(/^share\//, '')} › {file.path}</span>
			</span>
			<span class="file-acts">
				<!-- 0.95: straight into the app of the file type (T-Word, T-Excel …). -->
				{#if app}
					<button
						type="button"
						class="btn btn-primary"
						onclick={() => openInWebApp(app.key, file.root, file.path)}
						>{t('agent.openInApp', { app: app.name })}</button
					>
				{/if}
				<a
					class="btn"
					href={`/files?root=${encodeURIComponent(file.root)}&path=${encodeURIComponent(file.path.split('/').slice(0, -1).join('/'))}`}
					>{t('agent.openInFiles')}</a
				>
			</span>
		</div>
	{/each}
{/each}

<style>
	.file {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
		text-decoration: none;
		color: var(--text);
	}
	.file-acts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.file-acts .btn {
		text-decoration: none;
	}
	.file .doc {
		width: 36px;
		height: 42px;
		border-radius: var(--radius-small);
		display: grid;
		place-items: center;
		background: var(--chip);
		color: var(--text);
		font-weight: 700;
		font-size: var(--text-xs);
		flex: none;
	}
	.file .meta {
		display: flex;
		flex-direction: column;
		min-width: 0;
		flex: 1 1 12em;
	}
	.file .meta .muted {
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
</style>
