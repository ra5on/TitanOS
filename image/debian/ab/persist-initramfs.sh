#!/bin/sh
# init-bottom: the real root is mounted, PID 1 has not read its configuration yet.
PREREQ=""
prereqs() { echo "$PREREQ"; }
case "$1" in prereqs) prereqs; exit 0 ;; esac
. /scripts/functions
case " $(cat /proc/cmdline) " in *' rauc.slot=A '*|*' rauc.slot=B '*) ;; *) exit 0 ;; esac
task_root="${rootmnt:-/root}"
task_data="$task_root/var/lib/titan-system"
[ -d "$task_data" ] || panic 'Titan: Ziel für die Datenpartition fehlt im Systemimage.'
# Do not bind all of /var: dpkg/apt and the OS package inventory belong to
# the selected slot, while only mutable service state follows an A/B switch.
task_paths='var/lib/titan var/lib/titan-agent var/lib/titan-proxy var/lib/docker var/lib/containerd var/lib/libvirt var/lib/samba var/lib/systemd var/lib/private var/lib/dbus var/lib/wtmpdb var/cache var/log var/tmp var/spool var/srv/titan home'
for task_path in $task_paths tmp; do
    [ -d "$task_root/$task_path" ] && [ ! -L "$task_root/$task_path" ] || panic 'Titan: Schreibpfad fehlt im Systemimage.'
done
task_fsck=0
/sbin/e2fsck -p /dev/disk/by-partlabel/TITAN-DATA || task_fsck=$?
case "$task_fsck" in 0|1) ;; *) panic 'Titan: Datenpartition benötigt eine Dateisystemprüfung.' ;; esac
/bin/titan-mount -t ext4 -o rw /dev/disk/by-partlabel/TITAN-DATA "$task_data" || panic 'Titan: Datenpartition konnte nicht eingehängt werden.'
# A durable completed directory is the publication point; an interrupted seed
# is rebuilt from the pristine root, never accepted as complete.
if [ ! -d "$task_data/persistent" ]; then
    rm -rf "$task_data/.seed"
    mkdir -p "$task_data/.seed/etc"
    for task_path in $task_paths; do
        mkdir -p "$task_data/.seed/$task_path" || panic 'Titan: Initialisierung der persistenten Daten fehlgeschlagen.'
        cp -a "$task_root/$task_path/." "$task_data/.seed/$task_path/" || panic 'Titan: Initialisierung der persistenten Daten fehlgeschlagen.'
    done
    printf '1\n' > "$task_data/.seed/.readonly-layout-v1" || panic 'Titan: Persistenzkennung konnte nicht geschrieben werden.'
    sync
    mv "$task_data/.seed" "$task_data/persistent" || panic 'Titan: Persistenz konnte nicht abgeschlossen werden.'
    sync
fi
# Older A/B seeds already contain Samba cache under persistent/var/cache.
# Add new write paths atomically and keep every existing file and its metadata.
# A marker is published only after every new path is durable; an interrupted
# migration can resume without resetting identities, databases or user data.
if [ ! -f "$task_data/persistent/.readonly-layout-v1" ]; then
    rm -rf "$task_data/.readonly-seed"
    mkdir -p "$task_data/.readonly-seed" || panic 'Titan: Persistenz-Erweiterung konnte nicht vorbereitet werden.'
    for task_path in $task_paths; do
        task_target="$task_data/persistent/$task_path"
        if [ -e "$task_target" ] || [ -L "$task_target" ]; then
            [ -d "$task_target" ] && [ ! -L "$task_target" ] || panic 'Titan: Ungültiger persistenter Schreibpfad.'
            continue
        fi
        mkdir -p "$task_data/.readonly-seed/$task_path" "${task_target%/*}" || panic 'Titan: Persistenz-Erweiterung fehlgeschlagen.'
        cp -a "$task_root/$task_path/." "$task_data/.readonly-seed/$task_path/" || panic 'Titan: Persistenz-Erweiterung fehlgeschlagen.'
        sync
        mv "$task_data/.readonly-seed/$task_path" "$task_target" || panic 'Titan: Persistenz-Erweiterung konnte nicht abgeschlossen werden.'
    done
    # Fill only absent first-level factory cache entries. In particular the
    # existing Samba subtree is never traversed, chmodded or overwritten.
    for task_source in "$task_root/var/cache/"* "$task_root/var/cache/".[!.]* "$task_root/var/cache/"..?*; do
        [ -e "$task_source" ] || [ -L "$task_source" ] || continue
        task_target="$task_data/persistent/var/cache/${task_source##*/}"
        [ -e "$task_target" ] || [ -L "$task_target" ] || {
            cp -a "$task_source" "$task_data/.readonly-seed/cache-entry" || panic 'Titan: Cache-Initialisierung fehlgeschlagen.'
            sync
            mv "$task_data/.readonly-seed/cache-entry" "$task_target" || panic 'Titan: Cache-Initialisierung konnte nicht abgeschlossen werden.'
        }
    done
    sync
    printf '1\n' > "$task_data/persistent/.readonly-layout-v1" || panic 'Titan: Persistenzkennung konnte nicht geschrieben werden.'
    sync
    rm -rf "$task_data/.readonly-seed"
fi
# Only local changes enter the persistent upper layer. Unmodified Debian
# defaults come from the selected root slot, including CA and service updates.
# Disable lower-inode indexing: A and B intentionally have different identities.
mkdir -p "$task_data/etc-work"
/bin/titan-mount -t overlay overlay -o "lowerdir=$task_root/etc,upperdir=$task_data/persistent/etc,workdir=$task_data/etc-work,index=off,metacopy=off,redirect_dir=off" "$task_root/etc" || panic 'Titan: Persistente Konfiguration konnte nicht eingebunden werden.'
for task_path in $task_paths; do
    [ -d "$task_data/persistent/$task_path" ] && [ ! -L "$task_data/persistent/$task_path" ] || panic 'Titan: Persistente Verzeichnisse sind unvollständig.'
    /bin/titan-mount -o bind "$task_data/persistent/$task_path" "$task_root/$task_path" || panic 'Titan: Persistente Daten konnten nicht eingebunden werden.'
done
/bin/titan-mount -t tmpfs -o rw,nosuid,nodev,mode=1777,size=25% tmpfs "$task_root/tmp" || panic 'Titan: Temporärer Speicher konnte nicht eingebunden werden.'
# Non-recursive remount: the OS slot becomes read-only, while the already
# mounted overlay, DATA binds and tmpfs retain their own writable flags.
/bin/titan-mount -o remount,ro "$task_root" || panic 'Titan: Systempartition konnte nicht schreibgeschützt werden.'
