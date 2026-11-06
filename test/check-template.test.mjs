import assert from "node:assert/strict";
import { after, test } from "node:test";
import {
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const checker = join(repo, "scripts", "check-template.mjs");
const temporary = [];

after(() => {
  for (const directory of temporary) rmSync(directory, { recursive: true, force: true });
});

function fixture() {
  const root = mkdtempSync(join(tmpdir(), "agentic-template-l3-"));
  temporary.push(root);
  mkdirSync(join(root, ".agents", "skills"), { recursive: true });
  mkdirSync(join(root, ".claude"), { recursive: true });
  mkdirSync(join(root, ".github", "workflows"), { recursive: true });
  mkdirSync(join(root, "docs", "adr"), { recursive: true });
  mkdirSync(join(root, ".memory"), { recursive: true });
  mkdirSync(join(root, "scripts", "lib"), { recursive: true });

  writeFileSync(join(root, "AGENTS.md"), "# Test instructions\n", "utf8");
  symlinkSync("AGENTS.md", join(root, "CLAUDE.md"));
  symlinkSync("AGENTS.md", join(root, "GEMINI.md"));
  symlinkSync("AGENTS.md", join(root, ".cursorrules"));
  symlinkSync("../AGENTS.md", join(root, ".github", "copilot-instructions.md"));
  symlinkSync("../.agents/skills", join(root, ".claude", "skills"));

  writeFileSync(
    join(root, "docs", "README.md"),
    "# Documentation\n\n- [0001](adr/0001-x.md)\n",
    "utf8",
  );
  writeFileSync(join(root, "docs", "adr", "0001-x.md"), "# One\n", "utf8");
  writeFileSync(join(root, "docs", "adr", "TEMPLATE.md"), "# Template\n", "utf8");

  writeFileSync(
    join(root, ".github", "risk-paths.json"),
    JSON.stringify({
      schema_version: 1,
      paths: [
        { glob: ".github/risk-paths.json", why: "governance: the high-risk path list" },
        { glob: ".github/workflows/**", why: "governance: CI and the caller workflow" },
      ],
    }, null, 2) + "\n",
    "utf8",
  );
  writeFileSync(join(root, ".github", "workflows", "risk-paths.yml"), "# caller stub\n", "utf8");

  writeFileSync(
    join(root, ".memory", "corrections.jsonl"),
    '{"date":"2026-11-01","scope":"test","correction":"keep this guard","evidence":"PR-1 review","promoted_to":""}\n',
    "utf8",
  );

  copyFileSync(join(repo, "scripts", "lib", "risk-paths.mjs"), join(root, "scripts", "lib", "risk-paths.mjs"));
  copyFileSync(join(repo, "scripts", "check-corrections.mjs"), join(root, "scripts", "check-corrections.mjs"));
  return root;
}

function run(root) {
  return spawnSync(process.execPath, [checker, "--root", root], {
    cwd: repo,
    encoding: "utf8",
  });
}

function output(result) {
  return `${result.stdout || ""}\n${result.stderr || ""}`;
}

function assertClean(result) {
  assert.equal(result.status, 0, output(result));
}

function assertFailure(result, check) {
  assert.notEqual(result.status, 0, output(result));
  assert.match(output(result), new RegExp(check));
}

test("clean fixture tree exits 0", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  assertClean(run(root));
});

test("broken CLAUDE.md link exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  unlinkSync(join(root, "CLAUDE.md"));
  symlinkSync("missing.md", join(root, "CLAUDE.md"));
  assertFailure(run(root), "agent-symlinks");
});

test("GEMINI.md regular file exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  unlinkSync(join(root, "GEMINI.md"));
  writeFileSync(join(root, "GEMINI.md"), "AGENTS.md\n", "utf8");
  assertFailure(run(root), "agent-symlinks");
});

test("copilot instruction link to another file exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  unlinkSync(join(root, ".github", "copilot-instructions.md"));
  writeFileSync(join(root, "other.md"), "other\n", "utf8");
  symlinkSync("../other.md", join(root, ".github", "copilot-instructions.md"));
  assertFailure(run(root), "agent-symlinks");
});

test("cursorrules missing exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  unlinkSync(join(root, ".cursorrules"));
  assertFailure(run(root), "agent-symlinks");
});

test(".cursorrules as a regular file exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  unlinkSync(join(root, ".cursorrules"));
  writeFileSync(join(root, ".cursorrules"), "AGENTS.md\n", "utf8");
  assertFailure(run(root), "agent-symlinks");
});

test("missing .claude/skills link exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  unlinkSync(join(root, ".claude", "skills"));
  assertFailure(run(root), "agent-symlinks");
});

test(".agents/skills regular file exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  rmSync(join(root, ".agents", "skills"), { recursive: true, force: true });
  writeFileSync(join(root, ".agents", "skills"), "not a directory\n", "utf8");
  assertFailure(run(root), "agent-symlinks");
});

