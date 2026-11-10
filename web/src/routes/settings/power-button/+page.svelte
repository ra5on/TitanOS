<script lang="ts">
	import { api } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	type Action = 'ignore' | 'reboot' | 'poweroff';
	type Status = { settings: { action: Action }; readOnly: boolean; error?: string };
	let status = $state<Status | null>(null);
	let action = $state<Action>('poweroff');
	let busy = $state(false),
		error = $state(''),
		saved = $state(false);
	function take(result: Status) {
		status = result;
		action = result.settings.action;
	}
	function fail(err: unknown) {
		error = apiErrorText(err, { detail: true });
	}
	$effect(() => {
		api.get<Status>('/system/power-button').then(take, fail);
	});
	// PLAN 78-1: the one choice is stored when it changes (same request as
	// the Save button sent before).
	async function save() {
		busy = true;
		saved = false;
		error = '';
		try {
			take(await api.put<Status>('/system/power-button', { action }));
			saved = true;
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}
</script>

<PageHead title={t('powerButton.title')}>
	<p class="muted">{t('powerButton.intro')}</p>
</PageHead>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if status}
	{#if status.error}<p class="notice-warn" role="status">{status.error}</p>{/if}
	{#if status.readOnly}<p class="notice-warn">{t('energy.readOnly')}</p>{/if}
	<SettingGroup>
		<SettingRow icon="power" title={t('powerButton.press')} for="power-action">
			{#snippet description()}
				<p>{t('powerButton.shutdownHint')}</p>
				<p>{t('powerButton.longPress')}</p>
			{/snippet}
			{#snippet control()}
				<select
					id="power-action"
					class="field"
					bind:value={action}
					disabled={busy || status?.readOnly}
					onchange={save}
				>
					{#each ['ignore', 'reboot', 'poweroff'] as const as value (value)}<option {value}
							>{t(`powerButton.${value}`)}</option
						>{/each}
				</select>
			{/snippet}
		</SettingRow>
		{#if saved}<p class="muted" role="status">{t('powerButton.saved')}</p>{/if}
	</SettingGroup>
{:else if !error}<p>{t('common.loading')}</p>{/if}

<style>
	p {
		margin: 0;
	}
	.notice-error {
		white-space: pre-wrap;
	}
</style>
