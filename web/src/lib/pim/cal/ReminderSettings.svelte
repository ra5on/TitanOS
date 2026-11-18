<script lang="ts">
	// 1.13 (docs/KALENDER.md "Hinweise zu Erinnerungen"): how TitanOS tells
	// the account of an event's reminder – a hint in TitanOS while it is open
	// (on by default) and a push to the account's devices (off by default:
	// the iPhone alerts on its own). Read when opened, saved at once (the
	// preference kind "calendar"; the NAS keeps the hidden calendars apart).
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	let { phone = false }: { phone?: boolean } = $props();
	let hints = $state(true);
	let push = $state(false);
	let read = $state(false);
	let error = $state('');

	async function open(e: Event) {
		if (read || !(e.currentTarget as HTMLDetailsElement).open) return;
		try {
			const prefs = await api.get<{
				calendar?: { reminderHints?: unknown; reminderPush?: unknown } | null;
			}>('/preferences');
			hints = prefs?.calendar?.reminderHints !== false;
			push = prefs?.calendar?.reminderPush === true;
			read = true;
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	async function save() {
		error = '';
		try {
			await api.put('/preferences/calendar', { reminderHints: hints, reminderPush: push });
		} catch (err) {
			error = apiErrorText(err);
		}
	}
</script>

<details class="rs" class:phone ontoggle={open} data-reminder-settings>
	<summary>{t('calendar.reminder.settings')}</summary>
	<label class="check"
		><input type="checkbox" bind:checked={hints} disabled={!read} onchange={save} />{t(
			'calendar.reminder.hints',
		)}</label
	>
	<label class="check"
		><input type="checkbox" bind:checked={push} disabled={!read} onchange={save} />{t(
			'calendar.reminder.push',
		)}</label
	>
	<small class="muted">{t('calendar.reminder.note')}</small>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
</details>

<style>
	.rs {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		font-size: var(--text-sm);
	}
	summary {
		min-height: 32px;
		display: flex;
		align-items: center;
		cursor: pointer;
		font-weight: 600;
	}
	.phone summary,
	.phone .check {
		min-height: 44px;
	}
	.check {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		min-height: 32px;
		padding: var(--space-1) 0;
		overflow-wrap: break-word;
	}
	.check input {
		flex: none;
		margin-top: 2px;
		accent-color: var(--accent);
	}
	small {
		display: block;
	}
</style>
