const GAMMA = "https://gamma-api.polymarket.com";
const DATA = "https://data-api.polymarket.com";
const MIN_PNL = 100_000;
const HOLDERS_LIMIT = 20;
const STRENGTH_CONCURRENCY = 4;
const PNL_CONCURRENCY = 8;

const $ = (id) => document.getElementById(id);

/** @type {Array<MarketRow>} */
let rows = [];
let sortKey = "volume_24hr";
let sortDir = "desc";
let strengthAbort = null;
/** @type {Set<string>} */
const expanded = new Set();

/**
 * @typedef {object} LastTrade
 * @property {"BUY"|"SELL"|string} side
 * @property {number} size
 * @property {number} timestamp
 * @property {string} outcome
 * @property {"pending"|"ready"|"error"|"empty"} status
 */

/**
 * @typedef {object} QualifiedHolder
 * @property {string} wallet
 * @property {string} name
 * @property {number} size
 * @property {number} pnl
 * @property {LastTrade|null} last_trade
 */

/**
 * @typedef {object} MarketRow
 * @property {string} condition_id
 * @property {string} slug
 * @property {string} question
 * @property {string} event_slug
 * @property {string} event_title
 * @property {string} url
 * @property {number} volume_24hr
 * @property {number[]} outcome_prices
 * @property {string[]} outcomes
 * @property {"pending"|"ready"|"error"|"empty"} strength_status
 * @property {number|null} yes_strength
 * @property {number|null} no_strength
 * @property {number} yes_qualified_size
 * @property {number} no_qualified_size
 * @property {number} yes_qualified_count
 * @property {number} no_qualified_count
 * @property {number} total_qualified_size
 * @property {string} stronger
 * @property {QualifiedHolder[]} yes_holders
 * @property {QualifiedHolder[]} no_holders
 */

async function getJSON(url, signal) {
  const res = await fetch(url, { signal });
  if (!res.ok) throw new Error(`${res.status} ${url}`);
  return res.json();
}

async function resolveTag(slug, signal) {
  const t = await getJSON(`${GAMMA}/tags/slug/${encodeURIComponent(slug)}`, signal);
  return { id: Number(t.id), slug: t.slug, label: t.label };
}

function parseOutcomes(raw) {
  if (Array.isArray(raw)) return raw;
  if (typeof raw !== "string" || !raw) return [];
  try { return JSON.parse(raw); } catch { return []; }
}

function parsePrices(raw) {
  const arr = parseOutcomes(raw);
  return arr.map((x) => Number(x)).filter((n) => Number.isFinite(n));
}

function isBinaryYesNo(outcomes) {
  if (outcomes.length !== 2) return false;
  const a = outcomes[0].toLowerCase();
  const b = outcomes[1].toLowerCase();
  return (a === "yes" && b === "no") || (a === "no" && b === "yes");
}

async function listMarkets({ tagSlug, minVolume24, signal }) {
  const tag = await resolveTag(tagSlug, signal);
  const out = [];
  let cursor = "";
  for (;;) {
    const q = new URLSearchParams({
      tag_id: String(tag.id),
      limit: "100",
      order: "volume24hr",
      ascending: "false",
      closed: "false",
    });
    if (cursor) q.set("after_cursor", cursor);
    const page = await getJSON(`${GAMMA}/markets/keyset?${q}`, signal);
    const markets = page.markets || [];
    let stop = false;
    for (const m of markets) {
      if (!m.active || m.closed) continue;
      const vol = Number(m.volume24hr) || 0;
      if (vol <= minVolume24) { stop = true; break; }
      const outcomes = parseOutcomes(m.outcomes);
      if (!isBinaryYesNo(outcomes)) continue;
      const event = (m.events && m.events[0]) || {};
      const slug = m.slug || "";
      out.push({
        condition_id: m.conditionId,
        slug,
        question: m.question || slug,
        event_slug: event.slug || "",
        event_title: event.title || "",
        url: event.slug
          ? `https://polymarket.com/event/${event.slug}/${slug}`
          : `https://polymarket.com/market/${slug}`,
        volume_24hr: vol,
        outcome_prices: parsePrices(m.outcomePrices),
        outcomes,
        strength_status: "pending",
        yes_strength: null,
        no_strength: null,
        yes_qualified_size: 0,
        no_qualified_size: 0,
        yes_qualified_count: 0,
        no_qualified_count: 0,
        total_qualified_size: 0,
        stronger: "tie",
        yes_holders: [],
        no_holders: [],
      });
    }
    const next = page.next_cursor || "";
    if (stop || !next || next === cursor) break;
    cursor = next;
  }
  return { tag, markets: out };
}

