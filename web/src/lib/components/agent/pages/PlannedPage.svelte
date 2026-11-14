<script lang="ts">
	// "Geplant" of the KI-Agent as a page (PLAN 90-4): the morning report,
	// the tasks and reminders (Planned.svelte), and what planned tasks last
	// did – their chats, newest first.
	import Icon from '#lib/components/Icon.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import Planned from '../Planned.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { ChatInfo, Project } from '#lib/agent.ts';
	import PageShell from './PageShell.svelte';

	let {
		projects = [],
		chats = [],
		phone = false,
		onunfold,
		onopenchat,
		oncount,
	}: {
		projects?: Project[];
		chats?: ChatInfo[];
		phone?: boolean;
		onunfold?: () => void;
		onopenchat: (id: string) => void;
		oncount?: (n: number) => void;
	} = $props();

	const ran = $derived(
		chats
			.filter((c) => !c.author && ['schedule', 'reminder'].includes(c.origin ?? ''))
			.sort((a, b) => b.updated.localeCompare(a.updated))
			.slice(0, 5),
	);
</script>

<PageShell title={t('agent.planned')} lead={t('agent.schedulesIntro')} {phone} {onunfold}>
	<div><Planned {projects} {onopenchat} {oncount} /></div>
	{#if ran.length}
		<SettingGroup title={t('agent.plan.ran')}>
			{#each ran as chat (chat.id)}
				<button type="button" class="srow" onclick={() => onopenchat(chat.id)}>
					<div class="srow-head">
						<span class="srow-ico"
							><Icon name={chat.state === 'waiting' ? 'shield' : 'check'} size={19} /></span
						>
						<div class="srow-text">
							<span class="srow-title">{chat.title}</span>
							<p class="srow-desc">
								{formatDateTime(new Date(chat.updated))}{chat.preview ? ' · ' + chat.preview : ''}
							</p>
						</div>
						<span class="srow-chev"><Icon name="chevronRight" size={16} /></span>
					</div>
				</button>
			{/each}
		</SettingGroup>
	{/if}
</PageShell>

<style>
	button.srow {
		display: block;
		width: 100%;
		font: inherit;
		color: inherit;
		text-align: left;
		cursor: pointer;
	}
	button.srow:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
</style>
