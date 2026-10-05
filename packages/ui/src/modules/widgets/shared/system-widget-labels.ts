import type {TFunction} from 'i18next'

import type {TextWithProgressWidgetProps, ThreeStatsWidgetProps} from './constants'

// System widgets arrive from the OS in a fixed transport format. Translate
// only their known labels here, so app-provided widget text stays untouched.
export function localizeSystemTextWidget(
	widgetId: string | undefined,
	widget: TextWithProgressWidgetProps | undefined,
	t: TFunction,
): TextWithProgressWidgetProps | undefined {
	if (!widget || (widgetId !== 'titan:storage' && widgetId !== 'titan:memory')) return widget
	const remaining = widget.progressLabel?.match(/^(.+) left$/)?.[1]
	return {
		...widget,
		title: widgetId === 'titan:storage' ? t('storage') : t('memory'),
		progressLabel: remaining ? t('something-left', {left: remaining}) : widget.progressLabel,
	}
}

export function localizeSystemStatsWidget(
	widgetId: string | undefined,
	widget: ThreeStatsWidgetProps | undefined,
	t: TFunction,
): ThreeStatsWidgetProps | undefined {
	if (!widget?.items || widgetId !== 'titan:system-stats') return widget
	return {
		...widget,
		items: widget.items.map((item) => {
			const subtext =
				item.icon === 'system-widget-cpu'
					? t('cpu')
					: item.icon === 'system-widget-memory'
						? t('memory')
						: item.icon === 'system-widget-storage'
							? t('storage')
							: item.subtext
			return {...item, subtext}
		}) as ThreeStatsWidgetProps['items'],
	}
}
