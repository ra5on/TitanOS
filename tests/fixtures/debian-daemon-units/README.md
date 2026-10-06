These service and default files are unchanged test fixtures extracted from the
official Debian trixie amd64 binary packages. No daemon binary is bundled or
executed by these tests. `provenance.json` records the package download URLs,
archive SHA-256 hashes, original member names, ownership, modes and file hashes.

- Docker: `docker.io_26.1.5+dfsg1-9+deb13u1_amd64.deb`.
- libvirt: `libvirt-daemon_11.3.0-3+deb13u3_amd64.deb`.

The fixtures retain their upstream and Debian licenses independently of the
Titan license. Preserve the accompanying `docker-copyright` and
`libvirt-copyright` notices when sharing them. Docker copyright was extracted
from the same Docker archive. libvirt copyright was extracted from the matching
`libvirt-common_11.3.0-3+deb13u3_amd64.deb`, to which the daemon package's
documentation directory links. Standard license texts referenced by these
notices are also included: Apache-2.0, LGPL-2.1 and GPL-2.

Source and licensing references:

- https://sources.debian.org/src/docker.io/26.1.5%2Bdfsg1-9%2Bdeb13u1/
- https://sources.debian.org/src/libvirt/11.3.0-3%2Bdeb13u3/
- https://deb.debian.org/debian/pool/main/libv/libvirt/libvirt-common_11.3.0-3+deb13u3_amd64.deb
