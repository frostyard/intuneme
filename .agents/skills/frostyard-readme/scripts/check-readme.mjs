#!/usr/bin/env node
// Read-only README checker: rules R1 to R12 of docs/specs/readme.md (ADR-0072).
// It writes nothing. Every check reads the given text and the repository tree.
//
// Usage: node check-readme.mjs [--ref <ref>] [<dir>]
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

export const KINDS = {
  org: { required: ["What is here", "Use"], optional: [] },
  image: { required: ["Images", "Install"], optional: [] },
  packaging: {
    required: ["What it publishes", "Install"],
    optional: ["Add a package"],
  },
  app: { required: ["What it does", "Install"], optional: ["Remove"] },
  extension: {
    required: ["What it does", "Install"],
    optional: ["Settings", "Remove"],
  },
  tool: { required: ["What it does", "Install", "Use"], optional: [] },
  website: { required: ["What the site covers", "Deploy"], optional: [] },
  service: {
    required: ["What it does", "Deploy"],
    optional: ["Interface"],
  },
  library: { required: ["What it does", "Install", "Use"], optional: [] },
  infrastructure: { required: ["What is here", "Use"], optional: [] },
};

export const COMMON_OPTIONAL = [
  "Requirements",
  "How it works",
  "Problem records",
  "Privacy",
  "Limits",
  "Credits",
];

export const STATUS_VALUES = [
  "pre-release",
  "stable",
  "in production",
  "maintenance",
  "deprecated",
];

const MARKER = /^<!--\s*readme-kind:\s*([a-z-]+)\s*-->$/;
// CommonMark allows up to 3 leading spaces before a heading.
const HEADING = /^ {0,3}(#{1,6})\s+(.*)$/;
const STATUS_PREFIX = "**Status:**";
const DOC_PREFIX = "**Documentation:**";
const TAGLINE = /^\*\*[^*]+\*\*$/;
const LINK = /\[[^\]]*\]\(([^)\s]+)\)/g;
const INLINE_LINK = /\[[^\]]*\]\(\s*<?([^)\s>]+)>?\s*\)/g;
const AUTOLINK = /<(https?:\/\/[^>\s]+)>/g;
const BADGE_SOURCE = String.raw`\[!\[[^\]]*\]\(([^)\s]+)\)\]\(([^)\s]+)\)`;
const PRIVATE_V4 =
  /(?<![\d.])(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|100\.(?:6[4-9]|[7-9]\d|1[01]\d|12[0-7])\.\d{1,3}\.\d{1,3})(?!\.?\d)/g;
const ISO_DATE = /\b\d{4}-\d{2}-\d{2}\b/g;
const EMOJI = /\p{Extended_Pictographic}/gu;
const EMOJI_ALLOWED = new Set([0x00a9, 0x00ae, 0x2122]);

const LAST_TWO = ["Develop", "Where to go next"];

/** Count words the way `wc -w` does. */
function countWords(text) {
  return text.split(/\s+/).filter(Boolean).length;
}

function lineAt(text, index) {
  let line = 1;
  for (let i = 0; i < index && i < text.length; i += 1) {
    if (text[i] === "\n") line += 1;
  }
  return line;
}

/**
 * Split the text into lines marked as fenced-code content or not, and collect
 * the headings outside fenced code and the top block (lines before the first
 * level-2 heading).
 */
