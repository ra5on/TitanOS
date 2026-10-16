#!/usr/bin/env python3
"""Collect notices from the exact application dependencies and installed Debian packages.

The application pass runs before mkosi; the image pass adds Debian's copyright
files before squashfs is made. No network access or license-text substitution.
"""
import argparse
import json
import re
import shutil
import subprocess
from pathlib import Path
from urllib.parse import quote


REPO = Path(__file__).resolve().parent.parent
NOTICE = re.compile(r"^(licen[sc]e|copying|copyright|notice|ofl)([._-]|$)", re.I)


def run(*args, cwd=None):
    return subprocess.check_output(args, cwd=cwd, text=True).strip()


def notice_files(directory):
    return sorted(p for p in directory.rglob("*") if p.is_file()
                  and NOTICE.match(p.name)
                  and "node_modules" not in p.relative_to(directory).parts)


def copy_notices(directory, files, destination):
    copied = []
    for source in files:
        relative = source.relative_to(directory)
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)
        copied.append(str(target.relative_to(destination.parent.parent)))
    return copied


def write_inventory(destination, components):
    components.sort(key=lambda c: (c["ecosystem"], c["name"], c["version"]))
    (destination / "components.json").write_text(
        json.dumps(components, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    lines = ["TitanOS – Third-Party Components", "================================",
             "Generated from the built Go binary, installed npm tree and image package database.",
             "The npm list conservatively includes build tools as well as browser code and fonts.",
             "TitanOS's own code: FSL-1.1-ALv2; third-party components keep their own licenses.",
             "Full notices: /usr/share/titanos/licenses/ and the release's licenses.tar.gz.",
             "Debian firmware may be binary-only; source links identify its Debian distribution.", ""]
    for component in components:
        lines.extend([f"{component['ecosystem']}: {component['name']} {component['version']}",
                      f"  Source: {component['source']}",
                      f"  License: {component.get('license', 'see original notices')}"])
        lines.extend(f"  Notice: licenses/{p}" for p in component["notices"])
    (destination / "THIRD_PARTY_LICENSES").write_text("\n".join(lines) + "\n", encoding="utf-8")


def application(destination):
    destination.mkdir(parents=True, exist_ok=True)
    licenses = destination / "licenses"
    components = []
    binary = run("go", "version", "-m", str(REPO / "backend/bin/titand"))
    go_version = binary.splitlines()[0].rsplit(": ", 1)[1]
    goroot = Path(run("go", "env", "GOROOT"))
    if run("go", "env", "GOVERSION") != go_version:
        raise ValueError("Go toolchain must match the one that built titand")
    files = [goroot / "LICENSE", *notice_files(goroot / "src/vendor")]
    components.append(dict(ecosystem="Go", name="Go standard library", version=go_version,
                           source=f"https://go.dev/dl/{go_version}.src.tar.gz",
                           notices=copy_notices(goroot, files, licenses / "go/toolchain")))
    for line in binary.splitlines():
        fields = line.split()
        if not fields or fields[0] != "dep":
            continue
        name, version = fields[1:3]
        module = json.loads(run("go", "list", "-m", "-json", name, cwd=REPO / "backend"))
        if module.get("Replace") or module["Version"] != version:
            raise ValueError(f"dependency does not match binary: {name}")
        directory = Path(module["Dir"])
        files = notice_files(directory)
        if not files:
            raise ValueError(f"missing license text for {name}")
        components.append(dict(ecosystem="Go", name=name, version=version,
                               source=f"https://proxy.golang.org/{name}/@v/{version}.zip",
                               notices=copy_notices(directory, files, licenses / "go" / name.replace("/", "_"))))

    packages = json.loads((REPO / "web/package-lock.json").read_text())["packages"]
    for relative, locked in packages.items():
        if not relative:
            continue
        directory = REPO / "web" / relative
        if not directory.is_dir():
            if locked.get("optional"):
                continue  # optional binaries for other operating systems
            raise ValueError(f"npm dependency not installed: {relative}")
        package = json.loads((directory / "package.json").read_text())
        name, version = package["name"], package["version"]
        if version != locked["version"]:
            raise ValueError(f"npm dependency differs from lockfile: {relative}")
        files = notice_files(directory)
        if not files and (name, version) in {("is-reference", "3.0.3"), ("locate-character", "3.0.0")}:
            # These upstream releases contain only the MIT declaration in their
            # README, not a LICENSE file (also checked in the tagged source).
            # Preserve that declaration and author metadata verbatim.
            files = [directory / "README.md", directory / "package.json"]
        if not files:
            # A few npm tarballs omit their upstream LICENSE. Keep the verbatim
            # texts at pinned versions in the repository, with their provenance.
            directory = REPO / "scripts/licenses" / f"{name.replace('/', '_')}@{version}"
            files = notice_files(directory)
        if not files:
            raise ValueError(f"missing license text for {name}@{version}")
        notices = copy_notices(directory, files, licenses / "npm" / relative.replace("/", "_"))
        components.append(dict(ecosystem="npm", name=name, version=version,
                               license=package.get("license", "see original notices"),
                               source=locked["resolved"], notices=notices))
    # The adapted Immich composition retains its upstream license. Include
    # both the complete original license and our modified source in releases.
    directory = REPO / "backend/internal/catalog/builtin/immich"
    components.append(dict(ecosystem="Template", name="Immich composition", version="3.3.3-titanos",
                           license="AGPL-3.0-only",
                           source="https://github.com/immich-app/immich/releases/tag/v3.3.3",
                           notices=copy_notices(directory, [directory / "LICENSE", directory / "compose.yaml"],
                                                licenses / "templates/immich")))
    write_inventory(destination, components)


def debian_packages(status):
    for block in status.split("\n\n"):
        fields = dict(re.findall(r"^(Package|Version|Source|Status): (.*)$", block, re.M))
        if fields.get("Status") != "install ok installed":
            continue
        source, version = fields.get("Source", fields["Package"]), fields["Version"]
        match = re.fullmatch(r"(\S+) \((.+)\)", source)
        if match:
            source, version = match.groups()
        else:
            version = re.sub(r"\+b\d+$", "", version)
        yield fields["Package"], fields["Version"], source, version


def image(root):
    root = root.resolve()
    destination = root / "usr/share/titanos"
    components = json.loads((destination / "components.json").read_text())
    components = [c for c in components if c["ecosystem"] != "Debian"]
    for name, version, source, source_version in debian_packages((root / "var/lib/dpkg/status").read_text()):
        copyright_file = root / "usr/share/doc" / name / "copyright"
        if not copyright_file.resolve().is_relative_to(root) or not copyright_file.is_file():
            raise ValueError(f"missing in-image Debian copyright: {name}")
        target = destination / "licenses/debian" / name / "copyright"
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(copyright_file, target)
        components.append(dict(ecosystem="Debian", name=name, version=version,
                               source=f"https://snapshot.debian.org/package/{quote(source, safe='')}/{quote(source_version, safe='')}/",
                               notices=[f"debian/{name}/copyright"]))
    shutil.copytree(root / "usr/share/common-licenses", destination / "licenses/common-licenses", dirs_exist_ok=True)
    write_inventory(destination, components)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["application", "image"])
    parser.add_argument("path", type=Path)
    args = parser.parse_args()
    if args.mode == "application":
        application(args.path)
    else:
        image(args.path)
