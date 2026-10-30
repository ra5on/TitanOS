// Stand-in for $app/state with a fixed address.
export const page = {
	url: new URL('http://nas.test/'),
	params: {},
	route: { id: null },
	state: {},
};
export const navigating = { from: null, to: null, type: null };
export const updated = { current: false, check: async () => false };
