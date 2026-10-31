<script lang="ts">
	// The wizard after the first start: name of the NAS and time zone,
	// a look at the network, and what to do next. Every step can be skipped.
	import NetworkBridgeSetup from '#lib/components/NetworkBridgeSetup.svelte';
	import { api, ApiError } from '#lib/api.ts';
	import LanguageSwitch from '#lib/components/LanguageSwitch.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { refreshSession, session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { replaceState } from '$app/navigation';
	import { page } from '$app/state';
	import { untrack } from 'svelte';

	const STEPS = 3;
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
	let checkFailed = $state(false);
	async function check() {
		checkFailed = false;
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
		const value = <T,>(result: PromiseSettledResult<T>, test: (value: T) => boolean) =>
			result.status === 'fulfilled' ? test(result.value) : null;
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
	}
	$effect(() => {
		if (step === STEPS) untrack(check);
	});

	async function saveBasics(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			await api.put('/system/timezone', { timezone });
			if (hostname !== session.data?.hostname) {
				await api.put('/system/hostname', { hostname });
				await refreshSession();
			}
			step = 2;
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			// In development mode nothing is changed; the wizard still shows its steps.
			if (code === 'read_only') step = 2;
			else error = errorText(code);
		}
		busy = false;
	}
</script>

<div class="frame">
	<section class="tile">
		<div class="head">
			<div>
				<h1>{t('welcome.title')}</h1>
				<p class="muted">{t('welcome.step', { n: step, total: STEPS })}</p>
			</div>
			<LanguageSwitch />
		</div>

		{#if step === 1}
			<form onsubmit={saveBasics}>
				<h2>{t('welcome.basics')}</h2>
				<p class="muted">{t('welcome.intro')}</p>
				<label
					><span>{t('welcome.hostname')}</span>
					<input
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
						}}
						maxlength="63"
						pattern="[a-z0-9]([a-z0-9\-]*[a-z0-9])?"
						autocapitalize="none"
						spellcheck="false"
						required
					/>
					<small>{t('welcome.hostnameHint')}</small>
					{#if hostname}<small>{t('welcome.localName', { name: `${hostname}.local` })}</small>{/if}
				</label>
				<label
					><span>{t('welcome.timezone')}</span>
					<select class="field" bind:value={timezone}
						>{#each zones as zone (zone)}<option value={zone}>{zone}</option>{/each}</select
					>
					<small>{t('welcome.timezoneHint')}</small>
				</label>
				{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
				<div class="actions">
					<button type="button" class="btn" onclick={() => (step = 2)}>{t('welcome.skip')}</button>
					<button type="submit" class="btn btn-primary" disabled={busy}>{t('welcome.next')}</button>
				</div>
			</form>
		{:else if step === 2}
			<div class="body">
				<h2>{t('welcome.network')}</h2>
				<p>{t('welcome.networkNow', { address })}</p>
				<p class="muted">{t('welcome.networkHint')}</p>
				<NetworkBridgeSetup auto />
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
				{#if checkFailed}<p class="notice-warn" role="alert">
						{t('welcome.checkFailed')}
						<button type="button" class="btn small" onclick={check}>{t('ui.retry')}</button>
					</p>{/if}
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
		max-width: 640px;
		display: flex;
		flex-direction: column;
		gap: 18px;
		padding: 28px;
		font-size: 0.875rem;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		justify-content: space-between;
		gap: 12px;
	}
	h1 {
		font-size: 1.375rem;
		font-weight: 600;
	}
	h2 {
		font-size: 1.0625rem;
		font-weight: 600;
	}
	form,
	.body {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	small {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 10px;
		margin-top: 6px;
	}
	.todos {
		margin: 0;
		padding: 0;
		list-style: none;
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
		gap: 10px;
	}
	.todo {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 12px 14px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		text-decoration: none;
	}
	.todo {
		height: 100%;
		box-sizing: border-box;
	}
	.todo:hover {
		border-color: var(--accent);
	}
	.todo.done {
		opacity: 0.8;
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
</style>
