// Docker › an app: the page's state and actions. The compose file as text
// is the single truth, the form a view of one service in it; besides that
// the state of the app's containers, applying, discarding and removing.
// The page and its parts (AppHead, ServiceFields, EditorDialogs) get this one object.
import { guardWindowClose } from '#lib/desktop/unsaved.svelte.ts';
import { poll } from '#lib/poll.ts';
import { beforeNavigate, goto } from '$app/navigation';
import { onDestroy, untrack } from 'svelte';
import { dockerName } from '#lib/docker-name.ts';
import type { NewEntry } from '#lib/components/appform/AddEntryDialog.svelte';
import { storedAdvanced } from '#lib/components/appform/ViewSwitch.svelte';
import { validCpuset, validDevice, validMemLimit } from '#lib/compose/template.ts';
import { parseDocument, type Document } from 'yaml';
import { page } from '$app/state';
import {
	api,
	ApiError,
	type AppProxyStatus,
	type DockerContainer,
	type DockerStatus,
} from '#lib/api.ts';
import { stackPrimary, sanitizeDockerLayout } from '#lib/docker-layout.ts';
import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
import { session } from '#lib/session.svelte.ts';
import { system } from '#lib/system.svelte.ts';
import { isRunning, webLinks } from '../../container.ts';
import { inTailnet } from '#lib/app-proxy.ts';
import {
	check,
	readService,
	serviceNames,
	writeService,
	type ServiceForm,
} from '#lib/compose/service.ts';

export class AppEditor {
	name = $derived(page.params.name ?? '');

	// The compose file as text is the single truth. The form is a view of
	// one service in it: every change in the form is written into the text,
	// and the text is read again when the form is shown.
	original = $state('');
	text = $state('');
	view = $state<'form' | 'compose'>('form');
	services = $state<string[]>([]);
	service = $state('');
	form = $state<ServiceForm | null>(null);
	yamlError = $state('');
	status = $state<DockerStatus | null>(null);
	loadError = $state('');
	loaded = $state(false);
	applying = $state(false);
	result = $state<{ ok: boolean; text: string; detail: string } | null>(null);

	doc: Document | undefined;
	advanced = $state(storedAdvanced());
	adding = $state(false);

	// The service as it was when the page opened, to mark what changed.
	before = $derived.by(() => {
		if (!this.original || !this.service) return null;
		const parsed = parseDocument(this.original, { merge: true });
		return parsed.errors.length || !serviceNames(parsed).includes(this.service)
			? null
			: readService(parsed, this.service);
	});
	differs = (key: keyof ServiceForm) =>
		!!this.before &&
		!!this.form &&
		JSON.stringify(this.before[key]) !== JSON.stringify(this.form[key]);
	envChanged = (pair: { name: string; value: string }) =>
		!!this.before &&
		this.before.env.find((entry) => entry.name === pair.name)?.value !== pair.value;
	rowChanged = (key: 'paths' | 'ports', index: number) =>
		!!this.before &&
		!!this.form &&
		JSON.stringify(this.before[key][index]) !== JSON.stringify(this.form[key][index]);

	addEntry = (entry: NewEntry) => {
		this.adding = false;
		if (!this.form) return;
		if (entry.kind === 'path')
			this.form.paths.push({
				host: entry.host,
				container: entry.container,
				readOnly: entry.readOnly,
			});
		else if (entry.kind === 'port')
			this.form.ports.push({
				host: entry.host,
				container: entry.container,
				protocol: entry.protocol,
			});
		else if (entry.kind === 'variable')
			this.form.env.push({ name: entry.name, value: entry.value });
		else if (entry.kind === 'label')
			this.form.labels.push({ name: entry.name, value: entry.value });
		else this.form.devices.push({ value: entry.device });
		this.commit();
	};

	/** Reads the text into the form. Returns false if it is not valid YAML. */
	parse = (): boolean => {
		const parsed = parseDocument(this.text, { merge: true });
		if (parsed.errors.length > 0) {
			this.yamlError = parsed.errors[0].message;
			return false;
		}
		this.yamlError = '';
		this.doc = parsed;
		this.services = serviceNames(parsed);
		if (!this.services.includes(this.service)) this.service = this.services[0] ?? '';
		this.form = this.service ? readService(parsed, this.service) : null;
		return true;
	};

