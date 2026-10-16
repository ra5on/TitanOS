// Turns a template from the app store into the fields of the install
// panel and, with what was entered there, into the compose file of the app.

import { isScalar, isSeq, parseDocument } from 'yaml';
import { readService, serviceNames, WEBUI_LABEL, writeService, type Pair } from './service.ts';

/** A text a template gives either once or per language. */
export type Localized = string | Record<string, string> | null | undefined;

export function localize(value: Localized, locale: string): string {
	if (value === null || value === undefined) return '';
	if (typeof value !== 'object') return String(value);
	return String(value[locale] ?? value.en ?? value[''] ?? Object.values(value)[0] ?? '');
}

/** What titand sends along with a template. */
export type Template = {
	compose: string;
	timezone: string;
	uid: number;
	gid: number;
	/** Folder that holds the data folders of all apps, e.g. /mnt/data/appdata. */
	appdata: string;
	listening: { port: number; protocol: string }[];
	/** Devices of the template this NAS does not have. */
	missingDevices: string[];
};

export type ShareRef = { name: string; path: string };

/** Where the value of a field comes from. */
export type Origin = 'template' | 'titanos' | 'generated' | 'required';

export type VarField = {
	service: string;
	name: string;
	label: Localized;
	help: Localized;
	placeholder: string;
	value: string;
	origin: Origin;
	/** Secrets are kept in the app's .env file instead of the compose file. */
	secret: boolean;
	required: boolean;
	fill: '' | 'timezone' | 'uid' | 'gid';
};

export type PathField = {
	service: string;
	index: number;
	container: string;
	label: Localized;
	/** The path as the template has it; relative ones lie in the app's data folder. */
	template: string;
	/** The share TitanOS suggests for this path, if the template asks for one. */
	share: ShareRef | null;
	/** The template asks for a share here, whether one was found or not. */
	wanted: boolean;
	host: string;
	/** Set once the path was changed by hand; it then no longer follows the app's name. */
	touched: boolean;
};

export type PortField = {
	service: string;
	index: number;
	container: string;
	protocol: 'tcp' | 'udp';
	label: Localized;
	webui: boolean;
	templateHost: string;
	/** Who uses the template's port, if it is not free: a container's name, or '' for the system. */
	blockedBy: string | null;
	host: string;
};

export type OwnPath = { host: string; container: string };
export type OwnPort = { host: string; container: string; protocol: 'tcp' | 'udp' };

/** What an app is allowed that reaches beyond its own container. */
export type Risk = 'privileged' | 'hostNetwork' | 'dockerSocket' | 'rootMount' | 'capabilities';

export type InstallForm = {
	name: string;
	/** What the template's author wants read before the installation. */
	tips: Localized;
	risks: Risk[];
	/** Devices left out because this NAS does not have them. */
	missingDevices: string[];
	/** The service own entries are added to: the one with the web interface. */
	main: string;
	vars: VarField[];
	paths: PathField[];
	ports: PortField[];
	own: { vars: Pair[]; paths: OwnPath[]; ports: OwnPort[] };
};

type Meta = {
	main: unknown;
	tips: Localized;
	variables: Record<string, { label?: Localized; help?: Localized; placeholder?: unknown; required?: unknown; secret?: unknown; fill?: unknown; from?: unknown } | undefined>;
	paths: Record<string, { label?: Localized; suggest?: unknown } | undefined>;
	ports: Record<string, { label?: Localized; webui?: unknown } | undefined>;
};

function record(value: unknown): Record<string, never> {
	return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, never>) : ({} as Record<string, never>);
}

function readMeta(compose: string): Meta {
	const meta = record((parseDocument(compose).toJS() ?? {})['x-titanos']);
	return { main: meta.main, tips: meta.tips, variables: record(meta.variables), paths: record(meta.paths), ports: record(meta.ports) };
}

const FILLED: Record<string, VarField['fill']> = { TZ: 'timezone', PUID: 'uid', PGID: 'gid' };
const MEDIA_SHARES = ['media', 'medien', 'video', 'videos', 'filme', 'movies', 'musik', 'music', 'fotos', 'photos'];

export const isRelative = (path: string) => path.startsWith('./') || path === '.';

