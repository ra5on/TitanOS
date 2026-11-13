<script lang="ts">
	// PLAN 75-19/75-20, draft U2 (owner 9.11.2026): one account on its page –
	// the role, the apps it may use, its shares and its login, each a row
	// that opens downward (style E3). Einstellungen › Programme is gone: who
	// may use which app is set here.
	import { api, type Account, type AccessLevel, type Role, type Share } from '#lib/api.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import Expander from '#lib/components/explorer/Expander.svelte';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';

	import type { UserCameras } from '#lib/nvr/types.ts';

	type UserApp = { app: string; icon: string; allowed: boolean };
	type UserApps = { admin: boolean; newApps: boolean; apps: UserApp[] };

	type Props = {
		account: Account;
		me?: string;
		shares: Share[] | null;
		manageable: boolean;
		mayLock: boolean;
		offeredRoles: Role[];
		working: boolean;
		created: string;
		lockedTitle: string;
		onrole: (role: Role, select: HTMLSelectElement) => void;
		onpassword: () => void;
		onlock: () => void;
		ondelete: () => void;
		onreset2fa: () => void;
		onshares: () => void;
		onback?: () => void;
	};
	let {
		account,
		me,
		shares,
		manageable,
		mayLock,
		offeredRoles,
		working,
		created,
		lockedTitle,
		onrole,
		onpassword,
		onlock,
		ondelete,
		onreset2fa,
		onshares,
		onback,
	}: Props = $props();

	const self = $derived(account.name === me);
	const admin = $derived(account.role === 'owner' || account.role === 'admin');

	// ---- Apps ----
	let apps = $state<UserApps | null>(null);
	let appsError = $state('');
	let appsBusy = $state(false);
	let appsOpen = $state(true);
	$effect(() => {
		const name = account.name;
		apps = null;
		appsError = '';
		api.get<UserApps>(`/users/${encodeURIComponent(name)}/apps`).then(
			(res) => {
				if (name === account.name) apps = res;
			},
			(err) => {
				if (name === account.name) appsError = apiErrorText(err);
			},
		);
	});
	async function saveApps(body: { newApps?: boolean; apps?: Record<string, boolean> }) {
		appsBusy = true;
		appsError = '';
		try {
			apps = await api.put<UserApps>(`/users/${encodeURIComponent(account.name)}/apps`, body);
		} catch (err) {
			appsError = apiErrorText(err);
		} finally {
			appsBusy = false;
		}
	}
	const allowedCount = $derived(apps ? apps.apps.filter((a) => a.allowed).length : 0);
	const appsSummary = $derived(
		!apps
			? ''
			: apps.admin
				? t('users.apps.adminAll')
				: [
						t('users.apps.count', { allowed: allowedCount, total: apps.apps.length }),
						apps.newApps ? t('users.apps.newAuto') : t('users.apps.newManual'),
					].join(' · '),
	);
	function hue(name: string) {
		let h = 0;
		for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) % 360;
		return h;
	}

	// ---- Cameras (PLAN 79-7) ----
	// Which cameras of the NVR the account may watch live and whose
	// recordings it may see. A new account sees none.
	let cams = $state<UserCameras | null>(null);
	let camsError = $state('');
	let camsBusy = $state(false);
	/** This NAS has no NVR (or an older version): the group is left out. */
	let camsMissing = $state(false);
	$effect(() => {
		const name = account.name;
		cams = null;
		camsError = '';
		api.get<UserCameras>(`/users/${encodeURIComponent(name)}/cameras`).then(
			(res) => {
				if (name !== account.name) return;
				// An answer without the list is no NVR's answer: leave the group out.
				camsMissing = !Array.isArray(res?.cameras);
				cams = camsMissing ? null : res;
			},
			() => {
				if (name === account.name) camsMissing = true;
			},
		);
	});
	async function saveCamera(id: string, right: 'live' | 'recordings', on: boolean) {
		const row = cams?.cameras.find((c) => c.id === id);
		if (!row) return;
		camsBusy = true;
		camsError = '';
		try {
			const next = await api.put<UserCameras>(
				`/users/${encodeURIComponent(account.name)}/cameras`,
				{ cameras: { [id]: { live: row.live, recordings: row.recordings, [right]: on } } },
			);
			if (Array.isArray(next?.cameras)) cams = next;
		} catch (err) {
			camsError = apiErrorText(err);
		} finally {
			camsBusy = false;
		}
	}
	// A removed camera shows nowhere for other accounts: no switches for it.
	const camRows = $derived((cams?.cameras ?? []).filter((c) => !c.deleted));
	const camsSummary = $derived(
		!cams
			? ''
			: cams.admin
				? t('nvr.rights.adminAll')
				: camRows.length === 0
					? t('nvr.rights.noCameras')
					: t('nvr.rights.count', {
							live: camRows.filter((c) => c.live).length,
							recordings: camRows.filter((c) => c.recordings).length,
							total: camRows.length,
						}),
	);

	// ---- Shares ----
	const reach = $derived(
		(shares ?? [])
			.map((share) => ({ share, level: share.access[account.name] as AccessLevel | undefined }))
			.sort((a, b) => a.share.name.localeCompare(b.share.name, i18n.locale)),
	);
	const sharesSummary = $derived.by(() => {
		if (shares === null) return t('users.sharesUnknown');
		const rw = reach.filter((r) => r.level === 'rw').map((r) => r.share.name);
		const ro = reach.filter((r) => r.level === 'ro').map((r) => r.share.name);
		const parts = [];
		if (rw.length) parts.push(t('users.sharesSummaryRw', { names: rw.join(', ') }));
		if (ro.length) parts.push(t('users.sharesSummaryRo', { names: ro.join(', ') }));
		return parts.join(' · ') || t('users.sharesNone');
	});
	let shareBusy = $state('');
	let shareError = $state('');
	async function setShare(share: Share, level: '' | AccessLevel) {
		shareBusy = share.name;
		shareError = '';
		const body: Record<string, unknown> = { ...$state.snapshot(share) };
		delete body.quotaStatus;
		const access = { ...share.access };
		if (level) access[account.name] = level;
		else delete access[account.name];
		body.access = access;
		try {
			await api.put(`/shares/${encodeURIComponent(share.name)}`, body);
			onshares();
		} catch (err) {
			shareError = apiErrorText(err);
		} finally {
			shareBusy = '';
		}
	}
