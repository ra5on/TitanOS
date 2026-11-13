<script lang="ts">
	// PLAN 82-3: a camera may send e-mail to an account's own address. An
	// owner or administrator enters it under "Meine Benachrichtigungen"; an
	// account with the role "Benutzer" has no such page, so this one row on
	// its account page takes the address. It shows only for an account
	// that may see at least one camera (only then can it be a recipient).
	import { onMount } from 'svelte';
	import { api, ApiError } from '../api.ts';
	import SettingRow from '../components/settings/SettingRow.svelte';
	import { apiErrorText, t } from '../i18n/index.svelte.ts';

	type Answer = {
		settings: Record<string, unknown> & { email: { enabled: boolean; to: string } };
		mailServer: boolean;
	};

	let shown = $state(false);
	let answer = $state<Answer | null>(null);
	let address = $state('');
	let busy = $state(false);
	let saved = $state(false);
	let error = $state('');

	onMount(async () => {
		try {
			const list = await api.get<{ cameras: unknown[] }>('/nvr/cameras');
			if (!list.cameras?.length) return;
			answer = await api.get<Answer>('/account/notifications');
			address = answer.settings.email?.to ?? '';
			shown = true;
		} catch {
			// No NVR, no camera or no answer: the row stays away.
		}
	});

	async function save() {
		if (!answer) return;
		busy = true;
		saved = false;
		error = '';
		try {
			const settings = {
				...answer.settings,
				email: { ...answer.settings.email, to: address.trim() },
			};
			answer = await api.put<Answer>('/account/notifications', settings);
			address = answer.settings.email?.to ?? '';
			saved = true;
		} catch (err) {
			error = err instanceof ApiError ? apiErrorText(err) : String(err);
		} finally {
			busy = false;
		}
	}
</script>

{#if shown && answer}
	<SettingRow id="camera-mail" icon="send" title={t('nvr.account.mail')} for="camera-mail-input">
		{#snippet description()}
			<p>{t('nvr.account.mailHint')}</p>
			{#if !answer?.mailServer}<p class="warn">{t('nvr.account.mailNoServer')}</p>{/if}
			{#if saved}<p role="status">{t('nvr.account.mailSaved')}</p>{/if}
			{#if error}<p class="bad" role="alert">{error}</p>{/if}
		{/snippet}
		{#snippet control()}
			<input
				id="camera-mail-input"
				class="field mail"
				type="email"
				autocomplete="email"
				placeholder={t('nvr.account.mailPlaceholder')}
				bind:value={address}
				oninput={() => (saved = false)}
			/>
			<button type="button" class="btn" disabled={busy} onclick={save}>{t('common.save')}</button>
		{/snippet}
	</SettingRow>
{/if}

<style>
	.mail {
		width: min(18rem, 100%);
	}
	.warn {
		color: var(--warn-text);
	}
	.bad {
		color: var(--error-text);
	}
</style>
