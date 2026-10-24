import type { DashboardVisual } from './visual.ts';
/** Only an open editor owns a preview. Nothing is persisted until Save. */
export const dashboardScene = $state<{
	preview: DashboardVisual | null;
	urls: Record<string, string>;
}>({ preview: null, urls: {} });
