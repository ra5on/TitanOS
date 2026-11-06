#!/usr/bin/env bash
# Integration test of the titanos-viewer image (docs/FERNANSICHT.md).
#
#   viewer/test-integration.sh [image]      (default image: titanos-viewer:dev)
#
# Starts two throwaway busybox web servers and the viewer on an internal
# Docker network with the container settings titand uses
# (internal/docker/viewer.go: read-only root, tmpfs /tmp 256m and /run,
# cap-drop ALL, no-new-privileges, UID 1000, 1.5 GiB, 512 pids, 512m shm),
# drives it with the Go test client in a busybox container on the same
# network and checks: video/audio/hello/resize/scale, input (form submit
# through a CGI), clipboard, downloads, uploads and the file dialog, the
# URL policy (second server never contacted), screenshots, no way out of
# the network, RAM, bye on SIGTERM, persistent cookies across a new
# container, and the CPU fallback of the GPU encoders.
# Everything it creates is named fern63-test-* and removed at the end.
# shellcheck disable=SC2015,SC2016 # "test && ok || bad" is intended; the CGI lines must not expand
set -euo pipefail

IMAGE=${1:-titanos-viewer:dev}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
NET=fern63-test-net
KEY=fern63-test-key-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')
FAIL=0
CONTAINERS="fern63-test-viewer fern63-test-viewer2 fern63-test-gpu fern63-test-web fern63-test-other fern63-test-client"

cleanup() {
	# shellcheck disable=SC2086
	docker rm -f $CONTAINERS >/dev/null 2>&1 || true
	docker network rm "$NET" >/dev/null 2>&1 || true
	rm -rf "$WORK"
}
[ "${KEEP:-0}" = 1 ] || trap cleanup EXIT # KEEP=1: leave everything for debugging

ok() { echo "PASS $*"; }
bad() { echo "FAIL $*"; FAIL=1; }
ip_of() { docker inspect -f "{{(index .NetworkSettings.Networks \"$NET\").IPAddress}}" "$1"; }

echo "== building test client"
(cd "$ROOT/backend" && CGO_ENABLED=0 go build -o "$WORK/testclient" ./cmd/titanos-viewer/testclient)

