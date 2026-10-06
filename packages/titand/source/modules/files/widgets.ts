import type Titand from '../../index.js'

export const filesWidgets = {
	'files-recents': async function (titand: Titand, accountId: string) {
		const recentFiles = await titand.files.recents.get(accountId)

		return {
			type: 'files-list',
			link: '/files/Recents',
			refresh: '5s',
			items: recentFiles.slice(0, 3),
			noItemsText: 'files-widgets.recents.no-items-text',
		}
	},

	'files-favorites': async function (titand: Titand, accountId: string) {
		const favorites = await titand.files.favorites.listFavorites(accountId)

		return {
			type: 'files-grid',
			refresh: '30s',
			paths: favorites.slice(0, 4),
			noItemsText: 'files-widgets.favorites.no-items-text',
		}
	},
} as const