/** Where a relative path of the template ends up on the NAS. */
export function resolve(template: string, name: string, appdata: string): string {
	return `${appdata}/${name}${template.slice(1)}`.replace(/\/+$/, '');
}

/** The path a field shows as long as nobody changed it. */
export function defaultHost(field: PathField, name: string, appdata: string): string {
	if (field.share) return field.share.path;
	return isRelative(field.template) ? resolve(field.template, name, appdata) : field.template;
}

/** A random value for a secret the app needs but nobody has to know. */
export function generateSecret(length = 32): string {
	const alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789';
	// Values above the largest multiple of the alphabet's size are skipped,
	// so every character is equally likely.
	const limit = 256 - (256 % alphabet.length);
	let secret = '';
	while (secret.length < length) {
		for (const byte of crypto.getRandomValues(new Uint8Array(length))) {
			if (byte < limit && secret.length < length) secret += alphabet[byte % alphabet.length];
		}
	}
	return secret;
}

/**
 * Reads a template into the fields of the install panel. `used` maps
 * "port/protocol" to who has it: a container's name, or '' for the system.
 * Throws if the template is not a compose file.
 */
export function readTemplate(tpl: Template, name: string, shares: ShareRef[], used: Map<string, string>): InstallForm {
	const doc = parseDocument(tpl.compose);
	if (doc.errors.length > 0) throw new Error(doc.errors[0].message);
	const services = serviceNames(doc);
	if (services.length === 0) throw new Error('no services');
	const meta = readMeta(tpl.compose);
	const filled = { timezone: tpl.timezone, uid: String(tpl.uid), gid: String(tpl.gid) };

	const main = typeof meta.main === 'string' && services.includes(meta.main) ? meta.main : services[0];
	const form: InstallForm = { name, tips: meta.tips, risks: [], missingDevices: [], main, vars: [], paths: [], ports: [], own: { vars: [], paths: [], ports: [] } };
	const reserved = new Set<string>();
	const risks = new Set<Risk>();
	const content = record(doc.toJS().services);

	for (const service of services) {
		const entries = readService(doc, service);
		const plain = record(content[service]) as Record<string, unknown>;
		if (entries.privileged) risks.add('privileged');
		if (entries.network === 'host' || plain.pid === 'host') risks.add('hostNetwork');
		if (Array.isArray(plain.cap_add) && plain.cap_add.length > 0) risks.add('capabilities');
		for (const row of entries.paths) {
			if (/^\/(var\/)?run\/docker\.sock$/.test(row.host)) risks.add('dockerSocket');
			if (row.raw === undefined && row.host === '/') risks.add('rootMount');
		}
		for (const device of Array.isArray(plain.devices) ? plain.devices : []) {
			const host = typeof device === 'string' ? device.split(':')[0] : '';
			if (tpl.missingDevices.includes(host) && !form.missingDevices.includes(host)) form.missingDevices.push(host);
		}

		for (const pair of entries.env) {
			const info = meta.variables[pair.name] ?? {};
			// A dependent service uses the same generated secret, without a second editable copy.
			if (typeof info.from === 'string' && /^[A-Za-z_][A-Za-z0-9_]*$/.test(info.from)) continue;
			const fill = (['timezone', 'uid', 'gid'].includes(String(info.fill)) ? String(info.fill) : (FILLED[pair.name] ?? '')) as VarField['fill'];
			const field: VarField = {
				service,
				name: pair.name,
				label: info.label,
				help: info.help,
				placeholder: typeof info.placeholder === 'string' ? info.placeholder : '',
				value: pair.value,
				origin: 'template',
				secret: info.secret === 'generate',
				required: info.required === true,
				fill
			};
			if (field.secret) {
				field.value = generateSecret();
				field.origin = 'generated';
			} else if (fill) {
				field.value = filled[fill];
				field.origin = 'titanos';
			} else if (field.required && field.value === '') {
				field.origin = 'required';
			}
			form.vars.push(field);
		}

		entries.paths.forEach((row, index) => {
			// Named volumes and unusual entries stay as the template has them.
			if (row.raw !== undefined) return;
			const info = meta.paths[row.container] ?? {};
			let share: ShareRef | null = null;
			if (info.suggest === 'media') share = shares.find((candidate) => MEDIA_SHARES.includes(candidate.name.toLowerCase())) ?? null;
			if ((info.suggest === 'media' || info.suggest === 'share') && !share && shares.length === 1) share = shares[0];
			const wanted = info.suggest === 'media' || info.suggest === 'share';
			const field: PathField = { service, index, container: row.container, label: info.label, template: row.host, share, wanted, host: '', touched: false };
			field.host = defaultHost(field, name, tpl.appdata);
			form.paths.push(field);
		});

		entries.ports.forEach((row, index) => {
			if (row.raw !== undefined || row.host === '') return;
			const info = meta.ports[row.protocol === 'udp' ? `${row.container}/udp` : row.container] ?? {};
			const key = (port: number) => `${port}/${row.protocol}`;
			let port = Number(row.host);
			const blockedBy = used.get(key(port)) ?? null;
			// The next port nothing listens on and no other field of this app took.
			while (port < 65535 && (used.has(key(port)) || reserved.has(key(port)))) port++;
			reserved.add(key(port));
			form.ports.push({ service, index, container: row.container, protocol: row.protocol, label: info.label, webui: info.webui === true, templateHost: row.host, blockedBy: port === Number(row.host) ? null : (blockedBy ?? ''), host: String(port) });
			if (info.webui === true && typeof meta.main !== 'string') form.main = service;
		});
	}
	form.risks = [...risks];
	return form;
}

