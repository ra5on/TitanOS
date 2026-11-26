// The mobile file layouts, shared by the folder picker and the app menu.
export const layouts = [
	{ value: 'list', label: 'files.mobile.list', icon: 'list' },
	{ value: 'details', label: 'files.mobile.details', icon: 'columns' },
	{ value: 'grid', label: 'files.mobile.grid', icon: 'grid' },
	{ value: 'rail', label: 'files.mobile.rail', icon: 'sidebar' },
	{ value: 'cards', label: 'files.mobile.cards', icon: 'image' },
] as const;
export type Layout = (typeof layouts)[number]['value'];
