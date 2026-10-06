import {useIsMember} from '@/features/files/hooks/use-home-path'
import {trpcReact} from '@/trpc/trpc'

/**
 * Service to detect the type of network device (Titan or generic NAS)
 */

type NetworkDeviceType = 'titan' | 'nas'

/**
 * Extract hostname from network path
 * @param path Network path like "/Network/titan.local" or "/Network/192.168.1.100"
 * @returns hostname or null if invalid path
 */
const extractHostnameFromPath = (path: string): string | null => {
	if (!path.startsWith('/Network/')) return null

	const segments = path.split('/').filter(Boolean)
	if (segments.length < 2) return null

	return segments[1] // The hostname is the second segment after "Network"
}

/**
 * Hook to detect and cache the type of network device (Titan or generic NAS)
 * @param path Network path like "/Network/titan.local"
 * @returns Device type detection state
 */
export function useNetworkDeviceType(path: string) {
	const hostname = extractHostnameFromPath(path)
	const isMember = useIsMember()

	// Optimistically determine device type based on hostname
	const optimisticDeviceType: NetworkDeviceType = hostname?.toLowerCase().includes('titan') ? 'titan' : 'nas'

	const query = trpcReact.files.isServerAnTitanDevice.useQuery(
		{address: hostname!},
		{
			// Cache for 1 hour
			gcTime: 60 * 60 * 1000,
			// Consider data fresh for 30 minutes
			staleTime: 30 * 60 * 1000,
			// Don't retry on failure - fail fast for better UX
			retry: false,
			// Only run if we have a valid hostname
			// Members do not need access to the owner-only network management
			// endpoint. The hostname-based optimistic icon is sufficient.
			enabled: !!hostname && !isMember,
			// Refetch when window regains focus
			refetchOnWindowFocus: true,
		},
	)

	// Determine final device type
	let deviceType: NetworkDeviceType = 'nas'
	if (!hostname) {
		deviceType = 'nas'
	} else if (query.data !== undefined) {
		// Use actual result from TRPC query
		deviceType = query.data === true ? 'titan' : 'nas'
	} else {
		// Use optimistic value while loading or on error
		deviceType = optimisticDeviceType
	}

	return {
		deviceType,
		isLoading: query.isLoading,
		isError: query.isError,
		error: query.error,
	}
}
