"""The sole supported identity for fresh TitanOS installations and updates."""
import re

REPOSITORY = 'ra5on/TitanOS'
COMPATIBILITY = 'titan-rugix-amd64-v2'
VERSION = re.compile(r'(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)')


def validate_release(release):
    if not isinstance(release, dict):
        raise ValueError('Invalid TitanOS release metadata')
    version = release.get('version')
    if (not isinstance(version, str) or len(version) > 100 or not VERSION.fullmatch(version)
            or release.get('osVersion') != version or release.get('versionName') != f'TitanOS {version}'
            or release.get('stage') != 'stable' or release.get('architecture') != 'amd64'
            or release.get('systemCompatibility') != COMPATIBILITY):
        raise ValueError('Only numeric stable TitanOS AMD64 releases for the current disk layout are supported')
    if 'legacyUpdateBridgeTo' in release:
        raise ValueError('Transition releases are not supported')
    return version
