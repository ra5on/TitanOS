export function vmLabel(vm: { name: string; displayName?: string }): string {
	return vm.displayName || vm.name;
}

export function validVMDisplayName(value: string): boolean {
	const name = value.trim();
	return name.length > 0 && [...name].length <= 80 && !/[\p{Cc}\p{Cf}]/u.test(name);
}

/** Only the identifier goes into paths, URLs and libvirt domain commands. */
export function newVMIdentifier(label: string, taken: string[]): string {
	const base = label.normalize('NFKD').replace(/\p{M}/gu, '').toLowerCase().replaceAll('ß', 'ss').replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 31) || 'vm';
	const names = new Set(taken);
	let name = base;
	for (let i = 2; names.has(name); i++) {
		const suffix = `-${i}`;
		name = base.slice(0, 31 - suffix.length) + suffix;
	}
	return name;
}
