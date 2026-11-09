<script lang="ts">
	// The logs of one container: the last 300 lines, search with marked hits,
	// following (again every three seconds) and download as a text file.
	import { tick } from 'svelte';
	import { api, ApiError, type DockerContainer } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { dockerName } from '#lib/docker-name.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { poll } from '#lib/poll.ts';

	let { container, onclose }: { container: DockerContainer; onclose: () => void } = $props();

	// Only the answer to the latest request counts; a late answer for another
	// container is dropped.
	let request = 0;
	let text = $state<string | null>(null);
	let error = $state(''),
		follow = $state(false),
		search = $state(''),
		loading = $state(false);
	let box: HTMLElement | undefined = $state();

	async function load() {
		const mine = ++request;
		const id = container.id;
		error = '';
		loading = true;
		try {
			const res = await api.get<{ logs: string }>(`/docker/containers/${id}/logs?tail=300`);
			if (mine !== request || id !== container.id) return;
			text = res.logs;
			if (follow) {
				await tick();
				if (box) box.scrollTop = box.scrollHeight;
			}
		} catch (err) {
			if (mine === request && id === container.id)
				error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			if (mine === request) loading = false;
		}
	}
	// A new container starts afresh: no text, no following, no search.
	$effect(() => {
		void container.id;
		text = null;
		follow = false;
		search = '';
		void load();
	});
	// Following fetches the logs again every three seconds while the dialog is open.
	$effect(() => {
		if (!follow) return;
		return poll(() => load(), { interval: 3000, immediate: false });
	});

	type LogPart = { text: string; hit: boolean };
	const parts = $derived.by<LogPart[]>(() => {
		const all = text ?? '';
		const query = search.trim().toLowerCase();
		if (!query) return [{ text: all, hit: false }];
		const found: LogPart[] = [];
		const lower = all.toLowerCase();
		let at = 0;
		for (let hit = lower.indexOf(query); hit >= 0; hit = lower.indexOf(query, at)) {
			if (hit > at) found.push({ text: all.slice(at, hit), hit: false });
			found.push({ text: all.slice(hit, hit + query.length), hit: true });
			at = hit + query.length;
		}
		if (at < all.length) found.push({ text: all.slice(at), hit: false });
		return found;
	});
	const hits = $derived(parts.filter((part) => part.hit).length);
	$effect(() => {
		// The first hit comes into view when the search changes.
		if (!search.trim() || !box) return;
		void hits;
		tick().then(() => box?.querySelector('mark')?.scrollIntoView({ block: 'nearest' }));
	});

	function download(content: string) {
		const url = URL.createObjectURL(new Blob([content], { type: 'text/plain;charset=utf-8' }));
		const anchor = document.createElement('a');
		anchor.href = url;
		anchor.download = `${dockerName(container, 'distinct').replace(/[^\w.-]+/g, '_')}-logs.txt`;
		anchor.click();
		setTimeout(() => URL.revokeObjectURL(url), 1000);
	}
</script>

<Dialog title={t('docker.logsTitle', { name: dockerName(container, 'distinct') })} wide {onclose}>
	<div class="logs-tools">
		<input
			class="field logs-search"
			type="search"
			bind:value={search}
			placeholder={t('docker.logsSearch')}
			aria-label={t('docker.logsSearch')}
		/>
		{#if search.trim()}<span class="muted mono" role="status"
				>{t('docker.logsHits', { count: hits })}</span
			>{/if}
		<label class="check"
			><input type="checkbox" bind:checked={follow} /><span>{t('docker.logsFollow')}</span></label
		>
	</div>
	{#if error}<div class="notice-error logs-error" role="alert">
			<span>{error}</span><button type="button" class="btn" disabled={loading} onclick={load}
				>{t('common.retry')}</button
			>
		</div>{/if}
	<pre class="logs" bind:this={box}>{#if text === null}…{:else if !text}{t(
				'docker.logsEmpty',
			)}{:else}{#each parts as part, index (index)}{#if part.hit}<mark>{part.text}</mark
					>{:else}{part.text}{/if}{/each}{/if}</pre>
	<div class="dialog-actions">
		<button type="button" class="btn" disabled={!text} onclick={() => text && download(text)}
			><Icon name="download" size={16} />{t('docker.logsDownload')}</button
		>
		<button type="button" class="btn" disabled={loading} onclick={load}
			>{t('docker.logsRefresh')}</button
		>
		<button type="button" class="btn btn-primary" onclick={onclose}>{t('common.done')}</button>
	</div>
</Dialog>

<style>
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: var(--text-md);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: var(--space-2-5);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.logs {
		max-height: 60vh;
		margin: 0;
		padding: var(--space-3) 14px;
		overflow: auto;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		line-height: 1.5;
	}
	.logs mark {
		background: var(--warn-bg);
		color: var(--warn-text);
		outline: 1px solid var(--warn-border);
		border-radius: var(--radius-small);
	}
	.logs-tools {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-4);
		margin-bottom: var(--space-2-5);
	}
	.logs-search {
		flex: 1 1 14rem;
		min-width: 0;
	}
	.logs-error {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		margin-bottom: var(--space-2-5);
	}
</style>
