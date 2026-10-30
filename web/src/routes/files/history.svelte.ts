// Back and forward in the file manager, like in a browser. A step keeps
// the search, the selection and both scroll positions of the folder.

export type Location = {
	root: string;
	path: string;
	search: string;
	selected: string[];
	scroll: number;
	listScroll: number;
};

export class FileHistory {
	back = $state<Location[]>([]);
	forward = $state<Location[]>([]);

	/** Leaving `current` for another folder; the forward steps are gone. */
	leave(current: Location | null) {
		if (current) this.back = [...this.back, current].slice(-50);
		this.forward = [];
	}

	/** One step back or forward from `current`; the result is where to go. */
	go(direction: 'back' | 'forward', current: () => Location) {
		const target = (direction === 'back' ? this.back : this.forward).at(-1);
		if (!target) return;
		if (direction === 'back') {
			this.back = this.back.slice(0, -1);
			this.forward = [...this.forward, current()];
		} else {
			this.forward = this.forward.slice(0, -1);
			this.back = [...this.back, current()];
		}
		return target;
	}
}
