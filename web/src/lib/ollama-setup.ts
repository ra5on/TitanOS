// "Ollama per click": the Ollama template of the app catalog, filled in by
// the same code as the install panel of the Apps page. Two additions: the
// GPU (only NVIDIA, the only kind the Ollama image uses) goes to the Ollama
// service, and Ollama's API gets a port on 127.0.0.1 only, so that titand
// reaches it while the home network does not.
import { isMap, isSeq, parseDocument } from 'yaml';
import { hardwareSelections, withHardware } from './compose/hardware.ts';
import { build, check, readTemplate, type Template } from './compose/template.ts';
import { serviceNames } from './compose/service.ts';

export const OLLAMA_TEMPLATE = 'ollama';
export const OLLAMA_PORT = 11434;

export type OllamaInstall = {
	name: string;
	compose: string;
	env: Record<string, string>;
	/** Host port of Ollama's API on 127.0.0.1. */
	port: number;
	/** The address titand uses. */
	address: string;
};

/** The service that runs ollama/ollama, or null. */
export function ollamaService(compose: string): string | null {
	const doc = parseDocument(compose, { merge: true });
	for (const name of serviceNames(doc)) {
		const image = doc.getIn(['services', name, 'image']);
		if (typeof image === 'string' && /(^|\/)ollama\/ollama(:|@|$)/.test(image)) return name;
	}
	return null;
}

/**
 * Fills in the template. installed are the app names there are, used the
 * ports in use ("port/protocol" → who), gpu the ID of a container device.
 * Throws with a message if the template cannot be installed as it is.
 */
export function prepareOllama(
	tpl: Template,
	installed: string[],
	used: Map<string, string>,
	gpu: string | null,
): OllamaInstall {
	let name = OLLAMA_TEMPLATE;
	for (let n = 2; installed.includes(name); n++) name = `${OLLAMA_TEMPLATE}-${n}`;
	const form = readTemplate(tpl, name, [], used);
	const problems = check(form, installed, used);
	if (problems.blocking) throw new Error('template_incomplete');
	const built = build(tpl, form);
	const service = ollamaService(built.compose);
	if (!service) throw new Error('template_without_ollama');
	const selections = hardwareSelections(built.compose);
	if (gpu) selections[service] = [...new Set([...(selections[service] ?? []), gpu])];
	const compose = withHardware(built.compose, selections);

	let port = OLLAMA_PORT;
	while (used.has(`${port}/tcp`) || form.ports.some((p) => Number(p.host) === port)) port++;
	const doc = parseDocument(compose, { merge: true });
	const ports = doc.getIn(['services', service, 'ports']);
	const binding = `127.0.0.1:${port}:${OLLAMA_PORT}`;
	if (isSeq(ports)) ports.add(doc.createNode(binding));
	else {
		const node = doc.getIn(['services', service]);
		if (!isMap(node)) throw new Error('template_without_ollama');
		node.set('ports', doc.createNode([binding]));
	}
	return {
		name,
		compose: doc.toString({ lineWidth: 0 }),
		env: built.env,
		port,
		address: `http://127.0.0.1:${port}`,
	};
}

/**
 * Whether text can be an Ollama model reference: optionally a registry host
 * (with port) and namespaces, a name and a tag, e.g. qwen3:8b,
 * library/llama3.1 or hf.co/user/model-GGUF:Q4_K_M. No digests, spaces or "..".
 */
export function validOllamaModel(text: string): boolean {
	if (text.length === 0 || text.length > 200 || text.includes('..')) return false;
	return /^([A-Za-z0-9][A-Za-z0-9._-]*(:[0-9]{1,5})?\/)*[A-Za-z0-9][A-Za-z0-9._-]*(:[A-Za-z0-9_][A-Za-z0-9._-]{0,127})?$/.test(text);
}
