<script lang="ts">
	import { api, ApiError, type Account, type Role } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PasswordDialog from '#lib/components/PasswordDialog.svelte';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';

	const ROLES: Role[] = ['owner', 'admin', 'user'];
	const NAME = /^[a-z][a-z0-9_-]{0,31}$/;
	const MIN_PASSWORD_LENGTH = 10;

	const me = $derived(session.data?.user);
	let accounts = $state<Account[] | null>(null);
	let error = $state('');
	let dialog = $state<{ kind: 'create' } | { kind: 'password' | 'delete'; name: string } | null>(null);

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
		try {
			accounts = await api.get<Account[]>('/users');
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
	$effect(() => {
		load();
	});

	async function setRole(account: Account, role: Role) {
		error = '';
		try {
			await api.put(`/users/${account.name}`, { role });
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
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

	async function remove(name: string) {
		dialog = null;
		error = '';
		try {
			await api.delete(`/users/${name}`);
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
		await load();
	}
</script>

<div class="head">
	<div class="title">
		<h1>{t('users.title')}</h1>
		{#if accounts}<p class="muted">{accounts.length === 1 ? t('users.summaryOne') : t('users.summary', { count: accounts.length })}</p>{/if}
	</div>
	<button type="button" class="btn btn-primary" onclick={openCreate}>{t('users.new')}</button>
</div>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

<div class="tile table">
	<table>
		<thead>
			<tr>
				<th scope="col">{t('users.name')}</th>
				<th scope="col">{t('users.role')}</th>
				<th scope="col">{t('users.created')}</th>
				<th scope="col"></th>
			</tr>
		</thead>
		<tbody>
			{#each accounts ?? [] as account (account.name)}
				{@const manageable = mayManage(account.role)}
				<tr>
					<td>
						<span class="who">
							<span class="avatar" aria-hidden="true">{account.name.charAt(0).toUpperCase()}</span>
							<span>
								<span class="mono name">{account.name}</span>{#if account.name === me?.name}<span class="chip you">{t('users.you')}</span>{/if}
								{#if !account.smbReady}<span class="sub">{t('users.smbPending')}</span>{/if}
							</span>
						</span>
					</td>
					<td>
						{#if manageable}
							<select class="field role" aria-label="{t('users.role')}: {account.name}" value={account.role} onchange={(event) => setRole(account, event.currentTarget.value as Role)}>
								{#each offeredRoles as role (role)}<option value={role}>{t(`role.${role}`)}</option>{/each}
							</select>
						{:else}
							{t(`role.${account.role}`)}
						{/if}
					</td>
					<td class="muted">{date.format(new Date(account.created))}</td>
					<td class="actions">
						{#if manageable || account.name === me?.name}
							<button type="button" class="btn" onclick={() => (dialog = { kind: 'password', name: account.name })}>{t('users.changePassword')}</button>
						{/if}
						{#if manageable && account.name !== me?.name}
							<button type="button" class="btn" onclick={() => (dialog = { kind: 'delete', name: account.name })}>{t('users.delete')}</button>
						{/if}
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>

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
	<Dialog title={t('users.deleteTitle', { name })} onclose={() => (dialog = null)}>
		<p class="text">{t('users.deleteText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" onclick={() => remove(name)}>{t('users.delete')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		justify-content: space-between;
		gap: 12px;
	}
	.title {
		display: flex;
		flex-direction: column;
		gap: 4px;
		font-size: 0.875rem;
	}
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
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
		padding: 14px 16px;
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
