type PreparedBridge = {bridge: string; token?: string; expiresAt?: number}
type BridgeConfirmation = {state: 'preparing' | 'ready'; bridge: string}

// Confirmation must travel over HTTP to the same NAS origin. A queued WebSocket
// operation cannot prove that the browser has reconnected after the NIC moves.
export async function prepareAutomaticBridge({
	prepare,
	confirm,
	signal,
	now = Date.now,
	wait = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)),
}: {
	prepare: (signal: AbortSignal) => Promise<PreparedBridge>
	confirm: (token: string, signal: AbortSignal) => Promise<BridgeConfirmation>
	signal: AbortSignal
	now?: () => number
	wait?: (ms: number) => Promise<void>
}) {
	const request = async <T>(action: (requestSignal: AbortSignal) => Promise<T>, timeout: number): Promise<T> => {
		if (signal.aborted) throw new Error('[machine-bridge-cancelled]')
		const controller = new AbortController()
		const abort = () => controller.abort()
		signal.addEventListener('abort', abort, {once: true})
		const timer = setTimeout(abort, timeout)
		try {
			return await action(controller.signal)
		} finally {
			clearTimeout(timer)
			signal.removeEventListener('abort', abort)
		}
	}
	const prepared = await request(prepare, 20_000)
	if (!prepared.token) return prepared.bridge
	const deadline = Math.min(prepared.expiresAt ?? now() + 90_000, now() + 90_000)
	while (!signal.aborted && now() < deadline) {
		try {
			const result = await request(
				(requestSignal) => confirm(prepared.token!, requestSignal),
				Math.min(5_000, deadline - now()),
			)
			if (result.state === 'ready') return result.bridge
		} catch (error) {
			// Transport errors are expected during the short network switch. Server
			// refusals (including expired/rejected tokens) must never be hidden.
			const serverCode = (error as {data?: {code?: string}})?.data?.code
			if (serverCode || (error instanceof Error && /^\[machine-bridge-/.test(error.message))) throw error
		}
		await wait(Math.min(1_000, Math.max(0, deadline - now())))
	}
	throw new Error(signal.aborted ? '[machine-bridge-cancelled]' : '[machine-bridge-timeout]')
}
