<script lang="ts">
	// PLAN 67: Systemsteuerung › Darstellung › Anmeldeseite › Konten. How the
	// login page offers accounts: as circles or a list; all accounts of the
	// NAS (on by default) or only those of this browser; and whether the NAS
	// also shows them to someone outside the home network (off by default).
	// The fields belong to the login look: the editor saves them with it.
	import { api } from '#lib/api.ts';
	import Segments from '#lib/components/Segments.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { LoginLook } from './look.ts';

	let { look = $bindable() }: { look: LoginLook } = $props();

	// PLAN 69: each account on its own – shown or not (locked ones never are).
	type Account = { name: string; role: string; disabled?: boolean };
	let accounts = $state<Account[] | null>(null);
	$effect(() => {
		api.get<Account[]>('/users').then(
			(list) =>
				(accounts = (Array.isArray(list) ? list : []).toSorted((a, b) =>
					a.name.localeCompare(b.name),
				)),
			() => (accounts = []),
		);
	});
	function setShown(name: string, shown: boolean) {
		const rest = (look.hiddenAccounts ?? []).filter((n) => n !== name);
		look.hiddenAccounts = shown ? rest : [...rest, name].sort();
	}
	const roleKey = (role: string) =>
		role === 'owner' ? 'role.owner' : role === 'admin' ? 'role.admin' : 'role.user';

	const styles = $derived<{ value: LoginLook['accountsStyle']; label: string }[]>([
		{ value: 'circles', label: t('loginPick.circles') },
		{ value: 'list', label: t('loginPick.list') },
	]);
</script>

<!-- PLAN 78-1: a plain heading, every setting a row; the accounts are the
     parts of their row. -->
<div data-testid="login-accounts">
	<SettingGroup title={t('loginPick.title')} id="h-login-accounts">
		<SettingRow title={t('loginPick.style')} desc={t('loginPick.styleHint')}>
			{#snippet control()}
				<span class="previews" aria-hidden="true">
					<span class="mini circles" class:on={look.accountsStyle === 'circles'}
						><i></i><i></i><i></i></span
					>
					<span class="mini list" class:on={look.accountsStyle === 'list'}><i></i><i></i></span>
				</span>
				<Segments
					label={t('loginPick.style')}
					options={styles}
					value={look.accountsStyle}
					onchange={(v) => (look.accountsStyle = v)}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow
			title={t('loginPick.showAll')}
			desc={t('loginPick.showAllHint')}
			for="login-show-accounts"
		>
			{#snippet control()}
				<input
					id="login-show-accounts"
					type="checkbox"
					role="switch"
					class="switch"
					bind:checked={look.showAccounts}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow
			title={t('loginPick.external')}
			desc={t('loginPick.externalHint')}
			for="login-show-external"
		>
			{#snippet control()}
				<input
					id="login-show-external"
					type="checkbox"
					role="switch"
					class="switch"
					disabled={!look.showAccounts}
					bind:checked={look.showAccountsExternal}
				/>
			{/snippet}
		</SettingRow>
		{#if accounts?.length}
			<SettingRow
				id="login-accounts-each"
				title={t('loginPick.each')}
				desc={t('loginPick.eachHint')}
				open
			>
				<ul class="accounts" class:off={!look.showAccounts} data-testid="login-accounts-each">
					{#each accounts as a (a.name)}
						<li class="srow-sub">
							<span class="who">
								<span class="initial" aria-hidden="true">{a.name.slice(0, 1).toUpperCase()}</span>
								<span class="srow-text"
									><span class="srow-title">{a.name}</span><span class="srow-desc"
										>{t(roleKey(a.role))}{a.disabled ? ' · ' + t('loginPick.locked') : ''}</span
									></span
								>
							</span>
							<span class="srow-ctl">
								<input
									type="checkbox"
									role="switch"
									class="switch"
									aria-label={t('loginPick.showNamed', { name: a.name })}
									disabled={!look.showAccounts || a.disabled}
									checked={!a.disabled && !(look.hiddenAccounts ?? []).includes(a.name)}
									onchange={(e) => setShown(a.name, e.currentTarget.checked)}
								/>
							</span>
						</li>
					{/each}
				</ul>
			</SettingRow>
		{/if}
		<p class="group-note">{t('loginPick.note')}</p>
	</SettingGroup>
</div>

<style>
	.previews {
		display: inline-flex;
		gap: var(--space-1-5);
	}
	.mini {
		width: 46px;
		height: 30px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 3px;
		border: 1px solid var(--border);
		border-radius: 6px;
		background: var(--bg);
		opacity: 0.6;
	}
	.mini.on {
		border-color: var(--accent);
		opacity: 1;
	}
	.mini i {
		display: block;
		background: var(--muted);
	}
	.circles i {
		width: 9px;
		height: 9px;
		border-radius: 50%;
	}
	.list {
		flex-direction: column;
	}
	.list i {
		width: 30px;
		height: 6px;
		border-radius: 2px;
	}
	.off {
		opacity: 0.6;
	}
	.accounts {
		display: flex;
		flex-direction: column;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.accounts li + li {
		border-top: 1px solid var(--srow-line);
	}
	.who {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		flex: 1 1 0;
		min-width: 0;
	}
	.initial {
		display: grid;
		place-items: center;
		flex: none;
		width: 30px;
		height: 30px;
		border-radius: 50%;
		background: color-mix(in srgb, var(--accent) 28%, var(--surface));
		color: var(--text);
		font-weight: 600;
		font-size: var(--text-sm);
	}
	.group-note {
		margin: var(--space-1-5) 0 0;
		color: var(--muted);
		font-size: var(--text-sm);
	}
</style>
