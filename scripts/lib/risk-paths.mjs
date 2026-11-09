// Risk paths and levels (ADR-0068, ADR-0078, docs/specs/risk-paths.md): strict
// parser, validator for list versions 1 and 2, glob matcher, the level of a
// path and of a pull request, and a minimal GitHub API client. The labeling
// step lives in scripts/lib/pr-triage.mjs.
//
// This module has no static imports. The reusable workflow
// .github/workflows/pr-triage.yml embeds this file verbatim, writes it to a
// file in the runner temp directory, and scripts/lib/pr-triage.mjs imports it
// there, with no checkout. Edit this file, then run
// `npm run sync:risk-paths-workflow`; `npm run check:risk-paths` fails when
// the embedded copy drifts. Never edit the embedded copy by hand.
//
// Other repositories vendor this file and import validateRiskPaths,
// matchGlob, parseStrictJson and matchFiles. Keep those names, their
// signatures and their results for a version 1 list.

export const LIST_PATH = ".github/risk-paths.json";
export const LABEL = "risk:high";
export const FILE_LIST_LIMIT = 3000;
export const MAX_BRACE_EXPANSIONS = 256;
export const MAX_WHY_LENGTH = 200;

// Levels, strictest first: a lower number is stricter.
export const LEVELS = Object.freeze(["L1", "L2", "L3", "L4"]);
const LEVEL_LIST = LEVELS.join(", ");

// The org floor: a path that matches the floor is at least this strict, whatever
// a repository list says. A repository cannot change it.
export const ORG_FLOORS = Object.freeze({
  L1: Object.freeze({
    words: Object.freeze([
      "secret", "secrets", "credential", "credentials", "password", "passwords", "passwd",
      "signing", "keyring", "keyrings", "keys", "gpg", "pgp",
    ]),
    extensions: Object.freeze(["pem", "key", "p12", "pfx", "gpg", "asc"]),
  }),
  L2: Object.freeze({
    globs: Object.freeze([
      ".github/workflows/**",
      ".github/actions/**",
      ".github/risk-paths.json",
      "policies/**",
      ".github/policies/**",
    ]),
  }),
});
const MAX_JSON_DEPTH = 64;
const MAX_PAGES = 40;

// ---- Strict JSON ----------------------------------------------------------

