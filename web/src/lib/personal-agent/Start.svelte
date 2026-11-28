<script lang="ts">
	// 1.40 (§125, C1, owner 28.11.2026: "Früher gab es KI, da konnte man nur
	// einen Chat machen. Jetzt gibt's Agent … Der kann alles für mich
	// übernehmen"): the start of the Agent is not an empty chat. It shows the
	// living figure, what runs now, what waits for you and what you could ask.
	import Icon from '#lib/components/Icon.svelte';
	import Figure, { type Mood } from './Figure.svelte';
	import { msg, jobTitle } from './labels.ts';
	import type { Capability, Job } from './api.ts';
	let {
		name,
		jobs,
		caps,
		available,
		calm,
		listening = false,
		model,
		select,
		onapprove,
		onsuggest,
		onmodel,
		onsettings,
	}: {
		name: string;
		jobs: Job[];
		caps: Capability[];
		available: Capability[];
		calm: boolean;
		listening?: boolean;
		/** Text when the local model is not ready; empty when it is. */
		model: string;
		select: (j: Job) => void;
		onapprove: (j: Job) => Promise<void>;
		onsuggest: (kind: 'backup' | 'example' | 'files' | 'ask', text?: string) => void;
		onmodel: () => void;
		onsettings: () => void;
	} = $props();
	const running = $derived(
		jobs.filter((j) => j.status === 'running' || j.status === 'planned').slice(-3),
	);
	const waiting = $derived(jobs.filter((j) => j.status === 'waiting').slice(-3));
	const mood = $derived<Mood>(listening ? 'listen' : running.length ? 'work' : 'idle');
	const has = (id: string) => available.some((c) => c.id === id);
	const blocked = $derived(caps.some((c) => c.id === 'chat.ask' && c.effective === 'never'));
	// D2: the lock of an approved card opens before the card goes.
	let opening = $state<Record<string, boolean>>({});
	async function approve(j: Job) {
		opening = { ...opening, [j.id]: true };
		try {
			await onapprove(j);
		} catch {
			opening = { ...opening, [j.id]: false };
		}
	}
	function steps(j: Job) {
		const total = j.totalSteps ?? 0,
			done = Math.min(total, j.completedSteps ?? 0);
		return { total, done };
	}
	// D5: a tapped suggestion is pressed in and a light runs over it.
	function shine(e: MouseEvent) {
		const b = e.currentTarget as HTMLElement;
		b.classList.remove('pa-shine');
		void b.offsetWidth;
		b.classList.add('pa-shine');
	}
	const suggestions = $derived(
		[
			has('backup.status') && {
				kind: 'backup' as const,
				icon: 'shield',
				text: msg('suggestBackup'),
			},
			has('files.read') || has('office.table') || has('attachment.read')
				? { kind: 'files' as const, icon: 'folder', text: msg('suggestFiles') }
				: false,
			has('chat.ask') && { kind: 'ask' as const, icon: 'chat', text: msg('suggestAsk') },
			has('agent.example') && { kind: 'example' as const, icon: 'play', text: msg('example') },
		].filter(Boolean) as {
			kind: 'backup' | 'files' | 'ask' | 'example';
			icon: string;
			text: string;
		}[],
	);
</script>

<div class="pa-start">
	<div class="pa-orb">
		<Figure {mood} {calm} size={112} />
		<p class="pa-orb-say" class:pa-shimmer={!calm && mood !== 'idle'} role="status">
			{listening
				? msg('moodListen')
				: running.length
					? running.length === 1
						? jobTitle(running[0], caps)
						: msg('moodWorkMany').replace('{n}', String(running.length))
					: msg('moodIdle')}
		</p>
	</div>
	<div class="pa-hello">
		<h1>{msg('hello').replace('{name}', name)}</h1>
		<p class="pa-muted">
			{running.length || waiting.length
				? [
						running.length ? msg('runningCount').replace('{n}', String(running.length)) : '',
						waiting.length ? msg('waitingCount').replace('{n}', String(waiting.length)) : '',
					]
						.filter(Boolean)
						.join(' · ')
				: msg('helloSub')}
		</p>
	</div>
	{#if model}<div class="pa-note" role="status">
			<Icon name="info" />
			<div>
				<b>{msg('modelDown')}</b><span>{model}</span>
				<button class="pa-link" onclick={onmodel}
					>{msg('model')}<Icon name="chevronRight" size={14} /></button
				>
			</div>
		</div>{/if}
	{#if blocked}<button class="pa-link" onclick={onsettings}
			>{msg('needsSetup')}<Icon name="chevronRight" size={15} /></button
		>{/if}
	{#if running.length}<section class="pa-start-group">
			<h2>{msg('runningNow')}</h2>
			{#each running as j (j.id)}
				{@const st = steps(j)}
				<button class="pa-start-card" onclick={() => select(j)}>
					<span class="pa-sc-row"
						><b class="pa-shimmer-calm" class:pa-shimmer={!calm}>{jobTitle(j, caps)}</b
						>{#if st.total > 1}<small
								>{msg('stepOf')
									.replace('{n}', String(st.done + 1 > st.total ? st.total : st.done + 1))
									.replace('{m}', String(st.total))}</small
							>{/if}</span
					>
					<span
						class="pa-progress"
						class:pa-indeterminate={st.total <= 1 && !calm}
						role="progressbar"
						aria-label={jobTitle(j, caps)}
						aria-valuemin="0"
						aria-valuemax={st.total || 1}
						aria-valuenow={st.done}
						><u style={st.total > 1 ? `width:${Math.max(8, (st.done / st.total) * 100)}%` : ''}
						></u></span
					>
				</button>
			{/each}
		</section>{/if}
	{#if waiting.length}<section class="pa-start-group">
			<h2 class="pa-wait-head"><Icon name="lock" size={14} />{msg('waitingYou')}</h2>
			{#each waiting as j (j.id)}
				<div class="pa-start-card pa-waitcard" class:pa-opened={opening[j.id]}>
					<span class="pa-sc-row"
						><span class="pa-padlock" class:open={opening[j.id]} aria-hidden="true"
							><svg viewBox="0 0 32 32" width="26" height="26"
								><path class="sh" d="M11 15v-4a5 5 0 0 1 10 0v4" /><rect
									x="8"
									y="15"
									width="16"
									height="12"
									rx="3"
								/></svg
							></span
						><span class="pa-sc-col"
							><b>{j.preview?.title || jobTitle(j, caps)}</b>{#if j.preview?.text}<small
									class="pa-muted">{j.preview.text}</small
								>{/if}</span
						></span
					>
					<span class="pa-actions">
						{#if j.token}<button
								class="pa-primary"
								disabled={opening[j.id]}
								onclick={() => approve(j)}>{msg('approveShort')}</button
							>{/if}<button class="pa-button" onclick={() => select(j)}>{msg('look')}</button>
					</span>
				</div>
			{/each}
		</section>{/if}
	{#if suggestions.length}<section class="pa-start-group">
			<h2>{msg('suggestions')}</h2>
			<div class="pa-suggest">
				{#each suggestions as sug (sug.kind)}<button
						onclick={(e) => {
							shine(e);
							onsuggest(sug.kind, sug.text);
						}}><Icon name={sug.icon as never} size={18} />{sug.text}</button
					>{/each}
			</div>
		</section>{:else if !blocked}<p class="pa-muted">{msg('noTools')}</p>{/if}
</div>
