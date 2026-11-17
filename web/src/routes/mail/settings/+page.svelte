<script lang="ts">
	// 1.05, T-Mail settings (docs/TMAIL.md): the mailboxes (change, remove,
	// change the password after a failed login), notices per mailbox,
	// signatures and rules that run on the NAS for new mail.
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import Confirm from '#lib/nvr/Confirm.svelte';
	import AccountEditor from '#lib/tmail/AccountEditor.svelte';
	import { mailApi } from '#lib/tmail/api.ts';
	import StatusBadge from '#lib/tmail/StatusBadge.svelte';
	import {
		folderName,
		loadAccounts,
		loadFolders,
		mail,
		sortFolders,
	} from '#lib/tmail/store.svelte.ts';
	import type { MailAccount, MailSettings, Rule, Signature } from '#lib/tmail/types.ts';

	const editing = page.url.searchParams.get('edit') ?? '';

	let settings = $state<MailSettings | null>(null);
	let loadError = $state('');
	let busy = $state(false);
	let saveError = $state('');
	let savedNote = $state('');
	let changed = $state(false);
	let notifyError = $state('');
	let asking = $state<{ kind: 'signature' | 'rule'; id: string } | null>(null);

	onMount(async () => {
		try {
			settings = await mailApi.settings();
			settings.signatures ??= [];
			settings.rules ??= [];
		} catch (err) {
			loadError = apiErrorText(err);
		}
		for (const a of mail.accounts ?? []) if (!mail.folders[a.id]) void loadFolders(a.id);
	});

	const newId = () => Math.random().toString(36).slice(2, 10);

	function touch() {
		changed = true;
		savedNote = '';
	}

	async function save() {
		if (!settings) return;
		busy = true;
		saveError = '';
		try {
			const next = await mailApi.saveSettings($state.snapshot(settings));
			if (next) settings = { signatures: next.signatures ?? [], rules: next.rules ?? [] };
			changed = false;
			savedNote = t('mail.settings.saved');
		} catch (err) {
			saveError = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	function addSignature() {
		settings?.signatures.push({ id: newId(), name: t('mail.signature.newName'), text: '' });
		touch();
	}
	function addRule() {
		const first = mail.accounts?.[0]?.id ?? '';
		settings?.rules.push({
			id: newId(),
			account: first,
			enabled: true,
			when: { from: '', to: '', subject: '' },
			then: { moveTo: '', markRead: false, flag: false },
		});
		touch();
	}
	function removeAsked() {
		if (!settings || !asking) return;
		if (asking.kind === 'signature')
			settings.signatures = settings.signatures.filter((s) => s.id !== asking!.id);
		else settings.rules = settings.rules.filter((r) => r.id !== asking!.id);
		asking = null;
		touch();
	}
	const askedName = $derived.by(() => {
		if (!asking || !settings) return '';
		if (asking.kind === 'signature')
			return settings.signatures.find((s) => s.id === asking!.id)?.name ?? '';
		return ruleText(settings.rules.find((r) => r.id === asking!.id));
	});

	function ruleText(r: Rule | undefined): string {
		if (!r) return '';
		const parts = [
			r.when.from && t('mail.rule.fromHas', { text: r.when.from }),
			r.when.to && t('mail.rule.toHas', { text: r.when.to }),
			r.when.subject && t('mail.rule.subjectHas', { text: r.when.subject }),
		].filter(Boolean);
		return parts.join(', ') || t('mail.rule.empty');
	}

	async function setNotify(a: MailAccount, on: boolean) {
		notifyError = '';
		try {
			const next = await mailApi.updateAccount(a.id, {
				name: a.name,
				email: a.email,
				user: a.user,
				imap: a.imap,
				smtp: a.smtp,
				notify: on,
				signature: a.signature,
				password: '',
			});
			Object.assign(a, next ?? { notify: on });
		} catch (err) {
			notifyError = apiErrorText(err);
		}
	}

	function accountSaved(next: MailAccount) {
		const a = mail.accounts?.find((x) => x.id === next.id);
		if (a && next) Object.assign(a, next);
		else void loadAccounts();
	}
	function accountRemoved(id: string) {
		if (mail.accounts) mail.accounts = mail.accounts.filter((a) => a.id !== id);
		delete mail.folders[id];
		if (settings) {
			const before = settings.rules.length;
			settings.rules = settings.rules.filter((r) => r.account !== id);
			if (settings.rules.length !== before) touch();
		}
	}
	const accountName = (id: string) => {
		const a = mail.accounts?.find((x) => x.id === id);
		return a ? a.name || a.email : id;
	};
</script>

<PageHead
	title={t('mail.settings.title')}
	path={[{ label: t('desktop.app.mail'), href: '/mail' }]}
/>

<div class="settings-page column">
	<SettingGroup title={t('mail.settings.accounts')} id="mail-accounts">
		{#snippet tools()}
			<a class="btn" href="/mail/setup"><Icon name="plus" size={16} />{t('mail.side.add')}</a>
		{/snippet}
		{#each mail.accounts ?? [] as a (a.id)}
			<SettingRow
				icon="mail"
				id="mail-account-{a.id}"
				title={a.name || a.email}
				desc={a.email}
				open={a.id === editing}
			>
				{#snippet badge()}<StatusBadge status={a.status} />{/snippet}
				<div class="srow-block">
					<AccountEditor
						account={a}
						signatures={settings?.signatures ?? []}
						focusPassword={a.id === editing}
						onsaved={accountSaved}
						onremoved={accountRemoved}
					/>
				</div>
			</SettingRow>
		{:else}
			<p class="muted">{t('mail.empty.text')}</p>
		{/each}
	</SettingGroup>

	<SettingGroup
		title={t('mail.settings.notify')}
		id="mail-notify"
		desc={t('mail.settings.notifyDesc')}
	>
		{#if notifyError}<p class="notice-error" role="alert">{notifyError}</p>{/if}
		{#each mail.accounts ?? [] as a (a.id)}
			<SettingRow icon="bell" title={a.name || a.email} for="mail-notify-{a.id}">
				{#snippet control()}
					<input
						id="mail-notify-{a.id}"
						type="checkbox"
						role="switch"
						class="switch"
						checked={a.notify}
						onchange={(e) => setNotify(a, e.currentTarget.checked)}
					/>
				{/snippet}
			</SettingRow>
		{/each}
	</SettingGroup>

	{#if loadError}<p class="notice-error" role="alert">{loadError}</p>{/if}
	{#if settings}
		<SettingGroup
			title={t('mail.settings.signatures')}
			id="mail-signatures"
			desc={t('mail.settings.signaturesDesc')}
		>
			{#snippet tools()}
				<button type="button" class="btn" onclick={addSignature}
					><Icon name="plus" size={16} />{t('mail.signature.add')}</button
				>
			{/snippet}
			{#each settings.signatures as s, i (s.id)}
				<div class="item" aria-label={s.name}>
					<label
						><span>{t('mail.signature.name')}</span>
						<input
							id="mail-sig-{i}-name"
							class="field"
							type="text"
							bind:value={s.name}
							oninput={touch}
						/>
					</label>
					<label
						><span>{t('mail.signature.text')}</span>
						<textarea
							id="mail-sig-{i}-text"
							class="field"
							rows="4"
							bind:value={s.text}
							oninput={touch}></textarea>
					</label>
					<div class="item-btns">
						<button
							type="button"
							class="btn btn-danger-outline"
							onclick={() => (asking = { kind: 'signature', id: s.id })}
							><Icon name="trash" size={16} />{t('mail.signature.remove')}</button
						>
					</div>
				</div>
			{:else}
				<p class="muted">{t('mail.signature.none')}</p>
			{/each}
		</SettingGroup>

		<SettingGroup
			title={t('mail.settings.rules')}
			id="mail-rules"
			desc={t('mail.settings.rulesDesc')}
		>
			{#snippet tools()}
				<button type="button" class="btn" disabled={!mail.accounts?.length} onclick={addRule}
					><Icon name="plus" size={16} />{t('mail.rule.add')}</button
				>
			{/snippet}
			{#each settings.rules as r, i (r.id)}
				<fieldset class="item rule">
					<legend>{t('mail.rule.legend', { n: i + 1 })}</legend>
					<label class="inline"
						><input
							type="checkbox"
							role="switch"
							class="switch"
							bind:checked={r.enabled}
							onchange={touch}
						/><span>{r.enabled ? t('mail.rule.on') : t('mail.rule.off')}</span></label
					>
					<label
						><span>{t('mail.rule.account')}</span>
						<select
							id="mail-rule-{i}-account"
							class="field fit"
							bind:value={r.account}
							onchange={() => {
								r.then.moveTo = '';
								if (!mail.folders[r.account]) void loadFolders(r.account);
								touch();
							}}
						>
							{#each mail.accounts ?? [] as a (a.id)}<option value={a.id}
									>{accountName(a.id)}</option
								>{/each}
						</select>
					</label>
					<p class="when">{t('mail.rule.when')}</p>
					<div class="grid">
						<label
							><span>{t('mail.rule.from')}</span><input
								id="mail-rule-{i}-from"
								class="field"
								type="text"
								bind:value={r.when.from}
								oninput={touch}
							/></label
						>
						<label
							><span>{t('mail.rule.to')}</span><input
								id="mail-rule-{i}-to"
								class="field"
								type="text"
								bind:value={r.when.to}
								oninput={touch}
							/></label
						>
						<label
							><span>{t('mail.rule.subject')}</span><input
								id="mail-rule-{i}-subject"
								class="field"
								type="text"
								bind:value={r.when.subject}
								oninput={touch}
							/></label
						>
					</div>
					<p class="when">{t('mail.rule.then')}</p>
					<label
						><span>{t('mail.rule.moveTo')}</span>
						<select
							id="mail-rule-{i}-move"
							class="field fit"
							bind:value={r.then.moveTo}
							onchange={touch}
						>
							<option value="">{t('mail.rule.noMove')}</option>
							{#each sortFolders(mail.folders[r.account] ?? []) as f (f.path)}<option value={f.path}
									>{folderName(f)}</option
								>{/each}
						</select>
					</label>
					<label class="inline"
						><input type="checkbox" bind:checked={r.then.markRead} onchange={touch} /><span
							>{t('mail.rule.markRead')}</span
						></label
					>
					<label class="inline"
						><input type="checkbox" bind:checked={r.then.flag} onchange={touch} /><span
							>{t('mail.rule.flag')}</span
						></label
					>
					<div class="item-btns">
						<button
							type="button"
							class="btn btn-danger-outline"
							onclick={() => (asking = { kind: 'rule', id: r.id })}
							><Icon name="trash" size={16} />{t('mail.rule.remove')}</button
						>
					</div>
				</fieldset>
			{:else}
				<p class="muted">{t('mail.rule.none')}</p>
			{/each}
			<p class="muted small">{t('mail.settings.rulesNew')}</p>
		</SettingGroup>

		<div class="savebar">
			{#if saveError}<p class="notice-error" role="alert">{saveError}</p>{/if}
			{#if savedNote}<p class="notice-ok" role="status">{savedNote}</p>{/if}
			<button type="button" class="btn btn-primary" disabled={busy || !changed} onclick={save}
				>{t('mail.settings.saveSignaturesRules')}</button
			>
		</div>
	{/if}
</div>

{#if asking}
	<Confirm
		title={asking.kind === 'signature'
			? t('mail.signature.removeTitle')
			: t('mail.rule.removeTitle')}
		text={asking.kind === 'signature'
			? t('mail.signature.removeText', { name: askedName })
			: t('mail.rule.removeText', { rule: askedName })}
		confirm={asking.kind === 'signature' ? t('mail.signature.remove') : t('mail.rule.remove')}
		danger
		onconfirm={removeAsked}
		onclose={() => (asking = null)}
	/>
{/if}

<style>
	.item {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin: 0 0 var(--space-2);
		padding: var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--surface);
		min-width: 0;
	}
	.rule legend {
		padding: 0 var(--space-1);
		font-weight: 600;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		min-width: 0;
	}
	label > span:first-child {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	label.inline {
		flex-direction: row;
		align-items: center;
		gap: var(--space-2);
		min-height: 32px;
	}
	label.inline > span {
		color: var(--text);
		font-size: var(--text-md);
	}
	.field {
		width: 100%;
	}
	.fit {
		width: auto;
		max-width: 100%;
		align-self: flex-start;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr));
		gap: var(--space-2);
	}
	.when {
		margin: var(--space-1) 0 0;
		font-weight: 600;
		font-size: var(--text-md);
	}
	.item-btns {
		display: flex;
		justify-content: flex-end;
	}
	.item-btns .btn,
	.savebar .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.savebar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: flex-end;
		gap: var(--space-2);
	}
	.small {
		font-size: var(--text-sm);
	}
	@media (max-width: 899px) {
		.btn,
		.field,
		label.inline {
			min-height: 44px;
		}
	}
</style>
