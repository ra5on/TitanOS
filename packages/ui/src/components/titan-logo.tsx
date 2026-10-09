import {SVGProps, useId} from 'react'

// Wordmark, compact emblem and emblem-free lettering share the supplied Titan
// artwork. The emblem fills x < 670 above y = 140 and x < 620 below it; the
// lettering spans x >= 640, y 150-520 below the emblem's wings.
function TitanLogo({
	style,
	ref,
	compact = false,
	lettering = false,
	...props
}: SVGProps<SVGSVGElement> & {ref?: React.Ref<SVGSVGElement>; compact?: boolean; lettering?: boolean}) {
	const clipId = useId()
	const clip = compact ? 'M0 0H670V140H620V520H0Z' : lettering ? 'M700 0H2060V520H640V140H700Z' : undefined
	return (
		<svg
			xmlns='http://www.w3.org/2000/svg'
			width={compact ? 72 : lettering ? 152 : 220}
			viewBox={compact ? '0 0 670 520' : lettering ? '640 150 1420 370' : '0 0 2060 520'}
			fill='none'
			{...props}
			style={{filter: 'drop-shadow(0 1px 2px rgba(255,255,255,.45))', ...style}}
			ref={ref}
			aria-label='Titan'
		>
			{clip && (
				<defs>
					<clipPath id={clipId}>
						<path d={clip} />
					</clipPath>
				</defs>
			)}
			<image
				href='/assets/titan-wordmark.svg'
				width={2060}
				height={520}
				clipPath={clip ? `url(#${clipId})` : undefined}
			/>
		</svg>
	)
}
export default TitanLogo
