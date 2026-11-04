<script lang="ts">
	// The last ten runs of a script with their output, as kept on the volume.
	import { api } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { outcomeTone, shortDuration, type ScriptRun, type ScriptView } from './scripts.ts';

	type Props = { script: ScriptView; onclose: () => void };
	let { script, onclose }: Props = $props();

	let runs = $state<ScriptRun[] | null>(null);
	let error = $state('');
	$effect(() => {
		api
			.get<{ runs: ScriptRun[] }>(`/scripts/${script.id}/runs`)
			.then((body) => (runs = body.runs))
			.catch((err) => (error = apiErrorText(err)));
	});
</script>

<Dialog title={t('scripts.runsOf', { name: script.name })} wide {onclose}>
	<p class="muted">{t('scripts.runsHint')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if runs === null && !error}<p class="loading">{t('scripts.loading')}</p>{/if}
	{#if runs}<ul class="runs">
			{#each runs as run, i (run.started + i)}<li>
					<details>
						<summary
							><span class="chip {outcomeTone(run.outcome)}"
								>{t(
									`scripts.outcome.${run.outcome}` as MessageKey,
								)}{#if run.exit !== undefined && run.outcome === 'failed'}
									· {t('scripts.exit', { code: run.exit })}{/if}</span
							>
							<span>{formatDateTime(new Date(run.started))}</span>
							<span class="muted"
								>{t(`scripts.trigger.${run.trigger}` as MessageKey)}{#if run.durationMs}
									· {shortDuration(run.durationMs)}{/if}</span
							></summary
						>
						{#if run.error}<p class="notice-warn">{run.error}</p>{/if}
						{#if run.truncated}<p class="muted">{t('scripts.outputTruncated')}</p>{/if}
						<pre class="mono">{run.output || t('scripts.noOutput')}</pre>
					</details>
				</li>{:else}<li class="muted">{t('scripts.neverRun')}</li>{/each}
		</ul>{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('common.close')}</button>
	</div>
</Dialog>

<style>
	.runs {
		list-style: none;
		margin: 0 0 var(--space-3);
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	summary {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		cursor: pointer;
	}
	details p {
		margin: var(--space-2) 0 0;
	}
	pre {
		margin: var(--space-2) 0 0;
		max-height: 18rem;
		overflow: auto;
		padding: var(--space-3);
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
