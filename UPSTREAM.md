# Titan: Umbrel baseline

This experimental, noncommercial Titan distribution is based on the complete
published Umbrel 2.0.0 source, commit
`9298257b0e904ca8d8270b1702672666343f0b86`:
https://github.com/getumbrel/umbrel/tree/2.0.0

Umbrel's original LICENSE.md is preserved without modification. It applies to
the inherited code and this derivative distribution. Individual third-party
components and installable applications retain their respective licenses.
Titan is an independent project and is not an official Umbrel release.

The original README is preserved as README.upstream.md. The original GitHub
workflows are preserved as text in .titan/upstream-workflows/; they require
Umbrel-specific runners and publication credentials and are not activated here.
Titan supplies a dedicated AMD64 image build, verification and GitHub release
pipeline. The license and this notice are also included inside the image at
/usr/share/doc/titan/.

The first TitanOS 2.0 image preserves the published NAS functionality while
adding Titan branding and an independently controlled GitHub OS update feed.
The feed verifies an Ed25519-signed checksum inventory, signed release/build
metadata, compatible architecture/layout and the downloaded Rugix bundle
before installation. It never executes an update script supplied by a remote
release. Alpha, beta and stable channels are selected locally.

Upstream Google, Dropbox and OneDrive OAuth registrations and the hosted OAuth
redirect default are removed. Those optional providers require independently
configured runtime credentials and a redirect service before they are shown.
WebDAV and iCloud paths remain available. Third-party hosted integrations are
not transferred to Titan by this source import.

The official App Store is used as an external source. Titan does not copy or
relicense the separate catalog or its application images. Applications are
installed by the user; they are not all preinstalled in the disk image.

The old Titan implementation remains in Git history and existing releases.
The new baseline has a different disk layout and requires a fresh installation;
its Rugix update bundle cannot update the previous Titan/RAUC installation.
