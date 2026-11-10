<script lang="ts">
	// PLAN 66: App-Center › Programme für den Browser – every app of the
	// catalog with its state; administrators install and remove them.
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import DesktopIcon from '#lib/desktop/DesktopIcon.svelte';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { letterGradient, type WebApp } from './protocol.ts';
	import VolumeChoice from './VolumeChoice.svelte';
	import { loadWebApps, webapps } from './webapps.svelte.ts';

	// Craft apps whose web version does not start yet (PLAN 75-9: visible,
	// installing locked). PhotoCraft: storytold's web zip lacks a file the
	// app loads (docs/WEBAPPS.md); it comes into the catalog once it starts.
	const NOT_YET: { id: string; name: string; why: MessageKey }[] = [
		{ id: 'photocraft', name: 'PhotoCraft', why: 'webapps.center.photocraftWhy' },
	];
	const notYet = $derived(NOT_YET.filter((n) => !webapps.apps.some((a) => a.id === n.id)));
	// Originals with our own build as successor (WordCraft → T-Word) are no
	// longer offered; an installed one stays listed until it is replaced.
	const shown = $derived(webapps.apps.filter((a) => !a.successor || a.installed));
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	let busy = $state('');
	let error = $state('');
	let removing = $state<WebApp | null>(null);
	// PLAN 75-21: choosing where the programs lie (first install) or moving them.
	let volumeDialog = $state<{ then?: WebApp } | null>(null);
	let target = $state('');
	const installedCount = $derived(webapps.apps.filter((a) => a.installed).length);
	async function chooseVolume() {
		const next = volumeDialog?.then;
		busy = 'volume';
		error = '';
		try {
			await api.post('/webapps/volume', { volume: target });
			volumeDialog = null;
			await loadWebApps();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = '';
		}
		if (next && !error) await install(next);
	}

	$effect(() => {
		void loadWebApps();
	});
	// While an app installs, its progress is asked for every 1.5 s.
	const installing = $derived(webapps.apps.some((a) => a.state === 'downloading'));
	$effect(() => {
		if (!installing) return;
		const timer = setInterval(() => void loadWebApps(), 1500);
		return () => clearInterval(timer);
	});

	async function install(a: WebApp) {
		if (webapps.needsVolume) {
			volumeDialog = { then: a };
			return;
		}
		busy = a.id;
		error = '';
		try {
			await api.post(`/webapps/${encodeURIComponent(a.id)}/install`);
			await loadWebApps();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = '';
		}
	}
	async function remove() {
		const a = removing;
		if (!a) return;
		busy = a.id;
		error = '';
		try {
			await api.delete(`/webapps/${encodeURIComponent(a.id)}`);
			removing = null;
			await loadWebApps();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = '';
		}
	}
	/**
	 * PLAN 75-20: an original Craft app is replaced by our own build (WordCraft
	 * → T-Word): the build is installed, and once it is ready the original goes.
	 */
	const successorOf = (a: WebApp) =>
		a.successor ? webapps.apps.find((x) => x.id === a.successor) : undefined;
	async function replaceBy(a: WebApp) {
		const next = successorOf(a);
		if (!next) return;
		busy = a.id;
		error = '';
		try {
			if (!next.installed) await api.post(`/webapps/${encodeURIComponent(next.id)}/install`);
			for (let i = 0; i < 600; i++) {
				await loadWebApps();
				const now = webapps.apps.find((x) => x.id === next.id);
				if (now?.installed && now.state === 'ready') break;
				if (now?.state === 'error') throw new Error(stateText(now));
				await new Promise((ok) => setTimeout(ok, 1500));
			}
			await api.delete(`/webapps/${encodeURIComponent(a.id)}`);
			await loadWebApps();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = '';
		}
	}
	/** The NAS reports install errors as codes (no_volume, download_failed, …). */
	function errorWord(code = ''): string {
		const key = `error.${code}` as MessageKey;
		const text = /^[a-z_]+$/.test(code) ? t(key) : '';
		return text && text !== key ? text : code;
	}
	function stateText(a: WebApp): string {
		if (a.state === 'downloading')
			return t('webapps.state.downloading', { percent: formatPercent((a.progress ?? 0) * 100) });
		if (a.state === 'error') return t('webapps.state.error', { error: errorWord(a.error) });
		if (a.installed) return t('webapps.state.ready');
		if (a.pending) return t('webapps.state.pending');
		return t('webapps.state.missing');
	}
</script>

