import {createContext, useContext, useEffect, useRef, useState, type ReactNode} from 'react'

import {trpcReact} from '@/trpc/trpc'

export const DEFAULT_DESKTOP_TRANSPARENCY = 75

export function normalizeDesktopTransparency(value: unknown) {
	return typeof value === 'number' && Number.isFinite(value)
		? Math.max(0, Math.min(100, Math.round(value)))
		: DEFAULT_DESKTOP_TRANSPARENCY
}

type DesktopAppearance = {
	transparency: number
	setTransparency: (value: number) => void
	resetTransparency: () => void
	isSaving: boolean
	saveFailed: boolean
}

const DesktopAppearanceContext = createContext<DesktopAppearance>({
	transparency: DEFAULT_DESKTOP_TRANSPARENCY,
	setTransparency: () => {},
	resetTransparency: () => {},
	isSaving: false,
	saveFailed: false,
})

export function useDesktopAppearance() {
	return useContext(DesktopAppearanceContext)
}

export function DesktopAppearanceProvider({children}: {children: ReactNode}) {
	const {data: user} = trpcReact.user.get.useQuery()
	const utils = trpcReact.useUtils()
	const [draft, setDraft] = useState<{userId: string; value: number} | null>(null)
	const [saveFailed, setSaveFailed] = useState(false)
	const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
	const latestRequest = useRef(0)
	const accountId = useRef(user?.userId)
	accountId.current = user?.userId
	const mutation = trpcReact.user.set.useMutation()
	const pending = useRef<Promise<unknown> | null>(null)
	const saved = normalizeDesktopTransparency(user?.desktopTransparency)
	const transparency = user && draft?.userId === user.userId ? draft.value : saved

	useEffect(() => {
		document.documentElement.style.setProperty('--desktop-glass-opacity', String(1 - transparency / 100))
		return () => {
			document.documentElement.style.removeProperty('--desktop-glass-opacity')
		}
	}, [transparency])

	useEffect(() => {
		// Never flush one account's pending preference into the next login.
		latestRequest.current++
		setDraft(null)
		setSaveFailed(false)
		return () => clearTimeout(timer.current)
	}, [user?.userId])

	useEffect(() => {
		// Wait for the query cache to publish the saved value before discarding
		// the live preview, otherwise a slow notification briefly shows old glass.
		setDraft((previous) => (previous && previous.userId === user?.userId && previous.value === saved ? null : previous))
	}, [user?.userId, saved])

	const save = async (value: number, userId: string, requestId: number) => {
		// Serialize requests while rechecking identity before sending any queued
		// value. This avoids older slider movements overwriting the final choice.
		if (pending.current) await pending.current.catch(() => {})
		if (accountId.current !== userId || latestRequest.current !== requestId) return
		const request = mutation.mutateAsync({desktopTransparency: value})
		pending.current = request
		try {
			await request
			if (accountId.current !== userId || latestRequest.current !== requestId) return
			utils.user.get.setData(undefined, (previous) =>
				previous?.userId === userId ? {...previous, desktopTransparency: value} : previous,
			)
			setSaveFailed(false)
		} catch {
			if (accountId.current !== userId || latestRequest.current !== requestId) return
			// Keep the preview so the user can retry without losing their choice.
			setSaveFailed(true)
		} finally {
			if (pending.current === request) pending.current = null
		}
	}

	const update = (raw: number, immediate = false) => {
		if (!user) return
		const value = normalizeDesktopTransparency(raw)
		const requestId = ++latestRequest.current
		setDraft({userId: user.userId, value})
		setSaveFailed(false)
		clearTimeout(timer.current)
		if (immediate) void save(value, user.userId, requestId)
		else timer.current = setTimeout(() => void save(value, user.userId, requestId), 350)
	}

	return (
		<DesktopAppearanceContext
			value={{
				transparency,
				setTransparency: (value) => update(value),
				resetTransparency: () => update(DEFAULT_DESKTOP_TRANSPARENCY, true),
				isSaving: mutation.isPending,
				saveFailed,
			}}
		>
			{children}
		</DesktopAppearanceContext>
	)
}
