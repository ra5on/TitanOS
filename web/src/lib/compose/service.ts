// Reads one service of a compose file into the fields of the form and
// writes them back. Whatever the form does not understand stays as it is.

import { isAlias, isMap, isNode, isScalar, Scalar, type Document } from 'yaml';

export type PathRow = { host: string; container: string; readOnly: boolean; raw?: unknown };
export type PortRow = { host: string; container: string; protocol: 'tcp' | 'udp'; raw?: unknown };
export type Pair = { name: string; value: string };

export type ServiceForm = {
	image: string;
	restart: string;
	network: string;
	privileged: boolean;
	webui: string;
	paths: PathRow[];
	ports: PortRow[];
	env: Pair[];
	labels: Pair[];
};

export const WEBUI_LABEL = 'titanos.webui';
export const RESTART_POLICIES = ['no', 'always', 'unless-stopped', 'on-failure'];
export const NETWORKS = ['bridge', 'host', 'none'];

const PORT = /^(?:(\d+):)?(\d+)(?:\/(tcp|udp))?$/;

/** Names of the services in a compose file. */
export function serviceNames(doc: Document): string[] {
	const services = doc.get('services');
	return isMap(services) ? services.items.map((item) => String(isScalar(item.key) ? item.key.value : item.key)) : [];
}

function pairs(value: unknown): Pair[] {
	if (Array.isArray(value)) {
		return value.map((entry) => {
			const [name, ...rest] = String(entry).split('=');
			return { name, value: rest.join('=') };
		});
	}
	if (value && typeof value === 'object') {
		return Object.entries(value).map(([name, v]) => ({ name, value: v === null || v === undefined ? '' : String(v) }));
	}
	return [];
}

function list(value: unknown): unknown[] {
	return Array.isArray(value) ? value : [];
}

export function readService(doc: Document, service: string): ServiceForm {
	// Read as plain values, so that YAML aliases count as what they stand for.
	const plain = (doc.toJS()?.services?.[service] ?? {}) as Record<string, unknown>;
	const at = (key: string) => plain[key];
	const text = (key: string, fallback = '') => {
		const value = at(key);
		return value === undefined || value === null || typeof value === 'object' ? fallback : String(value);
	};
	const labels = pairs(at('labels'));

	return {
		image: text('image'),
		// Values the form has no option for ("on-failure:5", "service:db")
		// are kept as they are.
		restart: text('restart', 'no'),
		network: text('network_mode', 'bridge'),
		privileged: at('privileged') === true,
		webui: labels.find((label) => label.name === WEBUI_LABEL)?.value ?? '',
		labels: labels.filter((label) => label.name !== WEBUI_LABEL),
		env: pairs(at('environment')),
		paths: list(at('volumes')).map((entry): PathRow => {
			if (typeof entry === 'string') {
				const parts = entry.split(':');
				// "name:/path" with a named volume or anything unusual stays untouched.
				if ((parts.length === 2 || (parts.length === 3 && ['ro', 'rw'].includes(parts[2]))) && /^[./~]/.test(parts[0])) {
					return { host: parts[0], container: parts[1], readOnly: parts[2] === 'ro' };
				}
			} else if (entry && typeof entry === 'object') {
				// The long form, as long as it says nothing the short one cannot.
				const { type, source, target, read_only: readOnly, bind, ...rest } = entry as Record<string, unknown>;
				// Creating a missing folder is what the short form does anyway.
				const plainBind = bind === undefined || JSON.stringify(bind) === '{"create_host_path":true}';
				if (type === 'bind' && typeof source === 'string' && typeof target === 'string' && /^[./~]/.test(source) && plainBind && Object.keys(rest).length === 0) {
					// Some templates append the short form's ":ro" here as well.
					const mode = /:(ro|rw)$/;
					return { host: source.replace(mode, ''), container: target.replace(mode, ''), readOnly: readOnly === true || source.endsWith(':ro') || target.endsWith(':ro') };
				}
			}
			return { host: '', container: '', readOnly: false, raw: entry };
		}),
		ports: list(at('ports')).map((entry): PortRow => {
			const match = typeof entry === 'string' || typeof entry === 'number' ? PORT.exec(String(entry)) : null;
			if (match) return { host: match[1] ?? '', container: match[2], protocol: (match[3] as 'tcp' | 'udp') ?? 'tcp' };
			if (entry && typeof entry === 'object') {
				const { target, published, protocol = 'tcp', mode = 'ingress', ...rest } = entry as Record<string, unknown>;
				const number = (value: unknown) => (typeof value === 'number' || typeof value === 'string') && /^\d+$/.test(String(value));
				if (number(target) && (published === undefined || number(published)) && (protocol === 'tcp' || protocol === 'udp') && mode === 'ingress' && Object.keys(rest).length === 0) {
					return { host: published === undefined ? '' : String(published), container: String(target), protocol };
				}
			}
			return { host: '', container: '', protocol: 'tcp', raw: entry };
		})
	};
}

