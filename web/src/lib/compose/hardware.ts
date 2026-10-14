import { parseDocument } from 'yaml';

export function hardwareSelections(text: string): Record<string, string[]> {
 const doc = parseDocument(text).toJS();
 const services = Object.keys(doc?.services ?? {}).sort();
 return Object.fromEntries(services.map((name, index) => {
  const requested = doc.services[name]['x-titanos-hardware'] ?? (index === 0 ? doc['x-titanos']?.hardware : []) ?? [];
  return [name, Array.isArray(requested) ? requested.filter((value): value is string => typeof value === 'string') : []];
 }));
}

export function withHardware(text: string, selections: Record<string, string[]>): string {
 const doc = parseDocument(text);
 for (const [service, devices] of Object.entries(selections)) {
  if (doc.hasIn(['services', service])) doc.setIn(['services', service, 'x-titanos-hardware'], devices);
 }
 return doc.toString();
}
