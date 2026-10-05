#!/usr/bin/env python3
"""Validate complete, successful VM evidence before signing a public release."""
import json
from pathlib import Path
import sys
import os
import re

RUNTIME={'administrator_setup_login','system_update_state_confirmation','cpu_ram_metrics',
         'smb_multiuser_access','runtime_components','docker_app_lifecycle','docker_custom_network_lifecycle','docker_multi_container_lifecycle','docker_native_workbench','login_protection','storage_data_boundary',
         'storage_components','custom_service_containment','app_catalog_first_login','photos_lifecycle','app_install_responsiveness','os_root_protection'}
PHOTOS={'internal_data_library_created','bounded_png_upload','background_index_completed','private_preview_bytes',
        'original_bytes_match','unauthenticated_preview_denied','unauthenticated_original_denied',
        'favorite_album_roundtrip','trash_hides_original','trash_restore_exact_content','album_library_removed','test_folder_removed'}
INSTALL_RESPONSIVENESS={'active_install_samples','file_manager_usable','status_usable','max_request_ms',
                        'guest_memory_total_bytes','min_memory_available_bytes'}
FIRST_LOGIN_MODES={'default','generated','install','none','setup','documentation'}
STORAGE_BOUNDARY={'internal_data_resource_ready','default_browser_is_nas_data',
                  'administrator_os_listing_denied','administrator_os_read_denied','administrator_os_write_denied',
                  'data_create_read_edit_delete','stale_revision_rejected','test_file_removed'}
CONTAINMENT={'legacy_root_was_running','legacy_root_was_enabled','legacy_root_stopped','legacy_root_autostart_disabled',
             'unit_file_preserved','api_start_actions_blocked','early_boot_gate_verified','management_requires_gate'}
STORAGE_COMPONENTS={'ext4_tools','xfs_tools','zfs_module_loaded','zfs_module_matches_kernel','zpool_query','zfs_query'}
OS_ROOT_PROTECTION={'pid1_mounts_verified','root_readonly','root_has_no_writable_alias',
    'usr_slot_readonly','dpkg_slot_readonly','apt_slot_readonly','etc_overlay_on_data_writable',
    'data_partition_writable','persistent_state_mounts_writable','tmpfs_writable'}
AB={'baseline_boot_health','proxmox_style_data_growth','signed_update_staged_without_reboot',
    'update_boot_and_preserved_accounts_acls_data','manual_rollback_and_preserved_accounts_acls_data',
    'failed_candidate_fallback_after_reset','factory_defaults_follow_selected_slot'}


