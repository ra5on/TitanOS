<script lang="ts">
	import { matchesSetting, settingSections, settingsAreas } from '#lib/settings-search.ts';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { api } from '#lib/api.ts';
	import { notifications } from '#lib/notifications.svelte.ts';
	import { onMount } from 'svelte';

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const groups = ['system', 'network', 'storage', 'integrations', 'personal'] as const;
	const sortedAreas = $derived(
		settingsAreas
			.filter((area) => !area.admin || admin)
			.toSorted((a, b) => t(a.label).localeCompare(t(b.label), i18n.locale)),
	);

	// A short state on some tiles. Read once when the page opens, only from
	// answers titand keeps in RAM (no polling, no stick or volume access);
	// overdue backups come from the messages the bell has loaded anyway.
	type TileState = { text: MessageKey; tone: 'warn' | 'error' | 'info' };
	let readStates = $state<Record<string, TileState>>({});
	onMount(() => {
		if (!admin) return;
		void Promise.allSettled([
			api.get<{ available: unknown; pending?: unknown }>('/update'),
			api.get<{ status?: { onBattery?: boolean; lowBattery?: boolean } }>('/ups'),
			api.get<{ notAfter: string }>('/system/certificate'),
		]).then(([update, ups, certificate]) => {
			const next: Record<string, TileState> = {};
			if (update.status === 'fulfilled' && update.value?.available)
				next.updates = { text: 'settings.state.update', tone: 'info' };
			const power = ups.status === 'fulfilled' ? ups.value?.status : undefined;
			if (power?.onBattery)
				next.ups = {
					text: power.lowBattery ? 'settings.state.lowBattery' : 'settings.state.onBattery',
					tone: power.lowBattery ? 'error' : 'warn',
				};
			if (certificate.status === 'fulfilled' && certificate.value?.notAfter) {
				const days = (Date.parse(certificate.value.notAfter) - Date.now()) / 86_400_000;
				if (days < 0) next.access = { text: 'settings.state.certExpired', tone: 'error' };
				else if (days < 14) next.access = { text: 'settings.state.certExpiring', tone: 'warn' };
			}
			readStates = next;
		});
	});
	const tileStates = $derived.by(() => {
		const states = { ...readStates };
		// Per backup job, the newest of its messages tells whether it is overdue.
		const newest = new Map<string, string>();
		for (const notice of notifications.list)
			if (notice.kind.startsWith('backup_') && !newest.has(notice.subject))
				newest.set(notice.subject, notice.kind);
		if ([...newest.values()].includes('backup_overdue'))
			states.backups = { text: 'settings.state.backupOverdue', tone: 'warn' };
		return states;
	});

	let search = $state('');
	const visibleAreas = $derived(
		sortedAreas.flatMap((area) => {
			const sections = search.trim()
				? (settingSections[area.id] ?? []).filter((section) =>
						matchesSetting(search, `${t(area.label)} ${t(section.label)}`, section.id),
					)
				: [];
			return sections.length
				? sections.map((section) => ({
						href: section.href,
						id: area.id,
						label: `${t(area.label)} › ${t(section.label)}`,
						group: area.group,
						icon: area.icon,
					}))
				: matchesSetting(search, t(area.label), area.id)
					? [
							{
								href: `/settings/${area.id}`,
								id: area.id,
								label: t(area.label),
								group: area.group,
								icon: area.icon,
							},
						]
					: [];
		}),
	);
	const resultCount = $derived(visibleAreas.length);
</script>

<PageHead title={t('settings.title')} />

<label class="search"
	><span>{t('ui.searchSettings')}</span><input
		class="field"
		type="search"
		bind:value={search}
		placeholder={t('ui.searchHint')}
	/></label
>
{#if search}<div class="search-result">
		<span role="status">{t('ui.results', { count: resultCount })}</span><button
			class="btn"
			type="button"
			onclick={() => (search = '')}>{t('ui.clear')}</button
		>
	</div>{/if}
{#if search && resultCount === 0}<p>{t('ui.noSettings')}</p>{/if}
{#each groups as group}
	{@const entries = visibleAreas.filter((area) => area.group === group)}
	{#if entries.length}<section class="settings-group" aria-labelledby="group-{group}">
			<h2 class="tile-title" id="group-{group}">{t(`settings.group.${group}` as MessageKey)}</h2>
			<div class="areas">
				{#each entries as area (area.href)}
					<a class="tile" href={area.href}
						><span class="setting-icon"><Icon name={area.icon ?? 'tool'} size={26} /></span><span
							class="name">{area.label}</span
						>{#if tileStates[area.id] && !search}<span class="chip {tileStates[area.id].tone}"
								>{t(tileStates[area.id].text)}</span
							>{/if}</a
					>
				{/each}
			</div>
		</section>{/if}
{/each}

<style>
	.settings-group {
		display: grid;
		gap: 14px;
	}
	.setting-icon {
		color: var(--accent);
		display: flex;
	}
	.search {
		display: grid;
		gap: 8px;
		width: 100%;
	}
	.search-result {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.areas {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 180px), 1fr));
		gap: 10px;
	}
	a {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 12px;
		min-height: 96px;
		flex-direction: column;
		text-align: center;
		padding: 10px 14px;
		color: var(--text);
		text-decoration: none;
		font-size: 0.875rem;
	}
	a:hover,
	a:focus-visible {
		border-color: var(--accent);
	}
	.name {
		font-size: 0.9375rem;
		font-weight: 500;
	}
</style>
