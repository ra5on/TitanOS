<script lang="ts">
	import Icon from '#lib/components/Icon.svelte';
	import { openInProgram } from '#lib/agent/open.ts';
	import { webAppById, loadWebApps } from '#lib/webapps/webapps.svelte.ts';
	import { openInWebApp } from '#lib/webapps/open.ts';
	import { msg, iconFor, sourceTitle, jobTitle, outputKind, failure } from './labels.ts';
	import { nativeDraftLink, resultLink } from './open.ts';
	import { jobOutput, mailFields, sendPreview, sourceLocation, stepTool } from './sources.ts';
	import { depart } from './guide.ts';
	import type { Source, Job, Action, Capability } from './api.ts';
	let {
		job,
		caps,
		pending = false,
		onreview,
		onsource,
		onsettings,
		onaction,
		oncue,
		first = false,
	}: {
		job: Job;
		caps: Capability[];
		pending?: boolean;
		onreview?: (j: Job) => void;
		onsource?: (src: Source, j: Job) => void;
		onsettings?: () => void;
		onaction: (a: Action, job: Job) => void;
		oncue: (stops: string[], tool: string) => void;
		first?: boolean;
	} = $props();
	const mail = $derived(mailFields(job));
	const detail = $derived(jobOutput(job));
	const fields = $derived(
		detail?.data && typeof detail.data === 'object' && !Array.isArray(detail.data)
			? (detail.data as Record<string, unknown>)
			: {},
	);
	const before = $derived(typeof fields.before === 'string' ? fields.before : null),
		after = $derived(typeof fields.after === 'string' ? fields.after : null);
	const sources = $derived(detail?.sources?.length ? detail.sources : (job.request.sources ?? []));
	const doc = $derived(
		fields.document as { app?: unknown; root?: unknown; path?: unknown } | undefined,
	);
	const office = $derived(
		doc &&
			typeof doc.app === 'string' &&
			['t-word', 't-exel', 't-pdf', 't-deck'].includes(doc.app) &&
			typeof doc.root === 'string' &&
			typeof doc.path === 'string'
			? { app: doc.app, root: doc.root, path: doc.path }
			: null,
	);
	const verified = $derived(
		job.status === 'done' &&
			(sendPreview(job) ? fields.accepted === true : !!job.result && !fields.draft),
	);

	let article = $state<HTMLElement>();
	let opening = $state(false),
		confirmCancel = $state(false);
	function fieldValue(item: unknown): string {
		if (Array.isArray(item)) return item.map(fieldValue).join('\n');
		if (item && typeof item === 'object')
			return Object.entries(item)
				.map(([key, v]) => key + ' · ' + fieldValue(v))
				.join('\n');
		return item == null ? '' : String(item);
	}
	async function openResult() {
		if (opening) return;
		opening = true;
		try {
			oncue(['detail', 'native'], stepTool(job));
			await depart(article);
			if (!article?.isConnected) return;
			if (office) {
				await loadWebApps();
				if (webAppById(office.app)) {
					openInWebApp('webapp:' + office.app, office.root, office.path);
					return;
				}
			}
			const href = resultLink(job);
			if (href) openInProgram(href);
		} finally {
			opening = false;
		}
	}
</script>

