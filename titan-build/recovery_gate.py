"""Fail-closed validation of the real, versioned UEFI recovery release gate."""
import json
from pathlib import Path

TEST_FILE = 'packages/titand/source/modules/system/recovery.vm.test.ts'
CHECKS = {
    'fresh-no-rollback-and-authentication',
    'signed-update-api-reboot-confirmed',
    'data-and-user-retained-after-update',
    'boot-menu-previous-real-keyboard-selection',
    'manual-recovery-health-commit-data-retained',
    'automatic-confirmed-boot-without-input',
    'rollback-api-reboot-confirmed-data-retained',
    'stale-selection-does-not-reboot',
    'failed-unconfirmed-update-automatic-fallback',
    'failed-slot-not-offered-and-data-retained',
}


def unique_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result: raise ValueError('Duplicate recovery report JSON key')
        result[key] = value
    return result


def read_report(path):
    if not path.is_file() or path.is_symlink() or path.stat().st_size > 4 * 1024 ** 2:
        raise ValueError('The real recovery test evidence is missing or invalid')
    return json.loads(path.read_text(), object_pairs_hook=unique_pairs)


def validate(directory, version, source_commit):
    directory = Path(directory)
    report = read_report(directory / 'recovery-smoke.json')
    if (not isinstance(report, dict) or report.get('success') is not True
            or any(type(report.get(key)) is not int or report[key] != 0
                   for key in ('numFailedTests', 'numPendingTests', 'numTodoTests', 'numFailedTestSuites', 'numPendingTestSuites'))
            or type(report.get('numPassedTests')) is not int or report['numPassedTests'] < 7
            or report.get('numTotalTests') != report['numPassedTests']
            or type(report.get('numTotalTestSuites')) is not int or report['numTotalTestSuites'] < 1
            or report.get('numPassedTestSuites') != report['numTotalTestSuites']):
        raise ValueError('Every real system recovery test must pass without skips or failures')
    results = report.get('testResults')
    if (not isinstance(results, list) or len(results) != 1 or not isinstance(results[0], dict)
            or results[0].get('status') != 'passed' or not isinstance(results[0].get('name'), str)
            or not (results[0]['name'] == TEST_FILE or results[0]['name'].endswith('/' + TEST_FILE))):
        raise ValueError('The recovery report must come from the real UEFI recovery VM test')
    assertions = results[0].get('assertionResults')
    if (not isinstance(assertions, list) or len(assertions) != report['numPassedTests']
            or any(not isinstance(item, dict) or item.get('status') != 'passed'
                   or not isinstance(item.get('fullName'), str) or not item['fullName'] for item in assertions)
            or len({item['fullName'] for item in assertions}) != len(assertions)):
        raise ValueError('Every distinct recovery VM assertion must have passed')
    evidence = read_report(directory / 'recovery-evidence.json')
    expected = {'status': 'passed', 'baselineVersion': '0.0.0', 'candidateVersion': version,
                'sourceCommit': source_commit, 'privateBaseline': True, 'legacyMigrationTested': False}
    if (not isinstance(evidence, dict)
            or any(type(evidence.get(key)) is not type(value) or evidence.get(key) != value for key, value in expected.items())
            or not isinstance(evidence.get('checks'), list) or any(not isinstance(check, str) for check in evidence['checks'])
            or set(evidence['checks']) != CHECKS or len(evidence['checks']) != len(CHECKS)):
        raise ValueError('The exact release needs complete real update, rollback, boot menu and fallback evidence')
    requests = evidence.get('httpsRequests')
    required = {f'/github.com/ra5on/TitanOS/releases/download/v{item}/{asset}'
                for item in ('0.0.0', version) for asset in ('SHA256SUMS', 'SHA256SUMS.sig', 'release.json', 'build-manifest.json')}
    required.add(f'/github.com/ra5on/TitanOS/releases/download/v{version}/titan-{version}.update')
    if not isinstance(requests, list) or any(not isinstance(item, str) for item in requests) or not required <= set(requests):
        raise ValueError('Both versioned releases must be signature-verified over the real private HTTPS transport')
    return {'status': 'passed', 'passedTests': report['numPassedTests'], 'report': 'recovery-smoke.json',
            'evidence': 'recovery-evidence.json', **{key: evidence[key] for key in expected if key != 'status'}}
