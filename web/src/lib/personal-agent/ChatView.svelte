<script lang="ts">
	import Icon from '#lib/components/Icon.svelte';
	import AgentDots from './AgentDots.svelte';
	import Figure from './Figure.svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import { sourceTitle, msg, iconFor, jobTitle, outputKind, failure } from './labels.ts';
	import { jobOutput, mailFields, sourceLocation, stepTool, visibleSources } from './sources.ts';
	import { motifFor } from './guide.ts';
	import { answerHtml } from './markdown.ts';
	import type { Capability, Chat, Job, Settings, Source } from './api.ts';
	let {
		chat,
		jobs,
		caps,
		available,
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
		phaseFor,
	}: {
		chat: Chat | undefined;
		jobs: Job[];
		caps: Capability[];
		available: Capability[];
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
		phaseFor: (id: string) => { seconds: number };
	} = $props();
	const example = $derived(available.find((c) => c.id === 'agent.example'));
	function date(iso: string) {
		return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'long' });
	}
	function time(iso: string) {
		return new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
	}
	// 1.40 (§125): what changed while this chat was open. B3: a job that
	// finishes in view first shows the three dots becoming a tick, then its
	// answer appears word by word. D2: a job that leaves "waiting" opens its
	// lock. D4: a message that arrives in view slides in.
	const seen = new Map<string, string>();
	let first = true;
	const finishing = new SvelteSet<string>(),
		reveal = new SvelteSet<string>(),
		unlocked = new SvelteSet<string>(),
		fresh = new SvelteSet<string>();
	$effect(() => {
		for (const j of jobs) {
			const before = seen.get(j.id);
			if (before === undefined && !first) fresh.add(j.id);
			if (before && before !== 'done' && j.status === 'done' && jobOutput(j)?.text) {
				if (reduced || settings.motion === 'off') reveal.add(j.id);
				else finishing.add(j.id);
			}
			if (before === 'waiting' && j.status !== 'waiting') {
				unlocked.add(j.id);
				setTimeout(() => unlocked.delete(j.id), 1600);
			}
			seen.set(j.id, j.status);
		}
		first = false;
	});
	// The words of an answer appear one after the other (B3). Text stays
	// text: only its words are wrapped, links and tables keep their place.
	function words(node: HTMLElement, on: boolean) {
		if (!on || reduced || settings.motion === 'off') return;
		const walker = document.createTreeWalker(node, NodeFilter.SHOW_TEXT);
		const texts: Text[] = [];
		while (walker.nextNode()) texts.push(walker.currentNode as Text);
		let n = 0;
		for (const t of texts) {
			if (!t.data.trim()) continue;
			const frag = document.createDocumentFragment();
			for (const part of t.data.split(/(\s+)/)) {
				if (!part) continue;
				if (/^\s+$/.test(part)) {
					frag.append(part);
					continue;
				}
				const span = document.createElement('span');
				span.className = 'pa-word';
				span.style.animationDelay = Math.min(1600, n++ * 22) + 'ms';
				span.textContent = part;
				frag.append(span);
			}
			t.replaceWith(frag);
		}
	}
	function stepText(j: Job) {
		const total = j.totalSteps ?? 0,
			done = Math.min(total, j.completedSteps ?? 0);
		return total > 1
			? msg('stepOf')
					.replace('{n}', String(Math.min(total, done + 1)))
					.replace('{m}', String(total))
			: '';
	}
	function doing(j: Job) {
		const id = j.currentTool || j.request.tool;
		return caps.find((c) => c.id === id)?.title || jobTitle(j, caps);
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
			{@const sources = visibleSources(j)}
			{@const draft = mailFields(j)}
			{#if index === 0 || date(j.created) !== date(jobs[index - 1].created)}<p class="pa-date">
					{date(j.created)}
				</p>{/if}
			<p class="pa-bubble pa-user" class:pa-enter={fresh.has(j.id)}>
				{j.request.prompt || caps.find((c) => c.id === j.request.tool)?.title || j.request.tool}
			</p>
			<div class="pa-reply" class:pa-enter={fresh.has(j.id)} data-guide={'reply-' + j.id}>
				<div class="pa-author">
					<span class="pa-logo"><Icon name="sparkle" size={18} /></span><b>Agent</b><time
						datetime={j.created}>{time(j.updated)}</time
					>
				</div>
				{#if draft?.isDraft}{#if output?.text && output.text.trim() !== draft.text.trim()}<div
							class="pa-readable pa-bubble pa-agent pa-md"
						>
							{@html answerHtml(output.text)}
						</div>{:else}<p class="pa-readable pa-bubble pa-agent">{msg('draftReady')}</p>{/if}
				{:else if j.status === 'done' && output?.text && finishing.has(j.id)}<div class="pa-finish">
						<Figure
							mood="done"
							size={44}
							calm={reduced}
							label={msg('done')}
							ondone={() => {
								finishing.delete(j.id);
								reveal.add(j.id);
							}}
						/>
					</div>
				{:else if j.status === 'done' && output?.text}{#key reveal.has(j.id)}<div
							class="pa-readable pa-bubble pa-agent pa-md"
							use:words={reveal.has(j.id)}
						>
							{@html answerHtml(output.text)}
						</div>{/key}
				{:else if j.error}<p class="pa-warning"><Icon name="info" />{failure(j)}</p>
				{:else if j.status === 'waiting' || unlocked.has(j.id)}<button
						class="pa-status pa-waitline"
						onclick={() => select(j)}
						><span class="pa-padlock" class:open={unlocked.has(j.id)} aria-hidden="true"
							><svg viewBox="0 0 32 32" width="22" height="22"
								><path class="sh" d="M11 15v-4a5 5 0 0 1 10 0v4" /><rect
									x="8"
									y="15"
									width="16"
									height="12"
									rx="3"
								/></svg
							></span
						>{unlocked.has(j.id) ? msg('approved') : msg('needs')}<Icon
							name="chevronRight"
							size={14}
						/></button
					>
				{:else if j.status === 'done'}<p class="pa-readable pa-bubble pa-agent">
						{output?.title || msg('done')}
					</p>
				{:else}<div class="pa-status" aria-live="polite" data-guide={'step-' + j.id}>
						<AgentDots
							phase={phaseFor(j.id)}
							motif={motifFor(stepTool(j))}
							paused={j.status === 'paused'}
							calm={reduced ||
								settings.motion !== 'full' ||
								(j.status !== 'running' && j.status !== 'paused')}
						/><span class="pa-doing"
							><b class:pa-shimmer={!reduced && settings.motion !== 'off' && j.status !== 'paused'}
								>{j.status === 'paused' ? msg('pausedHere') : doing(j)}</b
							><small
								>{[
									msg(
										j.status === 'running' && ['preparing', 'executing', 'queued'].includes(j.phase)
											? j.phase
											: j.status === 'paused'
												? 'paused'
												: j.status,
									),
									stepText(j),
								]
									.filter(Boolean)
									.join(' · ')}</small
							></span
						>
					</div>
					{#if (j.totalSteps ?? 0) > 1}<div class="pa-path" aria-hidden="true">
							{#each Array.from({ length: j.totalSteps ?? 0 }) as _, i (i)}{#if i}<span
										class="pa-path-bar"
										><u style:transform={`scaleX(${i <= (j.completedSteps ?? 0) ? 1 : 0})`}
										></u></span
									>{/if}<span
									class="pa-path-node"
									class:done={i < (j.completedSteps ?? 0)}
									class:now={i === (j.completedSteps ?? 0)}
								></span>{/each}
						</div>{/if}{/if}
				{#if sources.length}<div class="pa-chips pa-proof">
						{#each sources as source, i}<button
								data-guide={j.id + '-source-' + i}
								onclick={() => (sourceLocation(source) ? onsource(source, j) : select(j))}
								><Icon
									name={j.status === 'done' ? 'check' : iconFor(source.kind)}
									size={13}
								/>{sourceTitle(source)}</button
							>{/each}
					</div>{/if}
				{#if output && !draft?.isDraft}<button
						class="pa-link pa-details"
						data-guide={'card-' + j.id}
						onclick={() => select(j)}
						><Icon name={iconFor(stepTool(j))} size={15} />{msg('details')}<Icon
							name="chevronRight"
							size={14}
						/></button
					>
				{:else if output}
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
