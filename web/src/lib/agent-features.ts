// The eight functions of the agent (PLAN 54), each off by default: NAS-wide
// settings (owners and administrators, GET/PUT /agent/features) and an
// account's own (GET/PUT /agent/features/me). The groups keep their types
// in src/lib/features/.
import { api } from './api.ts';
import type { DocsMine, DocsNAS, PaperMine } from './features/docs.ts';
import type { Emergency, Guard, Handbook } from './features/protect.ts';
import type { Advisor, WhatIf } from './features/foresight.ts';
import type { Buddy, Catalog } from './features/family.ts';

export type Features = {
	docs: DocsNAS;
	guard: Guard;
	handbook: Handbook;
	emergency: Emergency;
	whatIf: WhatIf;
	advisor: Advisor;
	buddy: Buddy;
	catalog: Catalog;
};
export type FeaturesMine = { docs: DocsMine; paper: PaperMine };
/** Which functions are there for every account of the agent. */
export type FeaturesPublic = Record<
	| 'docs'
	| 'deadlines'
	| 'paper'
	| 'guard'
	| 'handbook'
	| 'emergency'
	| 'whatIf'
	| 'advisor'
	| 'buddy'
	| 'catalog',
	boolean
>;

export const loadFeatures = () => api.get<Features>('/agent/features');
export const saveFeatures = (f: Features) => api.put<Features>('/agent/features', f);
export const loadMine = () =>
	api.get<{ mine: FeaturesMine; nas: FeaturesPublic }>('/agent/features/me');
export const saveMine = (m: FeaturesMine) => api.put<FeaturesMine>('/agent/features/me', m);

/** Views of the functions opened from the agent (menu, settings). */
export type FeatureViewId =
	'paper' | 'deadlines' | 'guard' | 'handbook' | 'emergency' | 'advisor' | 'buddy' | 'catalog';
