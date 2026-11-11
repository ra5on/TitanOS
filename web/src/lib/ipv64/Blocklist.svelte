<script lang="ts">
	// Blocklist and notifications: the v64 blocklist of IPv64 for the
	// firewall (off by default) and notifications from polling the account.
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Status } from './ipv64.ts';
	import { failure, when } from './ui.ts';

	let { status, onchange }: { status: Status; onchange: (next: Status) => void } = $props();

	let busy = $state(false);
	let error = $state('');
	let confirmOn = $state(false);
	const list = $derived(status.status.settings.blocklist);
	const info = $derived(status.status.blocklistStatus);
	const notify = $derived(status.status.settings.notify);

	async function setList(enabled: boolean) {
		busy = true;
		error = '';
		try {
			onchange(await api.put<Status>('/ipv64/blocklist', { enabled }));
			confirmOn = false;
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}
	async function refresh() {
		busy = true;
		error = '';
		try {
			onchange(await api.post<Status>('/ipv64/blocklist/refresh'));
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}
	async function setNotify(change: Partial<typeof notify>) {
		busy = true;
		error = '';
		try {
			onchange(await api.put<Status>('/ipv64/notify', { ...notify, ...change }));
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}
	// The state of the list is what is looked at here: its row stands open.
	let listOpen = $state(true);
</script>

<div class="ipv64-tab">
	<!-- PLAN 78-1: the switches are rows (each stores on change, as before);
	     the state of the list is the part of its row. -->
	{#snippet listParts()}
		<div class="srow-block">
			<dl>
				<div>
					<dt>{t('ipv64x.entries')}</dt>
					<dd>
						{info.entries}{#if info.truncated}
							({t('ipv64x.truncated')}){/if}
					</dd>
				</div>
				<div>
					<dt>{t('ipv64x.listKind')}</dt>
					<dd>{t(info.live ? 'ipv64x.live' : 'ipv64x.daily')}</dd>
				</div>
				<div>
					<dt>{t('ipv64x.fetched')}</dt>
					<dd>{when(info.fetched)}</dd>
				</div>
				<div>
					<dt>{t('ipv64x.next')}</dt>
					<dd>{when(info.next)}</dd>
				</div>
			</dl>
			{#if info.error}<p class="notice-error" role="alert">{info.error}</p>{/if}
		</div>
		<div class="srow-block actions">
			<button type="button" class="btn" disabled={busy} onclick={refresh}
				>{t('ipv64x.fetchNow')}</button
			>
		</div>
	{/snippet}
	<SettingGroup title={t('ipv64x.blocklist')} id="ipv64-blocklist" desc={t('ipv64x.blocklistText')}>
		{#if !status.firewall}<p class="notice-warn">
				{t('ipv64x.blocklistNoFirewall')}
				<a href="/settings/firewall">{t('ipv64x.toFirewall')}</a>
			</p>{/if}
		<SettingRow
			id="ipv64-blocklist-row"
			icon="shield"
			title={t('ipv64x.blocklistEnable')}
			desc={t('ipv64x.blocklistHint')}
			for="ipv64-blocklist-on"
			bind:open={listOpen}
			children={list.enabled ? listParts : undefined}
		>
			{#snippet control()}
				<input
					id="ipv64-blocklist-on"
					class="switch"
					type="checkbox"
					role="switch"
					checked={list.enabled}
					disabled={busy || status.status.readOnly}
					onchange={(e) => {
						const on = e.currentTarget.checked;
						e.currentTarget.checked = list.enabled;
						if (on) confirmOn = true;
						else setList(false);
					}}
				/>
			{/snippet}
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('ipv64x.notifications')} id="ipv64-notify" desc={t('ipv64x.notifyHint')}>
		<SettingRow icon="bell" title={t('ipv64x.notifyEnable')} for="ipv64-notify-on">
			{#snippet control()}
				<input
					id="ipv64-notify-on"
					class="switch"
					type="checkbox"
					role="switch"
					checked={notify.enabled}
					disabled={busy}
					onchange={(e) => setNotify({ enabled: e.currentTarget.checked })}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow icon="refresh" title={t('ipv64x.notifyFailover')} for="ipv64-notify-failover">
			{#snippet control()}
				<input
					id="ipv64-notify-failover"
					class="switch"
					type="checkbox"
					role="switch"
					checked={notify.failover}
					disabled={busy || !notify.enabled}
					onchange={(e) => setNotify({ failover: e.currentTarget.checked })}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow icon="activity" title={t('ipv64x.notifyChecks')} for="ipv64-notify-checks">
			{#snippet control()}
				<input
					id="ipv64-notify-checks"
					class="switch"
					type="checkbox"
					role="switch"
					checked={notify.healthchecks}
					disabled={busy || !notify.enabled}
					onchange={(e) => setNotify({ healthchecks: e.currentTarget.checked })}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow icon="globe" title={t('ipv64x.notifyTraffic')} for="ipv64-notify-traffic">
			{#snippet control()}
				<input
					id="ipv64-notify-traffic"
					class="switch"
					type="checkbox"
					role="switch"
					checked={notify.cdnTraffic}
					disabled={busy || !notify.enabled}
					onchange={(e) => setNotify({ cdnTraffic: e.currentTarget.checked })}
				/>
			{/snippet}
		</SettingRow>
		{#if notify.enabled}<p class="muted">
				{t('ipv64x.lastPoll', { at: when(status.status.poll.last) })}
			</p>{/if}
		{#if status.status.poll.error}<p class="notice-error" role="alert">
				{status.status.poll.error}
			</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	</SettingGroup>
</div>

{#if confirmOn}
	<Dialog title={t('ipv64x.blocklistEnable')} locked={busy} onclose={() => (confirmOn = false)}>
		<p>{t('ipv64x.blocklistConfirm')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (confirmOn = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" disabled={busy} onclick={() => setList(true)}
				>{t('ipv64x.switchOn')}</button
			>
		</div>
	</Dialog>
{/if}