export type Problems = {
	name: '' | 'invalid' | 'exists';
	vars: ('' | 'required')[];
	/** "volatile" is a warning: the folder is not on a volume and is lost with a restart. */
	paths: ('' | 'invalid' | 'volatile')[];
	ports: ('' | 'invalid' | 'duplicate' | 'inUse')[];
	/** "replaces" is a hint: the value takes the place of the template's. */
	ownVars: ('' | 'empty' | 'invalid' | 'duplicate' | 'replaces')[];
	ownPaths: ('' | 'invalid' | 'volatile')[];
	ownPorts: ('' | 'empty' | 'invalid' | 'duplicate' | 'inUse')[];
	/** Number of required fields that are still empty. */
	missing: number;
	blocking: boolean;
};

const APP_NAME = /^[a-z0-9][a-z0-9_-]{0,31}$/;
const ENV_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;
// Folders outside the volumes a container may well be given.
const SYSTEM_PATHS = /^\/(dev|run|sys|proc|var\/run|etc\/localtime$|etc\/timezone$|lib\/modules)(\/|$)/;

const validPort = (value: string) => /^\d{1,5}$/.test(value) && Number(value) >= 1 && Number(value) <= 65535;

function pathProblem(host: string, container: string): '' | 'invalid' | 'volatile' {
	if (!host.trim() || !container.trim().startsWith('/')) return 'invalid';
	if (isRelative(host) || host.startsWith('/mnt/') || SYSTEM_PATHS.test(host)) return '';
	return 'volatile';
}

/** Checks the form as it is typed. */
export function check(form: InstallForm, installed: string[], used: Map<string, string>): Problems {
	const all = [...form.ports, ...form.own.ports].map((port) => (validPort(port.host) ? `${Number(port.host)}/${port.protocol}` : ''));
	const portProblem = (host: string, container: string, at: number) => {
		if (!validPort(host) || !validPort(container)) return 'invalid';
		if (all.some((key, other) => other !== at && key === all[at])) return 'duplicate';
		return used.has(all[at]) ? 'inUse' : '';
	};
	const names = form.vars.map((field) => field.name);

	const problems: Problems = {
		name: !APP_NAME.test(form.name) ? 'invalid' : installed.includes(form.name) ? 'exists' : '',
		vars: form.vars.map((field) => (field.required && field.value.trim() === '' ? 'required' : '')),
		paths: form.paths.map((field) => pathProblem(field.host, field.container)),
		ports: form.ports.map((field, at) => portProblem(field.host, field.container, at)),
		ownVars: form.own.vars.map((pair, at) => {
			if (pair.name === '') return 'empty';
			if (!ENV_NAME.test(pair.name)) return 'invalid';
			if (form.own.vars.some((other, index) => index !== at && other.name === pair.name)) return 'duplicate';
			return names.includes(pair.name) ? 'replaces' : '';
		}),
		ownPaths: form.own.paths.map((row) => pathProblem(row.host, row.container)),
		ownPorts: form.own.ports.map((row, at) => (row.host === '' && row.container === '' ? 'empty' : portProblem(row.host, row.container, form.ports.length + at))),
		missing: 0,
		blocking: false
	};
	problems.missing = problems.vars.filter(Boolean).length;
	problems.blocking =
		problems.name !== '' ||
		problems.missing > 0 ||
		[...problems.paths, ...problems.ownPaths].includes('invalid') ||
		[...problems.ports, ...problems.ownPorts].some(Boolean) ||
		problems.ownVars.some((problem) => problem !== '' && problem !== 'replaces');
	return problems;
}

