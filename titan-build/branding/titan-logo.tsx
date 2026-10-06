import {SVGProps} from 'react'

// Titan's own T mark, shared by login, onboarding and system dialogs.
function TitanLogo({style, ref, ...props}: SVGProps<SVGSVGElement> & {ref?: React.Ref<SVGSVGElement>}) {
	return (
		<svg xmlns='http://www.w3.org/2000/svg' width={96} viewBox='0 0 96 47' fill='none' {...props} style={style} ref={ref}>
			<path fill='currentColor' d='M8 7H88V18H55V42H41V18H8Z' />
		</svg>
	)
}

export default TitanLogo
