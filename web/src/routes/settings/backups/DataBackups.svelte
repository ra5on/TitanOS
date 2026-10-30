<script lang="ts">
	import ClusterPair from '#lib/components/ClusterPair.svelte';
 import ApplicationSelection,{type Application} from '#lib/operations/ApplicationSelection.svelte';
 import ProbeDialog from '#lib/operations/ProbeDialog.svelte';
 import RecoveryPanel from '#lib/operations/RecoveryPanel.svelte';
 let probing=$state('');
	import { revealTarget } from '#lib/reveal-target.ts';
	let { restoreOnly = false }: { restoreOnly?: boolean } = $props();
	// Backups of shares to another volume, another server or the cloud.
	import { api, ApiError } from '#lib/api.ts';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { bandwidthForm, bandwidthParts, bandwidthPayload, maxBandwidth, type Bandwidth, type BandwidthForm } from '#lib/backup-bandwidth.ts';
	import FolderTree from './FolderTree.svelte';
	import SnapshotFiles from './SnapshotFiles.svelte';
	import SmbTargets, {type SmbTarget} from './SmbTargets.svelte';
	import TargetPanel from './TargetPanel.svelte';
	import ReplicationPanel from './ReplicationPanel.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { askAbout, assistant } from '#lib/assistant.svelte.ts';

	type Kind = 'smb-copy' | 'usb-copy' | 'local' | 's3' | 'sftp' | 'rest' | 'titanos';
	type Destination = {
		type: Kind;
		deviceId?: string;
 targetId?: string;
		volume?: string;
		folder?: string;
		endpoint?: string;
		bucket?: string;
		prefix?: string;
		accessKey?: string;
		secretKey?: string;
		host?: string;
		port?: number;
		user?: string;
		path?: string;
		url?: string;
		username?: string;
		password?: string;
	};
	type Job = { applications?:Application[];allowStop?:boolean; id: string; sources: string[]; folders?: Record<string, string[]>; skip?: string[]; patterns?: string[]; extras?: string[]; destination: Destination; enabled: boolean; every: 'hourly' | 'daily' | 'weekly' | 'monthly'; interval?: number; weekday: number; weekdays?: number[]; monthDay?: number; time: string; times?: string[]; windowFrom?: string; windowTo?: string; keep: { daily: number; weekly: number; monthly: number }; bandwidth?: Bandwidth };
	type State = { statusError?: string; probe?:{at:string;snapshot:string;files:number;bytes:number;truncated:boolean;error:string}; running: boolean; phase: string; percent: number; bytesDone: number; bytesTotal: number; bandwidthLimit?: number; lastRun: string | null; lastOK: string | null; lastError: string; warning: string; bytes: number; restoredTo: string; restoreError?: string; lastCheck?: string; checkError?: string };
	type View = { stateKnown?: boolean; job: Job; secretSet: Record<string, boolean>; state: State; next: string | null };
	type Overview = { smbTargets: SmbTarget[]; jobs: View[]; shares: { name: string; volume: string }[]; volumes: string[] | null };
	type Snapshot = { id: string; time: string; paths: string[]; bytes: number };

	let usbDrives = $state<{id: string; name: string; supported: boolean}[]>([]);
	let overview = $state<Overview | null>(null);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state(false);
	let loadError = $state('');
	$effect(() => { if (feedback) { const timer = setTimeout(() => feedback = '', 5000); return () => clearTimeout(timer); } });
	let pending = $state<Record<string, string>>({});
	let feedback = $state('');
	let retry = $state<{ job: Job; action: 'run' | 'stop' } | null>(null);
	// The job being created (isNew) or changed.
	let draft = $state<Job | null>(null);
	let isNew = $state(false);
	let secretSet = $state<Record<string, boolean>>({});
	let publicKey = $state('');
	let removing = $state<Job | null>(null);
	let restoring = $state<{ job: Job; snapshots: Snapshot[] | null; snapshot: string; volume: string; target: string; mode: 'all' | 'files'; paths: string[] } | null>(null);
	let checking = $state<{ job: Job; readData: number } | null>(null);
	let secret = $state<{ job: Job; password: string; asking: boolean } | null>(null);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: ['invalid_backup_job', 'backup_job_failed'].includes(code) && err instanceof ApiError ? err.message.replace(/^backup: invalid job: /, '') : '' };
	}

	// Answers can overtake each other: a look that started before a run was
	// accepted must not put back the state from before it.
	let generation = 0;
	function apply(next: Overview) {
		generation++;
		overview = next;
	}
	async function load() {
		const mine = ++generation;
		try {
			const next = await api.get<Overview>('/backups');
			if (mine !== generation) return;
			overview = next;
			loadError = '';
		} catch (err) {
			if (mine !== generation) return;
			loadError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
	// USB drives are only needed by the form; a drive list that cannot be
	// read does not make the page look broken.
	let usbError = $state('');
	async function loadUSB() {
		usbError = '';
		try {
			usbDrives = await api.get<typeof usbDrives>('/usb');
		} catch (err) {
			usbError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
	const active = $derived(overview?.jobs.some((view) => view.state.running) ?? false);
	$effect(() => {
		load();
		const timer = setInterval(() => !document.hidden && load(), active ? 2000 : 20_000);
		return () => clearInterval(timer);
	});

	const volumes = $derived(overview?.volumes ?? []);
	const WEEKDAYS = [1, 2, 3, 4, 5, 6, 0];

	function create() {
		error = null;
		isNew = true;
		secretSet = {};
		draft = { id: '', sources: [], applications: [], allowStop: false, destination: { type: 'local', volume: '', folder: '', port: 22 }, enabled: true, every: 'daily', interval: 6, weekday: 0, weekdays: [1], monthDay: 1, time: '02:30', times: ['02:30'], keep: { daily: 7, weekly: 4, monthly: 6 } };
		original = null;
		loadSelection(draft);
		loadUSB();
	}

	function edit(view: View) {
		error = null;
		isNew = false;
		secretSet = view.secretSet;
		draft = JSON.parse(JSON.stringify(view.job)) as Job;
		draft.destination = { port: 22, ...draft.destination, secretKey: '', password: '' };
		// Jobs from before the schedule grew carry one weekday and one time.
		draft.interval ||= 6;
		draft.monthDay ||= 1;
		// ApplicationSelection binds both; a bound prop may not be undefined.
		draft.applications ??= [];
		draft.allowStop ??= false;
		if (!draft.weekdays?.length) draft.weekdays = [draft.weekday];
		if (!draft.times?.length) draft.times = [draft.time];
		original = { ...draft.destination };
		loadSelection(draft);
		loadUSB();
		if (draft.destination.type === 'sftp') showKey();
	}
	// The destination as it was stored: a stored secret only stays for the
	// same server and account (the NAS does not send it anywhere else).
	let original = $state<Destination | null>(null);
	const secretKept = $derived.by(() => {
		const d = draft?.destination;
		if (!d || !original || d.type !== original.type) return { secretKey: false, password: false };
		return {
			secretKey: !!secretSet.secretKey && d.endpoint?.trim() === original.endpoint && d.accessKey?.trim() === original.accessKey,
			password: !!secretSet.password && d.url?.trim() === original.url && (d.username?.trim() ?? '') === (original.username ?? ''),
		};
	});

	type Mode = 'whole' | 'all' | 'only';
	const EXTRAS = ['appdata', 'vms', 'config'] as const;
	/** Per share: all of it, or with folders left out, or only some folders. */
	let modes = $state<Record<string, Mode>>({});
	let patterns = $state('');
	let bandwidth = $state<BandwidthForm>(bandwidthForm());

	function loadSelection(job: Job) {
		modes = {};
		for (const name of job.sources) {
			modes[name] = job.folders?.[name]?.length ? 'only' : job.skip?.some((skip) => skip.startsWith(`${name}/`)) ? 'all' : 'whole';
		}
		patterns = (job.patterns ?? []).join('\n');
		bandwidth = bandwidthForm(job.bandwidth);
	}

	function setMode(name: string, mode: Mode) {
		if (!draft) return;
		modes[name] = mode;
		// What belongs to the other way of choosing is dropped.
		if (mode !== 'only' && draft.folders) delete draft.folders[name];
		if (mode !== 'all' && draft.skip) draft.skip = draft.skip.filter((skip) => !skip.startsWith(`${name}/`));
	}

	function setFolders(name: string, folders: string[], skip: string[]) {
		if (!draft) return;
		draft.folders = { ...draft.folders, [name]: folders };
		draft.skip = skip;
	}

	function toggleExtra(extra: string) {
		if (!draft) return;
		const list = draft.extras ?? [];
		draft.extras = list.includes(extra) ? list.filter((e) => e !== extra) : [...list, extra];
	}

	function toggleDay(day: number) {
		if (!draft) return;
		const days = draft.weekdays ?? [];
		// At least one day stays.
		draft.weekdays = days.includes(day) ? (days.length > 1 ? days.filter((d) => d !== day) : days) : [...days, day];
	}

	const needsTimes = (every: string) => every !== 'hourly';
	const windowOn = $derived(!!draft?.windowFrom);
	function toggleWindow(on: boolean) {
		if (!draft) return;
		[draft.windowFrom, draft.windowTo] = on ? ['01:00', '06:00'] : ['', ''];
	}

	function toggleSource(name: string) {
		if (!draft) return;
		const on = !draft.sources.includes(name);
		draft.sources = on ? [...draft.sources, name] : draft.sources.filter((source) => source !== name);
		if (on) modes[name] = 'whole';
		else {
			setMode(name, 'whole');
			delete modes[name];
		}
	}

	/** A pairing code of another TitanOS: titanos-backup: and base64url JSON. */
	let pairCode = $state('');
	function decodeCode(text: string): { url: string; user: string; password: string } | null {
		try {
			const body = text.trim().replace(/^titanos-backup:/, '').replace(/-/g, '+').replace(/_/g, '/');
			const data = JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(body), (c) => c.charCodeAt(0))));
			return data.v === 1 && typeof data.url === 'string' && typeof data.user === 'string' && typeof data.password === 'string' ? data : null;
		} catch {
			return null;
		}
	}
	const pairing = $derived(decodeCode(pairCode));

	async function showKey() {
		if (publicKey) return;
		try {
			publicKey = (await api.post<{ publicKey: string }>('/backups/ssh-key')).publicKey;
		} catch (err) {
			problem(err);
		}
	}

	const plainCopy = (kind: string) => kind === 'usb-copy' || kind === 'smb-copy';
	function setKind(kind: Kind) {
		if (!draft) return;
		draft.destination.type = kind;
		if (plainCopy(kind)) { draft.applications=[];draft.allowStop=false;draft.extras = []; draft.keep = {daily: 0, weekly: 0, monthly: 0}; }
		if (kind === 'sftp') showKey();
		if (kind === 'titanos') pairCode = '';
	}

	// Only what belongs to the chosen kind of destination is sent.
	function destination(d: Destination): Destination {
		switch (d.type) {
			case 'smb-copy': return {type:'smb-copy',targetId:d.targetId,folder:d.folder};
			case 'usb-copy': return { type: 'usb-copy', deviceId: d.deviceId, folder: d.folder };
			case 'local':
				return { type: 'local', volume: d.volume, folder: d.folder };
			case 's3':
				return { type: 's3', endpoint: d.endpoint?.trim(), bucket: d.bucket?.trim(), prefix: d.prefix?.trim(), accessKey: d.accessKey?.trim(), secretKey: d.secretKey };
			case 'sftp':
				return { type: 'sftp', host: d.host?.trim(), port: Number(d.port) || 22, user: d.user?.trim(), path: d.path?.trim() };
			case 'titanos': {
				const code = decodeCode(pairCode);
				return { type: 'rest', url: code?.url, username: code?.user, password: code?.password };
			}
			default:
				return { type: 'rest', url: d.url?.trim(), username: d.username?.trim(), password: d.password };
		}
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!draft) return;
		busy = true;
		error = null;
		if (draft.destination.type === 'titanos' && !decodeCode(pairCode)) {
			error = { text: t('backup.pairCodeInvalid'), detail: '' };
			busy = false;
			return;
		}
		const chosen = draft.sources.filter((name) => modes[name] === 'only' && !(draft!.folders?.[name]?.length));
		if (chosen.length > 0) {
			error = { text: t('backup.noFolderChosen', { share: chosen[0] }), detail: '' };
			busy = false;
			return;
		}
		const folders = Object.fromEntries(Object.entries(draft.folders ?? {}).filter(([name, list]) => modes[name] === 'only' && list.length > 0));
		const skip = (draft.skip ?? []).filter((entry) => modes[entry.split('/')[0]] === 'all');
		const times = [...new Set((draft.times ?? []).filter(Boolean))].sort();
		const weekdays = [...(draft.weekdays ?? [])].sort();
		const job = { ...draft, folders, skip, patterns: patterns.split('\n').map((line) => line.trim()).filter(Boolean), extras: draft.extras ?? [], id: draft.id.trim(), interval: Number(draft.interval) || 0, monthDay: Number(draft.monthDay) || 0, times, time: times[0] ?? draft.time, weekdays, weekday: weekdays[0] ?? 0, windowFrom: draft.windowFrom || '', windowTo: draft.windowFrom ? draft.windowTo || '' : '', destination: destination(draft.destination), bandwidth: bandwidthPayload(bandwidth), keep: { daily: Number(draft.keep.daily) || 0, weekly: Number(draft.keep.weekly) || 0, monthly: Number(draft.keep.monthly) || 0 } };
		try {
			apply(isNew ? await api.post<Overview>('/backups', job) : await api.put<Overview>(`/backups/${job.id}`, job));
			feedback = t('ui.saved');
			const created = isNew ? job : null;
			draft = null;
			// Without its password a backup is worthless: show it right away.
			if (created && !plainCopy(created.destination.type)) showPassword(created as Job);
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	async function act(job: Job, action: 'run' | 'stop') {
		if (pending[job.id]) return;
		pending[job.id] = action; feedback = ''; retry = null;
		error = null;
		try {
			apply(await api.post<Overview>(`/backups/${job.id}/${action}`));
			feedback = t('ui.backupAccepted', { name: job.id });
		} catch (err) {
			problem(err); retry = { job, action };
		} finally { delete pending[job.id]; }
	}

	let removeError = $state('');
	async function remove() {
		if (!removing || busy) return;
		busy = true;
		removeError = '';
		try {
			apply(await api.delete<Overview>(`/backups/${removing.id}`));
			removing = null;
		} catch (err) {
			removeError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
		busy = false;
	}

	async function showPassword(job: Job) {
		error = null;
		const confirmed = secret?.asking === true;
		try {
			const res = await api.get<{ password: string }>(`/backups/${job.id}/password`);
			secret = { job, password: res.password, asking: false };
		} catch (err) {
			// Asked once; a second "confirm" answer or any other failure closes
			// the dialog instead of leaving it locked.
			if (!confirmed && err instanceof ApiError && err.code === 'confirm_required') secret = { job, password: '', asking: true };
			else {
				secret = null;
				problem(err);
			}
		}
	}
	let copiedPassword = $state(false);
	async function copyPassword() {
		if (!secret) return;
		try {
			await navigator.clipboard.writeText(secret.password);
			copiedPassword = true;
		} catch {
			// The password is shown and can be selected by hand.
		}
	}

	// Each opening of the restore dialog has its number; a list of saved
	// states that arrives late belongs to an earlier one and is dropped.
	let restoreRequest = 0;
	let dialogError = $state('');
	async function openRestore(job: Job) {
		error = null;
		dialogError = '';
		const mine = ++restoreRequest;
		restoring = { job, snapshots: null, snapshot: '', volume: volumes[0] ?? '', target: '', mode: 'all', paths: [] };
		try {
			const res = await api.get<{ snapshots: Snapshot[] }>(`/backups/${job.id}/snapshots`);
			if (mine !== restoreRequest || !restoring) return;
			restoring.snapshots = res.snapshots;
			restoring.snapshot = res.snapshots[0]?.id ?? '';
		} catch (err) {
			if (mine !== restoreRequest) return;
			problem(err);
			restoring = null;
		}
	}
	function closeRestore() {
		restoreRequest++;
		restoring = null;
		dialogError = '';
	}

	async function restore(event: SubmitEvent) {
		event.preventDefault();
		if (!restoring || busy) return;
		busy = true;
		dialogError = '';
		try {
			const res = await api.post<{ target: string }>(`/backups/${restoring.job.id}/restore`, { snapshot: restoring.snapshot, volume: restoring.volume, paths: restoring.mode === 'files' ? restoring.paths : undefined });
			restoring.target = res.target;
			load();
		} catch (err) {
			// The dialog stays with what was chosen; the reason is shown in it.
			dialogError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
		busy = false;
	}

	async function check(event: SubmitEvent) {
		event.preventDefault();
		if (!checking || busy) return;
		busy = true;
		dialogError = '';
		try {
			await api.post(`/backups/${checking.job.id}/check`, { readData: Number(checking.readData) });
			checking = null;
			load();
		} catch (err) {
			dialogError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
		busy = false;
	}

	function where(d: Destination): string {
		if (d.type === 'smb-copy') return `${d.targetId} / ${d.folder}`;
		if (d.type === 'usb-copy') return `${t('backup.kind.usb-copy')} ${d.deviceId} / ${d.folder}`;
		switch (d.type) {
			case 'local':
				return `/mnt/${d.volume}/${d.folder}`;
			case 's3':
				return `${d.endpoint}/${d.bucket}${d.prefix ? '/' + d.prefix : ''}`;
			case 'sftp':
				return `${d.user}@${d.host}:${d.path}`;
			default:
				return d.url ?? '';
		}
	}

	function when(job: Job): string {
		if (!job.enabled) return t('backup.manual');
		const times = (job.times?.length ? job.times : [job.time]).join(', ');
		let text: string;
		switch (job.every) {
			case 'hourly':
				text = t('backup.whenHourly', { hours: job.interval ?? 1, minute: job.time.slice(3) });
				break;
			case 'weekly': {
				const days = (job.weekdays?.length ? job.weekdays : [job.weekday]).slice().sort((a, b) => ((a + 6) % 7) - ((b + 6) % 7));
				text = t('backup.whenWeekly', { day: days.map((d) => t(`backup.dayShort.${d}` as 'backup.dayShort.0')).join(', '), time: times });
				break;
			}
			case 'monthly':
				text = t('backup.whenMonthly', { day: job.monthDay ?? 1, time: times });
				break;
			default:
				text = t('backup.whenDaily', { time: times });
		}
		return job.windowFrom ? `${text} · ${t('backup.whenWindow', { from: job.windowFrom, to: job.windowTo ?? '' })}` : text;
	}

	const rate = (mib: number) => (mib > 0 ? t('backup.bandwidthRate', { value: mib }) : t('backup.bandwidthNone'));
	function bandwidthText(b?: Bandwidth): string {
		const parts = bandwidthParts(b);
		if (!parts) return t('backup.bandwidthNone');
		return parts.window ? `${rate(parts.limit)} · ${t('backup.bandwidthWindowText', { from: parts.window.from, to: parts.window.to, limit: rate(parts.window.limit) })}` : rate(parts.limit);
	}
</script>

{#if !restoreOnly}<PageHead title={t('safeguard.data')} parent={{ href: '/settings/backups', label: t('safeguard.title') }}>
	<p class="muted">{t('backup.intro')}</p>
</PageHead>{:else}<h2 class="tile-title" id="restore-files" use:revealTarget>{t('safeguard.filesRestore')}</h2>{/if}

{#if error && !draft}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}

{#if loadError}<div class="notice-error" role="alert">{loadError} <button class="btn" onclick={load}>{t('ui.retry')}</button></div>{:else if !overview}<p class="loading" role="status">{t('common.loading')}</p>{/if}
{#if feedback}<p class="notice-ok" role="status">{feedback}</p>{/if}
{#if retry && error}<button type="button" class="btn" onclick={() => retry && act(retry.job, retry.action)}>{t('ui.retry')}</button>{/if}

{#if overview}
	{#if !restoreOnly}<RecoveryPanel/>{/if}
	{#if !restoreOnly}<div class="targets" id="backup-targets" use:revealTarget><SmbTargets targets={overview.smbTargets} onchange={load} /><TargetPanel /><ReplicationPanel /></div>{/if}
	{#if !restoreOnly}<h2 class="tile-title" id="backup-history" use:revealTarget>{t('safeguard.history')}</h2>{/if}
	{#each overview.jobs as view (view.job.id)}
		{@const job = view.job}
		{@const state = view.state}
		{@const next = view.next}
		<section class="tile panel" use:revealTarget id="backup-{job.id}">
			<div class="head">
				<div>
					<h2 class="name mono">{job.id}</h2>{#if job.destination.type === 'smb-copy'}<span class="chip warn">{t('backup.beta')}</span>{/if}
					<p class="muted">{[...job.sources,...(job.applications??[]).map(a=>a.name)].join(', ')} → <span class="mono">{where(job.destination)}</span></p>
				</div>
				{#if state.running}<span class="chip info">{t(`backup.phase.${state.phase}` as 'backup.phase.backup')}</span>
				{:else if view.stateKnown === false || state.statusError}<span class="chip warn">{t('tasks.state.unknown')}</span>
				{:else if state.lastError && state.lastError !== 'cancelled'}<span class="chip error">{t('backup.failed')}</span>
				{:else if state.lastError === 'cancelled'}<span class="chip warn">{t('backup.cancelled')}</span>
				{:else if state.warning}<span class="chip warn">{t('backup.withWarning')}</span>
				{:else if state.lastOK}<span class="chip ok">{t('backup.ok')}</span>
				{:else}<span class="chip">{t('backup.never')}</span>{/if}
			</div>
			{#if state.running}
				<div class="bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(state.percent)} aria-label={t(`backup.phase.${state.phase}` as 'backup.phase.backup')}><span style:width="{state.percent}%"></span></div>
				<p class="muted mono">{#if state.bytesTotal > 0}{state.percent.toFixed(0)} %{' · '}{formatBytes(state.bytesDone)} / {formatBytes(state.bytesTotal)}{:else}{formatBytes(state.bytesDone)}{/if}{#if job.bandwidth}{' · '}{(state.bandwidthLimit ?? 0) > 0 ? t('backup.bandwidthActive', { value: state.bandwidthLimit ?? 0 }) : t('backup.bandwidthActiveNone')}{/if}</p>
			{/if}
			{#if view.stateKnown === false || state.statusError}<p class="notice-warn">{t('coverage.stateUnknown')}</p>{/if}
			<dl>
				<div><dt>{t('backup.schedule')}</dt><dd>{when(job)}</dd></div>
				{#if next}<div><dt>{t('backup.next')}</dt><dd>{formatDateTime(new Date(next))}</dd></div>
				{:else if job.enabled}<div><dt>{t('backup.next')}</dt><dd class="muted">{t('backup.nextUnknown')}</dd></div>{/if}
				<div><dt>{t('safeguard.lastRun')}</dt><dd>{state.lastRun ? formatDateTime(new Date(state.lastRun)) : '–'}</dd></div>
				<div><dt>{t('backup.lastOK')}</dt><dd>{state.lastOK ? formatDateTime(new Date(state.lastOK)) : '–'}{#if state.lastOK && state.bytes > 0}{' · '}{formatBytes(state.bytes)}{/if}</dd></div>
				{#if job.bandwidth}<div><dt>{t('backup.bandwidth')}</dt><dd>{bandwidthText(job.bandwidth)}</dd></div>{/if}
				<div><dt>{t('backup.keeps')}</dt><dd>{plainCopy(job.destination.type) ? t('backup.copyRetain') : job.keep.daily + job.keep.weekly + job.keep.monthly > 0 ? t('backup.keepsText', { daily: job.keep.daily, weekly: job.keep.weekly, monthly: job.keep.monthly }) : t('backup.keepsAll')}</dd></div>
			</dl>
			{#if !state.running && state.lastError && state.lastError !== 'cancelled'}<div class="notice-error">{t('backup.lastError')}<pre>{state.lastError}</pre>{#if assistant.status?.available}<button type="button" class="btn" onclick={() => askAbout(t('assistant.askBackup', { job: job.id }), [{ name: `${job.id}-error.txt`, kind: 'text', text: state.lastError }])}><Icon name="chat" size={14} />{t('assistant.ask')}</button>{/if}</div>{/if}
			{#if !state.running && state.warning}<div class="notice-warn">{t('backup.warning')}<pre>{state.warning}</pre></div>{/if}
			{#if !state.running && state.lastCheck}
				{#if state.checkError}<div class="notice-error">{t('backup.checkFailed')}<pre>{state.checkError}</pre></div>
				{:else}<p class="muted">{t('backup.checkOk', { at: formatDateTime(new Date(state.lastCheck)) })}</p>{/if}
			{/if}
			{#if state.probe}<p class={state.probe.error?'notice-warn':'muted'}>{t('ops.probe')} · {formatDateTime(new Date(state.probe.at))} · {t('backup.probeFiles', { count: state.probe.files })} · {formatBytes(state.probe.bytes)}{#if state.probe.truncated}<span> · {t('ops.truncated')}</span>{/if}{#if state.probe.error}<span> · {state.probe.error}</span>{/if}</p>{/if}
			{#if !state.running && state.restoreError}<div class="notice-error">{t('backup.restoreFailed')}<pre>{state.restoreError}</pre></div>
			{:else if !state.running && state.restoredTo}<p class="notice-ok">{t('backup.restoredTo')} <span class="mono">{state.restoredTo}</span></p>{/if}
			{#if pending[job.id]}<p role="status">{t(pending[job.id] === 'run' ? 'ui.starting' : 'ui.stopping')}</p>{/if}
			<div class="row">
				{#if state.running}
					<button type="button" class="btn" disabled={!!pending[job.id]} onclick={() => act(job, 'stop')}>{t('backup.stop')}</button>
				{:else}
					{#if !restoreOnly}<button type="button" class="btn btn-primary" disabled={!!pending[job.id]} onclick={() => act(job, 'run')}>{t('backup.run')}</button>{/if}
					{#if !plainCopy(job.destination.type)}<button type="button" class="btn" onclick={() => openRestore(job)}>{t('backup.restore')}</button>
					<button type="button" class="btn" onclick={()=>probing=job.id}>{t('ops.probe')}</button>
					<button type="button" class="btn" onclick={() => ((checking = { job, readData: 0 }), (dialogError = ''))}>{t('backup.check')}</button>
					<button type="button" class="btn" onclick={() => showPassword(job)}>{t('backup.showPassword')}</button>{:else}<span class="muted">{t('backup.copyRestore')}</span>{/if}
					{#if !restoreOnly}<button type="button" class="btn" aria-label={t('backup.editNamed', { name: job.id })} onclick={() => edit(view)}>{t('backup.edit')}</button>
					<button type="button" class="btn" aria-label={t('backup.removeNamed', { name: job.id })} onclick={() => ((removing = job), (removeError = ''))}>{t('backup.remove')}</button>{/if}
				{/if}
			</div>
		</section>
	{:else}
		{#if !draft}<div class="tile panel"><p class="muted">{t('backup.none')}</p></div>{/if}
	{/each}

	{#if !draft && !restoreOnly}
		{#if overview.shares.length === 0}
			<p class="notice-warn">{t('backup.noShares')}</p>
		{:else}
			<div><button type="button" class="btn btn-primary" onclick={create}>{t('backup.add')}</button></div>
		{/if}
	{/if}
{/if}

{#if draft && overview}
	<form class="tile panel" onsubmit={save}>
		<h2 class="tile-title">{isNew ? t('backup.add') : t('backup.editTitle', { name: draft.id })}</h2>
		{#if isNew}
			<label class="narrow"><span>{t('backup.name')}</span>
				<input class="field mono" type="text" bind:value={draft.id} pattern="[A-Za-z][A-Za-z0-9\-]*" maxlength="31" autocapitalize="none" spellcheck="false" required />
				<small>{t('backup.nameHint')}</small>
			</label>
		{/if}
		<fieldset>
			<legend>{t('backup.sources')}</legend>
			{#each overview.shares as share (share.name)}
				<div class="source">
					<div class="row">
						<label class="check"><input type="checkbox" checked={draft.sources.includes(share.name)} onchange={() => toggleSource(share.name)} /><span class="mono">{share.name}</span></label>
						{#if draft.sources.includes(share.name)}
							<select class="field narrow" aria-label={t('backup.modeOf', { share: share.name })} value={modes[share.name] ?? 'whole'} onchange={(e) => setMode(share.name, e.currentTarget.value as Mode)}>
								<option value="whole">{t('backup.mode.whole')}</option>
								<option value="all">{t('backup.mode.all')}</option>
								<option value="only">{t('backup.mode.only')}</option>
							</select>
						{/if}
					</div>
					{#if draft.sources.includes(share.name) && modes[share.name] && modes[share.name] !== 'whole'}
						<p class="muted">{t(modes[share.name] === 'only' ? 'backup.onlyHint' : 'backup.allHint')}</p>
						<FolderTree share={share.name} mode={modes[share.name] as 'all' | 'only'} folders={draft.folders?.[share.name] ?? []} skip={draft.skip ?? []} onchange={(folders, skip) => setFolders(share.name, folders, skip)} />
					{/if}
				</div>
			{/each}
			{#if !plainCopy(draft.destination.type)}<p class="legend-sub">{t('backup.extras')}</p>
			<div class="row">
				{#each EXTRAS as extra (extra)}
					<label class="check"><input type="checkbox" checked={draft.extras?.includes(extra)} onchange={() => toggleExtra(extra)} /><span>{t(`backup.extra.${extra}` as 'backup.extra.appdata')}</span></label>
				{/each}
			</div>
			{/if}{#if !plainCopy(draft.destination.type)}<ApplicationSelection bind:applications={draft.applications} bind:allowStop={draft.allowStop}/>{/if}{#if draft.extras?.includes('config')}<p class="notice-warn">{t('backup.extra.configHint')}</p>{/if}
			<label class="block"><span>{t('backup.patterns')}</span>
				<textarea class="field mono" rows="2" bind:value={patterns} placeholder="*.tmp"></textarea>
			</label>
			<p class="muted">{t(plainCopy(draft.destination.type) ? 'backup.copyPatterns' : 'backup.patternsHint')}</p>
		</fieldset>
		<fieldset class="divided">
			<legend>{t('backup.destination')}</legend>
			<div class="segments" role="radiogroup" aria-label={t('backup.destination')}>
				{#each ['local', 'usb-copy', 'smb-copy', 'titanos', 's3', 'sftp', 'rest'] as const as kind (kind)}
					<button type="button" role="radio" aria-checked={draft.destination.type === kind} disabled={!isNew && (plainCopy(kind) !== plainCopy(draft.destination.type))} onclick={() => setKind(kind)}>{t(`backup.kind.${kind}`)}</button>
				{/each}
			</div>
			<p class="muted">{t(`backup.kindHint.${draft.destination.type}`)}</p>
			{#if draft.destination.type === 'smb-copy'}
 <span class="chip warn">{t('backup.beta')}</span>
 <label class="block"><span>{t('backup.smb.target')}</span><select class="field" bind:value={draft.destination.targetId} required><option value="" disabled>–</option>{#each overview?.smbTargets ?? [] as target(target.id)}<option value={target.id}>{target.id} · {target.host}/{target.share}</option>{/each}</select></label>
 <p class="muted">{t('backup.smb.first')}</p>
 <label class="block"><span>{t('backup.smb.jobFolder')}</span><input class="field" bind:value={draft.destination.folder} placeholder="backups/{draft.id}" /></label>
 {:else if draft.destination.type === 'usb-copy'}
 {#if usbError}<p class="notice-warn" role="alert">{usbError} <button type="button" class="btn small" onclick={loadUSB}>{t('ui.retry')}</button></p>{/if}
 <label class="block"><span>{t('backup.usbDevice')}</span><select class="field" bind:value={draft.destination.deviceId} required>
 <option value="" disabled>–</option>
 {#if draft.destination.deviceId && !usbDrives.some(d => d.id === draft?.destination.deviceId)}<option value={draft.destination.deviceId}>{draft.destination.deviceId} · {t('backup.usbMissing')}</option>{/if}
 {#each usbDrives.filter(d => d.supported) as drive (drive.id)}<option value={drive.id}>{drive.name} · {drive.id}</option>{/each}
 </select></label>
 <label class="block"><span>{t('backup.folder')}</span><input class="field mono" bind:value={draft.destination.folder} placeholder={`backups/${draft.id || 'name'}`} /></label>
 {:else if draft.destination.type === 'local'}
				<div class="row">
					<label class="narrow"><span>{t('backup.volume')}</span>
						<select class="field" bind:value={draft.destination.volume} required>
							<option value="" disabled>–</option>
							{#each volumes as volume (volume)}<option value={volume}>{volume}</option>{/each}
						</select>
					</label>
					<label class="narrow"><span>{t('backup.folder')}</span><input class="field mono" type="text" bind:value={draft.destination.folder} placeholder={`backups/${draft.id || 'name'}`} spellcheck="false" /></label>
				</div>
				{#if volumes.length === 0}<p class="notice-warn">{t('backup.noVolume')}</p>{/if}
				{:else if draft.destination.type === 's3'}
				<div class="row">
					<label class="wide"><span>{t('backup.endpoint')}</span><input class="field mono" type="url" bind:value={draft.destination.endpoint} placeholder="https://s3.eu-central-1.amazonaws.com" spellcheck="false" required /></label>
					<label class="narrow"><span>{t('backup.bucket')}</span><input class="field mono" type="text" bind:value={draft.destination.bucket} spellcheck="false" autocapitalize="none" required /></label>
					<label class="narrow"><span>{t('backup.prefix')}</span><input class="field mono" type="text" bind:value={draft.destination.prefix} spellcheck="false" /></label>
				</div>
				<div class="row">
					<label class="narrow"><span>{t('backup.accessKey')}</span><input class="field mono" type="text" bind:value={draft.destination.accessKey} autocomplete="off" spellcheck="false" required /></label>
					<label class="narrow"><span>{t('backup.secretKey')}</span><input class="field mono" type="password" bind:value={draft.destination.secretKey} autocomplete="new-password" placeholder={secretKept.secretKey ? t('notify.secretStored') : ''} required={!secretKept.secretKey} /></label>
 </div>
 {#if secretSet.secretKey && !secretKept.secretKey}<p class="notice-warn">{t('backup.secretAgain')}</p>{/if}
			{:else if draft.destination.type === 'sftp'}
				<div class="row">
					<label class="narrow"><span>{t('backup.host')}</span><input class="field mono" type="text" bind:value={draft.destination.host} spellcheck="false" autocapitalize="none" required /></label>
					<label class="tiny"><span>{t('notify.port')}</span><input class="field mono" type="number" min="1" max="65535" bind:value={draft.destination.port} required /></label>
					<label class="narrow"><span>{t('notify.username')}</span><input class="field mono" type="text" bind:value={draft.destination.user} spellcheck="false" autocapitalize="none" required /></label>
					<label class="narrow"><span>{t('backup.remotePath')}</span><input class="field mono" type="text" bind:value={draft.destination.path} placeholder="backups/nas" spellcheck="false" required /></label>
				</div>
				{#if publicKey}
					<label><span>{t('backup.publicKey')}</span><textarea class="field mono key" readonly rows="2" onfocus={(event) => event.currentTarget.select()}>{publicKey}</textarea></label>
					<p class="muted">{t('backup.publicKeyHint')}</p>
				{/if}
			{:else if draft.destination.type === 'titanos'}
				<ClusterPair onpair={(code) => (pairCode = code)} />
				<label class="block"><span>{t('backup.pairCode')}</span>
					<textarea class="field mono" rows="3" bind:value={pairCode} spellcheck="false" autocomplete="off" placeholder="titanos-backup:…" required></textarea>
				</label>
				{#if pairing}<p class="notice-ok">{t('backup.pairCodeOk', { url: pairing.url })}</p>{:else if pairCode.trim()}<p class="notice-warn">{t('backup.pairCodeInvalid')}</p>{/if}
				<p class="muted">{t('backup.pairCodeHint')}</p>
			{:else}
				<div class="row">
					<label class="wide"><span>{t('backup.restUrl')}</span><input class="field mono" type="url" bind:value={draft.destination.url} placeholder="https://backup.example.org:8000/nas" spellcheck="false" required /></label>
					<label class="narrow"><span>{t('notify.username')}</span><input class="field mono" type="text" bind:value={draft.destination.username} autocomplete="off" spellcheck="false" /></label>
					<label class="narrow"><span>{t('notify.password')}</span><input class="field mono" type="password" bind:value={draft.destination.password} autocomplete="new-password" placeholder={secretKept.password ? t('notify.secretStored') : ''} /></label>
 </div>
 {#if secretSet.password && !secretKept.password}<p class="notice-warn">{t('backup.secretAgain')}</p>{/if}
			{/if}
		</fieldset>
		<fieldset class="divided">
			<legend>{t('backup.schedule')}</legend>
			<label class="check"><input type="checkbox" bind:checked={draft.enabled} /><span>{t('backup.enabled')}</span></label>
			{#if draft.enabled}
				<div class="row">
					<label class="narrow"><span>{t('backup.every')}</span>
						<select class="field" bind:value={draft.every}>
							<option value="hourly">{t('backup.every.hourly')}</option>
							<option value="daily">{t('backup.every.daily')}</option>
							<option value="weekly">{t('backup.every.weekly')}</option>
							<option value="monthly">{t('backup.every.monthly')}</option>
						</select>
					</label>
					{#if draft.every === 'hourly'}
						<label class="narrow"><span>{t('backup.interval')}</span>
							<select class="field" bind:value={draft.interval}>
								{#each [1, 2, 3, 4, 6, 12] as hours (hours)}<option value={hours}>{t('backup.intervalHours', { hours })}</option>{/each}
							</select>
						</label>
					{/if}
					{#if draft.every === 'monthly'}
						<label class="tiny"><span>{t('backup.monthDay')}</span>
							<select class="field mono" bind:value={draft.monthDay}>
								{#each Array.from({ length: 28 }, (_, i) => i + 1) as day (day)}<option value={day}>{t('backup.monthDayOption', { day })}</option>{/each}
							</select>
						</label>
					{/if}
				</div>
				{#if draft.every === 'weekly'}
					<div class="days" role="group" aria-label={t('backup.weekdays')}>
						{#each WEEKDAYS as day (day)}
							<label class="check"><input type="checkbox" checked={draft.weekdays?.includes(day)} onchange={() => toggleDay(day)} /><span>{t(`backup.dayShort.${day}` as 'backup.dayShort.0')}</span></label>
						{/each}
					</div>
				{/if}
				{#if needsTimes(draft.every)}
					<div class="row">
						{#each draft.times ?? [] as _, i (i)}
							<label class="tiny"><span>{i === 0 ? t('update.autoTime') : ''}&nbsp;</span>
								<span class="inline"><input class="field mono" type="time" bind:value={draft.times![i]} required />
									{#if (draft.times?.length ?? 0) > 1}<button type="button" class="btn small" aria-label={t('backup.removeTime')} onclick={() => (draft!.times = draft!.times!.filter((_, n) => n !== i))}>✕</button>{/if}
								</span>
							</label>
						{/each}
						{#if (draft.times?.length ?? 0) < 8}<button type="button" class="btn small" onclick={() => (draft!.times = [...(draft!.times ?? []), '12:00'])}>{t('backup.addTime')}</button>{/if}
					</div>
				{:else}
					<label class="tiny"><span>{t('backup.minute')}</span><input class="field mono" type="time" bind:value={draft.times![0]} required /></label>
				{/if}
				<label class="check"><input type="checkbox" checked={windowOn} onchange={(e) => toggleWindow(e.currentTarget.checked)} /><span>{t('backup.window')}</span></label>
				{#if windowOn}
					<div class="row">
						<label class="tiny"><span>{t('backup.windowFrom')}</span><input class="field mono" type="time" bind:value={draft.windowFrom} required /></label>
						<label class="tiny"><span>{t('backup.windowTo')}</span><input class="field mono" type="time" bind:value={draft.windowTo} required /></label>
					</div>
					<p class="muted">{t('backup.windowHint')}</p>
				{/if}
				<p class="muted">{t('backup.catchUp')}</p>
			{/if}
		</fieldset>
		<fieldset class="divided">
			<legend>{t('backup.bandwidth')}</legend>
			<label class="check"><input type="checkbox" bind:checked={bandwidth.on} /><span>{t('backup.bandwidthLimit')}</span></label>
			{#if bandwidth.on}
				<div class="row">
					<label class="tiny"><span>{t('backup.bandwidthMax')}</span><input class="field mono" type="number" min="0" max={maxBandwidth} step="1" bind:value={bandwidth.limit} /></label>
				</div>
				<label class="check"><input type="checkbox" bind:checked={bandwidth.window} /><span>{t('backup.bandwidthWindow')}</span></label>
				{#if bandwidth.window}
					<div class="row">
						<label class="tiny"><span>{t('backup.windowFrom')}</span><input class="field mono" type="time" bind:value={bandwidth.windowFrom} required /></label>
						<label class="tiny"><span>{t('backup.windowTo')}</span><input class="field mono" type="time" bind:value={bandwidth.windowTo} required /></label>
						<label class="tiny"><span>{t('backup.bandwidthWindowLimit')}</span><input class="field mono" type="number" min="0" max={maxBandwidth} step="1" bind:value={bandwidth.windowLimit} /></label>
					</div>
				{/if}
				<p class="muted">{t(plainCopy(draft.destination.type) ? 'backup.bandwidthHintCopy' : 'backup.bandwidthHintRestic')}</p>
			{/if}
		</fieldset>
		{#if !plainCopy(draft.destination.type)}<fieldset class="divided">
			<legend>{t('backup.keeps')}</legend>
			<div class="row">
				<label class="tiny"><span>{t('backup.keepDaily')}</span><input class="field mono" type="number" min="0" max="999" bind:value={draft.keep.daily} /></label>
				<label class="tiny"><span>{t('backup.keepWeekly')}</span><input class="field mono" type="number" min="0" max="999" bind:value={draft.keep.weekly} /></label>
				<label class="tiny"><span>{t('backup.keepMonthly')}</span><input class="field mono" type="number" min="0" max="999" bind:value={draft.keep.monthly} /></label>
			</div>
			<p class="muted">{t('backup.keepHint')}</p>
		</fieldset>{/if}
		{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}
		<div class="row-actions">
			<button type="button" class="btn" onclick={() => ((draft = null), (error = null))} disabled={busy}>{t('common.cancel')}</button>
			<button type="submit" class="btn btn-primary" disabled={busy || (draft.sources.length === 0 && !draft.extras?.length && !draft.applications?.length)}>{isNew ? t('backup.create') : t('notify.save')}</button>
		</div>
	</form>
{/if}

{#if removing}
	<Dialog title={t('backup.removeTitle', { name: removing.id })} locked={busy} onclose={() => (removing = null)}>
		<p>{t('backup.removeText')}</p>
		{#if removeError}<p class="notice-error" role="alert">{removeError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (removing = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={remove}>{t('backup.remove')}</button>
		</div>
	</Dialog>
{/if}

{#if secret?.asking}
	<ConfirmDialog reason={t('backup.passwordReason')} onconfirmed={() => secret && showPassword(secret.job)} onclose={() => (secret = null)} />
{:else if secret}
	<Dialog title={t('backup.passwordTitle', { name: secret.job.id })} onclose={() => ((secret = null), (copiedPassword = false))}>
		<p>{t('backup.passwordText')}</p>
		<input class="field mono" type="text" readonly value={secret.password} onfocus={(event) => event.currentTarget.select()} aria-label={t('backup.passwordTitle', { name: secret.job.id })} />
		<p class="notice-warn">{t('backup.passwordWarn')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={copyPassword}>{copiedPassword ? t('backup.targetCopied') : t('backup.targetCopy')}</button>
			<button type="button" class="btn btn-primary" onclick={() => (secret = null)}>{t('backup.passwordDone')}</button>
		</div>
	</Dialog>
{/if}

{#if restoring}
	{@const r = restoring}
	<Dialog title={t('backup.restoreTitle', { name: r.job.id })} locked={busy} onclose={closeRestore}>
		{#if r.target}
			<p>{t('backup.restoreStarted')}</p>
			<p class="mono">{r.target}</p>
			<p class="muted">{t('backup.restoreWhere')}</p>
			<div class="dialog-actions"><button type="button" class="btn btn-primary" onclick={closeRestore}>{t('common.done')}</button></div>
		{:else if r.snapshots === null}
			<p class="muted">{t('backup.restoreLoading')}</p>
		{:else if r.snapshots.length === 0}
			<p class="muted">{t('backup.restoreNone')}</p>
			<div class="dialog-actions"><button type="button" class="btn" onclick={closeRestore}>{t('common.cancel')}</button></div>
		{:else}
			<form class="plain" onsubmit={restore}>
				<p class="muted">{t('backup.restoreText')}</p>
				<label><span>{t('backup.restoreWhich')}</span>
					<select class="field" bind:value={r.snapshot}>
						{#each r.snapshots as snapshot (snapshot.id)}<option value={snapshot.id}>{formatDateTime(new Date(snapshot.time))}{snapshot.bytes ? ` · ${formatBytes(snapshot.bytes)}` : ''}</option>{/each}
					</select>
				</label>
				<fieldset class="mode">
					<label class="check"><input type="radio" bind:group={r.mode} value="all" /><span>{t('backup.restoreAll')}</span></label>
					<label class="check"><input type="radio" bind:group={r.mode} value="files" /><span>{t('backup.restoreFiles')}</span></label>
				</fieldset>
				{#if r.mode === 'files'}
					<SnapshotFiles job={r.job.id} snapshot={r.snapshot} start={r.snapshots.find((s) => s.id === r.snapshot)?.paths?.[0] ?? '/'} bind:selected={r.paths} />
				{/if}
				<label><span>{t('backup.restoreVolume')}</span>
					<select class="field" bind:value={r.volume} required>
						{#each volumes as volume (volume)}<option value={volume}>{volume}</option>{/each}
					</select>
				</label>
				{#if volumes.length === 0}<p class="notice-warn">{t('backup.noVolume')}</p>{/if}
				{#if dialogError}<p class="notice-error" role="alert">{dialogError}</p>{/if}
				<div class="dialog-actions">
					<button type="button" class="btn" onclick={closeRestore} disabled={busy}>{t('common.cancel')}</button>
					<button type="submit" class="btn btn-primary" disabled={busy || !r.volume || (r.mode === 'files' && r.paths.length === 0)}>{t('backup.restoreDo')}</button>
				</div>
			</form>
		{/if}
	</Dialog>
{/if}

{#if checking}
	{@const c = checking}
	<Dialog title={t('backup.checkTitle', { name: c.job.id })} locked={busy} onclose={() => (checking = null)}>
		<form class="plain" onsubmit={check}>
			<p class="muted">{t('backup.checkText')}</p>
			<label><span>{t('backup.checkRead')}</span>
				<select class="field" bind:value={c.readData}>
					{#each [0, 5, 25, 100] as share}<option value={share}>{t(share === 0 ? 'backup.checkStructure' : 'backup.checkShare', { share })}</option>{/each}
				</select>
			</label>
			{#if dialogError}<p class="notice-error" role="alert">{dialogError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (checking = null)} disabled={busy}>{t('common.cancel')}</button>
				<button type="submit" class="btn btn-primary" disabled={busy}>{t('backup.checkDo')}</button>
			</div>
		</form>
	</Dialog>
{/if}

{#if probing}<ProbeDialog job={probing} onclose={()=>probing=''} onstarted={()=>{probing='';load();}}/>{/if}
<style>
	.mode { display: flex; flex-wrap: wrap; gap: 8px 20px; margin: 0; padding: 0; border: 0; }
	.mode .check { flex-direction: row; align-items: center; gap: 8px; }
	.targets { display: grid; gap: var(--gap); }
	section,
	form.tile {
		font-size: 0.875rem;
	}
	form.plain {
		display: flex;
		flex-direction: column;
		gap: 14px;
		font-size: 0.875rem;
	}
	p {
		margin: 0;
	}
	.head {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 12px;
	}
	.name {
		font-size: 1rem;
		font-weight: 600;
	}
	.head p {
		margin-top: 2px;
		overflow-wrap: anywhere;
	}
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: 12px 24px;
		margin: 0;
	}
	dt {
		color: var(--muted);
		font-size: 0.75rem;
	}
	dd {
		margin: 2px 0 0;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: 12px 20px;
	}
	fieldset {
		gap: 12px;
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
	.narrow {
		flex: 0 1 260px;
		max-width: 320px;
	}
	.tiny {
		flex: 0 1 120px;
		max-width: 140px;
	}
	.wide {
		flex: 1 1 360px;
		max-width: 560px;
	}
	label.check {
		flex-direction: row;
		align-items: center;
		gap: 8px;
		font-size: 0.875rem;
	}
	.check > span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.segments {
		display: inline-flex;
		flex-wrap: wrap;
		align-self: flex-start;
		gap: 4px;
		padding: 3px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segments button {
		height: 2.25rem;
		padding: 0 14px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.key {
		height: auto;
		padding: 10px 12px;
		font-size: 0.75rem;
		resize: none;
	}
	pre {
		margin: 6px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.days { display: flex; flex-wrap: wrap; gap: 0.25rem 1rem; }
	.inline { display: flex; align-items: center; gap: 0.4rem; }
	.source { margin-bottom: 0.6rem; }
	.legend-sub { margin: 0.8rem 0 0.3rem; font-weight: 600; }
	.block { display: grid; gap: 0.3rem; margin-top: 0.6rem; }
</style>
