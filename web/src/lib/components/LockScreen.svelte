<script lang="ts">
	// The lock screen of the desktop (PLAN 61, draft 32): clock and date, the
	// account, password (and code) or passkey. It covers everything while the
	// session is locked; the windows stay open behind it but cannot be
	// reached (inert, keys held back). In a frame of the desktop it only
	// covers the frame, without a form: the top window asks.
	import { onMount, tick } from 'svelte';
	import { ApiError } from '#lib/api.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import {
		inFrame,
		isLocked,
		lockMinutes,
		lockState,
		startLock,
		unlock,
		unlockWithPasskey,
	} from '#lib/lock.svelte.ts';
	import { passkeysPossible } from '#lib/passkeys.ts';
	import { logout, session } from '#lib/session.svelte.ts';
	import Icon from './Icon.svelte';

	let {
		background = '',
		tasks = 0,
		note = '',
	}: {
		/** CSS background of the screen (the desktop's picture); a calm gradient without. */
		background?: string;
		/** Tasks that keep running, for the quiet line at the bottom. */
		tasks?: number;
		/** A line of its own at the bottom instead of the default. */
		note?: string;
	} = $props();

	const user = $derived(session.data?.user ?? null);
	const shown = $derived(isLocked());
	const frame = inFrame();
	const initial = $derived(user?.name.charAt(0).toUpperCase() ?? '');
	const twoFactor = $derived(!!user?.twoFactor);
	const withPasskey = $derived(!!session.data?.passkey && passkeysPossible());

	let password = $state('');
	let code = $state('');
	let askCode = $state(false);
	let error = $state('');
	let now = $state(new Date());
	let field = $state<HTMLInputElement>();
	let codeField = $state<HTMLInputElement>();
	let root = $state<HTMLElement>();

	const time = $derived(
		new Intl.DateTimeFormat(i18n.locale, { hour: '2-digit', minute: '2-digit' }).format(now),
	);
	const date = $derived(
		new Intl.DateTimeFormat(i18n.locale, { weekday: 'long', day: 'numeric', month: 'long' }).format(
			now,
		),
	);
	const quiet = $derived.by(() => {
		if (note) return note;
		const parts: string[] = [];
		if (tasks === 1) parts.push(t('lock.task'));
		else if (tasks > 1) parts.push(t('lock.tasks', { count: tasks }));
		const minutes = lockMinutes();
		parts.push(minutes > 0 ? t('lock.after', { minutes }) : t('lock.manual'));
		return parts.join(' · ');
	});

	onMount(() => startLock());

	// Logged out (here or elsewhere): the next login starts unlocked.
	$effect(() => {
		if (!session.data?.user) lockState.locked = false;
	});

	// The clock, while the screen shows.
	$effect(() => {
		if (!shown) return;
		now = new Date();
		const timer = setInterval(() => (now = new Date()), 1000);
		return () => clearInterval(timer);
	});

	// A fresh form each time, the cursor in the password field.
	$effect(() => {
		if (!shown || frame) return;
		password = '';
		code = '';
		error = '';
		askCode = false;
		void tick().then(() => field?.focus());
	});

	// Everything else of the page is out of reach while locked: not
	// focusable, not read out, and no shortcut reaches it.
	$effect(() => {
		if (!shown || !root) return;
		const others = [...document.body.children].filter(
			(el): el is HTMLElement => el instanceof HTMLElement && el !== root,
		);
		const before = others.map((el) => [el.inert, el.getAttribute('aria-hidden')] as const);
		for (const el of others) {
			el.inert = true;
			el.setAttribute('aria-hidden', 'true');
		}
		// No key reaches a listener of the page (shortcuts such as Strg+K
		// listen on the window); typing into the fields still works, as it
		// is the default action. Escape does nothing at all.
		const hold = (e: KeyboardEvent) => {
			const inside = root?.contains(e.target as Node);
			e.stopImmediatePropagation();
			if (e.key === 'Escape' || !inside) e.preventDefault();
		};
		window.addEventListener('keydown', hold, true);
		return () => {
			window.removeEventListener('keydown', hold, true);
			others.forEach((el, i) => {
				el.inert = before[i][0];
				if (before[i][1] === null) el.removeAttribute('aria-hidden');
				else el.setAttribute('aria-hidden', before[i][1]!);
			});
		};
	});

	/** Moves the screen to the end of <body>, above everything of the page. */
	function toBody(node: HTMLElement) {
		document.body.appendChild(node);
		return { destroy: () => node.remove() };
	}

	function explain(err: unknown): string {
		if (err instanceof ApiError) {
			switch (err.code) {
				case 'wrong_password':
				case 'invalid_code':
				case 'passkey_failed':
				case 'passkey_expired':
				case 'second_factor_required':
					return t(`lock.error.${err.code}`);
				case 'rate_limited':
					return t('lock.error.rate_limited', { seconds: err.retryAfter || 30 });
				case 'unreachable':
					return t('lock.error.unreachable');
			}
			return t('lock.error.other');
		}
		if (err instanceof Error && (err.name === 'NotAllowedError' || err.message === 'cancelled'))
			return t('lock.error.passkey_cancelled');
		return t('lock.error.other');
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!password || lockState.busy) return;
		if ((twoFactor || askCode) && !code.trim()) {
			askCode = true;
			await tick();
			codeField?.focus();
			return;
		}
		error = '';
		try {
			await unlock({ password, code: code.trim() });
		} catch (err) {
			const reason = err instanceof ApiError ? err.code : '';
			// Only what was wrong is asked for again.
			if (reason === 'second_factor_required') askCode = true;
			if (reason === 'invalid_code') code = '';
			else if (reason !== 'second_factor_required') password = '';
			error = explain(err);
			await tick();
			if (reason === 'invalid_code' || reason === 'second_factor_required') codeField?.focus();
			else field?.focus();
		}
	}

	async function passkey() {
		error = '';
		try {
			await unlockWithPasskey();
		} catch (err) {
			error = explain(err);
		}
	}
