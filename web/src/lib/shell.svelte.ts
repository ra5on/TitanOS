/** What pages tell the frame around them, and the frame tells pages. */
export const shell = $state<{
	/** The running tasks as a sheet from the bottom (phone, PLAN 46). */
	tasksSheet: boolean;
}>({ tasksSheet: false });
