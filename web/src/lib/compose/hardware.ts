import { parseDocument } from 'yaml';
import { readService, serviceNames, writeService } from './service.ts';

export function hardwareSelections(text: string): Record<string, string[]> {
	const doc = parseDocument(text, { merge: true });
	return Object.fromEntries(
		serviceNames(doc)
			.sort()
			.map((name) => [name, readService(doc, name).hardware]),
	);
}

export function withHardware(text: string, selections: Record<string, string[]>): string {
	const doc = parseDocument(text, { merge: true });
	for (const [service, devices] of Object.entries(selections)) {
		if (doc.hasIn(['services', service])) {
			const form = readService(doc, service);
			form.hardware = devices;
			writeService(doc, service, form);
		}
	}
	return doc.toString({ lineWidth: 0 });
}
