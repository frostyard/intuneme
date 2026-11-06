// Risk paths (ADR-0068, docs/specs/risk-paths.md): strict parser, validator,
// glob matcher, a minimal GitHub API client and the labeling job.
//
// This module has no static imports. The reusable workflow
// .github/workflows/risk-paths.yml embeds this file verbatim and runs it as
// stdin to `node --input-type=module`, with no checkout. Edit this file, then
// run `npm run sync:risk-paths-workflow`; `npm run check:risk-paths` fails when
// the embedded copy drifts. Never edit the embedded copy by hand.

export const LIST_PATH = ".github/risk-paths.json";
export const LABEL = "risk:high";
export const FILE_LIST_LIMIT = 3000;
export const MAX_BRACE_EXPANSIONS = 256;
export const MAX_WHY_LENGTH = 200;
const MAX_JSON_DEPTH = 64;
const MAX_PAGES = 40;
const SUMMARY_ROW_LIMIT = 500;
const LOG_PREFIX = "risk-paths: ";

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

// Validates a parsed list against the spec field table. Collects every error.
export function validateRiskPaths(value) {
  const errors = [];
  if (!isPlainObject(value)) {
    return { ok: false, errors: ["$: the top level must be an object"] };
  }
  checkKeys(value, ["schema_version", "paths"], "$", errors);
  if (Object.hasOwn(value, "schema_version") && value.schema_version !== 1) {
    errors.push("schema_version: must be the integer 1");
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
        checkKeys(entry, ["glob", "why"], at, errors);
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

    async listPullFiles(owner, repo, number) {
      const files = [];
      let next = `/repos/${enc(owner)}/${enc(repo)}/pulls/${enc(String(number))}/files?per_page=100`;
      for (let page = 0; next; page++) {
        if (page >= MAX_PAGES) throw new Error(`GitHub API pull request file list has more than ${MAX_PAGES} pages`);
        // The token goes only to the configured API host.
        if (!next.startsWith("/") && !next.startsWith(`${base}/`)) {
          throw new Error("GitHub API pagination link leaves the API host");
        }
        const response = await request("GET", next);
        const items = await response.json();
        if (!Array.isArray(items)) throw new Error("GitHub API pull request file list is not an array");
        files.push(...items);
        next = nextLink(response.headers.get("link"));
      }
      return files;
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

// ---- Job ------------------------------------------------------------------

function escapeCell(value) {
  return String(value)
    .replace(/[\\`*_[\]|]/g, "\\$&")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\r\n|\r|\n|\u2028|\u2029/g, " ");
}

const NAME_PATTERN = /^[A-Za-z0-9_.-]+$/;

export async function runRiskPathsJob({ api, eventName, event, summary, log }) {
  const say = (message, value) => log(value === undefined ? `${LOG_PREFIX}${message}` : `${LOG_PREFIX}${message} ${JSON.stringify(value)}`);

  if (eventName !== "pull_request_target") {
    throw new Error(`the caller must run on pull_request_target, not ${JSON.stringify(eventName)}`);
  }
  const pr = event?.pull_request;
  const owner = event?.repository?.owner?.login;
  const repo = event?.repository?.name;
  const number = pr?.number;
  const baseSha = pr?.base?.sha;
  if (typeof owner !== "string" || !NAME_PATTERN.test(owner) || typeof repo !== "string" || !NAME_PATTERN.test(repo)) {
    throw new Error("the event has no valid repository owner and name");
  }
  if (!Number.isSafeInteger(number) || number < 1) throw new Error("the event has no valid pull request number");
  if (typeof baseSha !== "string" || !/^[0-9a-f]{40}$/.test(baseSha)) {
    throw new Error("the event has no valid pull_request.base.sha (40 hex characters)");
  }
  const fullName = `${owner}/${repo}`;
  say("repository", fullName);
  say("pull request", number);
  say("base sha", baseSha);

  const text = await api.getFileAtRef(owner, repo, LIST_PATH, baseSha);
  if (text === null) {
    say(`no list on base: ${LIST_PATH} does not exist at`, baseSha);
    await summary(`## Risk paths\n\nno list on base: \`${LIST_PATH}\` does not exist at \`${baseSha}\`. No label change.\n`);
    return { outcome: "no-list", matches: [], truncated: false };
  }

  let list;
  try {
    list = parseStrictJson(text);
  } catch (error) {
    throw new Error(`${LIST_PATH} at ${baseSha} is not valid: ${error.message}`);
  }
  const validation = validateRiskPaths(list);
  if (!validation.ok) {
    throw new Error(`${LIST_PATH} at ${baseSha} is not valid: ${validation.errors.join("; ")}`);
  }
  say("list entries", list.paths.length);

  if (!(await api.labelExists(owner, repo, LABEL))) {
    throw new Error(
      `label ${LABEL} does not exist in ${fullName}; the accountable owner must create it ` +
        `(gh label create ${LABEL} --repo ${fullName} --color B60205 --description "Touches a high-risk path (ADR-0068)")`,
    );
  }

  const files = await api.listPullFiles(owner, repo, number);
  const truncated = files.length >= FILE_LIST_LIMIT;
  say("changed files", files.length);
  const matches = matchFiles(list, files);

  if (!truncated && matches.length === 0) {
    say("no high-risk path changed");
    await summary("## Risk paths\n\nno high-risk path changed. No label change.\n");
    return { outcome: "no-match", matches, truncated };
  }

  await api.addLabels(owner, repo, number, [LABEL]);
  say("added label", LABEL);
  for (const match of matches) say("match", match);

  const lines = ["## Risk paths", "", `Added the label \`${LABEL}\`.`, ""];
  if (truncated) {
    say("file list truncated at", files.length);
    lines.push(
      `The file list is truncated: the GitHub API returned ${files.length} files, its limit is ${FILE_LIST_LIMIT}. The label is added (fail safe).`,
      "",
    );
  }
  if (matches.length > 0) {
    lines.push("| Path | Glob | Why |", "| --- | --- | --- |");
    for (const match of matches.slice(0, SUMMARY_ROW_LIMIT)) {
      lines.push(`| ${escapeCell(match.path)} | ${escapeCell(match.glob)} | ${escapeCell(match.why)} |`);
    }
    if (matches.length > SUMMARY_ROW_LIMIT) {
      lines.push("", `${matches.length - SUMMARY_ROW_LIMIT} more matches are not shown here; the job log lists every match.`);
    }
  }
  await summary(`${lines.join("\n")}\n`);
  return { outcome: "labeled", matches, truncated };
}

// Escapes a workflow-command value (the runner reads %, CR and LF specially).
function commandValue(text) {
  return String(text).replace(/%/g, "%25").replace(/\r/g, "%0D").replace(/\n/g, "%0A");
}

export async function main(env) {
  const token = env.GITHUB_TOKEN;
  try {
    const fs = await import("node:fs/promises");
    if (!env.GITHUB_EVENT_PATH) throw new Error("GITHUB_EVENT_PATH is not set");
    if (!token) throw new Error("GITHUB_TOKEN is not set");
    const event = JSON.parse(await fs.readFile(env.GITHUB_EVENT_PATH, "utf8"));
    const api = createGitHubApi({
      token,
      apiUrl: env.GITHUB_API_URL || "https://api.github.com",
      fetch: globalThis.fetch,
    });
    const summary = async (markdown) => {
      if (env.GITHUB_STEP_SUMMARY) await fs.appendFile(env.GITHUB_STEP_SUMMARY, markdown);
    };
    const log = (line) => {
      const text = String(line).replace(/[\r\n\u2028\u2029]/g, " ");
      process.stdout.write(`${text.startsWith(LOG_PREFIX) ? text : LOG_PREFIX + JSON.stringify(text)}\n`);
    };
    return await runRiskPathsJob({ api, eventName: env.GITHUB_EVENT_NAME, event, summary, log });
  } catch (error) {
    let message = String(error?.message ?? error);
    if (token) message = message.split(token).join("***");
    message = message.replace(/[\r\n\u2028\u2029]+/g, " ");
    process.stdout.write(`::error::${commandValue(message)}\n`);
    process.exitCode = 1;
    return undefined;
  }
}