async function mapPool(items, concurrency, fn, signal) {
  const results = new Array(items.length);
  let i = 0;
  async function worker() {
    while (i < items.length) {
      if (signal?.aborted) throw new DOMException("Aborted", "AbortError");
      const idx = i++;
      results[idx] = await fn(items[idx], idx);
    }
  }
  const n = Math.min(concurrency, Math.max(items.length, 1));
  await Promise.all(Array.from({ length: n }, () => worker()));
  return results;
}

async function lifetimePnL(wallet, signal) {
  const q = new URLSearchParams({
    user: wallet,
    timePeriod: "ALL",
    orderBy: "PNL",
    limit: "1",
    category: "OVERALL",
  });
  const entries = await getJSON(`${DATA}/v1/leaderboard?${q}`, signal);
  if (!entries?.length) return null;
  return Number(entries[0].pnl);
}

async function computeStrength(conditionId, signal) {
  const groups = await getJSON(
    `${DATA}/holders?market=${encodeURIComponent(conditionId)}&limit=${HOLDERS_LIMIT}`,
    signal,
  );
  /** @type {Array<{side:"yes"|"no", wallet:string, name:string, size:number}>} */
  const holders = [];
  for (const g of groups || []) {
    for (const h of g.holders || []) {
      const wallet = String(h.proxyWallet || "").toLowerCase();
      if (!wallet) continue;
      const side = h.outcomeIndex === 1 ? "no" : "yes";
      holders.push({
        side,
        wallet,
        name: displayName(h),
        size: Number(h.amount) || 0,
      });
    }
  }
  if (!holders.length) {
    return {
      yes_strength: null,
      no_strength: null,
      yes_qualified_size: 0,
      no_qualified_size: 0,
      yes_qualified_count: 0,
      no_qualified_count: 0,
      total_qualified_size: 0,
      stronger: "tie",
      strength_status: "empty",
      yes_holders: [],
      no_holders: [],
    };
  }

  const unique = [...new Set(holders.map((h) => h.wallet))];
  const pnls = new Map();
  await mapPool(unique, PNL_CONCURRENCY, async (w) => {
    pnls.set(w, await lifetimePnL(w, signal));
  }, signal);

  /** @type {QualifiedHolder[]} */
  const yesHolders = [];
  /** @type {QualifiedHolder[]} */
  const noHolders = [];
  for (const h of holders) {
    const pnl = pnls.get(h.wallet);
    if (pnl == null || !(pnl > MIN_PNL)) continue;
    const row = {
      wallet: h.wallet,
      name: h.name,
      size: h.size,
      pnl,
      last_trade: null,
    };
    if (h.side === "yes") yesHolders.push(row);
    else noHolders.push(row);
  }

  yesHolders.sort((a, b) => b.size - a.size);
  noHolders.sort((a, b) => b.size - a.size);

  const yesSize = yesHolders.reduce((s, h) => s + h.size, 0);
  const noSize = noHolders.reduce((s, h) => s + h.size, 0);
  const total = yesSize + noSize;
  const yes = total > 0 ? yesSize / total : null;
  const no = total > 0 ? noSize / total : null;
  let stronger = "tie";
  if (yesSize > noSize) stronger = "yes";
  else if (noSize > yesSize) stronger = "no";

  return {
    yes_strength: yes,
    no_strength: no,
    yes_qualified_size: yesSize,
    no_qualified_size: noSize,
    yes_qualified_count: yesHolders.length,
    no_qualified_count: noHolders.length,
    total_qualified_size: total,
    stronger,
    strength_status: total > 0 ? "ready" : "empty",
    yes_holders: yesHolders,
    no_holders: noHolders,
  };
}

function fmtVol(n) {
  if (n >= 1e6) return (n / 1e6).toFixed(2) + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1) + "k";
  return String(Math.round(n || 0));
}

function fmtShares(n) {
  if (n == null || Number.isNaN(n)) return "—";
  if (n >= 1e6) return (n / 1e6).toFixed(2) + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1) + "k";
  return n.toFixed(n >= 10 ? 0 : 1);
}