	/** Writes the form into the text. Called after every change in the form. */
	commit = () => {
		if (!this.doc || !this.form || !this.service) return;
		writeService(this.doc, this.service, $state.snapshot(this.form));
		this.text = this.doc.toString();
	};

	show = (next: 'form' | 'compose') => {
		if (next === 'form' && !this.parse()) return; // stay in the text until it is valid
		this.view = next;
	};

	pickService = (next: string) => {
		this.service = next;
		if (this.doc) this.form = readService(this.doc, next);
	};

	loadCompose = (app = this.name) => {
		this.loadError = '';
		api.get<{ compose: string }>(`/docker/apps/${app}`).then(
			(res) => {
				if (app !== this.name) return;
				this.original = this.text = res.compose ?? '';
				this.loaded = true;
				if (!this.parse()) this.view = 'compose';
			},
			(err) => {
				if (app === this.name) this.loadError = apiErrorText(err);
			},
		);
	};
	statusError = $state('');
	loadStatus = async () => {
		if (this.applying) return;
		try {
			this.status = await api.get<DockerStatus>('/docker');
			this.statusError = '';
		} catch (err) {
			this.statusError = apiErrorText(err);
		}
	};

	// The head of the page: state and load of the app's containers, with
	// the actions of the Docker page for the container of the chosen service.
	members = $derived(
		(this.status?.containers ?? []).filter((container) => container.app === this.name),
	);
	runningCount = $derived(this.members.filter(isRunning).length);
	primary = $derived(
		stackPrimary(
			{ id: `app:${this.name}`, name: this.name, members: this.members },
			sanitizeDockerLayout(session.docker).primaryServices,
		),
	);
	target = $derived(
		this.members.find((container) => container.service === this.service) ??
			this.primary ??
			this.members[0],
	);
	host = $derived(system.info?.network?.ipv4.split('/')[0] || location.hostname);
	quickLinks = $derived(
		this.primary
			? webLinks(this.primary, this.host, '').filter((link) => link.icon !== 'external')
			: [],
	);
	// "Im Tailnet veröffentlichen": the app's own device in the tailnet.
	proxy = $state<AppProxyStatus | null>(null);
	tailnetDialog = $state<'publish' | 'remove' | null>(null);
	// Counts changes, so that the HTTPS name section below reads them.
	proxyVersion = $state(0);
	published = $derived(inTailnet(this.proxy?.apps?.find((route) => route.app === this.name)));
	loadProxy = async () => {
		try {
			this.proxy = await api.get<AppProxyStatus>('/docker/proxy');
		} catch {
			this.proxy = null;
		}
	};
	restarting = $state(false);
	actionError = $state('');
	restartAll = async () => {
		if (this.restarting || this.members.length === 0) return;
		this.restarting = true;
		this.actionError = '';
		try {
			for (const container of this.members)
				await api.post(`/docker/containers/${encodeURIComponent(container.id)}/restart`);
		} catch (err) {
			this.actionError = apiErrorText(err);
		} finally {
			this.restarting = false;
			await this.loadStatus();
		}
	};
	logs = $state<{ container: DockerContainer; text: string | null; error: string } | null>(null);
	logsRequest = 0;
	showLogs = async (container: DockerContainer) => {
		const request = ++this.logsRequest;
		if (this.logs?.container.id !== container.id) this.logs = { container, text: null, error: '' };
		else this.logs.error = '';
		try {
			const res = await api.get<{ logs: string }>(
				`/docker/containers/${encodeURIComponent(container.id)}/logs?tail=300`,
			);
			if (request === this.logsRequest && this.logs?.container.id === container.id)
				this.logs.text = res.logs;
		} catch (err) {
			if (request === this.logsRequest && this.logs?.container.id === container.id)
				this.logs.error = apiErrorText(err);
		}
	};
	confirmDiscard = $state(false);
	discardChanges = () => {
		this.confirmDiscard = false;
		this.text = this.original;
		this.result = null;
		if (!this.parse()) this.view = 'compose';
	};

