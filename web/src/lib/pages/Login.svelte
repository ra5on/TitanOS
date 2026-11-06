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
					<h1 id="login-title">{words.title}</h1>
					{#if host && !current?.hideHost}<span class="mono host">{host}</span>{/if}
				</div>
			</div>
			{#if words.notice}<p class="notice">{words.notice}</p>{/if}

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
					<label>
						<span>{t('auth.name')}</span>
						<!-- svelte-ignore a11y_autofocus -->
						<input
							class="field"
							type="text"
							bind:value={name}
							autocomplete="username"
							autocapitalize="none"
							spellcheck="false"
							required
							autofocus={!preview}
						/>
					</label>
					<label>
						<span>{t('auth.password')}</span>
						<input
							class="field"
							type="password"
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
			</form>
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
	h1 {
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
