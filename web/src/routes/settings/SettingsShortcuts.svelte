<script lang="ts">
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import type { IconName } from '#lib/components/Icon.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { recentlyOpened } from '#lib/settings-opened.ts';

	let { admin }: { admin: boolean } = $props();
	const opened = $derived(recentlyOpened(session.data?.user?.name ?? '', admin));
	const tasks: { href: string; icon: IconName; title: MessageKey; admin?: boolean }[] = [
		{
			href: '/settings/network?tab=remote',
			icon: 'tailnet',
			title: 'settingshome.taskRemote',
			admin: true,
		},
		{
			href: '/settings/backups/restore#restore-files',
			icon: 'folder',
			title: 'safeguard.filesRestore',
			admin: true,
		},
		{
			href: '/settings/energy#disk-sleep',
			icon: 'disk',
			title: 'settingshome.taskSleep',
			admin: true,
		},
		{ href: '/account#acc-password', icon: 'lock', title: 'account.changePassword' },
	];
</script>

<SettingGroup title={t('settingshome.tasks')} id="settings-tasks">
	{#each tasks.filter((task) => admin || !task.admin) as task (task.href)}
		<SettingRow href={task.href} icon={task.icon} title={t(task.title)} />
	{/each}
</SettingGroup>

{#if opened.length}
	<SettingGroup title={t('settingshome.opened')} id="settings-opened">
		{#each opened as area (area.id)}
			<SettingRow href={'/settings/' + area.id} icon={area.icon} title={t(area.label)} />
		{/each}
	</SettingGroup>
{/if}
