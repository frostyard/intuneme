#!/usr/bin/env node
// Template gate (ADR-0003), run by `make verify`. Checks:
//   agent-symlinks  CLAUDE.md, GEMINI.md, .cursorrules and
//                   .github/copilot-instructions.md resolve to AGENTS.md;
//                   .claude/skills to .agents/skills (ADR-0002).
//   docs-index      every doc under docs/ (not TEMPLATE.md) is linked from
//                   docs/README.md, and every relative link there resolves.
//   risk-paths      .github/risk-paths.json passes core's validator, and rule C:
//                   the list and the caller are listed (frostyard/core
//                   docs/specs/risk-paths.md).
//   corrections     .memory/corrections.jsonl passes core's line rules
//                   (frostyard/core ADR-0018 schema), via scripts/check-corrections.mjs.
//   vendored        the 2 files copied from frostyard/core match their sha256 pins.
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  existsSync,
  lstatSync,
  readFileSync,
  readdirSync,
  realpathSync,
  statSync,
} from "node:fs";
import { dirname, join, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { checkRequiredPaths, parseStrictJson, validateRiskPaths } from "./lib/risk-paths.mjs";

const scriptDir = resolve(dirname(fileURLToPath(import.meta.url)));
const usage = "Usage: node scripts/check-template.mjs [--root DIR]";
let root = resolve(scriptDir, "..");
const args = process.argv.slice(2);
for (let i = 0; i < args.length; i++) {
  if (args[i] === "--root") {
    if (i + 1 >= args.length || !args[i + 1] || args[i + 1].startsWith("--")) {
      console.error(usage);
      process.exit(2);
    }
    root = resolve(args[++i]);
  } else {
    console.error(usage);
    process.exit(2);
  }
}

const failures = [];
const counts = { docs: 0, links: 0, riskEntries: 0, corrections: "" };

function failCheck(name, file, problem) {
  failures.push({ name, file, problem });
}

function sha256(file) {
  return createHash("sha256").update(readFileSync(file)).digest("hex");
}

// a. Canonical instruction and skill symlinks.
function checkAgentSymlinks() {
  const agents = join(root, "AGENTS.md");
  let agentsReal;
  try {
    const stat = lstatSync(agents);
    if (!stat.isFile() || stat.isSymbolicLink()) {
      failCheck("agent-symlinks", "AGENTS.md", "must be a regular file");
      return;
    }
    agentsReal = realpathSync(agents);
  } catch {
    failCheck("agent-symlinks", "AGENTS.md", "file does not exist");
    return;
  }

  const links = [
    ["CLAUDE.md", "AGENTS.md"],
    ["GEMINI.md", "AGENTS.md"],
    [".cursorrules", "AGENTS.md"],
    [".github/copilot-instructions.md", "AGENTS.md"],
  ];
  for (const [relative, _targetName] of links) {
    const file = join(root, relative);
    let stat;
    try {
      stat = lstatSync(file);
    } catch {
      failCheck("agent-symlinks", relative, "missing link");
      continue;
    }
    if (!stat.isSymbolicLink()) {
      failCheck("agent-symlinks", relative, "must be a symlink");
      continue;
    }
    try {
      if (realpathSync(file) !== agentsReal) {
        failCheck("agent-symlinks", relative, "symlink points to another file");
      }
    } catch {
      failCheck("agent-symlinks", relative, "broken link");
    }
  }

  const skillsLink = join(root, ".claude", "skills");
  const skillsDir = join(root, ".agents", "skills");
  try {
    if (!statSync(skillsDir).isDirectory()) {
      failCheck("agent-symlinks", ".agents/skills", "must be a directory");
    }
  } catch {
    failCheck("agent-symlinks", ".agents/skills", "directory does not exist");
  }

  let skillsStat;
  try {
    skillsStat = lstatSync(skillsLink);
  } catch {
    failCheck("agent-symlinks", ".claude/skills", "missing link");
  }
  if (skillsStat) {
    if (!skillsStat.isSymbolicLink()) {
      failCheck("agent-symlinks", ".claude/skills", "must be a symlink");
    } else {
      try {
        const skillReal = realpathSync(skillsDir);
        if (realpathSync(skillsLink) !== skillReal) {
          failCheck("agent-symlinks", ".claude/skills", "symlink points to another directory");
        }
      } catch {
        failCheck("agent-symlinks", ".claude/skills", "broken link");
      }
    }
  }
}

// b. Every Markdown document is indexed, and every docs index link resolves.
function markdownFiles(directory) {
  const found = [];
  if (!existsSync(directory)) return found;
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const file = join(directory, entry.name);
    if (entry.isDirectory()) found.push(...markdownFiles(file));
    else if (entry.isFile() && entry.name.endsWith(".md")) found.push(file);
  }
  return found;
}