	// Ports other containers already publish, for the live check.
	taken = $derived.by(() => {
		const map = new Map<string, string>();
		for (const container of this.status?.containers ?? []) {
			if (container.app === this.name) continue;
			for (const port of container.ports)
				map.set(`${port.host}/${port.protocol}`, dockerName(container, 'distinct'));
		}
		return map;
	});
	problems = $derived(this.form ? check(this.form, this.taken, t) : null);
	changed = $derived(this.text !== this.original);
	leaving = $state(false);
	pendingURL = '';
	allowNavigation = false;
	// Removing the app (danger zone): afterwards the app is gone, so the page
	// returns to the Docker overview without asking about unsaved changes.
	removing = $state(false);
	removeBusy = $state(false);
	removeError = $state('');
	removeApp = async (purge: boolean) => {
		this.removeBusy = true;
		this.removeError = '';
		try {
			await api.delete(
				`/docker/apps/${encodeURIComponent(this.name)}${purge ? `?deleteData=${encodeURIComponent(this.name)}` : ''}`,
			);
			this.removing = false;
			this.allowNavigation = true;
			await goto('/docker');
		} catch (err) {
			this.removeError = apiErrorText(err);
		} finally {
			this.removeBusy = false;
		}
	};
	discard = () => {
		this.allowNavigation = true;
		this.leaving = false;
		if (this.pendingURL) void goto(this.pendingURL);
	};

	limitsBad = $derived(
		!!this.form &&
			(!validCpuset(this.form.cpuset) ||
				!validMemLimit(this.form.memLimit) ||
				this.form.devices.some((row) => row.raw === undefined && !validDevice(row.value))),
	);
	blocked = $derived(
		this.applying ||
			!this.changed ||
			(this.view === 'form' ? !!this.problems?.any || this.limitsBad : false),
	);
	updateAvailable = $derived(this.members.some((container) => container.updateAvailable));
	hiddenCount = $derived(this.form ? 4 + this.form.labels.length : 0);

	// Leaving the page ends the wait for the outcome of an apply.
	destroyed = false;

	apply = async () => {
		if (this.view === 'compose' && !this.parse()) return;
		this.applying = true;
		this.result = null;
		try {
			await api.put(`/docker/apps/${this.name}`, { compose: this.text });
			// The containers are recreated in the background; wait for the outcome.
			for (;;) {
				await new Promise((resolve) => setTimeout(resolve, 1500));
				if (this.destroyed) return;
				const next = await api.get<DockerStatus>('/docker');
				if (this.destroyed) return;
				this.status = next;
				const job = this.status.jobs[this.name];
				if (job?.state === 'running') continue;
				if (!job) {
					// The NAS registers the job before it answers; a missing job
					// means it is gone (titand restarted), so its outcome is unknown.
					this.result = { ok: false, text: t('editor.jobLost'), detail: '' };
					break;
				}
				if (job.state === 'done') {
					this.original = this.text;
					this.result = { ok: true, text: t('editor.applied'), detail: '' };
					if (this.pendingURL) {
						this.allowNavigation = true;
						this.leaving = false;
						void goto(this.pendingURL);
					}
				} else {
					this.result = { ok: false, text: t('editor.failed'), detail: job.output };
				}
				break;
			}
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			this.result = {
				ok: false,
				text: errorText(code),
				detail:
					code === 'invalid_compose' || code === 'docker_failed' ? (err as ApiError).message : '',
			};
		}
		this.applying = false;
	};

	constructor() {
		guardWindowClose(() => this.changed && !this.applying);
		$effect(() => {
			const app = this.name;
			untrack(() => this.loadCompose(app));
		});
		$effect(() => poll(this.loadStatus, { interval: 5000 }));
		$effect(() => {
			void this.name;
			void this.loadProxy();
		});
		beforeNavigate((navigation) => {
			if (!this.changed || this.applying || this.allowNavigation) return;
			navigation.cancel();
			if (!navigation.willUnload) {
				this.pendingURL = navigation.to?.url.href ?? '';
				this.leaving = true;
			}
		});
		onDestroy(() => (this.destroyed = true));
	}
}
