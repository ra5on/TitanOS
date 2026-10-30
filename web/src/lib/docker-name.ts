import type { DockerContainer } from './api.ts';

/** Stable technical identities stay untouched; only labels use the chosen app name. */
export function dockerName(
	container: Pick<DockerContainer, 'managed' | 'app' | 'name' | 'service'>,
	context: 'app' | 'service' | 'distinct' = 'app',
): string {
	if (!container.managed || !container.app) return container.name;
	if (context === 'service') return container.service || container.name;
	if (context === 'distinct' && container.service && container.service !== container.app)
		return `${container.app} · ${container.service}`;
	return container.app;
}
