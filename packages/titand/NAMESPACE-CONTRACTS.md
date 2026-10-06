# Titan runtime namespace

The daemon package and executable are `titand`. The fresh OS provides the Unix
user/group `titan` (UID/GID 1000), data at `/home/titan/titan`, `titan.yaml`,
`titan.db`, and the `/titanOS` OS marker. Internal environment variables use
`TITAN_*`; there are no inherited OAuth registrations or runtime secrets.

App installation keeps these explicit external format contracts:

- Published repositories contain `umbrel-app.yml` and `umbrel-app-store.yml`.
  The default external repository has the catalog identity `umbrel-app-store`.
- Third-party exports and hooks receive `UMBREL_ROOT` as an alias of `TITAN_ROOT`.
  Recognized legacy helper and user-file references are rewritten to Titan's app
  helper and `titan.yaml`; installed export files are not rewritten in place.
- The external manifest field `optimizedForUmbrelHome` is normalized to
  `optimizedForTitanHome` at the parser boundary. Native definitions can specify
  the latter directly. Other `APP_*` and Compose package fields retain their
  published meaning and are not renamed.
- Actual repository, gallery, dependency and container-image coordinates keep
  their publishers' namespaces, including `getumbrel`. Copyright, trademark and
  license notices retain their actual owners.

`TitanPro` / `isTitanHome` are internal detector names. Their hardware predicates
retain the actual OEM DMI manufacturer `Umbrel, Inc.`, product names `Umbrel Home`
and `Umbrel Pro`, and original model IDs. Renaming those physical identities would
break detection and could make fan/EC operations unsafe on unsupported hardware.

Authentication issues only Titan browser/app cookies and `titan_` credentials;
MCP credentials use `titanmcp_`. Vendor cookies are never accepted or migrated and
are stripped before upstream apps see requests. The app handoff endpoint is
`/titan_/api/v1/auth/handoff`, mDNS uses `_titan._tcp`, and the certificate download
is `/lan-ingress/titan-local-ca.crt`. Frontend and native-client callers must use
the same contracts.

OS/build sources must coordinate `titan.service`, `/opt/titand`, `.titan-project`,
`/data/titan-os`, `/dev/disk/by-titan-id`, the `titantitan` fresh-pool wrapping key,
and `titan-*` hook paths with this daemon. VM resources use the `titan-machines`
libvirt network, `titan-vm` bridge, `titan-machine-*` domains and `titan_machines`
firewall rules. These are fresh installation contracts, not an old OS migration.
