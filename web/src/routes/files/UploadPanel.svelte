<script lang="ts">
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes, formatDuration, formatPercent } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { cancel, clearFinished, pause, resolve, resume, uploads, type Upload } from '#lib/uploads.svelte.ts';

	const done = $derived(uploads.items.filter((item) => item.state === 'done').length);
	const active = $derived(uploads.items.find((item) => item.state === 'uploading' || item.state === 'retrying'));
	const rate = $derived.by(() => {
		if (!active || active.speed <= 0) return '';
		const left = uploads.items.filter((item) => item.state === 'uploading' || item.state === 'queued').reduce((sum, item) => sum + item.file.size - item.sent, 0);
		return `${t('uploads.speed', { speed: formatBytes(active.speed) })} · ${t('sync.remaining', { time: formatDuration(left / active.speed) })}`;
	});

	function percent(item: Upload): number {
		return item.file.size > 0 ? (100 * item.sent) / item.file.size : 100;
	}
</script>

{#if uploads.items.length > 0}
	<section class="tile panel" aria-label={t('uploads.title', { done, total: uploads.items.length })}>
		<div class="head">
			<span class="title">{t('uploads.title', { done, total: uploads.items.length })}</span>
			<span class="mono muted">{rate}</span>
			<button type="button" class="btn small" onclick={clearFinished}>{t('uploads.clear')}</button>
		</div>
		{#each uploads.items as item (item.key)}
			<div class="item">
				<div class="line">
					<span class="name">{item.name}</span>
					<span class="muted where">{t('uploads.to', { path: [item.rootName, item.dir].filter(Boolean).join('/') })}</span>
					{#if item.state === 'done'}
						<span class="state ok"><Icon name="check" size={14} />{t('uploads.done')}</span>
					{:else if item.state === 'failed'}
						<span class="state error">{t('uploads.failed')}: {errorText(item.error)}</span>
					{:else if item.state === 'conflict'}
						<span class="state warn">{t('uploads.conflict')}</span>
					{:else if item.state === 'paused'}
						<span class="state warn">{t('uploads.paused')} · {formatPercent(percent(item))}</span>
					{:else if item.state === 'pressure'}
                        <span class="state warn">{t('error.upload_memory_pressure')}</span>
                    {:else if item.state === 'retrying'}
						<span class="state warn">{t('uploads.retrying')}</span>
					{:else if item.state === 'queued'}
						<span class="state muted">{t('uploads.queued')}</span>
					{:else}
						<span class="mono state">{formatPercent(percent(item))}</span>
					{/if}
					{#if item.state === 'conflict'}
						<button type="button" class="btn small" onclick={() => resolve(item, 'replace')}>{t('uploads.replace')}</button>
						<button type="button" class="btn small" onclick={() => resolve(item, 'rename')}>{t('uploads.keepBoth')}</button>
						<button type="button" class="btn small" onclick={() => resolve(item, 'skip')}>{t('uploads.skip')}</button>
					{:else if item.state === 'uploading' || item.state === 'retrying' || item.state === 'pressure' || item.state === 'queued'}
						<button type="button" class="icon" aria-label="{t('uploads.pause')}: {item.name}" onclick={() => pause(item)}><Icon name="pause" size={16} /></button>
						<button type="button" class="icon" aria-label="{t('uploads.cancel')}: {item.name}" onclick={() => cancel(item)}><Icon name="close" size={16} /></button>
					{:else if item.state === 'paused' || item.state === 'failed'}
						<button type="button" class="icon" aria-label="{t('uploads.resume')}: {item.name}" onclick={() => resume(item)}><Icon name="play" size={16} /></button>
						<button type="button" class="icon" aria-label="{t('uploads.cancel')}: {item.name}" onclick={() => cancel(item)}><Icon name="close" size={16} /></button>
					{/if}
				</div>
				{#if item.state === 'uploading' || item.state === 'retrying' || item.state === 'paused'}
					<div class="bar"><span style:width="{percent(item)}%"></span></div>
				{/if}
			</div>
		{/each}
	</section>
{/if}

<style>
	.panel {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 14px 16px;
		font-size: 0.8125rem;
	}
	.head,
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 12px;
	}
	.title {
		margin-right: auto;
		font-weight: 600;
	}
	.item {
		display: flex;
		flex-direction: column;
		gap: 6px;
		padding-top: 10px;
		border-top: 1px solid var(--panel-border);
	}
	.name {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.where {
		flex: 1 1 auto;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-size: 0.75rem;
	}
	.state {
		display: inline-flex;
		align-items: center;
		gap: 4px;
	}
	.state.ok {
		color: var(--ok-text);
	}
	.state.warn {
		color: var(--warn-text);
	}
	.state.error {
		color: var(--error-text);
	}
	.small {
		height: 1.875rem;
		padding: 0 0.625rem;
		font-size: 0.75rem;
	}
	.icon {
		width: 30px;
		height: 30px;
		display: flex;
		align-items: center;
		justify-content: center;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		cursor: pointer;
	}
	.icon:hover {
		background: var(--chip);
		color: var(--text);
	}
</style>
