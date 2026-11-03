#!/bin/bash
# May the draft common set <tag> of ra5on/TitanOS-Plugins be signed without
# the owner's approval? Fetches its index.json and the signed index of the
# common set the catalog names now, then asks scripts/plugin-guard.py.
# Exit 0: yes. Needs GH_TOKEN (drafts are not public), gh, jq and minisign.
set -euo pipefail
tag=${1:?usage: plugin-common-check.sh <draft tag>}
[[ $tag =~ ^common-[1-9][0-9]*$ ]] || { echo "not a common set: $tag"; exit 1; }
plugins=ra5on/TitanOS-Plugins
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/draft" "$work/published"

id=$(gh api "repos/$plugins/releases?per_page=100" --jq ".[] | select(.draft and .tag_name == \"$tag\") | .id")
[ -n "$id" ] || { echo "no draft $tag"; exit 1; }
asset=$(gh api "repos/$plugins/releases/$id/assets?per_page=100" --jq '.[] | select(.name == "index.json") | .id')
[ -n "$asset" ] || { echo "$tag has no index.json"; exit 1; }
gh api -H 'Accept: application/octet-stream' "repos/$plugins/releases/assets/$asset" > "$work/draft/index.json"

gh release download catalog --repo "$plugins" -p catalog.json -D "$work"
current=$(jq -r '.common.tag // empty' "$work/catalog.json")
[[ $current =~ ^common-[1-9][0-9]*$ ]] || { echo "the catalog names no common set"; exit 1; }
gh release download "$current" --repo "$plugins" -p index.json -p index.json.minisig -D "$work/published"

python3 "$(dirname "$0")/plugin-guard.py" common "$work/draft" "$work/published"
