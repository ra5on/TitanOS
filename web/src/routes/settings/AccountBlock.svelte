<script lang="ts">
	// §121: the phone's tab "Konto" – the own account on top of the settings'
	// areas, with logging out and, for owners and administrators, restarting
	// and shutting down the NAS (the known dialog of PowerDialog).
	import Icon from '#lib/components/Icon.svelte';
	import PowerDialog from '#lib/components/PowerDialog.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { logout, session } from '#lib/session.svelte.ts';

	let { admin }: { admin: boolean } = $props();
	let power = $state<'restart' | 'shutdown' | null>(null);
	const user = $derived(session.data?.user);
</script>

{#if user}
	<SettingGroup title={t('phone.tabs.account')} id="account-block">
		<SettingRow
			href="/account"
			icon="person"
			title={user.name}
			desc={t(`role.${user.role}` as MessageKey)}
		/>
		<div class="actions">
			<button type="button" class="btn" onclick={() => void logout()}
				><Icon name="logout" size={16} />{t('desktop.menu.logout')}</button
			>
			{#if admin}
				<button type="button" class="btn" onclick={() => (power = 'restart')}
					><Icon name="restart" size={16} />{t('desktop.menu.restartShort')}</button
				>
				<button type="button" class="btn btn-danger-outline" onclick={() => (power = 'shutdown')}
					><Icon name="power" size={16} />{t('desktop.menu.shutdownShort')}</button
				>
			{/if}
		</div>
	</SettingGroup>
{/if}
{#if power}<PowerDialog action={power} onclose={() => (power = null)} />{/if}

<style>
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		padding: 6px 0 2px;
	}
	.actions .btn {
		display: inline-flex;
		flex: 1 1 auto;
		justify-content: center;
		align-items: center;
		gap: 6px;
		min-height: 44px;
	}
	.btn-danger-outline {
		color: var(--error-text);
	}
</style>