<article bind:this={article} class="pa-detail" data-guide="detail" data-job={job.id}>
	<div class="pa-kicker">
		<Icon
			name={job.status === 'waiting' ? 'lock' : verified ? 'check' : iconFor(stepTool(job))}
		/>{outputKind(job)}
		{#if first && job.status === 'done' && job.result}<span class="pa-new">{msg('newResult')}</span
			>{/if}
	</div>
	<h2>{mail?.isDraft ? msg('mailDraft') : jobTitle(job, caps)}</h2>
	<p class="pa-status">
		<Icon name={job.status === 'failed' ? 'info' : 'clock'} />{msg(job.status)}
	</p>
	{#if mail}<p class="pa-status">
			<Icon name="lock" />{msg(
				verified
					? 'smtpAccepted'
					: job.phase === 'uncertain' || job.error === 'effect_unconfirmed'
						? 'uncertain'
						: 'unsent',
			)}
		</p>
		<dl class="pa-mail-fields">
			{#each [['account', mail.account], ['from', mail.from], ['to', mail.to], ['CC', mail.cc], ['BCC', mail.bcc], ['subject', mail.subject], ['attachments', mail.attachments || msg('noAttachments')]] as pair}{#if pair[1]}<div
					>
						<dt>{pair[0] === 'CC' || pair[0] === 'BCC' ? pair[0] : msg(pair[0])}</dt>
						<dd>{pair[1]}</dd>
					</div>{/if}{/each}
		</dl>
		<div class="pa-readable">{mail.text}</div>
	{:else if detail?.text}<div class="pa-readable">{detail.text}</div>{/if}
	{#if job.request.tool !== 'office.inspect' && (before !== null || after !== null)}
		<section class:pa-highlight={first} class="pa-diff" data-guide="region">
			<h3>{typeof fields.region === 'string' ? fields.region : msg('whatChanged')}</h3>
			{#if before !== null}<strong>{msg('before')}</strong>
				<pre>{before}</pre>{/if}
			{#if after !== null}<strong>{msg('after')}</strong>
				<pre>{after}</pre>{/if}
		</section>
	{/if}
	{#if !mail && job.status !== 'done' && job.preview && Object.keys(fields).some((k) => !['region', 'before', 'after'].includes(k))}
		<dl class="pa-fields">
			{#each Object.entries(fields).filter(([key]) => !['region', 'before', 'after'].includes(key)) as [key, value] (key)}
				<div>
					<dt>{key}</dt>
					<dd>{fieldValue(value)}</dd>
				</div>
			{/each}
		</dl>
	{/if}
	{#if sources.length}<h3>{msg('sourcesUsed')}</h3>
		<ul class="pa-source-list">
			{#each sources as source, index}<li data-guide={'evidence-' + index}>
					<button
						class="pa-link"
						disabled={!onsource || !sourceLocation(source)}
						onclick={() => onsource?.(source, job)}
						><Icon name={iconFor(source.kind)} />{sourceTitle(source)}</button
					>
					{#if typeof source.ref?.region === 'string'}<small>{source.ref.region}</small>{/if}
				</li>{/each}
		</ul>{/if}
	{#if verified && fields.sentCopy === false && !fields.providerFilesSent}<p class="pa-warning">
			<Icon name="info" />{msg('sentCopyFailed')}
		</p>{/if}
	{#if job.error && job.phase !== 'uncertain'}<p class="pa-warning" role="status">
			<Icon name="info" />{failure(job)}
		</p>{/if}
	{#if job.phase === 'uncertain'}<p class="pa-warning">
			<Icon name="info" />{msg('uncertain')}
		</p>{/if}
	{#if job.status === 'waiting' && job.token && job.preview}
		<div class="pa-approval" data-guide="lock">
			<strong><span class="pa-lock"><Icon name="lock" /></span>{msg('needs')}</strong>
			{#if sendPreview(job) && job.phase === 'approval' && fields.inReplyTo && fields.account}
				<button
					class="pa-link"
					onclick={async () => {
						oncue(['detail', 'native'], 'mail.reply');
						openInProgram('/mail/compose?personalReview=' + encodeURIComponent(job.id));
					}}>{msg('editDraft')}</button
				>
			{/if}
			<button class="pa-primary" disabled={pending} onclick={() => onaction('approve', job)}
				>{msg(
					job.phase === 'permission' ? 'permission' : sendPreview(job) ? 'confirmSend' : 'approve',
				)}</button
			>
		</div>
	{/if}
	{#if mail?.isDraft && job.status === 'done' && (onreview || onsettings)}<div class="pa-approval">
			<strong><Icon name="lock" />{msg('unsent')}</strong>
			{#if onreview && caps.some((c) => c.id === 'mail.send' && c.effective !== 'never')}<button
					class="pa-primary"
					disabled={pending}
					onclick={() => onreview?.(job)}>{msg('review')}</button
				>
			{:else if onsettings}<button class="pa-link" onclick={onsettings}>{msg('sendDenied')}</button
				>{/if}
		</div>{/if}
	{#if verified}<div class="pa-verified" class:pa-check-drawn={first}>
			<svg aria-hidden="true" viewBox="0 0 24 24"><path d="m4 12 5 5 11-12" pathLength="1" /></svg
			>{msg(sendPreview(job) ? 'smtpAccepted' : 'verified')}
		</div>{/if}
	{#if job.status === 'done' && job.result && (resultLink(job) || office)}<button
			class="pa-primary"
			data-guide="native"
			disabled={opening}
			onclick={openResult}
			><Icon name="external" />{msg(nativeDraftLink(job) ? 'editDraft' : 'openResult')}</button
		>{/if}
	{#if job.status === 'paused'}<button
			class="pa-button"
			disabled={pending}
			onclick={() => onaction('resume', job)}><Icon name="play" />{msg('resume')}</button
		>
	{:else if ['planned', 'running', 'waiting'].includes(job.status)}<button
			class="pa-button"
			disabled={pending}
			onclick={() => onaction('pause', job)}><Icon name="pause" />{msg('pause')}</button
		>{/if}
	{#if job.totalSteps}<section>
			<h3>{msg('progress')}</h3>
			<span>{job.completedSteps ?? 0} / {job.totalSteps}</span><progress
				value={job.completedSteps ?? 0}
				max={job.totalSteps}
			></progress>
		</section>{/if}
	{#if job.receipts?.length}<details>
			<summary>{msg('receipts')}</summary>{#each job.receipts as receipt}<p class="pa-status">
					<Icon name="check" />{receipt.title}
				</p>{/each}
		</details>{/if}
	{#if job.audit?.length}<details>
			<summary>{msg('audit')}</summary>{#each job.audit as entry}<p>
					<time>{new Date(entry.time).toLocaleString()}</time> · {entry.state}
				</p>{/each}
		</details>{/if}
	{#if ['planned', 'paused', 'running', 'waiting'].includes(job.status)}{#if confirmCancel}<p
				class="pa-warning"
			>
				<Icon name="info" />{msg('confirmCancel')}
			</p>
			<button class="pa-button" onclick={() => (confirmCancel = false)}>{msg('back')}</button
			><button
				class="pa-button"
				disabled={pending}
				onclick={() => {
					confirmCancel = false;
					onaction('cancel', job);
				}}>{msg('cancelJob')}</button
			>
		{:else}<button class="pa-link" onclick={() => (confirmCancel = true)}>{msg('cancelJob')}</button
			>{/if}{/if}
</article>
