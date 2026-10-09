import {SVGProps, useId} from 'react'

// Wordmark and compact emblem share the supplied Titan artwork.
function TitanLogo({
	style,
	ref,
	compact = false,
	...props
}: SVGProps<SVGSVGElement> & {ref?: React.Ref<SVGSVGElement>; compact?: boolean}) {
	const clipId = useId()
	return (
		<svg
			xmlns='http://www.w3.org/2000/svg'
			width={compact ? 72 : 220}
			viewBox={compact ? '0 0 670 520' : '0 0 2060 520'}
			fill='none'
			{...props}
			style={{filter: 'drop-shadow(0 1px 2px rgba(255,255,255,.45))', ...style}}
			ref={ref}
			aria-label='Titan'
		>
			<defs>
				<clipPath id={clipId}>
					<path d='M0 0H670V140H620V520H0Z' />
				</clipPath>
			</defs>
			<image
				href='/assets/titan-wordmark.svg'
				width={2060}
				height={520}
				clipPath={compact ? `url(#${clipId})` : undefined}
			/>
		</svg>
	)
}
export default TitanLogo