</script>

{#if shown && user}
	<div
		class="lockscreen"
		class:frame
		style:--lock-background={background || null}
		bind:this={root}
		use:toBody
		role="dialog"
		aria-modal="true"
		aria-label={t('lock.title')}
	>
		{#if !frame}
			<div class="scrim" aria-hidden="true"></div>
			<div class="center">
				<p class="clock" aria-hidden="true">{time}</p>
				<p class="date">{date}</p>
				<div class="who">
					<span class="avatar" aria-hidden="true">{initial}</span>
					<h1 class="name">{user.name}</h1>
				</div>
				<form class="unlock" onsubmit={submit} novalidate>
					<div class="pill">
						<input
							bind:this={field}
							bind:value={password}
							type="password"
							autocomplete="current-password"
							placeholder={t('lock.password')}
							aria-label={t('lock.password')}
							aria-describedby={error ? 'lock-error' : undefined}
							disabled={lockState.busy}
						/>
						<button
							type="submit"
							class="go"
							aria-label={t('lock.submit')}
							title={t('lock.submit')}
							disabled={lockState.busy || !password}><Icon name="arrowRight" size={18} /></button
						>
					</div>
					{#if twoFactor || askCode}
						<input
							class="code"
							bind:this={codeField}
							bind:value={code}
							type="text"
							inputmode="numeric"
							autocomplete="one-time-code"
							spellcheck="false"
							placeholder={t('lock.code')}
							aria-label={t('lock.code')}
							disabled={lockState.busy}
						/>
					{/if}
					{#if error}<p class="error" id="lock-error" role="alert">{error}</p>{/if}
					{#if lockState.busy}<p class="busy" role="status">{t('lock.busy')}</p>{/if}
				</form>
				<div class="actions">
					{#if withPasskey}
						<button type="button" class="chip" onclick={passkey} disabled={lockState.busy}
							><Icon name="key" size={14} />{t('lock.passkey')}</button
						>
					{/if}
					<button type="button" class="chip quiet-chip" onclick={logout}
						><Icon name="logout" size={14} />{t('lock.logout')}</button
					>
				</div>
			</div>
			<p class="quiet">{quiet}</p>
		{/if}
	</div>
{/if}

<style>
	.lockscreen {
		position: fixed;
		inset: 0;
		z-index: 2147483000;
		display: flex;
		flex-direction: column;
		align-items: center;
		/* Taller than a short screen (phone across): scrolls from the top. */
		justify-content: safe center;
		padding: var(--space-5) var(--space-4) calc(var(--space-5) + env(safe-area-inset-bottom));
		overflow-y: auto;
		color: #fff;
		background: var(
			--lock-background,
			radial-gradient(ellipse at 70% 15%, rgb(59 110 200 / 0.45), transparent 55%),
			radial-gradient(ellipse at 15% 90%, rgb(40 80 170 / 0.5), transparent 60%),
			linear-gradient(160deg, #070d1c 0%, #0f1f45 55%, #183673 100%)
		);
		background-size: cover;
		background-position: center;
		animation: lock-in 0.25s ease-out both;
	}
	/* In a frame of the desktop: nothing behind, no form. */
	.lockscreen.frame {
		background: #070d1c;
		animation: none;
	}
	/* Whatever the picture: white text stays readable. */
	.scrim {
		position: absolute;
		inset: 0;
		background: rgb(0 0 0 / 0.32);
		pointer-events: none;
	}
	.center {
		position: relative;
		display: flex;
		flex-direction: column;
		align-items: center;
		width: min(340px, 100%);
		margin: auto 0;
	}
	.clock {
		margin: 0;
		font-size: clamp(64px, 18vw, 128px);
		font-weight: 300;
		line-height: 1;
		letter-spacing: -0.02em;
		font-variant-numeric: tabular-nums;
		text-shadow: 0 2px 16px rgb(0 0 0 / 0.35);
	}
	.date {
		margin: var(--space-3) 0 0;
		font-size: var(--text-lg, 20px);
		text-shadow: 0 1px 8px rgb(0 0 0 / 0.35);
		text-align: center;
	}
	.who {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-3);
		margin-top: clamp(32px, 8vh, 72px);
	}
	.avatar {
		width: 84px;
		height: 84px;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: 50%;
		background: var(--accent);
		color: var(--on-accent);
		font-size: 34px;
		font-weight: 700;
		box-shadow: 0 6px 24px rgb(0 0 0 / 0.3);
	}
	.name {
		margin: 0;
		font-size: var(--text-lg, 20px);
		font-weight: 600;
		max-width: 100%;
		overflow-wrap: anywhere;
		text-align: center;
	}
	.unlock {
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
		width: 100%;
		margin-top: 14px;
	}
	.pill {
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		padding: var(--space-1) var(--space-1) var(--space-1) var(--space-4);
		border: 1px solid rgb(255 255 255 / 0.28);
		border-radius: 999px;
		background: rgb(8 14 30 / 0.62);
		backdrop-filter: blur(12px);
	}
	.pill:focus-within {
		border-color: var(--accent);
		box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 55%, transparent);
	}
	.pill input,
	.code {
		flex: 1;
		min-width: 0;
		height: 36px;
		border: 0;
		background: transparent;
		color: #fff;
		font: inherit;
		font-size: var(--text-md, 15px);
		outline: none;
	}
	.pill input::placeholder,
	.code::placeholder {
		color: rgb(255 255 255 / 0.72);
	}
	.code {
		flex: none;
		height: 44px;
		padding: 0 var(--space-4);
		border: 1px solid rgb(255 255 255 / 0.28);
		border-radius: 999px;
		background: rgb(8 14 30 / 0.62);
		letter-spacing: 0.08em;
	}
	.code:focus {
		border-color: var(--accent);
	}
	.go {
		width: 36px;
		height: 36px;
		flex: none;
		display: flex;
		align-items: center;
		justify-content: center;
		border: 0;
		border-radius: 50%;
		background: var(--accent);
		color: var(--on-accent);
		cursor: pointer;
	}
	.go:disabled {
		opacity: 0.55;
		cursor: default;
	}
	.go:focus-visible,
	.chip:focus-visible {
		outline: 2px solid #fff;
		outline-offset: 2px;
	}
	.error,
	.busy {
		margin: 0;
		padding: var(--space-2) var(--space-3);
		border-radius: 10px;
		background: rgb(8 14 30 / 0.72);
		font-size: var(--text-sm, 13px);
		text-align: center;
	}
	.error {
		border: 1px solid rgb(255 150 150 / 0.7);
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: var(--space-2);
		margin-top: var(--space-3);
	}
	.chip {
		min-height: 34px;
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		padding: 0 14px;
		border: 1px solid rgb(255 255 255 / 0.25);
		border-radius: 999px;
		background: rgb(8 14 30 / 0.62);
		color: #fff;
		font: inherit;
		font-size: var(--text-sm, 13px);
		cursor: pointer;
	}
	.chip:hover {
		background: rgb(20 32 64 / 0.8);
	}
	.quiet-chip {
		border-color: transparent;
		background: transparent;
		color: rgb(255 255 255 / 0.85);
	}
	.quiet {
		position: relative;
		margin: var(--space-5) 0 0;
		font-size: var(--text-sm, 13px);
		color: rgb(255 255 255 / 0.88);
		text-align: center;
		text-shadow: 0 1px 6px rgb(0 0 0 / 0.5);
	}
	@keyframes lock-in {
		from {
			opacity: 0;
		}
	}
	/* Short screens (phone across): a smaller clock, the form in view. */
	@media (max-height: 520px) {
		.clock {
			font-size: 56px;
		}
		.date {
			margin-top: var(--space-1);
		}
		.who {
			flex-direction: row;
			margin-top: var(--space-4);
		}
		.avatar {
			width: 44px;
			height: 44px;
			font-size: 20px;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.lockscreen {
			animation: none;
		}
	}
</style>
