# Aufbau von `backend/internal/api`

Erzeugt von `scripts/api-layout.py --write`; nicht von Hand ändern. Der Test
`scripts/tests/test_api_layout.py` schlägt fehl, wenn die Übersicht nicht
mehr stimmt.

Das Paket ist ein flacher Ordner. Was zusammengehört, zeigt der Dateiname:
Das erste Wort (bis zum ersten `_`) ist die Gruppe. Regeln dazu stehen in
`docs/rules/API.md` („Dateien im API-Paket“ und „Bereiche statt `s.opts`“).

| Gruppe | Dateien | Bereiche | Tests |
|---|---|---|---|
| about | `about.go` | `aboutArea` | 0 |
| access | `access.go` | `accountKeysArea`, `certificateArea`, `loginSettingsArea`, `sshArea` | 0 |
| account | `account.go`, `account_picture.go` | – | 3 |
| activity | `activity.go` | `activityArea` | 1 |
| admission | `admission.go` | – | 0 |
| agent | `agent.go`, `agent_addresses.go`, `agent_assist.go`, `agent_cluster.go`, `agent_code.go`, `agent_data.go`, `agent_docs.go`, `agent_docs_deadline.go`, `agent_docs_index.go`, `agent_docs_paper.go`, `agent_env.go`, `agent_extra.go`, `agent_family.go`, `agent_family54.go`, `agent_family54_buddy.go`, `agent_family54_catalog.go`, `agent_features.go`, `agent_features_stub.go`, `agent_foresight.go`, `agent_foresight_advisor.go`, `agent_foresight_model.go`, `agent_ha.go`, `agent_home.go`, `agent_initiative.go`, `agent_map.go`, `agent_media.go`, `agent_notes.go`, `agent_own.go`, `agent_prefs.go`, `agent_protect.go`, `agent_protect_emergency.go`, `agent_protect_guard.go`, `agent_protect_handbook.go`, `agent_reflect.go`, `agent_schedule.go`, `agent_settings.go`, `agent_share.go`, `agent_snapshot.go`, `agent_tools.go`, `agent_volume.go`, `agent_when.go` | – | 32 |
| api | `api.go` | – | 1 |
| app | `app_access.go`, `app_icon.go`, `app_proxy.go` | `appAccessArea`, `appIconArea`, `appProxyArea` | 4 |
| application | `application_restore.go` | – | 0 |
| apps | `apps.go` | `appStoreArea` | 0 |
| area | `area_hooks.go` | – | 0 |
| auth | `auth.go`, `auth_area.go` | `authArea` | 1 |
| backup | `backup_coverage.go` | – | 2 |
| backups | `backups.go`, `backups_vmdisks.go` | `backupsArea` | 1 |
| backuptarget | `backuptarget.go` | `backupTargetArea` | 0 |
| catalog | `catalog.go`, `catalog_actions.go`, `catalog_areas.go`, `catalog_files.go`, `catalog_status.go` | – | 2 |
| cluster | `cluster.go` | `clusterArea` | 1 |
| configbackup | `configbackup.go` | `configBackupArea` | 0 |
| craft | `craft_replace.go` | – | 1 |
| dashboard | `dashboard_background.go` | – | 1 |
| diagnose | `diagnose.go` | `diagnoseArea` | 0 |
| docker | `docker.go`, `docker_opens.go`, `docker_resources.go` | `dockerArea`, `dockerOpensArea`, `terminalArea` | 3 |
| energy | `energy.go` | `energyArea`, `powerScheduleArea` | 0 |
| fans | `fans.go` | `fansArea` | 0 |
| file | `file_download.go`, `file_media.go`, `file_tools.go` | – | 2 |
| files | `files.go` | `filesArea` | 0 |
| firewall | `firewall.go` | `firewallArea` | 1 |
| ha | `ha_area.go`, `ha_connections.go`, `ha_storage.go` | `haArea` | 2 |
| healthreport | `healthreport.go` | `healthReportArea` | 1 |
| homeassistant | `homeassistant.go`, `homeassistant_mqtt.go`, `homeassistant_telemetry.go`, `homeassistant_undo.go` | – | 3 |
| hwcheck | `hwcheck.go` | `hwcheckArea` | 1 |
| inprocess | `inprocess.go` | – | 0 |
| intel | `intel_gpu.go` | `intelGucArea`, `uasQuirksArea` | 1 |
| ipv64 | `ipv64.go`, `ipv64_area.go` | `ipv64Area` | 1 |
| letsencrypt | `letsencrypt.go` | `ddnsArea`, `letsEncryptArea` | 1 |
| lifecycle | `lifecycle.go` | – | 1 |
| login | `login_accounts.go`, `login_appearance.go`, `login_device.go` | `loginLookArea` | 2 |
| lxc | `lxc.go` | `lxcArea` | 1 |
| mcp | `mcp.go`, `mcp_tools.go` | – | 2 |
| media | `media.go` | `mediaArea` | 0 |
| memory | `memory_policy.go` | `memoryArea` | 0 |
| monitoring | `monitoring_export.go` | – | 0 |
| netbird | `netbird.go` | `netbirdArea` | 0 |
| network | `network.go` | `networkArea`, `timezoneArea` | 0 |
| notifications | `notifications.go` | `accountNotifyArea`, `notifyArea` | 0 |
| nvidia | `nvidia.go` | `nvidiaArea` | 0 |
| nvr | `nvr.go`, `nvr_events.go`, `nvr_exports.go`, `nvr_faces.go`, `nvr_live.go`, `nvr_ptz.go`, `nvr_recordings.go` | `nvrArea` | 12 |
| openapi | `openapi.go` | – | 0 |
| operations | `operations.go` | `maintenanceArea`, `monitoringArea` | 1 |
| passkeys | `passkeys.go` | – | 1 |
| plugins | `plugins.go` | `pluginsArea` | 1 |
| power | `power_button.go` | `powerButtonArea` | 0 |
| preferences | `preferences.go` | `prefsArea` | 0 |
| processes | `processes.go` | `processesArea` | 1 |
| public | `public_routes.go` | – | 0 |
| register | `register_accounts.go`, `register_agent.go`, `register_backups.go`, `register_docker.go`, `register_files.go`, `register_hardware.go`, `register_homeassistant.go`, `register_login.go`, `register_lxc.go`, `register_network.go`, `register_notifications.go`, `register_plugins.go`, `register_preferences.go`, `register_remote.go`, `register_shares.go`, `register_storage.go`, `register_system.go`, `register_terminal.go`, `register_usb.go`, `register_vms.go` | – | 0 |
| replication | `replication.go` | `replicationArea` | 0 |
| retired | `retired.go` | – | 1 |
| scripts | `scripts.go` | `scriptsArea` | 1 |
| server | `server_core.go` | – | 0 |
| services | `services.go` | `servicesArea` | 1 |
| session | `session_lock.go` | – | 1 |
| share | `share_data.go` | – | 2 |
| shares | `shares.go` | `sharesArea` | 1 |
| snapshot | `snapshot_folders.go` | – | 1 |
| startup | `startup.go` | `startupArea` | 0 |
| storage | `storage.go`, `storage_crypt.go` | `storageArea`, `storageCryptArea` | 3 |
| sysdata | `sysdata.go` | `sysDataArea` | 1 |
| system | `system.go` | `powerArea`, `systemArea` | 0 |
| tailscale | `tailscale.go` | `tailscaleArea` | 0 |
| terminal | `terminal_console.go` | – | 1 |
| tokens | `tokens.go` | `tokensArea` | 2 |
| tool | `tool_env.go` | – | 0 |
| ui | `ui.go` | – | 0 |
| update | `update.go` | `updateArea` | 0 |
| ups | `ups.go` | `upsArea` | 0 |
| usb | `usb.go` | `usbArea` | 0 |
| users | `users.go` | `usersArea` | 1 |
| viewer | `viewer.go` | `viewerArea` | 1 |
| vm | `vm_snapshots.go` | – | 4 |
| vms | `vms.go` | `vmsArea` | 0 |
| volume | `volume_checks.go` | `volumeUseArea` | 1 |
| webapps | `webapps.go` | `webappsArea` | 1 |
| webdav | `webdav.go` | `davArea` | 1 |
| webpush | `webpush.go` | `pushArea` | 1 |
| wireguard | `wireguard.go` | `wireGuardArea` | 1 |
