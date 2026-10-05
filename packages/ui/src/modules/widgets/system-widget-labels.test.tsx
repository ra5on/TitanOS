// @vitest-environment jsdom

import {createInstance} from 'i18next'
import {act} from 'react'
import {createRoot, type Root} from 'react-dom/client'
import {I18nextProvider} from 'react-i18next'
import {afterEach, beforeAll, describe, expect, it, vi} from 'vitest'

import de from '../../../public/locales/de.json'
import en from '../../../public/locales/en.json'
import {ExampleWidget, Widget} from './index'
import {liveUsageWidgets, type WidgetConfig} from './shared/constants'
import {localizeSystemStatsWidget, localizeSystemTextWidget} from './shared/system-widget-labels'

const fixtures = vi.hoisted(() => ({widget: undefined as unknown}))
vi.mock('@/trpc/trpc', () => ({
	trpcReact: {widget: {data: {useQuery: () => ({data: fixtures.widget, isLoading: false, isError: false})}}},
}))
vi.mock('@/providers/apps', () => ({
	useApps: () => ({userAppsKeyed: {}, systemAppsKeyed: {}, isLoading: false}),
}))
vi.mock('@/hooks/use-launch-app', () => ({useLaunchApp: () => vi.fn()}))
vi.mock('@/hooks/use-temperature-unit', () => ({
	useTemperatureUnit: () => ['c'],
	temperatureDescriptionsKeyed: {c: {label: '℃'}},
}))
vi.mock('react-router-dom', () => ({useNavigate: () => vi.fn()}))
vi.mock('@/components/ui/toast', () => ({toast: {error: vi.fn()}}))
vi.mock('@/features/files/widgets', () => ({
	filesWidgetTypes: ['files-list', 'files-grid'],
	FilesGridWidget: () => null,
	FilesListWidget: () => null,
}))
vi.mock('./shared/shared', () => ({
	WidgetContainer: ({children}: {children: React.ReactNode}) => <div>{children}</div>,
	widgetTextCva: () => '',
}))
vi.mock('./shared/tabler-icon', () => ({TablerIcon: () => null}))
vi.mock('@/hooks/use-is-mobile', () => ({useIsMobile: () => false}))

const i18n = createInstance()
beforeAll(async () => {
	await i18n.init({lng: 'de', fallbackLng: 'en', resources: {de: {translation: de}, en: {translation: en}}})
})
;(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true
let root: Root | undefined
let container: HTMLDivElement
afterEach(() => {
	if (root) act(() => root?.unmount())
	root = undefined
	document.body.replaceChildren()
})

function render(widgetId: string, preview = false) {
	if (!root) {
		container = document.createElement('div')
		document.body.appendChild(container)
		root = createRoot(container)
	}
	const config = liveUsageWidgets.find((widget) => widget.id === widgetId)!
	act(() =>
		root?.render(
			<I18nextProvider i18n={i18n}>
				{preview ? (
					<ExampleWidget widgetId={config.id} type={config.type} example={config.example} />
				) : (
					<Widget appId='live-usage' config={config} />
				)}
			</I18nextProvider>,
		),
	)
	return container.textContent ?? ''
}

describe('localized desktop system widgets', () => {
	it('keeps app-provided text, unknown stats, paths and loading placeholders untouched', () => {
		const t = i18n.getFixedT('de')
		const data = {title: 'Memory', progressLabel: '11.4 GB left', link: '/Home/Memory'}
		expect(localizeSystemTextWidget('sample-app:memory', data, t)).toBe(data)
		expect(localizeSystemTextWidget('titan:memory', data, t)?.link).toBe('/Home/Memory')
		const stats: WidgetConfig<'three-stats'> = {type: 'three-stats', items: liveUsageWidgets[2].example?.items}
		expect(localizeSystemStatsWidget('sample-app:system-stats', stats, t)).toBe(stats)
		expect(localizeSystemTextWidget('titan:memory', undefined, t)).toBeUndefined()
		expect(localizeSystemStatsWidget('titan:system-stats', undefined, t)).toBeUndefined()
	})

	it('renders real storage/memory data in German and updates on a language switch', async () => {
		await i18n.changeLanguage('de')
		const storage: WidgetConfig<'text-with-progress'> = {
			type: 'text-with-progress',
			title: 'Storage',
			text: '2.1 GB',
			subtext: '/ 13 GB',
			progressLabel: '10.9 GB left',
			progress: 0.16,
			link: '?dialog=live-usage&live-usage-tab=storage',
		}
		fixtures.widget = storage
		expect(render('titan:storage')).toContain('Speicherplatz')
		expect(container.textContent).toContain('10.9 GB verbleibend')
		expect(container.textContent).not.toMatch(/Storage|left/)
		await act(async () => {
			await i18n.changeLanguage('en')
		})
		expect(container.textContent).toContain('Storage')
		expect(container.textContent).toContain('10.9 GB left')
		await act(async () => {
			await i18n.changeLanguage('de')
		})
		fixtures.widget = {...storage, title: 'Memory'}
		expect(render('titan:memory')).toContain('Arbeitsspeicher')
		expect(storage.title).toBe('Storage')
		expect(storage.link).toBe('?dialog=live-usage&live-usage-tab=storage')
	})

	it('translates the live three-stat widget and every matching selector preview', async () => {
		await i18n.changeLanguage('de')
		fixtures.widget = {type: 'three-stats', items: liveUsageWidgets[2].example?.items}
		expect(render('titan:system-stats')).toContain('CPU')
		expect(container.textContent).toContain('Arbeitsspeicher')
		expect(container.textContent).toContain('Speicherplatz')
		for (const widget of liveUsageWidgets) {
			const text = render(widget.id, true)
			expect(text).not.toMatch(/Memory|Storage|left/)
			expect(text).toMatch(/Arbeitsspeicher|Speicherplatz/)
		}
	})
})
