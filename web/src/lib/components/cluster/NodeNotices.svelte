<script lang="ts">
	// The warnings and errors of a cluster node from the last 24 hours, the
	// ones its chip in the node list counts, read and deleted right there.
	// Another node is asked through the cluster proxy, which also works with
	// nodes of 0.43 and 0.44: they have the same notification routes.
	import { api } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { noticeText, type Notice } from '#lib/notifications.svelte.ts';
	import Dialog from '../Dialog.svelte';
	import Icon from '../Icon.svelte';

	type Props = { node: string; self: boolean; name: string; onchange?: () => void };
	let { node, self, name, onchange }: Props = $props();

	let notices = $state<Notice[] | null>(null);
	let error = $state('');
	let busy = $state(false);
	let confirmAll = $state(false);

	const base = $derived(
		self ? '/notifications' : `/cluster/nodes/${encodeURIComponent(node)}/api/notifications`,
	);
	const DAY = 24 * 60 * 60 * 1000;
	const shown = $derived(
		(notices ?? []).filter(
			(notice) => notice.severity !== 'info' && Date.now() - Date.parse(notice.time) < DAY,
		),
	);

	async function load() {
		error = '';
		try {
			notices = (await api.get<{ messages: Notice[] }>(base)).messages;
		} catch (err) {
			error = apiErrorText(err);
		}
	}

	async function dismiss(ids: number[]) {
		busy = true;
		error = '';
		try {
			notices = (await api.post<{ messages: Notice[] }>(`${base}/dismiss`, { ids })).messages;
			confirmAll = false;
			onchange?.();
		} catch (err) {
			error = apiErrorText(err);
		}
		busy = false;
	}

	$effect(() => {
		base;
		void load();
	});

	const CHIP = { info: 'info', warning: 'warn', error: 'error' } as const;
</script>

<div class="node-notices" role="region" aria-label={t('cluster.noticesOf', { name })}>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if notices === null && !error}
		<p class="muted small">{t('cluster.noticesLoading')}</p>
	{:else if notices !== null && shown.length === 0}
		<p class="muted small">{t('cluster.noticesNone')}</p>
	{:else if shown.length > 0}
		<ul>
			{#each shown as notice (notice.id)}
				{@const text = noticeText(notice)}
				<li>
					<div class="line">
						<span class="chip {CHIP[notice.severity]}"
							>{t(`notify.severity.${notice.severity}`)}</span
						>
						<time class="muted small" datetime={notice.time}
							>{formatDateTime(new Date(notice.time))}</time
						>
						<button
							class="btn small dismiss"
							type="button"
							disabled={busy}
							aria-label={t('notify.deleteOne', { title: text.title })}
							title={t('common.delete')}
							onclick={() => dismiss([notice.id])}><Icon name="trash" size={14} /></button
						>
					</div>
					<span class="what">{text.title}</span>
					{#if text.text}<span class="muted small">{text.text}</span>{/if}
				</li>
			{/each}
		</ul>
		<button type="button" class="btn small" disabled={busy} onclick={() => (confirmAll = true)}
			>{t('notify.deleteAll')}</button
		>
	{/if}
</div>

{#if confirmAll}
	<Dialog title={t('notify.deleteAllTitle')} locked={busy} onclose={() => (confirmAll = false)}>
		<p>{t('cluster.noticesDeleteText', { count: shown.length, name })}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (confirmAll = false)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				disabled={busy}
				onclick={() => dismiss(shown.map((notice) => notice.id))}
				>{t('notify.deleteAllConfirm')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.node-notices {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 8px;
		margin-top: 8px;
		padding: 10px 12px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-sm, 8px);
		width: 100%;
		box-sizing: border-box;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		width: 100%;
	}
	li {
		display: flex;
		flex-direction: column;
		gap: 3px;
		padding: 8px 0;
		border-top: 1px solid var(--panel-border);
		min-width: 0;
	}
	li:first-child {
		border-top: 0;
		padding-top: 0;
	}
	.line {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.dismiss {
		margin-left: auto;
	}
	.what {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.small {
		font-size: var(--text-sm);
	}
	p {
		margin: 0;
	}
</style>
