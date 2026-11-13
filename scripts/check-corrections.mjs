#!/usr/bin/env node
// Corrections inbox gate (ADR-0018, ADR-0081): 1 file for each record in
// .memory/corrections/, and the append-only .memory/corrections.jsonl during
// the cut-over. A repository has 1 form or the other, never both. With
// --base the tree at a ref is compared with the working tree, so a record is
// never removed, renamed or changed, except that an empty promoted_to is
// filled once.
import { execFileSync } from "node:child_process";
import { existsSync, lstatSync, readFileSync, readdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const usage = "Usage: node scripts/check-corrections.mjs [--root DIR] [--base REF]";
// The old form (1 JSON object per line) and the folder form (1 file for each
// record), relative to the root: the same path for the file system and for
// git, so both sides of a --base comparison name the same records.
const OLD = ".memory/corrections.jsonl";
const DIR = ".memory/corrections";
const NAME = /^(\d{4}-\d{2}-\d{2})-([a-z0-9]+(?:-[a-z0-9]+)*)\.json$/;
const SLUG_MAX = 60;
const REQUIRED = ["date", "scope", "correction", "evidence", "promoted_to"];
const KEY_FIELDS = ["date", "scope", "correction", "evidence"];

let root = scriptRoot;
let base;
const args = process.argv.slice(2);
for (let i = 0; i < args.length; i++) {
  const arg = args[i];
  if (arg === "--root" || arg === "--base") {
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
    if (arg === "--base") base = value;
  } else {
    console.error(usage);
    process.exit(2);
  }
}

const failures = [];

function fail(path, message, line = null) {
  failures.push(line === null ? `${path}: ${message}` : `${path}:${line}: ${message}`);
}

// A base reference that does not resolve and a Git object that cannot be read
// are fatal, so the --base check never passes without a comparison.
function fatal(message, path = OLD) {
  console.error(`FAIL corrections: ${path}: ${message}`);
  console.log("FAIL corrections: 1 problem(s)");
  process.exit(1);
}

function git(gitArgs, options = {}) {
  return execFileSync("git", ["-C", root, ...gitArgs], { stdio: "pipe", maxBuffer: 256 * 1024 * 1024, ...options });
}

function gitMessage(error) {
  return String(error.stderr || error.message).trim();
}

// `ls-tree -z` output, 1 entry for each object: mode, type, object id, path
// (the full path from the repository root, also with -r).
function treeEntries(gitArgs) {
  const entries = [];
  for (const item of git(gitArgs, { encoding: "utf8" }).split("\0")) {
    if (!item) continue;
    const tab = item.indexOf("\t");
    const [mode, type, oid] = item.slice(0, tab).split(" ");
    entries.push({ mode, type, oid, name: item.slice(tab + 1) });
  }
  return entries;
}

const decoder = new TextDecoder("utf-8", { fatal: true });

// null when the bytes are not valid UTF-8: the canonical form is UTF-8, so
// such a file can never be a record.
function decodeBytes(bytes) {
  try {
    return decoder.decode(bytes);
  } catch {
    return null;
  }
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

function validDate(value) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const [year, month, day] = value.split("-").map(Number);
  const date = new Date(Date.UTC(year, month - 1, day));
  return date.getUTCFullYear() === year && date.getUTCMonth() === month - 1 && date.getUTCDate() === day;
}

// Every content rule of a record, for both forms: JSON, 1 plain object, no
// duplicate key, exactly the 5 string fields, a real date, no empty text.
function recordProblems(text) {
  const problems = [];
  let value;
  try {
    value = JSON.parse(text);
  } catch {
    return { problems: ["invalid JSON"], record: null };
  }
  if (!plainObject(value)) return { problems: ["not a plain object"], record: null };

  const names = memberNames(text);
  const duplicates = names.filter((name, index) => names.indexOf(name) !== index);
  if (duplicates.length > 0) problems.push(`duplicate key(s): ${[...new Set(duplicates)].join(", ")}`);

  const keys = Object.keys(value);
  const missing = REQUIRED.filter((key) => !Object.hasOwn(value, key));
  const unknown = keys.filter((key) => !REQUIRED.includes(key));
  if (missing.length > 0 || unknown.length > 0) {
    const parts = [];
    if (missing.length > 0) parts.push(`missing key(s): ${missing.join(", ")}`);
    if (unknown.length > 0) parts.push(`unknown key(s): ${unknown.join(", ")}`);
    problems.push(parts.join("; "));
  }

  for (const key of REQUIRED) {
    if (Object.hasOwn(value, key) && typeof value[key] !== "string") {
      problems.push(`value for ${key} is not a string`);
    }
  }

  if (typeof value.date === "string" && !validDate(value.date)) problems.push(`invalid date: ${value.date}`);

  for (const key of ["scope", "correction", "evidence"]) {
    if (typeof value[key] === "string" && value[key].trim() === "") problems.push(`${key} is empty`);
  }

  return { problems, record: value };
}

// The canonical bytes: exactly these 5 fields, in this order, 2-space indent,
// 1 final newline (ADR-0081). Compared as bytes, never as a decoded string.
function canonicalBytes(record) {
  const ordered = {};
  for (const key of REQUIRED) ordered[key] = record[key];
  return Buffer.from(`${JSON.stringify(ordered, null, 2)}\n`, "utf8");
}

function splitLines(text) {
  const lines = text.split("\n");
  if (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();
  return lines;
}

function compareText(a, b) {
  return a < b ? -1 : a > b ? 1 : 0;
}

// The tree at the base ref: { old: text|null, folder: Map<name, bytes|null>|null }.
// The old file is read as a Buffer first, so an object that Git cannot read is
// fatal and never looks like "no file".
function readBase() {
  try {
    git(["rev-parse", "--verify", "--quiet", `${base}^{commit}`]);
  } catch {
    fatal(`base ref ${base} does not exist`);
  }
  const state = { old: null, oldValidUtf8: true, folder: null };

  let entries;
  try {
    entries = treeEntries(["ls-tree", "-z", base, "--", `./${OLD}`]);
  } catch (error) {
    fatal(`cannot list ${OLD} at ${base}: ${gitMessage(error)}`);
  }
  const oldEntry = entries.find((entry) => entry.name === OLD);
  if (oldEntry) {
    if (oldEntry.type !== "blob") fatal(`cannot read ${OLD} at ${base}: not a file`);
    try {
      const bytes = git(["cat-file", "blob", oldEntry.oid]);
      // The move to the folder reads this text as records, so its bytes must
      // be valid UTF-8; a lossy decode would write U+FFFD into a record.
      state.oldValidUtf8 = decodeBytes(bytes) !== null;
      state.old = bytes.toString("utf8");
    } catch (error) {
      fatal(`cannot read ${OLD} at ${base}: ${gitMessage(error)}`);
    }
  }

  let listed;
  try {
    listed = treeEntries(["ls-tree", "-r", "-z", base, "--", `./${DIR}/`]);
  } catch (error) {
    fatal(`cannot list ${DIR} at ${base}: ${gitMessage(error)}`);
  }
  if (listed.length > 0) {
    state.folder = new Map();
    for (const entry of listed) {
      const name = entry.name.startsWith(`${DIR}/`) ? entry.name.slice(DIR.length + 1) : entry.name;
      if (entry.type !== "blob") {
        state.folder.set(name, null);
        continue;
      }
      try {
        state.folder.set(name, git(["cat-file", "blob", entry.oid]));
      } catch (error) {
        fatal(`cannot read ${DIR}/${name} at ${base}: ${gitMessage(error)}`);
      }
    }
  }
  return state;
}

// A symbolic link at .memory would move the records out of the path this gate
// reads, so it fails before either form is read.
function linkedMemory() {
  try {
    return lstatSync(resolve(root, ".memory")).isSymbolicLink();
  } catch {
    return false;
  }
}

function readOld() {
  const absolute = resolve(root, OLD);
  if (!existsSync(absolute)) return null;
  try {
    return readFileSync(absolute, "utf8");
  } catch (error) {
    fatal(`cannot read file: ${error.code ?? error.message}`);
  }
}

// 1 record file of the folder, kept as bytes for a --base comparison. The
// optional empty .gitkeep is checked by checkFolder, never here.
function checkEntry(name, bytes, folder) {
  const path = `${DIR}/${name}`;
  const match = NAME.exec(name);
  if (!match) {
    fail(path, "bad file name (want <date>-<slug>.json)");
    return;
  }
  if (match[2].length > SLUG_MAX) fail(path, `slug longer than ${SLUG_MAX} characters`);
  folder.records++;

  const text = decodeBytes(bytes);
  if (text === null) {
    fail(path, "invalid UTF-8");
    return;
  }
  const { problems, record } = recordProblems(text);
  for (const problem of problems) fail(path, problem);
  if (record === null) return;
  if (!REQUIRED.every((key) => typeof record[key] === "string")) return;
  if (match[1] !== record.date) fail(path, `name date ${match[1]} does not equal date ${record.date}`);
  if (!bytes.equals(canonicalBytes(record))) fail(path, "not in canonical form");
}

// The folder form in the working tree. "Present" follows git: a folder counts
// only when it holds at least 1 entry, so an empty folder is absent. files
// keeps the bytes of each record file, null when they cannot be read.
function checkFolder() {
  const folder = { present: false, records: 0, files: new Map() };
  const absolute = resolve(root, DIR);
  let stat;
  try {
    stat = lstatSync(absolute);
  } catch (error) {
    if (error.code === "ENOENT" || error.code === "ENOTDIR") return folder;
    fail(DIR, `cannot read: ${error.code ?? error.message}`);
    return folder;
  }
  if (stat.isSymbolicLink()) {
    fail(DIR, "symbolic link, not followed");
    return folder;
  }
  if (!stat.isDirectory()) {
    fail(DIR, "not a folder");
    return folder;
  }
  let names;
  try {
    names = readdirSync(absolute);
  } catch (error) {
    fail(DIR, `cannot read: ${error.code ?? error.message}`);
    return folder;
  }
  if (names.length === 0) return folder;
  folder.present = true;

  for (const name of names.sort(compareText)) {
    const path = resolve(absolute, name);
    let entryStat;
    try {
      entryStat = lstatSync(path);
    } catch (error) {
      fail(`${DIR}/${name}`, `cannot read: ${error.code ?? error.message}`);
      folder.files.set(name, null);
      continue;
    }
    if (entryStat.isSymbolicLink()) {
      fail(`${DIR}/${name}`, "symbolic link, not followed");
      folder.files.set(name, null);
      continue;
    }
    if (name === ".gitkeep") {
      if (!entryStat.isFile()) fail(`${DIR}/.gitkeep`, "not a regular file");
      else if (entryStat.size !== 0) fail(`${DIR}/.gitkeep`, ".gitkeep is not empty");
      continue;
    }
    if (!entryStat.isFile()) {
      fail(`${DIR}/${name}`, "not a regular file");
      folder.files.set(name, null);
      continue;
    }
    let bytes;
    try {
      bytes = readFileSync(path);
    } catch {
      fail(`${DIR}/${name}`, "cannot read file");
      folder.files.set(name, null);
      continue;
    }
    folder.files.set(name, bytes);
    checkEntry(name, bytes, folder);
  }
  return folder;
}

// The old form: 1 JSON object for each line, the v1 rules, unchanged.
function checkOldLines(text) {
  const lines = splitLines(text);
  for (let i = 0; i < lines.length; i++) {
    const number = i + 1;
    const line = lines[i];
    if (line.trim() === "") {
      fail(OLD, "blank line", number);
      continue;
    }
    for (const problem of recordProblems(line).problems) fail(OLD, problem, number);
  }
  return lines;
}

// Both forms parse, and the only difference is an empty promoted_to that is
// now filled: the 1 change a record may take after it is written.
function promotionOnly(baseBytes, headBytes) {
  if (baseBytes === null || headBytes === null) return false;
  const baseText = decodeBytes(baseBytes);
  const headText = decodeBytes(headBytes);
  if (baseText === null || headText === null) return false;
  const before = parseObject(baseText);
  const after = parseObject(headText);
  if (before === null || after === null) return false;
  if (!KEY_FIELDS.every((key) => before[key] === after[key])) return false;
  return before.promoted_to === "" && typeof after.promoted_to === "string" && after.promoted_to !== "";
}

// The move from the old file to the folder: line N of the old file is
// <date>-legacy-<NNNN>.json, with every field of the line kept.
function checkMove(baseText, folder, ref) {
  const baseLines = splitLines(baseText);
  const width = baseLines.length > 9999 ? 5 : 4;
  const expected = new Map();
  for (let i = 0; i < baseLines.length; i++) {
    const number = i + 1;
    const record = parseObject(baseLines[i]);
    if (record === null || typeof record.date !== "string") {
      fail(OLD, `line ${number} is not a record: cannot name its legacy- record`, null);
      continue;
    }
    expected.set(`${record.date}-legacy-${String(number).padStart(width, "0")}.json`, { number, record });
  }

  for (const [name, { number, record }] of expected) {
    const path = `${DIR}/${name}`;
    const bytes = folder.files.get(name);
    if (bytes === undefined) {
      fail(path, `missing legacy- record for line ${number} of ${OLD}`);
      continue;
    }
    if (bytes === null) continue;
    const text = decodeBytes(bytes);
    const head = text === null ? null : parseObject(text);
    if (head === null) continue;
    const same = REQUIRED.every((key) => head[key] === record[key]);
    const filled = KEY_FIELDS.every((key) => head[key] === record[key])
      && record.promoted_to === ""
      && typeof head.promoted_to === "string"
      && head.promoted_to !== "";
    if (!same && !filled) fail(path, `record differs from line ${number} of ${OLD}`);
  }

  // Any other head file with a "legacy-" slug is a legacy- record that no
  // line of the old file asked for.
  for (const name of folder.files.keys()) {
    if (expected.has(name)) continue;
    const match = NAME.exec(name);
    if (!match || !match[2].startsWith("legacy-")) continue;
    const number = /legacy-(\d+)$/.exec(match[2]);
    const where = number ? `line ${Number(number[1])} but ${OLD} has` : `no line in ${OLD}, which has`;
    fail(`${DIR}/${name}`, `legacy- record for ${where} ${baseLines.length} line(s) at ${ref}`);
  }
}

function checkBase(baseState, head) {
  const headFolder = head.folder.present;
  const headOld = head.old !== null;
  const baseFolder = baseState.folder;
  const baseOld = baseState.old;

  if (baseFolder !== null) {
    if (headFolder) {
      for (const [name, baseBytes] of baseFolder) {
        if (name === ".gitkeep") continue;
        const path = `${DIR}/${name}`;
        if (!head.folder.files.has(name)) {
          fail(path, `record removed (it exists at ${base})`);
          continue;
        }
        const headBytes = head.folder.files.get(name);
        if (headBytes === null || baseBytes === null) continue;
        if (headBytes.equals(baseBytes)) continue;
        if (!promotionOnly(baseBytes, headBytes)) fail(path, "record changed (only an empty promoted_to may be filled)");
      }
      for (const name of head.folder.files.keys()) {
        if (baseFolder.has(name)) continue;
        const slug = NAME.exec(name)?.[2];
        if (slug && slug.startsWith("legacy-")) {
          fail(`${DIR}/${name}`, "new legacy- record (legacy- is reserved for the move from .memory/corrections.jsonl)");
        }
      }
      return;
    }
    if (headOld) {
      fail(OLD, "is retired in this repository; add a file under .memory/corrections/");
      return;
    }
    fail(DIR, `removed (it exists at ${base})`);
    return;
  }

  if (baseOld === null) return;
  if (headOld) {
    // The v1 append-only rule: line i of the base is line i of the head.
    const baseLines = splitLines(baseOld);
    const lines = splitLines(head.old);
    for (let i = 0; i < baseLines.length; i++) {
      const number = i + 1;
      if (i >= lines.length) {
        fail(OLD, "line removed", number);
        continue;
      }
      if (lines[i] === baseLines[i]) continue;
      const before = parseObject(baseLines[i]);
      const after = parseObject(lines[i]);
      const promotable = before !== null && after !== null
        && KEY_FIELDS.every((key) => before[key] === after[key])
        && before.promoted_to === ""
        && typeof after.promoted_to === "string"
        && after.promoted_to !== "";
      if (!promotable) fail(OLD, "line changed (only an empty promoted_to may be filled)", number);
    }
    return;
  }
  if (headFolder) {
    if (!baseState.oldValidUtf8) {
      fail(OLD, `${OLD} at ${base} is not valid UTF-8`);
      return;
    }
    checkMove(baseOld, head.folder, base);
    return;
  }
  fail(OLD, `removed (it exists at ${base})`);
}

const baseState = base !== undefined ? readBase() : null;
const head = { old: null, folder: { present: false, records: 0, files: new Map() } };
if (linkedMemory()) {
  fail(".memory", "symbolic link, not followed");
} else {
  head.old = readOld();
  head.folder = checkFolder();
}
const both = head.old !== null && head.folder.present;

const records = head.folder.records;
let lines = 0;
if (both) {
  fail(".memory", "both .memory/corrections.jsonl and .memory/corrections/ exist; finish the migration");
} else if (head.old !== null) {
  lines = checkOldLines(head.old).length;
}

if (baseState !== null && !both) checkBase(baseState, head);

if (failures.length > 0) {
  for (const failure of failures) console.error(`FAIL corrections: ${failure}`);
  console.log(`FAIL corrections: ${failures.length} problem(s)`);
  process.exit(1);
}

const compare = base !== undefined ? `, append-only vs ${base}` : "";
if (head.folder.present) {
  console.log(`ok   corrections: ${records} records${compare}`);
} else if (head.old !== null) {
  console.log(`ok   corrections: ${lines} lines (legacy form: migrate to .memory/corrections/)${compare}`);
} else {
  console.log(`ok   corrections: no records${compare}`);
}