function analyze(text) {
  const body = text.replace(/\r\n?/g, "\n");
  const rawLines = body.split("\n");
  const lines = [];
  let fence = null;
  for (let i = 0; i < rawLines.length; i += 1) {
    const line = rawLines[i];
    const trimmed = line.trim();
    const fenceStart = /^(`{3,}|~{3,})/.exec(trimmed);
    const entry = { number: i + 1, text: line, inFence: false, isFence: false };
    if (fence !== null) {
      if (fenceStart && trimmed.startsWith(fence)) {
        entry.isFence = true;
        fence = null;
      } else {
        entry.inFence = true;
      }
    } else if (fenceStart) {
      entry.isFence = true;
      fence = fenceStart[1];
    }
    lines.push(entry);
  }

  const content = lines.filter((l) => !l.inFence && !l.isFence);
  const headings = [];
  for (const line of content) {
    const match = HEADING.exec(line.text);
    if (!match) continue;
    headings.push({
      number: line.number,
      level: match[1].length,
      text: match[2].trim(),
      line,
    });
  }

  const firstH2 = headings.find((h) => h.level === 2) ?? null;
  const top = firstH2
    ? lines.filter((l) => l.number < firstH2.number)
    : lines.slice();
  return { text: body, lines, content, headings, top, firstH2 };
}

function markerKind(lines) {
  const found = [];
  for (const line of lines) {
    if (line.inFence || line.isFence) continue;
    const match = MARKER.exec(line.text.trim());
    if (match) found.push({ number: line.number, kind: match[1] });
  }
  return found;
}

/** The valid kind of the README, or null. */
export function readKind(text) {
  const { lines } = analyze(text);
  const found = markerKind(lines);
  if (found.length !== 1) return null;
  return Object.hasOwn(KINDS, found[0].kind) ? found[0].kind : null;
}

const isComment = (line) => line.text.trim().startsWith("<!--");
const isImage = (line) =>
  /<img/i.test(line.text) || line.text.trim().startsWith("![");
const isBadgeLine = (line) => line.text.includes("[![");
const isStatusLine = (line) => line.text.trim().startsWith(STATUS_PREFIX);
const isDocLine = (line) => line.text.trim().startsWith(DOC_PREFIX);
const isTagline = (line) => TAGLINE.test(line.text.trim());
// Fenced code content and the fence lines themselves are not prose.
const isContent = (line) => !line.inFence && !line.isFence;

/** Every link destination in one line, from [text](target) and from <autolink>. */
function linkTargets(text) {
  const targets = [];
  for (const match of text.matchAll(INLINE_LINK)) targets.push(match[1]);
  for (const match of text.matchAll(AUTOLINK)) targets.push(match[1]);
  return targets;
}

/** R5: an https link to pkg.go.dev. */
function isPkgGoDev(target) {
  try {
    const url = new URL(target);
    return url.protocol === "https:" && url.hostname.toLowerCase() === "pkg.go.dev";
  } catch {
    return false;
  }
}

/** R6: a badge image comes from 1 of the allowed sources. */
function badgeImageAllowed(image) {
  let url;
  try {
    url = new URL(image);
  } catch {
    return false;
  }
  const host = url.hostname.toLowerCase();
  const target = url.pathname;
  if (host === "github.com") {
    return /\/actions\/workflows\/[^/]+\/badge\.svg/.test(target);
  }
  if (host === "api.scorecard.dev" || host === "codecov.io") return true;
  if (host === "pkg.go.dev") return target.startsWith("/badge/");
  if (host === "img.shields.io") return target.startsWith("/github/v/release/");
  return false;
}

/**
 * Check one README text against rules R1 to R12.
 * @param {string} text the README text
 * @param {(path: string) => boolean} has tells whether the repository tree has a path
 * @returns {{rule: string, message: string}[]} findings; empty means it passes
 */
export function checkReadme(text, has = () => false) {
  const doc = analyze(text);
  const { lines, content, headings, top } = doc;
  const findings = [];
  const fail = (rule, message) => findings.push({ rule, message });

  // R1 — kind marker.
  const markers = markerKind(lines);
  let kind = null;
  if (markers.length !== 1) {
    const where = markers.length
      ? markers.map((m) => `line ${m.number}`).join(", ")
      : "none";
    fail(
      "R1",
      `expected exactly 1 "<!-- readme-kind: <kind> -->" line, found ${markers.length} (${where})`,
    );
  } else if (!Object.hasOwn(KINDS, markers[0].kind)) {
    fail(
      "R1",
      `unknown kind "${markers[0].kind}" at line ${markers[0].number}; use 1 of ${Object.keys(KINDS).join(", ")}`,
    );
  } else {
    kind = markers[0].kind;
  }

  // R2 — one level-1 title, and it comes first.
  const h1 = headings.filter((h) => h.level === 1);
  if (headings.length === 0) {
    fail("R2", "no heading: the README needs 1 level-1 title");
  } else {
    if (headings[0].level !== 1) {
      fail(
        "R2",
        `the first heading "${headings[0].text}" at line ${headings[0].number} is level ${headings[0].level}, not a level-1 title`,
      );
    }
    if (h1.length !== 1) {
      const where = h1.length
        ? h1.map((h) => `line ${h.number}`).join(", ")
        : "none";
      fail(
        "R2",
        `expected exactly 1 level-1 heading, found ${h1.length} (${where})`,
      );
    }
  }

  // R3 — the top block: a lead line before the status line, at most 130 words.
  const statusInTop = top.filter((line) => isContent(line) && isStatusLine(line));
  const statusNumber = statusInTop.length ? statusInTop[0].number : Infinity;
  const leadLines = top.filter(
    (line) =>
      !line.inFence &&
      !line.isFence &&
      line.text.trim() !== "" &&
      !isComment(line) &&
      !isImage(line) &&
      !isBadgeLine(line) &&
      !HEADING.test(line.text) &&
      !isStatusLine(line) &&
      !isDocLine(line) &&
      !isTagline(line),
  );
  if (!leadLines.some((line) => line.number < statusNumber)) {
    fail(
      "R3",
      "the top block has no lead paragraph before the status line",
    );
  }
  const topWords = top
    .filter(
      (line) =>
        !isComment(line) &&
        !isImage(line) &&
        !isBadgeLine(line) &&
        !HEADING.test(line.text),
    )
    .reduce((sum, line) => sum + countWords(line.text), 0);
  if (topWords > 130) {
    fail(
      "R3",
      `the top block has ${topWords} words (at most 130), without the marker, images, badges and headings`,
    );
  }

  // R4 — one status line with a known value, at most 40 words.
  if (statusInTop.length !== 1) {
    const where = statusInTop.length
      ? statusInTop.map((l) => `line ${l.number}`).join(", ")
      : "none";
    fail(
      "R4",
      `expected exactly 1 "${STATUS_PREFIX}" line in the top block, found ${statusInTop.length} (${where})`,
    );
  } else {
    const line = statusInTop[0];
    const rest = line.text
      .trim()
      .slice(STATUS_PREFIX.length)
      .trim()
      .toLowerCase();
    const value = STATUS_VALUES.find(
      (candidate) =>
        rest.startsWith(candidate) &&
        (rest.length === candidate.length ||
          /^[.,;:\s-]/.test(rest.slice(candidate.length))),
    );
    if (!value) {
      fail(
        "R4",
        `line ${line.number}: the status does not start with 1 of ${STATUS_VALUES.join(", ")}`,
      );
    }
    const words = countWords(line.text);
    if (words > 40) {
      fail("R4", `line ${line.number}: the status line has ${words} words (at most 40)`);
    }
  }

  // R5 — one documentation line with a link.
  const docInTop = top.filter((line) => isContent(line) && isDocLine(line));
  if (docInTop.length !== 1) {
    const where = docInTop.length
      ? docInTop.map((l) => `line ${l.number}`).join(", ")
      : "none";
    fail(
      "R5",
      `expected exactly 1 "${DOC_PREFIX}" line in the top block, found ${docInTop.length} (${where})`,
    );
  } else {
    const line = docInTop[0];
    const rest = line.text.trim().slice(DOC_PREFIX.length);
    const hasLink =
      /\[[^\]]*\]\([^)\s]+\)/.test(rest) || /<https?:\/\/[^>]+>/.test(rest);
    if (!hasLink) {
      fail("R5", `line ${line.number}: the documentation line has no link`);
    }
    if (kind === "library") {
      const targets = linkTargets(rest);
      if (!targets.some((target) => isPkgGoDev(target))) {
        fail(
          "R5",
          `line ${line.number}: a library links https://pkg.go.dev/ on the documentation line`,
        );
      }
    }
  }

  // R6 — at most 3 badges, in the top block, from the allowed sources.
  const badges = [];
  for (const line of content) {
    let at = line.text.indexOf("[![");
    while (at !== -1) {
      const pattern = new RegExp(BADGE_SOURCE, "y");
      pattern.lastIndex = at;
      const match = pattern.exec(line.text);
      if (match) {
        badges.push({ line, image: match[1], link: match[2] });
      } else {
        fail(
          "R6",
          `line ${line.number}: "[![" is not a full [![alt](image)](link) badge`,
        );
      }
      at = line.text.indexOf("[![", at + 3);
    }
  }
  if (badges.length > 3) {
    fail("R6", `there are ${badges.length} badges (at most 3)`);
  }
  for (const badge of badges) {
    if (!top.some((line) => line.number === badge.line.number)) {
      fail("R6", `line ${badge.line.number}: the badge is outside the top block`);
    }
    if (!badgeImageAllowed(badge.image)) {
      fail(
        "R6",
        `line ${badge.line.number}: the badge image ${badge.image} is not an allowed source`,
      );
    }
  }

  // R7 — the level-2 headings match the kind's sections table.
  const h2 = headings.filter((h) => h.level === 2);
  if (kind) {
    const { required, optional } = KINDS[kind];
    const names = [...required, ...optional, ...COMMON_OPTIONAL, ...LAST_TWO];
    const matched = h2.map((heading) => {
      const lower = heading.text.toLowerCase();
      let best = null;
      for (const name of names) {
        if (
          lower.startsWith(name.toLowerCase()) &&
          (best === null || name.length > best.length)
        ) {
          best = name;
        }
      }
      return { heading, name: best };
    });

    for (const entry of matched) {
      if (!entry.name) {
        fail(
          "R7",
          `line ${entry.heading.number}: the heading "${entry.heading.text}" matches no section name for a ${kind}`,
        );
      }
    }

    const positions = (name) =>
      matched.reduce((acc, entry, index) => {
        if (entry.name === name) acc.push(index);
        return acc;
      }, []);
    const developAt = positions("Develop");
    const nextAt = positions("Where to go next");
    if (developAt.length !== 1 || developAt[0] !== matched.length - 2) {
      fail(
        "R7",
        `"Develop" must be the second-to-last section and appear once, found ${developAt.length}${developAt.length ? ` at position ${developAt.map((i) => i + 1).join(", ")}` : ""}`,
      );
    }
    if (nextAt.length !== 1 || nextAt[0] !== matched.length - 1) {
      fail(
        "R7",
        `"Where to go next" must be the last section and appear once, found ${nextAt.length}${nextAt.length ? ` at position ${nextAt.map((i) => i + 1).join(", ")}` : ""}`,
      );
    }

    const seenRequired = matched
      .map((entry) => entry.name)
      .filter((name) => name !== null && required.includes(name));
    if (seenRequired.join("|") !== required.join("|")) {
      fail(
        "R7",
        `the required sections for a ${kind} are ${required.map((n) => `"${n}"`).join(", ")} in that order, each once; found ${seenRequired.length ? seenRequired.map((n) => `"${n}"`).join(", ") : "none"}`,
      );
    }
  }

  // R8 — no heading deeper than level 2.
  for (const heading of headings) {
    if (heading.level >= 3) {
      fail(
        "R8",
        `line ${heading.number}: the heading "${heading.text}" is level ${heading.level} (at most level 2)`,
      );
    }
  }

  // R9 — the Develop section has a fenced make line.
  const developHeading = h2.find((h) =>
    h.text.toLowerCase().startsWith("develop"),
  );
  if (!developHeading) {
    fail("R9", "there is no Develop section");
  } else {
    const nextH2 = h2.find((h) => h.number > developHeading.number);
    const end = nextH2 ? nextH2.number : Infinity;
    const body = lines.filter(
      (line) => line.number > developHeading.number && line.number < end,
    );
    const makeLine = body.some(
      (line) => line.inFence && /(^|\s)make (ci|check|verify)\b/.test(line.text),
    );
    if (!makeLine) {
      fail(
        "R9",
        `the Develop section at line ${developHeading.number} has no fenced code line with "make ci", "make check" or "make verify"`,
      );
    }
  }

  // R10 — Where to go next links the repository files the tree has.
  const nextHeading = h2.find((h) =>
    h.text.toLowerCase().startsWith("where to go next"),
  );
  if (!nextHeading) {
    fail("R10", "there is no Where to go next section");
  } else {
    const nextAfter = h2.find((h) => h.number > nextHeading.number);
    const end = nextAfter ? nextAfter.number : Infinity;
    const targets = new Set();
    for (const line of lines) {
      if (!isContent(line)) continue;
      if (line.number <= nextHeading.number || line.number >= end) continue;
      const pattern = new RegExp(LINK.source, "g");
      let match = pattern.exec(line.text);
      while (match) {
        let target = match[1].split("#")[0];
        if (target.startsWith("./")) target = target.slice(2);
        targets.add(target);
        match = pattern.exec(line.text);
      }
    }
    // Each file is required only when the tree has it, and a link to the other
    // SECURITY.md location does not stand in for the one that exists.
    const wanted = [
      "AGENTS.md",
      "docs/README.md",
      "SECURITY.md",
      ".github/SECURITY.md",
      "LICENSE",
    ];
    for (const path of wanted) {
      if (has(path) && !targets.has(path)) {
        fail(
          "R10",
          `the Where to go next section at line ${nextHeading.number} does not link ${path}`,
        );
      }
    }
  }

  // R11 — at most 1000 words.
  const words = countWords(doc.text);
  if (words > 1000) {
    fail("R11", `the README has ${words} words (at most 1000)`);
  }

  // R12 — what stays out.
  let emojiShown = 0;
  for (const match of doc.text.matchAll(EMOJI)) {
    const code = match[0].codePointAt(0);
    if (EMOJI_ALLOWED.has(code)) continue;
    emojiShown += 1;
    if (emojiShown <= 5) {
      fail(
        "R12",
        `emoji U+${code.toString(16).toUpperCase().padStart(4, "0")} at line ${lineAt(doc.text, match.index)}`,
      );
    }
  }
  if (emojiShown > 5) {
    fail("R12", `and ${emojiShown - 5} more emoji`);
  }
  for (const match of doc.text.matchAll(PRIVATE_V4)) {
    fail(
      "R12",
      `private IPv4 address ${match[0]} at line ${lineAt(doc.text, match.index)}`,
    );
  }
  let dates = 0;
  for (const line of content) {
    const pattern = new RegExp(ISO_DATE.source, "g");
    const found = line.text.match(pattern) ?? [];
    dates += found.length;
  }
  if (dates > 2) {
    fail("R12", `there are ${dates} dates in YYYY-MM-DD form outside fenced code (at most 2)`);
  }
  for (const line of lines) {
    if (line.text.includes("├──") || line.text.includes("└──")) {
      fail("R12", `line ${line.number}: tree drawing (├──, └──)`);
    }
  }

  return findings;
}

