import * as DialogPrimitive from '@radix-ui/react-dialog'
import * as React from 'react'

import {DialogCloseButton} from '@/components/ui/dialog-close-button'
import {ScrollArea} from '@/components/ui/scroll-area'
import {cn} from '@/lib/utils'

import {
	dialogContentAnimationClass,
	dialogContentAnimationSlideClass,
	dialogContentClass,
	dialogFooterClass,
	dialogOverlayClass,
	preventDialogDismissForToasts,
} from './shared/dialog'

const Dialog = DialogPrimitive.Root

const DialogTrigger = DialogPrimitive.Trigger

const DialogPortal = (props: DialogPrimitive.DialogPortalProps) => <DialogPrimitive.Portal {...props} />
DialogPortal.displayName = DialogPrimitive.Portal.displayName

function DialogOverlay({
	className,
	ref,
	...props
}: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay> & {
	ref?: React.Ref<React.ComponentRef<typeof DialogPrimitive.Overlay>>
}) {
	return <DialogPrimitive.Overlay ref={ref} className={cn(dialogOverlayClass, className)} {...props} />
}

function DialogContent({
	className,
	children,
	slide = true,
	onPointerDownOutside,
	ref,
	...props
}: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & {slide?: boolean} & {
	ref?: React.Ref<React.ComponentRef<typeof DialogPrimitive.Content>>
}) {
	return (
		<DialogPortal>
			<DialogOverlay />
			<DialogPrimitive.Content
				ref={ref}
				className={cn(
					dialogContentClass,
					dialogContentAnimationClass,
					slide && dialogContentAnimationSlideClass,
					'w-full max-w-[calc(100%-40px)] sm:max-w-[480px]',
					className,
				)}
				{...props}
				// Compose after the spread so a caller's handler adds to the toast
				// guard instead of replacing it
				onPointerDownOutside={(event) => {
					preventDialogDismissForToasts(event)
					onPointerDownOutside?.(event)
				}}
			>
				{children}
			</DialogPrimitive.Content>
		</DialogPortal>
	)
}

const DialogScrollableContent = ({
	children,
	showClose,
	fade = true,
	onOpenAutoFocus,
	onCloseAutoFocus,
	className,
}: {
	children: React.ReactNode
	showClose?: boolean
	fade?: boolean
	onOpenAutoFocus?: (e: Event) => void
	onCloseAutoFocus?: (e: Event) => void
	className?: string
}) => {
	return (
		<DialogContent
			className={cn('flex flex-col p-0', className)}
			onOpenAutoFocus={onOpenAutoFocus}
			onCloseAutoFocus={onCloseAutoFocus}
		>
			{/* TODO: adjust dialog inset if `showClose` is true so close button isn't too close to scrollbar */}
			<ScrollArea className='flex flex-col' dialogInset fade={fade}>
				{children}
			</ScrollArea>
			{showClose && <DialogCloseButton className='absolute top-2 right-2 z-50' />}
		</DialogContent>
	)
}

const DialogHeader = ({className, ...props}: React.HTMLAttributes<HTMLDivElement>) => (
	<div className={cn('flex flex-col space-y-1.5', className)} {...props} />
)
DialogHeader.displayName = 'DialogHeader'

const DialogFooter = ({className, ...props}: React.HTMLAttributes<HTMLDivElement>) => (
	<div className={cn(dialogFooterClass, className)} {...props} />
)
DialogFooter.displayName = 'DialogFooter'

function DialogTitle({
	className,
	ref,
	...props
}: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title> & {
	ref?: React.Ref<React.ComponentRef<typeof DialogPrimitive.Title>>
}) {
	return (
		<DialogPrimitive.Title
			ref={ref}
			className={cn('text-left text-17 leading-snug font-semibold -tracking-2', className)}
			{...props}
		/>
	)
}

function DialogDescription({
	className,
	ref,
	...props
}: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description> & {
	ref?: React.Ref<React.ComponentRef<typeof DialogPrimitive.Description>>
}) {
	return (
		<DialogPrimitive.Description
			ref={ref}
			className={cn('text-left text-13 leading-tight font-normal -tracking-2 text-white/40', className)}
			{...props}
		/>
	)
}

export {
	Dialog,
	DialogContent,
	DialogScrollableContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogPortal,
	DialogTitle,
	DialogTrigger,
}
