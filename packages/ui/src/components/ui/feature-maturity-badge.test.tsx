// @vitest-environment jsdom

import {act} from 'react'
import {createRoot, type Root} from 'react-dom/client'
import {afterEach, expect, it, vi} from 'vitest'

import {FeatureMaturityBadge, MaturityBadge} from './feature-maturity-badge'

vi.mock('react-i18next', () => ({useTranslation: () => ({t: (key: string) => key})}))
;(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true
let root: Root | undefined
afterEach(() => {
	if (root) act(() => root?.unmount())
	document.body.replaceChildren()
})

it('renders nothing for stable and unconfigured features and identifies explicit previews', () => {
	const container = document.createElement('div')
	root = createRoot(container)
	act(() => root?.render(<FeatureMaturityBadge feature='mcp' />))
	expect(container.innerHTML).toBe('')
	act(() => root?.render(<MaturityBadge maturity='alpha' />))
	expect(container.textContent).toBe('alpha')
	act(() => root?.render(<MaturityBadge maturity='beta' />))
	expect(container.textContent).toBe('beta')
	act(() => root?.render(<MaturityBadge maturity='stable' />))
	expect(container.innerHTML).toBe('')
})
