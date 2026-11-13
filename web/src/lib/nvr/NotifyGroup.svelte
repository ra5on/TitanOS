<script lang="ts">
	// PLAN 82-3: whom a camera tells and how, as rows of the camera's form –
	// what is reported, the accounts that get it with their own ways (bell,
	// push, e-mail), Home Assistant and the KI-Agent as switches of the
	// camera, the pause and the window. The NAS checks the accounts again
	// for every message; the form offers only accounts that may see the
	// camera and says plainly why a way would not arrive.
	import { onMount, untrack } from 'svelte';
	import { api } from '../api.ts';
	import SettingGroup from '../components/settings/SettingGroup.svelte';
	import SettingRow from '../components/settings/SettingRow.svelte';
	import SettingSub from '../components/settings/SettingSub.svelte';
	import { apiErrorText, t, type MessageKey } from '../i18n/index.svelte.ts';
	import { session } from '../session.svelte.ts';
	import {
		DAYS,
		DEFAULT_PAUSE,
		eligible,
		KIND_PILLS,
		kindMembers,
		kindPillsOf,
		legacyRecipients,
		missesNames,
		PAUSE_CHOICES,
		staleReason,
		toggleWay,
		waysFor,
		waysOf,
		windowOn,
		type KindPill,
		type RecipientAccount,
		type RecipientsAnswer,
		type Way,
	} from './notify.ts';
	import Pills from './Pills.svelte';
	import type { CameraNotify, NotifyRecipient } from './types.ts';

	let {
		notify = $bindable(),
		camera,
		objects,
		id,
	}: {
		/** The camera's choices; the form sends them as they are. */
		notify: CameraNotify;
		/** The camera's ID; none while it is added. */
		camera?: string;
		/** The objects the NAS allows. */
		objects: string[];
		/** The id of the group (the form's `{uid}-notify`). */
		id: string;
	} = $props();

	const me = $derived(session.data?.user?.name ?? '');
	/** A camera of 0.78: its old ways become a list once the accounts are known. */
	const legacy = untrack(() => notify.recipients == null && !!camera);
	const legacyEmail = untrack(() => legacy && notify.email);

	let accounts = $state<RecipientAccount[] | null>(null);
	let mailServer = $state(true);
	let loadError = $state('');
	let mainOff = $state(false);
	onMount(() => {
		// A new camera starts with the administrator who adds it (bell and push).
		if (!camera && notify.recipients == null && me)
			notify.recipients = [{ user: me, bell: true, push: true, email: false }];
		api
			.get<RecipientsAnswer>('/nvr/recipients')
			.then((answer) => {
				accounts = answer.accounts;
				mailServer = answer.mailServer;
				if (notify.recipients == null)
					notify.recipients = legacyRecipients(notify, answer.accounts);
			})
			.catch((err) => (loadError = apiErrorText(err)));
		api
			.get<{ notifyOff?: boolean }>('/nvr/settings')
			.then((s) => (mainOff = !!s.notifyOff))
			.catch(() => {
				// The note is a help only; the camera's own choices stay.
			});
	});

	const list = $derived(notify.recipients ?? []);
	const choosable = $derived((accounts ?? []).filter((a) => eligible(a, camera)));
	/** Chosen accounts that get nothing now (locked, deleted, no right on the camera). */
	const stale = $derived(
		accounts
			? list
					.map((entry) => ({ entry, why: staleReason(entry.user, accounts!, camera) }))
					.filter((s) => s.why !== '')
			: [],
	);
	const summary = $derived(
		list.length === 0
			? t('nvr.notify.nobody')
			: list.length === 1
				? t('nvr.notify.recipientsOne')
				: t('nvr.notify.recipientsCount', { count: list.length }),
	);

	function setWay(user: string, way: Way, on: boolean) {
		notify.recipients = toggleWay(list, user, way, on);
	}
	function remove(entry: NotifyRecipient) {
		notify.recipients = list.filter((r) => r.user !== entry.user);
	}
	function hints(account: RecipientAccount): string[] {
		const out: string[] = [];
		if (account.pushDevices === 0)
			out.push(t(account.pushDevicesOff > 0 ? 'nvr.notify.devicesOff' : 'nvr.notify.noDevice'));
		if (!mailServer) out.push(t('nvr.notify.noMailServer'));
		else if (!account.email) out.push(t('nvr.notify.noAddress'));
		if (missesNames(account, camera, notify.kinds)) out.push(t('nvr.notify.noNames'));
		return out;
	}

	const kindsChosen = $derived(kindPillsOf(notify.kinds));
	function toggleKind(pill: KindPill, on: boolean) {
		const members = kindMembers(pill);
		const rest = notify.kinds.filter((k) => !members.includes(k));
		const add = members.filter(
			(k) => ['plate', 'face', 'offline'].includes(k) || !objects.length || objects.includes(k),
		);
		notify.kinds = on ? [...rest, ...add] : rest;
	}

	const win = $derived(notify.window ?? { on: false, days: [], from: '', to: '' });
	function toggleDay(day: number, on: boolean) {
		const days = on ? [...win.days, day] : win.days.filter((d) => d !== day);
		notify.window = { ...win, days: DAYS.filter((d) => days.includes(d)) };
	}
</script>

