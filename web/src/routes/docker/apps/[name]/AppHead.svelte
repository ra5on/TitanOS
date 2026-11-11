<script lang="ts">
	// The head of the app's page: state and load of its containers, links to
	// the app, and restart, logs, console and the tailnet for it.
	import Icon from '#lib/components/Icon.svelte';
	import { dockerName } from '#lib/docker-name.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { addContainerConsole } from '#lib/terminal.svelte.ts';
	import { isRunning } from '../../container.ts';
	import type { AppEditor } from './app-editor.svelte.ts';

	let { ed }: { ed: AppEditor } = $props();
</script>

<section class="tile app-head" aria-label={t('editor.appState')}>
	<div class="app-state">
		{#if ed.members.length}
			<span class="state"
				><span
					class="dot"
					class:ok={ed.runningCount === ed.members.length}
					class:warn={ed.runningCount > 0 && ed.runningCount < ed.members.length}
					aria-hidden="true"
				></span>{t('docker.stackSummary', {
					running: ed.runningCount,
					total: ed.members.length,
				})}</span
			>
			{#if ed.runningCount}
				<span class="mono value"
					><span class="muted">{t('docker.cpu')}</span>
					{formatPercent(
						ed.members.filter(isRunning).reduce((sum, container) => sum + container.cpuPercent, 0),
					)}</span
				>
				<span class="mono value"
					><span class="muted">{t('docker.ram')}</span>
					{formatBytes(
						ed.members.filter(isRunning).reduce((sum, container) => sum + container.memoryBytes, 0),
					)}</span
				>
			{/if}
		{:else if ed.status}
			<span class="muted">{t('editor.noContainers')}</span>
		{:else if !ed.statusError}
			<span class="muted" role="status">{t('common.loading')}</span>
		{/if}
	</div>
	<div class="quick">
		{#each ed.quickLinks as link (link.label)}<a
				class="btn small"
				href={link.href}
				target="_blank"
				rel="noreferrer"
				title={link.label}><Icon name={link.icon} size={14} />{link.text}</a
			>{/each}
		<button
			type="button"
			class="btn small"
			disabled={!ed.target}
			onclick={() => ed.target && ed.showLogs(ed.target)}
			><Icon name="fileText" size={14} />{t('docker.logs')}</button
		>
		<button
			type="button"
			class="btn small"
			disabled={!ed.target || !isRunning(ed.target)}
			onclick={() =>
				ed.target && addContainerConsole(ed.target.id, dockerName(ed.target, 'distinct'))}
			><Icon name="terminal" size={14} />{t('docker.console')}</button
		>
		<button
			type="button"
			class="btn small"
			disabled={!ed.members.length || ed.restarting || ed.applying}
			onclick={ed.restartAll}
			><Icon name="restart" size={14} />{t(
				ed.restarting ? 'editor.restarting' : 'docker.restart',
			)}</button
		>
		{#if ed.proxy}<button
				type="button"
				class="btn small"
				onclick={() => (ed.tailnetDialog = ed.published ? 'remove' : 'publish')}
				><Icon name="tailnet" size={14} />{t(
					ed.published ? 'docker.tailnetUnpublish' : 'docker.tailnetPublish',
				)}</button
			>{/if}
	</div>
	{#if ed.statusError}<div class="notice-warn row-note" role="alert">
			<span>{ed.statusError}</span><button type="button" class="btn small" onclick={ed.loadStatus}
				>{t('common.retry')}</button
			>
		</div>{/if}
	{#if ed.actionError}<div class="notice-error row-note" role="alert">
			<span>{ed.actionError}</span><button
				type="button"
				class="btn small"
				disabled={ed.restarting}
				onclick={ed.restartAll}>{t('common.retry')}</button
			>
		</div>{/if}
</section>

<style>
	.app-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2-5) var(--space-4);
		padding: var(--space-3) 18px;
	}
	.app-state,
	.quick {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-4);
		font-size: var(--text-md);
	}
	.quick {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-4);
		font-size: var(--text-md);
	}
	.state {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
	}
	.value {
		font-size: var(--text-base);
	}
	.row-note {
		flex: 1 1 100%;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}
</style>
