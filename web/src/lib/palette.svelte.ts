export const palette = $state({open:false});
export function togglePalette(){palette.open=!palette.open;}

// Pages opened from the palette, newest first. Kept in RAM for this page
// session only; preferences never go to browser storage.
export const recentPages = $state<{ list: string[] }>({ list: [] });
export function rememberPage(href: string) {
	recentPages.list = [href, ...recentPages.list.filter((h) => h !== href)].slice(0, 5);
}
