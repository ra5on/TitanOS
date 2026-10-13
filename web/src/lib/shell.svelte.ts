/** What pages tell the frame around them. */
export const shell = $state<{
	/** Shown in the status bar instead of the system state, e.g. "edit mode". */
	statusNote: string;
}>({ statusNote: '' });
