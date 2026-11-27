import { api } from '#lib/api.ts';
import { t } from '#lib/i18n/index.svelte.ts';

export type LocalModel = {
	state: 'ready' | 'missing' | 'offline' | 'checking' | 'unavailable';
	model?: string;
};

export const usesLocalModel = (id: string) =>
	['chat.ask', 'mail.summarize', 'mail.reply', 'mail.event'].includes(id);

export const localReadiness = (signal: AbortSignal) =>
	api.get<LocalModel>('/personal-agent/model/readiness', signal);

export function modelStatus(value: LocalModel) {
	if (value.state === 'ready') return t('papim.model.ready', { model: value.model ?? '' });
	if (value.state === 'missing')
		return value.model
			? t('papim.model.selectedMissing', { model: value.model })
			: t('papim.model.empty');
	if (value.state === 'offline') return t('papim.model.offline');
	return value.state === 'checking' ? t('papim.model.checking') : t('papim.model.unavailable');
}
