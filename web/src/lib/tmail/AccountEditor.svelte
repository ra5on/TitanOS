<script lang="ts">
	// Changing a mailbox: name, servers, password (empty keeps the stored
	// one), its default signature; check the connection; remove it (asks
	// first; only TitanOS forgets it, nothing changes at the provider).
	import DangerZone from '../components/DangerZone.svelte';
	import Icon from '../components/Icon.svelte';
	import { apiErrorText, errorText, t } from '../i18n/index.svelte.ts';
	import Confirm from '../nvr/Confirm.svelte';
	import { mailApi } from './api.ts';
	import ServerFields from './ServerFields.svelte';
	import StatusBadge from './StatusBadge.svelte';
	import type { MailAccount, Signature, TestResult } from './types.ts';

	let {
		account,
		signatures,
		focusPassword = false,
		onsaved,
		onremoved,
	}: {
		account: MailAccount;
		signatures: Signature[];
		focusPassword?: boolean;
		onsaved: (a: MailAccount) => void;
		onremoved: (id: string) => void;
	} = $props();

	// A copy to edit; the account shown elsewhere changes only once saved.
	const start = (() => $state.snapshot(account))();
	let name = $state(start.name);
	let user = $state(start.user);
	let imap = $state(start.imap);
	let smtp = $state(start.smtp);
	let signature = $state(start.signature);
	let password = $state('');
	let busy = $state(false);
	let error = $state('');
	let saved = $state('');
	let result = $state<TestResult | null>(null);
	let confirmRemove = $state(false);
	let passwordField = $state<HTMLInputElement>();

	$effect(() => {
		if (focusPassword && passwordField) {
			passwordField.focus();
			passwordField.scrollIntoView({ block: 'center' });
		}
	});

	function body() {
		return {
			name: name.trim() || account.email,
			email: account.email,
			user: user.trim(),
			imap,
			smtp,
			notify: account.notify,
			signature,
			password,
		};
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		saved = '';
		try {
			const next = await mailApi.updateAccount(account.id, body());
			password = '';
			saved = t('mail.settings.saved');
			onsaved(next);
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	async function check() {
		busy = true;
		error = '';
		result = null;
		try {
			// Without a new password the stored one is used (by id).
			const b = body();
			result = await mailApi.test(password ? b : { ...b, password: undefined, id: account.id });
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	async function remove() {
		busy = true;
		error = '';
		try {
			await mailApi.deleteAccount(account.id);
			confirmRemove = false;
			onremoved(account.id);
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	const resultText = (r: { ok: boolean; code?: string }) =>
		r.ok ? t('mail.setup.ok') : r.code ? errorText(r.code) : t('mail.setup.failed');
</script>

<form class="editor" onsubmit={save}>
	<div class="state"><StatusBadge status={account.status} showOk /></div>
	{#if account.status.state === 'auth_failed'}
		<p class="notice-warn">{t('mail.settings.authFailed')}</p>
	{/if}
	<label
		><span>{t('mail.setup.name')}</span>
		<input id="mail-edit-{account.id}-name" class="field" type="text" bind:value={name} />
	</label>
	<label
		><span>{t('mail.settings.password')}</span>
		<input
			id="mail-edit-{account.id}-password"
			class="field"
			type="password"
			autocomplete="off"
			placeholder={account.passwordSet ? t('mail.settings.passwordKeep') : ''}
			bind:this={passwordField}
			bind:value={password}
		/>
		<span class="muted small">{t('mail.settings.passwordHint')}</span>
	</label>
	<ServerFields id="mail-edit-{account.id}" bind:user bind:imap bind:smtp />
	<label
		><span>{t('mail.settings.defaultSignature')}</span>
		<select id="mail-edit-{account.id}-signature" class="field sig" bind:value={signature}>
			<option value="">{t('mail.compose.noSignature')}</option>
			{#each signatures as s (s.id)}<option value={s.id}>{s.name}</option>{/each}
		</select>
	</label>
	{#if result}
		<ul class="results" aria-label={t('mail.setup.checkResult')}>
			{#each [['IMAP', result.imap], ['SMTP', result.smtp]] as const as [label, r] (label)}
				<li class:ok={r.ok} class:bad={!r.ok}>
					<Icon name={r.ok ? 'check' : 'close'} size={16} />
					<span><strong>{label}:</strong> {resultText(r)}</span>
				</li>
			{/each}
		</ul>
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if saved}<p class="notice-ok" role="status">{saved}</p>{/if}
	<div class="btns">
		<button type="button" class="btn" disabled={busy} onclick={check}
			>{t('mail.setup.check')}</button
		>
		<button type="submit" class="btn btn-primary" disabled={busy}>{t('common.save')}</button>
	</div>
	<DangerZone hint={t('mail.settings.removeHint')}>
		<button
			type="button"
			class="btn btn-danger"
			disabled={busy}
			onclick={() => (confirmRemove = true)}>{t('mail.settings.remove')}</button
		>
	</DangerZone>
</form>

{#if confirmRemove}
	<Confirm
		title={t('mail.settings.removeTitle')}
		text={[
			t('mail.settings.removeText', { name: account.name || account.email }),
			t('mail.settings.removeKeeps'),
		]}
		confirm={t('mail.settings.remove')}
		danger
		{busy}
		{error}
		onconfirm={remove}
		onclose={() => (confirmRemove = false)}
	/>
{/if}

<style>
	.editor {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-2) 0;
		min-width: 0;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	label > span:first-child {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.small {
		font-size: var(--text-sm);
	}
	.field {
		width: 100%;
	}
	.sig {
		width: auto;
		align-self: flex-start;
		max-width: 100%;
	}
	.results {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.results li {
		display: flex;
		gap: var(--space-2);
		align-items: flex-start;
		padding: var(--space-1-5) var(--space-2);
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		overflow-wrap: anywhere;
	}
	.results li.ok {
		border-color: var(--ok-border);
		background: var(--ok-bg);
		color: var(--ok-text);
	}
	.results li.bad {
		border-color: var(--error-border);
		background: var(--error-bg);
		color: var(--error-text);
	}
	.btns {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-2);
	}
	@media (max-width: 899px) {
		.btn,
		.field {
			min-height: 44px;
		}
	}
</style>
