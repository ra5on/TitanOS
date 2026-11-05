<script lang="ts">
	// The start of the KI-Agent (PLAN 48, new look PLAN 53), the same on the
	// desktop and the phone: the glowing orb with a greeting, the projects
	// as small chips, the tasks that run or wait right now and up to three
	// suggestions that fit the moment (warnings of the NAS, the time of
	// day), with "andere" for a fresh pick.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { ChatInfo, Project, Suggestion } from '#lib/agent.ts';

	let {
		user,
		chats,
		projects,
		project = '',
		suggestions,
		onopen,
		onproject,
		onnewproject,
		onstop,
		onfill,
		onshuffle,
		show = true,
		status = '',
	}: {
		user: string;
		chats: ChatInfo[];
		projects: Project[];
		/** The project a new chat goes to. */
		project?: string;
		suggestions: Suggestion[];
		onopen: (id: string) => void;
		onproject: (p: Project) => void;
		onnewproject: () => void;
		onstop: (c: ChatInfo) => void;
		onfill: (text: string) => void;
		/** Shows other suggestions. */
		onshuffle?: () => void;
		/** Suggestions switched on (Einstellungen › Darstellung). */
		show?: boolean;
		/** One line on how the NAS is doing. */
		status?: string;
	} = $props();
	const [hi, name] = $derived.by(() => {
		const i = hello.lastIndexOf(user);
		return i > 0 ? [hello.slice(0, i), hello.slice(i)] : [hello, ''];
	});
	const today = new Intl.DateTimeFormat(undefined, {
		weekday: 'long',
		day: 'numeric',
		month: 'long',
	}).format(new Date());

	const hour = new Date().getHours();
	const hello = $derived(
		hour < 11
			? t('agent.helloMorning', { name: user })
			: hour < 18
				? t('agent.helloDay', { name: user })
				: t('agent.helloEvening', { name: user }),
	);
	const active = $derived(chats.filter((c) => c.state === 'running' || c.state === 'waiting'));
	const inProject = $derived(projects.find((p) => p.id === project));
	const count = (id: string) => chats.filter((c) => c.project === id).length;
</script>

