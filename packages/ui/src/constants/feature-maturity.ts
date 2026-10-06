export type FeatureMaturity = 'stable' | 'alpha' | 'beta'
export type TitanFeature = 'files' | 'photos' | 'machines' | 'app-store' | 'mcp'

const featureIds: readonly TitanFeature[] = ['files', 'photos', 'machines', 'app-store', 'mcp']

/**
 * Optional build configuration, e.g. VITE_FEATURE_MATURITY='{"mcp":"beta"}'.
 * Unspecified features are stable. A release channel never implies feature maturity.
 */
export function parseFeatureMaturity(value: string | undefined): Partial<Record<TitanFeature, FeatureMaturity>> {
	if (!value) return {}
	try {
		const parsed: unknown = JSON.parse(value)
		if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
		return Object.fromEntries(
			Object.entries(parsed).filter(
				([feature, maturity]) =>
					featureIds.includes(feature as TitanFeature) && ['stable', 'alpha', 'beta'].includes(maturity),
			),
		)
	} catch {
		return {}
	}
}

const configuredMaturity = parseFeatureMaturity(import.meta.env.VITE_FEATURE_MATURITY)

export function getFeatureMaturity(feature: TitanFeature): FeatureMaturity {
	return configuredMaturity[feature] ?? 'stable'
}
