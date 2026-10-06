import {t} from '@/utils/i18n'

export function secondsToEta(seconds: number | null | undefined): string {
	if (seconds == null || seconds <= 0 || !Number.isFinite(seconds)) {
		return '-'
	}

	if (seconds < 60) return t('duration.short.seconds', {count: Math.round(seconds)})
	if (seconds < 3600) return t('duration.short.minutes', {count: Math.round(seconds / 60)})

	const totalMinutes = Math.round(seconds / 60)
	return t('duration.short.hours-minutes', {hours: Math.floor(totalMinutes / 60), minutes: totalMinutes % 60})
}
