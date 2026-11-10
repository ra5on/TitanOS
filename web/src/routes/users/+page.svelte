<script lang="ts">
	import { api, type Account, type Role, type Share } from '#lib/api.ts';
	import AccountDetail from './AccountDetail.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import PasswordDialog from '#lib/components/PasswordDialog.svelte';
	import { apiErrorText, errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import {
		ACCOUNT_STATUS_FILTERS,
		filterAccounts,
		mayLock,
		type AccountStatusFilter,
	} from '#lib/users-view.ts';

	const ROLES: Role[] = ['owner', 'admin', 'user'];
	const NAME = /^[a-z][a-z0-9_-]{0,31}$/;
	const MIN_PASSWORD_LENGTH = 10;

	const me = $derived(session.data?.user);
	let accounts = $state<Account[] | null>(null);
	// Which shares each account reaches; read from the list of shares.
	let shares = $state<Share[] | null>(null);
	let loading = $state(false);
	let loadError = $state('');
	let error = $state('');
	let dialog = $state<
		| { kind: 'create' }
		| { kind: 'password' | 'delete'; name: string }
		| { kind: 'twoFactor'; account: Account }
		| { kind: 'role'; account: Account; role: Role }
		| { kind: 'lock'; account: Account; disabled: boolean }
		| null
	>(null);

	// The new account being typed in.
	let draft = $state({ name: '', role: 'user' as Role, password: '', repeat: '' });
	let working = $state(false);
	let dialogError = $state('');

	/** Owners are only managed by owners. */
	function mayManage(role: Role): boolean {
		return me?.role === 'owner' || (me?.role === 'admin' && role !== 'owner');
	}
	const offeredRoles = $derived(ROLES.filter(mayManage));
	const date = $derived(new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium' }));
	// An account from before the field existed (or test data) has no date: a dash, never a crash.
	const day = (value?: string) => {
		const d = value ? new Date(value) : null;
		return d && !Number.isNaN(d.getTime()) ? date.format(d) : '–';
	};
	const dateTime = $derived(
		new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium', timeStyle: 'short' }),
	);
	// The status filter lives only in this page visit (no write to the stick).
	let statusFilter = $state<AccountStatusFilter>('all');
	const shown = $derived(accounts ? filterAccounts(accounts, statusFilter) : []);
	const filterLabel: Record<
		AccountStatusFilter,
		'users.filterAll' | 'users.filterActive' | 'users.filterLocked'
	> = {
		all: 'users.filterAll',
		active: 'users.filterActive',
		locked: 'users.filterLocked',
	};
	function lockedTitle(account: Account): string {
		if (!account.disabledAt) return t('users.statusLocked');
		const when = dateTime.format(new Date(account.disabledAt));
		return account.disabledBy
			? t('users.lockedSince', { date: when, name: account.disabledBy })
			: t('users.lockedAt', { date: when });
	}

	async function load() {
		loading = true;
		loadError = '';
		const [users, list] = await Promise.allSettled([
			api.get<Account[]>('/users'),
			api.get<Share[]>('/shares'),
		]);
		if (users.status === 'fulfilled') accounts = users.value;
		else loadError = apiErrorText(users.reason);
		// Without the shares the page still works; the column says it is unknown.
		shares = list.status === 'fulfilled' ? list.value : null;
		loading = false;
	}
	const reach = (name: string) =>
		(shares ?? [])
			.filter((share) => share.access[name])
			.map((share) => ({ name: share.name, level: share.access[name] }))
			.sort((a, b) => a.name.localeCompare(b.name, i18n.locale));
	$effect(() => {
		load();
	});

	/** Making someone an owner gives full rights over everything, so it is asked first. */
	function pickRole(account: Account, role: Role, select: HTMLSelectElement) {
		if (role === 'owner' && account.role !== 'owner') {
			select.value = account.role;
			dialogError = '';
			dialog = { kind: 'role', account, role };
			return;
		}
		setRole(account, role, select);
	}

	async function setRole(account: Account, role: Role, select?: HTMLSelectElement) {
		if (working) return;
		working = true;
		error = '';
		dialogError = '';
		let failed = false;
		try {
			await api.put(`/users/${account.name}`, { role });
		} catch (err) {
			failed = true;
			const text = apiErrorText(err);
			if (dialog?.kind === 'role') dialogError = text;
			else error = text;
		}
		working = false;
		if (!failed && dialog?.kind === 'role') dialog = null;
		await load();
		// The select shows what the NAS has; a refused role is turned back
		// (the value bound to it may not have changed, so set it directly).
		if (select)
			select.value = accounts?.find((entry) => entry.name === account.name)?.role ?? account.role;
	}

	/** For someone who lost the phone and the recovery codes. */
	async function resetTwoFactor(account: Account) {
		if (working) return;
		working = true;
		dialogError = '';
		try {
			await api.put(`/users/${account.name}`, { role: account.role, resetTwoFactor: true });
			dialog = null;
		} catch (err) {
			dialogError = apiErrorText(err);
		} finally {
			working = false;
		}
		await load();
	}

	function openCreate() {
		draft = { name: '', role: 'user', password: '', repeat: '' };
		dialogError = '';
		dialog = { kind: 'create' };
	}

	async function create(event: SubmitEvent) {
		event.preventDefault();
		dialogError = '';
		const name = draft.name.trim().toLowerCase();
		if (!NAME.test(name)) return void (dialogError = errorText('invalid_name'));
		if ([...draft.password].length < MIN_PASSWORD_LENGTH)
			return void (dialogError = errorText('password_too_short'));
		if (draft.password !== draft.repeat) return void (dialogError = t('auth.passwordMismatch'));
		working = true;
		try {
			await api.post('/users', { name, password: draft.password, role: draft.role });
			dialog = null;
			await load();
		} catch (err) {
			dialogError = apiErrorText(err);
		}
		working = false;
	}

	/** Locks or unlocks; a refusal keeps the dialog open with the reason. */
	async function setLocked(account: Account, disabled: boolean) {
		if (working) return;
		working = true;
		dialogError = '';
		try {
			await api.post(`/users/${account.name}/${disabled ? 'lock' : 'unlock'}`);
			dialog = null;
		} catch (err) {
			dialogError = apiErrorText(err);
		} finally {
			working = false;
		}
		await load();
	}

	// A failed removal keeps the dialog open with the reason.
	async function remove(name: string) {
		if (working) return;
		working = true;
		dialogError = '';
		try {
			await api.delete(`/users/${name}`);
			dialog = null;
			await load();
		} catch (err) {
			dialogError = apiErrorText(err);
		} finally {
			working = false;
		}
	}

	// PLAN 75-20 (U2): the chosen account; at first one's own.
	let chosen = $state('');
	let showDetail = $state(false);
	let narrow = $state(matchMedia('(max-width: 720px)').matches);
	$effect(() => {
		const query = matchMedia('(max-width: 720px)');
		const change = () => (narrow = query.matches);
		query.addEventListener('change', change);
		return () => query.removeEventListener('change', change);
	});
	const current = $derived(
		shown.find((a) => a.name === chosen) ??
			shown.find((a) => a.name === me?.name) ??
			shown[0] ??
			null,
	);
	function choose(name: string) {
		chosen = name;
		showDetail = true;
	}
	function hue(name: string) {
		let h = 0;
		for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) % 360;
		return h;
	}

	// PLAN 67-1: the menu bar of the desktop; actions on one account list the accounts.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	const findAccount = (name?: string) => accounts?.find((a) => a.name === name);
	const pick = (ok: (a: Account) => boolean) => () =>
		(accounts ?? []).filter(ok).map((a) => ({ id: a.name, text: a.name }));
	const canManage = (a: Account) => mayManage(a.role);
	registerMenuCommands({
		'users.new': openCreate,
		'users.password': {
			items: pick((a) => canManage(a) || a.name === me?.name),
			run: (name) => name && (dialog = { kind: 'password', name }),
		},
		'users.lock': {
			items: () =>
				(accounts ?? [])
					.filter((a) => accounts && mayLock(me, a, accounts))
					.map((a) => ({
						id: a.name,
						text: a.disabled ? t('users.unlock') + ': ' + a.name : a.name,
					})),
			run: (name) => {
				const account = findAccount(name);
				if (!account) return;
				dialogError = '';
				dialog = { kind: 'lock', account, disabled: !account.disabled };
			},
		},
		'users.reset2fa': {
			items: pick((a) => a.twoFactor && canManage(a) && a.name !== me?.name),
			run: (name) => {
				const account = findAccount(name);
				if (!account) return;
				dialogError = '';
				dialog = { kind: 'twoFactor', account };
			},
		},
		'users.delete': {
			items: pick((a) => canManage(a) && a.name !== me?.name),
			run: (name) => {
				if (!name) return;
				dialogError = '';
				dialog = { kind: 'delete', name };
			},
		},
		...Object.fromEntries(
			ACCOUNT_STATUS_FILTERS.map((f) => [
				'users.filter.' + f,
				{ run: () => (statusFilter = f), checked: () => statusFilter === f },
			]),
		),
		'users.refresh': { run: load, enabled: () => !loading },
	});
</script>

<PageHead title={t('users.title')}>
	{#if accounts}<p class="muted">
			{accounts.length === 1
				? t('users.summaryOne')
				: t('users.summary', { count: accounts.length })}
		</p>{/if}
	{#snippet actions()}
		<button type="button" class="btn btn-primary" onclick={openCreate}>{t('users.new')}</button>
	{/snippet}
</PageHead>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if loadError}
	<div class="notice-error failed" role="alert">
		<span>{loadError}</span><button type="button" class="btn" disabled={loading} onclick={load}
			>{t('common.retry')}</button
		>
	</div>
{/if}

{#if accounts === null}
	{#if !loadError}<p class="muted loading" role="status">{t('common.loading')}</p>{/if}
{:else}
	<!-- PLAN 75-19/75-20, draft U2: the accounts at the left, the chosen one at the right. -->
	<div class="u2" class:detail-only={narrow && showDetail}>
		<nav class="accounts" aria-label={t('users.list')}>
			<label class="filter-row">
				<span class="sr-only">{t('users.filter')}</span>
				<select class="field filter" bind:value={statusFilter} aria-label={t('users.filter')}>
					{#each ACCOUNT_STATUS_FILTERS as option (option)}<option value={option}
							>{t(filterLabel[option])}</option
						>{/each}
				</select>
			</label>
			<ul>
				{#each shown as account (account.name)}
					<li>
						<button
							type="button"
							class="acct"
							class:on={current?.name === account.name}
							class:locked={account.disabled}
							aria-current={current?.name === account.name ? 'true' : undefined}
							data-account={account.name}
							onclick={() => choose(account.name)}
						>
							<span class="avatar" style:--hue={hue(account.name)} aria-hidden="true"
								>{account.name.charAt(0).toUpperCase()}</span
							>
							<span class="lines"
								><span class="mono name">{account.name}</span><span class="sub"
									>{t(`role.${account.role}`)}{account.disabled
										? ' · ' + t('users.statusLocked')
										: ''}{account.twoFactor ? ' · ' + t('users.twoFactor') : ''}</span
								></span
							>
						</button>
					</li>
				{:else}
					<li class="muted empty">{t('users.filterEmpty')}</li>
				{/each}
			</ul>
			<button type="button" class="acct new" onclick={openCreate}>+ {t('users.new')}</button>
		</nav>
		{#if current}
			{@const account = current}
			<AccountDetail
				{account}
				me={me?.name}
				{shares}
				manageable={mayManage(account.role)}
				mayLock={!!accounts && mayLock(me, account, accounts)}
				{offeredRoles}
				{working}
				created={day(account.created)}
				lockedTitle={account.disabled ? lockedTitle(account) : ''}
				onrole={(role, select) => pickRole(account, role, select)}
				onpassword={() => (dialog = { kind: 'password', name: account.name })}
				onlock={() => {
					dialogError = '';
					dialog = { kind: 'lock', account, disabled: !account.disabled };
				}}
				ondelete={() => {
					dialogError = '';
					dialog = { kind: 'delete', name: account.name };
				}}
				onreset2fa={() => {
					dialogError = '';
					dialog = { kind: 'twoFactor', account };
				}}
				onshares={load}
				onback={narrow ? () => (showDetail = false) : undefined}
			/>
		{/if}
	</div>
{/if}

{#if dialog?.kind === 'create'}
	<Dialog title={t('users.createTitle')} locked={working} onclose={() => (dialog = null)}>
		<form onsubmit={create}>
			<label>
				<span>{t('auth.name')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input
					class="field"
					type="text"
					bind:value={draft.name}
					maxlength="32"
					autocapitalize="none"
					autocomplete="off"
					spellcheck="false"
					required
					autofocus
				/>
				<small>{t('auth.nameHint')}</small>
			</label>
			<label>
				<span>{t('users.role')}</span>
				<select class="field" bind:value={draft.role}>
					{#each offeredRoles as role (role)}<option value={role}>{t(`role.${role}`)}</option
						>{/each}
				</select>
				<small>{t(`users.roleHint.${draft.role}`)}</small>
			</label>
			<label>
				<span>{t('auth.password')}</span>
				<input
					class="field"
					type="password"
					bind:value={draft.password}
					autocomplete="new-password"
					required
				/>
				<small>{t('auth.passwordHint', { min: MIN_PASSWORD_LENGTH })}</small>
			</label>
			<label>
				<span>{t('auth.passwordRepeat')}</span>
				<input
					class="field"
					type="password"
					bind:value={draft.repeat}
					autocomplete="new-password"
					required
				/>
			</label>
			{#if dialogError}<p class="notice-warn" role="alert">{dialogError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (dialog = null)} disabled={working}
					>{t('common.cancel')}</button
				>
				<button type="submit" class="btn btn-primary" disabled={working}>{t('users.create')}</button
				>
			</div>
		</form>
	</Dialog>
{:else if dialog?.kind === 'password'}
	<PasswordDialog
		name={dialog.name}
		self={dialog.name === me?.name}
		onclose={() => (dialog = null)}
	/>
{:else if dialog?.kind === 'delete'}
	{@const name = dialog.name}
	{@const reached = reach(name)}
	<Dialog title={t('users.deleteTitle', { name })} locked={working} onclose={() => (dialog = null)}>
		<p class="text">{t('users.deleteText')}</p>
		{#if reached.length}<p class="text">
				{t('users.deleteShares', { shares: reached.map((entry) => entry.name).join(', ') })}
			</p>{/if}
		{#if dialogError}<p class="notice-error" role="alert">{dialogError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={working} onclick={() => (dialog = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={working} onclick={() => remove(name)}
				>{t('users.delete')}</button
			>
		</div>
	</Dialog>
{:else if dialog?.kind === 'role'}
	{@const change = dialog}
	<Dialog
		title={t('users.ownerTitle', { name: change.account.name })}
		locked={working}
		onclose={() => (dialog = null)}
	>
		<p class="text">{t('users.ownerText', { name: change.account.name })}</p>
		{#if dialogError}<p class="notice-error" role="alert">{dialogError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={working} onclick={() => (dialog = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-warn"
				disabled={working}
				onclick={() => setRole(change.account, change.role)}>{t('users.ownerSubmit')}</button
			>
		</div>
	</Dialog>
{:else if dialog?.kind === 'lock'}
	{@const change = dialog}
	<Dialog
		title={t(change.disabled ? 'users.lockTitle' : 'users.unlockTitle', {
			name: change.account.name,
		})}
		locked={working}
		onclose={() => (dialog = null)}
	>
		<p class="text">
			{t(change.disabled ? 'users.lockText' : 'users.unlockText', { name: change.account.name })}
		</p>
		{#if dialogError}<p class="notice-error" role="alert">{dialogError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={working} onclick={() => (dialog = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class={change.disabled ? 'btn btn-warn' : 'btn btn-primary'}
				disabled={working}
				onclick={() => setLocked(change.account, change.disabled)}
				>{t(change.disabled ? 'users.lock' : 'users.unlock')}</button
			>
		</div>
	</Dialog>
{:else if dialog?.kind === 'twoFactor'}
	{@const account = dialog.account}
	<Dialog
		title={t('users.resetTwoFactorTitle', { name: account.name })}
		locked={working}
		onclose={() => (dialog = null)}
	>
		<p class="text">{t('users.resetTwoFactorText', { name: account.name })}</p>
		{#if dialogError}<p class="notice-error" role="alert">{dialogError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={working} onclick={() => (dialog = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-warn"
				disabled={working}
				onclick={() => resetTwoFactor(account)}>{t('users.resetTwoFactor')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	/* PLAN 75-20, draft U2. */
	.u2 {
		display: grid;
		grid-template-columns: 230px minmax(0, 1fr);
		gap: var(--space-4);
		align-items: start;
	}
	.accounts {
		position: sticky;
		top: var(--space-2);
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.accounts ul {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.filter-row {
		margin-bottom: var(--space-2);
	}
	.filter {
		width: 100%;
	}
	.acct {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		width: 100%;
		min-height: 44px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: inherit;
		font: inherit;
		text-align: left;
		cursor: pointer;
		position: relative;
	}
	.acct:hover {
		background: color-mix(in srgb, var(--text) 5%, transparent);
	}
	.acct.on {
		background: color-mix(in srgb, var(--text) 8%, transparent);
	}
	.acct.on::before {
		content: '';
		position: absolute;
		left: 0;
		top: 10px;
		bottom: 10px;
		width: 3px;
		border-radius: 2px;
		background: var(--accent);
	}
	.acct.locked .name {
		text-decoration: line-through;
		color: var(--muted);
	}
	.acct.new {
		min-height: 36px;
		color: var(--accent);
	}
	.avatar {
		display: grid;
		place-items: center;
		flex: none;
		width: 26px;
		height: 26px;
		border-radius: 50%;
		background: hsl(var(--hue) 55% 42%);
		color: #fff;
		font-size: var(--text-sm);
		font-weight: 700;
	}
	.lines {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.name {
		overflow-wrap: anywhere;
	}
	.sub {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.empty {
		padding: var(--space-2);
	}
	@media (max-width: 720px) {
		.u2 {
			grid-template-columns: minmax(0, 1fr);
		}
		.u2 > :global(.account) {
			display: none;
		}
		.u2.detail-only > .accounts {
			display: none;
		}
		.u2.detail-only > :global(.account) {
			display: block;
		}
		.accounts {
			position: static;
		}
	}
	.failed {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2-5);
	}
	form {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: var(--text-md);
	}
	.filter-row {
		display: block;
	}
	small {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.text {
		font-size: var(--text-md);
	}
</style>
