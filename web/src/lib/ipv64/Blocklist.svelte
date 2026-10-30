<script lang="ts">
	// Blocklist and notifications: the v64 blocklist of IPv64 for the
	// firewall (off by default) and notifications from polling the account.
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
</script>

<div class="ipv64-tab">
	<section class="tile panel" aria-labelledby="ipv64-blocklist">
		<h2 class="tile-title" id="ipv64-blocklist">{t('ipv64x.blocklist')}</h2>
		<p>{t('ipv64x.blocklistText')}</p>
		{#if !status.firewall}<p class="notice-warn">
				{t('ipv64x.blocklistNoFirewall')} <a href="/settings/firewall">{t('ipv64x.toFirewall')}</a>
			</p>{/if}
		<label class="check"
			><input
				type="checkbox"
				checked={list.enabled}
				disabled={busy || status.status.readOnly}
				onchange={(e) => {
					const on = e.currentTarget.checked;
					e.currentTarget.checked = list.enabled;
					if (on) confirmOn = true;
					else setList(false);
				}}
			/><span>{t('ipv64x.blocklistEnable')}</span></label
		>
		{#if list.enabled}
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
			<div class="row-actions">
				<button type="button" class="btn" disabled={busy} onclick={refresh}
					>{t('ipv64x.fetchNow')}</button
				>
			</div>
		{/if}
		<p class="muted hint">{t('ipv64x.blocklistHint')}</p>
	</section>

	<section class="tile panel" aria-labelledby="ipv64-notify">
		<h2 class="tile-title" id="ipv64-notify">{t('ipv64x.notifications')}</h2>
		<p class="muted hint">{t('ipv64x.notifyHint')}</p>
		<label class="check"
			><input
				type="checkbox"
				checked={notify.enabled}
				disabled={busy}
				onchange={(e) => setNotify({ enabled: e.currentTarget.checked })}
			/><span>{t('ipv64x.notifyEnable')}</span></label
		>
		<label class="check"
			><input
				type="checkbox"
				checked={notify.failover}
				disabled={busy || !notify.enabled}
				onchange={(e) => setNotify({ failover: e.currentTarget.checked })}
			/><span>{t('ipv64x.notifyFailover')}</span></label
		>
		<label class="check"
			><input
				type="checkbox"
				checked={notify.healthchecks}
				disabled={busy || !notify.enabled}
				onchange={(e) => setNotify({ healthchecks: e.currentTarget.checked })}
			/><span>{t('ipv64x.notifyChecks')}</span></label
		>
		<label class="check"
			><input
				type="checkbox"
				checked={notify.cdnTraffic}
				disabled={busy || !notify.enabled}
				onchange={(e) => setNotify({ cdnTraffic: e.currentTarget.checked })}
			/><span>{t('ipv64x.notifyTraffic')}</span></label
		>
		{#if notify.enabled}<p class="muted">
				{t('ipv64x.lastPoll', { at: when(status.status.poll.last) })}
			</p>{/if}
		{#if status.status.poll.error}<p class="notice-error" role="alert">
				{status.status.poll.error}
			</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	</section>
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
