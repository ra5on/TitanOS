// PLAN 61: the desktop backgrounds that come with TitanOS – drawn here as
// small SVGs, one light and one dark version each, no picture files.

type Palette = [string, string, string, string, string];

const PALETTES: Record<string, { dark: Palette; light: Palette }> = {
	// base, deep, mid, glow, line
	wave: {
		dark: ['#060b18', '#0a1530', '#16336b', '#2a5bb0', '#7fb0ff'],
		light: ['#e9f1ff', '#cfe1ff', '#8fb8f5', '#5c8fe0', '#ffffff'],
	},
	aurora: {
		dark: ['#04100f', '#06231f', '#0d5a4f', '#2bb39a', '#9af5e0'],
		light: ['#eafaf6', '#c9f1e7', '#86dcc8', '#3fb39c', '#ffffff'],
	},
	dunes: {
		dark: ['#140b06', '#2a160b', '#6b3a16', '#c97a2f', '#ffcf99'],
		light: ['#fff5ea', '#ffe3c4', '#f6bd83', '#e08a3c', '#ffffff'],
	},
	peaks: {
		dark: ['#0b0716', '#1a1035', '#3b2a7a', '#7b5cd6', '#c9b8ff'],
		light: ['#f3efff', '#ddd3ff', '#b5a3f5', '#8566e0', '#ffffff'],
	},
	grid: {
		dark: ['#07090c', '#10151c', '#1c2632', '#2f6fb0', '#6cb6ff'],
		light: ['#f4f6f9', '#e4e9f0', '#c8d3e0', '#6c97c8', '#ffffff'],
	},
	calm: {
		dark: ['#0b0d10', '#14181d', '#20262e', '#3a4654', '#8fa3b8'],
		light: ['#f6f7f9', '#eceff3', '#d9dee5', '#aab4c0', '#ffffff'],
	},
};

/** Built-in photo wallpapers (static/backgrounds/<id>.webp). */
const PHOTOS = ['nordlicht', 'bergsee', 'kueste', 'nebelwald', 'wueste', 'weltraum'];

function svg(id: string, p: Palette): string {
	const [base, deep, mid, glow, line] = p;
	let body = '';
	switch (id) {
		case 'aurora':
			body = `<rect width='1600' height='1000' fill='${base}'/>
<path d='M-100 420C200 200 500 520 800 300s600 60 900-120V1100H-100Z' fill='${deep}'/>
<path d='M-100 520C260 340 520 620 860 420s560 80 840-60' fill='none' stroke='${glow}' stroke-opacity='.55' stroke-width='90' filter='url(#b)'/>
<path d='M-100 600C300 460 560 700 900 540s520 40 800-80' fill='none' stroke='${line}' stroke-opacity='.25' stroke-width='40' filter='url(#b)'/>
<path d='M-100 900C300 760 700 860 1000 800s480-60 700 0V1100H-100Z' fill='${mid}' opacity='.6'/>`;
			break;
		case 'dunes':
			body = `<rect width='1600' height='1000' fill='${base}'/><circle cx='1180' cy='300' r='130' fill='${glow}' opacity='.55' filter='url(#b)'/>
<path d='M-100 640C220 560 460 600 760 680s620 40 960-60V1100H-100Z' fill='${deep}'/>
<path d='M-100 760C300 680 600 740 900 800s540 0 800-60V1100H-100Z' fill='${mid}' opacity='.8'/>
<path d='M-100 880C260 820 640 900 1000 900s460-40 700-40V1100H-100Z' fill='${glow}' opacity='.45'/>`;
			break;
		case 'peaks':
			body = `<rect width='1600' height='1000' fill='${base}'/><rect width='1600' height='1000' fill='url(#r)'/>
<path d='M-100 820 260 420 520 700 860 300 1160 640 1400 460 1700 760V1100H-100Z' fill='${deep}'/>
<path d='M-100 900 320 600 640 840 980 560 1300 820 1700 640V1100H-100Z' fill='${mid}' opacity='.8'/>
<path d='M260 420 330 500 390 470M860 300 940 400 1010 360' fill='none' stroke='${line}' stroke-opacity='.5' stroke-width='3'/>`;
			break;
		case 'grid':
			body = `<rect width='1600' height='1000' fill='${base}'/><rect width='1600' height='1000' fill='url(#r)'/>
<g stroke='${line}' stroke-opacity='.08'>${Array.from({ length: 33 }, (_, i) => `<path d='M${i * 50} 0V1000'/>`).join('')}${Array.from({ length: 21 }, (_, i) => `<path d='M0 ${i * 50}H1600'/>`).join('')}</g>
<path d='M-100 760C300 640 600 720 900 600s500-120 800-60' fill='none' stroke='${glow}' stroke-width='3' stroke-opacity='.7'/>`;
			break;
		case 'calm':
			body = `<rect width='1600' height='1000' fill='${base}'/><rect width='1600' height='1000' fill='url(#r)'/>
<circle cx='1200' cy='260' r='420' fill='${mid}' opacity='.35' filter='url(#b)'/><circle cx='300' cy='860' r='360' fill='${deep}' opacity='.6' filter='url(#b)'/>`;
			break;
		default:
			body = `<rect width='1600' height='1000' fill='${base}'/><rect width='1600' height='1000' fill='url(#r)'/>
<path d='M-100 820C250 620 420 300 760 260s620 160 960 20V1100H-100Z' fill='${deep}' opacity='.9'/>
<path d='M-100 900C300 760 560 520 900 520s520 240 800 120V1100H-100Z' fill='${mid}' opacity='.6'/>
<path d='M300 1100C420 820 700 640 1000 660s460 200 700 60V1100Z' fill='${base}' opacity='.5'/>
<path d='M-100 820C250 620 420 300 760 260s620 160 960 20' fill='none' stroke='${line}' stroke-opacity='.35' stroke-width='2'/>
<path d='M-100 900C300 760 560 520 900 520s520 240 800 120' fill='none' stroke='${line}' stroke-opacity='.25' stroke-width='2'/>`;
	}
	const s = `<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 1600 1000' preserveAspectRatio='xMidYMid slice'><defs><radialGradient id='r' cx='.72' cy='.3' r='.75'><stop offset='0' stop-color='${glow}' stop-opacity='.5'/><stop offset='1' stop-color='${base}' stop-opacity='0'/></radialGradient><filter id='b' x='-50%' y='-50%' width='200%' height='200%'><feGaussianBlur stdDeviation='40'/></filter></defs>${body}</svg>`;
	return `url("data:image/svg+xml,${encodeURIComponent(s)}")`;
}

/** The CSS background of a built-in wallpaper. */
export function wallpaperCss(id: string, light: boolean): string {
	// PLAN 74: the photos (static/backgrounds) are the same in light and dark.
	if (PHOTOS.includes(id)) return `url("/backgrounds/${id}.webp")`;
	const p = PALETTES[id] ?? PALETTES.wave;
	return svg(id in PALETTES ? id : 'wave', light ? p.light : p.dark);
}

/** A slight tint by the time of day for the "living" background: cool at night, warm in the evening. */
export function dayTint(date = new Date()): number {
	const h = date.getHours() + date.getMinutes() / 60;
	// hue shift in degrees: -12 at night, 0 by day, +10 in the evening
	if (h < 6 || h >= 22) return -12;
	if (h < 9) return -6 + (h - 6) * 2;
	if (h < 17) return 0;
	return Math.min(10, (h - 17) * 2.5);
}
