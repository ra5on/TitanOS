type Navigation = {
	account: string;
	tab: 'volumes' | 'disks';
	query: string;
	filter: string;
	volume: string;
	sort: string;
	selected: string;
	disk: string;
	scroll: number;
};
let current: Navigation | undefined;
export function storageNavigation(account: string): Navigation {
	if (current?.account !== account)
		current = {
			account,
			tab: 'volumes',
			query: '',
			filter: 'all',
			volume: '',
			sort: 'name',
			selected: '',
			disk: '',
			scroll: 0,
		};
	return current;
}
