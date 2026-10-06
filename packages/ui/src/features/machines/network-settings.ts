import type {Machine} from '@/features/machines/types'

export type MachineNetwork = Machine['network']

export function sameMachineNetwork(current: MachineNetwork, next: MachineNetwork) {
	return (
		current.mode === next.mode &&
		(current.mode !== 'bridge' || (next.mode === 'bridge' && current.bridge === next.bridge))
	)
}

export function machineNetworkAvailable(network: MachineNetwork, bridges: string[]) {
	return network.mode !== 'bridge' || bridges.includes(network.bridge)
}

export function canChangeMachineNetwork(machine: Pick<Machine, 'state' | 'networkChangeAllowed'>) {
	return machine.state === 'stopped' && machine.networkChangeAllowed
}