// JSON.parse keeps the last of duplicate keys. This parser fails on a
// duplicate key at any level and names the position of every error.
export function parseStrictJson(text) {
  if (typeof text !== "string") throw new Error("JSON input is not a string");
  let pos = 0;

  const fail = (message) => {
    throw new Error(`invalid JSON at position ${pos}: ${message}`);
  };
  const skipWhitespace = () => {
    while (pos < text.length) {
      const c = text[pos];
      if (c === " " || c === "\t" || c === "\n" || c === "\r") pos++;
      else break;
    }
  };
  const expectLiteral = (word, value) => {
    if (text.startsWith(word, pos)) {
      pos += word.length;
      return value;
    }
    return fail("unexpected token");
  };

  const parseString = () => {
    // text[pos] is the opening quote.
    pos++;
    let out = "";
    while (true) {
      if (pos >= text.length) fail("unterminated string");
      const c = text[pos];
      const code = c.charCodeAt(0);
      if (c === '"') {
        pos++;
        return out;
      }
      if (code < 0x20) fail("control character in string");
      if (c !== "\\") {
        out += c;
        pos++;
        continue;
      }
      const e = text[pos + 1];
      const simple = { '"': '"', "\\": "\\", "/": "/", b: "\b", f: "\f", n: "\n", r: "\r", t: "\t" };
      if (e !== undefined && Object.hasOwn(simple, e)) {
        out += simple[e];
        pos += 2;
      } else if (e === "u") {
        const hex = text.slice(pos + 2, pos + 6);
        if (!/^[0-9a-fA-F]{4}$/.test(hex)) fail("invalid unicode escape");
        out += String.fromCharCode(parseInt(hex, 16));
        pos += 6;
      } else {
        fail("invalid escape");
      }
    }
  };

  const parseNumber = () => {
    const match = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/.exec(text.slice(pos));
    if (!match) fail("invalid number");
    pos += match[0].length;
    return Number(match[0]);
  };

  const parseValue = (depth) => {
    if (depth > MAX_JSON_DEPTH) fail("nesting too deep");
    skipWhitespace();
    if (pos >= text.length) fail("unexpected end of input");
    const c = text[pos];
    if (c === "{") return parseObject(depth);
    if (c === "[") return parseArray(depth);
    if (c === '"') return parseString();
    if (c === "t") return expectLiteral("true", true);
    if (c === "f") return expectLiteral("false", false);
    if (c === "n") return expectLiteral("null", null);
    if (c === "-" || (c >= "0" && c <= "9")) return parseNumber();
    return fail("unexpected token");
  };

  const parseObject = (depth) => {
    pos++;
    const object = {};
    const seen = new Set();
    skipWhitespace();
    if (text[pos] === "}") {
      pos++;
      return object;
    }
    while (true) {
      skipWhitespace();
      if (text[pos] !== '"') fail("expected a string key");
      const keyPos = pos;
      const key = parseString();
      if (seen.has(key)) {
        pos = keyPos;
        fail(`duplicate key ${JSON.stringify(key)}`);
      }
      seen.add(key);
      skipWhitespace();
      if (text[pos] !== ":") fail("expected ':'");
      pos++;
      const value = parseValue(depth + 1);
      // defineProperty keeps a "__proto__" key an own, unknown key.
      Object.defineProperty(object, key, { value, enumerable: true, writable: true, configurable: true });
      skipWhitespace();
      if (text[pos] === ",") {
        pos++;
        continue;
      }
      if (text[pos] === "}") {
        pos++;
        return object;
      }
      fail("expected ',' or '}'");
    }
  };

  const parseArray = (depth) => {
    pos++;
    const array = [];
    skipWhitespace();
    if (text[pos] === "]") {
      pos++;
      return array;
    }
    while (true) {
      array.push(parseValue(depth + 1));
      skipWhitespace();
      if (text[pos] === ",") {
        pos++;
        continue;
      }
      if (text[pos] === "]") {
        pos++;
        return array;
      }
      fail("expected ',' or ']'");
    }
  };

  const value = parseValue(0);
  skipWhitespace();
  if (pos !== text.length) fail("unexpected data after the JSON value");
  return value;
}

// ---- Globs ----------------------------------------------------------------

// Expands brace groups. Returns { expansions, errors }.
function expandBraces(glob) {
  const errors = [];
  const parts = []; // each part is a list of alternatives
  let literal = "";
  let i = 0;
  while (i < glob.length) {
    const c = glob[i];
    if (c === "}") {
      errors.push("unmatched '}'");
      i++;
      continue;
    }
    if (c !== "{") {
      literal += c;
      i++;
      continue;
    }
    const close = glob.indexOf("}", i + 1);
    const nextOpen = glob.indexOf("{", i + 1);
    if (close === -1) {
      errors.push("unmatched '{'");
      break;
    }
    if (nextOpen !== -1 && nextOpen < close) {
      errors.push("nested braces are not supported");
      break;
    }
    const alternatives = glob.slice(i + 1, close).split(",");
    if (alternatives.length < 2) errors.push("a brace group needs at least 2 alternatives");
    if (alternatives.some((alternative) => alternative === "")) errors.push("a brace group has an empty alternative");
    parts.push([literal]);
    parts.push(alternatives);
    literal = "";
    i = close + 1;
  }
  if (errors.length > 0) return { expansions: [], errors };
  parts.push([literal]);
  let expansions = [""];
  for (const alternatives of parts) {
    const next = [];
    for (const prefix of expansions) {
      for (const alternative of alternatives) next.push(prefix + alternative);
    }
    if (next.length > MAX_BRACE_EXPANSIONS) {
      return { expansions: [], errors: [`brace expansion gives more than ${MAX_BRACE_EXPANSIONS} globs`] };
    }
    expansions = next;
  }
  return { expansions, errors: [] };
}

