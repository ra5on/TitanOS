<script lang="ts">
	import './personal.css';
	import { onMount, onDestroy, tick } from 'svelte';
	import { api } from '#lib/api.ts';
	import DocsFolderPicker from '#lib/components/agent/features/DocsFolderPicker.svelte';
	import type { FolderRef } from '#lib/features/docs.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { t, tOr, apiErrorText } from '#lib/i18n/index.svelte.ts';
	import { mailApi } from '#lib/tmail/api.ts';
	import type { MailAccount } from '#lib/tmail/types.ts';
	import {
		agentState,
		createJob,
		changeJob,
		requestId,
		runSkill,
		type Action,
		type State,
		type Source,
		type Job,
		type JobRequest,
	} from './api.ts';
	import { openInProgram } from '#lib/agent/open.ts';
	import { colorStyle } from './appearance.ts';
	import JobDetail from './JobDetail.svelte';
	import MotionGuide from './MotionGuide.svelte';
	import { motifFor, type Cue } from './guide.ts';
	import { sourceLocation, stepTool } from './sources.ts';
	import { samePIMSource } from './pim.ts';
	let {
		app,
		sources,
		phone,
		onclose,
	}: {
		app: 'mail' | 'calendar' | 'contacts';
		sources: Source[];
		phone: boolean;
		onclose: () => void;
	} = $props();
	let composing = $state(false);
	let surface = $state<HTMLElement>();
	let accountState = $state<State | null>(null),
		job = $state<Job | null>(null),
		busy = $state(false),
		error = $state(''),
		choice = $state(''),
		thread = $state(true),
		prompt = $state(''),
		selected = $state<string[]>([]);
	let title = $state(''),
		start = $state(''),
		end = $state(''),
		email = $state(''),
		when = $state(''),
		mailboxes = $state<MailAccount[]>([]),
		box = $state(''),
		collection = $state(''),
		targets = $state<{ id: string; name: string }[]>([]);
	let cue = $state<Cue | null>(null),
		reduced = $state(true),
		live = true,
		lastKey = '',
		retryId = '',
		serial = 0;
	const source = $derived(sources[0]),
		caps = $derived(
			(accountState?.capabilities ?? []).filter(
				(c) =>
					c.effective !== 'never' &&
					(c.app === app || (app === 'calendar' && c.app === 'reminders')),
			),
		);
	const primeIds = ['mail.summarize', 'mail.reply', 'mail.event'];
	const options = $derived(
		caps.filter(
			(c) =>
				c.id.endsWith('.create') ||
				c.id === 'calendar.free' ||
				(source && c.app === source.kind && c.id !== 'mail.inbox') ||
				(source?.kind === 'mail.folder' && c.id === 'mail.inbox'),
		),
	);
	const prime = $derived(primeIds.map((k) => options.find((c) => c.id === k)).filter((c) => !!c));
	const allCaps = $derived(accountState?.capabilities ?? []);
	const delivery = $derived(
		source?.kind === 'mail' &&
			['mail.event', 'calendar.free', 'mail.reply', 'mail.send'].every((id) =>
				allCaps.some((c) => c.id === id && c.effective !== 'never'),
			),
	);
	const documentFlow = $derived(
		source?.kind === 'mail' &&
			['mail.event', 'calendar.free', 'office.document'].every((id) =>
				allCaps.some((c) => c.id === id && c.effective !== 'never'),
			),
	);
	let pickingFolder = $state(false),
		outputFolder = $state<FolderRef | null>(null),
		outputName = $state('Liefertermin.docx');
	const extra = $derived(options.filter((c) => !primeIds.includes(c.id)));
	const tool = $derived(
		choice === 'delivery' && delivery
			? { id: 'delivery', app: 'mail', title: t('papim.delivery') }
			: choice === 'delivery-document' && documentFlow
				? { id: 'delivery-document', app: 'mail', title: t('papim.docFlow') }
				: (options.find((c) => c.id === choice) ?? prime[0] ?? options[0]),
	);
	const create = $derived(!!tool?.id.endsWith('.create')),
		range = $derived(
			tool?.id === 'calendar.free' || tool?.id === 'delivery-document' || tool?.id === 'delivery',
		),
		invite = $derived(tool?.id === 'calendar.invite' || tool?.id === 'calendar.cancelInvite'),
		schedule = $derived(tool?.id === 'mail.followup');
	function openSource(s: Source) {
		if (s.kind === 'mail.folder' && s.ref) {
			openInProgram('/mail?' + new URLSearchParams({ a: s.ref.account, f: s.ref.folder }));
			return;
		}
		const target = sourceLocation(s);
		if (!target) return;
		if (target.pdf)
			window.open(target.href + '#page=' + target.page, '_blank', 'noopener,noreferrer');
		else openInProgram(target.href);
	}

	function motion(stops: string[], id: string) {
		cue = { id: ++serial, stops, motif: motifFor(id) };
	}
	onDestroy(() => {
		live = false;
	});
	onMount(() => {
		const q = matchMedia('(prefers-reduced-motion: reduce)');
		const changed = () => (reduced = q.matches);
		changed();
		q.addEventListener('change', changed);
		async function load() {
			try {
				const got = await agentState();
				if (!live) return;
				accountState = got;
				if (source)
					job =
						got.jobs
							.filter((j) => samePIMSource(j.request.sources?.[0], source))
							.sort((a, b) => b.updated.localeCompare(a.updated))[0] ?? null;
			} catch (e) {
				if (live) error = apiErrorText(e);
			}
		}
		void load();
		return () => q.removeEventListener('change', changed);
	});
	$effect(() => {
		const id = job?.id,
			status = job?.status;
		if (!id || status === 'done' || status === 'failed' || status === 'paused') return;
		let gone = false,
			inFlight = false;
		const timer = setInterval(async () => {
			if (gone || inFlight || document.hidden) return;
			inFlight = true;
			try {
				const got = await api.get<Job>('/personal-agent/jobs/' + encodeURIComponent(id));
				if (gone || !live) return;
				if (got.status === 'done' && job?.status !== 'done')
					motion(['source', 'detail'], stepTool(got));
				job = got;
			} catch (e) {
				if (!gone && live) error = apiErrorText(e);
			} finally {
				inFlight = false;
			}
		}, 1800);
		return () => {
			gone = true;
			clearInterval(timer);
		};
	});
	$effect(() => {
		if (!invite) return;
		let gone = false;
		void mailApi
			.accounts()
			.then((list) => {
				if (gone) return;
				mailboxes = list;
				box = list[0]?.id ?? '';
			})
			.catch((e) => {
				if (!gone) error = apiErrorText(e);
			});
		return () => {
			gone = true;
		};
	});
	$effect(() => {
		if ((!create && !range) || !tool) return;
		const scope = range ? 'calendar' : tool.app,
			endpoint =
				scope === 'calendar'
					? '/calendar/calendars'
					: scope === 'contacts'
						? '/contacts/overview'
						: '/calendar/tasklists';
		let gone = false;
		targets = [];
		collection = '';
		void api
			.get<
				| { id: string; name: string; kind?: string; readOnly?: boolean }[]
				| { books: { id: string; name: string }[] }
			>(endpoint)
			.then((raw) => {
				if (gone) return;
				const rows = Array.isArray(raw) ? raw : (raw.books ?? []);
				targets = rows.filter((c) => !('readOnly' in c) || !c.readOnly);
				collection = targets[0]?.id ?? '';
			})
			.catch((e) => {
				if (!gone) error = apiErrorText(e);
			});
		return () => {
			gone = true;
		};
	});
	function selectPart(id: string) {
		selected = selected.includes(id) ? selected.filter((k) => k !== id) : [...selected, id];
	}
	async function submit() {
		if (!tool || busy) return;
		busy = true;
		error = '';
		try {
			let args: Record<string, unknown> = {},
				runAt: string | undefined;
			let refs = create ? [] : sources.filter((s, i) => i === 0 || selected.includes(s.id));
			if (
				thread &&
				refs[0]?.kind === 'mail' &&
				['mail.summarize', 'mail.reply', 'mail.event', 'delivery', 'delivery-document'].includes(
					tool.id,
				)
			)
				refs = [{ ...refs[0], ref: { ...refs[0].ref, scope: 'thread' } }, ...refs.slice(1)];
			if (create) {
				args = { title, collection, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone };
				if (tool.app === 'calendar') Object.assign(args, { start, end });
				if (tool.app === 'reminders' && when) args.due = when;
				if (tool.app === 'contacts') args.email = email;
			}
			if (range) {
				const cal = {
					kind: 'calendar.range',
					id: collection,
					label: targets.find((t) => t.id === collection)?.name,
					ref: {
						collection,
						start: new Date(start).toISOString(),
						end: new Date(end).toISOString(),
					},
				};
				refs = tool.id === 'calendar.free' ? [cal] : [...refs, cal];
			}
			if (tool.id === 'delivery-document') {
				if (!outputFolder || !outputName.trim()) throw new Error(t('papim.pickTarget'));
				refs = [
					...refs,
					{
						kind: 'folder',
						id: outputFolder.path || '.',
						ref: { root: outputFolder.root },
						label: outputFolder.path || outputFolder.root,
					},
				];
				args = { name: outputName };
			}
			if (invite) {
				args = { account: box, to: email };
			}
			if (schedule) {
				if (!when || !(Date.parse(when) > Date.now())) throw new Error(t('papim.future'));
				runAt = new Date(when).toISOString();
				refs = [source];
			}
			if (tool.id === 'mail.send') {
				args = { text: prompt };
				refs = [source];
			}
			const input: Omit<JobRequest, 'requestId'> = {
				tool: tool.id,
				args,
				sources: refs,
				prompt,
				runAt,
			};
			if (refs.length && samePIMSource(job?.request.sources?.[0], refs[0]))
				input.chatId = job?.request.chatId;
			const key = JSON.stringify(input);
			if (key !== lastKey || !retryId) {
				lastKey = key;
				retryId = requestId();
			}
			const got = ['delivery', 'delivery-document'].includes(tool.id)
				? await runSkill(tool.id, {
						args: input.args,
						sources: input.sources,
						prompt: input.prompt,
						chatId: input.chatId,
						requestId: retryId,
					})
				: await createJob({ ...input, requestId: retryId });
			if (!live) return;
			job = got;
			composing = false;
			await tick();
			surface?.scrollTo({ top: 0 });
			retryId = '';
			motion(['submit', 'source', 'detail'], tool.id);
		} catch (e) {
			if (live)
				error =
					e instanceof Error && e.message === t('papim.future')
						? e.message
						: apiErrorText(e, { detail: true });
		} finally {
			if (live) busy = false;
		}
	}
	async function act(a: Action, j: Job) {
		if (busy) return;
		busy = true;
		error = '';
		try {
			const got = await changeJob(j.id, a, j.token);
			if (live) {
				job = got;
				motion([a === 'approve' ? 'lock' : 'detail', 'detail'], stepTool(j));
			}
		} catch (e) {
			if (live) error = apiErrorText(e, { detail: true });
		} finally {
			if (live) busy = false;
		}
	}
