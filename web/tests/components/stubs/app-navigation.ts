// Stand-in for $app/navigation: components under test do not navigate.
export const goto = async () => {};
export const beforeNavigate = () => {};
export const afterNavigate = () => {};
export const onNavigate = () => {};
export const invalidateAll = async () => {};
export const pushState = () => {};
export const replaceState = () => {};