def validate(directory):
    directory=Path(directory)
    if (directory/'boot-status').read_text().strip()!='passed':raise ValueError('Boot check did not pass')
    runtime=json.loads((directory/'runtime-test.json').read_text())
    ab=json.loads((directory/'ab-test.json').read_text())
    for result in (runtime,ab):
        if result.get('ok') is not True or result.get('raw_image_unchanged') is not True:
            raise ValueError('Failed checks or modified distributable image')
    checks=runtime.get('checks')
    if not isinstance(checks,list):raise ValueError('Missing runtime checks')
    names=[x['name'] for x in checks]
    if len(set(names))!=len(names):raise ValueError('Ambiguous runtime check names')
    passed={x['name'] for x in checks if x.get('status')=='passed'}
    if not RUNTIME.issubset(passed):raise ValueError('Required runtime check missing or failed')
    if any(x.get('status') not in ('passed','skipped') for x in checks):raise ValueError('Runtime check failed')
    protected=next(x for x in checks if x['name']=='os_root_protection').get('values')
    if (not isinstance(protected,dict) or set(protected)!=OS_ROOT_PROTECTION or
            any(value is not True for value in protected.values())):
        raise ValueError('Real host read-only OS root and writable persistent mounts were not completely verified')
    photos=next(x for x in checks if x['name']=='photos_lifecycle').get('values')
    if not isinstance(photos,dict) or set(photos)!=PHOTOS or any(value is not True for value in photos.values()):
        raise ValueError('Private Photos upload/index/download/album/trash lifecycle was not completely verified')
    responsive=next(x for x in checks if x['name']=='app_install_responsiveness').get('values')
    if (not isinstance(responsive,dict) or set(responsive)!=INSTALL_RESPONSIVENESS or
            type(responsive['active_install_samples']) is not int or not 1<=responsive['active_install_samples']<=512 or
            responsive['file_manager_usable'] is not True or responsive['status_usable'] is not True or
            type(responsive['max_request_ms']) is not int or not 0<=responsive['max_request_ms']<=8000 or
            type(responsive['guest_memory_total_bytes']) is not int or not 7*1024**3<=responsive['guest_memory_total_bytes']<=8*1024**3 or
            type(responsive['min_memory_available_bytes']) is not int or not 128*1024**2<=responsive['min_memory_available_bytes']<=responsive['guest_memory_total_bytes']):
        raise ValueError('File manager/status responsiveness and RAM reserve during an actual installation were not verified')
    catalog=next(x for x in checks if x['name']=='app_catalog_first_login').get('values')
    if (not isinstance(catalog,dict) or set(catalog)!={'app_count','first_login_mode_counts','ok'} or
            catalog['ok'] is not True or type(catalog['app_count']) is not int or not 1<=catalog['app_count']<=4096 or
            not isinstance(catalog['first_login_mode_counts'],dict) or set(catalog['first_login_mode_counts'])!=FIRST_LOGIN_MODES or
            any(type(value) is not int or value<0 for value in catalog['first_login_mode_counts'].values()) or
            sum(catalog['first_login_mode_counts'].values())!=catalog['app_count']):
        raise ValueError('Catalog guidance and first-login instructions were not verified for every offered template')
    boundary=next(x for x in checks if x['name']=='storage_data_boundary').get('values')
    if (not isinstance(boundary,dict) or set(boundary)!=STORAGE_BOUNDARY or
            any(value is not True for value in boundary.values())):
        raise ValueError('NAS data/OS boundary was not completely verified')
    containment=next(x for x in checks if x['name']=='custom_service_containment').get('values')
    if (not isinstance(containment,dict) or set(containment)!=CONTAINMENT or
            any(value is not True for value in containment.values())):
        raise ValueError('Legacy root service boot containment was not completely verified')
    components=next(x for x in checks if x['name']=='storage_components').get('values')
    component_fields=STORAGE_COMPONENTS|{'kernel','zfs_arc_max_bytes','zfs_arc_min_bytes','zfs_arc_size_bytes'}
    if (not isinstance(components,dict) or set(components)!=component_fields or
            any(components.get(key) is not True for key in STORAGE_COMPONENTS) or
            not isinstance(components['kernel'],str) or not re.fullmatch(r'[a-zA-Z0-9.+_-]{1,128}',components['kernel']) or
            type(components['zfs_arc_max_bytes']) is not int or components['zfs_arc_max_bytes']!=1073741824 or
            type(components['zfs_arc_min_bytes']) is not int or components['zfs_arc_min_bytes']!=134217728 or
            type(components['zfs_arc_size_bytes']) is not int or not 0<=components['zfs_arc_size_bytes']<2**64):
        raise ValueError('Installed storage tools, running-kernel ZFS module or bounded ARC were not verified')
    if not isinstance(ab.get('checks'),list) or not AB.issubset(ab['checks']):
        raise ValueError('Required update/rollback check missing')
    system_only = os.environ.get('TITAN_UPDATE_KIND') == 'system'
    initial_release = os.environ.get('TITAN_INITIAL_RELEASE') == 'true'
    if initial_release and (system_only or os.environ.get('TITAN_DRAFT_RELEASE') != 'true'):
        raise ValueError('Bootstrap evidence can only create a draft fresh-install image')
    baseline_source = 'fresh-install-bootstrap' if initial_release else 'published-system-release' if system_only else 'published-release'
    expected_version = '2.9.99' if initial_release else os.environ.get('TITAN_PREVIOUS_TAG', '').removeprefix('titan-').lstrip('v') if system_only else os.environ.get('TITAN_BASELINE_VERSION', '0.4.6-alpha.1')
    baseline_check = 'fresh_install_bootstrap_baseline' if initial_release else 'published_release_baseline'
    if (ab.get('baseline_source') != baseline_source or ab.get('baseline_version') != expected_version
            or ab.get('baseline_image_unchanged') is not True or baseline_check not in ab['checks']):
        raise ValueError('Update from the expected verified published baseline was not verified')
    if system_only:
        previous=json.loads((directory.parent/'debian-input/previous-manifest.json').read_text())
        if not expected_version or ab.get('baseline_bundle_sha256') != previous['bundle']['sha256']:
            raise ValueError('Published system baseline bundle was not verified')
    return {k:'passed' for k in ('boot_test','runtime_test','update_test','rollback_test')}


if __name__=='__main__':
    root=Path(sys.argv[1]);evidence=validate(root)
    (root/'release-evidence.json').write_text(json.dumps(evidence)+'\n')