function checkDocsIndex() {
  const docsDir = join(root, "docs");
  const indexFile = join(docsDir, "README.md");
  let indexText;
  if (!existsSync(indexFile)) {
    failCheck("docs-index", "docs/README.md", "file does not exist");
    return;
  }
  indexText = readFileSync(indexFile, "utf8");

  const relativeLinks = new Set();

  function addLinkTarget(rawTarget) {
    let target = rawTarget;
    if (/^[A-Za-z][A-Za-z0-9+.-]*:/u.test(target) || target.startsWith("#")) return;
    target = target.split("#", 1)[0];
    if (!target) return;
    try {
      target = decodeURIComponent(target);
    } catch {
      failCheck("docs-index", "docs/README.md", `bad percent escape in link target ${rawTarget}`);
      return;
    }
    const resolved = resolve(docsDir, target);
    if (!existsSync(resolved)) {
      failCheck("docs-index", "docs/README.md", `link to missing path ${target}`);
    }
    relativeLinks.add(resolved);
    counts.links++;
  }

  const inlineLinkPattern = /\]\(\s*(?:<([^>\n]+)>|([^()\s]+))(?:\s+(?:"[^"\n]*"|'[^'\n]*'|\([^()\n]*\)))?\s*\)/g;
  for (const match of indexText.matchAll(inlineLinkPattern)) {
    addLinkTarget(match[1] ?? match[2]);
  }

  const referenceDefinitionPattern = /^ {0,3}\[[^\]\n]+\]:\s*(?:<([^>\n]+)>|(\S+))/gm;
  for (const match of indexText.matchAll(referenceDefinitionPattern)) {
    addLinkTarget(match[1] ?? match[2]);
  }

  for (const file of markdownFiles(docsDir)) {
    if (file === indexFile) continue;
    if (file.endsWith(`${sep}TEMPLATE.md`) || file.endsWith("/TEMPLATE.md")) continue;
    counts.docs++;
    if (!relativeLinks.has(file)) {
      const relative = file.slice(root.length + 1).split(sep).join("/");
      failCheck("docs-index", relative, "document is not linked from docs/README.md");
    }
  }
}

// c. Strict risk-path list and Rule C required entries.
function checkRiskPaths() {
  const relative = ".github/risk-paths.json";
  const absolute = join(root, relative);
  if (!existsSync(absolute)) {
    failCheck("risk-paths", relative, "file does not exist");
    return;
  }

  let list;
  try {
    list = parseStrictJson(readFileSync(absolute, "utf8"));
  } catch (error) {
    failCheck("risk-paths", relative, error.message);
    return;
  }

  const validation = validateRiskPaths(list);
  if (!validation.ok) {
    for (const error of validation.errors) {
      failCheck("risk-paths", relative, error);
    }
  } else {
    const caller = ".github/workflows/risk-paths.yml";
    const callerAbsolute = join(root, caller);
    if (!existsSync(callerAbsolute)) {
      failCheck("risk-paths", caller, "caller file does not exist");
    }
    counts.riskEntries = list.paths.length;
    // Rule C (core docs/specs/risk-paths.md): each required path matches a
    // glob, and in a version 2 list each such glob is L1 or L2.
    for (const error of checkRequiredPaths(list, [relative, caller])) {
      failCheck("risk-paths", relative, error);
    }
  }
}

// d. Run the corrections child process from the same scripts folder.
function checkCorrections() {
  const script = join(scriptDir, "check-corrections.mjs");
  try {
    const output = execFileSync(process.execPath, [script, "--root", root], {
      cwd: root,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    }).trim();
    counts.corrections = output.replace(/^ok\s+corrections:\s*/, "");
  } catch (error) {
    const stdout = String(error.stdout || "").trim();
    const stderr = String(error.stderr || "").trim();
    const output = [stderr, stdout].filter(Boolean).join("\n");
    if (output) {
      for (const line of output.split("\n")) {
        if (line.startsWith("FAIL corrections:")) {
          failCheck("corrections", ".memory/corrections.jsonl", line.replace(/^FAIL corrections:\s*/, ""));
        } else {
          failCheck("corrections", ".memory/corrections.jsonl", line);
        }
      }
    } else {
      failCheck("corrections", ".memory/corrections.jsonl", `child process exited ${error.status ?? 1}`);
    }
  }
}

// e. Hash-pinned vendored core files. Update by copying core, never editing.
function checkVendored() {
  const vendored = [
    {
      path: "scripts/lib/risk-paths.mjs",
      source: "frostyard/core@c996ea3fb54bcf1a3c621113522213702a7673e2:scripts/lib/risk-paths.mjs",
      sha256: "6df0af009ff385467214f8df56aa5c5c92ac19ad26689b3344bb4d2654cdfa43",
    },
    {
      path: "scripts/check-corrections.mjs",
      source: "frostyard/core@1b02a129026329cef2b46d1fbd0465a863c1cd81:scripts/check-corrections.mjs",
      sha256: "1894791a1945289c681a9f109d147e50dc342840bf9774b8349a5d63597725d6",
    },
  ];
  for (const item of vendored) {
    const absolute = join(root, item.path);
    if (!existsSync(absolute) || !statSync(absolute).isFile()) {
      failCheck("vendored", item.path, `vendored from ${item.source}; copy it again from core and update the pin, never edit by hand`);
      continue;
    }
    if (sha256(absolute) !== item.sha256) {
      failCheck("vendored", item.path, `vendored from ${item.source}; copy it again from core and update the pin, never edit by hand`);
    }
  }
}

checkAgentSymlinks();
checkDocsIndex();
checkRiskPaths();
checkCorrections();
checkVendored();

if (failures.length > 0) {
  for (const failure of failures) {
    console.log(`FAIL ${failure.name}: ${failure.file}: ${failure.problem}`);
  }
  process.exit(1);
}

console.log("ok   agent-symlinks: 5 symlinks resolve to AGENTS.md and .agents/skills");
console.log(`ok   docs-index: ${counts.docs} docs indexed, ${counts.links} index links resolve`);
console.log(`ok   risk-paths: ${counts.riskEntries} entries, list and caller listed`);
console.log(`ok   corrections: ${counts.corrections}`);
console.log("ok   vendored: 2 pinned core copies match");
