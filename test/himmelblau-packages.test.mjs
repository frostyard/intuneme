import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

const script = fileURLToPath(new URL('../ubuntu-himmelblau/build_files/build', import.meta.url));

for (const scenario of [
  { name: 'accepts a pinned broker package', broker: 'himmelblau-broker', status: 73, apt: true },
  { name: 'rejects a different broker package before apt', broker: 'unrelated-package', status: 2, apt: false },
  { name: 'rejects a mismatched source pin before apt', broker: 'himmelblau-broker', mismatch: true, status: 2, apt: false },
]) {
  test(scenario.name, () => {
    const directory = mkdtempSync(join(tmpdir(), 'intuneme-package-test-'));
    try {
      const source = '1234567890abcdef1234567890abcdef12345678';
      const defaultRef = join(directory, 'default-ref');
      const packageRef = join(directory, 'package-ref');
      const aptLog = join(directory, 'apt-invocations');
      writeFileSync(defaultRef, `${source}\n`);
      writeFileSync(packageRef, `${scenario.mismatch ? '0'.repeat(40) : source}\n`);
      writeFileSync(join(directory, 'dpkg-deb'), `#!/bin/sh
case "$2" in
  */himmelblau.deb) echo himmelblau ;;
  */pam-himmelblau.deb) echo pam-himmelblau ;;
  */himmelblau-broker.deb) echo "$TEST_BROKER_PACKAGE" ;;
  *) exit 99 ;;
esac
`);
      writeFileSync(join(directory, 'apt'), `#!/bin/sh
printf '%s\\n' "$*" > "$TEST_APT_LOG"
exit 73
`);
      chmodSync(join(directory, 'dpkg-deb'), 0o755);
      chmodSync(join(directory, 'apt'), 0o755);
      const result = spawnSync('/bin/bash', [
        script, '--terminal-passkey',
        join(directory, 'himmelblau.deb'),
        join(directory, 'pam-himmelblau.deb'),
        join(directory, 'himmelblau-broker.deb'),
        defaultRef, packageRef, source,
      ], {
        env: {
          ...process.env,
          PATH: `${directory}:${process.env.PATH}`,
          TEST_BROKER_PACKAGE: scenario.broker,
          TEST_APT_LOG: aptLog,
        },
        encoding: 'utf8',
        timeout: 5000,
      });
      assert.ifError(result.error);
      assert.equal(result.status, scenario.status, result.stderr);
      if (scenario.apt) {
        assert.equal(readFileSync(aptLog, 'utf8'), 'update -y\n');
      } else {
        assert.throws(() => readFileSync(aptLog), { code: 'ENOENT' });
        assert.match(result.stderr, scenario.mismatch
          ? /does not match expected commit/
          : /broker override must be a himmelblau-broker Debian package/);
      }
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });
}
