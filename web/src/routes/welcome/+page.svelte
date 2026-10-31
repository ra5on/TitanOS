<script lang="ts">
	// The wizard after the first start: name of the NAS and time zone,
	// a look at the network, and what to do next. Every step can be skipped.
	import NetworkBridgeSetup from '#lib/components/NetworkBridgeSetup.svelte';
	import { api, ApiError } from '#lib/api.ts';
	import LanguageSwitch from '#lib/components/LanguageSwitch.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { pageTitle } from '#lib/page-title.svelte.ts';
	import { refreshSession, session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { replaceState } from '$app/navigation';
	import { page } from '$app/state';
	import { untrack } from 'svelte';

	const STEPS = 3;
	const STEP_NAMES = ['welcome.basics', 'welcome.network', 'welcome.done'] as const;
	// The browser tab names the page like every other one.
	$effect(() => {
		const shown = t('welcome.title');
		pageTitle.value = shown;
		return () => {
			if (pageTitle.value === shown) pageTitle.value = '';
		};
	});
	// The step is part of the address, so that reloading stays on it.
	const askedStep = Number(untrack(() => page.url.searchParams.get('step')));
	let step = $state(askedStep >= 1 && askedStep <= STEPS ? Math.floor(askedStep) : 1);
	$effect(() => {
		const wanted = String(step);
		if (untrack(() => page.url.searchParams.get('step')) === wanted) return;
		const url = new URL(untrack(() => page.url.href));
		url.searchParams.set('step', wanted);
		// Shallow: only the address changes, nothing is loaded again.
		replaceState(url, {});
	});
	let hostname = $state(session.data?.hostname ?? 'titanos');
	// The browser knows where it is; the NAS starts out in UTC.
	let timezone = $state(Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC');
	const zones = $derived([...new Set([timezone, 'UTC', ...Intl.supportedValuesOf('timeZone')])]);
	let busy = $state(false);
	let error = $state('');
	// Same rule as the backend: letters, digits and hyphens, no hyphen at the ends.
	const HOSTNAME = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;
	let hostnameProblem = $state('');
	let hostnameField = $state<HTMLInputElement>();
	const address = $derived(system.info?.network?.ipv4.split('/')[0] ?? location.hostname);

	// What is already done; null while unknown (not loaded or not readable).
	type Todo = {
		href: string;
		key:
			| 'welcome.todoVolume'
			| 'welcome.todoShare'
			| 'welcome.todoTwoFactor'
			| 'welcome.todoNotify'
			| 'welcome.todoStick'
			| 'welcome.todoApps'
			| 'welcome.todoDrivers'
			| 'welcome.todoBackup'
			| 'welcome.todoRemote'
			| 'welcome.todoPush';
		done: boolean | null;
	};
	let todos = $state<Todo[]>([]);
	let checking = $state(false);
	let checkFailed = $state(false);
	const doneCount = $derived(todos.filter((todo) => todo.done).length);
	const knownCount = $derived(todos.filter((todo) => todo.done !== null).length);
	async function check() {
		checkFailed = false;
		checking = true;
		const [storage, shares, notify, stick, plugins, backups, tailnet, wireguard, push] =
			await Promise.allSettled([
				api.get<{ volumes: unknown[] }>('/storage'),
				api.get<unknown[]>('/shares'),
				api.get<{ settings: Record<string, unknown> }>('/notifications/settings'),
				api.get<{ backup: { lastError: string; backups: unknown[] } }>('/config-backup'),
				api.get<{ recommendations?: unknown[] | null }>('/plugins'),
				api.get<{ jobs: { job: { enabled: boolean } }[] }>('/backups'),
				api.get<{ state: string }>('/plugins/tailscale'),
				api.get<{ enabled: boolean }>('/network/wireguard'),
				api.get<{ devices: unknown[] }>('/notifications/push'),
			]);
		// An answer of an unexpected shape counts as "unknown", not as an error of the page.
		const value = <T,>(result: PromiseSettledResult<T>, test: (value: T) => boolean) => {
			if (result.status !== 'fulfilled') return null;
			try {
				return test(result.value);
			} catch {
				return null;
			}
		};
		const channel = (settings: Record<string, unknown>) =>
			['email', 'ntfy', 'telegram', 'discord', 'homeAssistant'].some(
				(name) => (settings[name] as { enabled?: boolean } | undefined)?.enabled,
			);
		todos = [
			{
				href: '/storage',
				key: 'welcome.todoVolume',
				done: value(storage, (s) => s.volumes.length > 0),
			},
			{
				href: '/shares',
				key: 'welcome.todoShare',
				done: value(shares, (list) => Array.isArray(list) && list.length > 0),
			},
			{
				href: '/account',
				key: 'welcome.todoTwoFactor',
				done: session.data?.user ? session.data.user.twoFactor : null,
			},
			{
				href: '/settings/notifications',
				key: 'welcome.todoNotify',
				done: value(notify, (n) => channel(n.settings)),
			},
			{
				href: '/settings/backups/configuration',
				key: 'welcome.todoStick',
				done: value(stick, (c) => c.backup.backups.length > 0 && !c.backup.lastError),
			},
			{
				href: '/settings/backups/data',
				key: 'welcome.todoBackup',
				done: value(backups, (b) => b.jobs.some((j) => j.job.enabled)),
			},
			{
				href: '/settings/network?tab=remote',
				key: 'welcome.todoRemote',
				done:
					(tailnet.status === 'fulfilled' && tailnet.value.state === 'Running') ||
					(wireguard.status === 'fulfilled' && wireguard.value.enabled)
						? true
						: tailnet.status === 'rejected' && wireguard.status === 'rejected'
							? null
							: false,
			},
			{
				href: '/settings/notifications#push',
				key: 'welcome.todoPush',
				done: value(push, (p) => p.devices.length > 0),
			},
			{ href: '/docker', key: 'welcome.todoApps', done: null },
		];
		// Plugins for detected hardware (drivers, tools): only when there are any.
		if (plugins.status === 'fulfilled' && (plugins.value.recommendations?.length ?? 0) > 0) {
			todos.splice(1, 0, { href: '/plugins', key: 'welcome.todoDrivers', done: false });
		}
		checkFailed = [storage, shares, notify, stick].some((result) => result.status === 'rejected');
		checking = false;
	}
	$effect(() => {
		if (step === STEPS) untrack(check);
	});

	async function saveBasics(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		hostnameProblem = HOSTNAME.test(hostname) ? '' : t('welcome.hostnameInvalid');
		if (hostnameProblem) {
			hostnameField?.focus();
			return;
		}
		busy = true;
		try {
			await api.put('/system/timezone', { timezone });
			if (hostname !== session.data?.hostname) {
				await api.put('/system/hostname', { hostname });
				await refreshSession();
			}
			step = 2;
		} catch (err) {
			// In development mode nothing is changed; the wizard still shows its steps.
			if (err instanceof ApiError && err.code === 'read_only') step = 2;
			else error = apiErrorText(err, { detail: true });
		}
		busy = false;
	}
</script>

<div class="frame">
	<section class="tile" aria-labelledby="welcome-title">
		<div class="head">
			<h1 id="welcome-title">{t('welcome.title')}</h1>
			<ol class="steps" aria-label={t('welcome.step', { n: step, total: STEPS })}>
				{#each STEP_NAMES as name, index (name)}
					<li>
						<button
							type="button"
							class="step"
							class:current={step === index + 1}
							aria-current={step === index + 1 ? 'step' : undefined}
							aria-label="{index + 1}. {t(name)}"
							onclick={() => (step = index + 1)}
							><span class="number" aria-hidden="true">{index + 1}</span><span class="label"
								>{t(name)}</span
							></button
						>
					</li>
				{/each}
			</ol>
		</div>

		{#if step === 1}
			<form onsubmit={saveBasics} novalidate>
				<h2>{t('welcome.basics')}</h2>
				<p class="muted">{t('welcome.intro')}</p>
				<div class="group">
					<span class="caption" aria-hidden="true">{t('lang.label')}</span>
					<LanguageSwitch />
				</div>
				<label
					><span class="caption">{t('welcome.hostname')}</span>
					<input
						bind:this={hostnameField}
						class="field mono"
						type="text"
						value={hostname}
						oninput={(event) => {
							// Host names are lower case; typed capitals are changed right away.
							const field = event.currentTarget;
							const at = field.selectionStart;
							hostname = field.value.toLowerCase();
							field.value = hostname;
							if (at !== null) field.setSelectionRange(at, at);
							hostnameProblem = '';
						}}
						maxlength="63"
						autocapitalize="none"
						spellcheck="false"
						required
						aria-invalid={hostnameProblem !== ''}
						aria-describedby={hostnameProblem ? 'hostname-problem hostname-hint' : 'hostname-hint'}
					/>
					{#if hostnameProblem}<small class="problem" id="hostname-problem">{hostnameProblem}</small
						>{/if}
					<small id="hostname-hint"
						>{t('welcome.hostnameHint')}
						{#if HOSTNAME.test(hostname)}{t('welcome.localName', {
								name: `${hostname}.local`,
							})}{/if}</small
					>
				</label>
				<label
					><span class="caption">{t('welcome.timezone')}</span>
					<select class="field" bind:value={timezone} aria-describedby="timezone-hint"
						>{#each zones as zone (zone)}<option value={zone}>{zone}</option>{/each}</select
					>
					<small id="timezone-hint">{t('welcome.timezoneHint')}</small>
				</label>
				{#if error}<p class="notice-error pre" role="alert">{error}</p>{/if}
				<div class="actions">
					<button type="button" class="btn" onclick={() => (step = 2)}>{t('welcome.skip')}</button>
					<button type="submit" class="btn btn-primary" disabled={busy}
						>{t(busy ? 'welcome.saving' : 'welcome.next')}</button
					>
				</div>
			</form>
		{:else if step === 2}
			<div class="body">
				<h2>{t('welcome.network')}</h2>
				<p>{t('welcome.networkNow', { address })}</p>
				<p class="muted">{t('welcome.networkHint')}</p>
				<div class="panel">
					<p class="caption">{t('welcome.bridgeTitle')}</p>
					<p class="muted">{t('welcome.bridgeText')}</p>
					<NetworkBridgeSetup auto />
				</div>
				<a href="/settings/network">{t('welcome.networkSettings')}</a>
				<div class="actions">
					<button type="button" class="btn" onclick={() => (step = 1)}>{t('welcome.back')}</button>
					<button type="button" class="btn btn-primary" onclick={() => (step = 3)}
						>{t('welcome.next')}</button
					>
				</div>
			</div>
		{:else}
			<div class="body">
				<h2>{t('welcome.done')}</h2>
				<p class="muted">{t('welcome.doneText')}</p>
				{#if checking && todos.length === 0}<p class="muted" role="status">
						{t('welcome.checking')}
					</p>{/if}
				{#if knownCount > 0}<p class="progress">
						{t('welcome.progress', { done: doneCount, total: knownCount })}
					</p>{/if}
				{#if checkFailed}<div class="notice-warn row" role="alert">
						<span>{t('welcome.checkFailed')}</span>
						<button type="button" class="btn small" onclick={check}>{t('ui.retry')}</button>
					</div>{/if}
				<ul class="todos">
					{#each todos as todo (todo.href)}
						<li>
							<a class="todo" class:done={todo.done} href={todo.href}
								><span class="title"
									><span class="name">{t(todo.key)}</span>{#if todo.done !== null}<span
											class="chip"
											class:ok={todo.done}
											>{t(todo.done ? 'welcome.stateDone' : 'welcome.stateOpen')}</span
										>{/if}</span
								><span class="muted">{t(`${todo.key}Text`)}</span></a
							>
						</li>
					{/each}
				</ul>
				<div class="actions">
					<button type="button" class="btn" onclick={() => (step = 2)}>{t('welcome.back')}</button>
					<a class="btn btn-primary" href="/">{t('welcome.finish')}</a>
				</div>
			</div>
		{/if}
	</section>
</div>

<style>
	.frame {
		display: flex;
		justify-content: center;
	}
	section {
		width: 100%;
		max-width: 680px;
		display: flex;
		flex-direction: column;
		gap: 20px;
		padding: 28px;
		font-size: var(--text-md);
	}
	.head {
		display: flex;
		flex-direction: column;
		gap: 16px;
		padding-bottom: 18px;
		border-bottom: 1px solid var(--border);
	}
	h1 {
		font-size: var(--text-xl);
		font-weight: 600;
	}
	h2 {
		font-size: var(--text-lg);
		font-weight: 600;
	}
	p {
		margin: 0;
	}
	.steps {
		display: flex;
		gap: 6px;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.steps li {
		flex: 1;
		min-width: 0;
	}
	.step {
		display: flex;
		align-items: center;
		gap: 8px;
		width: 100%;
		padding: 6px 8px 8px 0;
		background: none;
		border: 0;
		border-bottom: 2px solid var(--border);
		color: var(--muted);
		font: inherit;
		font-size: var(--text-base);
		text-align: left;
		cursor: pointer;
	}
	.step:hover {
		color: var(--text);
	}
	.step.current {
		border-bottom-color: var(--accent);
		color: var(--text);
		font-weight: 600;
	}
	.number {
		flex: none;
		display: inline-grid;
		place-items: center;
		width: 22px;
		height: 22px;
		border: 1px solid currentColor;
		border-radius: 50%;
		font-size: var(--text-sm);
		font-variant-numeric: tabular-nums;
	}
	.step.current .number {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent);
	}
	.label {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	form,
	.body {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	label,
	.group {
		display: flex;
		flex-direction: column;
		align-items: stretch;
		gap: 6px;
		font-size: var(--text-md);
	}
	.group {
		align-items: flex-start;
	}
	.caption {
		font-weight: 500;
	}
	small {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.problem {
		color: var(--error-text);
		font-weight: 500;
	}
	.field[aria-invalid='true'] {
		border-color: var(--status-error);
	}
	.pre {
		white-space: pre-line;
	}
	.panel {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 14px 16px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: 10px;
		margin-top: 4px;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 12px;
	}
	.progress {
		font-weight: 500;
	}
	.todos {
		margin: 0;
		padding: 0;
		list-style: none;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
		gap: 10px;
	}
	.todo {
		display: flex;
		flex-direction: column;
		gap: 4px;
		height: 100%;
		box-sizing: border-box;
		padding: 12px 14px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		text-decoration: none;
	}
	.todo:hover {
		border-color: var(--accent);
	}
	.todo:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.todo.done .name {
		color: var(--muted);
	}
	.title {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 6px 10px;
	}
	.name {
		font-weight: 600;
	}
	@media (max-width: 560px) {
		section {
			padding: 20px 18px;
		}
		/* Only the current step keeps its name; the others show their number. */
		.step:not(.current) .label {
			display: none;
		}
		.steps li:has(.current) {
			flex: 3;
		}
		.todos {
			grid-template-columns: 1fr;
		}
		.actions > :global(*) {
			flex: 1;
			text-align: center;
			justify-content: center;
		}
	}
</style>
