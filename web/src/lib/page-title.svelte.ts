// The title of the page that is shown, set by PageHead, so that the browser
// tab says where one is: "Page – Host – TitanOS".
export const pageTitle = $state({ value: '' });

/** One place of the path above a page; with `href` it can be gone back to. */
export type PathCrumb = { label: string; href?: string };

// The path to the page that is shown ("Smart Home und KI › KI-Agent ›
// Verbindung und Modelle"), set by PageHead. In a window of the desktop the
// title bar shows it, each place above the page a link (owner 16 Nov 2026:
// one path at the top instead of a title, a path and a back row stacked).
export const pagePath = $state<{ value: PathCrumb[] }>({ value: [] });
