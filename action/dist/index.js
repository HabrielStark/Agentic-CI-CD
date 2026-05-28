#!/usr/bin/env node
"use strict";
/**
 * ReproForge GitHub Action entry point.
 *
 * Pure TypeScript using only Node 20 built-in modules (no node_modules at
 * runtime, so no third-party supply-chain risk). Compiled output is checked
 * in at action/dist/index.js.
 */
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
const node_child_process_1 = require("node:child_process");
const node_fs_1 = require("node:fs");
const path = __importStar(require("node:path"));
const os = __importStar(require("node:os"));
const https = __importStar(require("node:https"));
function getInput(name, fallback = '') {
    const envName = 'INPUT_' + name.replace(/ /g, '_').toUpperCase();
    const v = process.env[envName];
    if (v === undefined || v === '')
        return fallback;
    return v.trim();
}
function setOutput(name, value) {
    const file = process.env.GITHUB_OUTPUT;
    if (!file) {
        process.stdout.write(`::set-output name=${name}::${value}\n`);
        return;
    }
    const delim = 'rfdelim_' + Math.random().toString(36).slice(2);
    const block = `${name}<<${delim}\n${value}\n${delim}\n`;
    (0, node_fs_1.writeFileSync)(file, block, { flag: 'a' });
}
function info(msg) {
    process.stdout.write(msg + '\n');
}
function warn(msg) {
    process.stdout.write('::warning::' + msg + '\n');
}
function fail(msg) {
    process.stdout.write('::error::' + msg + '\n');
    process.exit(1);
}
function getEventPayload() {
    const p = process.env.GITHUB_EVENT_PATH;
    if (!p || !(0, node_fs_1.existsSync)(p))
        return null;
    try {
        return JSON.parse((0, node_fs_1.readFileSync)(p, 'utf8'));
    }
    catch {
        return null;
    }
}
function detectRunId(input) {
    if (input)
        return input;
    const ev = getEventPayload();
    const wr = ev?.workflow_run;
    if (wr && typeof wr.id === 'number')
        return String(wr.id);
    if (process.env.GITHUB_RUN_ID)
        return process.env.GITHUB_RUN_ID;
    return '';
}
function detectRepo(input) {
    if (input)
        return input;
    if (process.env.GITHUB_REPOSITORY)
        return process.env.GITHUB_REPOSITORY;
    return '';
}
function whichGo() {
    const r = (0, node_child_process_1.spawnSync)('go', ['version']);
    if (r.status === 0)
        return 'go';
    return null;
}
function repoRoot() {
    return path.resolve(__dirname, '..', '..');
}
function buildReproforge() {
    const root = repoRoot();
    if (!whichGo())
        fail('Go toolchain not available on runner. Install it via actions/setup-go.');
    const out = path.join(os.tmpdir(), 'reproforge-bin');
    (0, node_fs_1.mkdirSync)(out, { recursive: true });
    const bin = path.join(out, 'reproforge');
    info(`Building reproforge from ${root} → ${bin}`);
    const r = (0, node_child_process_1.spawnSync)('go', ['build', '-o', bin, './cmd/reproforge'], {
        cwd: root,
        stdio: 'inherit',
        env: { ...process.env, CGO_ENABLED: '0' },
    });
    if (r.status !== 0)
        fail('go build failed');
    return bin;
}
function execCmd(cmd, args, opts = {}) {
    info(`+ ${cmd} ${args.map((a) => (a.includes(' ') ? `'${a}'` : a)).join(' ')}`);
    const r = (0, node_child_process_1.spawnSync)(cmd, args, {
        cwd: opts.cwd ?? process.cwd(),
        env: { ...process.env, ...(opts.env ?? {}) },
        stdio: ['ignore', 'inherit', 'inherit'],
    });
    if (r.status !== 0 && !opts.ignoreError) {
        fail(`${cmd} ${args.join(' ')} exited with ${r.status}`);
    }
    return r;
}
async function postPRComment(token, body) {
    const ev = getEventPayload();
    let prNumber = null;
    let owner = null;
    let repo = null;
    const repoEnv = (process.env.GITHUB_REPOSITORY ?? '').split('/');
    if (repoEnv.length === 2) {
        owner = repoEnv[0];
        repo = repoEnv[1];
    }
    const wr = ev?.workflow_run;
    const pr = ev?.pull_request;
    if (wr?.pull_requests && wr.pull_requests.length > 0) {
        prNumber = wr.pull_requests[0].number;
    }
    else if (pr?.number) {
        prNumber = pr.number;
    }
    if (!prNumber || !owner || !repo) {
        warn('No PR context available to comment on. Skipping.');
        return;
    }
    const url = `https://api.github.com/repos/${owner}/${repo}/issues/${prNumber}/comments`;
    const data = JSON.stringify({ body });
    const opts = {
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
    const status = await new Promise((resolve, reject) => {
        const req = https.request(url, opts, (res) => {
            const chunks = [];
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
    }
    else {
        warn(`PR comment failed (status ${status})`);
    }
}
async function main() {
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
    (0, node_fs_1.mkdirSync)(outputDir, { recursive: true });
    const reportPath = path.join(outputDir, 'report.md');
    const env = { ...process.env };
    if (token)
        env.GITHUB_TOKEN = token;
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
    if ((0, node_fs_1.existsSync)(capPath))
        setOutput('capsule-path', capPath);
    else
        setOutput('capsule-path', '');
    setOutput('report-path', reportPath);
    if ((0, node_fs_1.existsSync)(reportPath)) {
        const rep = (0, node_fs_1.readFileSync)(reportPath, 'utf8');
        const m = rep.match(/Diagnosis \| \*\*([A-Za-z ]+)\*\*/);
        if (m && m[1])
            setOutput('diagnosis-category', m[1]);
        const fp = rep.match(/Failure fingerprint \| `([^`]+)`/);
        if (fp && fp[1])
            setOutput('fingerprint', fp[1]);
    }
    if (commentPR && (0, node_fs_1.existsSync)(reportPath)) {
        const body = (0, node_fs_1.readFileSync)(reportPath, 'utf8');
        await postPRComment(token, body);
    }
    if (uploadCapsule && (0, node_fs_1.existsSync)(capPath)) {
        const size = (0, node_fs_1.statSync)(capPath).size;
        info(`capsule ready at ${capPath} (${size} bytes). Use actions/upload-artifact in your workflow to upload "${artifactName}".`);
    }
}
main().catch((err) => {
    fail(err?.message ?? String(err));
});
