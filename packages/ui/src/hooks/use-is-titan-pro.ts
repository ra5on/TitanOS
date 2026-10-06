import {trpcReact} from '@/trpc/trpc'

export function useIsTitanPro({enabled = true}: {enabled?: boolean} = {}) {
	const isTitanProQ = trpcReact.hardware.titanPro.isTitanPro.useQuery(undefined, {enabled})
	const isTitanPro = !!isTitanProQ.data
	return {
		isTitanPro,
		hasData: isTitanProQ.data !== undefined,
		isLoading: isTitanProQ.isLoading,
		error: isTitanProQ.error,
		refetch: isTitanProQ.refetch,
		isFetching: isTitanProQ.isFetching,
	}
}
