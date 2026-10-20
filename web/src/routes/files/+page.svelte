<script lang="ts">
	import { untrack, tick, onMount } from 'svelte';
	import FavoritesDialog from './FavoritesDialog.svelte';
 import CreateFileDialog from './CreateFileDialog.svelte';
 import SearchDialog from './SearchDialog.svelte';
 import VirtualEntries from './VirtualEntries.svelte';
 import FolderTree from './FolderTree.svelte';
 import {beginFileDrag,endFileDrag,hasFileDrag,takeFileDrag,type FileDrag} from '#lib/file-drag.ts';
 import DownloadDialog from './DownloadDialog.svelte';
 import UsageDialog from './UsageDialog.svelte';
 import DuplicatesDialog from './DuplicatesDialog.svelte';
 import CompareDialog from './CompareDialog.svelte';
 import RenameBatchDialog from './RenameBatchDialog.svelte';
 import ArchiveDialog from './ArchiveDialog.svelte';
 import UndoPanel from './UndoPanel.svelte';
 import FileProperties from './FileProperties.svelte';
 import FolderPane from './FolderPane.svelte';
 import UploadReview from './UploadReview.svelte';
 import FilePreview from './FilePreview.svelte';
	import FileThumbnail from './FileThumbnail.svelte';
	import { filePreferences, folderPath, rangeSelection, previewKind, selectionType, type FolderView, type Favorite, type FileView } from '#lib/file-actions.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { fileList, type FileSort } from '#lib/file-list.ts';
	import { preferredFileRoot } from '#lib/file-root.ts';
	import { api, ApiError, type CopyJob, type FileEntry, type FileRoot, type UploadStatus, type UsbDevice } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes, formatDiskBytes, formatDateTime } from '#lib/format.ts';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { enqueue, recover, uploads } from '#lib/uploads.svelte.ts';
	import CopyDialog from './CopyDialog.svelte';
	import WindowsPath from './WindowsPath.svelte';
	let windowsPath = $state(false);
 let favoriteManager=$state(false),creatingFile=$state(false),selectionFilter=$state('images');
 let folderViews=$state<FolderView[]>([]);
 let defaults=$state<{view:FileView;sort:FileSort;descending:boolean}>({view:'list',sort:'name',descending:false});
 let shortcuts=$state(false),otherPreview=$state<{root:string;path:string;size:number}|null>(null);

	import JobsPanel from './JobsPanel.svelte';
	import TextEditor from './TextEditor.svelte';
	import { isEditableText } from '#lib/text-files.ts';
	import UploadPanel from './UploadPanel.svelte';
	import TrashView from './TrashView.svelte';

	const canAdminister = $derived(session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin');

	let roots = $state<FileRoot[] | null>(null);
	let usbDevices = $state<UsbDevice[]>([]);
	let usbBusy = $state('');
    let usbLoadError = $state(false);
	let copying = $state<{ move: boolean; restore?: boolean; from?: FileRoot; paths?: string[]; targetRoot?: string; targetDir?: string; pasted?: boolean } | null>(null);
	let editing = $state<string | null>(null);
	let jobsTick = $state(0);
	let trash = $state(false);
 let downloadPaths=$state<string[]|null>(null), usage=$state(false),duplicates=$state(false);
 let deepSearch=$state(false),comparison=$state(false),batchRename=$state(false),archive=$state<string|null>(null);
 let properties=$state<{root:FileRoot;path:string}|null>(null);
 let split=$state(false), right=$state<{root:string;path:string;selected:string[]}>({root:'',path:'',selected:[]});
 let uploadReview=$state<{root:FileRoot;dir:string;files:{file:File;path:string}[]}|null>(null);

 let dropReview=$state<{source:FileDrag;target:string;path:string}|null>(null);
 function reviewDrop(event:DragEvent,target:string,path:string){
  if(!hasFileDrag(event))return;
  event.preventDefault();event.stopPropagation();dragging=false;
  const source=takeFileDrag(event), destination=safeRoots.find(r=>r.id===target), origin=safeRoots.find(r=>r.id===source?.root);
  if(!source||!origin||origin.kind==='stick'||!destination?.writable)return;
  if(source.root===target&&source.paths.some(p=>path===p||path.startsWith(p+'/')))return;
  dropReview={source,target,path};
 }
 function acceptDrop(move:boolean){
  const review=dropReview;if(!review)return;
  const from=safeRoots.find(r=>r.id===review.source.root),target=safeRoots.find(r=>r.id===review.target);
  if(!from||!target?.writable||(move&&!from.writable))return;
  copying={from,paths:review.source.paths,move,targetRoot:target.id,targetDir:review.path};dropReview=null;
 }
 function dragEntry(event:DragEvent,entry:FileEntry){
  if(root?.kind==='stick'||trash){event.preventDefault();return;}
  const names=selectedNames.has(entry.name)?selected:[entry.name];selected=names;
  beginFileDrag(event,{root:rootId,paths:names.map(name=>join(dir,name))});
 }

 function transferPane(direction:'right'|'left',move:boolean){
  const source=direction==='right'?safeRoots.find(r=>r.id===rootId):rightRoot;
  const target=direction==='right'?rightRoot:safeRoots.find(r=>r.id===rootId);
  const names=direction==='right'?selected:right.selected;
  const sourceDir=direction==='right'?dir:right.path,targetDir=direction==='right'?right.path:dir;
  if(!source||!target?.writable||!names.length||(move&&!source.writable))return;
  copying={from:source,paths:names.map(n=>join(sourceDir,n)),move,targetRoot:target.id,targetDir};
 }
 function canRetry(job:CopyJob){return !!safeRoots.find(r=>r.id===job.to)?.writable && (!job.move||!!safeRoots.find(r=>r.id===job.from)?.writable);}


	let rootId = $state('');
	let dir = $state(''); // folder inside the root, "" for its top
	let entries = $state<FileEntry[] | null>(null);
	let selected = $state<string[]>([]);
	let error = $state('');
	let dragging = $state(false);
	let dialog = $state<{ kind: 'mkdir' } | { kind: 'rename'; name: string } | { kind: 'delete' } | null>(null);
	let typed = $state('');
	let confirmed = $state(false);
	let permanent = $state(false);
	let dialogError = $state('');
	let fileInput = $state<HTMLInputElement>();
	let folderInput = $state<HTMLInputElement>();
    let pending = $state<UploadStatus[]>([]);
    let recoveryInput = $state<HTMLInputElement>();
    let recovery = $state<{ status: UploadStatus; root: FileRoot; dir: string } | null>(null);
    let recovering = $state(false);
    const unfinished = $derived(pending.filter(part => !uploads.items.some(item => item.id === part.id)));
    async function recoverPicked(input: HTMLInputElement) {
        const file = input.files?.[0], selected = recovery;
        input.value = '';
        if (!file || !selected) return;
        recovering = true;
        try { await recover(file, selected.root, selected.dir, selected.status); await load(); }
        catch (err) { error = fail(err); }
        finally { recovering = false; recovery = null; }
    }
    async function removePending(part: UploadStatus) {
        try { await api.delete(`/files/uploads/${part.id}`); await load(); }
        catch (err) { error = fail(err); }
    }


    type Location = { root: string; path: string; search: string; selected: string[]; scroll: number; listScroll:number };
    let virtualList=$state<{getScroll:()=>number;setScroll:(top:number)=>void;reveal:(index:number)=>void;getColumns:()=>number}>();
    let back = $state<Location[]>([]), forward = $state<Location[]>([]);
    let anchor = $state('');
    let menu = $state<FileEntry | null>(null), preview = $state<FileEntry | null>(null);
    let favorites = $state<Favorite[]>([]);
    let preferencesReady = $state(false);
    let rememberedRoot = $state('');
    let preferenceError = $state('');
    let savingPreferences = $state(false);
    let view = $state<FileView>('list');
    let showHidden=$state(false);
    const views = ['list', 'small', 'large'] as const;
    let saveQueue = Promise.resolve();
    onMount(() => { void loadPreferences(); });
    async function loadPreferences() {
        try {
            const result = await api.get<{ files: unknown }>('/preferences');
            const prefs = filePreferences(result.files);
            split=prefs.split;showHidden=prefs.showHidden;rememberedRoot = prefs.root; view = prefs.view; sort = prefs.sort; descending = prefs.descending; favorites = prefs.favorites;
            folderViews=prefs.folders;defaults={view:prefs.view,sort:prefs.sort,descending:prefs.descending};applyFolderView();
            preferencesReady = true; preferenceError = '';
        } catch { preferenceError = t('files.preferencesLoadFailed'); }
    }
    function savePreferences() {
        if (!preferencesReady) return;
        if(folderViews.some(f=>f.root===rootId&&f.path===dir))folderViews=folderViews.map(f=>f.root===rootId&&f.path===dir?{...f,view,sort,descending}:f);
        else defaults={view,sort,descending};
        const value = { split, showHidden, root: rememberedRoot, ...defaults, folders:$state.snapshot(folderViews), favorites: $state.snapshot(favorites) };
        savingPreferences = true;
        saveQueue = saveQueue.then(async () => {
            try { await api.put('/preferences/files', value); preferenceError = ''; }
            catch { preferenceError = t('files.preferencesSaveFailed'); }
        });
        const pending = saveQueue;
        void pending.then(() => { if (pending === saveQueue) savingPreferences = false; });
    }
    const folderViewSaved=$derived(folderViews.some(f=>f.root===rootId&&f.path===dir));
    function applyFolderView(){const settings=folderViews.find(f=>f.root===rootId&&f.path===dir)??defaults;view=settings.view;sort=settings.sort;descending=settings.descending;}
    function toggleFolderView(){if(folderViewSaved){folderViews=folderViews.filter(f=>f.root!==rootId||f.path!==dir);applyFolderView();}else if(folderViews.length<200)folderViews=[...folderViews,{root:rootId,path:dir,view,sort,descending}];savePreferences();}
    function changeView(next: FileView) { view = next; savePreferences(); }
    const visibleFavorites=$derived(favorites.filter(f=>roots?.some(r=>r.id===f.root)));
    const isFavorite=$derived(favorites.some(f=>f.root===rootId && f.path===dir));
    function saveFavorites(next:Favorite[]) { favorites=next;savePreferences(); }
    function toggleFavorite() { if(isFavorite)saveFavorites(favorites.filter(f=>f.root!==rootId || f.path!==dir));else if(favorites.length<50)saveFavorites([...favorites,{root:rootId,path:dir}]); }
    function captureLocation():Location {return {root:rootId,path:dir,search,selected:[...selected],scroll:window.scrollY,listScroll:virtualList?.getScroll()??0};}
    function historyGo(direction:'back'|'forward') {
      const list=direction==='back'?back:forward;const target=list.at(-1);if(!target)return;
      if(direction==='back'){back=back.slice(0,-1);forward=[...forward,captureLocation()];}else{forward=forward.slice(0,-1);back=[...back,captureLocation()];}
      navigate(target.root,target.path,target);
    }
    function selectRange(name:string,add=false) {const range=rangeSelection(visibleEntries.map(e=>e.name),anchor,name);selected=add?[...new Set([...selected,...range])]:range;}
    function folderClick(entry:FileEntry,event:MouseEvent) {if(event.shiftKey)selectRange(entry.name,event.ctrlKey||event.metaKey);else if(event.ctrlKey||event.metaKey)toggle(entry.name,!selectedNames.has(entry.name));else open(rootId,join(dir,entry.name));}
    function entryMenu(entry:FileEntry) {if(!selectedNames.has(entry.name))selected=[entry.name];backgroundMenu=false;menu=entry;}

	const root = $derived(roots?.find((candidate) => candidate.id === rootId));
	const crumbs = $derived(dir ? dir.split('/') : []);
	const date = $derived(new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium', timeStyle: 'short' }));
	let search = $state('');
    let sort = $state<FileSort>('name');
    let descending = $state(false);
    const visibleEntries = $derived(fileList((entries ?? []).filter(entry=>showHidden || !entry.name.startsWith('.')), search, sort, descending, i18n.locale));
 const gallery=$derived(visibleEntries.filter(e=>!e.dir&&previewKind(e.name,e.size)==='image'));
 const galleryIndex=$derived(preview?gallery.findIndex(e=>e.name===preview?.name):-1);
    const selectedNames=$derived(new Set(selected));
    const allSelected = $derived(visibleEntries.length > 0 && visibleEntries.every(entry => selectedNames.has(entry.name)));

    const selectedEntries=$derived((entries??[]).filter(entry=>selectedNames.has(entry.name)));
    const selectedBytes=$derived(selectedEntries.filter(entry=>!entry.dir).reduce((sum,entry)=>sum+entry.size,0));
    const selectedFolders=$derived(selectedEntries.filter(entry=>entry.dir).length);
    const selectionKey=$derived(JSON.stringify([rootId,dir,selected]));
    let measured=$state<{key:string;bytes:number}|null>(null), measuring=$state(false);
    async function measureSelection(){
        const key=selectionKey;measuring=true;
        try { const result=await api.post<{bytes:number}>('/files/inspect',{root:rootId,paths:selected.map(name=>join(dir,name))});if(key===selectionKey)measured={key,bytes:result.bytes}; }
        catch(err){if(key===selectionKey)error=fail(err);}finally{measuring=false;}
    }
    let backupCatalogReady=$state(false);
    let unlockedRoot=$state(''), unlockDialog=$state(false);
    const backupDrives=$derived(usbDevices.filter(device=>usbBackups.some(backup=>backup.job.destination.deviceId===device.id)).map(device=>device.root));
    const backupLocked=$derived(root?.kind==='usb' && (!backupCatalogReady || usbLoadError || backupDrives.includes(rootId)) && unlockedRoot!==rootId);
    const writable=$derived(!!root?.writable && !backupLocked);
    const safeRoots=$derived((roots??[]).map(place=>place.kind==='usb' && (!backupCatalogReady || usbLoadError || backupDrives.includes(place.id)) && unlockedRoot!==place.id?{...place,writable:false}:place));
 const rightRoot=$derived(safeRoots.find(r=>r.id===right.root));

	const join = (...parts: string[]) => parts.filter(Boolean).join('/');
	const fail = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');

	async function loadRoots() {
		try {
			roots = await api.get<FileRoot[]>('/files/roots');
			if (!roots.some((candidate) => candidate.id === rootId)) {
				open(preferredFileRoot(roots, rememberedRoot), '');
			}
		} catch (err) {
			error = fail(err);
		}
	}

    type BackupState = { running: boolean; percent: number; bytesDone: number; bytesTotal: number; lastOK: string | null; lastError: string; warning: string };
    type USBBackup = { state?: BackupState; job: { id: string; destination: { type: string; deviceId?: string; folder?: string } } };
    let usbBackups = $state<USBBackup[]>([]);
    let backupLoadError = $state(false);
    let openingBackup = $state('');
    let backupProgressError = $state(false);
    const currentUSBBackup = $derived(usbBackups.find(backup => usbDevices.some(device => device.id===backup.job.destination.deviceId && device.root===rootId) && (dir===backupFolder(backup) || dir.startsWith(backupFolder(backup)+'/'))));
    async function loadBackupProgress() {
        try {
            const states = await api.get<Record<string,BackupState>>('/backups/progress');
            usbBackups=usbBackups.map(backup=>({...backup,state:states[backup.job.id]??backup.state}));backupProgressError=false;
        } catch { backupProgressError=true; }
    }
    $effect(() => { if(!canAdminister)return;void loadUSBBackups();const timer=setInterval(()=>{if(!document.hidden)void loadBackupProgress();},5000);return()=>clearInterval(timer); });
    async function loadUSBBackups() {
        try {
            const overview = await api.get<{jobs: USBBackup[]}>('/backups');
            usbBackups = overview.jobs.filter(view => view.job.destination.type === 'usb-copy');
            backupLoadError = false;backupCatalogReady=true;
        } catch { backupLoadError = true; }
    }
    function backupFolder(backup: USBBackup) { return backup.job.destination.folder || 'backups/' + backup.job.id; }
    async function openUSBBackup(backup: USBBackup, restore=false) {
        const id = backup.job.destination.deviceId;
        const drive = usbDevices.find(device => device.id === id);
        if (!drive || !drive.supported) return;
        openingBackup = backup.job.id; error = '';
        try {
            let devices = usbDevices;
            if (!drive.mounted) devices = await api.post<UsbDevice[]>(`/usb/${encodeURIComponent(drive.id)}/mount`);
            usbDevices = devices;
            const mounted = devices.find(device => device.id === id);
            if (!mounted?.root) throw new Error('unavailable');
            await loadRoots();
            const path = backupFolder(backup);
            // Do not create a backup directory just to browse it.
            await api.get<FileEntry[]>('/files/list?' + new URLSearchParams({root:mounted.root,path}));
            if(restore) {
                const source=roots?.find(root=>root.id===mounted.root);
                if(source)copying={move:false,restore:true,from:source,paths:[path]};
            } else open(mounted.root,path);
        } catch (err) { error = err instanceof ApiError && err.code === 'file_not_found' ? t('files.usbBackupMissing') : fail(err); }
        finally { openingBackup = ''; }
    }

	async function loadUsb() {
		if (!canAdminister) return;
		try {
			const list = await api.get<UsbDevice[]>('/usb');
            usbLoadError = false;
			const before = usbDevices.filter((d) => d.mounted).map((d) => d.id).join();
			usbDevices = list;
			// A drive that was mounted or taken away changes the places on the left.
			if (before !== list.filter((d) => d.mounted).map((d) => d.id).join()) loadRoots();
		} catch {
			usbLoadError = true;
			// The next look may work.
		}
	}

	async function usbAction(device: UsbDevice, action: 'mount' | 'eject') {
		usbBusy = device.id;
		error = '';
		try {
			usbDevices = await api.post<UsbDevice[]>(`/usb/${encodeURIComponent(device.id)}/${action}`);
			if (action === 'eject' && rootId === device.root) open('', '');
			await loadRoots();
			if (action === 'mount') {
				const mounted = usbDevices.find((d) => d.id === device.id);
				if (mounted?.root) open(mounted.root, '');
			}
		} catch (err) {
			error = fail(err);
		}
		usbBusy = '';
	}

	$effect(() => {
		if (!canAdminister) return;
		loadUsb();
		const timer = setInterval(() => !document.hidden && loadUsb(), 5000);
		return () => clearInterval(timer);
	});

	const unmounted = $derived(usbDevices.filter((device) => !device.mounted));
	const editable = $derived.by(() => {
		if (backupLocked || !(root?.editable || writable) || selected.length !== 1) return false;
		const entry = entries?.find((candidate) => candidate.name === selected[0]);
		return !!entry && !entry.dir && isEditableText(entry.name, entry.size);
	});

	async function load() {
		if (!rootId) { entries = null; pending = []; return; }
        const requestedRoot = rootId, requestedDir = dir;
		try {
			const query = `root=${encodeURIComponent(requestedRoot)}&path=${encodeURIComponent(requestedDir)}`;
            const [listing, parts] = await Promise.all([
                api.get<FileEntry[]>(`/files/list?${query}`),
                writable ? api.get<UploadStatus[]>(`/files/uploads?${query}`) : Promise.resolve([])
            ]);
            if (rootId !== requestedRoot || dir !== requestedDir) return;
            entries = listing; pending = parts;
			const names=new Set(entries.map(entry=>entry.name));selected=selected.filter(name=>names.has(name));
			error = '';
		} catch (err) {
            if(rootId!==requestedRoot || dir!==requestedDir)return;
			entries = [];
			error = fail(err);
		}
	}

    let refreshError=$state(false);
    onMount(()=>{
      let active=true, inFlight=false;let controller:AbortController|undefined;
      const blocked=()=>document.hidden||document.querySelector('dialog[open]')!==null||dragging||trash||!rootId||root?.kind==='stick'||entries===null||['INPUT','TEXTAREA','SELECT'].includes(document.activeElement?.tagName??'');
      async function refresh(){
        if(inFlight||blocked())return;
        const id=rootId,path=dir,before=entries;inFlight=true;controller=new AbortController();
        try{
          const listing=await api.get<FileEntry[]>('/files/list?'+new URLSearchParams({root:id,path}),controller.signal);
          if(!active||id!==rootId||path!==dir||before!==entries||blocked())return;
          refreshError=false;
          if(JSON.stringify(listing)===JSON.stringify(entries))return;
          const anchor=Array.from(document.querySelectorAll<HTMLElement>('[data-file-name]')).find(e=>e.getBoundingClientRect().top>=0);
          const name=anchor?.dataset.fileName,offset=anchor?.getBoundingClientRect().top,scroll=window.scrollY;
          entries=listing;const names=new Set(listing.map(e=>e.name));selected=selected.filter(n=>names.has(n));await tick();
          if(!active||id!==rootId||path!==dir)return;
          const next=Array.from(document.querySelectorAll<HTMLElement>('[data-file-name]')).find(e=>e.dataset.fileName===name);
          window.scrollTo({top:next&&offset!==undefined?window.scrollY+next.getBoundingClientRect().top-offset:scroll,behavior:'instant'});
        }catch{if(active&&id===rootId&&path===dir)refreshError=true;}finally{inFlight=false;}
      }
      const timer=setInterval(refresh,10000);document.addEventListener('visibilitychange',refresh);
      return()=>{active=false;controller?.abort();clearInterval(timer);document.removeEventListener('visibilitychange',refresh);};
    });

	function open(id: string, path: string) {
        if(rootId && (rootId!==id || dir!==path))back=[...back,captureLocation()].slice(-50);
        forward=[];navigate(id,path);
    }
    async function navigate(id: string, path: string, restore?:Location) {
        const chosen = roots?.find(root => root.id === id);
        if ((chosen?.kind === 'share' || chosen?.kind === 'volume') && rememberedRoot !== id) {
            rememberedRoot = id;
            if (rootId) savePreferences();
        }
        if(rootId!==id)unlockedRoot='';
		rootId = id;
		dir = path;
        applyFolderView();
		search = restore?.search ?? '';
		anchor='';menu=null;preview=null;backgroundMenu=false;pathInput=path;pathMessage='';
		trash = false;
		selected = [];
		entries = null; pending = [];
        await load();
        if(rootId!==id || dir!==path)return;
        if(restore){const names=new Set((entries??[] as FileEntry[]).map(e=>e.name));selected=restore.selected.filter(name=>names.has(name));}
        await tick();virtualList?.setScroll(restore?.listScroll??0);window.scrollTo({top:restore?.scroll ?? 0,behavior:'instant'});
	}

	// At the start, and whenever an upload finishes: it may belong to the
	// folder on screen, and it changes the free space.
	$effect(() => {
		uploads.finished;
        if (!preferencesReady) return;
		untrack(() => {
			loadRoots();
			load();
		});
	});

	function toggle(name: string, checked: boolean) {
		anchor=name;
		selected = checked ? [...selected, name] : selected.filter((other) => other !== name);
	}

	// A click on the row opens a folder or toggles the selection of a file.
	// Clicks on the checkbox or the folder button inside the row are left to
	// those controls.
	function rowClicked(entry: FileEntry, event: MouseEvent) {
		if (event.target instanceof Element && event.target.closest('input, button')) return;
		if(event.shiftKey)selectRange(entry.name,event.ctrlKey||event.metaKey);
        else if(event.ctrlKey||event.metaKey)toggle(entry.name,!selectedNames.has(entry.name));
        else if (entry.dir) open(rootId, join(dir, entry.name));
		else toggle(entry.name, !selectedNames.has(entry.name));
	}

    let backgroundMenu = $state(false);
    let pathInput = $state('');
    let pathMessage = $state('');
    let clipboard = $state<{ from: FileRoot; paths: string[]; move: boolean } | null>(null);
    function rememberSelection(move: boolean) {
        if (!root || !selected.length || root.kind === 'stick' || (move && !writable)) return;
        clipboard = { from: root, paths: selected.map(name => join(dir, name)), move };
        menu = null;
    }
    function paste() {
        if (!clipboard || !writable || (clipboard.move && !safeRoots.find(place=>place.id===clipboard?.from.id)?.writable)) return;
        copying = { ...clipboard, targetRoot: rootId, targetDir: dir, pasted: true };
        backgroundMenu = false;
    }
    function blankMenu(event: MouseEvent) {
        if (trash || !root || (event.target instanceof Element && event.target.closest('.row:not(.header),button,input,select,a'))) return;
        event.preventDefault(); menu = null; backgroundMenu = true;
    }
    async function goToPath(event: SubmitEvent) {
        event.preventDefault();
        const path = folderPath(pathInput);
        if (path === null) { pathMessage = t('files.invalidPath'); return; }
        const id = rootId;
        try {
            await api.get<FileEntry[]>('/files/list?' + new URLSearchParams({ root: id, path }));
            if (rootId === id) { pathMessage = ''; open(id, path); }
        } catch (err) { pathMessage = fail(err); }
    }
    async function copyPath() {
        try { await navigator.clipboard.writeText('/' + dir); pathMessage = t('files.pathCopied'); }
        catch { pathMessage = t('files.pathCopyFailed'); }
    }
    async function focusEntry(index:number){virtualList?.reveal(index);await tick();const name=visibleEntries[index]?.name;if(name)document.querySelector<HTMLElement>(`.browser [data-file-name="${CSS.escape(name)}"]`)?.focus({preventScroll:true});}
    function keyboard(event: KeyboardEvent) {
        if (trash || !root || document.querySelector('dialog') || event.altKey || event.defaultPrevented) return;
        if (event.target instanceof Element && event.target.closest('input:not([type=checkbox]):not([type=radio]),textarea,select,[contenteditable="true"]')) return;
        const key = event.key.toLowerCase(), command = event.ctrlKey || event.metaKey;
        if(key==='f1'){event.preventDefault();shortcuts=true;return;}
        if(event.target instanceof Element&&event.target.closest('.pane'))return;
        const listTarget=!(event.target instanceof Element)||event.target===document.body||!!event.target.closest('.file-entries');
        if(!command&&listTarget&&['arrowup','arrowdown','arrowleft','arrowright','home','end'].includes(key)&&visibleEntries.length){
          event.preventDefault();const focused=event.target instanceof Element?event.target.closest<HTMLElement>('[data-file-name]')?.dataset.fileName:undefined;
          const old=visibleEntries.findIndex(e=>e.name===(focused??selected.at(-1)));const columns=virtualList?.getColumns()??1;
          const index=key==='home'?0:key==='end'?visibleEntries.length-1:Math.max(0,Math.min(visibleEntries.length-1,old<0?0:old+(key==='arrowup'?-columns:key==='arrowdown'?columns:key==='arrowleft'?-1:1)));
          const name=visibleEntries[index].name;if(event.shiftKey){if(!anchor)anchor=visibleEntries[Math.max(0,old)].name;selectRange(name);}else{selected=[name];anchor=name;}void focusEntry(index);return;
        }
        if(!command&&listTarget&&(key==='enter'||key===' ')&&selected.length===1){
          if(event.target instanceof Element&&event.target.closest('button,input,a'))return;
          const entry=visibleEntries.find(e=>e.name===selected[0]);if(!entry)return;event.preventDefault();
          if(entry.dir&&key==='enter')open(rootId,join(dir,entry.name));else if(!entry.dir&&previewKind(entry.name,entry.size))preview=entry;return;
        }
        if (command && key === 'a') { event.preventDefault(); selected = visibleEntries.map(entry => entry.name); }
        else if (command && (key === 'c' || key === 'x') && selected.length && root.kind !== 'stick' && (key !== 'x' || writable)) { event.preventDefault(); rememberSelection(key === 'x'); }
        else if (command && key === 'v' && clipboard && writable) { event.preventDefault(); paste(); }
        else if (!command && key === 'f2' && selected.length === 1 && writable) { event.preventDefault(); show('rename'); }
        else if (!command && key === 'delete' && selected.length && writable) { event.preventDefault(); show('delete'); }
    }

	function download() {
		if (!entries || selected.length === 0) return;
		const single = selected.length === 1 ? entries.find((entry) => entry.name === selected[0]) : undefined;
		if(!single || single.dir){downloadPaths=selected.map(name=>join(dir,name));return;}
        const query = new URLSearchParams({ root: rootId });
		for (const name of selected) query.append('path', join(dir, name));
		// The browser saves the answer; nothing passes through this page's memory.
		location.href = `/api/v1/files/${single && !single.dir ? 'download' : 'zip'}?${query}`;
	}

	function show(kind: 'mkdir' | 'rename' | 'delete') {
        if(!writable)return;
		dialogError = '';
		confirmed = false;
		permanent = !root?.recycle;
		if (kind === 'rename') {
			typed = selected[0];
			dialog = { kind, name: selected[0] };
		} else {
			typed = '';
			dialog = { kind };
		}
	}

	async function submitDialog(event: SubmitEvent) {
		event.preventDefault();
		if (!dialog || !writable) return;
		dialogError = '';
		try {
			if (dialog.kind === 'mkdir') {
				await api.post('/files/mkdir', { root: rootId, path: join(dir, typed.trim()) });
			} else if (dialog.kind === 'rename') {
				await api.post('/files/rename', { root: rootId, from: join(dir, dialog.name), to: join(dir, typed.trim()) });
			} else {
				if (permanent && !confirmed) return;
				await api.post('/files/delete', { root: rootId, paths: selected.map((name) => join(dir, name)), permanent });
			}
			dialog = null;
   jobsTick++;
			selected = [];
			await load();
			loadRoots();
		} catch (err) {
			dialogError = fail(err);
		}
	}

	/** Creates the folders of an uploaded tree, parents first, then queues the files. */
	function upload(files: { file: File; path: string }[]) {
  if(root && writable && files.length)uploadReview={root,dir,files};
 }
 async function startUpload(files:{file:File;path:string;conflict:''|'rename'|'replace'}[]){
  const review=uploadReview;if(!review || !safeRoots.find(r=>r.id===review.root.id)?.writable)throw new ApiError(403,'read_only_share','');
  const target=review.dir;
		const folders = new Set<string>();
		for (const { path } of files) {
			const parts = path.split('/').slice(0, -1);
			for (let depth = 1; depth <= parts.length; depth++) folders.add(parts.slice(0, depth).join('/'));
		}
		for (const folder of [...folders].sort()) {
			try {
				await api.post('/files/mkdir', { root: review.root.id, path: join(target, folder) });
			} catch (err) {
				// The folder being there already is what we want.
				if (!(err instanceof ApiError && err.code === 'file_exists')) throw err;
			}
		}
		enqueue(
			files.map(({ file, path, conflict }) => ({ file, conflict, dir: join(target, path.split('/').slice(0, -1).join('/')) })),
			review.root
		);
  uploadReview=null;
		load();
	}

	function picked(input: HTMLInputElement) {
		const files = [...(input.files ?? [])].map((file) => ({ file, path: file.webkitRelativePath || file.name }));
		input.value = '';
		upload(files);
	}

	/** Reads a dropped folder with everything below it. */
	async function readEntry(entry: FileSystemEntry, prefix: string, out: { file: File; path: string }[]) {
		if (entry.isFile) {
			const file = await new Promise<File>((resolve, reject) => (entry as FileSystemFileEntry).file(resolve, reject));
			out.push({ file, path: prefix + entry.name });
		} else if (entry.isDirectory) {
			const reader = (entry as FileSystemDirectoryEntry).createReader();
			for (;;) {
				const batch = await new Promise<FileSystemEntry[]>((resolve, reject) => reader.readEntries(resolve, reject));
				if (batch.length === 0) break;
				for (const child of batch) await readEntry(child, prefix + entry.name + '/', out);
			}
		}
	}

	async function dropped(event: DragEvent) {
		if(hasFileDrag(event)){reviewDrop(event,rootId,dir);return;}
		event.preventDefault();
		dragging = false;
		if (!writable || !event.dataTransfer) return;
		// The entries have to be taken before the first await; the list is gone afterwards.
		const items = [...event.dataTransfer.items].map((item) => item.webkitGetAsEntry?.()).filter((entry): entry is FileSystemEntry => !!entry);
		const plain = [...event.dataTransfer.files];
		const files: { file: File; path: string }[] = [];
		if (items.length > 0) for (const entry of items) await readEntry(entry, '', files);
		else files.push(...plain.map((file) => ({ file, path: file.name })));
		upload(files);
	}
</script>

<svelte:window onkeydown={keyboard} />
{#if preferenceError}<p class="notice-error" role="alert">{preferenceError} <button class="btn" onclick={()=>preferencesReady?savePreferences():loadPreferences()}>{t('files.retryPreferences')}</button></p>{/if}
{#if savingPreferences}<p class="muted" role="status">{t('files.savingPreferences')}</p>{/if}
<PageHead title={t('files.title')} />

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

{#if roots && roots.length === 0}
	<EmptyState title={t('files.noRoots')} text={canAdminister ? t('files.noRootsAdmin') : undefined} />
{:else if roots}
	<div class="layout">
		<nav class="tile places" aria-label={t('files.shares')}>
			{#each ['share', 'volume', 'usb', 'stick'] as kind (kind)}
				{@const list = roots.filter((candidate) => candidate.kind === kind)}
				{#if list.length > 0}
					{#snippet placesList()}
						<span class="group">{t(kind === 'share' ? 'files.shares' : kind === 'volume' ? 'files.volumes' : kind === 'usb' ? 'files.usb' : 'files.stick')}{kind === 'share' ? '' : ` · ${t('files.adminOnly')}`}</span>
						{#each list as place (place.id)}
                            <FolderTree readOnly={!place.writable&&kind!=='stick'} root={place.id} name={place.name} currentRoot={trash?'':rootId} currentPath={dir} {showHidden} onopen={open}/>

							{#if kind === 'usb'}
								{@const device = usbDevices.find((d) => d.root === place.id)}
								{#if device}<button type="button" class="eject" disabled={usbBusy === device.id} onclick={() => usbAction(device, 'eject')}>{t('files.eject')}</button>{/if}
							{/if}
						{/each}
					{/snippet}
					{#if kind === 'volume'}
						<details class="advanced" open={root?.kind === 'volume'}>
							<summary>{t('files.advancedAccess')}</summary>
							{@render placesList()}
						</details>
					{:else}
						{@render placesList()}
					{/if}
				{/if}
			{/each}
			{#if canAdminister && unmounted.length > 0}
				<span class="group">{t('files.usbPlugged')}</span>
				{#each unmounted as device (device.id)}
					<div class="usb">
						<Icon name="disk" size={16} />
						<span class="place-name">{device.name} <span class="muted mono">{device.fsType}</span></span>
						{#if device.supported}
							<button type="button" class="mount" disabled={usbBusy === device.id} onclick={() => usbAction(device, 'mount')}>{t('files.mount')}</button>
						{:else}
							<span class="chip">{t('files.usbUnsupported')}</span>
						{/if}
					</div>
				{/each}
			{/if}
            {#if canAdminister}
                <span class="group">{t('files.usbBackups')}</span>
                {#each usbBackups as backup (backup.job.id)}
                    {@const drive = usbDevices.find(device => device.id === backup.job.destination.deviceId)}
                    <div class="backup-place">
                        <button type="button" class:current={!trash && drive?.root===rootId && dir===backupFolder(backup)} disabled={!drive?.supported || openingBackup!==''} onclick={()=>openUSBBackup(backup)}>
                            <Icon name="disk" size={16} /><span class="place-name">{backup.job.id}</span>
                        </button>
                        <span class="mono backup-path">/{backupFolder(backup)}</span>
                        <span class="muted">{drive ? drive.supported ? drive.mounted ? drive.name : t('files.usbBackupMount',{name:drive.name}) : t('files.usbUnsupported') : t(usbLoadError?'files.usbDeviceLoadFailed':'files.usbBackupOffline')}</span>
                        <span>{t('files.backupLastOK',{time:backup.state?.lastOK?formatDateTime(new Date(backup.state.lastOK)):t('files.backupNever')})}</span>
                        {#if drive?.mounted && Number.isFinite(drive.freeBytes)}<span>{t('files.backupFree',{size:formatDiskBytes(drive.freeBytes)})}</span>{/if}
                        {#if backupProgressError}<span>{t('files.backupProgressStale')}</span>
                        {:else if backup.state?.running}
                            <span role="status">{t('files.backupRunning')}</span>
                            <progress max="100" value={backup.state.bytesTotal>0?Math.min(100,Math.max(0,backup.state.percent)):undefined} aria-label={t('files.backupRunning')}></progress>
                            <span class="mono">{backup.state.bytesTotal>0?t('files.backupBytes',{done:formatBytes(backup.state.bytesDone),total:formatBytes(backup.state.bytesTotal)}):t('files.backupCounting')}</span>
                        {:else if backup.state?.lastError}<span class="backup-error">{t('files.backupFailed')}: {backup.state.lastError}</span>{/if}
                        {#if backup.state?.warning}<span>{backup.state.warning}</span>{/if}
                        <button type="button" disabled={!drive?.supported || openingBackup!=='' || backup.state?.running} onclick={()=>openUSBBackup(backup,true)}>{t('files.restoreBackup')}</button>
                    </div>
                {:else}
                    <span class="muted backup-empty">{t('files.usbBackupNone')}</span>
                {/each}
                {#if backupLoadError}<span class="backup-empty" role="alert">{t('files.usbBackupLoadFailed')}</span>{/if}
                <button type="button" onclick={()=>{loadUSBBackups();loadUsb();}}>{t('files.usbBackupRefresh')}</button>
                <a class="backup-settings" href="/settings/backups/data">{t('files.usbBackupSettings')}</a>
            {/if}
            {#if visibleFavorites.length}
                <span class="group">{t('files.favorites')}</span><button type="button" onclick={()=>favoriteManager=true}>{t('files.manageFavorites')}</button>
                {#each visibleFavorites as favorite (`${favorite.root}/${favorite.path}`)}
                    <div class="favorite-row"><button type="button" title={`${roots?.find(r=>r.id===favorite.root)?.name}/${favorite.path}`} onclick={()=>open(favorite.root,favorite.path)}><span class="place-name">{favorite.label||favorite.path.split('/').at(-1)||roots?.find(r=>r.id===favorite.root)?.name}</span></button><button type="button" aria-label={t('files.removeFavorite',{name:favorite.path||favorite.root})} onclick={()=>saveFavorites(favorites.filter(f=>f!==favorite))}>×</button></div>
                {/each}
            {/if}
			{#if root}
                <button type="button" class:current={trash} aria-current={trash ? 'page' : undefined} onclick={() => { trash = true; selected = []; }}><Icon name="trash" size={16} /><span>{t('files.trash')}</span></button>
            {/if}
			{#if root && root.kind !== 'stick'}
				<span class="usage muted">{root.name}<br /><span class="mono">{t('files.free', { free: formatDiskBytes(root.freeBytes), total: formatDiskBytes(root.totalBytes) })}</span></span>
			{/if}
		</nav>

        <div class="workspace" class:split>
        <div class="pane-actions">
         {#if !trash && root}<details class="tools"><summary class="btn">{t('files.tools')}</summary><div class="tool-options">
          <button class="btn" onclick={()=>selected=visibleEntries.filter(e=>!selectedNames.has(e.name)).map(e=>e.name)}>{t('files.invertSelection')}</button>
          <label>{t('files.selectType')}<select class="field" bind:value={selectionFilter}>{#each ['images','video','audio','documents','folders','other'] as type}<option value={type}>{t(`files.selectionType.${type}` as 'files.selectionType.images')}</option>{/each}</select></label><button class="btn" onclick={()=>selected=visibleEntries.filter(e=>selectionFilter==='folders'?e.dir:!e.dir&&selectionType(e.name)===selectionFilter).map(e=>e.name)}>{t('files.selectMatches')}</button>
          <button class="btn" onclick={()=>deepSearch=true}>{t('files.deepSearch')}</button>
          {#if root.kind!=='stick'}<button class="btn" onclick={()=>usage=true}>{t('files.usageTitle')}</button><button class="btn" onclick={()=>duplicates=true}>{t('files.duplicatesTitle')}</button>{/if}
          {#if split}<button class="btn" disabled={!rightRoot} onclick={()=>comparison=true}>{t('files.compareFolders')}</button>{/if}
         </div></details>{/if}
         <button class="btn" aria-pressed={split} onclick={()=>{split=!split;savePreferences();}}>{t('files.twoPanes')}</button>
         {#if split && !trash}

         {#if selected.length&&root?.kind!=='stick'}<button class="btn" disabled={!rightRoot?.writable} onclick={()=>transferPane('right',false)}>{t('files.copyRight')} →</button>
         <button class="btn" disabled={!selected.length||!writable||!rightRoot?.writable} onclick={()=>transferPane('right',true)}>{t('files.moveRight')} →</button>{/if}
         {#if right.selected.length}<button class="btn" disabled={!right.selected.length||!writable} onclick={()=>transferPane('left',false)}>← {t('files.copyLeft')}</button>
         <button class="btn" disabled={!right.selected.length||!rightRoot?.writable||!writable} onclick={()=>transferPane('left',true)}>← {t('files.moveLeft')}</button>{/if}
         {/if}
        </div>
        <section
            oncontextmenu={blankMenu}
			class="tile browser"
			class:dragging
			aria-label={root?.name}
			ondragover={(event) => {
				if (writable) {
					event.preventDefault();
					dragging = true;
				}
			}}
			ondragleave={(event) => {
				if (!event.currentTarget.contains(event.relatedTarget as Node)) dragging = false;
			}}
			ondrop={dropped}
		>
			{#if trash && root}
                {#if root.kind === 'share' && root.recycle}
				<TrashView {root} user={session.data?.user?.name ?? ''} admin={canAdminister} onclose={() => { trash = false; load(); }} onchanged={() => { load(); loadRoots(); }} />
                {:else}
                    <div class="toolbar"><button type="button" class="btn" onclick={() => { trash = false; load(); }}>{t('files.trashBack')}</button></div>
                    <EmptyState title={t('files.trashOf', { name: root.name })} text={t(root.kind === 'share' ? 'files.trashDisabled' : 'files.trashUnsupported')} />
                    {#if canAdminister && root.kind === 'share'}<a class="btn" href="/shares">{t('files.trashConfigure')}</a>{/if}
                {/if}
			{:else}
			<div class="toolbar">
                <button type="button" class="btn" disabled={!back.length} onclick={()=>historyGo('back')}>{t('files.back')}</button>
                <button type="button" class="btn" disabled={!forward.length} onclick={()=>historyGo('forward')}>{t('files.forward')}</button>
                <button type="button" class="btn" disabled={!dir} onclick={()=>open(rootId,dir.split('/').slice(0,-1).join('/'))}>{t('files.up')}</button>
                {#if root && root.kind!=='stick'}<button type="button" class="btn" aria-pressed={isFavorite} disabled={!isFavorite && favorites.length>=50} title={t('files.favoriteAccount')} onclick={toggleFavorite}>{t(isFavorite?'files.unfavorite':'files.favorite')}</button>{/if}
				{#if root?.kind === 'share' || root?.kind === 'volume'}<button type="button" class="btn" onclick={() => windowsPath = true}>{t('files.windowsPath')}</button>{/if}
				<div class="crumbs mono">
					{#if root}<button type="button" onclick={() => open(rootId, '')}>{root.name}</button>{/if}
					{#each crumbs as crumb, index (index)}
						<span class="muted">/</span>
						<button type="button" onclick={() => open(rootId, crumbs.slice(0, index + 1).join('/'))}>{crumb}</button>
					{/each}
				</div>
				{#if writable}
					<button type="button" class="btn" onclick={() => fileInput?.click()}><Icon name="upload" size={16} />{t('files.upload')}</button>
					<button type="button" class="btn" onclick={() => folderInput?.click()}>{t('files.uploadFolder')}</button>
					<button type="button" class="btn" onclick={() => show('mkdir')}>{t('files.newFolder')}</button><button type="button" class="btn" onclick={()=>creatingFile=true}>{t('files.newFile')}</button>
					<input bind:this={fileInput} type="file" multiple hidden onchange={(event) => picked(event.currentTarget)} />
					<input bind:this={folderInput} type="file" webkitdirectory hidden onchange={(event) => picked(event.currentTarget)} />
				{/if}

			</div>

			{#if unfinished.length > 0}
                <section class="unfinished" aria-label={t('uploads.unfinished')}>
                    <strong>{t('uploads.unfinished')}</strong>
                    <p class="muted">{recovering ? t('uploads.verifying') : t('uploads.recoverHint')}</p>
                    <input bind:this={recoveryInput} type="file" hidden onchange={event => recoverPicked(event.currentTarget)} />
                    {#each unfinished as part (part.id)}
                        <div class="unfinished-row">
                            <span>{part.name} · <span class="mono">{formatBytes(part.offset)} / {formatBytes(part.size)}</span></span>
                            <button class="btn" disabled={recovering || part.busy} onclick={() => { if (root) { recovery = { status: part, root, dir }; recoveryInput?.click(); } }}>{t('uploads.resume')}</button>
                            <button class="btn" disabled={recovering || part.busy} onclick={() => removePending(part)}>{t('uploads.removePartial')}</button>
                        </div>
                    {/each}
                </section>
            {/if}
            {#if root?.kind==='usb' && (!backupCatalogReady || usbLoadError || backupDrives.includes(rootId))}<div class="toolbar">
                <span>{t(backupLocked?'files.backupLocked':'files.backupUnlocked')}</span>
                <button class="btn" onclick={()=>{if(backupLocked)unlockDialog=true;else{unlockedRoot='';if(clipboard?.move)clipboard=null;}}}>{t(backupLocked?'files.unlockBackup':'files.lockBackup')}</button>
            </div>{/if}
            {#if !writable}<p class="notice-warn">{t(backupLocked?'files.rightReason.usb':root?.kind==='share'?'files.rightReason.share':root?.kind==='stick'?'files.rightReason.stick':'files.rightReason.readOnly')}</p>{/if}
            {#if refreshError}<p class="notice-warn" role="status">{t('files.autoRefreshFailed')}</p>{/if}
            <div class="toolbar file-filters">

                <label><input type="checkbox" checked={showHidden} onchange={event=>{showHidden=event.currentTarget.checked;selected=[];savePreferences();}} />{t('files.showHidden')}</label>
                <label class="search-field"><span>{t('files.searchFolder')}</span><input class="field" type="search" bind:value={search} oninput={() => { selected = []; anchor = ''; }} placeholder={t('files.searchFolder')} /></label>
                <label><span>{t('files.sortBy')}</span><select class="field" value={sort} disabled={!preferencesReady} onchange={(event) => { sort = event.currentTarget.value as FileSort; savePreferences(); }}>
                    <option value="name">{t('files.name')}</option><option value="size">{t('files.size')}</option><option value="modified">{t('files.modified')}</option>
                </select></label>
                <button type="button" class="btn" onclick={() => { descending = !descending; savePreferences(); }}>{t(descending ? 'files.descending' : 'files.ascending')}</button>
                <span class="muted" role="status">{t('files.resultCount', { shown: visibleEntries.length, total: entries?.length ?? 0 })}</span>
            </div>
            <div class="toolbar view-controls" aria-label={t('files.view')}>
 <button class="btn" onclick={()=>shortcuts=true}>{t('files.shortcuts')}</button>
                {#each views as mode}<button class="btn" disabled={!preferencesReady} title={mode==='large'?t('files.thumbnailHint'):undefined} aria-pressed={view===mode} onclick={()=>changeView(mode as FileView)}>{t(`files.view.${mode}`)}</button>{/each}
                {#if root?.kind!=='stick'}<label><input type="checkbox" checked={folderViewSaved} disabled={!preferencesReady||!folderViewSaved&&folderViews.length>=200} onchange={toggleFolderView}/>{t('files.rememberFolderView')}</label>{/if}
                <button class="btn" disabled={!clipboard || !writable} onclick={paste}>{t('files.paste')}</button>
                {#if clipboard}<span class="muted" role="status">{t(clipboard.move ? 'files.cutCount' : 'files.copiedCount',{count:clipboard.paths.length})}</span>{/if}
            </div>
            <form class="toolbar path-form" onsubmit={goToPath}>
                <label for="file-path">{t('files.folderPath')}</label><input id="file-path" class="field mono" bind:value={pathInput} placeholder="/" aria-describedby="file-path-hint" />
                <button class="btn" disabled={!root}>{t('files.goToPath')}</button><button type="button" class="btn" disabled={!root} onclick={copyPath}>{t('files.copyPath')}</button>
                <span id="file-path-hint" class="muted">{t('files.pathHint',{name:root?.name??''})}</span>
                {#if pathMessage}<span role="status">{pathMessage}</span>{/if}
            </form>
            <div class="file-entries" class:small={view==='small'} class:large={view==='large'}>
            <div class="row header">
				<input type="checkbox" aria-label={t('files.selectAll')} checked={allSelected} onchange={(event) => (selected = event.currentTarget.checked ? visibleEntries.map((entry) => entry.name) : [])} />
				<span>{t(view==='list'?'files.name':'files.selectAll')}</span>
				<span class="num">{t('files.size')}</span>
				<span class="num">{t('files.modified')}</span><span class="sr-only">{t('files.entryActions')}</span>
			</div>
			<VirtualEntries bind:this={virtualList} items={visibleEntries} key={e=>e.name} minWidth={view==='list'?0:view==='small'?220:160} rowHeight={view==='large'?260:72} label={t('files.title')} reset={`${rootId}/${dir}/${search}/${view}/${sort}/${descending}`}>
            {#snippet children(entry)}
				<!-- The whole row is a larger target for the mouse. Keyboards and screen
				     readers use the checkbox and the folder button inside it, so the row
				     itself is no control: one nested in another is announced badly. -->
				<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
				<div class="row" tabindex="-1" draggable={root?.kind!=='stick'} ondragstart={event=>dragEntry(event,entry)} ondragend={endFileDrag} ondragover={event=>{if(entry.dir&&writable&&hasFileDrag(event)){event.preventDefault();event.stopPropagation();}}} ondrop={event=>{if(entry.dir)reviewDrop(event,rootId,join(dir,entry.name));}} data-file-name={entry.name} class:picked={selectedNames.has(entry.name)} onclick={(event) => rowClicked(entry, event)} oncontextmenu={(event)=>{event.preventDefault();event.stopPropagation();entryMenu(entry);}}>
					<input type="checkbox" aria-label={t('files.select', { name: entry.name })} checked={selectedNames.has(entry.name)} onclick={(event)=>{if(event.shiftKey)selectRange(entry.name,event.ctrlKey||event.metaKey);else toggle(entry.name,event.currentTarget.checked);}} />
					{#if entry.dir}
						<button type="button" class="entry" onclick={(event) => folderClick(entry,event)}><Icon name="folder" size={view==='large'?48:view==='small'?24:16} /><span>{entry.name}</span></button>
					{:else}
						<span class="entry">{#if view==='large' && previewKind(entry.name,entry.size)==='image'}<FileThumbnail root={rootId} path={join(dir,entry.name)} size={entry.size} />{:else}<Icon name="file" size={view==='large'?48:view==='small'?24:16} />{/if}<span>{entry.name}</span></span>
					{/if}
					<span class="mono num muted">{entry.dir ? '–' : formatBytes(entry.size)}</span>
					<span class="num muted">{date.format(new Date(entry.modified))}</span>
                    <button type="button" class="btn entry-more" aria-label={t('files.actionsFor',{name:entry.name})} onclick={()=>entryMenu(entry)}>⋯</button>
				</div>
            {/snippet}</VirtualEntries>
            {#if !visibleEntries.length}
                {#if !root}<EmptyState inline title={t('files.chooseRoot')} />{:else if entries}<EmptyState inline title={t(search && entries.length > 0 ? 'files.noMatches' : 'files.empty')} />{/if}
            {/if}

            </div>
			{#if selected.length > 0}
				<div class="actions">
                    {#if writable && selected.length>1}<button class="btn" onclick={()=>batchRename=true}>{t('files.renameBatch')}</button>{/if}
					<span class="muted count">{t('files.selected', { count: selected.length })} · {formatBytes(measured?.key===selectionKey?measured.bytes:selectedBytes)}{#if selectedFolders && measured?.key!==selectionKey} · {t('files.foldersNotCounted',{count:selectedFolders})}{/if}</span>
                    {#if selectedFolders}<button class="btn" disabled={measuring} onclick={measureSelection}>{t(measuring?'files.measuring':'files.measureFolders')}</button>{/if}
					{#if currentUSBBackup}<button type="button" class="btn" disabled={currentUSBBackup.state?.running} onclick={()=>copying={move:false,restore:true}}>{t('files.restoreSelected')}</button>{/if}
					<button type="button" class="btn" onclick={download}><Icon name="download" size={16} />{t('files.download')}</button>
					{#if root?.kind !== 'stick'}
						<button type="button" class="btn" onclick={() => (copying = { move: false })}>{t('files.copy')}</button>
					{/if}
					{#if editable}<button type="button" class="btn" onclick={() => (editing = join(dir, selected[0]))}>{t('files.edit')}</button>{/if}
					{#if writable}
						<button type="button" class="btn" onclick={() => (copying = { move: true })}>{t('files.move')}</button>
						{#if selected.length === 1}<button type="button" class="btn" onclick={() => show('rename')}>{t('files.rename')}</button>{/if}
						<button type="button" class="btn btn-danger" onclick={() => show('delete')}>{t('files.delete')}</button>
					{/if}
				</div>
			{/if}

			{#if root?.kind === 'stick'}<p class="muted none">{t('files.stickNote')}</p>{/if}
			{#if writable}
				<p class="drop" class:active={dragging}>
					<Icon name="upload" size={18} />{dragging ? t('files.dropNow', { path: join(root?.name??'', dir) }) : t('files.dropHint')}
				</p>
			{/if}
			{/if}
		</section>
        {#if split}<FolderPane onpreview={(id,path,entry)=>otherPreview={root:id,path,size:entry.size}} ondropfiles={reviewDrop} roots={safeRoots} refresh={jobsTick+uploads.finished} {showHidden} onchange={(id,path,names)=>right={root:id,path,selected:names}}/>{/if}
        </div>
	</div>
{/if}

{#if dropReview}<Dialog title={t('files.dropTransfer')} onclose={()=>dropReview=null}>
 <p>{t('files.dropTransferHint',{count:dropReview.source.paths.length})}</p>
 <p class="mono">{safeRoots.find(r=>r.id===dropReview?.source.root)?.name}: {dropReview.source.paths.join(', ')}</p>
 <p class="mono">→ {safeRoots.find(r=>r.id===dropReview?.target)?.name}/{dropReview.path}</p>
 <div class="dialog-actions"><button class="btn" onclick={()=>dropReview=null}>{t('common.cancel')}</button><button class="btn" onclick={()=>acceptDrop(false)}>{t('files.copy')}</button><button class="btn btn-warn" disabled={!safeRoots.find(r=>r.id===dropReview?.source.root)?.writable} onclick={()=>acceptDrop(true)}>{t('files.move')}</button></div>
 </Dialog>{/if}
<JobsPanel {canRetry} onopen={(id,path)=>open(id,path)} refresh={jobsTick} onfinished={() => { load(); loadRoots(); jobsTick++; }} />
<UploadPanel />
<UndoPanel roots={safeRoots} refresh={jobsTick} onchanged={()=>{load();loadRoots();jobsTick++;}}/>

{#if copying && root && roots}
	<CopyDialog roots={safeRoots} from={copying.from ?? root} paths={copying.paths ?? selected.map((name) => join(dir, name))} move={copying.move} restore={copying.restore} targetRoot={copying.targetRoot} targetDir={copying.targetDir} startDir={dir} onclose={() => (copying = null)} ondone={() => { if(copying?.pasted && copying.move)clipboard=null;copying = null; selected = []; jobsTick++; }} />
{/if}
{#if editing && root}
	<TextEditor root={root.id} stick={root.kind === 'stick'} path={editing} onclose={() => { editing = null; load(); }} />
{/if}

{#if windowsPath && root}<WindowsPath {root} {dir} onclose={() => windowsPath = false} />{/if}

{#if dialog}
	{@const kind = dialog.kind}
	<Dialog
		title={kind === 'mkdir'
			? t('files.newFolderTitle')
			: kind === 'rename'
				? t('files.renameTitle', { name: dialog.name })
				: selected.length === 1
					? t('files.deleteTitleOne', { name: selected[0] })
					: t('files.deleteTitleMany', { count: selected.length })}
		onclose={() => (dialog = null)}
	>
		<form onsubmit={submitDialog}>
			{#if kind === 'delete'}
				{#if root?.recycle}
                    <label class="field-row"><span>{t('files.deleteAction')}</span>
                        <select class="field" bind:value={permanent} onchange={() => { confirmed = false; }}>
                            <option value={false}>{t('files.moveToTrash')}</option>
                            <option value={true}>{t('files.trashDeleteForever')}</option>
                        </select>
                    </label>
                {/if}
                <p class={permanent ? 'notice-error' : 'text'}>{t(permanent ? (root?.recycle ? 'files.trashForeverText' : 'files.deletePermanent') : 'files.deleteRecycle')}</p>
				{#if permanent}
					<label class="confirm"><input type="checkbox" bind:checked={confirmed} /><span>{t('files.deleteConfirm')}</span></label>
				{/if}
			{:else}
				<label class="field-row">
					<span>{t(kind === 'mkdir' ? 'files.name' : 'files.newName')}</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input class="field" type="text" bind:value={typed} maxlength="255" autocomplete="off" spellcheck="false" required autofocus />
				</label>
			{/if}
			{#if dialogError}<p class="notice-warn" role="alert">{dialogError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button>
				{#if kind === 'delete'}
					<button type="submit" class="btn {permanent ? 'btn-danger' : 'btn-warn'}" disabled={permanent && !confirmed}>{t(permanent ? 'files.trashDeleteForever' : 'files.moveToTrash')}</button>
				{:else}
					<button type="submit" class="btn btn-primary">{t(kind === 'mkdir' ? 'files.create' : 'files.rename')}</button>
				{/if}
			</div>
		</form>
	</Dialog>
{/if}

{#if downloadPaths && root}<DownloadDialog {root} paths={downloadPaths} onclose={()=>downloadPaths=null}/>{/if}
{#if duplicates && root}<DuplicatesDialog root={safeRoots.find(r=>r.id===rootId)??root} {dir} {showHidden} onclose={()=>duplicates=false} onopen={path=>{duplicates=false;open(rootId,path);}} onchanged={()=>{load();loadRoots();jobsTick++;}}/>{/if}
{#if usage && root}<UsageDialog {root} {dir} {showHidden} onclose={()=>usage=false} onopen={path=>{usage=false;open(rootId,path);}}/>{/if}
{#if deepSearch && root}<SearchDialog {root} {dir} {showHidden} onclose={()=>deepSearch=false} onopen={path=>{deepSearch=false;open(rootId,path);}}/>{/if}
{#if comparison && root && rightRoot}<CompareDialog left={safeRoots.find(r=>r.id===rootId)??root} leftDir={dir} right={rightRoot} rightDir={right.path} {showHidden} ondone={()=>{comparison=false;jobsTick++;}} onclose={()=>comparison=false} onopen={(id,path)=>{comparison=false;open(id,path);}}/>{/if}
{#if batchRename && root}<RenameBatchDialog {root} {dir} names={selected} onclose={()=>{batchRename=false;load();jobsTick++;}} ondone={()=>{batchRename=false;selected=[];load();jobsTick++;}}/>{/if}
{#if archive && root}<ArchiveDialog {root} path={archive} {dir} roots={safeRoots} onclose={()=>archive=null} ondone={()=>{archive=null;jobsTick++;}}/>{/if}
{#if properties}<FileProperties root={properties.root} path={properties.path} locked={properties.root.kind==='usb'&&!safeRoots.find(r=>r.id===properties?.root.id)?.writable&&properties.root.writable} onclose={()=>properties=null}/>{/if}
{#if uploadReview}<UploadReview root={uploadReview.root} dir={uploadReview.dir} files={uploadReview.files} onclose={()=>uploadReview=null} onconfirm={startUpload}/>{/if}
{#if favoriteManager}<FavoritesDialog {favorites} onsave={saveFavorites} onclose={()=>favoriteManager=false}/>{/if}
{#if creatingFile&&root&&writable}<CreateFileDialog root={rootId} {dir} onclose={()=>creatingFile=false} oncreated={path=>{creatingFile=false;editing=path;load();}}/>{/if}
{#if otherPreview}<FilePreview {...otherPreview} onclose={()=>otherPreview=null}/>{/if}
{#if shortcuts}<Dialog title={t('files.shortcuts')} onclose={()=>shortcuts=false}>
 <p>{t('files.shortcutsHint')}</p><dl class="file-properties">
 {#each [['↑ ↓ ← →','navigate'],['Enter','open'],['Space','preview'],['Shift + ↑ / ↓','range'],['Ctrl / ⌘ + A','all'],['Ctrl / ⌘ + C / X / V','clipboard'],['F2','rename'],['Delete','delete'],['F1','help']] as const as [keys,action]}<dt class="mono">{keys}</dt><dd>{t(`files.key.${action}`)}</dd>{/each}
 </dl><div class="dialog-actions"><button class="btn" onclick={()=>shortcuts=false}>{t('files.editClose')}</button></div>
 </Dialog>{/if}
{#if preview && root}<FilePreview root={root.id} path={join(dir,preview.name)} size={preview.size} onprevious={galleryIndex>0?()=>preview=gallery[galleryIndex-1]:undefined} onnext={galleryIndex>=0&&galleryIndex<gallery.length-1?()=>preview=gallery[galleryIndex+1]:undefined} onclose={()=>{preview=null;}} />{/if}
{#if menu && root}
 <Dialog title={selected.length===1?t('files.actionsFor',{name:menu.name}):t('files.selected',{count:selected.length})} onclose={()=>{menu=null;}}>
  {#if selected.length===1}<dl class="file-properties"><dt>{t('files.path')}</dt><dd>{root.name}/{join(dir,menu.name)}</dd><dt>{t('files.size')}</dt><dd>{menu.dir?'–':formatBytes(menu.size)}</dd><dt>{t('files.modified')}</dt><dd>{date.format(new Date(menu.modified))}</dd></dl>{/if}
  <div class="file-menu">
   {#if writable && selected.length>1}<button class="btn" onclick={()=>{menu=null;batchRename=true;}}>{t('files.renameBatch')}</button>{/if}
   {#if selected.length===1 && !menu.dir && /\.zip$/i.test(menu.name)}<button class="btn" onclick={()=>{archive=join(dir,menu!.name);menu=null;}}>{t('files.archiveTitle')}</button>{/if}
   {#if selected.length===1}<button class="btn" onclick={()=>{properties={root:root!,path:join(dir,menu!.name)};menu=null;}}>{t('files.properties')}</button>{/if}
   {#if root.kind!=='stick'}<button class="btn" onclick={()=>rememberSelection(false)}>{t('files.clipboardCopy')}</button>{#if writable}<button class="btn" onclick={()=>rememberSelection(true)}>{t('files.cut')}</button>{/if}{/if}
   {#if selected.length===1}{#if menu.dir}<button class="btn" onclick={()=>{const name=menu!.name;menu=null;open(rootId,join(dir,name));}}>{t('files.openFolder')}</button>{:else if previewKind(menu.name,menu.size)}<button class="btn" onclick={()=>{preview=menu;menu=null;}}>{t('files.preview')}</button>{/if}{/if}
   <button class="btn" onclick={()=>{menu=null;download();}}>{t('files.download')}</button>
   {#if root.kind!=='stick'}<button class="btn" onclick={()=>{menu=null;copying={move:false};}}>{t('files.copy')}</button>{/if}
   {#if editable}<button class="btn" onclick={()=>{menu=null;editing=join(dir,selected[0]);}}>{t('files.edit')}</button>{/if}
   {#if writable}<button class="btn" onclick={()=>{menu=null;copying={move:true};}}>{t('files.move')}</button>{#if selected.length===1}<button class="btn" onclick={()=>{menu=null;show('rename');}}>{t('files.rename')}</button>{/if}{/if}
  </div>
  <div class="dialog-actions"><button class="btn" onclick={()=>{menu=null;}}>{t('files.editClose')}</button>{#if writable}<button class="btn btn-danger" onclick={()=>{menu=null;show('delete');}}>{t('files.delete')}</button>{/if}</div>
 </Dialog>
{/if}

{#if backgroundMenu && root}
 <Dialog title={t('files.folderActions')} onclose={()=>{backgroundMenu=false;}}>
  <div class="file-menu">
   {#if writable}
    <button class="btn" onclick={()=>{backgroundMenu=false;show('mkdir');}}>{t('files.newFolder')}</button>
    <button class="btn" onclick={()=>{backgroundMenu=false;fileInput?.click();}}>{t('files.upload')}</button>
    <button class="btn" disabled={!clipboard} onclick={paste}>{t('files.paste')}</button>
   {/if}
   {#each views as mode}<button class="btn" title={mode==='large'?t('files.thumbnailHint'):undefined} aria-pressed={view===mode} onclick={()=>{changeView(mode as FileView);backgroundMenu=false;}}>{t(`files.view.${mode}`)}</button>{/each}
  </div>
  <div class="dialog-actions"><button class="btn" onclick={()=>{backgroundMenu=false;}}>{t('files.editClose')}</button></div>
 </Dialog>
{/if}

{#if unlockDialog}<Dialog title={t('files.unlockBackup')} onclose={()=>unlockDialog=false}>
 <p>{t('files.unlockBackupHelp')}</p><div class="dialog-actions"><button class="btn" onclick={()=>unlockDialog=false}>{t('common.cancel')}</button><button class="btn btn-warn" onclick={()=>{unlockedRoot=rootId;unlockDialog=false;}}>{t('files.unlockBackup')}</button></div>
</Dialog>{/if}

<style>
 .row:focus-visible{outline:2px solid var(--accent);outline-offset:-2px;}
 .tools{position:relative;}.tools summary{cursor:pointer;}.tool-options{display:flex;flex-wrap:wrap;gap:8px;padding:8px;background:var(--surface);border:1px solid var(--border);border-radius:var(--radius-ctl);}

 .workspace{min-width:0;display:grid;grid-template-columns:minmax(0,1fr);gap:var(--gap);align-items:start;}.workspace.split{grid-template-columns:repeat(2,minmax(0,1fr));}.pane-actions{grid-column:1/-1;display:flex;gap:8px;flex-wrap:wrap;}@media(max-width:1100px){.workspace.split{grid-template-columns:minmax(0,1fr);}}
 .backup-place progress { width:100%; height:8px; accent-color:var(--accent); } .backup-error { color:var(--error-text); overflow-wrap:anywhere; }
 .backup-place { display:flex; flex-direction:column; gap:4px; padding:4px 10px 10px; font-size:0.75rem; }
 .backup-place button { padding-left:0; } .backup-path { overflow-wrap:anywhere; }
 .backup-empty, .backup-settings { padding:6px 10px; font-size:0.8125rem; }
 .backup-settings { color:var(--accent); } .places button:disabled { opacity:0.6; cursor:default; }

 .path-form { flex-direction:row; } .path-form input { flex:1 1 180px; min-width:0; } .path-form span { flex-basis:100%; font-size:0.8125rem; }
 .view-controls [aria-pressed="true"], .file-menu [aria-pressed="true"] { border-color:var(--accent); color:var(--accent); }
 .file-entries { min-height:120px; }
 .file-entries.small, .file-entries.large { display:block; padding:12px; }

 .small .row.header, .large .row.header { grid-column:1/-1; display:flex; border:0; }
 .small .row.header > span:nth-child(n+3), .large .row.header > span:nth-child(n+3) { display:none; }
 .small .row:not(.header), .large .row:not(.header) { border:1px solid var(--border); border-radius:var(--radius-ctl); padding:10px; grid-template-columns:18px minmax(0,1fr) 30px; gap:8px; }
 .small .row:not(.header) > .num, .large .row:not(.header) > .num { display:none; }
 .large .row:not(.header) { align-content:start; }
 .large .entry { grid-column:1/-1; grid-row:2; flex-direction:column; text-align:center; min-height:130px; justify-content:center; }
 .large .entry span { white-space:normal; overflow-wrap:anywhere; width:100%; }
 .large .entry-more { grid-column:3; grid-row:1; }

 .favorite-row { display:flex; min-width:0; } .favorite-row > button:first-child { flex:1; min-width:0; }
 .entry-more { padding:0; min-height:36px; } .file-menu { display:flex; flex-wrap:wrap; gap:8px; } .file-properties dd { margin:0 0 8px; overflow-wrap:anywhere; } .file-properties dt { color:var(--muted); font-size:0.8rem; }
 .unfinished { padding: calc(var(--row-pad) + 2px) var(--tile-pad); border-bottom: 1px solid var(--border); }
 .unfinished-row { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; margin-top: 10px; }
 .unfinished-row > span { flex: 1; overflow-wrap: anywhere; }
	.layout {
		display: grid;
		grid-template-columns: 230px minmax(0, 1fr);
		gap: var(--gap);
		align-items: start;
	}
	.places {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: 12px 8px;
	}
	.advanced summary {
		padding: 10px;
		cursor: pointer;
		font-size: 0.875rem;
		color: var(--text);
	}
	.advanced button { width: 100%; }
	.group {
		display: block;
		padding: 8px 10px 4px;
		font-size: 0.75rem;
		color: var(--muted);
	}
	.places button {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 38px;
		padding: 0 10px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		font: inherit;
		font-size: 0.875rem;
		text-align: left;
		cursor: pointer;
	}
	.places button:hover,
	.places button.current {
		background: var(--chip);
		color: var(--text);
	}
	.places button.current {
		font-weight: 500;
	}
	.place-name {
		flex: 1 1 auto;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.usage {
		margin: 10px 10px 2px;
		padding-top: 10px;
		border-top: 1px solid var(--panel-border);
		font-size: 0.75rem;
	}
	.browser {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.browser.dragging {
		border-color: var(--accent);
	}
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		padding: 12px 14px;
		border-bottom: 1px solid var(--panel-border);
	}
	.file-filters label { display: flex; flex-direction: column; gap: 4px; min-width: 0; font-size: 0.8125rem; }
	.file-filters { align-items: end; }
	.file-filters .search-field { flex: 1 1 220px; }
	.file-filters .field { width: 100%; min-width: 0; }
	.toolbar .btn {
		height: 2.375rem;
	}
	.crumbs {
		flex: 1 1 200px;
		min-width: 0;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px;
		font-size: 0.875rem;
	}
	.crumbs button {
		padding: 4px 6px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-small);
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.crumbs button:hover {
		background: var(--chip);
	}
	.row {
		display: grid;
		grid-template-columns: 22px minmax(0, 1fr) 90px 170px 38px;
		align-items: center;
		gap: 12px;
		cursor: pointer;
		padding: 9px 14px;
		border-bottom: 1px solid var(--panel-border);
		font-size: 0.875rem;
	}
	.row.header {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.row.picked {
		background: color-mix(in srgb, var(--accent) 12%, transparent);
	}
	.row input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.num {
		text-align: right;
		font-size: 0.8125rem;
		white-space: nowrap;
	}
	.entry {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
		padding: 0;
		background: transparent;
		border: 0;
		color: var(--text);
		font: inherit;
		text-align: left;
	}
	button.entry {
		cursor: pointer;
	}
	button.entry:hover span {
		text-decoration: underline;
	}
	.entry span {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.none {
		padding: 28px 14px;
		font-size: 0.875rem;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		padding: 10px 14px;
		background: var(--chip);
		border-bottom: 1px solid var(--panel-border);
	}
	.actions .btn {
		height: 2.25rem;
	}
	.count {
		margin-right: auto;
		font-size: 0.8125rem;
	}
	.drop {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		margin: 14px;
		padding: 16px;
		border: 1px dashed var(--outline-border);
		border-radius: var(--radius-ctl);
		font-size: 0.8125rem;
		color: var(--muted);
	}
	.drop.active {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 8%, transparent);
		color: var(--text);
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.field-row {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.875rem;
	}
	.text {
		font-size: 0.875rem;
	}
	@media (max-width: 760px) {
		.layout {
			grid-template-columns: 1fr;
		}
		.row {
			grid-template-columns: 22px minmax(0, 1fr) 80px 38px;
		}
		.row > :nth-child(4) {
			display: none;
		}
	}
	.usb { display: flex; align-items: center; gap: 10px; min-height: 38px; padding: 0 10px; color: var(--muted); font-size: 0.875rem; }
	.mount, .eject { background: var(--chip); border: 0; border-radius: var(--radius-ctl); color: var(--text); cursor: pointer; font: inherit; font-size: 0.8rem; padding: 0.2rem 0.6rem; }
	.eject { align-self: flex-end; margin: -4px 10px 4px; }
</style>
