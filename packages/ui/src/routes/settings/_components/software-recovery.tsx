import {useId, useRef, useState} from 'react'

import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle} from '@/components/ui/dialog'
import {useGlobalSystemState} from '@/providers/global-system-state'
import {trpcReact} from '@/trpc/trpc'

export function SoftwareRecovery() {
	const id = useId()
	const noButton = useRef<HTMLButtonElement>(null)
	const userQ = trpcReact.user.get.useQuery()
	const owner = userQ.data?.role === 'owner'
	const recoveryQ = trpcReact.system.recoveryStatus.useQuery(undefined, {
		enabled: owner,
		retry: false,
		refetchOnWindowFocus: true,
		refetchInterval: 30_000,
	})
	const {rollback, isPowerActionPending} = useGlobalSystemState()
	const [selection, setSelection] = useState('')
	const [confirmOpen, setConfirmOpen] = useState(false)
	const available = recoveryQ.data?.previous ?? []
	const selected = selection ? available.find((item) => item.selection === selection) : available[0]

	if (!owner) return null

	return (
		<section className='min-w-0 px-5 py-4' aria-labelledby={`${id}-title`}>
			<h3 id={`${id}-title`} className='mb-2 text-14 font-medium text-white/90'>
				Wiederherstellung
			</h3>
			<p className='mb-3 text-12 leading-relaxed text-white/50'>
				Starte einen zuvor bestätigten Systemstand. Dateien bleiben erhalten; App- und Datenbankänderungen werden nicht
				zurückgesetzt.
			</p>
			{recoveryQ.isLoading ? (
				<p className='text-12 text-white/50'>Systemstände werden geprüft…</p>
			) : recoveryQ.error ? (
				<div className='flex flex-wrap items-center gap-3'>
					<p role='status' className='text-12 text-white/60'>
						Wiederherstellung ist gerade nicht verfügbar.
					</p>
					<Button onClick={() => void recoveryQ.refetch()}>Erneut prüfen</Button>
				</div>
			) : available.length === 0 ? (
				<p role='status' className='text-12 text-white/50'>
					{recoveryQ.data?.reason || 'Noch kein vorheriger bestätigter Systemstand vorhanden.'}
				</p>
			) : (
				<div className='flex min-w-0 flex-wrap items-end gap-3'>
					<div className='min-w-0 flex-1'>
						<label htmlFor={`${id}-version`} className='mb-1.5 block text-12 text-white/60'>
							Vorheriger Systemstand
						</label>
						<select
							id={`${id}-version`}
							className='h-10 w-full min-w-0 rounded-10 border border-white/15 bg-black/30 px-3 text-13 text-white outline-hidden focus-visible:ring-2 focus-visible:ring-white/40'
							value={selected?.selection ?? ''}
							onChange={(event) => setSelection(event.target.value)}
							disabled={isPowerActionPending}
						>
							{available.map((item) => (
								<option key={item.selection} value={item.selection} className='bg-neutral-900'>
									{item.name} · Slot {item.slot.toUpperCase()}
								</option>
							))}
						</select>
					</div>
					<Button
						disabled={!selected || isPowerActionPending}
						onClick={() => {
							if (selected) {
								setSelection(selected.selection)
								setConfirmOpen(true)
							}
						}}
					>
						Wiederherstellen
					</Button>
				</div>
			)}
			<Dialog open={confirmOpen && !!selected} onOpenChange={setConfirmOpen}>
				<DialogContent
					onOpenAutoFocus={(event) => {
						event.preventDefault()
						noButton.current?.focus()
					}}
				>
					<DialogHeader>
						<DialogTitle>{selected?.name} wiederherstellen?</DialogTitle>
						<DialogDescription>
							Das NAS startet neu. Deine Dateien bleiben erhalten. Änderungen an Apps und ihren Datenbanken werden nicht
							rückgängig gemacht.
						</DialogDescription>
					</DialogHeader>
					<div className='flex flex-row justify-center gap-3'>
						<Button
							variant='primary'
							size='dialog'
							disabled={!selected || isPowerActionPending}
							onClick={() => {
								if (!selected) return
								setConfirmOpen(false)
								rollback(selected.selection)
							}}
						>
							Ja
						</Button>
						<Button ref={noButton} size='dialog' onClick={() => setConfirmOpen(false)}>
							Nein
						</Button>
					</div>
				</DialogContent>
			</Dialog>
		</section>
	)
}
