import {motion, useReducedMotion} from 'motion/react'
import {useEffect} from 'react'

import {cn} from '@/lib/utils'

// This is Titan's original T mark. The exported API stays compatible with
// onboarding, without retaining the upstream trademark's geometry.
export function UmbrelLogoDraw({
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
			width={96}
			viewBox='0 0 96 47'
			fill='none'
			aria-hidden
			className={cn('overflow-visible', className)}
			initial={reducedMotion ? false : {opacity: 0, y: 4}}
			animate={{opacity: restOpacity, y: 0}}
			transition={{duration: reducedMotion ? 0 : 0.7 / Math.max(0.1, speed), delay: reducedMotion ? 0 : delay}}
			onAnimationComplete={reducedMotion ? undefined : onComplete}
		>
			<path fill='currentColor' d='M8 7H88V18H55V42H41V18H8Z' />
		</motion.svg>
	)
}

export default UmbrelLogoDraw
