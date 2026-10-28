<script lang="ts">
	// Settings › Firewall: master switch, zone matrix, Docker apps, own rules
	// (all on trial when switched on), automatic blocking with its list and
	// the packets dropped lately.
	import {
		api,
		ApiError,
		type FirewallAutoBlock,
		type FirewallConfig,
		type FirewallRule,
		type FirewallStatus,
		type FirewallZone,
		type FirewallZones,
	} from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import {
		appPorts,
		appZones,
		cloneConfig,
		countdown,
		ruleProblem,
		sameRules,
		servicePorts,
		validSource,
		zones,
	} from '#lib/firewall.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import { parseNetworks } from '#lib/ssh-networks.ts';

	let status = $state<FirewallStatus | null>(null);
	let loadError = $state('');
	let draft = $state<FirewallConfig | null>(null);
	let busy = $state(false);
	let error = $state('');
	let notice = $state('');
	let left = $state(0);

	const base = $derived(status ? (status.pending ?? status.config) : null);
	const dirty = $derived(!!draft && !!base && !sameRules(draft, base));
	const problems = $derived(draft ? draft.rules.map(ruleProblem) : []);
	const valid = $derived(problems.every((p) => p === ''));

	async function load() {
		try {
			const next = await api.get<FirewallStatus>('/firewall');
			take(next);
			loadError = '';
		} catch (err) {
			loadError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}

	// Takes over the answer; a draft being edited stays as it is.
	function take(next: FirewallStatus) {
		const editing = dirty;
		status = next;
		left = next.pendingSeconds;
		if (!editing || !draft) draft = cloneConfig(next.pending ?? next.config);
		if (!autoEdited) autoDraft = { ...next.config.autoBlock, exempt: [...next.config.autoBlock.exempt] };
		if (!autoEdited) exemptText = next.config.autoBlock.exempt.join('\n');
	}
	$effect(() => {
		void load();
	});

	// While a change is on trial: the countdown every second, the state
	// every five (pausing while the page is hidden).
	$effect(() => {
		if (!status?.pending) return;
		const tick = setInterval(() => (left = Math.max(0, left - 1)), 1000);
		const poll = setInterval(() => {
			if (!document.hidden) void load();
		}, 5000);
		return () => {
			clearInterval(tick);
			clearInterval(poll);
		};
	});

	async function apply() {
		if (!draft || busy) return;
		busy = true;
		error = notice = '';
		try {
			const next = await api.put<FirewallStatus>('/firewall', $state.snapshot(draft) as Record<string, unknown>);
			draft = null;
			take(next);
			notice = next.pending ? '' : t('ui.saved');
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}

	async function trial(action: 'confirm' | 'revert') {
		if (busy) return;
		busy = true;
		error = notice = '';
		try {
			const next = await api.post<FirewallStatus>(`/firewall/${action}`);
			draft = null;
			take(next);
			notice = t(action === 'confirm' ? 'firewall.confirmed' : 'firewall.reverted');
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
			void load();
		} finally {
			busy = false;
		}
	}

	function discard() {
		if (base) draft = cloneConfig(base);
		error = '';
	}

	function setService(id: string, zone: FirewallZone, on: boolean) {
		if (!draft) return;
		draft.services[id] = { ...draft.services[id], [zone]: on };
	}
	function setApp(key: string, zone: FirewallZone, on: boolean) {
		if (!draft) return;
		draft.apps[key] = { ...appZones(draft, key), [zone]: on };
	}
	function resetApp(key: string) {
		if (!draft) return;
		delete draft.apps[key];
	}
	function addRule() {
		draft?.rules.push({ action: 'allow', protocol: 'tcp', ports: '', source: '', zone: '', comment: '' });
	}
	function moveRule(i: number, by: number) {
		if (!draft) return;
		const j = i + by;
		if (j < 0 || j >= draft.rules.length) return;
		const rules = draft.rules;
		[rules[i], rules[j]] = [rules[j], rules[i]];
	}
	function removeRule(i: number) {
		draft?.rules.splice(i, 1);
	}

	// Apps of before keep their setting even while not running.
	const appRows = $derived.by(() => {
		if (!status || !draft) return [];
		const rows = status.apps.map((app) => ({ ...app, running: true }));
		for (const key of Object.keys(draft.apps))
			if (!rows.some((row) => row.key === key))
				rows.push({ key, name: key.replace(/^container:/, ''), ports: [], running: false });
		return rows.toSorted((a, b) => a.name.localeCompare(b.name));
	});

	// The automatic blocking is saved on its own and applies at once.
	let autoDraft = $state<FirewallAutoBlock | null>(null);
	let autoEdited = $state(false);
	let exemptText = $state('');
	let autoBusy = $state(false);
	let autoError = $state('');
	let autoNotice = $state('');
	const exemptList = $derived(parseNetworks(exemptText));
	const exemptValid = $derived(exemptList.every(validSource));
	async function saveAutoBlock() {
		if (!autoDraft || autoBusy) return;
		autoBusy = true;
		autoError = autoNotice = '';
		try {
			const body = { ...$state.snapshot(autoDraft), exempt: exemptList };
			const next = await api.put<FirewallStatus>('/firewall/autoblock', body);
			autoEdited = false;
			take(next);
			autoNotice = t('ui.saved');
		} catch (err) {
			autoError = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			autoBusy = false;
		}
	}

	let releasing = $state<string | null>(null);
	let releaseBusy = $state(false);
	let releaseError = $state('');
	async function release() {
		if (!releasing || releaseBusy) return;
		releaseBusy = true;
		releaseError = '';
		try {
			take(await api.post<FirewallStatus>('/firewall/unblock', { address: releasing }));
			releasing = null;
		} catch (err) {
			releaseError = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			releaseBusy = false;
		}
	}

	let recentBusy = $state(false);
	async function clearRecent() {
		recentBusy = true;
		try {
			take(await api.delete<FirewallStatus>('/firewall/recent'));
		} catch (err) {
			loadError = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			recentBusy = false;
		}
	}

	const stateChip = $derived.by((): { text: MessageKey; tone: string } => {
		if (!status) return { text: 'common.loading', tone: '' };
		if (status.error) return { text: 'firewall.stateError', tone: 'error' };
		if (status.pending) return { text: 'firewall.stateTrial', tone: 'warn' };
		if (status.config.enabled) return { text: 'firewall.stateOn', tone: 'ok' };
		if (status.active) return { text: 'firewall.stateBlocksOnly', tone: 'info' };
		return { text: 'firewall.stateOff', tone: '' };
	});
	const zoneLabel = (zone: string) => t(`firewall.zone.${zone}` as MessageKey);
	const sourceLabel = (source: string) => t(`firewall.source.${source}` as MessageKey);
</script>

{#snippet zoneBoxes(value: FirewallZones, label: string, onchange: (zone: FirewallZone, on: boolean) => void, off = false)}
	{#each zones as zone (zone)}
		<td class="zone"
			><input
				type="checkbox"
				checked={value[zone]}
				disabled={off || busy || !!status?.pending}
				aria-label={`${label}: ${zoneLabel(zone)}`}
				onchange={(event) => onchange(zone, event.currentTarget.checked)}
			/></td
		>
	{/each}
{/snippet}

<PageHead title={t('firewall.title')} parent={{ href: '/settings', label: t('nav.settings') }} />

{#if loadError}<div class="notice-error" role="alert">
		{loadError}<button class="btn" onclick={load}>{t('ui.retry')}</button>
	</div>{/if}

{#if status && draft}
	{#if status.pending}<section class="notice-warn trial" role="status">
			<strong>{t('firewall.trial', { time: countdown(left) })}</strong>
			<p>{t('firewall.trialHint')}</p>
			<div class="row-actions">
				<button class="btn" disabled={busy} onclick={() => trial('revert')}>{t('firewall.revert')}</button
				><button class="btn btn-primary" disabled={busy} onclick={() => trial('confirm')}
					>{t('firewall.confirm')}</button
				>
			</div>
		</section>{/if}

	<section class="tile panel" aria-labelledby="firewall-main">
		<h2 class="tile-title" id="firewall-main" use:revealTarget>{t('firewall.title')}</h2>
		<div><span class="chip {stateChip.tone}">{t(stateChip.text)}</span></div>
		<label class="check"
			><input type="checkbox" bind:checked={draft.enabled} disabled={busy || !!status.pending} /><span
				>{t('firewall.enable')}</span
			></label
		>
		<p class="muted">{t('firewall.intro')}</p>
		<p class="muted">{t('firewall.notFiltered')}</p>
		{#if status.clientAddress}<p class="muted">
				{t('firewall.you', {
					address: status.clientAddress,
					zone: status.clientZone ? zoneLabel(status.clientZone) : '–',
				})}
			</p>{/if}
		{#if status.error}<p class="notice-error" role="alert">
				{t('firewall.loadError', { detail: status.error })}
			</p>{/if}
	</section>

	<section class="tile panel" aria-labelledby="firewall-zones">
		<h2 class="tile-title" id="firewall-zones" use:revealTarget>{t('firewall.zones')}</h2>
		<dl class="zones-list">
			<div>
				<dt>{zoneLabel('home')}</dt>
				<dd class="mono">{(status.homeNetworks ?? []).join(', ') || '–'}</dd>
			</div>
			<div>
				<dt>{zoneLabel('vpn')}</dt>
				<dd><span class="mono">{(status.vpnNetworks ?? []).join(', ')}</span> · tailscale0, wt0</dd>
			</div>
			<div><dt>{zoneLabel('internet')}</dt><dd>{t('firewall.internetText')}</dd></div>
		</dl>
		<p class="muted small">{t('firewall.zonesHint')}</p>
		<div class="table">
			<table>
				<thead
					><tr
						><th>{t('firewall.service')}</th><th>{t('firewall.ports')}</th>{#each zones as zone (zone)}<th
								class="zone">{zoneLabel(zone)}</th
							>{/each}</tr
					></thead
				>
				<tbody>
					{#each status.services as service (service.id)}
						<tr>
							<td
								><strong>{t(`firewall.svc.${service.id}` as MessageKey)}</strong><br /><span
									class="muted small">{t(`firewall.svcText.${service.id}` as MessageKey)}</span
								></td
							>
							<td class="mono small">{servicePorts(service)}</td>
							{@render zoneBoxes(
								draft.services[service.id] ?? service.default,
								t(`firewall.svc.${service.id}` as MessageKey),
								(zone, on) => setService(service.id, zone, on),
							)}
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<p class="muted small">{t('firewall.always')}</p>
	</section>

	<section class="tile panel" aria-labelledby="firewall-apps">
		<h2 class="tile-title" id="firewall-apps" use:revealTarget>{t('firewall.apps')}</h2>
		<p class="muted">{t('firewall.appsText')}</p>
		<div class="table">
			<table>
				<thead
					><tr
						><th>{t('firewall.app')}</th><th>{t('firewall.ports')}</th>{#each zones as zone (zone)}<th
								class="zone">{zoneLabel(zone)}</th
							>{/each}<th></th></tr
					></thead
				>
				<tbody>
					{#each appRows as app (app.key)}
						<tr>
							<td
								><strong>{app.name}</strong>{#if !app.running}<br /><span class="muted small"
										>{t('firewall.appGone')}</span
									>{/if}{#if app.hostNetwork}<br /><span class="chip warn">{t('firewall.hostNetwork')}</span
									>{/if}</td
							>
							<td class="mono small">{appPorts(app) || '–'}</td>
							{@render zoneBoxes(appZones(draft, app.key), app.name, (zone, on) => setApp(app.key, zone, on))}
							<td
								>{#if draft.apps[app.key]}<button
										class="btn small"
										type="button"
										disabled={busy || !!status.pending}
										onclick={() => resetApp(app.key)}>{t('firewall.appDefault')}</button
									>{/if}</td
							>
						</tr>
					{/each}
					<tr class="new-apps">
						<td><strong>{t('firewall.newApps')}</strong><br /><span class="muted small">{t('firewall.newAppsText')}</span></td>
						<td></td>
						{@render zoneBoxes(draft.newApps, t('firewall.newApps'), (zone, on) => {
							if (draft) draft.newApps = { ...draft.newApps, [zone]: on };
						})}
						<td></td>
					</tr>
				</tbody>
			</table>
		</div>
		{#if appRows.some((app) => app.hostNetwork)}<p class="notice-warn">{t('firewall.hostNetworkHint')}</p>{/if}
	</section>

	<section class="tile panel" aria-labelledby="firewall-rules">
		<h2 class="tile-title" id="firewall-rules" use:revealTarget>{t('firewall.rules')}</h2>
		<p class="muted">{t('firewall.rulesText')}</p>
		{#if draft.rules.length}<div class="table">
				<table class="rules">
					<thead
						><tr
							><th>{t('firewall.ruleOn')}</th><th>{t('firewall.ruleAction')}</th><th>{t('firewall.ruleProtocol')}</th><th
								>{t('firewall.rulePortsLabel')}</th
							><th>{t('firewall.ruleSourceLabel')}</th><th>{t('firewall.ruleZone')}</th><th
								>{t('firewall.ruleCommentLabel')}</th
							><th></th></tr
						></thead
					>
					<tbody>
						{#each draft.rules as rule, i (i)}
							{@const locked = busy || !!status.pending}
							<tr>
								<td
									><input
										type="checkbox"
										checked={!rule.disabled}
										disabled={locked}
										aria-label={t('firewall.ruleOn')}
										onchange={(event) => (rule.disabled = !event.currentTarget.checked || undefined)}
									/></td
								>
								<td
									><select class="field" bind:value={rule.action} disabled={locked} aria-label={t('firewall.ruleAction')}
										><option value="allow">{t('firewall.allow')}</option><option value="deny"
											>{t('firewall.deny')}</option
										></select
									></td
								>
								<td
									><select class="field" bind:value={rule.protocol} disabled={locked} aria-label={t('firewall.ruleProtocol')}
										><option value="tcp">TCP</option><option value="udp">UDP</option><option value="any"
											>{t('firewall.anyProtocol')}</option
										></select
									></td
								>
								<td
									><input
										class="field mono port"
										bind:value={rule.ports}
										placeholder={t('firewall.allPorts')}
										disabled={locked}
										aria-label={t('firewall.rulePortsLabel')}
									/></td
								>
								<td
									><input
										class="field mono"
										bind:value={rule.source}
										placeholder={t('firewall.anySource')}
										spellcheck="false"
										disabled={locked}
										aria-label={t('firewall.ruleSourceLabel')}
									/></td
								>
								<td
									><select class="field" bind:value={rule.zone} disabled={locked} aria-label={t('firewall.ruleZone')}
										><option value="">{t('firewall.allZones')}</option>{#each zones as zone (zone)}<option
												value={zone}>{zoneLabel(zone)}</option
											>{/each}</select
									></td
								>
								<td
									><input
										class="field"
										bind:value={rule.comment}
										maxlength="80"
										disabled={locked}
										aria-label={t('firewall.ruleCommentLabel')}
									/></td
								>
								<td class="buttons"
									><button
										class="btn small"
										type="button"
										disabled={locked || i === 0}
										aria-label={t('firewall.ruleUp')}
										onclick={() => moveRule(i, -1)}>↑</button
									><button
										class="btn small"
										type="button"
										disabled={locked || i === draft.rules.length - 1}
										aria-label={t('firewall.ruleDown')}
										onclick={() => moveRule(i, 1)}>↓</button
									><button class="btn small" type="button" disabled={locked} onclick={() => removeRule(i)}
										>{t('firewall.ruleRemove')}</button
									></td
								>
							</tr>
							{#if problems[i]}<tr
									><td colspan="8"><p class="notice-warn" role="alert">{t(problems[i] as MessageKey)}</p></td></tr
								>{/if}
						{/each}
					</tbody>
				</table>
			</div>{:else}<p class="muted">{t('firewall.noRules')}</p>{/if}
		<div class="row-actions">
			<button class="btn" type="button" disabled={busy || !!status.pending || draft.rules.length >= 100} onclick={addRule}
				>{t('firewall.addRule')}</button
			>
		</div>
	</section>

	{#if dirty || error || notice}<div class="save-bar {dirty ? 'notice-warn' : ''}">
			{#if dirty}<p>{t(draft.enabled ? 'firewall.unsavedTrial' : 'firewall.unsaved')}</p>{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			{#if notice}<p role="status">{notice}</p>{/if}
			{#if dirty}<div class="row-actions">
					<button class="btn" disabled={busy} onclick={discard}>{t('common.cancel')}</button
					><button class="btn btn-primary" disabled={busy || !valid} onclick={apply}
						>{t(draft.enabled ? 'firewall.applyTrial' : 'common.save')}</button
					>
				</div>{/if}
		</div>{/if}

	{#if autoDraft}<form
			class="tile panel"
			aria-labelledby="firewall-autoblock"
			onsubmit={(event) => {
				event.preventDefault();
				void saveAutoBlock();
			}}
			oninput={() => {
				autoEdited = true;
				autoNotice = '';
			}}
		>
			<h2 class="tile-title" id="firewall-autoblock" use:revealTarget>{t('firewall.autoBlock')}</h2>
			<label class="check"
				><input type="checkbox" bind:checked={autoDraft.enabled} /><span>{t('firewall.autoBlockEnable')}</span></label
			>
			<p class="muted">{t('firewall.autoBlockText')}</p>
			<div class="numbers">
				<label class="stack"
					><span>{t('firewall.attempts')}</span><input
						class="field"
						type="number"
						min="2"
						max="100"
						bind:value={autoDraft.attempts}
						disabled={!autoDraft.enabled}
					/></label
				>
				<label class="stack"
					><span>{t('firewall.within')}</span><input
						class="field"
						type="number"
						min="1"
						max="1440"
						bind:value={autoDraft.within}
						disabled={!autoDraft.enabled}
					/></label
				>
				<label class="stack"
					><span>{t('firewall.blockMinutes')}</span><input
						class="field"
						type="number"
						min="1"
						max="525600"
						bind:value={autoDraft.minutes}
						disabled={!autoDraft.enabled || autoDraft.permanent}
					/></label
				>
			</div>
			<label class="check"
				><input type="checkbox" bind:checked={autoDraft.permanent} disabled={!autoDraft.enabled} /><span
					>{t('firewall.permanent')}</span
				></label
			>
			<label class="check"
				><input type="checkbox" bind:checked={autoDraft.exemptLocal} disabled={!autoDraft.enabled} /><span
					>{t('firewall.exemptLocal')}</span
				></label
			>
			<label class="stack"
				><span>{t('firewall.exempt')}</span><textarea
					class="field mono"
					rows="3"
					spellcheck="false"
					placeholder="203.0.113.0/24"
					bind:value={exemptText}
					disabled={!autoDraft.enabled}
				></textarea></label
			>
			{#if !exemptValid}<p class="notice-warn" role="alert">{t('firewall.ruleSource')}</p>{/if}
			<p class="muted small">{t('firewall.autoBlockSources')}</p>
			{#if autoError}<p class="notice-error" role="alert">{autoError}</p>{/if}
			<div class="row-actions">
				<button class="btn btn-primary" disabled={autoBusy || !autoEdited || !exemptValid}>{t('common.save')}</button>
				{#if autoNotice}<span class="muted" role="status">{autoNotice}</span>{/if}
			</div>

			<h3 class="divided">{t('firewall.blocked')}</h3>
			{#if status.blocked.length}<div class="table">
					<table>
						<thead
							><tr
								><th>{t('firewall.address')}</th><th>{t('firewall.blockSource')}</th><th>{t('firewall.since')}</th><th
									>{t('firewall.until')}</th
								><th></th></tr
							></thead
						>
						<tbody>
							{#each status.blocked as block (block.address)}
								<tr>
									<td class="mono">{block.address}</td>
									<td>{sourceLabel(block.source)} ({block.attempts})</td>
									<td>{formatDateTime(new Date(block.since))}</td>
									<td>{block.until ? formatDateTime(new Date(block.until)) : t('firewall.forever')}</td>
									<td
										><button class="btn small" type="button" onclick={() => (releasing = block.address)}
											>{t('firewall.release')}</button
										></td
									>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>{:else}<p class="muted">{t('firewall.noBlocked')}</p>{/if}
		</form>{/if}

	<section class="tile panel" aria-labelledby="firewall-recent">
		<h2 class="tile-title" id="firewall-recent" use:revealTarget>{t('firewall.recent')}</h2>
		<p class="muted">{t('firewall.recentText')}</p>
		{#if status.recent.length}<div class="table">
				<table>
					<thead
						><tr
							><th>{t('firewall.time')}</th><th>{t('firewall.address')}</th><th>{t('firewall.zoneColumn')}</th><th
								>{t('firewall.ruleProtocol')}</th
							><th>{t('firewall.port')}</th><th>{t('firewall.interface')}</th></tr
						></thead
					>
					<tbody>
						{#each status.recent as drop, i (i)}
							<tr>
								<td>{formatDateTime(new Date(drop.at))}</td>
								<td class="mono">{drop.source}</td>
								<td
									>{drop.zone === 'blocked' ? t('firewall.zoneBlocked') : zoneLabel(drop.zone)}{#if drop.app}
										· {t('firewall.toApp')}{/if}</td
								>
								<td>{drop.protocol.toUpperCase()}</td>
								<td class="mono">{drop.port ?? '–'}</td>
								<td class="mono">{drop.interface ?? ''}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>{:else}<p class="muted">{t('firewall.noRecent')}</p>{/if}
		<div class="row-actions">
			<button class="btn" type="button" onclick={load}>{t('firewall.refresh')}</button>
			<button class="btn" type="button" disabled={recentBusy || !status.recent.length} onclick={clearRecent}
				>{t('firewall.clearRecent')}</button
			>
		</div>
	</section>
{:else if !loadError}<p role="status">{t('common.loading')}</p>{/if}

{#if releasing}
	<Dialog title={t('firewall.releaseTitle')} locked={releaseBusy} onclose={() => (releasing = null)}>
		<p>{t('firewall.releaseText', { address: releasing })}</p>
		{#if releaseError}<p class="notice-error" role="alert">{releaseError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={releaseBusy} onclick={() => (releasing = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-warn" disabled={releaseBusy} onclick={release}
				>{t('firewall.release')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.table {
		overflow-x: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
	}
	th,
	td {
		padding: 8px 10px;
		border-bottom: 1px solid var(--border);
		text-align: left;
		vertical-align: middle;
	}
	th {
		color: var(--muted);
		font-weight: 500;
		font-size: 0.75rem;
	}
	.zone {
		text-align: center;
		width: 5.5rem;
	}
	.small {
		font-size: 0.8125rem;
	}
	.rules .field {
		min-width: 7rem;
	}
	.rules input.field.mono:not(.port) {
		min-width: 11rem;
	}
	.rules .port {
		width: 7rem;
	}
	.buttons {
		white-space: nowrap;
	}
	.buttons .btn + .btn {
		margin-left: 4px;
	}
	.new-apps td {
		border-bottom: none;
	}
	.zones-list {
		display: grid;
		gap: 6px;
		margin: 0;
	}
	.zones-list div {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 12px;
	}
	.zones-list dt {
		font-weight: 500;
		min-width: 7rem;
	}
	.zones-list dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.numbers {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
	}
	.numbers .stack {
		flex: 1 1 10rem;
	}
	.save-bar {
		position: sticky;
		bottom: 12px;
		z-index: 2;
	}
	.trial strong {
		font-variant-numeric: tabular-nums;
	}
</style>
