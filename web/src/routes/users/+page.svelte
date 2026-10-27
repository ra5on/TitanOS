<script lang="ts">
	import { api, ApiError, type Account, type Role, type Share } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import PasswordDialog from '#lib/components/PasswordDialog.svelte';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';

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

	async function load() {
		loading = true;
		loadError = '';
		const [users, list] = await Promise.allSettled([api.get<Account[]>('/users'), api.get<Share[]>('/shares')]);
		if (users.status === 'fulfilled') accounts = users.value;
		else loadError = errorText(users.reason instanceof ApiError ? users.reason.code : 'internal');
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
			const text = errorText(err instanceof ApiError ? err.code : 'internal');
			if (dialog?.kind === 'role') dialogError = text;
			else error = text;
		}
		working = false;
		if (!failed && dialog?.kind === 'role') dialog = null;
		await load();
		// The select shows what the NAS has; a refused role is turned back
		// (the value bound to it may not have changed, so set it directly).
		if (select) select.value = accounts?.find((entry) => entry.name === account.name)?.role ?? account.role;
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
			dialogError = errorText(err instanceof ApiError ? err.code : 'internal');
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
		if ([...draft.password].length < MIN_PASSWORD_LENGTH) return void (dialogError = errorText('password_too_short'));
		if (draft.password !== draft.repeat) return void (dialogError = t('auth.passwordMismatch'));
		working = true;
		try {
			await api.post('/users', { name, password: draft.password, role: draft.role });
			dialog = null;
			await load();
		} catch (err) {
			dialogError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
		working = false;
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
			dialogError = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			working = false;
		}
	}
</script>

