/**
 * ReproForge GitHub Action entry point.
 *
 * Pure TypeScript using only Node 20 built-in modules (no node_modules at
 * runtime, so no third-party supply-chain risk). Compiled output is checked
 * in at action/dist/index.js.
 */

import { spawnSync, SpawnSyncReturns } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync, statSync } from 'node:fs';
import * as path from 'node:path';
import * as os from 'node:os';
import * as https from 'node:https';

type StringMap = Record<string, string>;

function getInput(name: string, fallback = ''): string {
    const envName = 'INPUT_' + name.replace(/ /g, '_').toUpperCase();
    const v = process.env[envName];
    if (v === undefined || v === '') return fallback;
    return v.trim();
}

function setOutput(name: string, value: string): void {
    const file = process.env.GITHUB_OUTPUT;
    if (!file) {
        process.stdout.write(`::set-output name=${name}::${value}\n`);
        return;
    }
    const delim = 'rfdelim_' + Math.random().toString(36).slice(2);
    const block = `${name}<<${delim}\n${value}\n${delim}\n`;
    writeFileSync(file, block, { flag: 'a' });
}

function info(msg: string): void {
    process.stdout.write(msg + '\n');
}

function warn(msg: string): void {
    process.stdout.write('::warning::' + msg + '\n');
}

function fail(msg: string): never {
    process.stdout.write('::error::' + msg + '\n');
    process.exit(1);
}

function getEventPayload(): Record<string, unknown> | null {
    const p = process.env.GITHUB_EVENT_PATH;
    if (!p || !existsSync(p)) return null;
    try {
        return JSON.parse(readFileSync(p, 'utf8'));
    } catch {
        return null;
    }
}

function detectRunId(input: string): string {
    if (input) return input;
    const ev = getEventPayload();
    const wr = ev?.workflow_run as Record<string, unknown> | undefined;
    if (wr && typeof wr.id === 'number') return String(wr.id);
    if (process.env.GITHUB_RUN_ID) return process.env.GITHUB_RUN_ID;
    return '';
}

function detectRepo(input: string): string {
    if (input) return input;
    if (process.env.GITHUB_REPOSITORY) return process.env.GITHUB_REPOSITORY;
    return '';
}

function whichGo(): string | null {
    const r = spawnSync('go', ['version']);
    if (r.status === 0) return 'go';
    return null;
}

function repoRoot(): string {
    return path.resolve(__dirname, '..', '..');
}

function buildReproforge(): string {
    const root = repoRoot();
    if (!whichGo()) fail('Go toolchain not available on runner. Install it via actions/setup-go.');
    const out = path.join(os.tmpdir(), 'reproforge-bin');
    mkdirSync(out, { recursive: true });
    const bin = path.join(out, 'reproforge');
    info(`Building reproforge from ${root} → ${bin}`);
    const r = spawnSync('go', ['build', '-o', bin, './cmd/reproforge'], {
        cwd: root,
        stdio: 'inherit',
        env: { ...process.env, CGO_ENABLED: '0' },
    });
    if (r.status !== 0) fail('go build failed');
    return bin;
}

function execCmd(cmd: string, args: string[], opts: { cwd?: string; env?: StringMap; ignoreError?: boolean } = {}): SpawnSyncReturns<Buffer> {
    info(`+ ${cmd} ${args.map((a) => (a.includes(' ') ? `'${a}'` : a)).join(' ')}`);
    const r = spawnSync(cmd, args, {
        cwd: opts.cwd ?? process.cwd(),
        env: { ...process.env, ...(opts.env ?? {}) },
        stdio: ['ignore', 'inherit', 'inherit'],
    });
    if (r.status !== 0 && !opts.ignoreError) {
        fail(`${cmd} ${args.join(' ')} exited with ${r.status}`);
    }
    return r;
}

