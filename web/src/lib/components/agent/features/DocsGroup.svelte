<script lang="ts">
	// Gruppe „Dokumente und Fristen“ (PLAN 54-D, drafts 01 and 02): three
	// rows in the list of functions; the switch there is the NAS-wide one
	// (owners and administrators). A tap opens the settings of the account:
	// its folders, deadline mode, inbox folder and mailbox.
	import FeatureRow from './FeatureRow.svelte';
	import DocsSheet from './DocsSheet.svelte';
	import DocsFolderPicker from './DocsFolderPicker.svelte';
	import DocsViews from './DocsViews.svelte';
	import type { Features, FeaturesMine, FeaturesPublic } from '#lib/agent-features.ts';
	import {
		docsRun,
		docsStatus,
		paperMailTest,
		paperRun,
		place,
		type DocsFolder,
		type DocsMine,
		type DocsStatus,
		type FolderRef,
		type PaperMail,
		type PaperMine,
	} from '#lib/features/docs.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';

	type Props = {
		admin: boolean;
		nas: Features | null;
		pub: FeaturesPublic | null;
		mine: FeaturesMine | null;
		savenas: (next: Features) => Promise<void>;
		savemine: (next: FeaturesMine) => Promise<void>;
		calm?: boolean;
	};
	let { admin, nas, pub, mine, savenas, savemine, calm = false }: Props = $props();

	let page = $state<null | 'memory' | 'paper' | 'deadlines' | 'paperView'>(null);
	let status = $state<DocsStatus | null>(null);
	let error = $state('');
	let note = $state('');
	let picking = $state<null | 'folder' | 'target'>(null);

	const docs = $derived<DocsMine>(mine?.docs ?? {});
	const paper = $derived<PaperMine>(mine?.paper ?? {});
	const nd = $derived(nas?.docs ?? {});

	async function setNAS(part: 'enabled' | 'deadlines' | 'paper', on: boolean) {
		if (!nas) return;
		const next = { ...nas.docs, [part]: on };
		if (on) next.enabled = true;
		try {
			await savenas({ ...nas, docs: next });
		} catch {
			// The panel shows the error.
		}
	}
	async function saveDocs(next: DocsMine) {
		if (!mine) return;
		error = '';
		try {
			await savemine({ ...mine, docs: next });
			void refresh();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function savePaper(next: PaperMine) {
		if (!mine) return;
		error = '';
		try {
			await savemine({ ...mine, paper: next });
			void refresh();
		} catch (e) {
			error = apiErrorText(e);
			throw e;
		}
	}

	let timer: ReturnType<typeof setTimeout> | undefined;
	async function refresh() {
		clearTimeout(timer);
		if (!pub?.docs || (page !== 'memory' && page !== 'paper')) return;
		try {
			status = await docsStatus();
			if (status.running) timer = setTimeout(refresh, 2000);
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	$effect(() => {
		if (page === 'memory' || page === 'paper') void refresh();
		return () => clearTimeout(timer);
	});

	const folders = $derived<DocsFolder[]>(
		status?.folders ??
			(docs.custom ? (docs.folders ?? []) : [{ root: 'share/KI-Agent', path: 'Dokumente' }]).map(
				(f) => ({ ...f, files: 0, state: 'new' as const }),
			),
	);
	function removeFolder(f: FolderRef) {
		const list = (
			docs.custom ? (docs.folders ?? []) : [{ root: 'share/KI-Agent', path: 'Dokumente' }]
		).filter((x) => x.root !== f.root || x.path !== f.path);
		void saveDocs({ ...docs, custom: true, folders: list });
	}
	function addFolder(f: FolderRef) {
		picking = null;
		const list = docs.custom
			? (docs.folders ?? [])
			: [{ root: 'share/KI-Agent', path: 'Dokumente' }];
		if (list.some((x) => x.root === f.root && x.path === f.path)) return;
		void saveDocs({ ...docs, custom: true, folders: [...list, f] });
	}
	async function runNow() {
		note = '';
		try {
			await docsRun();
			setTimeout(refresh, 400);
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	const lead = $derived(docs.lead?.length ? docs.lead : [14, 2]);
	let leadText = $state('');
	$effect(() => {
		leadText = lead.join(', ');
	});
	function saveLead() {
		const list = leadText
			.split(/[,;\s]+/)
			.map((x) => Number(x))
			.filter((n) => Number.isInteger(n) && n >= 0 && n <= 365)
			.slice(0, 3);
		void saveDocs({ ...docs, lead: list });
	}
	const leadWords = $derived(
		lead.length === 2
			? t('agent.f.docs.leadTwo', { a: lead[0], b: lead[1] })
			: lead.map((n) => t('agent.f.docs.leadOne', { n })).join(', '),
	);

	// Mailbox form: saved with its own button; the password only when typed.
	let mail = $state<PaperMail>({});
	let password = $state('');
	$effect(() => {
		mail = { ...(paper.mail ?? {}) };
		password = '';
	});
	async function saveMail() {
		note = '';
		const next: PaperMail = { ...mail, port: Number(mail.port) || 0 };
		delete next.passwordSet;
		if (password) next.password = password;
		try {
			await savePaper({ ...paper, mail: next });
			note = t('agent.f.saved');
		} catch {
			// shown
		}
	}
	async function testMail() {
		note = '';
		error = '';
		try {
			await paperMailTest();
			note = t('agent.f.docs.mailOk');
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function lookNow() {
		note = '';
		try {
			await paperRun();
			note = t('agent.f.docs.paperLooking');
			setTimeout(refresh, 1500);
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	const tags: Record<string, { cls: string; key: MessageKey }> = {
		done: { cls: 'ok', key: 'agent.f.docs.state.done' },
		running: { cls: 'warn', key: 'agent.f.docs.state.running' },
		asleep: { cls: 'warn', key: 'agent.f.docs.state.asleep' },
		noAccess: { cls: 'bad', key: 'agent.f.docs.state.noAccess' },
		error: { cls: 'bad', key: 'agent.f.docs.state.error' },
	};
	const stateTag = (s: string) =>
		tags[s] ?? { cls: '', key: 'agent.f.docs.state.new' as MessageKey };
	function folderSub(f: DocsFolder): string {
		const parts = [t('agent.f.docs.files', { count: f.files })];
		if (f.state === 'running' && f.percent != null) parts.push(`${f.percent} %`);
		else if (f.lastRun)
			parts.push(t('agent.f.docs.lastRun', { at: formatDateTime(new Date(f.lastRun)) }));
		if (f.pending) parts.push(t('agent.f.docs.pending', { count: f.pending }));
		return parts.join(' · ');
	}
</script>

{#snippet sw(checked: boolean, label: string, onchange: (on: boolean) => void)}
	<label class="sw"
		><input
			type="checkbox"
			role="switch"
			{checked}
			aria-label={label}
			onchange={(e) => onchange(e.currentTarget.checked)}
		/><span aria-hidden="true"></span></label
	>
{/snippet}

<div class="fgrp">
	<FeatureRow
		icon="📄"
		color="#3b82f6"
		title={t('agent.f.docs.memory')}
		sub={t('agent.f.docs.memorySub')}
		on={admin ? !!nd.enabled : !!pub?.docs}
		toggle={admin}
		ontoggle={(on) => setNAS('enabled', on)}
		onopen={() => (page = 'memory')}
	/>
	<FeatureRow
		icon="⏰"
		color="#f59e0b"
		title={t('agent.f.docs.deadlines')}
		sub={docs.mode === 'remind'
			? t('agent.f.docs.deadlinesSubRemind')
			: t('agent.f.docs.deadlinesSub')}
		on={admin ? !!nd.enabled && !!nd.deadlines : !!pub?.deadlines}
		toggle={admin}
		ontoggle={(on) => setNAS('deadlines', on)}
		onopen={() => (page = 'memory')}
	/>
	<FeatureRow
		icon="🧾"
		color="#10b981"
		title={t('agent.f.docs.paper')}
		sub={paper.filing === 'ask' ? t('agent.f.docs.paperSubAsk') : t('agent.f.docs.paperSub')}
		on={admin ? !!nd.enabled && !!nd.paper : !!pub?.paper}
		toggle={admin}
		ontoggle={(on) => setNAS('paper', on)}
		onopen={() => (page = 'paper')}
	/>
</div>

{#if page === 'memory'}
	<DocsSheet title={t('agent.f.docs.memory')} {calm} onclose={() => (page = null)}>
		{#if !pub?.docs}
			<p class="muted">{t('agent.f.notThere')}</p>
		{:else}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dgrp">
				<div class="drow">
					<span class="dt"
						><strong>{t('agent.f.docs.memory')}</strong><small
							>{t('agent.f.docs.nightly', { at: status?.at ?? nd.at ?? '03:00' })}</small
						></span
					>
					{@render sw(!!docs.on, t('agent.f.docs.memory'), (on) => saveDocs({ ...docs, on }))}
				</div>
			</div>
			<h3 class="dh">{t('agent.f.docs.folders')}</h3>
			<div class="dgrp">
				{#each folders as f (f.root + '/' + f.path)}
					{@const tag = stateTag(f.state)}
					<div class="drow">
						<span class="dic" aria-hidden="true">📁</span>
						<span class="dt"><strong>{place(f)}</strong><small>{folderSub(f)}</small></span>
						<span class="tag {tag.cls}">{t(tag.key)}</span>
						<button
							type="button"
							class="x"
							aria-label={t('agent.f.docs.remove', { name: place(f) })}
							onclick={() => removeFolder(f)}>✕</button
						>
					</div>
				{/each}
				<div class="drow">
					<span class="dt"
						><button type="button" class="link" onclick={() => (picking = 'folder')}
							>+ {t('agent.f.docs.addFolder')}</button
						><small>{t('agent.f.docs.addHint')}</small></span
					>
				</div>
			</div>
			<div class="acts">
				<button type="button" class="btn" disabled={!docs.on || status?.running} onclick={runNow}
					>{status?.running ? t('agent.f.docs.state.running') : t('agent.f.docs.runNow')}</button
				>
			</div>
			{#if pub?.deadlines}
				<h3 class="dh">{t('agent.f.docs.deadlines')}</h3>
				<div class="dgrp">
					<div class="drow">
						<span class="dt"
							><strong>{t('agent.f.docs.findDeadlines')}</strong><small
								>{t('agent.f.docs.findHint')}</small
							></span
						>
						{@render sw(!!docs.deadlines, t('agent.f.docs.findDeadlines'), (on) =>
							saveDocs({ ...docs, deadlines: on }),
						)}
					</div>
					<div class="drow col">
						<strong>{t('agent.f.docs.found')}</strong>
						<div class="seg" role="group" aria-label={t('agent.f.docs.found')}>
							<button
								type="button"
								aria-pressed={docs.mode !== 'remind'}
								onclick={() => saveDocs({ ...docs, mode: 'suggest' })}
								>{t('agent.f.docs.modeSuggest')}</button
							>
							<button
								type="button"
								aria-pressed={docs.mode === 'remind'}
								onclick={() => saveDocs({ ...docs, mode: 'remind' })}
								>{t('agent.f.docs.modeRemind')}</button
							>
						</div>
					</div>
					<div class="drow col">
						<label class="dt" for="docs-lead"
							><strong>{t('agent.f.docs.remind')}</strong><small>{leadWords}</small></label
						>
						<input
							id="docs-lead"
							class="field"
							inputmode="numeric"
							bind:value={leadText}
							onchange={saveLead}
							aria-describedby="docs-lead-hint"
						/>
						<small id="docs-lead-hint" class="muted">{t('agent.f.docs.leadHint')}</small>
					</div>
					<div class="drow">
						<span class="dt"><strong>{t('agent.f.docs.suggestions')}</strong></span>
						<button type="button" class="btn" onclick={() => (page = 'deadlines')}
							>{t('agent.f.docs.show')}</button
						>
					</div>
				</div>
			{/if}
			<h3 class="dh">{t('agent.f.docs.ocr')}</h3>
			<div class="dgrp">
				<div class="drow">
					<span class="dt"
						><strong>{t('agent.f.docs.pdfText')}</strong><small
							>{t('agent.f.docs.pdfTextSub')}</small
						></span
					>
					<span class="tag ok">{t('agent.f.docs.always')}</span>
				</div>
				<div class="drow">
					<span class="dt"
						><strong>{t('agent.f.docs.scans')}</strong><small>{t('agent.f.docs.scansSub')}</small
						></span
					>
					{@render sw(!docs.noScans, t('agent.f.docs.scans'), (on) =>
						saveDocs({ ...docs, noScans: !on }),
					)}
				</div>
			</div>
		{/if}
	</DocsSheet>
{:else if page === 'paper'}
	<DocsSheet title={t('agent.f.docs.paper')} {calm} onclose={() => (page = null)}>
		{#if !pub?.paper}
			<p class="muted">{t('agent.f.notThere')}</p>
		{:else}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			{#if note}<p class="notice-ok" role="status">{note}</p>{/if}
			<div class="dgrp">
				<div class="drow">
					<span class="dt"
						><strong>{t('agent.f.docs.paper')}</strong><small>{t('agent.f.docs.paperOwn')}</small
						></span
					>
					{@render sw(!!paper.on, t('agent.f.docs.paper'), (on) =>
						savePaper({ ...paper, on }).catch(() => {}),
					)}
				</div>
			</div>
			<h3 class="dh">{t('agent.f.docs.inbox')}</h3>
			<div class="dgrp">
				<div class="drow">
					<span class="dic" aria-hidden="true">📥</span>
					<span class="dt"
						><strong>{status?.paper?.folder ?? 'KI-Agent › Eingang'}</strong><small
							>{t('agent.f.docs.inboxSub')}</small
						>
						{#if status?.paper?.ask || status?.paper?.waiting}
							<small
								>{t('agent.f.docs.inboxCounts', {
									ask: status.paper.ask,
									waiting: status.paper.waiting,
								})}</small
							>
						{/if}
					</span>
					<button type="button" class="btn" disabled={!paper.on} onclick={lookNow}
						>{t('agent.f.docs.lookNow')}</button
					>
				</div>
			</div>
			<h3 class="dh">{t('agent.f.docs.filing')}</h3>
			<div class="dgrp">
				<div class="drow col">
					<div class="seg" role="group" aria-label={t('agent.f.docs.filing')}>
						<button
							type="button"
							aria-pressed={paper.filing !== 'ask'}
							onclick={() => savePaper({ ...paper, filing: 'auto' }).catch(() => {})}
							>{t('agent.f.docs.filingAuto')}</button
						>
						<button
							type="button"
							aria-pressed={paper.filing === 'ask'}
							onclick={() => savePaper({ ...paper, filing: 'ask' }).catch(() => {})}
							>{t('agent.f.docs.filingAsk')}</button
						>
					</div>
					<small class="muted">{t('agent.f.docs.filingHint')}</small>
				</div>
				<div class="drow">
					<span class="dic" aria-hidden="true">🗂️</span>
					<span class="dt"
						><strong
							>{place(
								paper.target?.root
									? paper.target
									: { root: 'share/KI-Agent', path: 'Dokumente/Ablage' },
							)}</strong
						><small>{t('agent.f.docs.target')}</small></span
					>
					<button type="button" class="btn" onclick={() => (picking = 'target')}
						>{t('agent.f.docs.change')}</button
					>
				</div>
				<div class="drow col">
					<label for="docs-scheme"><strong>{t('agent.f.docs.scheme')}</strong></label>
					<select
						id="docs-scheme"
						class="field"
						value={paper.scheme || 'sender'}
						onchange={(e) =>
							savePaper({
								...paper,
								scheme: e.currentTarget.value as PaperMine['scheme'],
							}).catch(() => {})}
					>
						<option value="sender">{t('agent.f.docs.schemeSender')}</option>
						<option value="type">{t('agent.f.docs.schemeType')}</option>
						<option value="year">{t('agent.f.docs.schemeYear')}</option>
					</select>
				</div>
			</div>
			<h3 class="dh">{t('agent.f.docs.mailbox')}</h3>
			<div class="dgrp">
				<div class="drow">
					<span class="dt"
						><strong>{t('agent.f.docs.mailOn')}</strong><small>{t('agent.f.docs.mailSub')}</small
						></span
					>
					{@render sw(!!mail.enabled, t('agent.f.docs.mailOn'), (on) => (mail.enabled = on))}
				</div>
				<div class="drow col">
					<label for="docs-host">{t('agent.f.docs.host')}</label>
					<input
						id="docs-host"
						class="field"
						autocomplete="off"
						placeholder="imap.example.org"
						bind:value={mail.host}
					/>
					<label for="docs-port">{t('agent.f.docs.port')}</label>
					<input
						id="docs-port"
						class="field"
						inputmode="numeric"
						placeholder={mail.security === 'starttls' ? '143' : '993'}
						bind:value={mail.port}
					/>
					<span>{t('agent.f.docs.security')}</span>
					<div class="seg" role="group" aria-label={t('agent.f.docs.security')}>
						<button
							type="button"
							aria-pressed={mail.security !== 'starttls'}
							onclick={() => (mail.security = 'tls')}>TLS</button
						>
						<button
							type="button"
							aria-pressed={mail.security === 'starttls'}
							onclick={() => (mail.security = 'starttls')}>STARTTLS</button
						>
					</div>
					<label for="docs-user">{t('agent.f.docs.mailUser')}</label>
					<input id="docs-user" class="field" autocomplete="off" bind:value={mail.user} />
					<label for="docs-pw">{t('agent.f.docs.password')}</label>
					<input
						id="docs-pw"
						class="field"
						type="password"
						autocomplete="new-password"
						placeholder={mail.passwordSet ? t('agent.f.docs.passwordSet') : ''}
						bind:value={password}
					/>
					<span>{t('agent.f.docs.mailTake')}</span>
					<div class="seg" role="group" aria-label={t('agent.f.docs.mailTake')}>
						<button
							type="button"
							aria-pressed={mail.mode !== 'attachments'}
							onclick={() => (mail.mode = 'both')}>{t('agent.f.docs.mailBoth')}</button
						>
						<button
							type="button"
							aria-pressed={mail.mode === 'attachments'}
							onclick={() => (mail.mode = 'attachments')}>{t('agent.f.docs.mailAttach')}</button
						>
					</div>
					<small class="muted">{t('agent.f.docs.mailHint')}</small>
					{#if status?.paper?.mailError}
						<p class="notice-error" role="alert">
							{t('agent.f.docs.mailFailed', { error: status.paper.mailError })}
						</p>
					{:else if status?.paper?.mailAt}
						<small class="muted"
							>{t('agent.f.docs.mailAt', {
								at: formatDateTime(new Date(status.paper.mailAt)),
							})}</small
						>
					{/if}
					<div class="acts">
						<button type="button" class="btn" disabled={!mail.passwordSet} onclick={testMail}
							>{t('agent.f.docs.mailTest')}</button
						>
						<button type="button" class="btn btn-primary" onclick={saveMail}
							>{t('common.save')}</button
						>
					</div>
				</div>
			</div>
			<div class="acts">
				<button type="button" class="btn btn-primary" onclick={() => (page = 'paperView')}
					>{t('agent.f.docs.openPaper')}</button
				>
			</div>
		{/if}
	</DocsSheet>
{:else if page === 'deadlines' || page === 'paperView'}
	<DocsViews
		id={page === 'deadlines' ? 'deadlines' : 'paper'}
		{calm}
		onclose={() => (page = page === 'deadlines' ? 'memory' : 'paper')}
	/>
{/if}

{#if picking}
	<DocsFolderPicker
		title={picking === 'folder' ? t('agent.f.docs.addFolder') : t('agent.f.docs.target')}
		writable={picking === 'target'}
		onclose={() => (picking = null)}
		onpick={(f) =>
			picking === 'folder'
				? addFolder(f)
				: ((picking = null), savePaper({ ...paper, target: f }).catch(() => {}))}
	/>
{/if}

<style>
	.acts {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		justify-content: flex-end;
	}
	.x {
		flex: none;
		width: 32px;
		height: 32px;
		border-radius: 50%;
		border: 1px solid var(--border);
		background: none;
		color: var(--muted);
		cursor: pointer;
	}
	.sw {
		position: relative;
		flex-shrink: 0;
		width: 44px;
		height: 26px;
	}
	.sw input {
		position: absolute;
		inset: 0;
		opacity: 0;
		margin: 0;
		cursor: pointer;
	}
	.sw span {
		position: absolute;
		inset: 0;
		border-radius: 13px;
		background: color-mix(in srgb, var(--text) 18%, transparent);
		pointer-events: none;
	}
	.sw span::after {
		content: '';
		position: absolute;
		top: 3px;
		left: 3px;
		width: 20px;
		height: 20px;
		border-radius: 50%;
		background: #fff;
		transition: left 0.15s;
	}
	.sw input:checked + span {
		background: var(--accent);
	}
	.sw input:checked + span::after {
		left: 21px;
	}
	.sw input:focus-visible + span {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
</style>
