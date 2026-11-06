# titanos-viewer: Chromium on Xvfb with GStreamer capture for the TitanOS
# remote view (docs/FERNANSICHT.md). Built by scripts/build-viewer-image.sh,
# which places the statically built agent binary next to this file.
FROM debian:trixie-slim@sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2

# titand removes older images with this label once a newer one is loaded
# and verified (backend/internal/viewer/image.go); never other images.
LABEL titanos.viewer.image="1"
# The release that built the image (scripts/build-viewer-image.sh). A
# reused image keeps the label of the release that built it.
ARG VIEWER_RELEASE=dev
LABEL titanos.viewer.release="${VIEWER_RELEASE}"

ARG DEBIAN_FRONTEND=noninteractive
COPY dpkg-excludes /etc/dpkg/dpkg.cfg.d/titanos-viewer
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      ca-certificates tini \
      chromium xvfb xclip \
      pulseaudio \
      gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
      gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-x \
      gstreamer1.0-pulseaudio \
      intel-media-va-driver mesa-va-drivers \
      fonts-noto-core fonts-noto-color-emoji; \
    mkdir -p /usr/share/titanos-viewer; \
    dpkg-query -W -f='${Package} ${Version} ${source:Package} ${source:Version}\n' | sort > /usr/share/titanos-viewer/packages.txt; \
    apt-get clean; \
    rm -rf /var/lib/apt/lists/* /var/cache/apt/* /var/log/apt /var/log/dpkg.log; \
    fc-cache -f; \
    # Managed policies: a real root-owned directory; titand bind-mounts the
    # policy file read-only into it (proto.PolicyPath). Nothing else there.
    rm -rf /etc/chromium/policies/managed /etc/chromium/policies/recommended; \
    install -d -o root -g root -m 0755 /etc/chromium/policies /etc/chromium/policies/managed; \
    mkdir -p /etc/titanos-viewer /profile; \
    useradd --uid 1000 --user-group --no-create-home --home-dir /tmp/home --shell /usr/sbin/nologin viewer; \
    chown 1000:1000 /profile

COPY pulse.pa /etc/titanos-viewer/pulse.pa
COPY titanos-viewer /usr/local/bin/titanos-viewer

USER 1000:1000
ENV HOME=/tmp/home LANG=C.UTF-8
WORKDIR /tmp
EXPOSE 7300
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/titanos-viewer"]
