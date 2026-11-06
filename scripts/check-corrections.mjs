#!/usr/bin/env node
// Append-only JSONL gate for .memory/corrections.jsonl (ADR-0018).
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, isAbsolute, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const scriptRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const usage = "Usage: node scripts/check-corrections.mjs [--root DIR] [--file PATH] [--base REF]";

let root = scriptRoot;
let file = ".memory/corrections.jsonl";
let base;
const args = process.argv.slice(2);
for (let i = 0; i < args.length; i++) {
  const arg = args[i];
  if (arg === "--root" || arg === "--file" || arg === "--base") {
    if (i + 1 >= args.length) {
      console.error(usage);
      process.exit(2);
    }
    const value = args[++i];
    if (!value || value.startsWith("--")) {
      console.error(usage);
      process.exit(2);
    }
    if (arg === "--root") root = resolve(value);
    if (arg === "--file") file = value;
    if (arg === "--base") base = value;
  } else {
    console.error(usage);
    process.exit(2);
  }
}

const absoluteFile = resolve(root, file);
// One path for both the file system and git: relative to the root, never
// outside it, so `git show REF:./PATH` reads the same file that is checked.
const relativeFile = relative(root, absoluteFile);
if (relativeFile === "" || relativeFile === ".." || relativeFile.startsWith(`..${sep}`) || isAbsolute(relativeFile)) {
  console.error(`--file must name a file inside --root: ${file}`);
  console.error(usage);
  process.exit(2);
}
file = relativeFile.split(sep).join("/");
const failures = [];

// Read the file at the base ref. Returns null only when the ref has no such
// path; every other git failure (bad ref, unreadable object, output limit)
// is fatal, so the append-only check can never pass without a comparison.
function readBase() {
  const git = (args, options = {}) =>
    execFileSync("git", ["-C", root, ...args], { stdio: "pipe", maxBuffer: 256 * 1024 * 1024, ...options });
  try {
    git(["rev-parse", "--verify", "--quiet", `${base}^{commit}`]);
  } catch {
    fatal(`base ref ${base} does not exist`);
  }
  let listed;
  try {
    listed = git(["ls-tree", "-z", "--name-only", `${base}`, "--", `./${file}`], { encoding: "utf8" });
  } catch (error) {
    fatal(`cannot list ${file} at ${base}: ${String(error.stderr || error.message).trim()}`);
  }
  if (!listed.split("\0").includes(file)) return null;
  try {
    return git(["show", `${base}:./${file}`], { encoding: "utf8" });
  } catch (error) {
    fatal(`cannot read ${file} at ${base}: ${String(error.stderr || error.message).trim()}`);
  }
}

function fatal(message) {
  console.error(`FAIL corrections: ${file}: ${message}`);
  console.log("FAIL corrections: 1 problem(s)");
  process.exit(1);
}

// Top-level member names of a JSON object text, in order, decoded. JSON.parse
// keeps only the last of duplicate names, so duplicates are found here.
function memberNames(text) {
  const names = [];
  let depth = 0;
  let expectName = false;
  for (let i = 0; i < text.length; i++) {
    const char = text[i];
    if (char === '"') {
      let j = i + 1;
      while (text[j] !== '"') j += text[j] === "\\" ? 2 : 1;
      if (depth === 1 && expectName) {
        names.push(JSON.parse(text.slice(i, j + 1)));
        expectName = false;
      }
      i = j;
    } else if (char === "{" || char === "[") {
      depth++;
      expectName = char === "{" && depth === 1;
    } else if (char === "}" || char === "]") {
      depth--;
    } else if (char === "," && depth === 1) {
      expectName = true;
    }
  }
  return names;
}

const baseText = base !== undefined ? readBase() : null;

function addFailure(line, message) {
  failures.push(line ? `${file}:${line}: ${message}` : `${file}: ${message}`);
}

function splitLines(text) {
  const lines = text.split("\n");
  if (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();
  return lines;
}

function plainObject(value) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  const proto = Object.getPrototypeOf(value);
  return proto === Object.prototype || proto === null;
}

function parseObject(text) {
  try {
    const value = JSON.parse(text);
    return plainObject(value) ? value : null;
  } catch {
    return null;
  }
}

if (!existsSync(absoluteFile)) {
  // A file that the base has and the head lost is a removal, not "no file".
  if (baseText !== null) fatal(`file removed (it exists at ${base})`);
  console.log("ok   corrections: no file");
  process.exit(0);
}

const text = readFileSync(absoluteFile, "utf8");
const lines = splitLines(text);
const required = ["date", "scope", "correction", "evidence", "promoted_to"];

for (let i = 0; i < lines.length; i++) {
  const line = lines[i];
  const number = i + 1;
  if (line.trim() === "") {
    addFailure(number, "blank line");
    continue;
  }

  let value;
  try {
    value = JSON.parse(line);
  } catch {
    addFailure(number, "invalid JSON");
    continue;
  }

  if (!plainObject(value)) {
    addFailure(number, "not a plain object");
    continue;
  }

  const names = memberNames(line);
  const duplicates = names.filter((name, index) => names.indexOf(name) !== index);
  if (duplicates.length > 0) {
    addFailure(number, `duplicate key(s): ${[...new Set(duplicates)].join(", ")}`);
  }

  const keys = Object.keys(value);
  const missing = required.filter((key) => !Object.hasOwn(value, key));
  const unknown = keys.filter((key) => !required.includes(key));
  if (missing.length > 0 || unknown.length > 0) {
    const parts = [];
    if (missing.length > 0) parts.push(`missing key(s): ${missing.join(", ")}`);
    if (unknown.length > 0) parts.push(`unknown key(s): ${unknown.join(", ")}`);
    addFailure(number, parts.join("; "));
  }

  for (const key of required) {
    if (Object.hasOwn(value, key) && typeof value[key] !== "string") {
      addFailure(number, `value for ${key} is not a string`);
    }
  }

  if (typeof value.date === "string") {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value.date)) {
      addFailure(number, `invalid date: ${value.date}`);
    } else {
      const [year, month, day] = value.date.split("-").map(Number);
      const date = new Date(Date.UTC(year, month - 1, day));
      if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) {
        addFailure(number, `invalid date: ${value.date}`);
      }
    }
  }

  for (const key of ["scope", "correction", "evidence"]) {
    if (typeof value[key] === "string" && value[key].trim() === "") {
      addFailure(number, `${key} is empty`);
    }
  }
}

if (baseText !== null) {
  const baseLines = splitLines(baseText);
  for (let i = 0; i < baseLines.length; i++) {
    const number = i + 1;
    if (i >= lines.length) {
      addFailure(number, "line removed");
      continue;
    }
    if (lines[i] === baseLines[i]) continue;

    const baseObject = parseObject(baseLines[i]);
    const currentObject = parseObject(lines[i]);
    const promotable = baseObject !== null && currentObject !== null
      && baseObject.date === currentObject.date
      && baseObject.scope === currentObject.scope
      && baseObject.correction === currentObject.correction
      && baseObject.evidence === currentObject.evidence
      && baseObject.promoted_to === ""
      && typeof currentObject.promoted_to === "string"
      && currentObject.promoted_to !== "";

    if (!promotable) {
      addFailure(number, "line changed (only an empty promoted_to may be filled)");
    }
  }
}

if (failures.length > 0) {
  for (const failure of failures) console.error(`FAIL corrections: ${failure}`);
  console.log(`FAIL corrections: ${failures.length} problem(s)`);
  process.exit(1);
}

console.log(`ok   corrections: ${lines.length} lines${base !== undefined ? `, append-only vs ${base}` : ""}`);
