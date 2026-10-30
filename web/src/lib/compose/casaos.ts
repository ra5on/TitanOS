// Turns a template from a CasaOS store into one TitanOS can install.
//
// CasaOS templates are compose files that expect that system: app data
// under /DATA/AppData/$AppID, shared folders under /DATA, and the variables
// $PUID, $PGID and $TZ. Their descriptions live in x-casaos blocks.

import { parseDocument } from 'yaml';
import { readService, serviceNames, writeService } from './service.ts';

export type CasaContext = {
	timezone: string;
	uid: number;
	gid: number;
	/** The address of the NAS in the local network. */
	host: string;
};

type Texts = Record<string, string>;

export function isCasaOS(compose: string): boolean {
	return parseDocument(compose).has('x-casaos');
}

function record(value: unknown): Record<string, unknown> {
	return value && typeof value === 'object' && !Array.isArray(value)
		? (value as Record<string, unknown>)
		: {};
}

/** Reduces the locales of a CasaOS text (en_US, de_DE, …) to the languages TitanOS speaks. */
function texts(value: unknown): Texts | undefined {
	if (typeof value === 'string') return value.trim() ? { en: value.trim() } : undefined;
	const result: Texts = {};
	for (const [locale, text] of Object.entries(record(value))) {
		if (typeof text !== 'string' || !text.trim()) continue;
		const key = locale.toLowerCase();
		if (key === 'de_de') result.de = text.trim();
		// en_US wins over en_GB.
		if (key === 'en_us' || (key === 'en_gb' && !result.en)) result.en = text.trim();
	}
	return Object.keys(result).length > 0 ? result : undefined;
}

// What some stores put where a description belongs.
const PLACEHOLDER_LABEL = /^container (variable|path|port)\b/i;

function label(value: unknown, self: string): Texts | undefined {
	const found = texts(value);
	if (!found) return undefined;
	for (const [language, text] of Object.entries(found)) {
		if (PLACEHOLDER_LABEL.test(text) || text === self || text.length > 120) delete found[language];
	}
	return Object.keys(found).length > 0 ? found : undefined;
}

// Folders of CasaOS that hold media; everything else under /DATA is some shared folder.
const MEDIA_FOLDERS = ['media', 'gallery', 'music', 'movies', 'tv', 'photos'];
// "[CASAOS_IP]" and its relatives stand for the address of the machine itself.
const HOST_PLACEHOLDER = /\[(CHANGE_TO_LAN_IP|(YOUR_)?(CASAOS_)?(LAN_)?IP)\]/g;
const OPEN_PLACEHOLDER = /\[[A-Z][A-Z0-9_ ]{2,}\]/;

/** Returns the template as a compose file with an x-titanos block. */
export function fromCasaOS(compose: string, appId: string, ctx: CasaContext): string {
	const values: Record<string, string> = {
		TZ: ctx.timezone,
		PUID: String(ctx.uid),
		PGID: String(ctx.gid),
	};
	const text = compose
		.replace(/\$\{AppID\}|\$AppID\b/g, appId)
		.replace(
			/(?<!\$)\$\{(TZ|PUID|PGID)(?::?-[^}]*)?\}|(?<!\$)\$(TZ|PUID|PGID)\b/g,
			(_match, braced: string, plain: string) => values[braced ?? plain],
		)
		.replace(HOST_PLACEHOLDER, ctx.host);

	const doc = parseDocument(text);
	if (doc.errors.length > 0) throw new Error(doc.errors[0].message);
	const top = record(doc.toJS()['x-casaos']);
	const services = serviceNames(doc);
	const main = typeof top.main === 'string' && services.includes(top.main) ? top.main : services[0];
	const portMap = /^\d+$/.test(String(top.port_map ?? '')) ? String(top.port_map) : '';

	const variables: Record<string, Record<string, unknown>> = {};
	const paths: Record<string, Record<string, unknown>> = {};
	const ports: Record<string, Record<string, unknown>> = {};
	const note = (
		into: Record<string, Record<string, unknown>>,
		key: string,
		entry: Record<string, unknown>,
	) => {
		const kept = Object.fromEntries(
			Object.entries(entry).filter(([, value]) => value !== undefined),
		);
		if (Object.keys(kept).length > 0) into[key] = { ...into[key], ...kept };
	};

	for (const service of services) {
		const described = record(doc.toJS().services?.[service]?.['x-casaos']);
		const describedAs = (list: unknown, container: string) =>
			(Array.isArray(list) ? list : []).find(
				(entry) => String(record(entry).container) === container,
			);
		doc.deleteIn(['services', service, 'x-casaos']);

		const entries = readService(doc, service);
		for (const pair of entries.env) {
			note(variables, pair.name, {
				label: label(record(describedAs(described.envs, pair.name)).description, pair.name),
			});
			// "[EUFY_USERNAME]" is a blank to fill in, not a value.
			if (OPEN_PLACEHOLDER.test(pair.value)) {
				note(variables, pair.name, { required: true, placeholder: pair.value });
				pair.value = '';
			}
			// "${TOKEN:?Token required}" refuses to start without a value from outside.
			const demanded = /^\$\{[A-Za-z_][A-Za-z0-9_]*:?\?([^}]*)\}$/.exec(pair.value);
			if (demanded) {
				note(variables, pair.name, {
					required: true,
					placeholder: demanded[1].trim().slice(0, 120),
				});
				pair.value = '';
			}
		}
		for (const row of entries.paths) {
			if (row.raw !== undefined) continue;
			let suggest: string | undefined;
			const own = /^\/DATA\/AppData\/[^/]+(\/.*)?$/.exec(row.host);
			if (own) {
				// The app's own folder becomes its data folder on the Docker volume.
				row.host = '.' + (own[1] ?? '');
			} else if (row.host === '/DATA' || row.host.startsWith('/DATA/')) {
				const [folder = 'data', ...rest] = row.host.split('/').slice(2);
				suggest = MEDIA_FOLDERS.includes(folder.toLowerCase()) ? 'media' : 'share';
				row.host = ['.', folder.toLowerCase(), ...rest].join('/');
			}
			note(paths, row.container, {
				label: label(
					record(describedAs(described.volumes, row.container)).description,
					row.container,
				),
				suggest,
			});
		}
		for (const row of entries.ports) {
			if (row.raw !== undefined) continue;
			const webui =
				service === main && row.host !== '' && row.host === portMap && row.protocol === 'tcp'
					? true
					: undefined;
			note(ports, row.protocol === 'udp' ? `${row.container}/udp` : row.container, {
				label: label(
					record(describedAs(described.ports, row.container)).description,
					row.container,
				),
				webui,
			});
		}
		if (service === main && portMap !== '' && entries.webui === '') {
			const index = typeof top.index === 'string' && top.index.startsWith('/') ? top.index : '/';
			entries.webui =
				top.scheme === 'https'
					? `https://[host]:${portMap}${index}`
					: portMap + (index === '/' ? '' : index);
		}
		writeService(doc, service, entries);
	}

	doc.delete('x-casaos');
	const meta: Record<string, unknown> = {
		icon: top.icon,
		main,
		tips: texts(record(top.tips).before_install),
		variables,
		paths,
		ports,
	};
	doc.set(
		'x-titanos',
		Object.fromEntries(Object.entries(meta).filter(([, value]) => value !== undefined)),
	);
	return doc.toString({ lineWidth: 0 });
}