<SettingGroup title={t('nvr.camera.notifications')} {id}>
	{#if mainOff}<p class="notice-warn note" role="status">{t('nvr.notify.mainOff')}</p>{/if}
	<SettingRow icon="info" title={t('nvr.notify.kinds')} desc={t('nvr.notify.kindsHint')}>
		{#snippet control()}
			<Pills
				label={t('nvr.notify.kinds')}
				options={KIND_PILLS.map((pill) => ({
					value: pill,
					label: t(`nvr.notify.kind.${pill}` as MessageKey),
				}))}
				chosen={kindsChosen}
				ontoggle={toggleKind}
			/>
		{/snippet}
	</SettingRow>
	<SettingRow
		icon="users"
		title={t('nvr.notify.recipients')}
		desc={camera ? t('nvr.notify.recipientsHint') : t('nvr.notify.recipientsNewHint')}
		id="nvr-notify-recipients"
		{summary}
	>
		{#if legacy}
			<div class="srow-block">
				<p class="note">{t('nvr.notify.legacy')}</p>
				{#if legacyEmail}<p class="note">{t('nvr.notify.legacyEmail')}</p>{/if}
			</div>
		{/if}
		{#if loadError}
			<div class="srow-block"><p class="notice-error" role="alert">{loadError}</p></div>
		{:else if !accounts}
			<div class="srow-block"><p class="muted" role="status">{t('nvr.notify.loading')}</p></div>
		{/if}
		{#each choosable as account (account.name)}
			{@const entry = list.find((r) => r.user === account.name)}
			<SettingSub
				title={account.name === me ? t('nvr.notify.you', { name: account.name }) : account.name}
			>
				{#snippet description()}
					<p>{account.admin ? t('nvr.notify.admin') : t('nvr.notify.userNoBell')}</p>
					{#each hints(account) as hint (hint)}<p class="way-hint">{hint}</p>{/each}
				{/snippet}
				{#snippet control()}
					<Pills
						label={t('nvr.notify.waysFor', { name: account.name })}
						options={waysFor(account).map((way) => ({
							value: way,
							label: t(`nvr.notify.way.${way}` as MessageKey),
						}))}
						chosen={waysOf(entry)}
						ontoggle={(way, on) => setWay(account.name, way, on)}
					/>
				{/snippet}
			</SettingSub>
		{/each}
		{#each stale as { entry, why } (entry.user)}
			<SettingSub title={entry.user}>
				{#snippet description()}
					<p class="way-hint">{t(`nvr.notify.stale.${why}` as MessageKey)}</p>
				{/snippet}
				{#snippet control()}
					<button type="button" class="btn" onclick={() => remove(entry)}
						>{t('nvr.notify.remove')}</button
					>
				{/snippet}
			</SettingSub>
		{/each}
	</SettingRow>
	<SettingRow
		icon="home"
		title={t('nvr.notify.homeAssistant')}
		desc={t('nvr.notify.homeAssistantHint')}
	>
		{#snippet control()}
			<input
				type="checkbox"
				role="switch"
				class="switch"
				aria-label={t('nvr.notify.homeAssistant')}
				bind:checked={notify.homeAssistant}
			/>
		{/snippet}
	</SettingRow>
	<SettingRow icon="sparkle" title={t('nvr.notify.agent')} desc={t('nvr.notify.agentHint')}>
		{#snippet control()}
			<input
				type="checkbox"
				role="switch"
				class="switch"
				aria-label={t('nvr.notify.agent')}
				bind:checked={notify.agent}
			/>
		{/snippet}
	</SettingRow>
	<SettingRow
		icon="pause"
		title={t('nvr.notify.pauseTitle')}
		desc={t('nvr.notify.pauseHint')}
		for="{id}-pause"
	>
		{#snippet control()}
			<select
				id="{id}-pause"
				class="field"
				value={notify.pauseSeconds ?? DEFAULT_PAUSE}
				onchange={(e) => (notify.pauseSeconds = Number(e.currentTarget.value))}
			>
				{#each PAUSE_CHOICES as seconds (seconds)}
					<option value={seconds}>{t(`nvr.notify.pause.${seconds}` as MessageKey)}</option>
				{/each}
			</select>
		{/snippet}
	</SettingRow>
	<SettingRow icon="clock" title={t('nvr.notify.window')} desc={t('nvr.notify.windowHint')}>
		{#snippet control()}
			<input
				type="checkbox"
				role="switch"
				class="switch"
				aria-label={t('nvr.notify.window')}
				checked={win.on}
				onchange={(e) => (notify.window = windowOn(notify.window, e.currentTarget.checked))}
			/>
		{/snippet}
	</SettingRow>
	{#if win.on}
		<SettingRow icon="grid" title={t('nvr.notify.days')} desc={t('nvr.notify.daysHint')}>
			{#snippet control()}
				<Pills
					label={t('nvr.notify.days')}
					options={DAYS.map((day) => ({
						value: String(day),
						label: t(`nvr.notify.day.${day}` as MessageKey),
						// At least one day: the NAS takes no window without one.
						disabled: win.days.length === 1 && win.days[0] === day,
					}))}
					chosen={win.days.map(String)}
					ontoggle={(day, on) => toggleDay(Number(day), on)}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow icon="clock" title={t('nvr.notify.time')} desc={t('nvr.notify.timeHint')}>
			{#snippet control()}
				<label class="unit"
					><span>{t('nvr.notify.from')}</span><input
						class="field time"
						type="time"
						required
						value={win.from}
						onchange={(e) =>
							e.currentTarget.value && (notify.window = { ...win, from: e.currentTarget.value })}
					/></label
				>
				<label class="unit"
					><span>{t('nvr.notify.to')}</span><input
						class="field time"
						type="time"
						required
						value={win.to}
						onchange={(e) =>
							e.currentTarget.value && (notify.window = { ...win, to: e.currentTarget.value })}
					/></label
				>
			{/snippet}
		</SettingRow>
	{/if}
</SettingGroup>

<style>
	.note {
		margin: 0;
	}
	.way-hint {
		color: var(--muted);
	}
	.unit {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.unit span {
		min-width: 2em;
	}
	.time {
		width: 7.5rem;
	}
</style>
