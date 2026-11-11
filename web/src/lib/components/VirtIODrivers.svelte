<script lang="ts">
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import { poll } from '#lib/poll.ts';
	import { untrack } from 'svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	import type { MediaJob } from '#lib/media.ts';
	let {
		volume,
		initial = '',
		defaultAuto = true,
		disabled = false,
		onready,
		onbusy,
	}: {
		volume: string;
		initial?: string;
		defaultAuto?: boolean;
		disabled?: boolean;
		onready: (path: string) => void;
		onbusy: (busy: boolean) => void;
	} = $props();
	let automatic = $state(untrack(() => defaultAuto)),
		path = $state(untrack(() => initial)),
		job = $state<MediaJob | null>(null),
		error = $state(''),
		retry = $state(0);
	$effect(() => {
		const dest = volume;
		const auto = automatic;
		retry;
		let alive = true;
		let stopPolling: (() => void) | undefined;
		if (!auto || !dest || disabled) {
			onbusy(false);
			onready(path);
			return;
		}
		onbusy(true);
		onready('');
		error = '';
		job = null;
		let polling = false;
		async function refresh() {
			if (polling || !alive) return;
			polling = true;
			try {
				if (!job) {
					const result = await api.post<MediaJob>('/vms/virtio', { volume: dest });
					if (!alive) return;
					job = result;
				} else {
					const res = await api.get<{ jobs: MediaJob[] }>('/vms/imports');
					if (!alive) return;
					job = res.jobs.find((j) => j.id === job?.id) ?? job;
				}
				if (job?.phase === 'done' && job.output) {
					path = job.output;
					onready(path);
					onbusy(false);
					stopPolling?.();
				} else if (job && ['failed', 'review', 'cancelled'].includes(job.phase)) {
					error = job.error ?? t('error.media_failed');
					stopPolling?.();
				}
			} catch (e) {
				if (alive) {
					error = apiErrorText(e);
					stopPolling?.();
				}
			} finally {
				polling = false;
			}
		}
		stopPolling = poll(refresh, { interval: 1500 });
		return () => {
			alive = false;
			stopPolling?.();
			onbusy(false);
		};
	});
</script>

{#snippet mediaLink()}<a href="/vms/media">{t('media.title')}</a>{/snippet}
{#snippet state()}
	{#if automatic && (!job || job.phase !== 'done')}<span class="state" role="status">
			{t('media.virtioLoading')}{#if job?.total}{' '}{formatBytes(job.bytes)} / {formatBytes(
					job.total,
				)}{/if}
		</span>{/if}
	{#if error}<span class="state failed" role="alert">{error}</span>{/if}
{/snippet}

<!-- PLAN 78-5: two rows of the form (the only user is the VM form). -->
<FormRow label={t('media.virtioAuto')} id="virtio-auto" hint={t('media.virtioHint')} more={state}>
	<div class="line">
		{#if error}<button type="button" class="btn" onclick={() => retry++}
				>{t('media.virtioRetry')}</button
			>{/if}
		<input
			id="virtio-auto"
			class="switch"
			type="checkbox"
			role="switch"
			bind:checked={automatic}
			{disabled}
		/>
	</div>
</FormRow>
<FormRow label={t('media.driverISO')} id="virtio-iso" more={mediaLink}>
	<div class="line">
		<input
			id="virtio-iso"
			class="field mono"
			bind:value={path}
			readonly={automatic}
			{disabled}
			oninput={() => onready(path)}
		/>
	</div>
</FormRow>

<style>
	.state {
		display: block;
		color: var(--text);
	}
	.state.failed {
		color: var(--error-text);
	}
</style>