// Returns a list of error strings; an empty list means the glob is valid.
export function validateGlob(glob) {
  if (typeof glob !== "string") return ["glob is not a string"];
  if (glob === "") return ["glob is empty"];
  const errors = [];
  for (const c of ["[", "]", "!", "\\"]) {
    if (glob.includes(c)) errors.push(`${JSON.stringify(c)} is not supported`);
  }
  const { expansions, errors: braceErrors } = expandBraces(glob);
  errors.push(...braceErrors);
  for (const expansion of expansions) {
    if (expansion.startsWith("/")) errors.push("a glob must not start with '/'");
    if (expansion.startsWith("./")) errors.push("a glob must not start with './'");
    for (const segment of expansion.split("/")) {
      if (segment === "") errors.push("a glob must not have an empty segment");
      else if (segment === "..") errors.push("a glob must not have a '..' segment");
      else if (segment.includes("**") && segment !== "**") errors.push("'**' must be a whole segment");
    }
  }
  return [...new Set(errors)];
}

function segmentPattern(segment) {
  let source = "^";
  for (const c of segment) {
    if (c === "*") source += "[^/]*";
    else if (c === "?") source += "[^/]";
    else source += c.replace(/[.*+?^$()|[\]{}\\/]/g, "\\$&");
  }
  return new RegExp(`${source}$`, "u");
}

function matchSegments(globSegments, pathSegments) {
  const patterns = globSegments.map((segment) => (segment === "**" ? null : segmentPattern(segment)));
  const memo = new Map();
  const visit = (g, p) => {
    const key = g * (pathSegments.length + 1) + p;
    if (memo.has(key)) return memo.get(key);
    let result;
    if (g === patterns.length) {
      result = p === pathSegments.length;
    } else if (patterns[g] === null) {
      // "**" matches 0 or more whole segments.
      result = visit(g + 1, p) || (p < pathSegments.length && visit(g, p + 1));
    } else {
      result = p < pathSegments.length && patterns[g].test(pathSegments[p]) && visit(g + 1, p + 1);
    }
    memo.set(key, result);
    return result;
  };
  return visit(0, 0);
}

// Whole-path, case-sensitive match of a valid glob against a repository path.
export function matchGlob(glob, path) {
  if (typeof glob !== "string" || typeof path !== "string" || path === "") return false;
  const { expansions, errors } = expandBraces(glob);
  if (errors.length > 0) return false;
  const pathSegments = path.split("/");
  return expansions.some((expansion) => matchSegments(expansion.split("/"), pathSegments));
}

// ---- Validation -----------------------------------------------------------

function isPlainObject(value) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  const proto = Object.getPrototypeOf(value);
  return proto === Object.prototype || proto === null;
}

function checkKeys(value, allowed, at, errors) {
  for (const key of Object.keys(value)) {
    if (!allowed.includes(key)) errors.push(`${at}: unknown key ${JSON.stringify(key)}`);
  }
  for (const key of allowed) {
    if (!Object.hasOwn(value, key)) errors.push(`${at}: missing key ${JSON.stringify(key)}`);
  }
}

const isLevel = (value) => typeof value === "string" && LEVELS.includes(value);

// Validates a parsed list against the spec field table. Collects every error.
// Version 1 has the keys schema_version and paths, and entries glob and why.
// Version 2 adds default_level and a level in each entry.
export function validateRiskPaths(value) {
  const errors = [];
  if (!isPlainObject(value)) {
    return { ok: false, errors: ["$: the top level must be an object"] };
  }
  const v2 = value.schema_version === 2;
  checkKeys(value, v2 ? ["schema_version", "default_level", "paths"] : ["schema_version", "paths"], "$", errors);
  if (value.schema_version !== 1 && value.schema_version !== 2) {
    errors.push("schema_version: must be the integer 1 or 2");
  }
  if (v2 && Object.hasOwn(value, "default_level") && !isLevel(value.default_level)) {
    errors.push(`default_level: must be 1 of ${LEVEL_LIST}`);
  }
  if (Object.hasOwn(value, "paths")) {
    if (!Array.isArray(value.paths)) {
      errors.push("paths: must be an array");
    } else {
      if (value.paths.length === 0) errors.push("paths: must have at least 1 entry");
      const globs = new Map();
      value.paths.forEach((entry, index) => {
        const at = `paths[${index}]`;
        if (!isPlainObject(entry)) {
          errors.push(`${at}: must be an object`);
          return;
        }
        checkKeys(entry, v2 ? ["glob", "level", "why"] : ["glob", "why"], at, errors);
        if (Object.hasOwn(entry, "glob")) {
          if (typeof entry.glob !== "string") {
            errors.push(`${at}.glob: must be a string`);
          } else {
            for (const error of validateGlob(entry.glob)) {
              errors.push(`${at}.glob: ${JSON.stringify(entry.glob)}: ${error}`);
            }
            if (globs.has(entry.glob)) {
              errors.push(`${at}.glob: duplicate glob ${JSON.stringify(entry.glob)} (also paths[${globs.get(entry.glob)}])`);
            } else {
              globs.set(entry.glob, index);
            }
          }
        }
        if (v2 && Object.hasOwn(entry, "level") && !isLevel(entry.level)) {
          errors.push(`${at}.level: must be 1 of ${LEVEL_LIST}`);
        }
        if (Object.hasOwn(entry, "why")) {
          if (typeof entry.why !== "string") {
            errors.push(`${at}.why: must be a string`);
          } else if (entry.why.trim() === "") {
            errors.push(`${at}.why: must not be empty`);
          } else if ([...entry.why].length > MAX_WHY_LENGTH) {
            errors.push(`${at}.why: must be at most ${MAX_WHY_LENGTH} characters`);
          }
        }
      });
    }
  }
  return { ok: errors.length === 0, errors };
}

