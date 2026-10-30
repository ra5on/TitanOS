/**
 * Ready-made automations. Choosing one only fills the form; nothing is saved
 * until the person saves it. Texts are in the translation files under
 * automation.tpl.<id>.name and .prompt.
 */
export type TemplateTrigger = {
	kind: 'daily' | 'weekly' | 'event';
	time?: string;
	weekday?: number;
	event?: 'notification' | 'usb' | 'volume' | 'homeassistant';
	target?: string;
	percent?: number;
	to?: string;
};

export type AutomationTemplate = {
	id: string;
	trigger: TemplateTrigger;
	execution: 'inherit' | 'confirm';
	notify: 'errors' | 'always' | 'never';
	/** Event triggers other than folders are for owners and administrators. */
	admin: boolean;
	/** Needs Home Assistant to be connected. */
	homeAssistant?: boolean;
};

export const automationTemplates: AutomationTemplate[] = [
	{
		id: 'storageReport',
		trigger: { kind: 'weekly', time: '08:00', weekday: 1 },
		execution: 'inherit',
		notify: 'errors',
		admin: true,
	},
	{
		id: 'containerUpdates',
		trigger: { kind: 'daily', time: '19:00' },
		execution: 'inherit',
		notify: 'errors',
		admin: true,
	},
	{
		id: 'backupFailed',
		trigger: { kind: 'event', event: 'notification', target: 'backup_failed' },
		execution: 'inherit',
		notify: 'errors',
		admin: true,
	},
	{
		id: 'usbPhotos',
		trigger: { kind: 'event', event: 'usb', target: '' },
		execution: 'confirm',
		notify: 'always',
		admin: true,
	},
	{
		id: 'volumeFull',
		trigger: { kind: 'event', event: 'volume', target: '', percent: 85 },
		execution: 'inherit',
		notify: 'errors',
		admin: true,
	},
	{
		id: 'nobodyHome',
		trigger: { kind: 'event', event: 'homeassistant', target: '', to: 'not_home' },
		execution: 'inherit',
		notify: 'errors',
		admin: true,
		homeAssistant: true,
	},
];

/** The templates an account can use. */
export function templatesFor(admin: boolean, homeAssistant: boolean): AutomationTemplate[] {
	return automationTemplates.filter(
		(tpl) => (admin || !tpl.admin) && (homeAssistant || !tpl.homeAssistant),
	);
}
