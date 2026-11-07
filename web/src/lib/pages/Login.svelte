<script lang="ts">
	// The login page in the style of the desktop (PLAN 64): the wallpaper of
	// the desktop (or the picture the owner chose for the login page), clock
	// and date, and one glass card with the form. Password, second factor and
	// passkey work as before. The owner's login look (Settings › Appearance ›
	// Login page) still sets theme, accent, picture, place, width and texts.
	import { ApiError } from '#lib/api.ts';
	import { apiErrorText, errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { login, refreshSession, session } from '#lib/session.svelte.ts';
	import { loginWithPasskey, passkeysPossible } from '#lib/passkeys.ts';
	import {
		loginAppearance,
		loginBackgroundURL,
		loginTexts,
		type LoginLook,
	} from '#lib/login/look.ts';
	import { loginLook } from '#lib/login/state.svelte.ts';
	import { loginWallpaper } from '#lib/login/wallpaper.ts';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { cssVariables, dataAttributes, resolvePalette } from '#lib/theme/themes.ts';
	import { wallpaperCss } from '#lib/desktop/wallpapers.ts';
	import Logo from '#lib/components/Logo.svelte';
	import LanguageSwitch from '#lib/components/LanguageSwitch.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import {
		forgetAccount,
		initials,
		mergeAccounts,
		rememberAccount,
		rememberedAccounts,
		serverAccounts,
		type AccountsStyle,
		type LoginAccount,
	} from '#lib/login/accounts.ts';
	import { onMount, tick } from 'svelte';

	type Props = {
		/** Only shows the page (preview of Settings › Appearance): nothing is sent. */
		preview?: boolean;
		look?: LoginLook | null;
		imageURL?: string;
		hostname?: string;
	};
	let { preview = false, look, imageURL = '', hostname }: Props = $props();

	let name = $state('');
	let password = $state('');
	let busy = $state(false);
	let error = $state('');
	// Set once the password was right and the account asks for a second factor.
	let needsCode = $state(false);
	let code = $state('');
	const passkeys = passkeysPossible();

	// PLAN 67: the account picker. Accounts of this browser (remembered after
	// a login) and, where the owner allows it, those the NAS lists. Picking
	// one fills the name and asks for the password; "Anderes Konto" shows the
	// plain form. Nothing about an account is told by timing or wording: the
	// same error as ever for a wrong name or password.
	let local = $state<LoginAccount[]>([]);
	let server = $state<{ style: AccountsStyle; accounts: LoginAccount[] } | null>(null);
	/** 'pick' while the picker shows, 'form' once an account (or "other") is chosen. */
	let step = $state<'pick' | 'form'>('pick');
	let chosen = $state<LoginAccount | null>(null);
	let menuFor = $state<string | null>(null);
	let nameField = $state<HTMLInputElement>();
	let passwordField = $state<HTMLInputElement>();

	onMount(() => {
		if (preview) return;
		local = rememberedAccounts();
		void serverAccounts().then((answer) => (server = answer));
	});
	const accounts = $derived.by<LoginAccount[]>(() => {
		// The editor's preview shows the signed-in account only, in the chosen style.
		if (preview) {
			const me = session.data?.user?.name;
			return me ? [{ name: me }] : [];
		}
		return mergeAccounts(local, server?.accounts ?? []);
	});
	const picking = $derived(step === 'pick' && accounts.length > 0 && !needsCode);
	const remembered = (name: string) => local.some((a) => a.name === name);

	async function pick(account: LoginAccount | null) {
		menuFor = null;
		chosen = account;
		name = account?.name ?? '';
		password = '';
		error = '';
		step = 'form';
		await tick();
		(account ? passwordField : nameField)?.focus();
	}
	async function backToPicker() {
		step = 'pick';
		chosen = null;
		name = '';
		password = '';
		error = '';
		await tick();
		document.querySelector<HTMLElement>('.accounts .account')?.focus();
	}
	function forget(account: LoginAccount) {
		forgetAccount(account.name);
		local = rememberedAccounts();
		menuFor = null;
	}
	function remember() {
		const user = session.data?.user;
		if (user) rememberAccount(user.name);
	}

	const current = $derived(look === undefined ? loginLook.value : look);
	const resolved = $derived(loginAppearance(current, appearance.current));
	const style = $derived(
		resolved.scoped && current?.theme !== 'inherit'
			? Object.entries(cssVariables(resolved.appearance, appearance.systemLight))
					.map(([key, value]) => `${key}: ${value}`)
					.join('; ')
			: undefined,
	);
	const attributes = $derived(
		resolved.scoped && current?.theme !== 'inherit'
			? dataAttributes(resolved.appearance, appearance.systemLight)
			: {},
	);
	const dark = $derived(resolvePalette(resolved.appearance, appearance.systemLight).dark);
	const picture = $derived(loginBackgroundURL(current, imageURL));
	const wallpaper = $derived(`${wallpaperCss(loginWallpaper(), !dark)} center / cover`);
	const words = $derived(loginTexts(current, i18n.locale, t('auth.loginTitle')));
	const host = $derived(hostname ?? session.data?.hostname ?? '');
	const position = $derived(current?.position ?? 'center');
	const pickStyle = $derived<AccountsStyle>(
		preview ? (current?.accountsStyle ?? 'circles') : (server?.style ?? 'circles'),
	);

	let now = $state(new Date());
	$effect(() => {
		const timer = setInterval(() => (now = new Date()), 1000);
		return () => clearInterval(timer);
	});
	const time = $derived(
		new Intl.DateTimeFormat(i18n.locale, { hour: '2-digit', minute: '2-digit' }).format(now),
	);
	const date = $derived(
		new Intl.DateTimeFormat(i18n.locale, { weekday: 'long', day: 'numeric', month: 'long' }).format(
			now,
		),
	);

	async function withPasskey() {
		if (preview) return;
		busy = true;
		error = '';
		try {
			await loginWithPasskey();
			await refreshSession();
			remember();
		} catch (err) {
			if (err instanceof DOMException || (err instanceof Error && err.message === 'cancelled')) {
				error = t(
					err instanceof DOMException && err.name !== 'NotAllowedError'
						? 'passkeys.browserError'
						: 'passkeys.cancelled',
				);
			} else if (err instanceof ApiError && err.code === 'rate_limited' && err.retryAfter > 0) {
				error = t('auth.rateLimited', { seconds: err.retryAfter });
			} else {
				error = apiErrorText(err);
			}
		} finally {
			busy = false;
		}
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (preview) return;
		busy = true;
		error = '';
		try {
			await login(name, password, code);
			remember();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'second_factor_required') {
				needsCode = true;
				return;
			}
			if (err instanceof ApiError && err.code === 'invalid_code') {
				code = '';
				error = errorText(err.code);
				return;
			}
			password = '';
			code = '';
			needsCode = false;
			if (err instanceof ApiError && err.code === 'rate_limited' && err.retryAfter > 0) {
				error = t('auth.rateLimited', { seconds: err.retryAfter });
			} else {
				error = apiErrorText(err);
			}
		} finally {
			busy = false;
		}
	}