// ---- Matching -------------------------------------------------------------

// files: entries of the pulls/{n}/files shape { filename, status, previous_filename? }.
// A rename matches by its new path and by its previous path.
export function matchFiles(list, files) {
  const entries = Array.isArray(list) ? list : list.paths;
  const matches = [];
  const seen = new Set();
  for (const file of files) {
    const paths = [file.filename];
    if (typeof file.previous_filename === "string" && file.previous_filename !== file.filename) {
      paths.push(file.previous_filename);
    }
    for (const path of paths) {
      for (const entry of entries) {
        if (!matchGlob(entry.glob, path)) continue;
        const key = JSON.stringify([path, entry.glob]);
        if (seen.has(key)) continue;
        seen.add(key);
        matches.push({ path, glob: entry.glob, why: entry.why });
      }
    }
  }
  return matches;
}

// ---- Levels ---------------------------------------------------------------

const levelRank = (level) => LEVELS.indexOf(level);

// The strictest of 2 levels; the first one wins a tie.
const stricter = (a, b) => (levelRank(b) < levelRank(a) ? b : a);

// The org floor of a path: { level, reason } or null.
function floorOfPath(path) {
  const words = path.toLowerCase().split(/[/.\-_]/).filter((word) => word !== "");
  const word = words.find((candidate) => ORG_FLOORS.L1.words.includes(candidate));
  if (word !== undefined) return { level: "L1", reason: `org floor: path word ${JSON.stringify(word)}` };
  const last = path.split("/").pop();
  const dot = last.lastIndexOf(".");
  if (dot > 0) {
    const extension = last.slice(dot + 1).toLowerCase();
    if (ORG_FLOORS.L1.extensions.includes(extension)) {
      return { level: "L1", reason: `org floor: extension ${JSON.stringify(extension)}` };
    }
  }
  const glob = ORG_FLOORS.L2.globs.find((candidate) => matchGlob(candidate, path));
  if (glob !== undefined) return { level: "L2", reason: `org floor: ${glob}` };
  return null;
}

// A version 1 list reads as every glob at L1 and default_level L3.
function levelEntries(list) {
  const v2 = list.schema_version === 2;
  return {
    defaultLevel: v2 ? list.default_level : "L3",
    entries: list.paths.map((entry) => ({ glob: entry.glob, level: v2 ? entry.level : "L1", why: entry.why })),
  };
}

// The level of 1 path under a valid list of version 1 or 2.
// Returns { path, level, source, glob, why }; source is "glob", "default" or "floor".
export function levelOfFile(list, path) {
  const { defaultLevel, entries } = levelEntries(list);
  let result = { path, level: defaultLevel, source: "default", glob: null, why: null };
  let best = null;
  for (const entry of entries) {
    if (!matchGlob(entry.glob, path)) continue;
    if (best === null || levelRank(entry.level) < levelRank(best.level)) best = entry;
  }
  if (best !== null) result = { path, level: best.level, source: "glob", glob: best.glob, why: best.why };
  const floor = floorOfPath(path);
  if (floor !== null && levelRank(floor.level) < levelRank(result.level)) {
    result = { path, level: floor.level, source: "floor", glob: null, why: floor.reason };
  }
  return result;
}

