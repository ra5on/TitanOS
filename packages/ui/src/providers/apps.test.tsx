// @vitest-environment jsdom

import {act} from 'react'
import {createRoot, type Root} from 'react-dom/client'
import {afterEach, expect, it, vi} from 'vitest'

import {AppsProvider, systemAppsKeyed, useApps} from './apps'

const fixtures = vi.hoisted(() => ({language: 'en'}))
const dictionaries: Record<string, Record<string, string>> = {
	en: {files: 'Files', photos: 'Photos', settings: 'Settings', 'machines.titan-machines': 'Machines'},
	de: {
		files: 'Dateien',
		photos: "Foto's",
		settings: 'Einstellungen',
		'machines.titan-machines': 'Virtuelle Maschinen',
	},
}
const translate = (key: string) => dictionaries[fixtures.language][key] ?? key
vi.mock('react-i18next', () => ({useTranslation: () => ({t: (key: string) => translate(key)})}))
vi.mock('@/utils/i18n', () => ({t: (key: string) => translate(key)}))
vi.mock('@/hooks/use-app-install', () => ({pollStates: []}))
vi.mock('@/utils/misc', () => ({
	keyBy: (items: {id: string}[]) => Object.fromEntries(items.map((item) => [item.id, item])),
}))
vi.mock('@/trpc/trpc', () => ({
	trpcReact: {
		apps: {
			list: {
				useQuery: () => ({
					data: [{id: 'third-party', name: 'Original App Name', icon: '/original.svg'}],
					isLoading: false,
				}),
			},
		},
		user: {get: {useQuery: () => ({data: {role: 'owner'}})}},
		useUtils: () => ({}),
		eventBus: {listen: {useSubscription: () => {}}},
	},
}))
;(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true
let root: Root | undefined
afterEach(() => {
	if (root) act(() => root?.unmount())
	document.body.replaceChildren()
})

function Probe() {
	const apps = useApps()
	return (
		<div>
			{apps.allApps.map((app) => (
				<span key={app.id} data-id={app.id}>
					{app.name}
				</span>
			))}
		</div>
	)
}

it('updates every provider consumer on a language change without renaming installed apps or route IDs', () => {
	const container = document.createElement('div')
	root = createRoot(container)
	fixtures.language = 'en'
	act(() =>
		root?.render(
			<AppsProvider>
				<Probe />
			</AppsProvider>,
		),
	)
	expect(container.querySelector('[data-id="TITAN_files"]')?.textContent).toBe('Files')
	fixtures.language = 'de'
	act(() =>
		root?.render(
			<AppsProvider>
				<Probe />
			</AppsProvider>,
		),
	)
	expect(container.querySelector('[data-id="TITAN_files"]')?.textContent).toBe('Dateien')
	expect(container.querySelector('[data-id="TITAN_photos"]')?.textContent).toBe("Foto's")
	expect(container.querySelector('[data-id="TITAN_machines"]')?.textContent).toBe('Virtuelle Maschinen')
	expect(container.querySelector('[data-id="third-party"]')?.textContent).toBe('Original App Name')
	expect(systemAppsKeyed.TITAN_files.systemAppTo).toBe('/files')
})
