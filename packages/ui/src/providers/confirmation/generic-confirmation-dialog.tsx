import React, {useEffect, useId, useRef, useState} from 'react'
import {useTranslation} from 'react-i18next'

import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {Checkbox, checkboxContainerClass, checkboxLabelClass} from '@/components/ui/checkbox'
import {cn} from '@/lib/utils'
import type {ConfirmationOptions, ConfirmationResult} from '@/providers/confirmation/types'

interface GenericConfirmationDialogProps {
	isOpen: boolean
	options: ConfirmationOptions | null
	onResolve: (result: ConfirmationResult) => void
	onReject: (reason?: any) => void
}

export const GenericConfirmationDialog: React.FC<GenericConfirmationDialogProps> = ({
	isOpen,
	options,
	onResolve,
	onReject,
}) => {
	const {t} = useTranslation()
	const [applyToAllChecked, setApplyToAllChecked] = useState(false)
	const checkboxId = useId()
	const cancelButton = useRef<HTMLButtonElement>(null)

	// Reset checkbox state when dialog options change (i.e., a new confirmation opens)
	useEffect(() => {
		if (isOpen) {
			setApplyToAllChecked(false)
		}
	}, [isOpen, options])

	if (!options) {
		// Render nothing if options are null (e.g., during fade-out animation or initial state)
		return null
	}

	const {title, message, actions, icon: IconComponent, showApplyToAll} = options
	const isYesNo =
		actions.length === 2 &&
		actions.some((action) => action.label === t('yes')) &&
		actions.some((action) => action.label === t('no'))
	const orderedActions = isYesNo ? [...actions].sort((a) => (a.label === t('yes') ? -1 : 1)) : actions

	// If the action represents a user cancellation (with the value "cancel"),
	// propagate the promise rejection so callers can distinguish cancellation from
	// other confirmed actions. Otherwise, resolve with the chosen value.
	const handleActionClick = (value: string | number) => {
		if (value === 'cancel') {
			// Treat this as an explicit cancellation
			onReject('cancel')
			return
		}
		onResolve({actionValue: value, applyToAll: showApplyToAll ? applyToAllChecked : false})
	}

	// Use onOpenChange for dismissal (clicking outside, pressing Esc)
	const handleOpenChange = (open: boolean) => {
		if (!open) {
			// Trigger reject only if the dialog was intentionally closed by the user
			// without choosing an action.
			onReject('Dialog dismissed by user')
		}
	}

	return (
		<AlertDialog open={isOpen} onOpenChange={handleOpenChange}>
			<AlertDialogContent
				onOpenAutoFocus={(event) => {
					if (!isYesNo) return
					event.preventDefault()
					cancelButton.current?.focus()
				}}
			>
				<AlertDialogHeader icon={IconComponent}>
					<AlertDialogTitle>{title}</AlertDialogTitle>
					{message && <AlertDialogDescription>{message}</AlertDialogDescription>}
				</AlertDialogHeader>

				{/* Action Buttons */}
				<div
					dir={isYesNo ? 'ltr' : undefined}
					className={
						isYesNo
							? 'flex flex-row justify-center gap-2'
							: 'flex flex-col justify-center gap-y-2 md:flex-row md:gap-x-2 md:gap-y-0'
					}
				>
					{orderedActions.map((action) => (
						<AlertDialogAction
							key={action.label}
							ref={isYesNo && action.label === t('no') ? cancelButton : undefined}
							variant={action.variant || 'default'}
							className={isYesNo ? 'min-w-0 flex-1 px-3' : 'px-6'}
							onClick={() => handleActionClick(action.value)}
						>
							{action.label}
						</AlertDialogAction>
					))}
				</div>

				{/* "Apply to all" checkbox (only if enabled) */}
				{showApplyToAll && (
					<AlertDialogFooter>
						<div className={cn(checkboxContainerClass)}>
							<Checkbox
								id={checkboxId}
								checked={applyToAllChecked}
								onCheckedChange={(checked) => setApplyToAllChecked(!!checked)}
								className='h-4 w-4 rounded-4'
							/>
							<label htmlFor={checkboxId} className={cn(checkboxLabelClass, 'text-12 text-white/40')}>
								{t('confirmation.apply-to-all')}
							</label>
						</div>
					</AlertDialogFooter>
				)}
			</AlertDialogContent>
		</AlertDialog>
	)
}
