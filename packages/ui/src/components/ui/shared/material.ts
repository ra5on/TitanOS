import {tw} from '@/utils/tw'

/**
 * Semantic material roles. The visual values live in CSS custom properties in
 * index.css so designers can tune the whole system without touching component
 * markup.
 */
export const materialSurfaceClasses = {
	contextMenu: tw`titan-material titan-material-context-menu`,
	dropdown: tw`titan-material titan-material-dropdown`,
	popover: tw`titan-material titan-material-popover`,
	modal: tw`titan-material titan-material-modal`,
	toast: tw`titan-material titan-material-toast`,
}

export const floatingContentAnimationClass = tw`animate-in fade-in-80 data-[state=open]:animate-in data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2`
