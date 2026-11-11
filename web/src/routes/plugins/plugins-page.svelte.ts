// Plugins: the page's state and actions – the catalog and what is installed,
// search and category, the plugin shown, running steps, the install form and
// the dialogs. The page and PluginDetails get this one object.
import { tick, untrack } from 'svelte';
import { api } from '#lib/api.ts';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { latestFor, type PluginProgress } from '#lib/plugin-progress.ts';
import { poll } from '#lib/poll.ts';
import { storage } from '#lib/storage.svelte.ts';
import {
	aboutOf,
	CATEGORIES,
	categoryOf,
	containsOf,
	isLink,
	localized,
	matches,
	pluginProblem,
	type Category,
	type Plugin,
	type Source,
	type Status,
	type Problem,
} from './plugins.ts';

export class PluginsPage {
	status = $state<Status | null>(null);
	error = $state<Problem | null>(null);
	picked = $state('');
	asking = $state(false);
	activate = $state(true);
	volume = $state('');
	busy = $state(false);
	refreshing = $state(false);
	// The plugin the remove dialog is about, fixed when it opens; the polled
	// list may change meanwhile.
	removing = $state<{ name: string; title: string } | null>(null);
	// Likewise the update dialog: plugin and the version it was opened for.
	updating = $state<{
		name: string;
		title: string;
		path: string;
		version: string;
		official: boolean;
		download?: boolean;
	} | null>(null);
	// Community plugins: a single link, and the list of sources.
	linking = $state(false);
	linkURL = $state('');
	linkPreview = $state<Plugin | null>(null);
	linkChecking = $state(false);
	sourcesOpen = $state(false);
	sourceURL = $state('');
	sourceAdding = $state(false);
	sourceRemoving = $state<Source | null>(null);
	sources = $derived(this.status?.sources ?? []);
	sourceName = (id?: string) => this.sources.find((entry) => entry.id === id)?.name ?? '';
	tailnetPending = $state(false);
	netbirdPending = $state(false);
	// Steps of installations, updates and removals as the NAS reports them.
	// They keep running when the page is left; a finished one is shown for the
	// plugins changed here and for failures until it is dismissed.
	operations = $state<PluginProgress[]>([]);
	actedOn = $state<string[]>([]);
	working = $derived(this.operations.some((entry) => entry.state === 'running'));

	locked = $derived(
		this.busy ||
			this.working ||
			this.tailnetPending ||
			this.netbirdPending ||
			!!this.status?.driverUpdate?.pending,
	);
	plugins = $derived(this.status?.plugins ?? []);
	current = $derived(this.plugins.find((plugin) => plugin.name === this.picked) ?? this.plugins[0]);
	volumes = $derived(
		(storage.status?.volumes ?? []).filter((entry) => entry.mounted).map((entry) => entry.name),
	);
	text = (texts: Record<string, string> | null) => localized(texts, i18n.locale);
	contains = (plugin: Plugin) => containsOf(plugin, i18n.locale);
	about = (plugin: Plugin) => aboutOf(plugin, i18n.locale);
	query = $state('');
	area = $state<Category | ''>('');
	shownPlugins = $derived(
		this.plugins.filter(
			(plugin) =>
				(!this.area || categoryOf(plugin) === this.area) &&
				matches(plugin, this.query.trim().toLowerCase(), i18n.locale),
		),
	);
	groups = $derived(
		CATEGORIES.map((id) => ({
			id,
			plugins: this.shownPlugins.filter((plugin) => categoryOf(plugin) === id),
		})).filter((group) => group.plugins.length > 0),
	);
	usedAreas = $derived(
		CATEGORIES.filter((id) => this.plugins.some((plugin) => categoryOf(plugin) === id)),
	);
	recommendations = $derived(this.status?.recommendations ?? []);
	fits = (plugin: Plugin) =>
		!plugin.installed && this.recommendations.some((entry) => entry.plugin === plugin.name);
	link = isLink;

	// Opens the plugin and, if it can be installed right away, its install form.
	showRecommended = async (name: string) => {
		const plugin = this.plugins.find((entry) => entry.name === name);
		if (!plugin || this.busy) return;
		this.query = '';
		this.area = '';
		await this.pick(name);
		if (!plugin.installed) this.ask();
	};

	stepsFor = (name: string): PluginProgress | undefined => {
		const entry = latestFor(this.operations, name);
		if (!entry) return undefined;
		return entry.state === 'running' || entry.state === 'failed' || this.actedOn.includes(name)
			? entry
			: undefined;
	};
	loadProgress = async () => {
		try {
			this.operations =
				(await api.get<{ operations: PluginProgress[] }>('/plugins/progress')).operations ?? [];
		} catch {
			// The status poll shows connection problems; the steps keep their last state.
		}
	};
	dismiss = async (entry: PluginProgress | undefined) => {
		if (!entry) return;
		this.operations = this.operations.filter((other) => other.id !== entry.id);
		this.actedOn = this.actedOn.filter((name) => name !== entry.plugin);
		try {
			await api.delete(`/plugins/progress/${encodeURIComponent(entry.id)}`);
		} catch {
			// Already gone, or still kept by the NAS: the next poll shows it again.
		}
	};

	problem = (err: unknown) => {
		this.error = pluginProblem(err);
	};

