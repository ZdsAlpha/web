import { spawnSync } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const result = spawnSync('go', ['test', './...'], {
  cwd: path.resolve(import.meta.dirname, '..'),
  env: {
    ...process.env,
    GOCACHE: mkdtempSync(path.join(tmpdir(), 'arehman-web-gocache-')),
  },
  stdio: 'inherit',
});

process.exit(result.status ?? 1);