{#if webapps.loaded && !webapps.unavailable && webapps.apps.length}
	<section class="tile panel browser-apps" aria-labelledby="browser-apps">
		<h2 class="tile-title" id="browser-apps">{t('webapps.center.title')}</h2>
		<p class="muted">{t('webapps.center.intro')}</p>
		{#if !webapps.volume}<p class="notice-warn" role="status">
				{t('webapps.center.noVolume')}
			</p>{/if}
		{#if admin && webapps.volume && webapps.volumes.length > 1 && installedCount > 0}
			<div class="where">
				<span class="grow"
					><span class="muted small">{t('webapps.volume.where')}</span><br /><b>{webapps.volume}</b
					><span class="muted small">
						· {t(installedCount === 1 ? 'webapps.volume.count.one' : 'webapps.volume.count.other', {
							count: installedCount,
						})}</span
					></span
				>
				<button type="button" class="btn" disabled={!!busy} onclick={() => (volumeDialog = {})}
					>{t('webapps.volume.move')}</button
				>
			</div>
		{/if}
		<ul class="list">
			{#each shown as a (a.id)}
				<li class="app">
					<DesktopIcon
						glyph=""
						gradient={letterGradient(a.id)}
						image={a.icon ?? ''}
						name={a.name}
						size={40}
					/>
					<div class="text">
						<b
							>{a.name}{#if a.version}<span class="muted ver">{' '}{a.version}</span>{/if}</b
						>
						<span class="muted small types">{a.types.map((x) => '.' + x).join(' ')}</span>
						<span class="status small" class:err={a.state === 'error'} role="status">
							<span
								class="dot"
								class:on={a.installed && a.state === 'ready'}
								class:run={a.state === 'downloading'}
								aria-hidden="true"
							></span>{stateText(a)}{#if a.size && !a.installed}
								· {formatBytes(a.size)}{/if}
						</span>
						{#if a.state === 'downloading'}<progress max="1" value={a.progress ?? 0}
							></progress>{/if}
					</div>
					{#if admin}
						<div class="actions">
							{#if a.installed && successorOf(a)}
								<button
									type="button"
									class="btn btn-primary"
									disabled={!!busy || !webapps.volume}
									onclick={() => replaceBy(a)}
									>{busy === a.id
										? t('webapps.center.replacing', { app: successorOf(a)?.name ?? '' })
										: t('webapps.center.replaceBy', { app: successorOf(a)?.name ?? '' })}</button
								>
							{/if}
							{#if a.installed}
								<button
									type="button"
									class="btn"
									disabled={!!busy || a.state === 'downloading'}
									onclick={() => (removing = a)}>{t('webapps.center.remove')}</button
								>
							{:else if a.state !== 'downloading'}
								<button
									type="button"
									class="btn btn-primary"
									disabled={!!busy || !webapps.volume}
									onclick={() => install(a)}
									>{a.state === 'error' ? t('common.retry') : t('webapps.center.install')}</button
								>
							{/if}
						</div>
					{/if}
				</li>
			{/each}
			{#each notYet as n (n.id)}
				<li class="app">
					<DesktopIcon glyph="" gradient={letterGradient(n.id)} image="" name={n.name} size={40} />
					<div class="text">
						<b>{n.name}</b>
						<span class="muted small">{t(n.why)}</span>
						<span class="status small" role="status"
							><span class="dot" aria-hidden="true"></span>{t('webapps.center.notYet')}</span
						>
					</div>
					{#if admin}
						<div class="actions">
							<button type="button" class="btn" disabled>{t('webapps.center.install')}</button>
						</div>
					{/if}
				</li>
			{/each}
		</ul>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	</section>
{/if}

{#if volumeDialog}
	<Dialog
		title={t(volumeDialog.then ? 'webapps.volume.choose' : 'webapps.volume.moveTitle')}
		locked={busy === 'volume'}
		onclose={() => (volumeDialog = null)}
	>
		<p>{t(volumeDialog.then ? 'webapps.volume.hint' : 'webapps.volume.moveText')}</p>
		<VolumeChoice
			volumes={webapps.volumes}
			bind:value={target}
			current={volumeDialog.then ? '' : webapps.volume}
			disabled={busy === 'volume'}
		/>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button
				type="button"
				class="btn btn-primary"
				disabled={!!busy || !target || (!volumeDialog.then && target === webapps.volume)}
				onclick={chooseVolume}
				>{busy === 'volume'
					? t('webapps.volume.moving')
					: volumeDialog.then
						? t('webapps.center.install')
						: t('webapps.volume.moveButton')}</button
			>
			<button
				type="button"
				class="btn"
				disabled={busy === 'volume'}
				onclick={() => (volumeDialog = null)}>{t('common.cancel')}</button
			>
		</div>
	</Dialog>
{/if}

{#if removing}
	<Dialog
		title={t('webapps.center.removeTitle', { app: removing.name })}
		onclose={() => (removing = null)}
	>
		<p>{t('webapps.center.removeText', { app: removing.name })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={!!busy} onclick={remove}
				>{t('webapps.center.remove')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.browser-apps {
		margin-bottom: var(--space-4);
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr));
		gap: var(--space-2-5);
	}
	.app {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		padding: var(--space-3);
		border: 1px solid var(--border);
		border-radius: calc(10px * var(--radius-scale, 1));
		flex-wrap: wrap;
	}
	.text {
		flex: 1 1 160px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 3px;
	}
	.ver {
		font-weight: 400;
		font-size: var(--text-sm);
	}
	.types {
		overflow-wrap: break-word;
	}
	.status {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		flex-wrap: wrap;
	}
	.status.err {
		color: var(--danger-text, var(--text));
	}
	.dot {
		width: 9px;
		height: 9px;
		border-radius: 50%;
		border: 1.5px solid var(--muted);
		flex: none;
	}
	.dot.on {
		background: var(--ok-text);
		border-color: var(--ok-text);
	}
	.dot.run {
		border-color: var(--accent);
		border-style: dashed;
	}
	progress {
		width: 100%;
		height: 6px;
	}
	.actions {
		display: flex;
		gap: var(--space-2);
		margin-left: auto;
	}
	.where {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		margin: var(--space-2) 0;
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
	}
	.where .grow {
		flex: 1 1 14rem;
		min-width: 0;
		overflow-wrap: anywhere;
	}
</style>
