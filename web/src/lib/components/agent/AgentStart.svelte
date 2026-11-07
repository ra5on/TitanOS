<script lang="ts">
	// The start of the KI-Agent (PLAN 48, new look PLAN 53), the same on the
	// desktop and the phone: the agent's symbol with a greeting (PLAN 68:
	// no glow, colors of the theme), the projects
	// as small chips, the tasks that run or wait right now and up to three
	// suggestions that fit the moment (warnings of the NAS, the time of
	// day), with "andere" for a fresh pick.
	import Icon from '#lib/components/Icon.svelte';
	import DesktopIcon from '#lib/desktop/DesktopIcon.svelte';
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
		<span class="orb" aria-hidden="true"
			><DesktopIcon glyph="agent" gradient="violet" size={48} name={t('nav.agent')} /></span
		>
		<h1>{hi}{name}</h1>
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
				<button type="button" onclick={() => onfill(s.text)}>
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
	/* PLAN 68: cards and chips of the TitanOS theme, no glass or glow. */
	.start {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		width: 100%;
		max-width: 808px;
		margin: 0 auto;
		padding: 0 24px 12px;
	}
	.hello {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 4px;
		padding: clamp(16px, 6vh, 48px) 0 16px;
	}
	.orb {
		display: block;
		margin-bottom: 10px;
	}
	h1 {
		margin: 0;
		font-size: clamp(1.5rem, 4vw, 2rem);
		font-weight: 700;
		letter-spacing: -0.015em;
		line-height: 1.15;
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
		margin: 0 -24px 16px;
		padding: 2px 24px;
	}
	.chip-p {
		flex: none;
		display: inline-flex;
		align-items: center;
		gap: 6px;
		min-height: 34px;
		padding: 0 12px 0 10px;
		border-radius: 999px;
		border: 1px solid var(--border);
		background: var(--surface);
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		white-space: nowrap;
		cursor: pointer;
	}
	.chip-p:hover {
		background: color-mix(in srgb, var(--text) 7%, var(--surface));
	}
	.chip-p .em {
		display: grid;
		place-items: center;
	}
	.chip-p.on {
		border-color: color-mix(in srgb, var(--accent) 60%, var(--border));
		background: color-mix(in srgb, var(--accent) 18%, var(--surface));
		font-weight: 600;
	}
	.chip-p.add {
		color: var(--muted);
	}
	.runs {
		display: flex;
		flex-direction: column;
		margin-bottom: 16px;
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
		overflow: hidden;
	}
	.run {
		display: flex;
		align-items: center;
		gap: 6px;
		border-top: 1px solid var(--border);
	}
	.run:first-child {
		border-top: 0;
	}
	.run.waiting {
		box-shadow: inset 3px 0 0 var(--warn-border);
	}
	.open {
		flex: 1;
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 48px;
		padding: 6px 16px;
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
		flex: none;
	}
	.waiting .dot {
		background: var(--warn-text);
	}
	.t {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.t small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.stop {
		width: 32px;
		height: 32px;
		margin-right: 10px;
		border-radius: var(--radius-ctl);
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
		letter-spacing: 0.05em;
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
		font-size: var(--text-sm);
		text-transform: none;
		letter-spacing: 0;
		cursor: pointer;
		min-height: 32px;
	}
	.sugg {
		display: flex;
		flex-direction: column;
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
		overflow: hidden;
	}
	.sugg button {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 11px 16px;
		border: 0;
		border-top: 1px solid var(--border);
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.sugg button:first-child {
		border-top: 0;
	}
	.sugg button:hover {
		background: color-mix(in srgb, var(--text) 5%, transparent);
	}
	.sugg button:focus-visible,
	.chip-p:focus-visible,
	.open:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.sq {
		width: 32px;
		height: 32px;
		border-radius: var(--radius-ctl);
		flex: none;
		display: grid;
		place-items: center;
		color: var(--muted);
		background: var(--chip);
	}
	.sugg strong {
		font-weight: 600;
	}
	.sugg small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.why {
		align-self: flex-start;
		font-style: normal;
		font-size: var(--text-xs);
		font-weight: 600;
		padding: 1px 7px;
		border-radius: 999px;
		margin-bottom: 3px;
		border: 1px solid transparent;
	}
	.why.warn {
		background: var(--warn-bg);
		border-color: var(--warn-border);
		color: var(--warn-text);
	}
	.why.info {
		background: var(--chip);
		color: var(--text);
	}
	@media (max-width: 760px) {
		.start {
			padding: 0 14px 12px;
		}
		.chips {
			margin: 0 -14px 16px;
			padding: 2px 14px;
		}
	}
	.why.ok {
		background: var(--ok-bg);
		border-color: var(--ok-border);
		color: var(--ok-text);
	}
</style>