<PageHead title={t('users.title')}>
	{#if accounts}<p class="muted">{accounts.length === 1 ? t('users.summaryOne') : t('users.summary', { count: accounts.length })}</p>{/if}
	{#snippet actions()}
		<button type="button" class="btn btn-primary" onclick={openCreate}>{t('users.new')}</button>
	{/snippet}
</PageHead>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if loadError}
	<div class="notice-error failed" role="alert"><span>{loadError}</span><button type="button" class="btn" disabled={loading} onclick={load}>{t('common.retry')}</button></div>
{/if}

{#if accounts === null}
	{#if !loadError}<p class="muted" role="status">{t('common.loading')}</p>{/if}
{:else}
<div class="tile table">
	<table>
		<thead>
			<tr>
				<th scope="col">{t('users.name')}</th>
				<th scope="col">{t('users.role')}</th>
				<th scope="col">{t('users.shares')}</th>
				<th scope="col">{t('users.created')}</th>
				<th scope="col"></th>
			</tr>
		</thead>
		<tbody>
			{#each accounts as account (account.name)}
				{@const manageable = mayManage(account.role)}
				<tr>
					<td>
						<span class="who">
							<span class="avatar" aria-hidden="true">{account.name.charAt(0).toUpperCase()}</span>
							<span>
								<span class="mono name">{account.name}</span>{#if account.name === me?.name}<span class="chip you">{t('users.you')}</span>{/if}
								{#if !account.smbReady}<span class="sub">{t('users.smbPending')}</span>{/if}
								{#if account.twoFactor}<span class="chip">{t('users.twoFactor')}</span>{/if}
							</span>
						</span>
					</td>
					<td>
						{#if manageable && account.name !== me?.name}
							<select class="field role" aria-label="{t('users.role')}: {account.name}" value={account.role} disabled={working} onchange={(event) => pickRole(account, event.currentTarget.value as Role, event.currentTarget)}>
								{#each offeredRoles as role (role)}<option value={role}>{t(`role.${role}`)}</option>{/each}
							</select>
						{:else}
							{t(`role.${account.role}`)}
						{/if}
					</td>
					<td class="reach">
						{#if shares === null}
							<span class="muted">{t('users.sharesUnknown')}</span>
						{:else}
							{#each reach(account.name) as entry (entry.name)}
								<a class="chip" href="/shares?share={encodeURIComponent(entry.name)}&tab=access" title={t(`share.${entry.level}`)}><span class="mono">{entry.name}</span> · {t(entry.level === 'rw' ? 'users.shareRw' : 'users.shareRo')}</a>
							{:else}
								<span class="muted">{t('users.sharesNone')}</span>
							{/each}
						{/if}
					</td>
					<td class="muted">{date.format(new Date(account.created))}</td>
					<td class="actions">
						{#if manageable || account.name === me?.name}
							<button type="button" class="btn" onclick={() => (dialog = { kind: 'password', name: account.name })}>{t('users.changePassword')}</button>
							{#if account.twoFactor && manageable && account.name !== me?.name}
								<button type="button" class="btn" onclick={() => ((dialogError = ''), (dialog = { kind: 'twoFactor', account }))}>{t('users.resetTwoFactor')}</button>
							{/if}
						{/if}
						{#if manageable && account.name !== me?.name}
							<button type="button" class="btn" onclick={() => ((dialogError = ''), (dialog = { kind: 'delete', name: account.name }))}>{t('users.delete')}</button>
						{/if}
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>
{/if}

{#if dialog?.kind === 'create'}
	<Dialog title={t('users.createTitle')} locked={working} onclose={() => (dialog = null)}>
		<form onsubmit={create}>
			<label>
				<span>{t('auth.name')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input class="field" type="text" bind:value={draft.name} maxlength="32" autocapitalize="none" autocomplete="off" spellcheck="false" required autofocus />
				<small>{t('auth.nameHint')}</small>
			</label>
			<label>
				<span>{t('users.role')}</span>
				<select class="field" bind:value={draft.role}>
					{#each offeredRoles as role (role)}<option value={role}>{t(`role.${role}`)}</option>{/each}
				</select>
				<small>{t(`users.roleHint.${draft.role}`)}</small>
			</label>
			<label>
				<span>{t('auth.password')}</span>
				<input class="field" type="password" bind:value={draft.password} autocomplete="new-password" required />
				<small>{t('auth.passwordHint', { min: MIN_PASSWORD_LENGTH })}</small>
			</label>
			<label>
				<span>{t('auth.passwordRepeat')}</span>
				<input class="field" type="password" bind:value={draft.repeat} autocomplete="new-password" required />
			</label>
			{#if dialogError}<p class="notice-warn" role="alert">{dialogError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (dialog = null)} disabled={working}>{t('common.cancel')}</button>
				<button type="submit" class="btn btn-primary" disabled={working}>{t('users.create')}</button>
			</div>
		</form>
	</Dialog>
{:else if dialog?.kind === 'password'}
	<PasswordDialog name={dialog.name} self={dialog.name === me?.name} onclose={() => (dialog = null)} />
{:else if dialog?.kind === 'delete'}
	{@const name = dialog.name}
	{@const reached = reach(name)}
	<Dialog title={t('users.deleteTitle', { name })} locked={working} onclose={() => (dialog = null)}>
		<p class="text">{t('users.deleteText')}</p>
		{#if reached.length}<p class="text">{t('users.deleteShares', { shares: reached.map((entry) => entry.name).join(', ') })}</p>{/if}
		{#if dialogError}<p class="notice-error" role="alert">{dialogError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={working} onclick={() => (dialog = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={working} onclick={() => remove(name)}>{t('users.delete')}</button>
		</div>
	</Dialog>
{:else if dialog?.kind === 'role'}
	{@const change = dialog}
	<Dialog title={t('users.ownerTitle', { name: change.account.name })} locked={working} onclose={() => (dialog = null)}>
		<p class="text">{t('users.ownerText', { name: change.account.name })}</p>
		{#if dialogError}<p class="notice-error" role="alert">{dialogError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={working} onclick={() => (dialog = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" disabled={working} onclick={() => setRole(change.account, change.role)}>{t('users.ownerSubmit')}</button>
		</div>
	</Dialog>
{:else if dialog?.kind === 'twoFactor'}
	{@const account = dialog.account}
	<Dialog title={t('users.resetTwoFactorTitle', { name: account.name })} locked={working} onclose={() => (dialog = null)}>
		<p class="text">{t('users.resetTwoFactorText', { name: account.name })}</p>
		{#if dialogError}<p class="notice-error" role="alert">{dialogError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={working} onclick={() => (dialog = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" disabled={working} onclick={() => resetTwoFactor(account)}>{t('users.resetTwoFactor')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.table {
		overflow-x: auto;
	}
	table {
		width: 100%;
		min-width: 720px;
		border-collapse: collapse;
		font-size: 0.875rem;
	}
	th {
		padding: calc(var(--row-pad) + 2px) var(--tile-pad);
		border-bottom: 1px solid var(--panel-border);
		text-align: left;
		font-size: 0.75rem;
		font-weight: 500;
		color: var(--muted);
	}
	td {
		padding: 10px 16px;
		border-bottom: 1px solid var(--panel-border);
		vertical-align: middle;
	}
	tbody tr:last-child td {
		border-bottom: 0;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.avatar {
		width: 32px;
		height: 32px;
		flex: none;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: var(--radius-ctl);
		background: var(--accent);
		color: var(--on-accent);
		font-size: 0.8125rem;
		font-weight: 700;
	}
	.name {
		font-weight: 500;
	}
	.you {
		margin-left: 8px;
	}
	.sub {
		display: block;
		font-size: 0.75rem;
		color: var(--muted);
	}
	.role {
		width: auto;
		height: 2.25rem;
		font-size: 0.875rem;
	}
	.failed {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
	}
	.reach {
		max-width: 22rem;
	}
	.reach a {
		display: inline-block;
		margin: 2px 4px 2px 0;
		color: var(--text);
		text-decoration: none;
		font-size: 0.75rem;
	}
	.reach a:hover {
		text-decoration: underline;
	}
	.actions {
		text-align: right;
		white-space: nowrap;
	}
	.actions .btn {
		height: 2.25rem;
		margin-left: 6px;
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.875rem;
	}
	small {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.text {
		font-size: 0.875rem;
	}
</style>
