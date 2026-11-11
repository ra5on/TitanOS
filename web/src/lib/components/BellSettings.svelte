<script lang="ts">
	// Which messages this account's bell shows. The NAS keeps all of them;
	// mail and push follow the shared settings. One row of "Mein Konto"
	// (PLAN 78-5, anchor #bell): the level and the areas are its parts.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { bell } from '#lib/notifications.svelte.ts';
	import { bellGroups, sanitizeBell } from '#lib/notify-groups.ts';
	import SettingRow from './settings/SettingRow.svelte';
	import SettingSub from './settings/SettingSub.svelte';

	let draft = $state(sanitizeBell(bell.settings));
	let error = $state('');
	let saved = $state(false);
	let busy = $state(false);

	// What the row says closed: the level the bell shows from (as stored).
	const summary = $derived(t(`notify.min.${bell.settings.minSeverity}`));

	async function save() {
		busy = true;
		error = '';
		saved = false;
		try {
			const value = sanitizeBell(draft);
			await api.put('/preferences/bell', value);
			bell.settings = value;
			saved = true;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<SettingRow id="bell" icon="bell" title={t('bell.title')} desc={t('bell.intro')} {summary}>
	<SettingSub title={t('bell.minSeverity')} for="bell-min">
		{#snippet control()}
			<select
				id="bell-min"
				class="field"
				bind:value={draft.minSeverity}
				onchange={() => (saved = false)}
			>
				<option value="info">{t('notify.min.info')}</option>
				<option value="warning">{t('notify.min.warning')}</option>
				<option value="error">{t('notify.min.error')}</option>
			</select>
		{/snippet}
	</SettingSub>
	<div class="srow-block"><p class="muted" id="bell-groups">{t('bell.groups')}</p></div>
	{#each bellGroups as group}
		<SettingSub title={t(`bell.group.${group}`)} for="bell-group-{group}">
			{#snippet control()}
				<input
					id="bell-group-{group}"
					class="switch"
					type="checkbox"
					role="switch"
					aria-describedby="bell-groups"
					bind:checked={draft.groups[group]}
					onchange={() => (saved = false)}
				/>
			{/snippet}
		</SettingSub>
	{/each}
	<div class="srow-block"><p class="muted">{t('bell.storageErrors')}</p></div>
	<div class="srow-block actions">
		<button class="btn btn-primary" type="button" onclick={save} disabled={busy}
			>{t('common.save')}</button
		>
		{#if saved}<span role="status">{t('notify.saved')}</span>{/if}
	</div>
	{#if error}<div class="srow-block">
			<p class="notice-error" role="alert">{error}</p>
		</div>{/if}
</SettingRow>
