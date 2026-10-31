<script lang="ts">
	import TextDiff from './TextDiff.svelte';
	import { beforeNavigate } from '$app/navigation';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { MAX_TEXT_BYTES } from '#lib/text-files.ts';
	type TextFile = { content: string; version: string };
	type Props = { root: string; path: string; stick?: boolean; onclose: () => void };
	let { root, path, stick = false, onclose }: Props = $props();
	let previous = $state<TextFile | null>(null),
		showPrevious = $state(false),
		restoreConfirmed = $state(false);
	let text = $state('');
	let original = $state('');
	let version = $state('');
	let loaded = $state(false);
	let error = $state('');
	let busy = $state(false);
	let saved = $state(false);
	let discard = $state(false);
	let form: HTMLFormElement;
	const dirty = $derived(loaded && text !== original);
	const tooLarge = $derived(new TextEncoder().encode(text).length > MAX_TEXT_BYTES);

	$effect(() => {
		let disposed = false;
		loaded = false;
		api
			.get<TextFile>(
				`/files/text?root=${encodeURIComponent(root)}&path=${encodeURIComponent(path)}`,
			)
			.then((body) => {
				if (!disposed) {
					text = original = body.content;
					version = body.version;
					loaded = true;
				}
			})
			.catch((err) => {
				if (!disposed) error = apiErrorText(err);
			});
		return () => {
			disposed = true;
		};
	});
	beforeNavigate((navigation) => {
		if (busy || (dirty && !navigation.willUnload && !window.confirm(t('files.editDiscard'))))
			navigation.cancel();
	});
	function beforeUnload(event: BeforeUnloadEvent) {
		if (dirty || busy) {
			event.preventDefault();
			event.returnValue = '';
		}
	}
	function close() {
		if (busy) return;
		if (dirty) discard = true;
		else onclose();
	}
	function keydown(event: KeyboardEvent) {
		if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
			event.preventDefault();
			if (dirty && !busy && !tooLarge) form.requestSubmit();
		}
	}
	async function loadPrevious() {
		busy = true;
		error = '';
		try {
			previous = await api.get<TextFile>(
				'/files/text/previous?' + new URLSearchParams({ root, path }),
			);
			showPrevious = true;
			restoreConfirmed = false;
		} catch (err) {
			error =
				err instanceof ApiError && err.code === 'file_not_found'
					? t('files.previousMissing')
					: apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	async function restorePrevious() {
		if (!previous || !restoreConfirmed || busy || dirty) return;
		busy = true;
		error = '';
		try {
			const result = await api.put<TextFile>('/files/text', {
				root,
				path,
				content: previous.content,
				version,
			});
			text = original = result.content;
			version = result.version;
			saved = true;
			showPrevious = false;
			previous = null;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!loaded || busy || tooLarge) return;
		busy = true;
		error = '';
		saved = false;
		const content = text;
		try {
			const result = await api.put<TextFile>('/files/text', { root, path, content, version });
			original = content;
			version = result.version;
			saved = true;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<svelte:window onbeforeunload={beforeUnload} onkeydown={keydown} />
<Dialog title={t('files.editTitle', { name: path })} onclose={close} locked={dirty || busy} wide>
	<form onsubmit={save} bind:this={form}>
		<p class={stick ? 'notice-warn' : 'muted'}>
			{t(stick ? 'files.editWarning' : 'files.editHint')}
		</p>
		{#if !showPrevious}<textarea
				class="field mono"
				rows="18"
				bind:value={text}
				oninput={() => (saved = false)}
				spellcheck="false"
				disabled={!loaded || busy}
				aria-label={path}></textarea>{/if}
		<button
			type="button"
			class="btn"
			disabled={!loaded || busy || dirty || showPrevious}
			onclick={loadPrevious}>{t('files.previousVersion')}</button
		>
		{#if showPrevious && previous}<section>
				<h3>{t('files.previousCompare')}</h3>
				<p class="muted">{t('files.previousHint')}</p>
				<TextDiff before={previous.content} after={original} />
				<label
					><input type="checkbox" bind:checked={restoreConfirmed} disabled={busy} />{t(
						'files.previousConfirm',
					)}</label
				>
				<div class="dialog-actions">
					<button type="button" class="btn" disabled={busy} onclick={() => (showPrevious = false)}
						>{t('common.cancel')}</button
					><button
						type="button"
						class="btn btn-danger"
						disabled={busy || dirty || !restoreConfirmed}
						onclick={restorePrevious}>{t('files.previousRestore')}</button
					>
				</div>
			</section>{/if}
		{#if tooLarge}<p class="notice-error" role="alert">{t('files.editTooLarge')}</p>{/if}
		{#if saved}<p class="notice-ok" role="status">{t('files.editSaved', { name: path })}</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if discard}
			<p class="notice-warn">{t('files.editDiscard')}</p>
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (discard = false)}
					>{t('files.editContinue')}</button
				>
				<button type="button" class="btn" onclick={onclose}>{t('files.editDiscardButton')}</button>
			</div>
		{:else}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={close} disabled={busy}
					>{t('files.editClose')}</button
				>
				<button
					type="submit"
					class="btn btn-primary"
					disabled={busy || !loaded || !dirty || tooLarge}>{t('notify.save')}</button
				>
			</div>
		{/if}
	</form>
</Dialog>

<style>
	section {
		margin: 16px 0;
	}
	textarea {
		width: 100%;
		box-sizing: border-box;
		resize: vertical;
		min-height: 16rem;
		margin-bottom: 0.8rem;
		font-size: var(--text-base);
	}
</style>