<div class="start">
	<div class="hello">
		<span class="orb" class:busy={active.some((c) => c.state === 'running')} aria-hidden="true"
			><Icon name="sparkle" size={32} /></span
		>
		<h1>{hi}<span class="name">{name}</span></h1>
		<p>
			{inProject
				? t('agent.startInProject', { name: inProject.name })
				: status
					? `${today} · ${status}`
					: today}
		</p>
	</div>

	<div class="chips" role="group" aria-label={t('agent.projects')}>
		{#each projects as p (p.id)}
			<button
				type="button"
				class="chip-p"
				class:on={p.id === project}
				style:--tile={p.color || 'var(--accent)'}
				title={count(p.id) === 1
					? t('agent.chatCountOne')
					: t('agent.chatCount', { count: count(p.id) })}
				onclick={() => onproject(p)}
			>
				<span class="em" aria-hidden="true">{p.icon || '📁'}</span>{p.name}
			</button>
		{/each}
		<button type="button" class="chip-p add" onclick={onnewproject}
			><Icon name="plus" size={14} />{t('agent.projectChip')}</button
		>
	</div>

	{#if active.length}
		<div class="runs">
			{#each active as chat (chat.id)}
				<div class="run" class:waiting={chat.state === 'waiting'}>
					<button type="button" class="open" onclick={() => onopen(chat.id)}>
						<span class="dot" aria-hidden="true"></span>
						<span class="t"
							>{chat.title}<small
								>{chat.state === 'waiting' ? t('agent.waitingForYou') : t('agent.working')}</small
							></span
						>
					</button>
					{#if chat.state === 'running'}
						<button
							type="button"
							class="stop"
							aria-label={t('agent.stopChat', { title: chat.title })}
							onclick={() => onstop(chat)}><Icon name="stop" size={12} /></button
						>
					{/if}
				</div>
			{/each}
		</div>
	{/if}

	{#if show && suggestions.length}
		<div class="cap">
			<span>{t('agent.fitsNow')}</span>
			{#if onshuffle}<button type="button" class="other" onclick={onshuffle}
					><Icon name="refresh" size={13} />{t('agent.otherSuggestions')}</button
				>{/if}
		</div>
		<div class="sugg">
			{#each suggestions as s, i (s.text)}
				<button
					type="button"
					style:--i={i}
					style:--c={s.color ?? 'var(--accent)'}
					onclick={() => onfill(s.text)}
				>
					<span class="sq" aria-hidden="true"><Icon name={s.icon ?? 'sparkle'} size={18} /></span>
					<span class="t">
						{#if s.tag}<em class="why {s.tone ?? 'info'}">{s.tag}</em>{/if}
						<strong>{s.title}</strong><small>{s.sub}</small>
					</span>
					<Icon name="chevronRight" size={16} />
				</button>
			{/each}
		</div>
	{/if}
</div>

<style>
	.start {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		width: 100%;
		max-width: 780px;
		margin: 0 auto;
		padding: 0 var(--ag-pad, 20px) 12px;
	}
	.hello {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 6px;
		padding: clamp(16px, 6vh, 56px) 0 18px;
	}
	.orb {
		width: 72px;
		height: 72px;
		border-radius: 24px;
		display: grid;
		place-items: center;
		margin-bottom: 12px;
		color: #fff;
		background: var(--ag-gradient, var(--accent));
		box-shadow:
			0 20px 50px -14px color-mix(in srgb, var(--ag-accent-2, var(--accent)) 80%, transparent),
			inset 0 1px 0 rgb(255 255 255 / 0.35);
		position: relative;
		animation: ag-float 6s ease-in-out infinite;
	}
	.orb::after {
		content: '';
		position: absolute;
		inset: -9px;
		border-radius: 30px;
		border: 1px solid color-mix(in srgb, var(--ag-accent-2, var(--accent)) 40%, transparent);
		box-shadow: 0 0 40px color-mix(in srgb, var(--ag-accent-2, var(--accent)) 35%, transparent);
	}
	.orb.busy::after {
		animation: ag-pulse 1.6s ease-in-out infinite;
	}
	@keyframes ag-float {
		50% {
			transform: translateY(-4px);
		}
	}
	@keyframes ag-pulse {
		50% {
			opacity: 0.35;
			transform: scale(1.08);
		}
	}
	:global(.agent.calm) .orb,
	:global(.agent.calm) .orb::after {
		animation: none;
	}
	h1 {
		margin: 0;
		font-size: clamp(1.75rem, 5vw, 2.4rem);
		font-weight: 800;
		letter-spacing: -0.025em;
		line-height: 1.1;
	}
	.name {
		background: linear-gradient(
			90deg,
			color-mix(in srgb, var(--accent) 70%, #fff),
			color-mix(in srgb, var(--ag-accent-2, var(--accent)) 60%, #fff)
		);
		-webkit-background-clip: text;
		background-clip: text;
		color: transparent;
	}
	.hello p {
		margin: 0;
		color: var(--muted);
	}
	.chips {
		display: flex;
		gap: 8px;
		overflow-x: auto;
		scrollbar-width: none;
		margin: 0 calc(-1 * var(--ag-pad, 20px)) 16px;
		padding: 2px var(--ag-pad, 20px);
	}
	.chip-p {
		flex: none;
		display: inline-flex;
		align-items: center;
		gap: 7px;
		min-height: 38px;
		padding: 0 13px 0 8px;
		border-radius: 999px;
		border: 1px solid color-mix(in srgb, var(--text) 12%, transparent);
		background: color-mix(in srgb, var(--surface) 45%, transparent);
		backdrop-filter: blur(16px);
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		font-weight: 500;
		white-space: nowrap;
		cursor: pointer;
	}
	.chip-p .em {
		width: 26px;
		height: 26px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: color-mix(in srgb, var(--tile) 35%, transparent);
	}
	.chip-p.on {
		border-color: color-mix(in srgb, var(--tile) 70%, transparent);
		background: color-mix(in srgb, var(--tile) 22%, transparent);
	}
	.chip-p.add {
		padding-left: 12px;
		border-style: dashed;
		color: var(--muted);
	}
	.runs {
		display: flex;
		flex-direction: column;
		gap: 6px;
		margin-bottom: 14px;
	}
	.run {
		display: flex;
		align-items: center;
		gap: 6px;
		border-radius: 16px;
		border: 1px solid color-mix(in srgb, var(--text) 10%, transparent);
		background: color-mix(in srgb, var(--surface) 50%, transparent);
		backdrop-filter: blur(18px);
	}
	.run.waiting {
		border-color: var(--warn-border, #f59e0b);
	}
	.open {
		flex: 1;
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 48px;
		padding: 6px 12px;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--accent);
		box-shadow: 0 0 10px var(--accent);
		flex: none;
	}
	.t {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.t small {
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.stop {
		width: 34px;
		height: 34px;
		margin-right: 8px;
		border-radius: 50%;
		border: 1px solid var(--border);
		background: none;
		color: var(--text);
		display: grid;
		place-items: center;
		cursor: pointer;
	}
	.cap {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin: 2px 2px 8px;
		font-size: var(--text-xs);
		font-weight: 600;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.other {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		border: 0;
		background: none;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-xs);
		text-transform: none;
		letter-spacing: 0;
		cursor: pointer;
		min-height: 32px;
	}
	.sugg {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.sugg button {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 12px;
		border-radius: 18px;
		border: 1px solid color-mix(in srgb, var(--text) 10%, transparent);
		background: color-mix(in srgb, var(--surface) calc(var(--ag-alpha, 0.55) * 100%), transparent);
		backdrop-filter: blur(22px) saturate(160%);
		box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.05);
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
		animation: ag-in 0.45s calc(var(--i) * 70ms) both cubic-bezier(0.2, 0.8, 0.2, 1);
		transition:
			transform 0.15s,
			border-color 0.15s;
	}
	.sugg button:hover {
		border-color: color-mix(in srgb, var(--c) 50%, transparent);
	}
	.sugg button:active {
		transform: scale(0.985);
	}
	:global(.agent.calm) .sugg button {
		animation: none;
	}
	@keyframes ag-in {
		from {
			opacity: 0;
			transform: translateY(10px);
		}
	}
	.sq {
		width: 38px;
		height: 38px;
		border-radius: 12px;
		flex: none;
		display: grid;
		place-items: center;
		color: #fff;
		background: linear-gradient(145deg, var(--c), color-mix(in srgb, var(--c) 45%, #0b1020));
		box-shadow: 0 8px 18px -8px var(--c);
	}
	.sugg strong {
		font-weight: 600;
	}
	.sugg small {
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.why {
		align-self: flex-start;
		font-style: normal;
		font-size: 11px;
		font-weight: 600;
		padding: 1px 7px;
		border-radius: 999px;
		margin-bottom: 3px;
	}
	.why.warn {
		background: color-mix(in srgb, #fbbf24 18%, transparent);
		color: color-mix(in srgb, #fbbf24 70%, var(--text));
	}
	.why.info {
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		color: color-mix(in srgb, var(--accent) 65%, var(--text));
	}
	.why.ok {
		background: color-mix(in srgb, #34d399 16%, transparent);
		color: color-mix(in srgb, #34d399 70%, var(--text));
	}
</style>
