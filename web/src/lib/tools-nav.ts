// The areas of Tools, in the order of the sidebar (decision of the owner of
// 4 Nov 2026, variant B "Seitenleiste wie Einstellungen"): what to watch,
// what to look after, and help. Every tools page is one row here; the
// sidebar (desktop) and the overview list (phone) read the same list.
import type { IconName } from '#lib/components/Icon.svelte';
import type { MessageKey } from '#lib/i18n/index.svelte.ts';

export type ToolsGroup = 'observe' | 'maintain' | 'help';
export type ToolsEntry = { href: string; label: MessageKey; text: MessageKey; icon: IconName };

export const toolsGroups: { group: ToolsGroup; entries: ToolsEntry[] }[] = [
	{
		group: 'observe',
		entries: [
			{ href: '/tools/tasks', label: 'ops.tasks', text: 'tools.text.tasks', icon: 'list' },
			{
				href: '/tools/taskmanager',
				label: 'taskmanager.title',
				text: 'taskmanager.navText',
				icon: 'monitor',
			},
			{
				href: '/tools/monitoring',
				label: 'ops.monitoring',
				text: 'tools.text.monitoring',
				icon: 'activity',
			},
			{
				href: '/tools/activity',
				label: 'ops.activity',
				text: 'tools.text.activity',
				icon: 'clock',
			},
			{ href: '/tools/logs', label: 'tools.logs', text: 'tools.text.logs', icon: 'fileText' },
		],
	},
	{
		group: 'maintain',
		entries: [
			{
				href: '/tools/maintenance',
				label: 'ops.maintenance',
				text: 'tools.text.maintenance',
				icon: 'tool',
			},
			{
				href: '/tools/scripts',
				label: 'scripts.title',
				text: 'tools.text.scripts',
				icon: 'terminal',
			},
			{
				href: '/tools/hardware',
				label: 'hwcheck.title',
				text: 'tools.text.hardware',
				icon: 'chip',
			},
			{ href: '/tools/disks', label: 'disktest.title', text: 'tools.text.disks', icon: 'disk' },
		],
	},
	{
		group: 'help',
		entries: [
			{ href: '/tools/bundle', label: 'tools.bundle', text: 'tools.text.bundle', icon: 'download' },
		],
	},
];

/** Old anchors on the tools overview and the pages that replaced them. */
export const toolsAnchors: Record<string, string> = {
	'#tools-logs': '/tools/logs',
	'#tools-disks': '/tools/disks',
	'#tools-bundle': '/tools/bundle',
};
