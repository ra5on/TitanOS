<script lang="ts">
	// PLAN 66, update to 0.63 (owner 7.11.2026): once for owners and
	// administrators – install the office programs as web apps and, as an
	// option, remove the Docker versions of the Craft apps they replace.
	import { api } from '#lib/api.ts';
	import { activeNode } from '#lib/cluster-node.ts';
	import { clusterNodes } from '#lib/cluster.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatPercent } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { sanitizeHints } from '../../routes/storage/checks-hint.ts';
	import OfficeApps from './OfficeApps.svelte';
	import { loadWebApps, webapps } from './webapps.svelte.ts';

	// Per node (PLAN 69): the hints live on this NAS, the web apps on the
	// node the tab manages – another node of the cluster asks once on its own.
	const node = activeNode();
	const HINT = node ? `webapps-0.63@${node}` : 'webapps-0.63';
	const nodeName = $derived(
		node ? (clusterNodes.nodes.find((n) => n.id === node)?.name ?? node) : '',
	);
	let later = $state(false);
	let started = $state(false);
	let busy = $state(false);
	let error = $state('');

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const dismissed = $derived(sanitizeHints(session.hints)[HINT] === true);
	const office = $derived(webapps.apps.filter((a) => a.default));
	const missing = $derived(
		office.filter((a) => !a.installed && !a.pending && a.state !== 'downloading'),
	);
	const show = $derived(
		admin &&
			session.prefsLoaded > 0 &&
			!dismissed &&
			!later &&
			webapps.loaded &&
			!webapps.unavailable &&
			(started || webapps.docker.length > 0 || missing.length > 0),
	);

	// A migration that runs (also after a reload) shows its progress, not the choice again.
	$effect(() => {
		if (webapps.migration?.running) started = true;
	});
	let install = $state<string[] | null>(null);
	let remove = $state<string[] | null>(null);
	$effect(() => {
		if (!show || started) return;
		install ??= missing.map((a) => a.id);
	});
	// A Docker app is preselected only when its web app comes (or is there).
	const comes = (id: string) =>
		(install ?? []).includes(id) || !!webapps.apps.find((a) => a.id === id)?.installed;
	$effect(() => {
		if (!show || started || remove !== null || install === null) return;
		remove = webapps.docker.filter((d) => comes(d.webapp)).map((d) => d.app);
	});
	const chosenRemove = $derived(
		(remove ?? []).filter((app) => {
			const d = webapps.docker.find((x) => x.app === app);
			return !!d && comes(d.webapp);
		}),
	);

	async function dismiss() {
		const value = { ...sanitizeHints(session.hints), [HINT]: true };
		try {
			await api.put('/preferences/hints', value);
			session.hints = value;
		} catch (e) {
			error = apiErrorText(e);
		}
	}

	async function go() {
		busy = true;
		error = '';
		try {
			const apps = install ?? [];
			if (chosenRemove.length)
				await api.post('/webapps/migrate', { install: apps, removeDocker: chosenRemove });
			else await api.post('/webapps/defaults/install', { apps });
			started = true;
			await loadWebApps();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	// Progress while installing and removing.
	const running = $derived(
		started &&
			(webapps.apps.some((a) => a.state === 'downloading') || !!webapps.migration?.running),
	);
	$effect(() => {
		if (!running) return;
		const timer = setInterval(() => void loadWebApps(), 1500);
		return () => clearInterval(timer);
	});
	function itemText(state: string): string {
		switch (state) {
			case 'waiting':
				return t('webapps.migrate.item.waiting');
			case 'removing':
				return t('webapps.migrate.item.removing');
			case 'removed':
				return t('webapps.migrate.item.removed');
			case 'kept':
				return t('webapps.migrate.item.kept');
			default:
				return t('webapps.migrate.item.failed');
		}
	}
</script>

{#if show}
	<Dialog title={t('webapps.migrate.title')} locked={busy} onclose={() => (later = true)}>
		<div class="migrate">
			{#if !started}
				<h3>{t('webapps.migrate.whyTitle')}</h3>
				{#if nodeName}<p class="node">{t('webapps.migrate.onNode', { name: nodeName })}</p>{/if}
				<p>{t('webapps.migrate.whyText')}</p>
				{#if office.length}
					<OfficeApps
						apps={office}
						bind:selected={() => install ?? [], (v) => (install = v)}
						disabled={busy}
					/>
				{/if}
				{#if !webapps.volume}<p class="muted small">{t('welcome.officeNoVolume')}</p>{/if}
				{#if webapps.docker.length}
					<h3>{t('webapps.migrate.dockerTitle')}</h3>
					<p>{t('webapps.migrate.dockerText')}</p>
					<ul class="docker">
						{#each webapps.docker as d (d.app)}
							{@const possible = comes(d.webapp)}
							<li>
								<label class="check">
									<input
										type="checkbox"
										disabled={busy || !possible}
										checked={possible && (remove ?? []).includes(d.app)}
										onchange={(e) =>
											(remove = e.currentTarget.checked
												? [...(remove ?? []), d.app]
												: (remove ?? []).filter((x) => x !== d.app))}
									/>
									<span class="what">
										<b>{t('webapps.migrate.removeApp', { app: d.app })}</b>
										<span class="muted small"
											>{possible
												? t('webapps.migrate.removeHint', { app: d.app })
												: t('webapps.migrate.keepHint', {
														webapp: webapps.apps.find((a) => a.id === d.webapp)?.name ?? d.webapp,
													})}</span
										>
									</span>
								</label>
							</li>
						{/each}
					</ul>
				{/if}
				<details>
					<summary>{t('webapps.migrate.more')}</summary>
					<p class="muted small">{t('webapps.migrate.moreText')}</p>
				</details>
			{:else}
				<p role="status">
					{running ? t('webapps.migrate.running') : t('webapps.migrate.finished')}
				</p>
				<ul class="progress">
					{#each office.filter((a) => (install ?? []).includes(a.id)) as a (a.id)}
						<li>
							<b>{a.name}</b>
							<span class="muted small"
								>{a.state === 'downloading'
									? t('webapps.state.downloading', {
											percent: formatPercent((a.progress ?? 0) * 100),
										})
									: a.state === 'error'
										? t('webapps.state.error', { error: a.error ?? '' })
										: a.installed
											? t('webapps.state.ready')
											: a.pending
												? t('webapps.state.pending')
												: t('webapps.migrate.item.waiting')}</span
							>
						</li>
					{/each}
					{#each webapps.migration?.items ?? [] as item (item.app)}
						<li>
							<b>{t('webapps.migrate.dockerApp', { app: item.app })}</b>
							<span class="muted small"
								>{itemText(item.state)}{#if item.error}: {item.error}{/if}</span
							>
						</li>
					{/each}
				</ul>
			{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		</div>
		<div class="dialog-actions">
			{#if !started}
				<button type="button" class="btn" disabled={busy} onclick={dismiss}
					>{t('webapps.migrate.never')}</button
				>
				<button type="button" class="btn" disabled={busy} onclick={() => (later = true)}
					>{t('webapps.migrate.later')}</button
				>
				<button
					type="button"
					class="btn btn-primary"
					disabled={busy || (!(install ?? []).length && !chosenRemove.length)}
					onclick={go}>{t('webapps.migrate.install')}</button
				>
			{:else}
				<button type="button" class="btn btn-primary" disabled={running} onclick={dismiss}
					>{t('common.done')}</button
				>
			{/if}
		</div>
	</Dialog>
{/if}

<style>
	.node {
		font-weight: 600;
	}
	.migrate {
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
		min-width: 0;
	}
	h3 {
		margin: var(--space-1-5) 0 0;
		font-size: var(--text-md);
	}
	p {
		margin: 0;
		overflow-wrap: break-word;
	}
	.docker,
	.progress {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.check {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2-5);
	}
	.check input {
		margin-top: 3px;
		flex: none;
	}
	.what {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		overflow-wrap: break-word;
	}
	.progress li {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	summary {
		cursor: pointer;
	}
</style>