function fmtPnl(n) {
  if (n == null || Number.isNaN(n)) return "—";
  const sign = n < 0 ? "-" : "";
  const abs = Math.abs(n);
  if (abs >= 1e6) return `${sign}$${(abs / 1e6).toFixed(2)}M`;
  if (abs >= 1e3) return `${sign}$${(abs / 1e3).toFixed(1)}k`;
  return `${sign}$${Math.round(abs)}`;
}

function displayName(h) {
  const name = String(h.name || "").trim();
  const pseudo = String(h.pseudonym || "").trim();
  if (h.displayUsernamePublic !== false && name) return name;
  if (pseudo) return pseudo;
  if (name) return name;
  return "";
}

function fmtWallet(w) {
  if (!w || w.length < 12) return w || "—";
  return `${w.slice(0, 6)}…${w.slice(-4)}`;
}

function fmtTrader(h) {
  return h.name || fmtWallet(h.wallet);
}

function fmtPrice(m) {
  const p = m.outcome_prices || [];
  if (p.length < 2) return "—";
  return `${Math.round(p[0] * 100)}¢ / ${Math.round(p[1] * 100)}¢`;
}

function fmtWhen(ts) {
  if (!ts) return "—";
  const ms = ts > 1e12 ? ts : ts * 1000;
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return "—";
  const diff = Date.now() - d.getTime();
  const sec = Math.round(diff / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.round(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.round(min / 60);
  if (hr < 48) return `${hr}h ago`;
  const day = Math.round(hr / 24);
  if (day < 60) return `${day}d ago`;
  return d.toLocaleDateString();
}

function fmtLastTrade(t) {
  if (!t || t.status === "pending") return `<span class="pending">…</span>`;
  if (t.status === "error") return `<span class="na">err</span>`;
  if (t.status === "empty" || !t.side) return `<span class="na">n/a</span>`;
  const side = String(t.side).toUpperCase();
  const cls = side === "BUY" ? "trade-buy" : side === "SELL" ? "trade-sell" : "";
  return `<span class="${cls}">${side}</span> ${fmtShares(t.size)} <span class="muted">${t.outcome || ""} · ${fmtWhen(t.timestamp)}</span>`;
}

/** Donut: green = Yes share of total, red = No. */
function shareDonut(yesSize, noSize) {
  const total = yesSize + noSize;
  if (!(total > 0)) {
    return `<span class="donut donut-empty" title="n/a" aria-hidden="true"></span>`;
  }
  const yesPct = (yesSize / total) * 100;
  const title = `Yes ${fmtShares(yesSize)} (${yesPct.toFixed(0)}%) · No ${fmtShares(noSize)} (${(100 - yesPct).toFixed(0)}%)`;
  return `<span class="donut" title="${title}" style="--yes-pct:${yesPct.toFixed(2)}" aria-label="${title}"></span>`;
}

async function fetchLastTrade(wallet, conditionId, signal) {
  const q = new URLSearchParams({
    user: wallet,
    market: conditionId,
    limit: "1",
  });
  const trades = await getJSON(`${DATA}/trades?${q}`, signal);
  if (!Array.isArray(trades) || !trades.length) {
    return { side: "", size: 0, timestamp: 0, outcome: "", status: "empty" };
  }
  const t = trades[0];
  return {
    side: String(t.side || ""),
    size: Number(t.size) || 0,
    timestamp: Number(t.timestamp) || 0,
    outcome: String(t.outcome || ""),
    status: "ready",
  };
}

async function fillLastTrades(market, signal) {
  const all = [...(market.yes_holders || []), ...(market.no_holders || [])];
  const need = all.filter((h) => !h.last_trade || h.last_trade.status === "pending");
  if (!need.length) return;
  for (const h of need) {
    h.last_trade = { side: "", size: 0, timestamp: 0, outcome: "", status: "pending" };
  }
  await mapPool(need, PNL_CONCURRENCY, async (h) => {
    try {
      h.last_trade = await fetchLastTrade(h.wallet, market.condition_id, signal);
    } catch (e) {
      if (e?.name === "AbortError") throw e;
      h.last_trade = { side: "", size: 0, timestamp: 0, outcome: "", status: "error" };
    }
  }, signal);
}

function filteredRows() {
  const needle = $("q").value.trim().toLowerCase();
  let list = rows;
  if (needle) {
    list = list.filter((m) => {
      const hay = [m.question, m.slug, m.event_title, m.event_slug].join(" ").toLowerCase();
      return hay.includes(needle);
    });
  }
  const dir = sortDir === "asc" ? 1 : -1;
  const key = sortKey;
  const strongerRank = { yes: 1, no: -1, tie: 0 };
  return [...list].sort((a, b) => {
    let av = a[key];
    let bv = b[key];
    if (key === "stronger") {
      av = strongerRank[a.stronger] ?? 0;
      bv = strongerRank[b.stronger] ?? 0;
      if (a.strength_status !== "ready") av = null;
      if (b.strength_status !== "ready") bv = null;
    }
    const aNull = av == null || (typeof av === "number" && Number.isNaN(av));
    const bNull = bv == null || (typeof bv === "number" && Number.isNaN(bv));
    if (aNull && bNull) return 0;
    if (aNull) return 1;
    if (bNull) return -1;
    if (av === bv) return b.volume_24hr - a.volume_24hr;
    return av > bv ? dir : -dir;
  });
}

function setSortHeaders() {
  document.querySelectorAll("th.sortable").forEach((th) => {
    const key = th.getAttribute("data-key");
    const active = key === sortKey;
    th.classList.toggle("active", active);
    th.dataset.dir = active ? (sortDir === "asc" ? "↑" : "↓") : "";
  });
}

function holdersTable(sideLabel, holders) {
  const rowsHtml = holders.length
    ? holders.map((h) =>
      `<tr>` +
      `<td class="trader"><a href="https://polymarket.com/profile/${h.wallet}" target="_blank" rel="noopener noreferrer" title="${h.wallet}">${fmtTrader(h)}</a></td>` +
      `<td class="num">${fmtShares(h.size)}</td>` +
      `<td class="num">${fmtPnl(h.pnl)}</td>` +
      `<td class="last-trade">${fmtLastTrade(h.last_trade)}</td>` +
      `</tr>`
    ).join("")
    : `<tr><td colspan="4" class="na">No &gt;$100k holders</td></tr>`;

  return (
    `<div class="side-block side-${sideLabel.toLowerCase()}">` +
    `<div class="side-label">${sideLabel}</div>` +
    `<table class="holders">` +
    `<thead><tr><th>Trader</th><th class="num">Shares</th><th class="num">Lifetime PnL</th><th>Last trade</th></tr></thead>` +
    `<tbody>${rowsHtml}</tbody>` +
    `</table>` +
    `</div>`
  );
}

function detailRow(m, colSpan) {
  const tr = document.createElement("tr");
  tr.className = "detail-row";
  tr.dataset.id = m.condition_id;
  if (m.strength_status !== "ready") {
    tr.innerHTML = `<td colspan="${colSpan}"><div class="detail">Holders not ready.</div></td>`;
    return tr;
  }
  tr.innerHTML =
    `<td colspan="${colSpan}">` +
    `<div class="detail">` +
    holdersTable("Yes", m.yes_holders || []) +
    holdersTable("No", m.no_holders || []) +
    `</div>` +
    `</td>`;
  return tr;
}

function render() {
  setSortHeaders();
  const list = filteredRows();
  const ready = rows.filter((r) => r.strength_status === "ready" || r.strength_status === "empty").length;
  $("meta").innerHTML =
    `<span>loaded <b>${rows.length}</b></span>` +
    `<span>showing <b>${list.length}</b></span>` +
    `<span>holders <b>${ready}/${rows.length}</b></span>` +
    `<span>sort <b>${sortKey} ${sortDir}</b></span>`;

  const body = $("tbody");
  body.innerHTML = "";
  if (!list.length) {
    $("empty").hidden = false;
    return;
  }
  $("empty").hidden = true;
  const frag = document.createDocumentFragment();
  const colSpan = 7;

  for (const m of list) {
    const tr = document.createElement("tr");
    tr.className = "market-row" + (expanded.has(m.condition_id) ? " open" : "");
    tr.dataset.id = m.condition_id;
    tr.tabIndex = 0;

    let sharesCell = `<span class="pending">…</span>`;
    let tradersCell = `<span class="pending">…</span>`;
    let donut = `<span class="donut donut-empty" aria-hidden="true"></span>`;
    if (m.strength_status === "ready") {
      sharesCell = `${fmtShares(m.yes_qualified_size)} / ${fmtShares(m.no_qualified_size)}`;
      tradersCell = `${m.yes_qualified_count} / ${m.no_qualified_count}`;
      donut = shareDonut(m.yes_qualified_size, m.no_qualified_size);
    } else if (m.strength_status === "empty") {
      sharesCell = `<span class="na">n/a</span>`;
      tradersCell = `<span class="na">n/a</span>`;
    } else if (m.strength_status === "error") {
      sharesCell = `<span class="na">err</span>`;
      tradersCell = `<span class="na">err</span>`;
    }

    tr.innerHTML =
      `<td class="expand"><span class="chev" aria-hidden="true"></span></td>` +
      `<td class="num">${fmtVol(m.volume_24hr)}</td>` +
      `<td class="market"><a href="${m.url}" target="_blank" rel="noopener noreferrer"></a><span class="evt"></span></td>` +
      `<td class="num hide-sm">${fmtPrice(m)}</td>` +
      `<td class="num"><span class="shares-wrap"><span class="shares-text">${sharesCell}</span>${donut}</span></td>` +
      `<td class="num">${tradersCell}</td>` +
      `<td class="num hide-sm">${m.stronger === "tie" && m.strength_status !== "ready" ? "—" : m.stronger}</td>`;
    tr.querySelector("a").textContent = m.question;
    tr.querySelector(".evt").textContent = m.event_title || m.event_slug || "";
    frag.appendChild(tr);

    if (expanded.has(m.condition_id)) {
      frag.appendChild(detailRow(m, colSpan));
    }
  }
  body.appendChild(frag);
}

async function toggleExpand(id) {
  if (!id) return;
  if (expanded.has(id)) {
    expanded.delete(id);
    render();
    return;
  }
  expanded.add(id);
  render();
  const market = rows.find((r) => r.condition_id === id);
  if (!market || market.strength_status !== "ready") return;
  const signal = strengthAbort?.signal;
  try {
    await fillLastTrades(market, signal);
    if (expanded.has(id)) render();
  } catch (e) {
    if (e?.name !== "AbortError" && expanded.has(id)) render();
  }
}

async function fillStrength(signal) {
  await mapPool(rows, STRENGTH_CONCURRENCY, async (row) => {
    try {
      const s = await computeStrength(row.condition_id, signal);
      Object.assign(row, s);
    } catch (e) {
      if (e?.name === "AbortError") throw e;
      row.strength_status = "error";
      row.yes_holders = [];
      row.no_holders = [];
    }
    render();
  }, signal);
}

async function load() {
  const err = $("err");
  err.hidden = true;
  if (strengthAbort) strengthAbort.abort();
  strengthAbort = new AbortController();
  const { signal } = strengthAbort;

  $("load").disabled = true;
  $("load").textContent = "Loading…";
  rows = [];
  expanded.clear();
  render();

  try {
    const minVol = Number($("minVol").value) || 0;
    const tag = $("tag").value;
    const { tag: tagInfo, markets } = await listMarkets({
      tagSlug: tag,
      minVolume24: minVol,
      signal,
    });
    rows = markets;
    $("tagLabel").textContent = `${tagInfo.label} (${tagInfo.id})`;
    render();
    $("load").textContent = "Holders…";
    await fillStrength(signal);
  } catch (e) {
    if (e?.name !== "AbortError") {
      err.hidden = false;
      err.textContent = String(e.message || e);
    }
  } finally {
    $("load").disabled = false;
    $("load").textContent = "Load";
  }
}

function bindSort() {
  document.querySelectorAll("th.sortable").forEach((th) => {
    th.addEventListener("click", () => {
      const key = th.getAttribute("data-key");
      if (sortKey === key) sortDir = sortDir === "desc" ? "asc" : "desc";
      else {
        sortKey = key;
        sortDir = key === "question" ? "asc" : "desc";
      }
      render();
    });
  });
}

$("tbody").addEventListener("click", (e) => {
  const t = /** @type {HTMLElement} */ (e.target);
  if (t.closest("a")) return;
  const row = t.closest("tr.market-row");
  if (!row) return;
  toggleExpand(row.dataset.id);
});

$("tbody").addEventListener("keydown", (e) => {
  if (e.key !== "Enter" && e.key !== " ") return;
  const row = /** @type {HTMLElement} */ (e.target).closest?.("tr.market-row");
  if (!row) return;
  e.preventDefault();
  toggleExpand(row.dataset.id);
});

$("q").addEventListener("input", render);
$("load").addEventListener("click", load);
$("tag").addEventListener("change", load);
$("minVol").addEventListener("keydown", (e) => {
  if (e.key === "Enter") load();
});
bindSort();
load();
