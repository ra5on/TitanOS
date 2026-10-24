<script lang="ts">
	import { matchesSetting, settingSections } from '#lib/settings-search.ts';
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const areas: { href: string; label: MessageKey; admin?: boolean }[] = [
		{ href: 'appearance', label: 'menu.appearance' },
		{ href: 'network', label: 'netcfg.title', admin: true },
		{ href: 'notifications', label: 'notify.settingsTitle', admin: true },
		{ href: 'updates', label: 'update.title', admin: true },
		{ href: 'access', label: 'access.title', admin: true },
		{ href: 'assistant', label: 'assistant.settingsTitle', admin: true },
		{ href: 'backups', label: 'safeguard.title', admin: true },
		{ href: 'timezone', label: 'settings.datetime', admin: true },
		{ href: 'ups', label: 'ups.title', admin: true },
		{ href: 'homeassistant', label: 'ha.title', admin: true },
		{ href: 'hardware', label: 'hardware.title', admin: true },
		{ href: 'power-button', label: 'powerButton.title', admin: true },
		{ href: 'memory', label: 'memoryPolicy.title', admin: true },
		{ href: 'energy', label: 'energy.title', admin: true },
		{ href: 'temperatures', label: 'temperatures.title' },
		{ href: 'about', label: 'about.title' },
	];
	const groups = ['system', 'network', 'storage', 'integrations', 'personal'] as const;
	const grouping: Record<string, (typeof groups)[number]> = {
		network: 'network',
		access: 'network',
		backups: 'storage',
		memory: 'storage',
		assistant: 'integrations',
		homeassistant: 'integrations',
		appearance: 'personal',
		notifications: 'personal',
		temperatures: 'personal',
	};
	const icons: Record<string, IconName> = {
		appearance: 'palette',
		network: 'globe',
		notifications: 'bell',
		updates: 'download',
		access: 'key',
		assistant: 'chat',
		backups: 'archive',
		timezone: 'clock',
		ups: 'power',
		homeassistant: 'globe',
		hardware: 'monitor',
		'power-button': 'power',
		memory: 'grid',
		energy: 'power',
		temperatures: 'activity',
		about: 'fileText',
	};
	const sortedAreas = $derived(
		areas
			.filter((area) => !area.admin || admin)
			.toSorted((a, b) => t(a.label).localeCompare(t(b.label), i18n.locale)),
	);

	let search = $state('');
	const visibleAreas = $derived(
		sortedAreas.flatMap((area) => {
			const sections = search.trim()
				? (settingSections[area.href] ?? []).filter((section) =>
						matchesSetting(search, `${t(area.label)} ${t(section.label)}`, section.id),
					)
				: [];
			return sections.length
				? sections.map((section) => ({
						href: section.href,
						label: `${t(area.label)} › ${t(section.label)}`,
						group: grouping[area.href] ?? 'system',
						icon: icons[area.href],
					}))
				: matchesSetting(search, t(area.label), area.href)
					? [
							{
								href: `/settings/${area.href}`,
								label: t(area.label),
								group: grouping[area.href] ?? 'system',
								icon: icons[area.href],
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
						></a
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
