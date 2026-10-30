<script lang="ts">
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

<div class="drivers">
	<label class="check"
		><input type="checkbox" bind:checked={automatic} {disabled} /><span
			>{t('media.virtioAuto')}</span
		></label
	><small class="muted">{t('media.virtioHint')}</small
	>{#if automatic && (!job || job.phase !== 'done')}<p role="status">
			{t('media.virtioLoading')}{#if job?.total}
				{formatBytes(job.bytes)} / {formatBytes(job.total)}{/if}
		</p>{/if}{#if error}<p class="notice-error" role="alert">{error}</p>
		<button type="button" class="btn" onclick={() => retry++}>{t('media.virtioRetry')}</button
		>{/if}<label
		><span>{t('media.driverISO')}</span><input
			class="field mono"
			bind:value={path}
			readonly={automatic}
			{disabled}
			oninput={() => onready(path)}
		/></label
	><a href="/vms/media">{t('media.title')}</a>
</div>

<style>
	.drivers {
		display: grid;
		gap: var(--gap);
		min-width: 0;
	}
	.check {
		display: flex;
		align-items: center;
		gap: 8px;
		min-height: 44px;
	}
	.check input {
		width: 18px;
		height: 18px;
	}
	.drivers > label:not(.check) {
		display: grid;
		gap: 8px;
	}
	.drivers input {
		min-width: 0;
	}
</style>
