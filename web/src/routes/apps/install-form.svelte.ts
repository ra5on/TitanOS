// Installing an app: the page's state and actions – the template and the
// form made from it, the checks, the compose text, the HTTPS name, and the
// job of the installation until the app runs. InstallPage and its parts
// (InstallFields, InstallProgress) get this one object.
import { guardWindowClose } from '#lib/desktop/unsaved.svelte.ts';
import { beforeNavigate, goto } from '$app/navigation';
import { onDestroy, onMount } from 'svelte';
import {
	api,
	ApiError,
	type AppProxyStatus,
	type AppStore,
	type CatalogApp,
	type DockerStatus,
	type Share,
} from '#lib/api.ts';
import { pullProgress } from '#lib/appform.ts';
import { proxyBody, proxyForm, suggestedProxyName, validProxyName } from '#lib/app-proxy.ts';
import type { NewEntry } from '#lib/components/appform/AddEntryDialog.svelte';
import { storedAdvanced } from '#lib/components/appform/ViewSwitch.svelte';
import { where } from '#lib/components/appform/where.ts';
import { fromCasaOS, isCasaOS } from '#lib/compose/casaos.ts';
import { hardwareSelections, withHardware } from '#lib/compose/hardware.ts';
import { validImage } from '#lib/compose/image.ts';
import {
	build,
	check,
	defaultHost,
	isRelative,
	localize,
	readTemplate,
	type InstallForm,
	type PathField as TemplatePath,
	type PortField,
	type Template,
	type VarField,
} from '#lib/compose/template.ts';
import { dockerName } from '#lib/docker-name.ts';
import { apiErrorText, errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
import { system } from '#lib/system.svelte.ts';
import { webLinks } from '../docker/container.ts';

export class Installer {
	readonly #source: () => string;
	readonly #id: () => string;
	/** The app of the catalog; without it an own container is set up. */
	get source() {
		return this.#source();
	}
	get id() {
		return this.#id();
	}
	own = $derived(!this.source || !this.id);

	app = $state<CatalogApp | null>(null);
	sourceName = $state('');
	docker = $state<DockerStatus | null>(null);
	shares = $state<Share[]>([]);
	proxy = $state<AppProxyStatus | null>(null);
	tpl = $state<Template | null>(null);
	form = $state<InstallForm | null>(null);
	loadError = $state('');
	view = $state<'form' | 'compose'>('form');
	text = $state('');
	draftEnv: Record<string, string> = {};
	selectedHardware = $state<Record<string, string[]>>({});
	advanced = $state(storedAdvanced());
	// The basic view shows one image: that of the main service (Nextcloud,
	// not its database), else the first.
	mainImage = $derived(
		Math.max(0, this.form?.images.findIndex((field) => field.service === this.form?.main) ?? 0),
	);
	adding = $state(false);
	error = $state<{ text: string; detail: string } | null>(null);
	// The HTTPS name the app gets once it runs (Settings › HTTPS names for apps).
	https = $state({ enabled: true, name: '' });
	httpsTouched = false;

	// After "Install": the job of the app until it runs.
	phase = $state<'form' | 'installing' | 'done' | 'failed'>('form');
	job = $state<{ state: string; output: string } | null>(null);
	started = 0;
	elapsed = $state(0);
	httpsResult = $state<{ ok: boolean; text: string } | null>(null);

	installedApps = $derived(this.docker?.apps ?? []);
	ready = $derived(!!this.docker?.settings.enabled && this.docker.running);
	host = $derived(system.info?.network?.ipv4.split('/')[0] || location.hostname);
	threads = $derived(system.info?.cpu.threads ?? 0);
	shareRefs = $derived(
		this.shares
			.filter((share) => share.available)
			.map((share) => ({ name: share.name, volume: share.volume })),
	);

	// Who has which port: containers by name, everything else is the system.
	used = $derived.by(() => {
		const map = new Map<string, string>();
		for (const port of this.tpl?.listening ?? []) map.set(`${port.port}/${port.protocol}`, '');
		for (const container of this.docker?.containers ?? [])
			for (const port of container.ports)
				map.set(`${port.host}/${port.protocol}`, dockerName(container, 'distinct'));
		return map;
	});
	by = (name: string | null | undefined) => name || t('apps.system');

	problems = $derived(this.form ? check(this.form, this.installedApps, this.used) : null);
	imageProblem = $derived(
		!!this.form && this.form.images.some((field) => !validImage(field.image)),
	);

	// Ask before leaving a form with entries of the user.
	baseline = '';
	snapshot = () =>
		JSON.stringify({
			form: this.form,
			selectedHardware: this.selectedHardware,
			https: this.https,
			text: this.text,
		});
	leaving = $state<string | null>(null);
	allowLeave = false;
	leave = (href: string) => {
		this.allowLeave = true;
		void goto(href);
	};

	/** Paths nobody changed follow the name of the app; so does the HTTPS name. */
	renamed = () => {
		if (!this.form || !this.tpl) return;
		for (const field of this.form.paths)
			if (!field.touched) field.host = defaultHost(field, this.form.name, this.tpl.appdata);
		if (!this.httpsTouched) this.https.name = suggestedProxyName(this.form.name);
	};
	editPath = (field: TemplatePath) => {
		if (this.form && this.tpl)
			field.touched = field.host !== defaultHost(field, this.form.name, this.tpl.appdata);
	};

	origin = (kind: 'template' | 'titanos' | 'generated' | 'required' | 'own') => {
		return t(`apps.origin.${kind}`);
	};
	varOrigin = (field: VarField) => {
		if (field.required && field.value.trim() === '') return 'required' as const;
		return field.origin === 'required' ? ('own' as const) : field.origin;
	};
	pathOrigin = (field: TemplatePath) => {
		if (field.touched) return 'own' as const;
		return field.share || isRelative(field.template) ? ('titanos' as const) : ('template' as const);
	};
	varHint = (field: VarField): string => {
		const help = localize(field.help, i18n.locale);
		const note =
			field.secret && field.origin === 'generated'
				? t('apps.note.generated')
				: field.fill && field.origin === 'titanos'
					? t(`apps.note.${field.fill}`)
					: '';
		return [
			t('appform.variable', { name: field.name }),
			help || note,
			this.origin(this.varOrigin(field)),
		]
			.filter(Boolean)
			.join(' · ');
	};
	pathHint = (field: TemplatePath, problem: string): { text: string; tone: '' | 'warn' } => {
		if (problem === 'invalid') return { text: t('apps.pathInvalid'), tone: 'warn' };
		if (problem === 'volatile') return { text: t('apps.pathVolatile'), tone: 'warn' };
		const note = field.share
			? t('apps.note.share', { name: field.share.name })
			: !field.touched && isRelative(field.template) && field.wanted
				? t('apps.note.noShare')
				: '';
		return {
			text: [
				t('appform.containerPathHint', { path: field.container }),
				where(field.host) || note,
				this.origin(this.pathOrigin(field)),
			]
				.filter(Boolean)
				.join(' · '),
			tone: '',
		};
	};
	portHint = (field: PortField, problem: string): { text: string; tone: '' | 'warn' | 'ok' } => {
		const target = t('appform.containerPortHint', { port: `${field.container}/${field.protocol}` });
		if (problem === 'invalid') return { text: t('apps.portInvalid'), tone: 'warn' };
		if (problem === 'duplicate') return { text: t('apps.portDuplicate'), tone: 'warn' };
		if (problem === 'inUse')
			return {
				text: `${t('apps.portInUse', { by: this.by(this.used.get(`${Number(field.host)}/${field.protocol}`)) })} · ${target}`,
				tone: 'warn',
			};
		if (field.blockedBy !== null)
			return {
				text: `${t('apps.portMoved', { port: field.templateHost, by: this.by(field.blockedBy), free: field.host })} · ${target}`,
				tone: 'warn',
			};
		return { text: `${target} · ${t('apps.portFree', { port: field.host })}`, tone: '' };
	};

	/** The next port nothing uses, for "take <port>". */
	freePort = (from: number, protocol: string): number => {
		const taken = new Set(
			[...(this.form?.ports ?? []), ...(this.form?.own.ports ?? [])].map(
				(port) => `${Number(port.host)}/${port.protocol}`,
			),
		);
		let port = from;
		while (
			port < 65535 &&
			(this.used.has(`${port}/${protocol}`) || taken.has(`${port}/${protocol}`))
		)
			port++;
		return port;
	};

	addEntry = (entry: NewEntry) => {
		if (!this.form) return;
		this.adding = false;
		if (entry.kind === 'path')
			this.form.own.paths.push({ host: entry.host, container: entry.container });
		else if (entry.kind === 'port')
			this.form.own.ports.push({
				host: entry.host,
				container: entry.container,
				protocol: entry.protocol,
			});
		else if (entry.kind === 'variable')
			this.form.own.vars.push({ name: entry.name, value: entry.value });
		else if (entry.kind === 'label')
			this.form.own.labels.push({ name: entry.name, value: entry.value });
		else this.form.own.devices.push(entry.device);
	};

	// Lines only the advanced view shows, for "Show more settings (n)".
	hiddenCount = $derived(
		this.form ? 3 + (this.form.images.length > 1 ? this.form.images.length - 1 : 0) : 0,
	);

	// The port the HTTPS name leads to: the web interface of the main service.
	webPort = $derived(
		this.form?.ports.find((port) => port.webui && port.service === this.form?.main) ??
			this.form?.ports.find((port) => port.service === this.form?.main) ??
			null,
	);
	offerHttps = $derived(!!this.proxy && !this.proxy.off && !!this.webPort);
	httpsProblem = $derived(
		this.offerHttps && this.https.enabled && !validProxyName(this.https.name.trim().toLowerCase())
			? t('appform.httpsNameInvalid')
			: this.offerHttps &&
				  this.https.enabled &&
				  this.proxy!.apps.some((route) => route.name === this.https.name.trim().toLowerCase())
				? t('appform.httpsNameTaken')
				: '',
	);

	show = (next: 'form' | 'compose') => {
		if (next === this.view || !this.tpl || !this.form) return;
		if (next === 'compose') {
			const built = build(this.tpl, $state.snapshot(this.form));
			this.text = withHardware(built.compose, this.selectedHardware);
			this.draftEnv = built.env;
		} else if (!this.readCompose()) return;
		this.view = next;
	};
	readCompose = (): boolean => {
		if (!this.tpl || !this.form) return false;
		try {
			const next = { ...this.tpl, compose: this.text };
			const edited = readTemplate(next, this.form.name, [], this.used, this.draftEnv);
			this.selectedHardware = hardwareSelections(this.text);
			this.tpl = next;
			this.form = edited;
			this.error = null;
			return true;
		} catch (err) {
			this.error = { text: t('editor.yamlError'), detail: err instanceof Error ? err.message : '' };
			return false;
		}
	};

	destroyed = false;

	install = async () => {
		if (!this.tpl || !this.form) return;
		if (this.view === 'compose' && !this.readCompose()) return;
		if (
			check(this.form, this.installedApps, this.used).blocking ||
			this.imageProblem ||
			this.httpsProblem
		) {
			this.error = { text: t('apps.correctFields'), detail: '' };
			return;
		}
		this.error = null;
		const name = this.form.name;
		const result = build(this.tpl, $state.snapshot(this.form));
		this.phase = 'installing';
		this.started = Date.now();
		this.job = { state: 'running', output: '' };
		try {
			await api.post('/docker/apps', {
				name,
				compose: withHardware(result.compose, this.selectedHardware),
				env: result.env,
			});
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			this.phase = 'form';
			this.job = null;
			this.error = {
				text: errorText(code),
				detail:
					code === 'invalid_compose' || code === 'docker_failed' ? (err as ApiError).message : '',
			};
			return;
		}
		// Images are pulled in the background; the job tells how far it is.
		for (;;) {
			await new Promise((resolve) => setTimeout(resolve, 1200));
			if (this.destroyed) return;
			this.elapsed = Math.round((Date.now() - this.started) / 1000);
			try {
				this.docker = await api.get<DockerStatus>('/docker');
			} catch {
				continue; // a lost answer is asked again
			}
			const current = this.docker.jobs[name];
			if (!current) {
				this.job = { state: 'failed', output: this.job?.output ?? '' };
				this.phase = 'failed';
				return;
			}
			this.job = current;
			if (current.state === 'running') continue;
			if (current.state === 'failed') {
				this.phase = 'failed';
				return;
			}
			break;
		}
		if (this.offerHttps && this.https.enabled && this.webPort) {
			const route = proxyForm(null, this.https.name.trim().toLowerCase());
			route.enabled = true;
			route.port = Number(this.webPort.host);
			try {
				await api.put(`/docker/proxy/apps/${encodeURIComponent(name)}`, proxyBody(route, false));
				this.httpsResult = { ok: true, text: '' };
			} catch (err) {
				this.httpsResult = { ok: false, text: apiErrorText(err) };
			}
			try {
				this.docker = await api.get<DockerStatus>('/docker');
			} catch {
				// The links below may lack the new name until the page is opened again.
			}
		}
		this.phase = 'done';
	};

	progress = $derived(this.job ? pullProgress(this.job.output) : null);
	// The main service first; the others show in the advanced view.
	gpuServices = $derived(
		Object.keys(this.selectedHardware)
			.sort((a, b) => Number(b === this.form?.main) - Number(a === this.form?.main))
			.filter((service, index) => this.advanced || index === 0),
	);
	installedName = $derived(this.form?.name ?? '');
	links = $derived(
		(this.docker?.containers ?? [])
			.filter((container) => container.app === this.installedName)
			.flatMap((container) => webLinks(container, this.host, ''))
			.filter((link) => link.icon !== 'external'),
	);
	title = $derived(
		this.own
			? t('appform.ownTitle')
			: t('appform.installTitle', { name: this.app?.name ?? this.id }),
	);

	constructor(source: () => string, id: () => string) {
		this.#source = source;
		this.#id = id;
		onMount(async () => {
			try {
				const [store, status, shareList] = await Promise.all([
					this.own ? Promise.resolve(null) : api.get<AppStore>('/apps'),
					api.get<DockerStatus>('/docker'),
					api.get<Share[]>('/shares').catch(() => [] as Share[]),
				]);
				this.docker = status;
				this.shares = shareList;
				if (store) {
					this.app =
						store.apps.find((entry) => entry.source === this.source && entry.id === this.id) ??
						null;
					this.sourceName =
						store.sources.find((entry) => entry.id === this.source)?.name ?? this.source;
					if (!this.app) throw new ApiError(404, 'template_not_found', '');
				}
				this.proxy = await api.get<AppProxyStatus>('/docker/proxy').catch(() => null);
				const res = await api.get<Template>(
					this.own
						? '/apps/blank'
						: `/apps/sources/${encodeURIComponent(this.source)}/apps/${encodeURIComponent(this.id)}`,
				);
				// A second copy of an installed app needs a name of its own.
				const base = this.own ? 'container' : (this.app?.id ?? this.id);
				let name = base;
				for (let n = 2; this.installedApps.includes(name); n++) name = `${base.slice(0, 29)}-${n}`;
				// Templates of CasaOS stores are translated first.
				if (isCasaOS(res.compose)) {
					res.compose = fromCasaOS(res.compose, base, {
						timezone: res.timezone,
						uid: res.uid,
						gid: res.gid,
						host: this.host,
					});
				}
				this.tpl = res;
				this.selectedHardware = hardwareSelections(res.compose);
				this.form = readTemplate(
					res,
					name,
					this.shares
						.filter((share) => share.available)
						.map((share) => ({ name: share.name, path: share.path })),
					this.used,
				);
				if (this.own) this.form.images = [{ service: this.form.main, image: '' }];
				this.https.name = suggestedProxyName(name);
			} catch (err) {
				this.loadError =
					err instanceof Error && !(err instanceof ApiError)
						? `${t('apps.templateBroken')} ${err.message}`
						: apiErrorText(err);
			}
		});
		$effect(() => {
			if (this.form && !this.baseline) this.baseline = this.snapshot();
		});
		guardWindowClose(
			() =>
				!this.allowLeave &&
				this.phase === 'form' &&
				!!this.form &&
				!!this.baseline &&
				this.snapshot() !== this.baseline,
		);
		beforeNavigate((navigation) => {
			if (
				this.allowLeave ||
				this.phase !== 'form' ||
				!this.form ||
				this.snapshot() === this.baseline
			)
				return;
			navigation.cancel();
			if (!navigation.willUnload) this.leaving = navigation.to?.url.href ?? '/apps';
		});
		onDestroy(() => (this.destroyed = true));
	}
}