/**
 * A string that is written in quotes. "8080:80" and "22:22" must stay
 * strings; some YAML readers take unquoted ones for base-60 numbers.
 */
function quoted(value: string): Scalar {
	const node = new Scalar(value);
	node.type = Scalar.QUOTE_DOUBLE;
	return node;
}

/**
 * Writes the form back into the document. Only what differs from the
 * document is written: everything else keeps its form, comments and anchors.
 */
export function writeService(doc: Document, service: string, form: ServiceForm) {
	const before = readService(doc, service);
	const same = (key: keyof ServiceForm) => JSON.stringify(before[key]) === JSON.stringify(form[key]);
	const path = (key: string) => ['services', service, key];
	const set = (key: string, value: unknown) => {
		const empty = value === undefined || (Array.isArray(value) && value.length === 0) || (value !== null && typeof value === 'object' && !Array.isArray(value) && Object.keys(value).length === 0);
		if (empty) return void doc.deleteIn(path(key));
		const node = doc.createNode(value);
		// Other services may refer to this entry by its anchor.
		const old = doc.getIn(path(key), true);
		if (isNode(old) && !isAlias(old) && old.anchor && !isAlias(node)) node.anchor = old.anchor;
		doc.setIn(path(key), node);
	};

	if (!same('image')) set('image', form.image.trim());
	if (!same('restart')) set('restart', form.restart === 'no' ? undefined : form.restart);
	// With the network of the host or none there is nothing to publish.
	const publishes = form.network !== 'host' && form.network !== 'none';
	if (!same('network')) {
		// Compose's own networks stay as they are unless the mode is host or none.
		if (form.network === 'bridge') doc.deleteIn(path('network_mode'));
		else doc.setIn(path('network_mode'), form.network);
		if (!publishes) doc.deleteIn(path('ports'));
	}
	if (!same('privileged')) set('privileged', form.privileged ? true : undefined);
	if (!same('paths')) {
		set(
			'volumes',
			form.paths.map((row) => row.raw ?? quoted(`${row.host.trim()}:${row.container.trim()}${row.readOnly ? ':ro' : ''}`))
		);
	}
	if (publishes && !same('ports')) {
		set(
			'ports',
			form.ports.map((row) => row.raw ?? quoted(`${row.host.trim() ? row.host.trim() + ':' : ''}${row.container.trim()}${row.protocol === 'udp' ? '/udp' : ''}`))
		);
	}
	if (!same('env')) set('environment', Object.fromEntries(form.env.filter((pair) => pair.name.trim()).map((pair) => [pair.name.trim(), pair.value])));
	if (!same('labels') || !same('webui')) {
		const labels = form.labels.filter((pair) => pair.name.trim()).map((pair) => [pair.name.trim(), pair.value]);
		if (form.webui.trim()) labels.push([WEBUI_LABEL, form.webui.trim()]);
		set('labels', Object.fromEntries(labels));
	}
}

export type Problems = { ports: string[]; env: string[]; paths: string[]; any: boolean };

/**
 * Checks the form as it is typed. `taken` maps "port/protocol" to the name
 * of another container that already publishes it.
 */
export function check(form: ServiceForm, taken: Map<string, string>, t: (key: 'editor.portInvalid' | 'editor.portDuplicate' | 'editor.portInUse' | 'editor.envInvalid' | 'editor.envDuplicate' | 'editor.pathInvalid', params?: Record<string, string>) => string): Problems {
	const validPort = (value: string) => /^\d+$/.test(value) && Number(value) >= 1 && Number(value) <= 65535;
	const ports = form.ports.map((row, index) => {
		if (row.raw !== undefined) return '';
		if (!validPort(row.container) || (row.host !== '' && !validPort(row.host))) return t('editor.portInvalid');
		if (row.host === '') return '';
		const key = `${Number(row.host)}/${row.protocol}`;
		if (form.ports.some((other, at) => at !== index && other.raw === undefined && other.host !== '' && `${Number(other.host)}/${other.protocol}` === key)) return t('editor.portDuplicate');
		const user = taken.get(key);
		return user ? t('editor.portInUse', { name: user }) : '';
	});
	const env = form.env.map((pair, index) => {
		if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(pair.name)) return t('editor.envInvalid');
		return form.env.some((other, at) => at !== index && other.name === pair.name) ? t('editor.envDuplicate') : '';
	});
	const paths = form.paths.map((row) => (row.raw === undefined && (!row.host.trim() || !row.container.trim().startsWith('/')) ? t('editor.pathInvalid') : ''));
	return { ports, env, paths, any: [...ports, ...env, ...paths].some(Boolean) || !form.image.trim() };
}