/**
 * Writes what was entered into the template. Secrets are returned apart:
 * the compose file only refers to them as ${NAME}.
 */
export function build(tpl: Template, form: InstallForm): { compose: string; env: Record<string, string> } {
	const doc = parseDocument(tpl.compose);
	const env: Record<string, string> = {};
	const meta = readMeta(tpl.compose);
	const own = new Map(form.own.vars.map((pair) => [pair.name, pair.value]));

	for (const service of serviceNames(doc)) {
		const entries = readService(doc, service);
		const templatePorts = entries.ports.map((row) => row.host);

		for (const pair of entries.env) {
			const field = form.vars.find((candidate) => candidate.service === service && candidate.name === pair.name);
			if (!field) {
				const from = meta.variables[pair.name]?.from;
				if (typeof from === 'string' && /^[A-Za-z_][A-Za-z0-9_]*$/.test(from)) pair.value = '${' + from + '}';
				continue;
			}
			if (field.secret && field.value !== '') {
				env[field.name] = field.value;
				pair.value = '${' + field.name + '}';
			} else {
				pair.value = field.value;
			}
			// An own variable of the same name takes the place of the template's.
			if (service === form.main && own.has(pair.name)) pair.value = own.get(pair.name)!;
		}
		// A variable left empty is left out: many apps take an empty value
		// for a wrong one instead of for their default.
		entries.env = entries.env.filter((pair) => pair.value !== '');

		for (const field of form.paths) {
			if (field.service !== service || !entries.paths[field.index]) continue;
			// An untouched path in the app's data folder stays relative.
			const unchanged = !field.touched && !field.share;
			entries.paths[field.index].host = unchanged ? field.template : field.host.trim();
		}
		for (const field of form.ports) {
			if (field.service === service && entries.ports[field.index]) entries.ports[field.index].host = String(Number(field.host));
		}

		// The address of the web interface follows its port. It is a port
		// ("8096"), a port with a path ("8096/web") or a URL with "[host]".
		const webui = form.ports.find((field) => field.service === service && field.webui);
		const address = /^(https?:\/\/\[host\]:)?(\d+)(.*)$/.exec(entries.webui);
		if (webui && entries.webui === '') entries.webui = String(Number(webui.host));
		else if (address) {
			const moved = form.ports.find((field) => field.service === service && field.index === templatePorts.indexOf(address[2]));
			if (moved) entries.webui = (address[1] ?? '') + String(Number(moved.host)) + address[3];
		}

		// A device the NAS does not have would keep the container from starting.
		const devices = doc.getIn(['services', service, 'devices']);
		if (isSeq(devices) && form.missingDevices.length > 0) {
			devices.items = devices.items.filter((item) => !(isScalar(item) && form.missingDevices.includes(String(item.value).split(':')[0])));
			if (devices.items.length === 0) doc.deleteIn(['services', service, 'devices']);
		}

		if (service === form.main) {
			for (const pair of form.own.vars) {
				if (pair.name !== '' && !entries.env.some((existing) => existing.name === pair.name)) entries.env.push({ name: pair.name, value: pair.value });
			}
			for (const row of form.own.paths) entries.paths.push({ host: row.host.trim(), container: row.container.trim(), readOnly: false });
			for (const row of form.own.ports) {
				if (row.host !== '' || row.container !== '') entries.ports.push({ host: String(Number(row.host)), container: String(Number(row.container)), protocol: row.protocol });
			}
		}
		writeService(doc, service, entries);
	}
	// Long texts of the template stay on their lines.
	return { compose: doc.toString({ lineWidth: 0 }), env };
}

export { WEBUI_LABEL };
