import {ChevronDown, Loader2} from 'lucide-react'

import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuRadioGroup,
	DropdownMenuRadioItem,
	DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {SpecRow} from '@/features/machines/components/spec-form'
import type {MachineNetwork} from '@/features/machines/network-settings'
import {t} from '@/utils/i18n'

export function MachineNetworkField({
	value,
	onChange,
	bridges,
	disabled,
	note,
	isLoading,
	isError,
	onRefresh,
	automaticBridge,
	isPreparingBridge,
	bridgeError,
	onPrepareBridge,
}: {
	value: MachineNetwork
	onChange: (network: MachineNetwork) => void
	bridges: string[]
	disabled?: boolean
	note?: string
	isLoading?: boolean
	isError?: boolean
	onRefresh: () => void
	automaticBridge?: {available: boolean; interface?: string; reason?: 'wifi' | 'unavailable' | 'unsupported' | 'busy'}
	isPreparingBridge?: boolean
	bridgeError?: string
	onPrepareBridge: () => Promise<string | undefined>
}) {
	const options = [
		{value: 'nat', label: t('machines.network-nat'), description: t('machines.network-nat-description')},
		{
			value: 'host-only',
			label: t('machines.network-host-only'),
			description: t('machines.network-host-only-description'),
		},
		{value: 'bridge', label: t('machines.network-bridge'), description: t('machines.network-bridge-description')},
	] as const
	const selected = options.find((option) => option.value === value.mode)!
	const bridgeAvailable = value.mode !== 'bridge' || bridges.includes(value.bridge)
	const triggerClass =
		'flex min-h-10 w-full min-w-0 items-center justify-between gap-2 rounded-8 bg-white/6 px-3 py-2 text-13 font-medium -tracking-2 text-white/70 outline-hidden transition-colors hover:bg-white/10 focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed'

	return (
		<SpecRow
			label={t('machines.network')}
			note={note ?? (isPreparingBridge ? t('machines.network-bridge-preparing-note') : selected.description)}
			stackOnMobile
		>
			<div className='flex w-full min-w-0 flex-col gap-2 sm:w-48'>
				<DropdownMenu onOpenChange={(open) => open && onRefresh()}>
					<DropdownMenuTrigger asChild>
						<button
							type='button'
							disabled={disabled || isPreparingBridge}
							aria-busy={isPreparingBridge}
							aria-label={t('machines.network')}
							className={`${triggerClass} ${isPreparingBridge ? 'disabled:opacity-100' : 'disabled:opacity-35'}`}
						>
							<span className='truncate'>
								{isPreparingBridge ? t('machines.network-bridge-preparing') : selected.label}
							</span>
							{isPreparingBridge ? (
								<Loader2 aria-hidden className='size-3.5 shrink-0 animate-spin' />
							) : (
								<ChevronDown aria-hidden className='size-3.5 shrink-0 text-white/40' />
							)}
						</button>
					</DropdownMenuTrigger>
					<DropdownMenuContent align='end' className='w-72 max-w-[calc(100vw-32px)]'>
						<DropdownMenuRadioGroup
							value={value.mode}
							onValueChange={(mode) => {
								if (mode === value.mode && (mode !== 'bridge' || bridgeAvailable)) return
								if (disabled || isPreparingBridge) return
								if (mode === 'bridge' && bridges.length) onChange({mode, bridge: bridges[0]})
								else if (mode === 'bridge' && automaticBridge?.available)
									void onPrepareBridge().then((bridge) => bridge && onChange({mode: 'bridge', bridge}))
								else if (mode === 'nat' || mode === 'host-only') onChange({mode})
							}}
						>
							{options.map((option) => (
								<DropdownMenuRadioItem
									key={option.value}
									value={option.value}
									disabled={
										disabled ||
										isPreparingBridge ||
										(option.value === 'bridge' &&
											(isLoading || isError || (!bridges.length && !automaticBridge?.available)))
									}
								>
									<div className='min-w-0'>
										<div>{option.label}</div>
										<div className='text-11 font-normal whitespace-normal opacity-50'>{option.description}</div>
									</div>
								</DropdownMenuRadioItem>
							))}
						</DropdownMenuRadioGroup>
						{!isLoading && !isError && !bridges.length && (
							<p className='px-3 py-2 text-11 text-white/50'>
								{automaticBridge?.available
									? t('machines.network-bridge-automatic', {interface: automaticBridge.interface ?? ''})
									: automaticBridge?.reason === 'wifi'
										? t('machines.network-bridge-wifi')
										: automaticBridge?.reason === 'busy'
											? t('machines-error.machine-bridge-busy')
											: t('machines.network-no-bridges')}
							</p>
						)}
					</DropdownMenuContent>
				</DropdownMenu>
				{value.mode === 'bridge' && (
					<DropdownMenu onOpenChange={(open) => open && onRefresh()}>
						<DropdownMenuTrigger asChild>
							<button
								type='button'
								disabled={disabled || isPreparingBridge || isLoading || isError}
								aria-label={t('machines.network-bridge-interface')}
								className={`${triggerClass} disabled:opacity-35`}
							>
								<span className='truncate'>{value.bridge}</span>
								<ChevronDown aria-hidden className='size-3.5 shrink-0 text-white/40' />
							</button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align='end' className='max-w-[calc(100vw-32px)] min-w-48'>
							<DropdownMenuRadioGroup
								value={value.bridge}
								onValueChange={(bridge) => onChange({mode: 'bridge', bridge})}
							>
								{bridges.map((bridge) => (
									<DropdownMenuRadioItem key={bridge} value={bridge}>
										{bridge}
									</DropdownMenuRadioItem>
								))}
							</DropdownMenuRadioGroup>
						</DropdownMenuContent>
					</DropdownMenu>
				)}
				{!disabled && isError && (
					<p role='status' className='text-12 text-destructive2-lightest'>
						{t('machines.network-load-failed')}
					</p>
				)}
				{!disabled && bridgeError && (
					<p role='alert' className='text-12 text-destructive2-lightest'>
						{bridgeError}
					</p>
				)}
				{!disabled && !isLoading && !isError && !bridgeAvailable && (
					<p role='status' className='text-12 text-destructive2-lightest'>
						{t('machines.network-bridge-missing')}
					</p>
				)}
			</div>
		</SpecRow>
	)
}
