// A host-network operation owns its reservation until its confirmation and
// rollback have finished. Nested cleanup operations keep the outer reservation.
let activeHostNetworkChanges = 0

export function hostNetworkChangePending() {
	return activeHostNetworkChanges > 0
}

export async function withHostNetworkChange<T>(operation: () => Promise<T>): Promise<T> {
	activeHostNetworkChanges++
	try {
		return await operation()
	} finally {
		activeHostNetworkChanges--
	}
}