// The level of a pull request: the strictest level of its changed paths.
// files: entries of the pulls/{n}/files shape, or plain path strings. A rename
// counts by both names. A file list at the API limit is truncated: L1.
export function levelOfPullRequest(list, files) {
  const paths = [];
  const seen = new Set();
  const add = (path) => {
    if (typeof path !== "string" || path === "" || seen.has(path)) return;
    seen.add(path);
    paths.push(path);
  };
  for (const file of files) {
    if (typeof file === "string") {
      add(file);
    } else {
      add(file?.filename);
      add(file?.previous_filename);
    }
  }
  const results = paths.map((path) => levelOfFile(list, path));
  const truncated = files.length >= FILE_LIST_LIMIT;
  let level = levelEntries(list).defaultLevel;
  if (results.length > 0) level = results.map((result) => result.level).reduce(stricter);
  if (truncated) level = "L1";
  return { level, truncated, files: results };
}

// Rule C for the gates: each required path must match a glob and, in a
// version 2 list, no glob that matches it may be L3 or L4. Returns error strings.
export function checkRequiredPaths(list, requiredPaths) {
  const errors = [];
  const v2 = list.schema_version === 2;
  for (const required of requiredPaths) {
    const matching = list.paths.filter((entry) => matchGlob(entry.glob, required));
    if (matching.length === 0) errors.push(`no glob matches the required path ${required}`);
    if (!v2) continue;
    for (const entry of matching) {
      if (entry.level === "L3" || entry.level === "L4") {
        errors.push(`the glob ${entry.glob} matches the required path ${required} at level ${entry.level}; it must be L1 or L2`);
      }
    }
  }
  return errors;
}

// ---- GitHub API -----------------------------------------------------------