function usage() {
  console.error("usage: node check-readme.mjs [--ref <ref>] [<dir>]");
  process.exit(2);
}

function run(argv) {
  let ref = null;
  let dir = null;
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--ref") {
      const value = argv[i + 1];
      if (!value || value.startsWith("-")) usage();
      ref = value;
      i += 1;
    } else if (arg.startsWith("-")) {
      usage();
    } else if (dir === null) {
      dir = arg;
    } else {
      usage();
    }
  }

  const root = resolve(dir ?? ".");
  let text = null;
  let has;
  if (ref === null) {
    const file = join(root, "README.md");
    if (existsSync(file)) text = readFileSync(file, "utf8");
    has = (path) => existsSync(join(root, path));
  } else {
    try {
      text = execFileSync("git", ["-C", root, "show", `${ref}:README.md`], {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "pipe"],
      });
    } catch {
      text = null;
    }
    has = (path) => {
      try {
        execFileSync("git", ["-C", root, "cat-file", "-e", `${ref}:${path}`], {
          stdio: ["ignore", "ignore", "ignore"],
        });
        return true;
      } catch {
        return false;
      }
    };
  }

  if (text === null) {
    console.log("FAIL R2 README.md is missing");
    process.exit(1);
  }

  const findings = checkReadme(text, has);
  if (findings.length === 0) {
    const kind = readKind(text) ?? "unknown";
    console.log(`ok README.md (${kind}, ${countWords(text)} words)`);
    process.exit(0);
  }
  for (const finding of findings) {
    console.log(`FAIL ${finding.rule} ${finding.message}`);
  }
  process.exit(1);
}

const entry = process.argv[1];
const isMain = (() => {
  if (!entry) return false;
  try {
    return pathToFileURL(resolve(entry)).href === import.meta.url;
  } catch {
    return false;
  }
})();

if (isMain) run(process.argv.slice(2));
