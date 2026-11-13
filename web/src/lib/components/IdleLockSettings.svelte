<script lang="ts">
	// PLAN 84-2 (owner 13.11.2026): the automatic lock of one's own account
	// under Settings › Access, next to "Angemeldet bleiben" – the same choice
	// as in the desktop's settings, on the computer and on the phone, each
	// with "nie".
	import { onMount } from 'svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import {
		connectLock,
		LOCK_CHOICES,
		LOCK_CHOICES_PHONE,
		lockLink,
		setLock,
	} from '#lib/desktop/lock-link.svelte.ts';
	import SettingRow from './settings/SettingRow.svelte';
	import SettingSub from './settings/SettingSub.svelte';

	onMount(connectLock);

	const label = (m: number) =>
		m === 0 ? t('desktop.settings.never') : t('desktop.settings.minutes', { count: m });
	const choicesWith = (list: number[], value: number | null) =>
		value === null || list.includes(value) ? list : [...list, value].sort((a, b) => a - b);
	const summary = $derived(
		lockLink.minutes === null
			? undefined
			: t('idleLock.summary', {
					computer: label(lockLink.minutes),
					phone: label(lockLink.phone ?? 0),
				}),
	);
</script>

<SettingRow
	id="access-idle-lock"
	icon="lock"
	title={t('idleLock.title')}
	desc={t('idleLock.hint')}
	{summary}
>
	{#if lockLink.minutes !== null}
		<SettingSub
			title={t('desktop.settings.lock')}
			desc={t('desktop.settings.lockHint')}
			for="access-idle-lock-pc"
		>
			{#snippet control()}
				<select
					id="access-idle-lock-pc"
					class="field"
					value={lockLink.minutes}
					onchange={(e) => setLock(Number(e.currentTarget.value), lockLink.phone ?? 0)}
				>
					{#each choicesWith(LOCK_CHOICES, lockLink.minutes) as m (m)}<option value={m}
							>{label(m)}</option
						>{/each}
				</select>
			{/snippet}
		</SettingSub>
		<SettingSub
			title={t('desktop.settings.lockPhone')}
			desc={t('desktop.settings.lockPhoneHint')}
			for="access-idle-lock-phone"
		>
			{#snippet control()}
				<select
					id="access-idle-lock-phone"
					class="field"
					value={lockLink.phone ?? 0}
					onchange={(e) => setLock(lockLink.minutes ?? 0, Number(e.currentTarget.value))}
				>
					{#each choicesWith(LOCK_CHOICES_PHONE, lockLink.phone) as m (m)}<option value={m}
							>{label(m)}</option
						>{/each}
				</select>
			{/snippet}
		</SettingSub>
	{/if}
</SettingRow>
