<script lang="ts">
	// The steps of a plugin installation, update or removal as the NAS reports
	// them: a mark per step, the download bar and the error of a failed step.
	import { formatBytes, formatMegabytesPerSecond } from '#lib/format.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { downloadPercent, pluginStepKey, type PluginProgress } from '#lib/plugin-progress.ts';

	let { progress, ondismiss }: { progress: PluginProgress; ondismiss?: () => void } = $props();
	const percent = $derived(downloadPercent(progress));
	const marks: Record<string, string> = {
		pending: '○',
		running: '●',
		done: '✓',
		failed: '✕',
		skipped: '–',
	};
	const stateKey = (state: string) => `plugins.stepState.${state}` as MessageKey;
</script>

<div class="steps" role="status" aria-live="polite">
	<ol>
		{#each progress.steps as step (step.id)}
			<li class={step.state} aria-current={step.state === 'running' ? 'step' : undefined}>
				<span class="mark" aria-hidden="true">{marks[step.state] ?? '○'}</span>
				<span class="label"
					>{t(pluginStepKey(step.id))}<span class="sr-only">
						– {t(stateKey(step.state))}</span
					>{#if step.state === 'skipped'}<span class="muted">
							· {t(stateKey(step.state))}</span
						>{/if}</span
				>
				{#if step.id === 'download' && step.state === 'running'}
					<div class="download">
						{#if percent !== undefined}
							<div
								class="bar"
								role="progressbar"
								aria-valuemin="0"
								aria-valuemax="100"
								aria-valuenow={Math.round(percent)}
								aria-label={t('plugins.step.download')}
							>
								<span style:width="{percent}%"></span>
							</div>
						{:else}
							<progress aria-label={t('plugins.step.download')}></progress>
						{/if}
						<span class="muted mono"
							>{progress.total > 0
								? `${formatBytes(progress.bytes)} / ${formatBytes(progress.total)}`
								: formatBytes(progress.bytes)}{#if progress.speedBytesPerSecond > 0}{' · '}{t(
									'plugins.speed',
									{ speed: formatMegabytesPerSecond(progress.speedBytesPerSecond) },
								)}{/if}</span
						>
					</div>
				{/if}
				{#if step.state === 'failed' && step.error}<pre>{step.error}</pre>{/if}
			</li>
		{/each}
	</ol>
	{#if progress.state === 'failed'}
		<div class="notice-error" role="alert">
			{t('plugins.stepsFailed')}
			{#if progress.error && !progress.steps.some((step) => step.state === 'failed')}<pre>{progress.error}</pre>{/if}
		</div>
	{:else if progress.state === 'done'}
		<p class="notice-ok">{t('plugins.stepsDone')}</p>
	{/if}
	{#if ondismiss && progress.state !== 'running'}
		<button type="button" class="btn" onclick={ondismiss}>{t('plugins.stepsDismiss')}</button>
	{/if}
</div>

<style>
	.steps {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 8px;
		font-size: var(--text-base);
	}
	ol {
		width: 100%;
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	li {
		display: grid;
		grid-template-columns: 1.25rem 1fr;
		align-items: baseline;
		column-gap: 6px;
		color: var(--muted);
	}
	li.running,
	li.done,
	li.failed {
		color: var(--text);
	}
	li.running .label {
		font-weight: 600;
	}
	.mark {
		text-align: center;
	}
	li.done .mark {
		color: var(--status-ok);
	}
	li.running .mark {
		color: var(--accent);
	}
	li.failed .mark {
		color: var(--status-error);
	}
	.download,
	pre {
		grid-column: 2;
	}
	.download {
		display: flex;
		flex-direction: column;
		gap: 4px;
		margin-top: 4px;
	}
	.download progress {
		width: 100%;
		accent-color: var(--accent);
	}
	pre {
		margin: 4px 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
	.notice-error pre {
		grid-column: auto;
	}
</style>