	// Polling goes on during a change, so the page keeps showing what the
	// NAS does; an answer that started before the change ended is dropped.
	changes = 0;
	load = async () => {
		void this.loadProgress();
		const started = this.changes;
		try {
			const next = await api.get<Status>('/plugins');
			if (started === this.changes) this.status = next;
		} catch (err) {
			if (!this.status) this.problem(err);
		}
	};
	refreshCatalog = async (force = false) => {
		if (this.busy || this.refreshing) return;
		this.refreshing = true;
		try {
			const updated = await api.post<Status>('/plugins/catalog', { force });
			if (!this.busy) this.status = updated;
		} catch (err) {
			this.problem(err);
		} finally {
			this.refreshing = false;
		}
	};

	run = async (action: () => Promise<Status>, plugin = '') => {
		if (plugin && !this.actedOn.includes(plugin)) this.actedOn = [...this.actedOn, plugin];
		this.busy = true;
		this.error = null;
		this.changes++;
		try {
			this.status = await action();
			this.asking = false;
			this.removing = null;
			this.updating = null;
		} catch (err) {
			this.problem(err);
		}
		this.busy = false;
		this.changes++;
		await this.load();
	};
	// Steps of the shown plugin outside its dialogs: after coming back to the
	// page, or a failure to look at.
	shown = $derived(this.current ? this.stepsFor(this.current.name) : undefined);
	removingSteps = $derived(this.removing ? this.stepsFor(this.removing.name) : undefined);
	updatingSteps = $derived(this.updating ? this.stepsFor(this.updating.name) : undefined);

	// The box shows what the NAS has; a refused change is turned back.
	toggle = async (name: string, box: HTMLInputElement) => {
		await this.run(() =>
			api.post<Status>(`/plugins/${name}/${box.checked ? 'enable' : 'disable'}`),
		);
		const plugin = this.status?.plugins.find((entry) => entry.name === name);
		if (plugin) box.checked = plugin.enabled;
	};

	// A new attempt starts without the marks of the previous one.
	clearFinished = (name: string) => {
		const entry = this.stepsFor(name);
		if (entry && entry.state !== 'running') void this.dismiss(entry);
	};

	openRemove = (plugin: Plugin) => {
		this.clearFinished(plugin.name);
		this.error = null;
		this.removing = { name: plugin.name, title: plugin.title };
	};

	openUpdate = (plugin: Plugin) => {
		if (!plugin.update) return;
		this.clearFinished(plugin.name);
		this.error = null;
		this.updating = { name: plugin.name, title: plugin.title, ...plugin.update };
	};

	openLink = () => {
		this.error = null;
		this.linkURL = '';
		this.linkPreview = null;
		this.activate = true;
		this.volume = this.status?.volume || this.volumes[0] || '';
		this.linking = true;
	};
	// Reads only the manifest; the image is downloaded when installing.
	checkLink = async () => {
		if (!this.linkURL.trim() || this.linkChecking) return;
		this.linkChecking = true;
		this.error = null;
		this.linkPreview = null;
		try {
			this.linkPreview = await api.post<Plugin>('/plugins/external/inspect', {
				url: this.linkURL.trim(),
			});
		} catch (err) {
			this.problem(err);
		} finally {
			this.linkChecking = false;
		}
	};
	installLink = async () => {
		const preview = this.linkPreview;
		if (!preview) return;
		await this.run(
			() =>
				api.post<Status>('/plugins/external', {
					url: this.linkURL.trim(),
					volume: this.volume,
					confirm: preview.name,
					activate: this.activate,
				}),
			preview.name,
		);
		if (!this.error) this.linking = false;
	};
	addSource = async () => {
		if (!this.sourceURL.trim() || this.sourceAdding) return;
		this.sourceAdding = true;
		this.error = null;
		try {
			await api.post('/plugins/sources', { url: this.sourceURL.trim() });
			this.sourceURL = '';
			await this.refreshCatalog(true);
		} catch (err) {
			this.problem(err);
		} finally {
			this.sourceAdding = false;
		}
	};
	removeSource = async (source: Source) => {
		this.error = null;
		try {
			await api.delete(`/plugins/sources/${encodeURIComponent(source.id)}`);
			this.sourceRemoving = null;
			await this.refreshCatalog(true);
		} catch (err) {
			this.problem(err);
		}
	};

	ask = () => {
		if (this.current) this.clearFinished(this.current.name);
		this.activate = true;
		this.volume = this.status?.volume || this.volumes[0] || '';
		this.error = null;
		this.asking = true;
	};

	details: HTMLElement | undefined = $state();
	pick = async (name: string) => {
		if (this.busy) return;
		this.picked = name;
		this.asking = false;
		this.error = null;
		// On a phone the details lie below the list: bring them into view.
		if (window.matchMedia('(max-width: 760px)').matches) {
			await tick();
			this.details?.scrollIntoView({ block: 'start', behavior: 'smooth' });
		}
	};

	constructor() {
		$effect(() => {
			void untrack(this.load).then(() => this.refreshCatalog());
			return poll(this.load, { interval: 5000, immediate: false });
		});
		// While something runs, follow its steps every second.
		$effect(() => {
			if (!this.busy && !this.working) return;
			return poll(this.loadProgress, { interval: 1000, immediate: false });
		});
	}
}
