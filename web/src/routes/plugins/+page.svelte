<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';

	type Plugin = {
		name: string;
		title: string;
		description: Record<string, string> | null;
		version: string;
		size: number;
		kernel: string;
		contains: Record<string, string[]> | null;
		modules: string[] | null;
		services: string[] | null;
		installed: boolean;
		enabled: boolean;
		active: boolean;
		path: string;
		official: boolean;
		problem: string;
	};
	type Status = { volume: string; kernel: string; plugins: Plugin[] };

	let status = $state<Status | null>(null);
	let error = $state<{ text: string; detail: string } | null>(null);
	let picked = $state('');
	let asking = $state(false);
	let confirm = $state('');
	let volume = $state('');
	let busy = $state(false);
	let removing = $state(false);

	const plugins = $derived(status?.plugins ?? []);
	const current = $derived(plugins.find((plugin) => plugin.name === picked) ?? plugins[0]);
	const volumes = $derived((storage.status?.volumes ?? []).filter((entry) => entry.mounted).map((entry) => entry.name));
	const text = (texts: Record<string, string> | null) => texts?.[i18n.locale] ?? texts?.en ?? Object.values(texts ?? {})[0] ?? '';
	const contains = (plugin: Plugin) => plugin.contains?.[i18n.locale] ?? plugin.contains?.en ?? [];
	const monogram = (plugin: Plugin) => plugin.title.replace(/[^a-zA-Z0-9]/g, '').slice(0, 2).replace(/^./, (c) => c.toUpperCase()) || '?';
	const hue = (plugin: Plugin) => [...plugin.name].reduce((sum, char) => (sum * 31 + char.charCodeAt(0)) % 360, 7);

	function badge(plugin: Plugin): { text: string; kind: string } {
		if (!plugin.installed) return { text: t('plugins.available'), kind: '' };
		if (plugin.problem) return { text: t('plugins.notLoaded'), kind: 'warn' };
		if (plugin.active) return { text: t('plugins.active'), kind: 'ok' };
		return { text: t(plugin.enabled ? 'plugins.waiting' : 'plugins.off'), kind: '' };
	}

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: (code === 'plugin_failed' || code === 'invalid_plugin') && err instanceof ApiError ? err.message : '' };
	}

	async function load() {
		if (document.hidden || busy) return;
		try {
			status = await api.get<Status>('/plugins');
		} catch (err) {
			if (!status) problem(err);
		}
	}
	$effect(() => {
		load();
		const timer = setInterval(load, 5000);
		return () => clearInterval(timer);
	});

	async function run(action: () => Promise<Status>) {
		busy = true;
		error = null;
		try {
			status = await action();
			asking = false;
			removing = false;
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	function ask() {
		confirm = '';
		volume = status?.volume || volumes[0] || '';
		error = null;
		asking = true;
	}

	function pick(name: string) {
		picked = name;
		asking = false;
		error = null;
	}
</script>

<div class="title">
	<h1>{t('plugins.title')}</h1>
	<p class="muted intro">{t('plugins.intro')} <a href="/apps">{t('nav.apps')}</a></p>
</div>

{#if error && !asking}
	<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>
{/if}

{#if status && plugins.length === 0}
	<section class="tile empty">
		<h2>{t('plugins.emptyTitle')}</h2>
		<p class="muted">{t('plugins.emptyText')}</p>
		<p class="muted">{t('plugins.emptyOfficial')}</p>
	</section>
{:else if status && current}
	<div class="layout">
		<div class="list">
			{#each plugins as plugin (plugin.name)}
				{@const state = badge(plugin)}
				<button type="button" class="tile card" aria-pressed={plugin.name === current.name} onclick={() => pick(plugin.name)}>
					<span class="monogram" style:background="hsl({hue(plugin)} 45% 42%)" aria-hidden="true">{monogram(plugin)}</span>
					<span class="about">
						<span class="name-row"><span class="name">{plugin.title}</span><span class="muted source">{t(plugin.official ? 'plugins.official' : 'plugins.community')}</span></span>
						<span class="description">{text(plugin.description)}</span>
					</span>
					<span class="chip {state.kind}">{state.text}</span>
				</button>
			{/each}
			<p class="muted note">{status.volume ? t('plugins.where', { volume: status.volume }) : t('plugins.whereNone')}</p>
		</div>

		<aside class="tile" aria-labelledby="plugin-title">
			<div class="head">
				<span class="monogram large" style:background="hsl({hue(current)} 45% 42%)" aria-hidden="true">{monogram(current)}</span>
				<div class="about">
					<h2 id="plugin-title">{current.title}</h2>
					<span class="description">{text(current.description)}</span>
					<span class="muted mono meta">{current.name} · {current.version || '–'} · {formatBytes(current.size)} · {t(current.official ? 'plugins.official' : 'plugins.community')}</span>
				</div>
			</div>

			<div class="body">
				{#if contains(current).length > 0}
					<div class="block">
						<h3>{t('plugins.contains')}</h3>
						<div class="chips">{#each contains(current) as entry (entry)}<span class="chip">{entry}</span>{/each}</div>
					</div>
				{/if}
				<div class="block">
					<h3>{t('plugins.does')}</h3>
					<ul>
						<li>{t('plugins.doesFiles')}</li>
						{#if current.modules?.length}<li>{t('plugins.doesModules')} <span class="mono">{current.modules.join(', ')}</span></li>{/if}
						{#if current.services?.length}<li>{t('plugins.doesServices')} <span class="mono">{current.services.join(', ')}</span></li>{/if}
						{#if current.kernel}<li>{t('plugins.forKernel', { kernel: current.kernel })}</li>{/if}
					</ul>
				</div>

				{#if current.problem === 'kernel'}
					<p class="notice-warn" role="status">{t('plugins.kernelMismatch', { built: current.kernel, running: status.kernel })}</p>
				{/if}

				{#if !current.installed && !asking}
					<div class="row">
						<span class="muted mono path">{current.path}</span>
						<button type="button" class="btn btn-primary" onclick={ask}>{t('plugins.install')}</button>
					</div>
				{:else if !current.installed}
					<form class="confirm" onsubmit={(event) => (event.preventDefault(), run(() => api.post<Status>('/plugins', { path: current.path, volume, confirm })))}>
						<p class="notice-warn">{t(current.official ? 'plugins.trustOfficial' : 'plugins.trustCommunity')}</p>
						{#if !status.volume}
							<label><span>{t('plugins.volume')}</span>
								<select class="field" bind:value={volume}>{#each volumes as entry (entry)}<option value={entry}>{entry}</option>{/each}</select>
							</label>
						{/if}
						{#if !current.official}
							<label><span>{t('plugins.confirmName', { name: current.name })}</span>
								<input class="field mono" type="text" bind:value={confirm} autocapitalize="none" spellcheck="false" />
							</label>
						{/if}
						{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}
						<div class="row">
							<button type="button" class="btn" onclick={() => (asking = false)}>{t('common.cancel')}</button>
							<button type="submit" class="btn btn-primary" disabled={busy || !volume || (!current.official && confirm !== current.name)}>{t(busy ? 'plugins.installing' : 'plugins.allow')}</button>
						</div>
					</form>
				{:else}
					<div class="row">
						<label class="check"><input type="checkbox" checked={current.enabled} disabled={busy} onchange={(event) => run(() => api.post<Status>(`/plugins/${current.name}/${event.currentTarget.checked ? 'enable' : 'disable'}`))} /><span>{t('plugins.enabled')}</span></label>
						<button type="button" class="btn" onclick={() => (removing = true)} disabled={busy}>{t('plugins.remove')}</button>
					</div>
					<p class="muted note">{t('plugins.enabledNote')}</p>
				{/if}
			</div>
		</aside>
	</div>
{/if}

{#if removing && current}
	<Dialog title={t('plugins.removeTitle', { name: current.title })} onclose={() => (removing = false)}>
		<p class="text">{t('plugins.removeText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = false)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" onclick={() => run(() => api.delete<Status>(`/plugins/${current.name}`))} disabled={busy}>{t('plugins.removeSubmit')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.title {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
	.intro {
		max-width: 60rem;
		font-size: 0.875rem;
	}
	.empty {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 32px;
		font-size: 0.875rem;
	}
	.empty h2 {
		font-size: 1.0625rem;
		font-weight: 600;
	}
	.empty p {
		max-width: 52rem;
	}
	.layout {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: 24px;
	}
	.list {
		flex: 1 1 520px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.card {
		display: flex;
		align-items: center;
		gap: 14px;
		padding: 14px 16px;
		border-width: 2px;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.card[aria-pressed='true'] {
		border-color: var(--accent);
	}
	.monogram {
		width: 44px;
		height: 44px;
		flex: none;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: var(--radius-ctl);
		color: var(--on-monogram);
		font-weight: 600;
	}
	.monogram.large {
		width: 56px;
		height: 56px;
		font-size: 1.25rem;
	}
	.about {
		flex: 1 1 auto;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 3px;
	}
	.name-row {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 4px 10px;
	}
	.name {
		font-size: 0.9375rem;
		font-weight: 600;
	}
	.source {
		font-size: 0.75rem;
	}
	.description {
		font-size: 0.8125rem;
		overflow-wrap: anywhere;
	}
	.note {
		font-size: 0.8125rem;
	}
	aside {
		flex: 0 1 520px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}
	.head {
		display: flex;
		align-items: flex-start;
		gap: 14px;
		padding: 20px;
		border-bottom: 1px solid var(--panel-border);
	}
	h2 {
		font-size: 1.125rem;
		font-weight: 600;
	}
	.meta,
	.path {
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
	.body {
		display: flex;
		flex-direction: column;
		gap: 16px;
		padding: 18px 20px;
		font-size: 0.875rem;
	}
	.block {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	h3 {
		font-size: 0.75rem;
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	ul {
		margin: 0;
		padding-left: 1.25rem;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.confirm {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
	}
	.check span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.text {
		font-size: 0.875rem;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
