<script lang="ts">
	// Settings › Notifications › Health report: a regular summary of the NAS
	// through the channels above (backend package healthreport).
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	type Settings = {
		enabled: boolean;
		every: 'weekly' | 'monthly';
		weekday: number;
		day: number;
		time: string;
	};
	type Section = { title: string; status: 'ok' | 'warning' | 'error'; lines: string[] };
	type Answer = {
		settings: Settings;
		next?: string;
		preview: { title: string; status: string; sections: Section[] };
	};

	let answer = $state<Answer | null>(null);
	let draft = $state<Settings | null>(null);
	let busy = $state(false);
	let error = $state('');
	let message = $state('');
	let showPreview = $state(false);

	function take(next: Answer) {
		answer = next;
		draft = { ...next.settings };
	}

	onMount(async () => {
		try {
			take(await api.get<Answer>('/health-report'));
		} catch (err) {
			error = apiErrorText(err);
		}
	});

	async function run(action: () => Promise<Answer>, done: MessageKey) {
		busy = true;
		error = message = '';
		try {
			take(await action());
			message = t(done);
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	function save() {
		const body = draft;
		if (body) void run(() => api.put<Answer>('/health-report', body), 'ui.saved');
	}
	const send = () => run(() => api.post<Answer>('/health-report/send', {}), 'report.sent');
	const days = [1, 2, 3, 4, 5, 6, 0];
	// What is stored, in a few words, at the right of the row.
	const summary = $derived.by(() => {
		const stored = answer?.settings;
		if (!stored) return undefined;
		if (!stored.enabled) return t('report.off');
		return `${t(stored.every === 'weekly' ? 'report.weekly' : 'report.monthly')} · ${stored.time}`;
	});
	const mark = { ok: '✔', warning: '⚠', error: '✖' };
</script>

<!-- PLAN 78-1: one row that opens to its parts; the parts are one form with
     its own Save (same request as before). -->
<SettingRow
	id="health-report"
	icon="activity"
	title={t('report.title')}
	desc={t('report.intro')}
	{summary}
>
	{#if draft}
		{@const d = draft}
		<SettingSub title={t('report.enabled')} for="report-enabled">
			{#snippet control()}
				<input
					id="report-enabled"
					class="switch"
					type="checkbox"
					role="switch"
					bind:checked={d.enabled}
					disabled={busy}
				/>
			{/snippet}
		</SettingSub>
		<SettingSub title={t('report.every')} for="report-every">
			{#snippet control()}
				<select id="report-every" class="field" bind:value={d.every} disabled={busy}
					><option value="weekly">{t('report.weekly')}</option><option value="monthly"
						>{t('report.monthly')}</option
					></select
				>
			{/snippet}
		</SettingSub>
		{#if d.every === 'weekly'}
			<SettingSub title={t('report.weekday')} for="report-weekday">
				{#snippet control()}
					<select id="report-weekday" class="field" bind:value={d.weekday} disabled={busy}
						>{#each days as day (day)}<option value={day}
								>{t(`report.day.${day}` as MessageKey)}</option
							>{/each}</select
					>
				{/snippet}
			</SettingSub>
		{:else}
			<SettingSub title={t('report.dayOfMonth')} for="report-day">
				{#snippet control()}
					<input
						id="report-day"
						class="field"
						type="number"
						min="1"
						max="28"
						bind:value={d.day}
						disabled={busy}
					/>
				{/snippet}
			</SettingSub>
		{/if}
		<SettingSub
			title={t('report.time')}
			for="report-time"
			desc={answer?.next
				? t('report.next', { time: formatDateTime(new Date(answer.next)) })
				: undefined}
		>
			{#snippet control()}
				<input id="report-time" class="field" type="time" bind:value={d.time} disabled={busy} />
			{/snippet}
		</SettingSub>
		{#if error}<div class="srow-block"><p class="notice-error" role="alert">{error}</p></div>{/if}
		<div class="srow-block actions">
			<button type="button" class="btn btn-primary" disabled={busy} onclick={save}
				>{t('common.save')}</button
			>
			<button type="button" class="btn" disabled={busy} onclick={send}>{t('report.sendNow')}</button
			>
			<button
				type="button"
				class="btn"
				onclick={() => (showPreview = !showPreview)}
				aria-expanded={showPreview}>{t('report.preview')}</button
			>
			{#if message}<span class="muted" role="status">{message}</span>{/if}
		</div>
		{#if showPreview && answer}
			<div class="srow-block">
				<div class="preview">
					<strong>{answer.preview.title}</strong>
					{#each answer.preview.sections as section (section.title)}
						<div class="section {section.status}">
							<span class="title"
								><span aria-hidden="true">{mark[section.status]}</span> {section.title}</span
							>
							{#each section.lines as line, i (i)}<span class="line">{line}</span>{/each}
						</div>
					{/each}
				</div>
			</div>
		{/if}
	{:else if error}
		<div class="srow-block"><p class="notice-error" role="alert">{error}</p></div>
	{:else}
		<div class="srow-block"><p class="loading" role="status">{t('common.loading')}</p></div>
	{/if}
</SettingRow>

<style>
	.preview {
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
		padding: var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	.section {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.title {
		font-weight: 500;
	}
	.warning .title {
		color: var(--warn-text);
	}
	.error .title {
		color: var(--error-text);
	}
	.line {
		padding-left: 1.4em;
		font-size: var(--text-base);
	}
</style>
