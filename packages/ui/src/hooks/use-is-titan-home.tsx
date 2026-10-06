import {trpcReact} from '@/trpc/trpc'

export function useIsTitanHome() {
	const isTitanHomeQ = trpcReact.migration.isTitanHome.useQuery()
	const isTitanHome = !!isTitanHomeQ.data
	return {
		isTitanHome,
		isLoading: isTitanHomeQ.isLoading,
	}
}
