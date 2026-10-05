import {useTranslation} from 'react-i18next'

import {getFeatureMaturity, type FeatureMaturity, type TitanFeature} from '@/constants/feature-maturity'

/** Stable features have no badge; previews must be explicitly configured. */
export function FeatureMaturityBadge({feature}: {feature: TitanFeature}) {
	return <MaturityBadge maturity={getFeatureMaturity(feature)} />
}

export function MaturityBadge({maturity}: {maturity: FeatureMaturity}) {
	const {t} = useTranslation()
	if (maturity === 'stable') return null
	return (
		<span className='rounded-full bg-brand/25 px-1.5 py-[3px] text-[9px] leading-none font-semibold tracking-[0.06em] text-brand-lightest uppercase'>
			{maturity === 'alpha' ? t('alpha') : t('beta')}
		</span>
	)
}
