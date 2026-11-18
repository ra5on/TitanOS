<script lang="ts">
	// 1.11: T-Kalender (docs/KALENDER.md). Step 11 builds the frame and asks
	// titand whether the calendars can be read (GET /calendar/access); the
	// month, week and list views follow in step 15 in place of the body.
	import { api } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import ProgramShell from '#lib/pim/ProgramShell.svelte';
	import { calendarShell, shellOfError, type ShellState } from '#lib/pim/shell.ts';

	let shown = $state<ShellState>({ kind: 'loading' });
	$effect(() => {
		api
			.get<unknown>('/calendar/access')
			.then((access) => (shown = calendarShell(access)))
			.catch((err) => (shown = shellOfError(err)));
	});
</script>

<PageHead title={t('desktop.app.calendar')} />

<ProgramShell program="calendar" view={shown}>
	<div class="ready" data-pim-ready>
		<p class="text">{t('calendar.shell.text')}</p>
		<a class="btn" href="/calendar/iphone"
			><Icon name="device" size={16} />{t('calendar.side.iphone')}</a
		>
	</div>
</ProgramShell>

<style>
	.ready {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-3);
	}
	.ready p {
		margin: 0;
	}
</style>
