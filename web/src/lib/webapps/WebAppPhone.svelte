<script lang="ts">
	// PLAN 66: a web app full screen on the phone, with Back (to where it was
	// opened from) and the app's menu. The file it shows lives in the page's
	// history state, so Back closes it.
	import { page } from '$app/state';
	import { goto, pushState } from '$app/navigation';
	import Dialog from '#lib/components/Dialog.svelte';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import DesktopIcon from '#lib/desktop/DesktopIcon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import WebAppView from './WebAppView.svelte';
	import { baseName, letterGradient, type DocRef } from './protocol.ts';
	import { loadWebApps, webAppById, webapps } from './webapps.svelte.ts';

	// Back (button, gesture) with unsaved work: the app stays and asks first.
	let last = $state.raw<App.PageState['webapp']>(undefined);
	let unsaved = $state(false);
	let leaving = $state(false);
	// Decided at once (not in an effect): the app must not be taken down first.
	const held = $derived(!page.state.webapp && !!last && !leaving && unsaved);
	$effect(() => {
		const now = page.state.webapp;
		if (now) {
			last = now;
			leaving = false;
		} else if (held) {
			asking = 'back';
			pushState('', { ...page.state, webapp: last });
		} else last = undefined;
	});
	const shown = $derived(page.state.webapp ?? (held ? last : undefined));
	const app = $derived(shown ? webAppById(shown.id) : undefined);
	const doc = $derived<DocRef | undefined>(
		shown?.root && shown.path ? { root: shown.root, path: shown.path } : undefined,
	);
	let name = $state('');
	let view = $state<WebAppView>();
	let asking = $state<'back' | 'restart' | null>(null);

	$effect(() => {
		if (shown && !webapps.loaded) void loadWebApps();
	});
	$effect(() => {
		void shown?.path;
		name = doc ? baseName(doc.path) : '';
	});

	function back() {
		if (view?.isUnsaved()) asking = 'back';
		else history.back();
	}
	function leave() {
		leaving = true;
		unsaved = false;
		history.back();
	}
	function setDoc(next: DocRef | undefined) {
		if (!shown) return;
		void goto(page.url.pathname + page.url.search, {
			state: { ...page.state, webapp: { id: shown.id, root: next?.root, path: next?.path } },
			shallow: true,
			replaceState: true,
		});
	}
</script>

{#if shown}
	<div class="full" role="dialog" aria-label={app?.name ?? shown.id}>
		<header class="head">
			<button type="button" class="icon-btn big" onclick={back} aria-label={t('desktop.phone.home')}
				><Icon name="chevronLeft" size={22} /></button
			>
			{#if app}<DesktopIcon
					glyph=""
					gradient={letterGradient(app.id)}
					image={app.icon ?? ''}
					name={app.name}
					size={24}
				/>{/if}
			<b class="title"
				>{app?.name ?? shown.id}{#if name}<span class="doc">{' · '}{name}</span>{/if}</b
			>
			{#if app}
				<Dropdown label={t('webapps.menu.more')} icon="more" align="right">
					{#snippet children(close)}
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								view?.openPicker();
							}}><Icon name="folder" size={16} /><span>{t('webapps.menu.open')}</span></button
						>
						{#if app.bridge === 'host'}<button
								type="button"
								role="menuitem"
								onclick={() => {
									close();
									view?.saveAs();
								}}><Icon name="download" size={16} /><span>{t('webapps.menu.saveAs')}</span></button
							>{/if}
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								view?.openTab();
							}}
							><Icon name="external" size={16} /><span>{t('desktop.window.openTab')}</span></button
						>
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								if (view?.isUnsaved()) asking = 'restart';
								else view?.restart();
							}}><Icon name="restart" size={16} /><span>{t('webapps.restart')}</span></button
						>
					{/snippet}
				</Dropdown>
			{/if}
		</header>
		<div class="body">
			{#if app?.installed && app.url}
				<WebAppView
					bind:this={view}
					{app}
					{doc}
					ondoc={setDoc}
					onname={(n) => (name = n)}
					onunsaved={(u) => (unsaved = u)}
				/>
			{:else if webapps.loaded}
				<p class="pad">{t('webapps.unknownApp')}</p>
			{/if}
		</div>
	</div>
	{#if asking}
		<Dialog title={t('webapps.unsavedTitle')} onclose={() => (asking = null)}>
			<p>
				{t(asking === 'back' ? 'webapps.unsavedText' : 'webapps.unsavedRestart', {
					app: app?.name ?? '',
				})}
			</p>
			<div class="dialog-actions">
				<button type="button" class="btn btn-primary" onclick={() => (asking = null)}
					>{t('common.cancel')}</button
				>
				<button
					type="button"
					class="btn btn-danger"
					onclick={() => {
						const was = asking;
						asking = null;
						if (was === 'back') leave();
						else view?.restart();
					}}>{t(asking === 'back' ? 'webapps.closeAnyway' : 'webapps.restartAnyway')}</button
				>
			</div>
		</Dialog>
	{/if}
{/if}

<style>
	.full {
		position: fixed;
		inset: 0;
		z-index: 60;
		display: flex;
		flex-direction: column;
		background: var(--bg);
	}
	.head {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: calc(52px + env(safe-area-inset-top));
		padding: env(safe-area-inset-top) calc(var(--space-1-5) + env(safe-area-inset-right)) 0
			calc(6px + env(safe-area-inset-left));
		border-bottom: 1px solid var(--border);
		background: var(--header);
		flex: none;
	}
	.title {
		flex: 1;
		min-width: 0;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.doc {
		font-weight: 400;
		color: var(--muted);
	}
	.icon-btn.big {
		width: 44px;
		height: 44px;
		display: grid;
		place-items: center;
		border: 0;
		background: none;
		color: var(--text);
		border-radius: 10px;
		flex: none;
	}
	.body {
		flex: 1;
		min-height: 0;
		display: flex;
		padding-bottom: env(safe-area-inset-bottom);
	}
	.pad {
		padding: var(--space-5);
	}
</style>
