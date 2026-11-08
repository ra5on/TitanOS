<script lang="ts">
	// The login page (PLAN 74, owner's design of 8.11.2026): the moving
	// northern lights (or the picture the owner chose), no card; in the middle
	// a row of account pictures, the chosen one large with its name once below
	// and the password as a glass pill; TitanOS mark bottom left, time and date
	// top right. Password, second factor and passkey work as before. The
	// owner's login look (Settings › Appearance › Login page) still sets theme,
	// accent, picture, place, texts and the account list.
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
	import LiveBackground from '#lib/live/LiveBackground.svelte';
	import { isLivePicture } from '#lib/live/shaders.ts';
	import {
		pictureStyle,
		firstLetter,
		forgetAccount,
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
	/** PLAN 74: 'pick' – the row of accounts with the password below; 'form' – "Anderes Konto" (name and password). */
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
	const picking = $derived(step === 'pick' && accounts.length > 0);
	// The account the row shows large: the one picked, else the first (last used on this browser).
	const selected = $derived(
		picking ? (accounts.find((a) => a.name === chosen?.name) ?? accounts[0]) : null,
	);
	$effect(() => {
		if (selected && !needsCode) name = selected.name;
	});
	// The accounts of the NAS arrive after the page: then the password takes the focus once.
	let focused = false;
	$effect(() => {
		if (!picking || preview || focused || !passwordField) return;
		focused = true;
		const active = document.activeElement;
		if (!active || active === document.body) passwordField.focus();
	});
	const remembered = (name: string) => local.some((a) => a.name === name);

	async function pick(account: LoginAccount | null) {
		menuFor = null;
		chosen = account;
		name = account?.name ?? '';
		password = '';
		error = '';
		needsCode = false;
		step = account ? 'pick' : 'form';
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
		passwordField?.focus();
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
	// PLAN 74: the northern lights and the mountain lake move – as the owner's
	// built-in picture or as the desktop wallpaper of the last account here.
	const desktopPaper = loginWallpaper();
	const live = $derived(
		current?.background.source === 'builtin'
			? isLivePicture(current.background.id)
				? current.background.id
				: null
			: !picture && isLivePicture(desktopPaper)
				? desktopPaper
				: null,
	);
	const wallpaper = $derived(`${wallpaperCss(desktopPaper, !dark)} center / cover`);
	const words = $derived.by(() => {
		const texts = loginTexts(current, i18n.locale, t('auth.loginTitle'));
		// PLAN 74: the standard title is for screen readers only; an own title of the owner shows.
		return { ...texts, custom: texts.title !== t('auth.loginTitle') };
	});
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

	function sheen(e: PointerEvent) {
		const pill = (e.target as Element).closest<HTMLElement>('.pill');
		if (!pill) return;
		const r = pill.getBoundingClientRect();
		pill.style.setProperty('--sx', `${e.clientX - r.left}px`);
		pill.style.setProperty('--sy', `${e.clientY - r.top}px`);
	}

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
		{#if live && !preview}
			<LiveBackground picture={live} moving={!current?.still} />
		{:else if picture && current}
			<div
				class="picture"
				style:background-image={`url("${picture}")`}
				style:background-size={current.background.fit}
				style:background-position={`${current.background.x}% ${current.background.y}%`}
				style:transform={current.background.fit === 'cover'
					? `scale(${current.background.zoom / 100})`
					: undefined}
			></div>
		{:else if live}
			<div class="picture" style:background-image={`url("/backgrounds/${live}.webp")`}></div>
		{:else}
			<div class="picture wallpaper" style:background={wallpaper}></div>
		{/if}
		<div class="dim" style:opacity={(current?.background.dim ?? 30) / 100}></div>
	</div>

	<div class="time" aria-hidden={preview ? 'true' : undefined}>
		<p class="clock">{time}</p>
		<p class="date">{date}</p>
	</div>

	<section class="card" class:wide={current?.width === 'wide'} aria-labelledby="login-title">
		<!-- The editor's preview is no page of its own: no second main heading there. -->
		<svelte:element
			this={preview ? 'p' : 'h1'}
			class="h1"
			class:sr-only={!words.custom}
			id="login-title"
			>{words.title}{#if host && !current?.hideHost && !words.custom}<span>{' – ' + host}</span
				>{/if}</svelte:element
		>
		{#if words.notice}<p class="notice">{words.notice}</p>{/if}

		{#if picking && selected}
			<div class="picker" role="group" aria-label={t('login.pick')}>
				<ul class="accounts {pickStyle}">
					{#each accounts as account (account.name)}
						{@const shown = account.displayName ?? account.name}
						{@const pic = pictureStyle(account.name, account.picture)}
						{@const on = account.name === selected.name}
						<li
							class:on
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
								aria-pressed={on}
								onclick={() => pick(account)}
							>
								<span
									class="avatar"
									aria-hidden="true"
									style:background-image={`url("${pic.url}")`}
									style:background-position={pic.position}
									><span class="letter">{firstLetter(account)}</span></span
								>
								<span class="account-name">{shown}</span>
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
				</ul>
				<p class="chosen" aria-live="polite">{selected.displayName ?? selected.name}</p>
			</div>
		{/if}

		<form onsubmit={submit} onpointermove={sheen}>
			{#if picking && !needsCode}
				<!-- The picked account; the name goes along for password managers. -->
				<input
					class="sr-only"
					type="text"
					tabindex="-1"
					aria-hidden="true"
					value={name}
					autocomplete="username"
					readonly
				/>
			{/if}
			{#if needsCode}
				<p class="hint">{t('auth.codeHint')}</p>
				<div class="pill">
					<!-- svelte-ignore a11y_autofocus -->
					<input
						class="mono"
						type="text"
						bind:value={code}
						inputmode="numeric"
						autocomplete="one-time-code"
						spellcheck="false"
						placeholder={t('auth.code')}
						aria-label={t('auth.code')}
						required
						autofocus
					/>
					<button class="go" type="submit" disabled={busy} aria-label={t('auth.login')}
						><Icon name="arrowRight" size={16} /></button
					>
				</div>
			{:else}
				{#if !picking}
					<div class="pill plain">
						<!-- svelte-ignore a11y_autofocus -->
						<input
							type="text"
							bind:this={nameField}
							bind:value={name}
							autocomplete="username"
							autocapitalize="none"
							spellcheck="false"
							placeholder={t('auth.name')}
							aria-label={t('auth.name')}
							required
							autofocus={!preview && !accounts.length}
						/>
					</div>
				{/if}
				<div class="pill" class:filled={!!password}>
					<!-- svelte-ignore a11y_autofocus -->
					<input
						type="password"
						bind:this={passwordField}
						bind:value={password}
						autocomplete="current-password"
						placeholder={t('auth.password')}
						aria-label={t('auth.password')}
						required
						autofocus={!preview && picking}
					/>
					<button class="go" type="submit" disabled={busy} aria-label={t('auth.login')}
						><Icon name="arrowRight" size={16} /></button
					>
				</div>
			{/if}
			{#if error}<p class="error" role="alert">{error}</p>{/if}
			<div class="links">
				{#if needsCode}
					<button type="button" class="link" onclick={backToPicker}>{t('login.back')}</button>
				{:else}
					{#if picking}
						<button type="button" class="link" onclick={() => pick(null)}>{t('login.other')}</button
						>
					{:else if accounts.length}
						<button type="button" class="link" onclick={backToPicker}>{t('login.back')}</button>
					{/if}
					{#if passkeys}
						<button type="button" class="link" disabled={busy} onclick={withPasskey}
							>{t('passkeys.login')}</button
						>
					{/if}
				{/if}
			</div>
		</form>
	</section>

	{#if !current?.hideLogo}<div class="mark"><Logo size={22} /></div>{/if}
	<div class="lang"><LanguageSwitch /></div>
</main>

<style>
	/* PLAN 74: no card – the page is the picture with the form on it. White
	   text over the picture with a shade behind (contrast at least 4.5:1). */
	.login {
		position: relative;
		z-index: 1;
		min-height: 100vh;
		min-height: 100dvh;
		color: #fff;
		overflow: hidden;
	}
	.login.preview {
		min-height: 0;
		height: 100%;
	}
	.paper {
		position: fixed;
		inset: 0;
		z-index: -1;
		background: #0b0f16;
	}
	.preview .paper {
		position: absolute;
	}
	.picture {
		position: absolute;
		inset: 0;
		background: center / cover no-repeat;
	}
	.dim {
		position: absolute;
		inset: 0;
		background: radial-gradient(ellipse at 50% 52%, rgba(0, 0, 0, 0.35), rgba(0, 0, 0, 0.85));
	}
	.time {
		position: absolute;
		top: 24px;
		right: 30px;
		text-align: right;
		text-shadow: 0 1px 10px rgba(0, 0, 0, 0.55);
		font-variant-numeric: tabular-nums;
	}
	.clock {
		margin: 0;
		font-size: 30px;
		font-weight: 300;
		line-height: 1.1;
	}
	.date {
		margin: 2px 0 0;
		font-size: var(--text-sm);
	}
	.card {
		position: absolute;
		left: 50%;
		top: 50%;
		transform: translate(-50%, -50%);
		width: min(360px, calc(100% - 32px));
		display: flex;
		flex-direction: column;
		align-items: center;
		text-align: center;
	}
	.card.wide {
		width: min(480px, calc(100% - 32px));
	}
	.left .card {
		left: clamp(16px, 12vw, 220px);
		transform: translate(0, -50%);
	}
	.right .card {
		left: auto;
		right: clamp(16px, 12vw, 220px);
		transform: translate(0, -50%);
	}
	.h1 {
		margin: 0 0 14px;
		font-size: 22px;
		font-weight: 600;
		text-shadow: 0 1px 10px rgba(0, 0, 0, 0.5);
	}
	.notice {
		margin: 0 0 14px;
		padding: 8px 14px;
		border-radius: 12px;
		background: rgba(0, 0, 0, 0.45);
		font-size: var(--text-sm);
		white-space: pre-line;
	}
	.hint {
		margin: 0 0 10px;
		font-size: var(--text-sm);
		text-shadow: 0 1px 6px rgba(0, 0, 0, 0.6);
	}
	.picker {
		display: flex;
		flex-direction: column;
		align-items: center;
	}
	.accounts {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		flex-wrap: wrap;
		gap: 18px;
		min-height: 112px;
	}
	.accounts li {
		position: relative;
	}
	.account {
		all: unset;
		cursor: pointer;
		display: flex;
		flex-direction: column;
		align-items: center;
		border-radius: 50%;
		transition: transform 0.35s cubic-bezier(0.2, 0.9, 0.3, 1.2);
	}
	.account:focus-visible .avatar {
		outline: 2px solid #fff;
		outline-offset: 3px;
	}
	.avatar {
		position: relative;
		width: 56px;
		height: 56px;
		border-radius: 50%;
		background: center / cover no-repeat;
		box-shadow: 0 10px 30px -10px rgba(0, 0, 0, 0.7);
		opacity: 0.75;
		outline: 2px solid transparent;
		outline-offset: 3px;
		transition:
			width 0.4s cubic-bezier(0.2, 0.9, 0.3, 1.1),
			height 0.4s cubic-bezier(0.2, 0.9, 0.3, 1.1),
			opacity 0.3s,
			outline-color 0.3s;
	}
	.letter {
		position: absolute;
		inset: 0;
		display: grid;
		place-items: center;
		font-weight: 700;
		font-size: 22px;
		color: #fff;
		text-shadow: 0 1px 8px rgba(0, 0, 0, 0.6);
		transition: font-size 0.4s;
	}
	.account:hover {
		transform: translateY(-4px);
	}
	.account:hover .avatar {
		opacity: 1;
		outline-color: rgba(255, 255, 255, 0.55);
	}
	.on .avatar {
		width: 96px;
		height: 96px;
		opacity: 1;
		box-shadow: 0 18px 44px -14px rgba(0, 0, 0, 0.75);
	}
	.on .letter {
		font-size: 38px;
	}
	/* The names of the others appear on hover only; the chosen one stands below once. */
	.account-name {
		position: absolute;
		top: calc(100% + 8px);
		left: 50%;
		transform: translateX(-50%);
		font-size: 12px;
		white-space: nowrap;
		opacity: 0;
		pointer-events: none;
		text-shadow: 0 1px 6px rgba(0, 0, 0, 0.7);
		transition: opacity 0.2s;
	}
	li:not(.on) .account:is(:hover, :focus-visible) .account-name {
		opacity: 1;
	}
	.accounts.list {
		flex-direction: column;
		align-items: stretch;
		gap: 6px;
		min-height: 0;
	}
	.list .account {
		flex-direction: row;
		gap: 10px;
		border-radius: 12px;
		padding: 4px 10px 4px 4px;
	}
	.list .avatar,
	.list .on .avatar {
		width: 36px;
		height: 36px;
	}
	.list .letter,
	.list .on .letter {
		font-size: 15px;
	}
	.list .account-name {
		position: static;
		transform: none;
		opacity: 1;
		font-size: var(--text-base);
	}
	.list .on .account {
		background: rgba(255, 255, 255, 0.16);
	}
	.list + .chosen {
		display: none;
	}
	.account-more {
		position: absolute;
		top: -2px;
		right: -6px;
		width: 24px;
		height: 24px;
		border-radius: 50%;
		border: 0;
		display: grid;
		place-items: center;
		background: rgba(0, 0, 0, 0.55);
		color: #fff;
		cursor: pointer;
		opacity: 0;
		transition: opacity 0.2s;
	}
	li:hover .account-more,
	.account-more:focus-visible,
	.account-more[aria-expanded='true'] {
		opacity: 1;
	}
	.account-menu {
		position: absolute;
		z-index: 5;
		top: calc(100% + 6px);
		left: 50%;
		transform: translateX(-50%);
		min-width: 200px;
		padding: 4px;
		border-radius: 12px;
		background: rgba(20, 24, 30, 0.92);
		box-shadow: 0 16px 40px -12px rgba(0, 0, 0, 0.7);
	}
	.account-menu button {
		width: 100%;
		border: 0;
		background: none;
		color: #fff;
		font: inherit;
		text-align: left;
		padding: 8px 10px;
		border-radius: 8px;
		cursor: pointer;
	}
	.account-menu button:hover,
	.account-menu button:focus-visible {
		background: rgba(255, 255, 255, 0.12);
	}
	.chosen {
		margin: 16px 0 0;
		font-size: 20px;
		font-weight: 600;
		text-shadow: 0 1px 10px rgba(0, 0, 0, 0.55);
	}
	form {
		display: flex;
		flex-direction: column;
		align-items: center;
		width: 100%;
	}
	.pill {
		position: relative;
		margin-top: 16px;
		width: 300px;
		max-width: 100%;
		height: 44px;
		border-radius: 22px;
		display: flex;
		align-items: center;
		padding: 0 5px 0 18px;
		background: rgba(255, 255, 255, 0.16);
		border: 1px solid rgba(255, 255, 255, 0.3);
		backdrop-filter: blur(22px) saturate(1.5);
		-webkit-backdrop-filter: blur(22px) saturate(1.5);
		overflow: hidden;
		transition: background 0.25s;
	}
	.pill.plain {
		padding-right: 18px;
	}
	.pill + .pill {
		margin-top: 10px;
	}
	.pill:focus-within {
		background: rgba(255, 255, 255, 0.22);
	}
	/* A light edge runs round the pill while it has the focus. */
	.pill::before {
		content: '';
		position: absolute;
		inset: -1px;
		border-radius: 22px;
		padding: 1.5px;
		background: conic-gradient(
			from var(--edge-angle),
			transparent 0 70%,
			rgba(255, 255, 255, 0.95) 85%,
			transparent 100%
		);
		-webkit-mask:
			linear-gradient(#000 0 0) content-box,
			linear-gradient(#000 0 0);
		mask:
			linear-gradient(#000 0 0) content-box,
			linear-gradient(#000 0 0);
		-webkit-mask-composite: xor;
		mask-composite: exclude;
		opacity: 0;
		transition: opacity 0.3s;
		pointer-events: none;
	}
	.pill:focus-within::before {
		opacity: 1;
		animation: edge 2.6s linear infinite;
	}
	@property --edge-angle {
		syntax: '<angle>';
		inherits: false;
		initial-value: 0deg;
	}
	@keyframes edge {
		to {
			--edge-angle: 360deg;
		}
	}
	/* A soft sheen follows the pointer over the pill. */
	.pill::after {
		content: '';
		position: absolute;
		inset: 0;
		pointer-events: none;
		background: radial-gradient(
			110px circle at var(--sx, -200px) var(--sy, -200px),
			rgba(255, 255, 255, 0.18),
			transparent 70%
		);
	}
	.pill input {
		flex: 1;
		min-width: 0;
		border: 0;
		background: none;
		outline: none;
		color: #fff;
		font: inherit;
		font-size: 15px;
	}
	.pill input[type='password'] {
		letter-spacing: 0.08em;
	}
	.pill input::placeholder {
		color: rgba(255, 255, 255, 0.8);
		letter-spacing: 0;
	}
	.go {
		position: relative;
		z-index: 1;
		flex: none;
		width: 34px;
		height: 34px;
		border-radius: 50%;
		border: 0;
		display: grid;
		place-items: center;
		background: rgba(255, 255, 255, 0.92);
		color: #10151c;
		cursor: pointer;
	}
	.go :global(svg) {
		transition: transform 0.25s cubic-bezier(0.2, 0.9, 0.3, 1.3);
	}
	.go:hover :global(svg) {
		transform: translateX(3px);
	}
	.go:disabled {
		opacity: 0.6;
		cursor: default;
	}
	.error {
		margin: 10px 0 0;
		padding: 6px 12px;
		border-radius: 10px;
		background: rgba(0, 0, 0, 0.55);
		font-size: var(--text-sm);
	}
	.links {
		margin-top: 14px;
		display: flex;
		gap: 18px;
		justify-content: center;
		flex-wrap: wrap;
	}
	.link {
		border: 0;
		background: none;
		padding: 2px 0;
		color: #fff;
		font: inherit;
		font-size: 12.5px;
		opacity: 0.85;
		cursor: pointer;
		text-shadow: 0 1px 6px rgba(0, 0, 0, 0.7);
	}
	.link:hover,
	.link:focus-visible {
		opacity: 1;
		text-decoration: underline;
		text-underline-offset: 3px;
	}
	.mark {
		position: absolute;
		left: 30px;
		bottom: 26px;
		opacity: 0.92;
		filter: drop-shadow(0 1px 8px rgba(0, 0, 0, 0.5));
	}
	.lang {
		position: absolute;
		right: 30px;
		bottom: 22px;
	}
	.preview .time,
	.preview .mark,
	.preview .lang {
		transform: scale(0.8);
	}
	@media (max-width: 640px) {
		.time {
			top: 18px;
			right: 18px;
		}
		.clock {
			font-size: 22px;
		}
		.mark {
			left: 18px;
			bottom: 20px;
		}
		.lang {
			right: 18px;
			bottom: 16px;
		}
		.left .card,
		.right .card {
			left: 50%;
			right: auto;
			transform: translate(-50%, -50%);
		}
	}
</style>
