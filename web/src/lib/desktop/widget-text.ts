// The name and the line of explanation of each widget kind (desktop gallery, PLAN 87 phone).
import type { MessageKey } from '../i18n/index.svelte.ts';
import type { WidgetKind } from './prefs.ts';

export const WIDGET_TEXT: Record<WidgetKind, [MessageKey, MessageKey]> = {
	status: ['desktop.widget.status', 'desktop.gallery.status'],
	cpuram: ['desktop.widget.cpuram', 'desktop.gallery.cpuram'],
	network: ['desktop.widget.network', 'desktop.gallery.network'],
	volumes: ['desktop.widget.volumes', 'desktop.gallery.volumes'],
	docker: ['desktop.widget.docker', 'desktop.gallery.docker'],
	vms: ['desktop.widget.vms', 'desktop.gallery.vms'],
	backups: ['desktop.widget.backups', 'desktop.gallery.backups'],
	temperatures: ['desktop.widget.temperatures', 'desktop.gallery.temperatures'],
	clock: ['desktop.widget.clock', 'desktop.gallery.clock'],
	homeassistant: ['desktop.widget.homeassistant', 'desktop.gallery.homeassistant'],
	tasks: ['desktop.widget.tasks', 'desktop.gallery.tasks'],
	igpu: ['desktop.widget.igpu', 'desktop.gallery.igpu'],
	npu: ['desktop.widget.npu', 'desktop.gallery.npu'],
	gpu: ['desktop.widget.gpu', 'desktop.gallery.gpu'],
	camera: ['desktop.widget.camera', 'desktop.gallery.camera'],
};
