<script lang="ts">
	import Icon from '#lib/components/Icon.svelte';
	import AgentDots from './AgentDots.svelte';
	import { sourceTitle, msg, iconFor, jobTitle, outputKind, failure } from './labels.ts';
	import { jobOutput, mailFields, sourceLocation, stepTool } from './sources.ts';
	import { motifFor } from './guide.ts';
	import type { Capability, Chat, Job, Settings, Source } from './api.ts';
	let {
		chat,
		jobs,
		caps,
		settings,
		reduced,
		select,
		onreview,
		onedit,
		onsource,
		onexample,
		onsettings,
		oncontext,
		onbackground,
		oncue,
	}: {
		chat: Chat | undefined;
		jobs: Job[];
		caps: Capability[];
		settings: Settings;
		reduced: boolean;
		select: (j: Job) => void;
		onreview: (j: Job) => void;
		onedit: (j: Job) => void;
		onsource: (s: Source, j: Job) => void;
		onexample: () => void;
		onsettings: () => void;
		oncontext: () => void;
		onbackground: (j: Job) => void;
		oncue: (stops: string[], tool: string) => void;
	} = $props();
	const available = $derived(caps.filter((c) => c.effective !== 'never'));
	const example = $derived(available.find((c) => c.id === 'agent.example'));
	function date(iso: string) {
		return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'long' });
	}
	function time(iso: string) {
		return new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
	}
</script>

{#if !chat && !jobs.length}
	<div class="pa-welcome">
		<span class="pa-logo lg"><Icon name="sparkle" size={32} /></span>
		<h1>{msg('greeting')}</h1>
		{#if example}<button class="pa-example" onclick={onexample}
				><span class="pa-logo"><Icon name="play" /></span><span
					><b>{msg('example')}</b><small>{msg('startMessage')}</small></span
				><Icon name="arrowRight" /></button
			>{/if}
		{#if caps.some((c) => c.id === 'chat.ask' && c.effective === 'never')}<button
				class="pa-link"
				onclick={onsettings}>{msg('needsSetup')}<Icon name="chevronRight" size={15} /></button
			>{/if}
		{#if available.length}<button class="pa-link" onclick={oncontext}
				>{msg('available')}<Icon name="chevronRight" size={15} /></button
			>
		{:else}<p>{msg('noTools')}</p>
			<button class="pa-button" onclick={onsettings}>{msg('settings')}</button>{/if}
	</div>
{:else}
	<div class="pa-conversation">
		{#each jobs as j, index (j.id)}
			{@const output = jobOutput(j)}
			{@const draft = mailFields(j)}
			{#if index === 0 || date(j.created) !== date(jobs[index - 1].created)}<p class="pa-date">
					{date(j.created)}
				</p>{/if}
			<p class="pa-bubble pa-user">
				{j.request.prompt || caps.find((c) => c.id === j.request.tool)?.title || j.request.tool}
			</p>
			<div class="pa-reply" data-guide={'reply-' + j.id}>
				<div class="pa-author">
					<span class="pa-logo"><Icon name="sparkle" size={18} /></span><b>Agent</b><time
						datetime={j.created}>{time(j.updated)}</time
					>
				</div>
				{#if draft?.isDraft}<p class="pa-readable pa-bubble pa-agent">
						{output?.text && output.text.trim() !== draft.text.trim()
							? output.text
							: msg('draftReady')}
					</p>{:else if j.status === 'done' && output?.text}<div
						class="pa-readable pa-bubble pa-agent"
					>
						{output.text}
					</div>
				{:else if j.error}<p class="pa-warning"><Icon name="info" />{failure(j)}</p>
				{:else if j.status === 'waiting'}<span class="pa-status"
						><Icon name="lock" />{msg('needs')}</span
					>
				{:else if j.status === 'done'}<p class="pa-readable pa-bubble pa-agent">
						{output?.title || msg('done')}
					</p>
				{:else}<div class="pa-status" aria-live="polite">
						<AgentDots
							motif={motifFor(stepTool(j))}
							paused={j.status === 'paused'}
							calm={reduced || settings.motion !== 'full' || j.status !== 'running'}
						/>{msg(
							j.status === 'running' && ['preparing', 'executing', 'queued'].includes(j.phase)
								? j.phase
								: j.status === 'paused'
									? 'pausedHere'
									: j.status,
						)}
					</div>{/if}
				{#if (output?.sources ?? []).length}<div class="pa-chips pa-proof">
						{#each output?.sources ?? [] as source, i}<button
								data-guide={j.id + '-source-' + i}
								onclick={() => (sourceLocation(source) ? onsource(source, j) : select(j))}
								><Icon
									name={j.status === 'done' ? 'check' : iconFor(source.kind)}
									size={13}
								/>{sourceTitle(source)}</button
							>{/each}
					</div>{/if}
				{#if output}
					<div class="pa-result-card" data-guide={'card-' + j.id}>
						<button class="pa-card-open" onclick={() => select(j)}
							><span class="pa-title-row"
								><Icon name={iconFor(stepTool(j))} /><b
									>{draft?.isDraft ? msg('mailDraft') : jobTitle(j, caps)}</b
								><Icon name="chevronRight" size={16} /></span
							>
							<small
								>{draft?.isDraft ? msg('waitingYou') + ' · ' + msg('unsent') : outputKind(j)}</small
							></button
						>
						{#if draft?.isDraft}<div class="pa-card-excerpt">
								<span>{msg('to')} · {draft.to}</span><b>{draft.subject}</b><span>{draft.text}</span>
							</div>
							<div class="pa-mobile-card-actions pa-actions">
								<button class="pa-button" data-guide={'editor-' + j.id} onclick={() => onedit(j)}
									>{msg('edit')}</button
								><button
									class="pa-primary"
									onclick={() => {
										select(j);
										onreview(j);
									}}>{msg('review')}</button
								>
							</div>
						{/if}
					</div>
				{:else if j.status === 'planned' || j.status === 'running' || j.status === 'paused'}<button
						class="pa-link"
						onclick={() => onbackground(j)}
						>{msg('background')}<Icon name="arrowRight" size={14} /></button
					>{/if}
			</div>
		{/each}
	</div>
{/if}
