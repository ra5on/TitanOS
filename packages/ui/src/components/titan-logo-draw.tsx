import {motion, useReducedMotion} from 'motion/react'
import {useEffect} from 'react'

import {cn} from '@/lib/utils'

// Titan's own T mark with an onboarding entrance that respects reduced motion.
export function TitanLogoDraw({
	className,
	restOpacity = 0.85,
	delay = 0,
	speed = 1,
	onComplete,
}: {
	className?: string
	restOpacity?: number
	delay?: number
	speed?: number
	onComplete?: () => void
}) {
	const reducedMotion = useReducedMotion()
	useEffect(() => {
		if (reducedMotion) onComplete?.()
	}, [reducedMotion, onComplete])

	return (
		<motion.svg
			xmlns='http://www.w3.org/2000/svg'
			width={220}
			viewBox='0 0 2060 520'
			fill='none'
			aria-hidden
			className={cn('overflow-visible', className)}
			initial={reducedMotion ? false : {opacity: 0, y: 4}}
			animate={{opacity: restOpacity, y: 0}}
			transition={{duration: reducedMotion ? 0 : 0.7 / Math.max(0.1, speed), delay: reducedMotion ? 0 : delay}}
			onAnimationComplete={reducedMotion ? undefined : onComplete}
		>
			<image
				href='/assets/titan-wordmark.svg'
				width={2060}
				height={520}
				style={{filter: 'drop-shadow(0 1px 2px rgba(255,255,255,.45))'}}
			/>
		</motion.svg>
	)
}

export default TitanLogoDraw