</script>

<svelte:window
	onclick={(e) => {
		if (menuFor && !(e.target as Element).closest?.('.account-menu, .account-more')) menuFor = null;
	}}
/>

<main
	class="login"
	class:preview
	class:light={!dark}
	class:left={position === 'left'}
	class:right={position === 'right'}
	{style}
	{...attributes}
	data-testid="login-page"
>
	<div class="paper" aria-hidden="true">
		{#if picture && current}
			<div
				class="picture"
				style:background-image={`url("${picture}")`}
				style:background-size={current.background.fit}
				style:background-position={`${current.background.x}% ${current.background.y}%`}
				style:transform={current.background.fit === 'cover'
					? `scale(${current.background.zoom / 100})`
					: undefined}
			></div>
			<div class="dim" style:opacity={current.background.dim / 100}></div>
		{:else}
			<div class="picture wallpaper" style:background={wallpaper}></div>
		{/if}
	</div>

	<div class="content">
		<div class="time" aria-hidden={preview ? 'true' : undefined}>
			<p class="clock mono">{time}</p>
			<p class="date">{date}</p>
		</div>

		<section class="card" class:wide={current?.width === 'wide'} aria-labelledby="login-title">
			<div class="head">
				{#if !current?.hideLogo}<Logo size={44} stacked />{/if}
				<div class="titles">
					<!-- The editor's preview is no page of its own: no second main heading there. -->
					<svelte:element this={preview ? 'p' : 'h1'} class="h1" id="login-title"
						>{words.title}</svelte:element
					>
					{#if host && !current?.hideHost}<span class="mono host">{host}</span>{/if}
				</div>
			</div>
			{#if words.notice}<p class="notice">{words.notice}</p>{/if}

			{#if picking}
				<div class="picker" role="group" aria-label={t('login.pick')}>
					<p class="pick-title">{t('login.pick')}</p>
					<ul class="accounts {pickStyle}">
						{#each accounts as account (account.name)}
							{@const shown = account.displayName ?? account.name}
							<li
								oncontextmenu={(e) => {
									if (!remembered(account.name) || preview) return;
									e.preventDefault();
									menuFor = account.name;
								}}
							>
								<button
									type="button"
									class="account"
									aria-label={t('login.as', { name: shown })}
									onclick={() => pick(account)}
								>
									<span class="avatar" aria-hidden="true">{initials(account)}</span>
									<span class="account-name">{shown}</span>
									{#if account.displayName && account.displayName !== account.name}<span
											class="account-login mono">{account.name}</span
										>{/if}
								</button>
								{#if remembered(account.name) && !preview}
									<button
										type="button"
										class="account-more"
										aria-label={t('login.accountMenu', { name: shown })}
										aria-haspopup="menu"
										aria-expanded={menuFor === account.name}
										onclick={() => (menuFor = menuFor === account.name ? null : account.name)}
										><Icon name="more" size={14} /></button
									>
									{#if menuFor === account.name}
										<div
											class="account-menu"
											role="menu"
											tabindex="-1"
											onkeydown={(e) => {
												if (e.key === 'Escape') menuFor = null;
											}}
										>
											<!-- svelte-ignore a11y_autofocus -->
											<button
												type="button"
												role="menuitem"
												autofocus
												onclick={() => forget(account)}
												>{t('login.forgetNamed', { name: shown })}</button
											>
										</div>
									{/if}
								{/if}
							</li>
						{/each}
						<li>
							<button type="button" class="account other" onclick={() => pick(null)}>
								<span class="avatar" aria-hidden="true"><Icon name="plus" size={18} /></span>
								<span class="account-name">{t('login.other')}</span>
							</button>
						</li>
					</ul>
				</div>
			{:else}
				<form onsubmit={submit}>
					{#if needsCode}
						<p class="hint">{t('auth.codeHint')}</p>
						<label>
							<span>{t('auth.code')}</span>
							<!-- svelte-ignore a11y_autofocus -->
							<input
								class="field mono"
								type="text"
								bind:value={code}
								inputmode="numeric"
								autocomplete="one-time-code"
								spellcheck="false"
								required
								autofocus
							/>
						</label>
					{:else}
						{#if chosen}
							<!-- The picked account; the name goes along for password managers. -->
							<div class="chosen">
								<span class="avatar small" aria-hidden="true">{initials(chosen)}</span>
								<span class="chosen-name">{chosen.displayName ?? chosen.name}</span>
								<button type="button" class="link" onclick={backToPicker}
									>{t('login.switch')}</button
								>
							</div>
							<input
								class="sr-only"
								type="text"
								tabindex="-1"
								aria-hidden="true"
								value={name}
								autocomplete="username"
								readonly
							/>
						{:else}
							<label>
								<span>{t('auth.name')}</span>
								<!-- svelte-ignore a11y_autofocus -->
								<input
									class="field"
									type="text"
									bind:this={nameField}
									bind:value={name}
									autocomplete="username"
									autocapitalize="none"
									spellcheck="false"
									required
									autofocus={!preview && !accounts.length}
								/>
							</label>
						{/if}
						<label>
							<span>{t('auth.password')}</span>
							<input
								class="field"
								type="password"
								bind:this={passwordField}
								bind:value={password}
								autocomplete="current-password"
								required
							/>
						</label>
					{/if}
					{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
					<button class="btn btn-primary" type="submit" disabled={busy}
						>{t('auth.login')}<Icon name="arrowRight" size={16} /></button
					>
					{#if passkeys && !needsCode}
						<button class="btn" type="button" disabled={busy} onclick={withPasskey}
							><Icon name="key" size={16} />{t('passkeys.login')}</button
						>
					{/if}
					{#if accounts.length && !chosen && !needsCode}
						<button type="button" class="link back" onclick={backToPicker}>{t('login.back')}</button
						>
					{/if}
				</form>
			{/if}
		</section>
	</div>

	<div class="lang"><LanguageSwitch /></div>
</main>

<style>
	.login {
		position: relative;
		z-index: 1;
		min-height: 100vh;
		min-height: 100dvh;
		display: flex;
		flex-direction: column;
		color: var(--text);
		isolation: isolate;
		overflow: hidden;
		padding: calc(var(--space-5) + env(safe-area-inset-top)) var(--space-4)
			calc(var(--space-3) + env(safe-area-inset-bottom));
	}
	.login.preview {
		position: absolute;
		inset: 0;
		min-height: 0;
	}
	.paper {
		position: fixed;
		inset: 0;
		z-index: -1;
		background: #070d1c;
	}
	.preview .paper {
		position: absolute;
	}
	.picture {
		position: absolute;
		inset: 0;
		background-repeat: no-repeat;
	}
	.wallpaper {
		filter: var(--wallpaper-filter, none);
	}
	.dim {
		position: absolute;
		inset: 0;
		background: var(--image-dim, #000);
	}
	.content {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		/* A short screen (phone across) scrolls from the top. */
		justify-content: safe center;
		gap: clamp(20px, 5vh, 48px);
		width: 100%;
	}
	.left .content {
		align-items: flex-start;
		padding-left: max(0px, calc(8vw - var(--space-4)));
	}
	.right .content {
		align-items: flex-end;
		padding-right: max(0px, calc(8vw - var(--space-4)));
	}
	.time {
		color: #fff;
		text-align: center;
		text-shadow: 0 2px 16px rgb(0 0 0 / 0.4);
	}
	.clock {
		margin: 0;
		font-size: clamp(56px, 14vw, 104px);
		font-weight: 300;
		line-height: 1;
		letter-spacing: -0.02em;
		font-variant-numeric: tabular-nums;
	}
	.date {
		margin: var(--space-2) 0 0;
		font-size: var(--text-lg);
	}
	.card {
		width: min(380px, 100%);
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		padding: var(--space-5);
		border-radius: calc(var(--radius-tile) + 4px);
		background: var(--desk-surface, color-mix(in srgb, var(--surface) 72%, transparent));
		backdrop-filter: blur(var(--desk-blur, 22px)) saturate(1.4);
		-webkit-backdrop-filter: blur(var(--desk-blur, 22px)) saturate(1.4);
		border: 1px solid color-mix(in srgb, #fff 14%, transparent);
		box-shadow: 0 20px 50px rgb(0 0 0 / 0.35);
	}
	.card.wide {
		width: min(480px, 100%);
	}
	.head {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-3);
		min-width: 0;
		text-align: center;
	}
	.titles {
		display: flex;
		flex-direction: column;
		align-items: center;
		min-width: 0;
	}
	.h1 {
		margin: 0;
		font-size: var(--text-lg);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.host {
		color: var(--muted);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.notice {
		margin: 0;
		font-size: var(--text-md);
		white-space: pre-line;
		overflow-wrap: anywhere;
	}
	form {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.hint {
		margin: 0;
		font-size: var(--text-md);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: var(--text-md);
	}
	form .btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
	}
	/* PLAN 67: the account picker – circles (avatar and name) or a list. */
	.picker {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.pick-title {
		margin: 0;
		text-align: center;
		color: var(--muted);
		font-size: var(--text-md);
	}
	.accounts {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.accounts li {
		position: relative;
		min-width: 0;
	}
	.account {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		min-width: 0;
		padding: 8px;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.account:hover,
	.account:focus-visible {
		background: color-mix(in srgb, var(--text) 8%, transparent);
	}
	.avatar {
		flex: none;
		display: grid;
		place-items: center;
		width: 40px;
		height: 40px;
		border-radius: 50%;
		background: var(--accent);
		color: var(--on-accent);
		font-weight: 600;
		font-size: var(--text-md);
	}
	.other .avatar {
		background: color-mix(in srgb, var(--text) 12%, transparent);
		color: var(--text);
	}
	.account-name {
		min-width: 0;
		font-size: var(--text-md);
		font-weight: 500;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.account-login {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	/* Circles: a row of avatars with the name under each. */
	.accounts.circles {
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: var(--space-2);
	}
	.circles li {
		width: 92px;
	}
	.circles .account {
		flex-direction: column;
		gap: 6px;
		padding: 10px 4px;
		text-align: center;
	}
	.circles .avatar {
		width: 56px;
		height: 56px;
		font-size: var(--text-lg);
	}
	/* Names wrap (two lines) instead of being cut off. */
	.circles .account-name {
		max-width: 100%;
		font-size: var(--text-base);
		white-space: normal;
		overflow-wrap: anywhere;
		line-height: 1.25;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
	}
	.circles .account-login {
		display: none;
	}
	/* List: one row per account. */
	.accounts.list {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.list .account {
		padding-right: 40px;
	}
	.account-more {
		position: absolute;
		top: 4px;
		right: 4px;
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		padding: 0;
		background: transparent;
		border: 0;
		border-radius: 50%;
		color: var(--muted);
		cursor: pointer;
	}
	.list .account-more {
		top: 50%;
		transform: translateY(-50%);
	}
	.account-more:hover,
	.account-more:focus-visible {
		background: color-mix(in srgb, var(--text) 12%, transparent);
		color: var(--text);
	}
	.account-menu {
		position: absolute;
		z-index: 5;
		top: 34px;
		right: 0;
		min-width: 200px;
		padding: 4px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		box-shadow: 0 10px 30px rgb(0 0 0 / 0.35);
	}
	.account-menu button {
		width: 100%;
		padding: 8px 10px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-small, 6px);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		text-align: left;
		cursor: pointer;
	}
	.account-menu button:hover,
	.account-menu button:focus-visible {
		background: color-mix(in srgb, var(--text) 10%, transparent);
	}
	.chosen {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
	}
	.avatar.small {
		width: 32px;
		height: 32px;
		font-size: var(--text-base);
	}
	.chosen-name {
		flex: 1;
		min-width: 0;
		font-weight: 600;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.link {
		padding: 4px 0;
		background: none;
		border: 0;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-base);
		text-decoration: underline;
		cursor: pointer;
	}
	.link.back {
		align-self: center;
	}
	.lang {
		display: flex;
		justify-content: center;
		padding-top: var(--space-3);
	}
	.lang :global(select),
	.lang :global(button) {
		color: var(--text);
	}
	@media (max-width: 480px) {
		.card {
			padding: var(--space-4);
		}
		.left .content,
		.right .content {
			align-items: center;
			padding: 0;
		}
	}
	@media (prefers-reduced-transparency: reduce) {
		.card {
			background: var(--surface);
			backdrop-filter: none;
			-webkit-backdrop-filter: none;
		}
	}
</style>