</script>

<section
	bind:this={surface}
	class="pim-agent pa-ui"
	data-theme-choice={accountState?.settings.theme ?? 'system'}
	class:phone
	style={colorStyle(accountState?.settings)}
	aria-label={t('agentpim.panel.title')}
>
	<header>
		<strong><Icon name="sparkle" size={18} /> {t('agentpim.panel.title')}</strong><button
			class="icon"
			title={t('papim.workspace')}
			aria-label={t('papim.workspace')}
			onclick={() => openInProgram(job ? '/agent?job=' + encodeURIComponent(job.id) : '/agent')}
			><Icon name="external" size={18} /></button
		><button class="icon" aria-label={t('common.close')} onclick={onclose}
			><Icon name="close" size={18} /></button
		>
	</header>
	{#if source?.kind === 'mail'}<small>{t('papim.conversation')}</small>{/if}
	{#if source}<p class="source" data-guide="source">
			<Icon
				name={app === 'mail' ? 'mail' : app === 'calendar' ? 'calendar' : 'users'}
				size={17}
			/><button type="button" class="source-link" onclick={() => openSource(source)}
				>{source.label || source.id}</button
			>
		</p>{:else if app === 'mail'}<p>{t('papim.pickMail')}</p>{/if}
	{#if options.length && tool && (!job || composing)}
		<form
			onsubmit={(e) => {
				e.preventDefault();
				void submit();
			}}
		>
			{#if prime.length}
				<div class="choices" aria-label={t('papim.action')}>
					{#each prime as c (c.id)}<button
							type="button"
							aria-pressed={tool.id === c.id}
							onclick={() => (choice = c.id)}>{tOr('papim.tool.' + c.id, c.title)}</button
						>{/each}
				</div>
			{/if}
			{#if extra.length || delivery || documentFlow}<label
					>{t(prime.length ? 'papim.more' : 'papim.action')}
					<select
						value={primeIds.includes(tool.id) ? '' : tool.id}
						onchange={(e) => (choice = e.currentTarget.value)}
					>
						{#if prime.length}<option value="">{t('papim.more')}</option>{/if}
						{#each extra as c (c.id)}<option value={c.id}
								>{tOr('papim.tool.' + c.id, c.title)}</option
							>{/each}
						{#if delivery}<option value="delivery">{t('papim.delivery')}</option>{/if}
						{#if documentFlow}<option value="delivery-document">{t('papim.docFlow')}</option>{/if}
					</select></label
				>{/if}
			{#if create || range}<label
					>{t('papim.target')}<select bind:value={collection} required
						>{#each targets as c (c.id)}<option value={c.id}>{c.name}</option>{/each}</select
					></label
				>
				{#if create}<label
						>{t('papim.title')}<input bind:value={title} required maxlength="512" /></label
					>{/if}
				{#if range || tool.app === 'calendar'}<label
						>{t('papim.start')}<input type="datetime-local" bind:value={start} required /></label
					><label>{t('papim.end')}<input type="datetime-local" bind:value={end} required /></label
					>{/if}
			{/if}
			{#if tool.id === 'delivery-document'}
				<label>{t('papim.filename')}<input required bind:value={outputName} /></label>
				<button class="btn" type="button" onclick={() => (pickingFolder = true)}
					>{outputFolder
						? outputFolder.root + ' / ' + outputFolder.path
						: t('papim.pickTarget')}</button
				>
			{/if}

			{#if invite}<label
					>{t('papim.from')}<select bind:value={box} required
						>{#each mailboxes as b (b.id)}<option value={b.id}>{b.email}</option>{/each}</select
					></label
				>{/if}
			{#if invite || tool.id === 'contacts.create'}<label
					>{t('papim.to')}<input type="email" bind:value={email} required={invite} /></label
				>{/if}
			{#if schedule || tool.id === 'reminders.create'}<label
					>{t('papim.when')}<input
						type="datetime-local"
						bind:value={when}
						required={schedule}
					/></label
				>{/if}
			{#if ['mail.summarize', 'mail.reply', 'mail.event', 'mail.send', 'delivery', 'delivery-document'].includes(tool.id)}
				<label
					>{t(tool.id === 'mail.send' ? 'papim.body' : 'papim.prompt')}<textarea
						bind:value={prompt}
						rows="2"
						required={tool.id === 'mail.send'}
						maxlength="12000"></textarea></label
				>
				{#if tool.id !== 'mail.send'}<label class="check"
						><input type="checkbox" bind:checked={thread} />{t('papim.withThread')}</label
					>{/if}
				{#if tool.id !== 'mail.send' && sources.length > 1}<fieldset>
						<legend>{t('papim.pdf')}</legend>{#each sources.slice(1) as file (file.id)}<label
								class="check"
								><input
									type="checkbox"
									checked={selected.includes(file.id)}
									onchange={() => selectPart(file.id)}
								/><span>{file.label}</span></label
							>{/each}
					</fieldset>{/if}
			{/if}
			<button
				class="btn primary"
				disabled={busy ||
					(create && !collection) ||
					(tool.id === 'delivery-document' && !outputFolder)}
				data-guide="submit">{t('papim.submit')}</button
			>
		</form>
	{:else if accountState && (!source || caps.length === 0) && app !== 'mail'}<p>
			{t('papim.noAction')}
		</p>{/if}
	{#if error}<p role="alert">{error}</p>{/if}
	{#if job}<JobDetail
			{job}
			caps={accountState?.capabilities ?? []}
			pending={busy}
			onaction={act}
			oncue={motion}
			onreview={(j) => openInProgram('/agent?job=' + encodeURIComponent(j.id))}
			onsource={openSource}
			onsettings={() => openInProgram('/agent?view=settings')}
		/>{/if}
	{#if job && !composing && options.length}<button
			type="button"
			class="btn"
			onclick={() => (composing = true)}>{t('papim.another')}</button
		>{/if}
	<MotionGuide {cue} mode={accountState?.settings.motion ?? 'off'} {reduced} />
</section>
{#if pickingFolder}<DocsFolderPicker
		title={t('papim.pickTarget')}
		writable
		onpick={(f) => {
			outputFolder = f;
			pickingFolder = false;
		}}
		onclose={() => (pickingFolder = false)}
	/>{/if}

<style>
	.choices {
		display: flex;
		flex-wrap: wrap;
		gap: 0.45rem;
	}
	.choices button,
	.source-link {
		font: inherit;
		color: inherit;
		background: transparent;
		border: 1px solid var(--border);
		border-radius: 0.65rem;
		padding: 0.5rem 0.65rem;
		min-height: 42px;
	}
	.choices button[aria-pressed='true'] {
		border-color: var(--pa-accent, var(--accent));
	}
	.source-link {
		border: 0;
		text-decoration: underline;
		text-align: left;
		overflow-wrap: anywhere;
		padding: 0;
	}

	.pim-agent {
		box-sizing: border-box;
		position: relative;
		display: flex;
		flex-direction: column;
		align-items: stretch;
		gap: 0.85rem;
		padding: 1rem;
		overflow: auto;
		height: 100%;
		min-height: 0;
		font-size: 0.94rem;
		color: var(--pa-text, var(--text));
		background: var(--pa-background, var(--panel));
	}
	.pim-agent.phone {
		max-height: 71dvh;
	}
	.pim-agent header {
		display: flex;
		align-items: center;
		gap: 0.25rem;
	}
	.pim-agent header strong {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		margin-right: auto;
		font-size: 1.15rem;
	}
	.pim-agent .icon {
		width: 2.8rem;
		height: 2.8rem;
		display: grid;
		place-items: center;
		background: transparent;
		border: 0;
		color: inherit;
		border-radius: 0.7rem;
	}
	.pim-agent p {
		margin: 0;
	}
	.source {
		display: flex;
		gap: 0.35rem;
		align-items: center;
		overflow-wrap: anywhere;
	}
	.pim-agent form {
		display: grid;
		gap: 0.65rem;
	}
	.pim-agent label:not(.check) {
		display: grid;
		gap: 0.25rem;
	}
	.pim-agent .check {
		display: flex;
		gap: 0.45rem;
		align-items: center;
		min-height: 2.6rem;
	}
	.pim-agent .check span {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.pim-agent fieldset {
		min-width: 0;
		border: 1px solid var(--border);
		border-radius: 0.65rem;
	}
	.pim-agent select,
	.pim-agent input:not([type='checkbox']),
	.pim-agent textarea {
		font: inherit;
		min-width: 0;
		box-sizing: border-box;
		width: 100%;
		border: 1px solid var(--border);
		border-radius: 0.6rem;
		padding: 0.6rem;
		color: inherit;
		background: var(--surface);
	}
	.pim-agent :global(.pa-detail) {
		padding: 0.85rem;
		border: 1px solid var(--border);
		border-radius: 0.85rem;
		background: var(--pa-card, var(--surface));
		overflow-wrap: anywhere;
	}
	.pim-agent :global(.pa-detail h2) {
		font-size: 1.04rem;
	}
	.pim-agent :global(.pa-readable) {
		white-space: pre-wrap;
		line-height: 1.45;
	}
	.pim-agent :global(.pa-detail button) {
		font: inherit;
		color: inherit;
		min-height: 2.65rem;
		border: 1px solid var(--border);
		border-radius: 0.6rem;
		background: var(--surface);
		margin: 0.25rem;
	}
	.pim-agent :global(.pa-verified svg) {
		width: 1rem;
		height: 1rem;
		fill: none;
		stroke: currentColor;
		stroke-width: 2;
	}
	.pim-agent :global(.pa-detail pre) {
		white-space: pre-wrap;
	}
	.pim-agent :global(.pa-status),
	.pim-agent :global(.pa-kicker) {
		display: flex;
		align-items: center;
		gap: 0.3rem;
	}
</style>
