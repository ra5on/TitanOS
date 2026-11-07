<script lang="ts">
	// PLAN 68: the agent's side bar beside Einstellungen › KI-Agent in the
	// agent's window, so the settings sit in the program like in the draft.
	// It reads the lists once; every entry leads back into the agent.
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.ts';
	import AgentMenu from './AgentMenu.svelte';
	import type { ChatInfo, Project, Status } from '#lib/agent.ts';

	let chats = $state<ChatInfo[]>([]);
	let projects = $state<Project[]>([]);
	let plannedCount = $state(0);
	let project = $state('');
	let ready = $state(false);
	$effect(() => {
		void (async () => {
			try {
				const status = await api.get<Status>('/agent/status');
				if (!status.enabled || !status.allowed || !status.volume) return;
				const [c, p] = await Promise.all([
					api.get<{ chats: ChatInfo[] }>('/agent/chats'),
					api.get<{ projects: Project[] }>('/agent/projects'),
				]);
				chats = c.chats;
				projects = p.projects;
				ready = true;
				api
					.get<{ schedules?: unknown[] }>('/agent/schedules')
					.then((r) => (plannedCount = r.schedules?.length ?? 0))
					.catch(() => {});
			} catch {
				ready = false;
			}
		})();
	});
	const toAgent = (query = '') => void goto('/agent' + query);
</script>

{#if ready}
	<aside class="ag-side">
		<AgentMenu
			{chats}
			{projects}
			{project}
			{plannedCount}
			settingsOn
			onopen={(id, author) =>
				toAgent(
					`?chat=${encodeURIComponent(id)}${author ? `&author=${encodeURIComponent(author)}` : ''}`,
				)}
			onnew={() => toAgent()}
			onproject={(id) => (project = id)}
			onnewproject={() => toAgent()}
			oneditproject={() => toAgent()}
			onknowledge={() => toAgent()}
			onplanned={() => toAgent()}
			onnotes={() => toAgent()}
			onsettings={() => {}}
		/>
	</aside>
{/if}

<style>
	/* Beside the page, edge to edge in the window (the page moves aside). */
	.ag-side {
		position: fixed;
		z-index: 2;
		top: 0;
		bottom: 0;
		left: 0;
		width: 220px;
		border-right: 1px solid var(--border);
		background: var(--bg);
	}
	.ag-side::before {
		content: '';
		position: absolute;
		inset: 0;
		z-index: -1;
		background: color-mix(in srgb, var(--surface) 70%, transparent);
	}
	:global(main.in-window:has(.ag-side)) {
		padding-left: calc(220px + var(--page-pad));
	}
</style>
