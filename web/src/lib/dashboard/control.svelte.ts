export const dashboardControl = $state<{ edit: (() => void) | null; editing: boolean }>({
	edit: null,
	editing: false,
});