</script>

<section class="account" aria-label={account.name}>
	{#if onback}<button type="button" class="btn back" onclick={onback}>‹ {t('users.list')}</button
		>{/if}
	<header class="head">
		<span class="avatar big" style:--hue={hue(account.name)} aria-hidden="true"
			>{account.name.charAt(0).toUpperCase()}</span
		>
		<div class="who">
			<h2>
				<span class="mono">{account.name}</span>{#if self}<span class="chip you"
						>{t('users.you')}</span
					>{/if}
			</h2>
			<p class="muted">
				{t(`role.${account.role}`)} · {account.disabled
					? lockedTitle || t('users.statusLocked')
					: t('users.createdOn', { date: created })}
			</p>
			{#if !account.smbReady}<p class="muted small">{t('users.smbPending')}</p>{/if}
		</div>
		<div class="acts">
			{#if manageable || self}<button type="button" class="btn" onclick={onpassword}
					>{t('users.changePassword')}</button
				>{/if}
			{#if mayLock}<button type="button" class="btn" onclick={onlock}
					>{account.disabled ? t('users.unlock') : t('users.lock')}</button
				>{/if}
			{#if manageable && !self}<button
					type="button"
					class="btn btn-danger-outline"
					onclick={ondelete}>{t('users.delete')}</button
				>{/if}
		</div>
	</header>

	<Expander icon="users" title={t('users.role')} summary={t(`users.roleHint.${account.role}`)}>
		{#snippet control()}
			{#if manageable && !self}
				<select
					class="field role"
					aria-label="{t('users.role')}: {account.name}"
					value={account.role}
					disabled={working}
					onchange={(event) => onrole(event.currentTarget.value as Role, event.currentTarget)}
				>
					{#each offeredRoles as role (role)}<option value={role}>{t(`role.${role}`)}</option
						>{/each}
				</select>
			{:else}<span class="muted">{t(`role.${account.role}`)}</span>{/if}
		{/snippet}
	</Expander>

	<Expander icon="grid" title={t('users.apps.title')} summary={appsSummary} bind:open={appsOpen}>
		{#if appsError}<p class="notice-error part" role="alert">{appsError}</p>{/if}
		{#if !apps}
			<p class="muted part" role="status">{t('common.loading')}</p>
		{:else if apps.admin}
			<p class="muted part">{t('users.apps.adminHint')}</p>
		{:else if apps.apps.length === 0}
			<p class="muted part">{t('users.apps.none')}</p>
		{:else}
			<div class="apps">
				{#each apps.apps as a (a.app)}
					<label class="app">
						<input
							type="checkbox"
							checked={a.allowed}
							disabled={appsBusy}
							onchange={(event) => saveApps({ apps: { [a.app]: event.currentTarget.checked } })}
						/>
						<span
							class="app-icon"
							style:--hue={hue(a.app)}
							class:letter={!a.icon}
							aria-hidden="true"><AppIcon src={a.icon} name={a.app} /></span
						>
						<span class="mono">{a.app}</span>
					</label>
				{/each}
			</div>
			<div class="apps-foot">
				<label class="new">
					<input
						type="checkbox"
						checked={apps.newApps}
						disabled={appsBusy}
						onchange={(event) => saveApps({ newApps: event.currentTarget.checked })}
					/>
					{t('users.apps.newLabel')}
				</label>
				<span class="all">
					<button
						type="button"
						class="btn small"
						disabled={appsBusy}
						onclick={() =>
							saveApps({ apps: Object.fromEntries(apps!.apps.map((a) => [a.app, true])) })}
						>{t('users.apps.all')}</button
					>
					<button
						type="button"
						class="btn small"
						disabled={appsBusy}
						onclick={() =>
							saveApps({ apps: Object.fromEntries(apps!.apps.map((a) => [a.app, false])) })}
						>{t('users.apps.noneButton')}</button
					>
				</span>
			</div>
		{/if}
	</Expander>

	<Expander icon="folder" title={t('users.shares')} summary={sharesSummary}>
		{#if shareError}<p class="notice-error part" role="alert">{shareError}</p>{/if}
		{#if shares === null}
			<p class="muted part">{t('users.sharesUnknown')}</p>
		{:else if shares.length === 0}
			<p class="muted part">{t('users.sharesNone')}</p>
		{:else}
			{#each reach as r (r.share.name)}
				<div class="part">
					<span class="t"
						><span class="mono">{r.share.name}</span><small>{r.share.volume}</small></span
					>
					{#if manageable || admin}
						<select
							class="field"
							aria-label={t('users.shareLevelFor', { share: r.share.name })}
							value={r.level ?? ''}
							disabled={shareBusy === r.share.name}
							onchange={(event) => setShare(r.share, event.currentTarget.value as '' | AccessLevel)}
						>
							<option value="">{t('users.shareNone')}</option>
							<option value="ro">{t('share.ro')}</option>
							<option value="rw">{t('share.rw')}</option>
						</select>
					{:else}
						<span class="muted">{r.level ? t(`share.${r.level}`) : t('users.shareNone')}</span>
					{/if}
				</div>
			{/each}
		{/if}
	</Expander>

	{#if !camsMissing}
		<Expander
			icon="camera"
			title={t('nvr.rights.title')}
			summary={camsSummary}
			id="account-cameras"
		>
			{#if camsError}<p class="notice-error part" role="alert">{camsError}</p>{/if}
			{#if !cams}
				<p class="muted part" role="status">{t('common.loading')}</p>
			{:else if cams.admin}
				<p class="muted part">{t('nvr.rights.adminHint')}</p>
			{:else if camRows.length === 0}
				<p class="muted part">{t('nvr.rights.noCameras')}</p>
			{:else}
				<p class="muted part">{t('nvr.rights.hint')}</p>
				{#each camRows as c (c.id)}
					<div class="part cam" data-camera={c.id}>
						<span class="t">{c.name}</span>
						<span class="cam-rights">
							<label
								><input
									type="checkbox"
									role="switch"
									class="switch"
									checked={c.live}
									disabled={camsBusy || !(manageable || admin)}
									aria-label={t('nvr.rights.liveFor', { camera: c.name })}
									onchange={(event) => saveCamera(c.id, 'live', event.currentTarget.checked)}
								/><span>{t('nvr.rights.live')}</span></label
							>
							<label
								><input
									type="checkbox"
									role="switch"
									class="switch"
									checked={c.recordings}
									disabled={camsBusy || !(manageable || admin)}
									aria-label={t('nvr.rights.recordingsFor', { camera: c.name })}
									onchange={(event) => saveCamera(c.id, 'recordings', event.currentTarget.checked)}
								/><span>{t('nvr.rights.recordings')}</span></label
							>
						</span>
					</div>
				{/each}
			{/if}
		</Expander>
	{/if}
	<Expander
		icon="key"
		title={t('users.login')}
		summary={account.twoFactor ? t('users.twoFactorOn') : t('users.twoFactorOff')}
	>
		<div class="part">
			<span class="t"
				>{t('users.twoFactor')}<small
					>{account.twoFactor ? t('users.twoFactorOn') : t('users.twoFactorOff')}</small
				></span
			>
			{#if account.twoFactor && manageable && !self}<button
					type="button"
					class="btn small"
					onclick={onreset2fa}>{t('users.resetTwoFactor')}</button
				>{:else if self && !account.twoFactor}<a class="btn small" href="/account"
					>{t('users.twoFactorSetUp')}</a
				>{/if}
		</div>
		<div class="part">
			<span class="t">{t('users.password')}<small>{t('users.passwordNoteShort')}</small></span>
			{#if manageable || self}<button type="button" class="btn small" onclick={onpassword}
					>{t('users.changePassword')}</button
				>{/if}
		</div>
	</Expander>
</section>

<style>
	.account {
		min-width: 0;
	}
	.back {
		margin-bottom: var(--space-3);
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-3) var(--space-4);
		margin-bottom: var(--space-4);
	}
	/* The name keeps a readable width; the buttons go below it on a phone. */
	.who {
		flex: 1 1 12rem;
		min-width: 0;
	}
	.who h2 {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		margin: 0;
		font-size: var(--text-xl);
	}
	.who p {
		margin: 2px 0 0;
	}
	.small {
		font-size: var(--text-sm);
	}
	/* PLAN 79-7: the camera's name keeps its width; the two switches wrap
	   below it on a phone instead of squeezing the name letter by letter. */
	.cam {
		flex-wrap: wrap;
	}
	.cam .t {
		flex: 1 1 9rem;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.cam-rights {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2) var(--space-4);
	}
	.cam-rights label {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		white-space: nowrap;
	}
	.acts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
	}
	.avatar {
		display: grid;
		place-items: center;
		flex: none;
		border-radius: 50%;
		background: hsl(var(--hue) 55% 42%);
		color: #fff;
		font-weight: 700;
	}
	.avatar.big {
		width: 52px;
		height: 52px;
		font-size: var(--text-xl);
	}
	.apps {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
		gap: 2px var(--space-4);
		padding: var(--space-2-5) var(--space-4) var(--space-2)
			calc(var(--space-4) + 30px + var(--space-3));
	}
	.app,
	.new {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 34px;
		cursor: pointer;
	}
	.app-icon {
		display: grid;
		place-items: center;
		flex: none;
		width: 24px;
		height: 24px;
		overflow: hidden;
		border-radius: 6px;
		background: var(--chip);
		font-size: var(--text-xs);
		font-weight: 700;
		text-transform: uppercase;
	}
	.app-icon.letter {
		background: hsl(var(--hue) 50% 36%);
		color: #fff;
	}
	.apps-foot {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-4);
		margin: 0 var(--space-4) 0 calc(var(--space-4) + 30px + var(--space-3));
		padding: var(--space-2) 0 var(--space-2-5);
		border-top: 1px solid var(--border);
	}
	.all {
		display: flex;
		gap: var(--space-1-5);
		margin-left: auto;
	}
	p.part {
		margin: 0;
	}
	@media (max-width: 560px) {
		.apps,
		.apps-foot {
			padding-left: var(--space-4);
			margin-left: 0;
		}
		.apps-foot {
			margin: 0 var(--space-4);
		}
	}
</style>
