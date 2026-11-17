<script lang="ts">
	// Setting up a mailbox in steps (docs/TMAIL.md, "Einrichten"): the
	// address → the servers proposed for it (GET /mail/providers, a fixed
	// list, nothing asked on the internet) → the password, with the
	// provider's hint about app passwords → "Verbindung prüfen" → save. The
	// password lives only in this form and goes to titand in the body of the
	// request; it is never stored in the browser.
	import Icon from '../components/Icon.svelte';
	import { apiErrorText, errorText, t, tOr } from '../i18n/index.svelte.ts';
	import { mailApi } from './api.ts';
	import ServerFields from './ServerFields.svelte';
	import { domainOf, providerHint, validAddress } from './text.ts';
	import type { MailAccount, Server, TestResult } from './types.ts';

	let { onsaved }: { onsaved: (account: MailAccount) => void } = $props();

	type Step = 'address' | 'servers' | 'password';
	let step = $state<Step>('address');
	let email = $state('');
	let name = $state('');
	let user = $state('');
	let imap = $state<Server>({ host: '', port: 993, security: 'tls' });
	let smtp = $state<Server>({ host: '', port: 465, security: 'tls' });
	let known = $state(false);
	let appPassword = $state(false);
	let editServers = $state(false);
	let password = $state('');
	let busy = $state(false);
	let error = $state('');
	let result = $state<TestResult | null>(null);

	const hint = $derived(providerHint(email));
	const passed = $derived(!!result?.imap.ok && !!result?.smtp.ok);
	const steps: Step[] = ['address', 'servers', 'password'];
	const number = $derived(steps.indexOf(step) + 1);

	async function toServers(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		if (!validAddress(email)) {
			error = t('mail.setup.badAddress');
			return;
		}
		busy = true;
		user = user || email.trim();
		try {
			const p = await mailApi.provider(email.trim());
			imap = { ...p.imap };
			smtp = { ...p.smtp };
			appPassword = !!p.appPassword;
			known = true;
			editServers = false;
		} catch {
			// Not in the list (or no answer): the usual names to start from, open to change.
			const domain = domainOf(email);
			imap = { host: `imap.${domain}`, port: 993, security: 'tls' };
			smtp = { host: `smtp.${domain}`, port: 465, security: 'tls' };
			appPassword = false;
			known = false;
			editServers = true;
		} finally {
			busy = false;
		}
		step = 'servers';
	}

	function changed() {
		result = null;
	}

	function body() {
		return {
			name: name.trim() || email.trim(),
			email: email.trim(),
			user: user.trim() || email.trim(),
			imap,
			smtp,
			notify: true,
			signature: '',
			password,
		};
	}

	async function check() {
		error = '';
		result = null;
		busy = true;
		try {
			result = await mailApi.test(body());
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	async function save() {
		error = '';
		busy = true;
		try {
			const saved = await mailApi.createAccount(body());
			password = '';
			onsaved(saved);
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	const secText = (s: Server['security']) =>
		s === 'starttls' ? t('mail.setup.starttls') : t('mail.setup.tls');
	const resultText = (r: { ok: boolean; code?: string }) =>
		r.ok ? t('mail.setup.ok') : r.code ? errorText(r.code) : t('mail.setup.failed');
</script>

<div class="wizard">
	<p class="muted steps" aria-live="polite">
		{t('mail.setup.step', { n: number, total: steps.length })}
	</p>

	{#if step === 'address'}
		<form class="part" onsubmit={toServers}>
			<h2>{t('mail.setup.addressTitle')}</h2>
			<label
				><span>{t('mail.setup.email')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input
					id="mail-setup-email"
					class="field"
					type="email"
					autocomplete="email"
					spellcheck="false"
					required
					autofocus
					bind:value={email}
				/>
			</label>
			<label
				><span>{t('mail.setup.name')}</span>
				<input id="mail-setup-name" class="field" type="text" bind:value={name} />
				<span class="muted small">{t('mail.setup.nameHint')}</span>
			</label>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="nav">
				<button type="submit" class="btn btn-primary" disabled={busy || !email.trim()}
					>{t('mail.setup.next')}</button
				>
			</div>
		</form>
	{:else if step === 'servers'}
		<form
			class="part"
			onsubmit={(e) => {
				e.preventDefault();
				step = 'password';
			}}
		>
			<h2>{t('mail.setup.serversTitle')}</h2>
			{#if known}
				<p class="notice-ok">{t('mail.setup.known', { domain: domainOf(email) })}</p>
			{:else}
				<p class="notice-warn">{t('mail.setup.unknown', { domain: domainOf(email) })}</p>
			{/if}
			{#if editServers}
				<ServerFields id="mail-setup" bind:user bind:imap bind:smtp onchange={changed} />
			{:else}
				<dl class="summary">
					<dt>{t('mail.setup.imap')}</dt>
					<dd>{imap.host}:{imap.port} · {secText(imap.security)}</dd>
					<dt>{t('mail.setup.smtp')}</dt>
					<dd>{smtp.host}:{smtp.port} · {secText(smtp.security)}</dd>
					<dt>{t('mail.setup.user')}</dt>
					<dd>{user}</dd>
				</dl>
				<button type="button" class="btn" onclick={() => (editServers = true)}
					>{t('mail.setup.editServers')}</button
				>
			{/if}
			<div class="nav">
				<button type="button" class="btn" onclick={() => (step = 'address')}
					>{t('mail.setup.back')}</button
				>
				<button
					type="submit"
					class="btn btn-primary"
					disabled={!imap.host || !smtp.host || !imap.port || !smtp.port}
					>{t('mail.setup.next')}</button
				>
			</div>
		</form>
	{:else}
		<form
			class="part"
			onsubmit={(e) => {
				e.preventDefault();
				void check();
			}}
		>
			<h2>{t('mail.setup.passwordTitle')}</h2>
			{#if appPassword}
				<p class="notice-warn app">
					<Icon name="key" size={16} /><span>{t('mail.setup.appPassword')}</span>
				</p>
			{/if}
			{#if hint}
				<p class="muted hint">{tOr(`mail.hint.${hint}`, '')}</p>
			{/if}
			<label
				><span>{appPassword ? t('mail.setup.appPasswordLabel') : t('mail.setup.password')}</span>
				<input
					id="mail-setup-password"
					class="field"
					type="password"
					autocomplete="off"
					required
					bind:value={password}
					oninput={changed}
				/>
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
			<div class="nav">
				<button type="button" class="btn" onclick={() => (step = 'servers')}
					>{t('mail.setup.back')}</button
				>
				<button
					type={passed ? 'button' : 'submit'}
					class="btn"
					class:btn-primary={!passed}
					disabled={busy || !password}>{t('mail.setup.check')}</button
				>
				{#if result}
					<button
						type="button"
						class="btn"
						class:btn-primary={passed}
						disabled={busy}
						onclick={save}>{passed ? t('mail.setup.save') : t('mail.setup.saveAnyway')}</button
					>
				{/if}
			</div>
			{#if busy}<p class="muted" role="status">{t('mail.setup.working')}</p>{/if}
		</form>
	{/if}
</div>

<style>
	.wizard {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		max-width: 40rem;
	}
	.steps {
		margin: 0;
		font-size: var(--text-sm);
	}
	.part {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-4);
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
	}
	h2 {
		margin: 0;
		font-size: var(--text-lg);
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
	.summary {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		gap: var(--space-1) var(--space-3);
		margin: 0;
	}
	.summary dt {
		color: var(--muted);
	}
	.summary dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.summary + .btn {
		align-self: flex-start;
	}
	.app {
		display: flex;
		gap: var(--space-2);
		align-items: flex-start;
	}
	.hint {
		margin: 0;
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
	.nav {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-2);
	}
	@media (max-width: 899px) {
		.part {
			padding: var(--space-3);
		}
		.btn,
		.field {
			min-height: 44px;
		}
	}
</style>
