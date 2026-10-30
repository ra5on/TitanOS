<script lang="ts">
	// Docker › Automatic updates: which apps take newer images by themselves
	// and when. After an update TitanOS watches the app and goes back to the
	// previous images if it does not run properly.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	type Result = {
		at: string;
		outcome: 'updated' | 'rolledBack' | 'failed' | 'current';
		detail?: string;
	};
	type Status = {
		settings: { apps: string[]; weekdays: number[]; time: string };
		results: Record<string, Result>;
		next?: string;
	};

	let { apps, onclose }: { apps: string[]; onclose: () => void } = $props();
	let status = $state<Status | null>(null);
	let chosen = $state<string[]>([]);
	let weekdays = $state<number[]>([]);
	let time = $state('04:00');
	let busy = $state(false);
	let error = $state('');

	function take(next: Status) {
		status = next;
		chosen = [...next.settings.apps];
		weekdays = [...next.settings.weekdays];
		time = next.settings.time;
	}

	onMount(async () => {
		try {
			take(await api.get<Status>('/docker/auto-update'));
		} catch (err) {
			error = apiErrorText(err);
		}
	});

	async function save() {
		busy = true;
		error = '';
		try {
			take(await api.put<Status>('/docker/auto-update', { apps: chosen, weekdays, time }));
			onclose();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	const days = [1, 2, 3, 4, 5, 6, 0];
	const toggle = <T,>(list: T[], value: T, on: boolean) =>
		on ? [...list, value] : list.filter((v) => v !== value);
</script>

<Dialog title={t('autoUpdate.title')} {onclose}>
	<p class="muted">{t('autoUpdate.intro')}</p>
	{#if status}
		<fieldset>
			<legend>{t('autoUpdate.apps')}</legend>
			{#each apps as app (app)}
				{@const result = status.results[app]}
				<label class="check app"
					><input
						type="checkbox"
						checked={chosen.includes(app)}
						disabled={busy}
						onchange={(e) => (chosen = toggle(chosen, app, e.currentTarget.checked))}
					/><span>{app}</span>
					{#if result}<span class="muted small"
							>{t(`autoUpdate.outcome.${result.outcome}` as MessageKey)} · {formatDateTime(
								new Date(result.at),
							)}{result.detail ? ` · ${result.detail}` : ''}</span
						>{/if}</label
				>
			{:else}
				<p class="muted">{t('autoUpdate.noApps')}</p>
			{/each}
		</fieldset>
		<fieldset>
			<legend>{t('autoUpdate.window')}</legend>
			<div class="days">
				{#each days as day (day)}
					<label class="check"
						><input
							type="checkbox"
							checked={weekdays.includes(day)}
							disabled={busy}
							onchange={(e) => (weekdays = toggle(weekdays, day, e.currentTarget.checked))}
						/><span>{t(`report.day.${day}` as MessageKey).slice(0, 2)}</span></label
					>
				{/each}
			</div>
			<label class="stack narrow"
				><span>{t('autoUpdate.time')}</span><input
					class="field"
					type="time"
					bind:value={time}
					disabled={busy}
				/></label
			>
			<p class="muted small">
				{t(weekdays.length ? 'autoUpdate.windowHint' : 'autoUpdate.everyDay')}
			</p>
			{#if status.next}<p class="muted small">
					{t('autoUpdate.next', { time: formatDateTime(new Date(status.next)) })}
				</p>{/if}
		</fieldset>
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
		<button type="button" class="btn btn-primary" disabled={busy || !status} onclick={save}
			>{t('common.save')}</button
		>
	</div>
</Dialog>

<style>
	fieldset {
		border: 0;
		margin: 0 0 12px;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	legend {
		font-weight: 500;
		margin-bottom: 4px;
	}
	.app {
		flex-wrap: wrap;
	}
	.days {
		display: flex;
		gap: 10px;
		flex-wrap: wrap;
	}
	.narrow input {
		width: 8rem;
	}
</style>
