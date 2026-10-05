import {t} from '@/utils/i18n'

export function getUserErrorMessage(error: unknown) {
	const message = error instanceof Error ? error.message : String(error)
	if (message.includes('A user with this name already exists')) return t('users.error.name-taken')
	if (message.includes('User not found')) return t('users.error.not-found')
	if (message.includes('The owner account cannot be deleted')) return t('users.error.owner-delete')
	if (message.includes("The owner's password cannot be reset")) return t('users.error.owner-password')
	return t('users.error.generic')
}