async function postPRComment(token: string, body: string): Promise<void> {
    const ev = getEventPayload();
    let prNumber: number | null = null;
    let owner: string | null = null;
    let repo: string | null = null;

    const repoEnv = (process.env.GITHUB_REPOSITORY ?? '').split('/');
    if (repoEnv.length === 2) {
        owner = repoEnv[0]!;
        repo = repoEnv[1]!;
    }

    const wr = ev?.workflow_run as
        | { pull_requests?: Array<{ number: number }> }
        | undefined;
    const pr = ev?.pull_request as { number?: number } | undefined;
    if (wr?.pull_requests && wr.pull_requests.length > 0) {
        prNumber = wr.pull_requests[0]!.number;
    } else if (pr?.number) {
        prNumber = pr.number;
    }
    if (!prNumber || !owner || !repo) {
        warn('No PR context available to comment on. Skipping.');
        return;
    }

    const url = `https://api.github.com/repos/${owner}/${repo}/issues/${prNumber}/comments`;
    const data = JSON.stringify({ body });
    const opts: https.RequestOptions = {
        method: 'POST',
        headers: {
            'authorization': 'Bearer ' + token,
            'content-type': 'application/json',
            'accept': 'application/vnd.github+json',
            'user-agent': 'reproforge-action/0.1',
            'content-length': Buffer.byteLength(data),
            'x-github-api-version': '2022-11-28',
        },
    };
    const status = await new Promise<number>((resolve, reject) => {
        const req = https.request(url, opts, (res) => {
            const chunks: Buffer[] = [];
            res.on('data', (c) => chunks.push(c));
            res.on('end', () => resolve(res.statusCode ?? 0));
            res.on('error', reject);
        });
        req.on('error', reject);
        req.write(data);
        req.end();
    });
    if (status >= 200 && status < 300) {
        info(`PR comment posted (status ${status})`);
    } else {
        warn(`PR comment failed (status ${status})`);
    }
}

async function main(): Promise<void> {
    if (process.argv.includes('--self-test')) {
        info('reproforge-action self-test ok');
        return;
    }

    const runId = detectRunId(getInput('run-id'));
    const repo = detectRepo(getInput('repository'));
    const token = getInput('github-token', process.env.GITHUB_TOKEN ?? '');
    const commentPR = getInput('comment-pr', 'false') === 'true';
    const uploadCapsule = getInput('upload-capsule', 'true') !== 'false';
    const artifactName = getInput('artifact-name', 'reproforge-capsule');
    const ai = getInput('ai', 'none');
    const outputDir = getInput('output-dir', 'reproforge-out');

    if (!runId || !repo) {
        fail('Missing run-id or repository. Provide inputs or run inside a workflow_run trigger.');
    }

    const bin = buildReproforge();

    mkdirSync(outputDir, { recursive: true });
    const reportPath = path.join(outputDir, 'report.md');

    const env: StringMap = { ...process.env } as StringMap;
    if (token) env.GITHUB_TOKEN = token;

    execCmd(bin, [
        '--out', outputDir,
        'from-run',
        '--run', runId,
        '--repo', repo,
        '--write-capsule',
        '--write-report', reportPath,
    ], { env });

    if (ai && ai !== 'none') {
        const capDir = path.join(outputDir, 'capsule');
        execCmd(bin, ['patch', capDir, '--ai', ai, '--json', path.join(outputDir, 'patch.json')], {
            env, ignoreError: true,
        });
    }

    const capPath = path.join(outputDir, `rf-${runId}.tar.zst`);
    if (existsSync(capPath)) setOutput('capsule-path', capPath);
    else setOutput('capsule-path', '');
    setOutput('report-path', reportPath);

    if (existsSync(reportPath)) {
        const rep = readFileSync(reportPath, 'utf8');
        const m = rep.match(/Diagnosis \| \*\*([A-Za-z ]+)\*\*/);
        if (m && m[1]) setOutput('diagnosis-category', m[1]);
        const fp = rep.match(/Failure fingerprint \| `([^`]+)`/);
        if (fp && fp[1]) setOutput('fingerprint', fp[1]);
    }

    if (commentPR && existsSync(reportPath)) {
        const body = readFileSync(reportPath, 'utf8');
        await postPRComment(token, body);
    }

    if (uploadCapsule && existsSync(capPath)) {
        const size = statSync(capPath).size;
        info(`capsule ready at ${capPath} (${size} bytes). Use actions/upload-artifact in your workflow to upload "${artifactName}".`);
    }
}

main().catch((err: unknown) => {
    fail((err as Error)?.message ?? String(err));
});
