#!/usr/bin/env node

import { promises as fs } from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';
import { transform } from 'esbuild';

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '..');
const staticRoot = path.join(repoRoot, 'internal', 'web', 'static');
const globalScripts = new Set(['capture.ts', 'polyfill.ts']);

async function listTSFiles(dir) {
  const entries = await fs.readdir(dir, { withFileTypes: true });
  const out = [];
  for (const entry of entries) {
    if (entry.name === 'vendor') {
      continue;
    }
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...await listTSFiles(fullPath));
      continue;
    }
    if (entry.isFile() && fullPath.endsWith('.ts') && !fullPath.endsWith('.d.ts')) {
      out.push(fullPath);
    }
  }
  out.sort();
  return out;
}

async function buildFile(absPath) {
  const relPath = path.relative(staticRoot, absPath);
  const outPath = absPath.slice(0, -3) + '.js';
  const mapPath = `${outPath}.map`;
  const source = await fs.readFile(absPath, 'utf8');
  const format = globalScripts.has(path.basename(absPath)) ? 'iife' : 'esm';
  const result = await transform(source, {
    charset: 'utf8',
    format,
    loader: 'ts',
    sourcemap: 'external',
    sourcefile: relPath,
    target: 'es2022',
  });
  await fs.writeFile(outPath, `${result.code}\n//# sourceMappingURL=${path.basename(mapPath)}\n`);
  await fs.writeFile(mapPath, result.map);
}

const files = await listTSFiles(staticRoot);
if (files.length === 0) {
  throw new Error(`no TypeScript sources found under ${staticRoot}`);
}

await Promise.all(files.map(buildFile));
process.stdout.write(`built ${files.length} frontend modules\n`);
