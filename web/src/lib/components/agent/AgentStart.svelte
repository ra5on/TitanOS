<script lang="ts">
	// The start of the KI-Agent (PLAN 48), a new chat like in the big chat
	// apps, the same on the desktop and the phone: a greeting with the
	// glowing agent orb, the projects as tiles to swipe through, the tasks
	// that run or wait right now, and four suggestions as equal tiles.
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
	} = $props();

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
			><Icon name="sparkle" size={28} /></span
		>
		<h1>{hello}</h1>
		<p>
			{inProject ? t('agent.startInProject', { name: inProject.name }) : t('agent.startQuestion')}
		</p>
	</div>

	<div class="tiles" role="group" aria-label={t('agent.projects')}>
		{#each projects as p (p.id)}
			<button
				type="button"
				class="ptile"
				class:on={p.id === project}
				style:--tile={p.color || 'var(--accent)'}
				onclick={() => onproject(p)}
			>
				<span class="icon" aria-hidden="true">{p.icon || '📁'}</span>
				<strong>{p.name}</strong>
				<small
					>{count(p.id) === 1
						? t('agent.chatCountOne')
						: t('agent.chatCount', { count: count(p.id) })}</small
				>
			</button>
		{/each}
		<button type="button" class="ptile add" onclick={onnewproject}>
			<span class="icon" aria-hidden="true"><Icon name="plus" size={20} /></span>
			<strong>{t('agent.projectTile')}</strong>
			<small>{t('agent.projectTileSub')}</small>
		</button>
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

	<div class="sugg">
		{#each suggestions as s (s.text)}
			<button type="button" onclick={() => onfill(s.text)}
				><strong>{s.title}</strong><small>{s.sub}</small></button
			>
		{/each}
	</div>
</div>

<style>
	.start {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		width: 100%;
		max-width: 800px;
		margin: 0 auto;
		padding: 0 var(--ag-pad, 20px) 12px;
	}
	.hello {
		flex: 1 0 auto;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		text-align: center;
		gap: 8px;
		padding: 24px 0 18px;
	}
	.orb {
		width: 64px;
		height: 64px;
		border-radius: 22px;
		display: grid;
		place-items: center;
		color: #fff;
		background: var(--ag-orb, var(--accent));
		box-shadow:
			0 0 0 6px color-mix(in srgb, var(--accent) 12%, transparent),
			0 0 60px 4px color-mix(in srgb, var(--accent) 50%, transparent);
		margin-bottom: 6px;
	}
	:global(.agent.glow) .orb {
		animation: float 6s ease-in-out infinite;
	}
	:global(.agent.glow) .orb.busy {
		animation:
			float 6s ease-in-out infinite,
			breathe 1.8s ease-in-out infinite;
	}
	:global(.agent.calm) .orb {
		animation: none !important;
	}
	@keyframes float {
		50% {
			transform: translateY(-5px);
		}
	}
	@keyframes breathe {
		50% {
			box-shadow:
				0 0 0 12px color-mix(in srgb, var(--accent) 8%, transparent),
				0 0 80px 14px color-mix(in srgb, var(--accent) 65%, transparent);
		}
	}
	h1 {
		margin: 0;
		font-size: clamp(24px, 3vw, 30px);
		letter-spacing: -0.01em;
		background: linear-gradient(
			90deg,
			var(--text),
			color-mix(in srgb, var(--accent) 70%, var(--text))
		);
		-webkit-background-clip: text;
		background-clip: text;
		color: transparent;
	}
	.hello p {
		margin: 0;
		color: var(--muted);
		font-size: 15px;
	}
	.tiles {
		display: flex;
		gap: 10px;
		overflow-x: auto;
		padding: 4px var(--ag-pad, 20px) 8px;
		margin: 0 calc(-1 * var(--ag-pad, 20px)) 8px;
		scrollbar-width: none;
	}
	.ptile {
		flex: none;
		width: 124px;
		height: 96px;
		border-radius: 18px;
		padding: 10px 12px;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		justify-content: flex-end;
		gap: 1px;
		text-align: left;
		font: inherit;
		color: #fff;
		cursor: pointer;
		border: 1px solid color-mix(in srgb, var(--tile) 55%, transparent);
		background: linear-gradient(
			145deg,
			color-mix(in srgb, var(--tile) 88%, transparent),
			color-mix(in srgb, var(--tile) 38%, transparent)
		);
		box-shadow: 0 12px 30px -14px var(--tile);
		transition:
			transform 0.18s ease,
			box-shadow 0.18s ease;
	}
	.ptile:hover {
		transform: translateY(-2px);
		box-shadow: 0 16px 34px -12px var(--tile);
	}
	.ptile.on {
		outline: 2px solid var(--tile);
		outline-offset: 2px;
	}
	.ptile .icon {
		font-size: 20px;
		margin-bottom: auto;
		display: grid;
	}
	.ptile strong {
		font-size: 14px;
		max-width: 100%;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.ptile small {
		font-size: 12px;
		opacity: 0.85;
	}
	.ptile.add {
		--tile: var(--muted);
		border: 1px dashed var(--border);
		background: color-mix(in srgb, var(--surface) 50%, transparent);
		color: var(--text);
		box-shadow: none;
	}
	.runs {
		display: flex;
		flex-direction: column;
		gap: 8px;
		margin-bottom: 10px;
	}
	.run {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 4px 6px 4px 4px;
		border-radius: 14px;
		border: 1px solid color-mix(in srgb, var(--accent) 40%, transparent);
		background: linear-gradient(
			110deg,
			color-mix(in srgb, var(--accent) 18%, var(--surface)),
			color-mix(in srgb, var(--surface) 70%, transparent)
		);
	}
	.run.waiting {
		border-color: var(--warn-border);
		background: var(--warn-bg);
	}
	.open {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		text-align: left;
		cursor: pointer;
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex: none;
		background: var(--accent);
		box-shadow: 0 0 10px var(--accent);
		animation: pulse 1.8s ease-in-out infinite;
	}
	.run.waiting .dot {
		background: var(--warn-text);
		box-shadow: none;
		animation: none;
	}
	:global(.agent.calm) .dot {
		animation: none;
	}
	@keyframes pulse {
		50% {
			opacity: 0.4;
		}
	}
	.t {
		min-width: 0;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.t small {
		color: var(--muted);
		margin-left: 8px;
		font-size: var(--text-sm);
	}
	.stop {
		width: 30px;
		height: 30px;
		border-radius: 50%;
		border: 0;
		flex: none;
		display: grid;
		place-items: center;
		background: color-mix(in srgb, var(--text) 12%, transparent);
		color: var(--text);
		cursor: pointer;
	}
	.sugg {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		gap: 8px;
	}
	.sugg button {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-height: 62px;
		padding: 10px 12px;
		border-radius: 14px;
		border: 1px solid var(--border);
		background: color-mix(in srgb, var(--surface) 70%, transparent);
		backdrop-filter: blur(14px);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		text-align: left;
		cursor: pointer;
		transition:
			border-color 0.18s ease,
			transform 0.18s ease;
	}
	.sugg button:hover {
		border-color: color-mix(in srgb, var(--accent) 50%, var(--border));
		transform: translateY(-1px);
	}
	.sugg small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	@media (max-width: 900px) {
		.sugg {
			grid-template-columns: repeat(2, minmax(0, 1fr));
		}
	}
	@media (max-width: 760px) {
		.start {
			--ag-pad: 16px;
		}
		.hello {
			padding-top: 8px;
		}
	}
</style>