mkdir -p "$WORK/www/cgi-bin" "$WORK/www2" "$WORK/log" "$WORK/profile"
chmod 777 "$WORK/log" "$WORK/profile"
touch "$WORK/log/probe.txt"
chmod 666 "$WORK/log/probe.txt"
cat >"$WORK/www/index.html" <<'EOF'
<!doctype html><html><head><meta charset="utf-8"><title>viewer test</title>
<style>html,body{margin:0;height:100%;background:#d00000;color:#fff;font:20px sans-serif}
#c{position:fixed;right:0;bottom:0;width:20px;height:20px;background:#00d000}
#f{position:absolute;left:10px;top:200px;width:300px;height:60px}</style></head>
<body><form action="/cgi-bin/submit" method="get"><input id="q" name="q" autofocus style="font-size:24px;width:600px"></form>
<p><a href="http://fern63-test-other/">other</a> <a href="/file.txt" download>download</a></p>
<input type="file" id="f" onchange="fetch('/cgi-bin/submit?file='+encodeURIComponent(this.files[0].name))">
<div id="c"></div>
<script>
try{const ac=new AudioContext();const o=ac.createOscillator();o.frequency.value=440;o.connect(ac.destination);o.start()}catch(e){}
fetch('/cgi-bin/submit?probe=1');
setInterval(()=>{document.cookie='persist='+Date.now()+'; max-age=86400; path=/; SameSite=Lax'},1000);
</script></body></html>
EOF
printf '%s\n' '#!/bin/sh' \
	'case "$QUERY_STRING" in' \
	'probe=*) echo "cookie=$HTTP_COOKIE" >>/log/probe.txt ;;' \
	'file=*) echo "$QUERY_STRING" >>/log/file.txt ;;' \
	'*) echo "$QUERY_STRING" >>/log/query.txt ;;' \
	'esac' \
	'printf "Content-Type: text/html\r\n\r\n<html><body>submitted</body></html>\n"' >"$WORK/www/cgi-bin/submit"
chmod 755 "$WORK/www/cgi-bin/submit"
echo 'hello download' >"$WORK/www/file.txt"
echo '<html><body style="background:blue">OTHER</body></html>' >"$WORK/www2/index.html"

docker network create --internal "$NET" >/dev/null
docker run -d --name fern63-test-web --network "$NET" -v "$WORK/www:/www:ro" -v "$WORK/log:/log" busybox:1.37 httpd -f -vv -p 80 -h /www >/dev/null
docker run -d --name fern63-test-other --network "$NET" -v "$WORK/www2:/www:ro" busybox:1.37 httpd -f -vv -p 80 -h /www >/dev/null
WEBIP=$(ip_of fern63-test-web)

# viewer <name> [extra docker args...]: start with titand's settings
viewer() {
	local name=$1
	shift
	docker run -d --name "$name" --network "$NET" \
		--read-only --tmpfs /tmp:rw,nosuid,nodev,size=256m,mode=1777 --tmpfs /run:rw,nosuid,nodev,noexec,size=16m,mode=0755 \
		--cap-drop ALL --security-opt no-new-privileges --user 1000:1000 --ipc private \
		--memory 1536m --memory-swap 1536m --pids-limit 512 --shm-size 512m \
		-v "$WORK/profile:/profile" -e HOME=/profile \
		-e VIEWER_URL="http://$WEBIP:80" -e VIEWER_KEY="$KEY" -e VIEWER_AUDIO=1 "$@" \
		"$IMAGE" >/dev/null
	for _ in $(seq 50); do
		docker logs "$name" 2>&1 | grep -q 'ready on' && return 0
		sleep 0.2
	done
	bad "$name did not get ready"
	docker logs "$name" 2>&1 | tail -20
}

client() { docker run --rm --network "$NET" -v "$WORK/testclient:/testclient:ro" busybox:1.37 /testclient "$@"; }
client_wget() { docker run --rm --network "$NET" busybox:1.37 wget -q -T 3 -O /dev/null "$1"; }

viewer fern63-test-viewer
VIP=$(ip_of fern63-test-viewer)

# positive control: the second server is reachable and logs requests
client_wget http://fern63-test-other/ || bad "control: other server not reachable"
OTHER_BEFORE=$(docker logs fern63-test-other 2>&1 | grep -c 'url:' || true)
[ "$OTHER_BEFORE" -ge 1 ] && ok "control: other server logs requests" || bad "control: other server logs nothing"

echo "== stream test"
if ! client -addr "$VIP:7300" -key "$KEY" -audio | tee "$WORK/client.log"; then FAIL=1; fi

OTHER_AFTER=$(docker logs fern63-test-other 2>&1 | grep -c 'url:' || true)
[ "$OTHER_AFTER" -eq "$OTHER_BEFORE" ] && ok "policy: link to another origin blocked (no request)" || bad "policy: other server was contacted"
if grep -qxF 'q=abcz%2FA%C3%A4%C3%B6%E2%82%AC+Z%21' "$WORK/log/query.txt" 2>/dev/null; then
	ok "input: form submitted with the typed text"
else
	bad "input: typed text not submitted ($(cat "$WORK/log/query.txt" 2>/dev/null))"
fi
[ -f "$WORK/profile/Hochgeladen/ä.txt" ] && ok "upload: file in /profile/Hochgeladen" || bad "upload: file missing"
grep -qxF 'file=%C3%A4.txt' "$WORK/log/file.txt" 2>/dev/null && ok "file dialog: ä.txt picked from Hochgeladen" ||
	bad "file dialog: no file picked ($(cat "$WORK/log/file.txt" 2>/dev/null))"

shot() { # shot <w> <h>: screenshot of the top-left region
	docker exec -e GST_REGISTRY=/tmp/titanos-viewer/gst-registry.bin fern63-test-viewer gst-launch-1.0 -q ximagesrc display-name=:99 num-buffers=1 \
		startx=0 starty=0 endx=$(($1 - 1)) endy=$(($2 - 1)) ! videoconvert ! pngenc ! filesink location=/tmp/shot.png >/dev/null
	docker exec fern63-test-viewer cat /tmp/shot.png >"$WORK/shot.png" # docker cp cannot read tmpfs
}
px() { "$WORK/testclient" pngpixel "$WORK/shot.png" "$1" "$2"; }
green() { read -r r g b _ <<<"$(px "$1" "$2")"; [ "$g" -gt 150 ] && [ "$r" -lt 80 ] && ok "screenshot: green corner at $1,$2" || bad "screenshot: $1,$2 = $r $g $b, want green"; }
red() { read -r r g b _ <<<"$(px "$1" "$2")"; [ "$r" -gt 150 ] && [ "$g" -lt 80 ] && ok "screenshot: page at $1,$2" || bad "screenshot: $1,$2 = $r $g $b, want red"; }
black() { read -r r g b _ <<<"$(px "$1" "$2")"; [ "$r" -lt 30 ] && [ "$g" -lt 30 ] && [ "$b" -lt 30 ] && ok "screenshot: outside window at $1,$2" || bad "screenshot: $1,$2 = $r $g $b, want black"; }

echo "== screenshot (window resized to 1000x600)"
shot 1200 800
green 990 590
red 500 120
black 1010 300

echo "== network isolation"
for target in 1.1.1.1 "$(ip -4 route get 1.1.1.1 | sed -n 's/.* via \([0-9.]*\).*/\1/p')" "$(ip -4 route get 1.1.1.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')"; do
	[ -n "$target" ] || continue
	if client_wget "http://$target/" 2>/dev/null; then bad "egress: $target reachable"; else ok "egress: $target unreachable"; fi
done

echo "== scale 2 and RAM"
client -addr "$VIP:7300" -key "$KEY" -phase stream >"$WORK/stream.log" &
sleep 5
docker stats --no-stream --format 'RAM viewer container, 1000x600 scale 1 streaming: {{.MemUsage}} CPU {{.CPUPerc}}' fern63-test-viewer
wait $! || FAIL=1
cat "$WORK/stream.log"
docker stats --no-stream --format 'RAM viewer container, 1280x800 (640x400 scale 2) idle: {{.MemUsage}}' fern63-test-viewer
shot 1400 900
green 1270 790
black 1290 300

echo "== 3840x2160 (1920x1080 scale 2)"
client -addr "$VIP:7300" -key "$KEY" -phase big >"$WORK/big.log" &
sleep 9
docker stats --no-stream --format 'RAM viewer container, 3840x2160 streaming: {{.MemUsage}} CPU {{.CPUPerc}}' fern63-test-viewer
docker exec fern63-test-viewer df -h /tmp | tail -1 | awk '{print "tmpfs /tmp used: " $3 " of " $2}'
wait $! || FAIL=1
cat "$WORK/big.log"

echo "== SIGTERM with a client connected, then cookies in a new container"
client -addr "$VIP:7300" -key "$KEY" -phase hold >"$WORK/hold.log" &
HOLD=$!
sleep 4
STOP_MS=$(($(date +%s%N) / 1000000))
start=$(date +%s)
docker stop -t 10 fern63-test-viewer >/dev/null
echo "stopped after $(($(date +%s) - start)) s"
wait $HOLD || FAIL=1
cat "$WORK/hold.log"
docker logs fern63-test-viewer 2>&1 | tail -n 4
PROBES=$(wc -l <"$WORK/log/probe.txt")
viewer fern63-test-viewer2
for _ in $(seq 30); do
	[ "$(wc -l <"$WORK/log/probe.txt")" -gt "$PROBES" ] && break
	sleep 0.5
done
COOKIE=$(tail -n +"$((PROBES + 1))" "$WORK/log/probe.txt" | head -1 | sed -n 's/.*persist=\([0-9]*\).*/\1/p')
if [ -n "$COOKIE" ] && [ "$COOKIE" -gt $((STOP_MS - 5000)) ]; then
	ok "cookies: persistent cookie set $((STOP_MS - COOKIE)) ms before SIGTERM is there in a new container"
else
	bad "cookies: new container sent '${COOKIE:-none}' (stop at $STOP_MS)"
fi
docker rm -f fern63-test-viewer fern63-test-viewer2 >/dev/null

echo "== GPU encoders fall back to the CPU"
for gpu in vaapi nvenc; do
	extra=(-e "VIEWER_GPU=$gpu")
	if [ "$gpu" = vaapi ] && [ -e /dev/dri/renderD128 ]; then
		extra+=(--device /dev/dri/renderD128 --group-add "$(stat -c %g /dev/dri/renderD128)" -e VIEWER_GPU_DEVICE=/dev/dri/renderD128)
	fi
	viewer fern63-test-gpu "${extra[@]}"
	client -addr "$(ip_of fern63-test-gpu):7300" -key "$KEY" -phase stream -wantgpu cpu >"$WORK/gpu.log" || FAIL=1
	grep -E '^(PASS hello|PASS video|FAIL)' "$WORK/gpu.log" | sed "s/^/[$gpu] /"
	docker logs fern63-test-gpu 2>&1 | grep -E 'video:' | head -3 | sed "s/^/[$gpu] /"
	docker rm -f fern63-test-gpu >/dev/null
done

if [ "$FAIL" -ne 0 ] || grep -q '^FAIL' "$WORK"/*.log; then
	echo "RESULT: FAIL"
	exit 1
fi
echo "RESULT: PASS"
