<script lang="ts">
	import { goto } from '$app/navigation';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { logout, session } from '#lib/session.svelte.ts';
	import Icon from './Icon.svelte';
	import LanguageSwitch from './LanguageSwitch.svelte';
	import PowerDialog from './PowerDialog.svelte';

	const user = $derived(session.data?.user);
	const initial = $derived(user?.name.charAt(0).toUpperCase() ?? '');
	const canAdminister = $derived(user?.role === 'owner' || user?.role === 'admin');

	let open = $state(false);
	let power = $state<'restart' | 'shutdown' | null>(null);
	let root = $state<HTMLElement>();

	function onWindowClick(event: MouseEvent) {
		if (open && !root?.contains(event.target as Node)) open = false;
	}

	function onKeydown(event: KeyboardEvent) {
		if (event.key === 'Escape') open = false;
	}

	function openAppearance() {
		open = false;
		goto('/settings/appearance');
	}

	function ask(action: 'restart' | 'shutdown') {
		open = false;
		power = action;
	}
</script>

<svelte:window onclick={onWindowClick} onkeydown={onKeydown} />

{#if user}
	<div class="account" bind:this={root}>
		<button type="button" class="trigger" class:open aria-haspopup="menu" aria-expanded={open} onclick={() => (open = !open)}>
			<span class="avatar" aria-hidden="true">{initial}</span>
			<span class="name">{user.name}</span>
			<Icon name="chevronDown" size={14} />
		</button>

		{#if open}
			<div class="menu floating-menu" role="menu" aria-label={t('menu.account')}>
				<div class="who">
					<span class="avatar large" aria-hidden="true">{initial}</span>
					<span class="lines">
						<span class="who-name">{user.name}</span>
						<span class="who-role">{t(`role.${user.role}`)} · {t(`role.${user.role}Hint`)}</span>
					</span>
				</div>
				<hr />
				<a role="menuitem" href="/account" onclick={() => (open = false)}><Icon name="key" />{t('menu.account')}</a>
				<button type="button" role="menuitem" onclick={openAppearance}><Icon name="palette" />{t('menu.appearance')}</button>
				<div class="language"><Icon name="globe" /><span>{t('lang.label')}</span><LanguageSwitch /></div>
				{#if canAdminister}
					<hr />
					<button type="button" role="menuitem" onclick={() => ask('restart')}><Icon name="restart" />{t('menu.restart')}</button>
					<button type="button" role="menuitem" onclick={() => ask('shutdown')}><Icon name="power" />{t('menu.shutdown')}</button>
				{/if}
				<hr />
				<button type="button" role="menuitem" onclick={logout}><Icon name="logout" />{t('menu.logout')}</button>
			</div>
		{/if}
	</div>
{/if}



{#if power}
	<PowerDialog action={power} onclose={() => (power = null)} />
{/if}

<style>
	.account {
		position: relative;
	}
	.trigger {
		height: 44px;
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 0 10px 0 6px;
		background: transparent;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.875rem;
		cursor: pointer;
	}
	.trigger.open,
	.trigger:hover {
		background: var(--chip);
	}
	.avatar {
		width: 30px;
		height: 30px;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: calc(var(--radius-ctl) * 0.8);
		background: var(--accent);
		color: var(--on-accent);
		font-size: 0.8125rem;
		font-weight: 700;
	}
	.avatar.large {
		width: 40px;
		height: 40px;
		border-radius: var(--radius-ctl);
		font-size: 1rem;
	}
	.menu {
		position: absolute;
		right: 0;
		top: 52px;
		z-index: 30;
		width: 300px;
		max-width: calc(100vw - 24px);
		display: flex;
		flex-direction: column;
		padding: 6px;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 10px 12px;
	}
	.lines {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.who-name {
		font-size: 0.9375rem;
		font-weight: 600;
	}
	.who-role {
		font-size: 0.75rem;
		color: var(--muted);
	}
	hr {
		height: 1px;
		margin: 6px 4px;
		border: 0;
		background: var(--border);
	}
	.menu button,
	.language {
		min-height: 40px;
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 0 10px;
		font-size: 0.875rem;
	}
	.menu button {
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.875rem;
		text-align: left;
		cursor: pointer;
	}
	.menu button:hover {
		background: var(--chip);
	}
	.language span {
		flex: 1 1 auto;
	}
	@media (max-width: 760px) {
		.name {
			display: none;
		}
	}
</style>