export function createGitHubApi({ token, apiUrl = "https://api.github.com", fetch }) {
  if (!token) throw new Error("no GitHub token");
  if (typeof fetch !== "function") throw new Error("no fetch function");
  const base = String(apiUrl).replace(/\/+$/, "");
  const enc = encodeURIComponent;

  const request = async (method, path, { accept = "application/vnd.github+json", body, allow404 = false } = {}) => {
    const url = path.startsWith(`${base}/`) ? path : `${base}${path}`;
    const shownPath = url.slice(base.length).split("?")[0];
    const headers = {
      Accept: accept,
      Authorization: `Bearer ${token}`,
      "X-GitHub-Api-Version": "2022-11-28",
      "User-Agent": "frostyard-risk-paths",
    };
    if (body !== undefined) headers["Content-Type"] = "application/json";
    let response;
    try {
      response = await fetch(url, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        redirect: "error",
      });
    } catch (error) {
      throw new Error(`GitHub API ${method} ${shownPath} failed: ${error?.message ?? "network error"}`);
    }
    if (allow404 && response.status === 404) return null;
    if (response.status < 200 || response.status > 299) {
      throw new Error(`GitHub API ${method} ${shownPath} failed: HTTP ${response.status}`);
    }
    return response;
  };

  const nextLink = (header) => {
    if (!header) return null;
    for (const part of header.split(",")) {
      const match = /<([^>]+)>\s*;\s*rel="next"/.exec(part);
      if (match) return match[1];
    }
    return null;
  };

  // Reads every page of a list. The body is an array, or an object whose
  // `field` is the array.
  const paginate = async (first, { field, what }) => {
    const items = [];
    let next = first;
    for (let page = 0; next; page++) {
      if (page >= MAX_PAGES) throw new Error(`GitHub API ${what} has more than ${MAX_PAGES} pages`);
      // The token goes only to the configured API host.
      if (!next.startsWith("/") && !next.startsWith(`${base}/`)) {
        throw new Error("GitHub API pagination link leaves the API host");
      }
      const response = await request("GET", next);
      const body = await response.json();
      const part = field === undefined ? body : body?.[field];
      if (!Array.isArray(part)) throw new Error(`GitHub API ${what} is not an array`);
      items.push(...part);
      next = nextLink(response.headers.get("link"));
    }
    return items;
  };

  return {
    async getFileAtRef(owner, repo, path, ref) {
      const encodedPath = path.split("/").map(enc).join("/");
      const response = await request(
        "GET",
        `/repos/${enc(owner)}/${enc(repo)}/contents/${encodedPath}?ref=${enc(ref)}`,
        { accept: "application/vnd.github.raw+json", allow404: true },
      );
      return response === null ? null : await response.text();
    },

    // The pull request as it is now: a delayed or re-run job compares its head
    // with the head of its event.
    async getPullRequest(owner, repo, number) {
      const response = await request("GET", `/repos/${enc(owner)}/${enc(repo)}/pulls/${enc(String(number))}`);
      return await response.json();
    },

    async listPullFiles(owner, repo, number) {
      return paginate(`/repos/${enc(owner)}/${enc(repo)}/pulls/${enc(String(number))}/files?per_page=100`, {
        what: "pull request file list",
      });
    },

    async listIssueComments(owner, repo, number) {
      return paginate(`/repos/${enc(owner)}/${enc(repo)}/issues/${enc(String(number))}/comments?per_page=100`, {
        what: "issue comment list",
      });
    },

    async listIssueLabels(owner, repo, number) {
      return paginate(`/repos/${enc(owner)}/${enc(repo)}/issues/${enc(String(number))}/labels?per_page=100`, {
        what: "issue label list",
      });
    },

    // The comment GitHub created, so that a caller can compare its id with another
    // run's comment for the same head.
    async createIssueComment(owner, repo, number, body) {
      const response = await request("POST", `/repos/${enc(owner)}/${enc(repo)}/issues/${enc(String(number))}/comments`, {
        body: { body },
      });
      return await response.json();
    },

    // Returns true when the comment went away, false when it was already gone (HTTP 404).
    async deleteIssueComment(owner, repo, commentId) {
      const response = await request(
        "DELETE",
        `/repos/${enc(owner)}/${enc(repo)}/issues/comments/${enc(String(commentId))}`,
        { allow404: true },
      );
      return response !== null;
    },

    // Returns true when the label came off, false when the issue did not have it (HTTP 404).
    async removeIssueLabel(owner, repo, number, name) {
      const response = await request(
        "DELETE",
        `/repos/${enc(owner)}/${enc(repo)}/issues/${enc(String(number))}/labels/${enc(name)}`,
        { allow404: true },
      );
      return response !== null;
    },

    // The latest run of each check name (filter=latest), as GitHub's checks
    // rollup counts them: an older failed run that a re-run replaced does not
    // count.
    async listCheckRuns(owner, repo, sha) {
      return paginate(`/repos/${enc(owner)}/${enc(repo)}/commits/${enc(sha)}/check-runs?filter=latest&per_page=100`, {
        field: "check_runs",
        what: "check run list",
      });
    },

    // The statuses array, not the combined state: the combined state is
    // "pending" when a commit has no statuses.
    async listCommitStatuses(owner, repo, sha) {
      return paginate(`/repos/${enc(owner)}/${enc(repo)}/commits/${enc(sha)}/status?per_page=100`, {
        field: "statuses",
        what: "commit status list",
      });
    },

    // The rules that apply to a branch (the rulesets API), all pages. The branch
    // is encoded part by part, as a path is.
    async listBranchRules(owner, repo, branch) {
      const encodedBranch = String(branch).split("/").map(enc).join("/");
      return paginate(`/repos/${enc(owner)}/${enc(repo)}/rules/branches/${encodedBranch}?per_page=100`, {
        what: "branch rule list",
      });
    },

    async labelExists(owner, repo, name) {
      const response = await request("GET", `/repos/${enc(owner)}/${enc(repo)}/labels/${enc(name)}`, { allow404: true });
      return response !== null;
    },

    async addLabels(owner, repo, number, labels) {
      await request("POST", `/repos/${enc(owner)}/${enc(repo)}/issues/${enc(String(number))}/labels`, {
        body: { labels },
      });
    },
  };
}

// ---- Helpers --------------------------------------------------------------

export function escapeCell(value) {
  return String(value)
    .replace(/[\\`*_[\]|]/g, "\\$&")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\r\n|\r|\n|\u2028|\u2029/g, " ");
}

export const NAME_PATTERN = /^[A-Za-z0-9_.-]+$/;

// Escapes a workflow-command value (the runner reads %, CR and LF specially).
export function commandValue(text) {
  return String(text).replace(/%/g, "%25").replace(/\r/g, "%0D").replace(/\n/g, "%0A");
}
