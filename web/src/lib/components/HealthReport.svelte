<script lang="ts">
	// Settings › Notifications › Health report: a regular summary of the NAS
	// through the channels above (backend package healthreport).
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	type Settings = { enabled: boolean; every: 'weekly' | 'monthly'; weekday: number; day: number; time: string };
	type Section = { title: string; status: 'ok' | 'warning' | 'error'; lines: string[] };
	type Answer = { settings: Settings; next?: string; preview: { title: string; status: string; sections: Section[] } };

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
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	});

	async function run(action: () => Promise<Answer>, done: MessageKey) {
		busy = true;
		error = message = '';
		try {
			take(await action());
			message = t(done);
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
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
	const mark = { ok: '✔', warning: '⚠', error: '✖' };
</script>

<section class="tile panel" id="health-report" aria-labelledby="health-report-title">
	<h2 class="tile-title" id="health-report-title">{t('report.title')}</h2>
	<p class="muted">{t('report.intro')}</p>
	{#if draft}
		<label class="check"><input type="checkbox" bind:checked={draft.enabled} disabled={busy} /><span>{t('report.enabled')}</span></label>
		<div class="row">
			<label class="stack"
				><span>{t('report.every')}</span><select class="field" bind:value={draft.every} disabled={busy}
					><option value="weekly">{t('report.weekly')}</option><option value="monthly">{t('report.monthly')}</option></select
				></label
			>
			{#if draft.every === 'weekly'}
				<label class="stack"
					><span>{t('report.weekday')}</span><select class="field" bind:value={draft.weekday} disabled={busy}
						>{#each days as day (day)}<option value={day}>{t(`report.day.${day}` as MessageKey)}</option>{/each}</select
					></label
				>
			{:else}
				<label class="stack"
					><span>{t('report.dayOfMonth')}</span><input class="field" type="number" min="1" max="28" bind:value={draft.day} disabled={busy} /></label
				>
			{/if}
			<label class="stack"><span>{t('report.time')}</span><input class="field" type="time" bind:value={draft.time} disabled={busy} /></label>
		</div>
		{#if answer?.next}<p class="muted small">{t('report.next', { time: formatDateTime(new Date(answer.next)) })}</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if message}<p class="muted" role="status">{message}</p>{/if}
		<div class="row-actions">
			<button type="button" class="btn btn-primary" disabled={busy} onclick={save}>{t('common.save')}</button>
			<button type="button" class="btn" disabled={busy} onclick={send}>{t('report.sendNow')}</button>
			<button type="button" class="btn" onclick={() => (showPreview = !showPreview)} aria-expanded={showPreview}>{t('report.preview')}</button>
		</div>
		{#if showPreview && answer}
			<div class="preview">
				<strong>{answer.preview.title}</strong>
				{#each answer.preview.sections as section (section.title)}
					<div class="section {section.status}">
						<span class="title"><span aria-hidden="true">{mark[section.status]}</span> {section.title}</span>
						{#each section.lines as line, i (i)}<span class="line">{line}</span>{/each}
					</div>
				{/each}
			</div>
		{/if}
	{:else if error}
		<p class="notice-error" role="alert">{error}</p>
	{/if}
</section>

<style>
	.row {
		display: flex;
		gap: 12px;
		flex-wrap: wrap;
	}
	.preview {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 12px;
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
		font-size: 0.8125rem;
	}
</style>
