/** What pages tell the frame around them, and the frame tells pages. */
export const shell = $state<{
	/** Shown in the status bar instead of the system state, e.g. "edit mode". */
	statusNote: string;
	/** Opens the phone menu (round menu layout); null when there is none. */
	openMenu: (() => void) | null;
	/** The page shows its own menu button, so the round one is hidden. */
	ownMenu: boolean;
}>({ statusNote: '', openMenu: null, ownMenu: false });