test("unindexed doc exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "docs", "design"), { recursive: true });
  writeFileSync(join(root, "docs", "design", "x.md"), "# X\n", "utf8");
  assertFailure(run(root), "docs-index");
});

test("TEMPLATE.md that is not indexed exits 0", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "docs", "design"), { recursive: true });
  writeFileSync(join(root, "docs", "design", "TEMPLATE.md"), "# Template\n", "utf8");
  assertClean(run(root));
});

test("index link to missing doc exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(join(root, "docs", "README.md"), "# Docs\n\n[Missing](missing.md)\n", "utf8");
  assertFailure(run(root), "docs-index");
});

test("percent-encoded and angle-bracket inline link exits 0", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "docs", "design"), { recursive: true });
  writeFileSync(join(root, "docs", "design", "a b.md"), "# A\n", "utf8");
  writeFileSync(join(root, "docs", "README.md"), "# Docs\n\n- [0001](adr/0001-x.md)\n- [A](<design/a%20b.md>)\n", "utf8");
  assertClean(run(root));
});

test("reference definition index exits 0", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "docs", "design"), { recursive: true });
  writeFileSync(join(root, "docs", "design", "ref.md"), "# Ref\n", "utf8");
  writeFileSync(join(root, "docs", "README.md"), "# Docs\n\n- [0001](adr/0001-x.md)\n[Ref][ref]\n\n[ref]: <design/ref.md> \"Reference\"\n", "utf8");
  assertClean(run(root));
});

test("reference definition to missing file exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(join(root, "docs", "README.md"), "# Docs\n\n- [0001](adr/0001-x.md)\n[Missing][missing]\n\n  [missing]: design/missing.md \"Missing\"\n", "utf8");
  assertFailure(run(root), "docs-index");
});

test("bad percent escape exits 1 with docs-index", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(join(root, "docs", "README.md"), "# Docs\n\n- [0001](adr/0001-x.md)\n[Bad](design/%zz.md)\n", "utf8");
  assertFailure(run(root), "docs-index");
});

test("inline link without closing parenthesis exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "docs", "design"), { recursive: true });
  writeFileSync(join(root, "docs", "design", "x.md"), "# X\n", "utf8");
  writeFileSync(join(root, "docs", "README.md"), "# Docs\n\n- [0001](adr/0001-x.md)\n[Doc](design/x.md\n", "utf8");
  assertFailure(run(root), "docs-index");
});

test("inline link with title exits 0", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "docs", "design"), { recursive: true });
  writeFileSync(join(root, "docs", "design", "x.md"), "# X\n", "utf8");
  writeFileSync(join(root, "docs", "README.md"), "# Docs\n\n- [0001](adr/0001-x.md)\n[Doc](design/x.md \"Title\")\n", "utf8");
  assertClean(run(root));
});

test("unknown risk-path key exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(join(root, ".github", "risk-paths.json"), '{"schema_version":1,"paths":[],"unknown":true}\n', "utf8");
  assertFailure(run(root), "risk-paths");
});

test("invalid risk-path JSON exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(join(root, ".github", "risk-paths.json"), '{bad json', "utf8");
  assertFailure(run(root), "risk-paths");
});

test("missing risk-path list file exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  unlinkSync(join(root, ".github", "risk-paths.json"));
  assertFailure(run(root), "risk-paths");
});

test("risk-path list with no caller glob exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(join(root, ".github", "risk-paths.json"), JSON.stringify({
    schema_version: 1,
    paths: [{ glob: ".github/risk-paths.json", why: "governance: list" }],
  }) + "\n", "utf8");
  assertFailure(run(root), "risk-paths");
});

test("correction with a sixth field exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(join(root, ".memory", "corrections.jsonl"), '{"date":"2026-11-01","scope":"test","correction":"fix","evidence":"PR-1","promoted_to":"","extra":true}\n', "utf8");
  assertFailure(run(root), "corrections");
});

test("correction with bad date exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(join(root, ".memory", "corrections.jsonl"), '{"date":"2026-02-30","scope":"test","correction":"fix","evidence":"PR-1","promoted_to":""}\n', "utf8");
  assertFailure(run(root), "corrections");
});

test("correction with empty evidence exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(join(root, ".memory", "corrections.jsonl"), '{"date":"2026-11-01","scope":"test","correction":"fix","evidence":"","promoted_to":""}\n', "utf8");
  assertFailure(run(root), "corrections");
});

test("empty corrections file exits 0", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(join(root, ".memory", "corrections.jsonl"), "", "utf8");
  assertClean(run(root));
});

test("changed vendored risk-path library exits 1", (t) => {
  const root = fixture();
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const library = join(root, "scripts", "lib", "risk-paths.mjs");
  writeFileSync(library, readFileSync(library, "utf8") + " ", "utf8");
  assertFailure(run(root), "vendored");
});
