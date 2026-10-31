// A neutral mark for the operating system of a VM, guessed from the
// template it was created with and, for "other", from its names and its
// installation medium. Only TitanOS' own line icons, no vendor logos.
import type { VM } from './api.ts';
import type { IconName } from './components/Icon.svelte';

type VMNames = Pick<VM, 'template' | 'name' | 'iso' | 'description'> & { displayName?: string };

const LINUX =
	/linux|ubuntu|debian|fedora|alpine|\barch\b|mint|cent ?os|rocky|alma|suse|kali|manjaro|pop[-_ ]?os|nixos|gentoo|proxmox|truenas[-_ ]?scale|rhel/i;
const WINDOWS = /windows|\bwin ?(7|8|10|11|xp|20\d\d)\b|win(10|11)/i;
const HOME = /home[-_ ]?assistant|\bhaos/i;
const FIREWALL = /opnsense|pfsense|ipfire|openwrt|firewall/i;

export function vmOsIcon(vm: VMNames): IconName {
	if (vm.template === 'windows') return 'windows';
	if (vm.template === 'linux') return 'linux';
	const text = [vm.name, vm.displayName ?? '', vm.iso, vm.description].join(' ');
	if (HOME.test(text)) return 'home';
	if (FIREWALL.test(text)) return 'shield';
	if (WINDOWS.test(text)) return 'windows';
	if (LINUX.test(text)) return 'linux';
	return 'monitor';
}
