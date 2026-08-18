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
 * @property {number} price
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
 * @property {number} net_shares_abs
 * @property {number} net_traders_abs
 * @property {number|null} net_strength_abs
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
      const icon = String(m.icon || m.image || event.icon || event.image || "").trim();
      out.push({
        condition_id: m.conditionId,
        slug,
        question: m.question || slug,
        event_slug: event.slug || "",
        event_title: event.title || "",
        icon,
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
        net_shares_abs: 0,
        net_traders_abs: 0,
        net_strength_abs: null,
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
      net_shares_abs: 0,
      net_traders_abs: 0,
      net_strength_abs: null,
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

  /** @type {Map<string, {wallet:string, name:string, yes:number, no:number, pnl:number}>} */
  const byWallet = new Map();
  for (const h of holders) {
    const pnl = pnls.get(h.wallet);
    if (pnl == null || !(pnl > MIN_PNL)) continue;
    let row = byWallet.get(h.wallet);
    if (!row) {
      row = { wallet: h.wallet, name: h.name, yes: 0, no: 0, pnl };
      byWallet.set(h.wallet, row);
    }
    if (h.name && !row.name) row.name = h.name;
    if (h.side === "yes") row.yes += h.size;
    else row.no += h.size;
  }

  /** @type {QualifiedHolder[]} */
  const yesHolders = [];
  /** @type {QualifiedHolder[]} */
  const noHolders = [];
  for (const row of byWallet.values()) {
    const net = row.yes - row.no;
    if (net === 0) continue;
    const entry = {
      wallet: row.wallet,
      name: row.name,
      size: Math.abs(net),
      pnl: row.pnl,
      last_trade: null,
    };
    if (net > 0) yesHolders.push(entry);
    else noHolders.push(entry);
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
    net_shares_abs: Math.abs(yesSize - noSize),
    net_traders_abs: Math.abs(yesHolders.length - noHolders.length),
    net_strength_abs: yes == null || no == null ? null : Math.abs(yes - no),
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
  const raw = h.name || fmtWallet(h.wallet);
  if (raw.length <= 25) return raw;
  return `${raw.slice(0, 24)}…`;
}

function fmtPrice(m) {
  const p = m.outcome_prices || [];
  if (p.length < 2) return "—";
  return `${Math.round(p[0] * 100)}¢ / ${Math.round(p[1] * 100)}¢`;
}

function fmtPct(x) {
  if (x == null || Number.isNaN(x)) return "—";
  return (x * 100).toFixed(1) + "%";
}

function fmtWhen(ts) {
  if (!ts) return "—";
  const ms = ts > 1e12 ? ts : ts * 1000;
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return "—";
  const hr = Math.max(0, Math.round((Date.now() - d.getTime()) / 3600000));
  if (hr < 48) return `${hr}h`;
  return `${Math.round(hr / 24)}d`;
}

function fmtTradePrice(p) {
  if (p == null || Number.isNaN(p)) return "";
  return `${Math.round(p * 100)}¢`;
}

function fmtLastTrade(t) {
  if (!t || t.status === "pending") return `<span class="pending">…</span>`;
  if (t.status === "error") return `<span class="na">err</span>`;
  if (t.status === "empty" || !t.side) return `<span class="na">n/a</span>`;
  const side = String(t.side).toUpperCase();
  const cls = side === "BUY" ? "trade-buy" : side === "SELL" ? "trade-sell" : "";
  const px = fmtTradePrice(t.price);
  const meta = [t.outcome || "", px, fmtWhen(t.timestamp)].filter(Boolean).join(" · ");
  return `<span class="${cls}">${side}</span> ${fmtShares(t.size)} <span class="muted">${meta}</span>`;
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
    return { side: "", size: 0, price: 0, timestamp: 0, outcome: "", status: "empty" };
  }
  const t = trades[0];
  return {
    side: String(t.side || ""),
    size: Number(t.size) || 0,
    price: Number(t.price) || 0,
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
    h.last_trade = { side: "", size: 0, price: 0, timestamp: 0, outcome: "", status: "pending" };
  }
  await mapPool(need, PNL_CONCURRENCY, async (h) => {
    try {
      h.last_trade = await fetchLastTrade(h.wallet, market.condition_id, signal);
    } catch (e) {
      if (e?.name === "AbortError") throw e;
      h.last_trade = { side: "", size: 0, price: 0, timestamp: 0, outcome: "", status: "error" };
    }
  }, signal);
}

function favoredSharpCount(m) {
  if (m.strength_status !== "ready") return 0;
  if (m.stronger === "yes") return m.yes_qualified_count || 0;
  if (m.stronger === "no") return m.no_qualified_count || 0;
  // Tie: require sharps on either side meeting the bar via max
  return Math.max(m.yes_qualified_count || 0, m.no_qualified_count || 0);
}

function tradeTimestampMs(t) {
  if (!t || !t.timestamp) return 0;
  return t.timestamp > 1e12 ? t.timestamp : t.timestamp * 1000;
}

function recentSharpCount(m, withinHours) {
  const cutoff = Date.now() - withinHours * 3600 * 1000;
  const all = [...(m.yes_holders || []), ...(m.no_holders || [])];
  let n = 0;
  for (const h of all) {
    const t = h.last_trade;
    if (!t || t.status !== "ready") continue;
    if (tradeTimestampMs(t) >= cutoff) n += 1;
  }
  return n;
}

function maxOutcomeCents(m) {
  const p = m.outcome_prices || [];
  if (p.length < 2) return null;
  return Math.max(p[0], p[1]) * 100;
}

function passesSharpFilters(m) {
  const minFavored = Math.max(0, Number($("minFavoredSharps").value) || 0);
  const minRecent = Math.max(0, Number($("minRecentSharps").value) || 0);
  const hours = Math.max(1, Number($("recentHours").value) || 24);
  const maxPrice = Number($("maxPriceCts").value);

  if (Number.isFinite(maxPrice) && maxPrice > 0) {
    const cts = maxOutcomeCents(m);
    if (cts == null || cts > maxPrice) return false;
  }

  if (minFavored > 0) {
    if (m.strength_status !== "ready") return false;
    if (favoredSharpCount(m) < minFavored) return false;
  }
  if (minRecent > 0) {
    if (m.strength_status !== "ready") return false;
    if (recentSharpCount(m, hours) < minRecent) return false;
  }
  return true;
}

/** Absolute imbalance for Shares / Strength / Traders (direction-agnostic). */
function netAbsForSort(m, key) {
  if (m.strength_status !== "ready") return null;
  if (key === "net_shares_abs") {
    return Math.abs((m.yes_qualified_size || 0) - (m.no_qualified_size || 0));
  }
  if (key === "net_traders_abs") {
    return Math.abs((m.yes_qualified_count || 0) - (m.no_qualified_count || 0));
  }
  if (key === "net_strength_abs") {
    if (m.yes_strength == null || m.no_strength == null) return null;
    return Math.abs(m.yes_strength - m.no_strength);
  }
  return m[key];
}

function filteredRows() {
  const needle = $("q").value.trim().toLowerCase();
  let list = rows.filter(passesSharpFilters);
  if (needle) {
    list = list.filter((m) => {
      const hay = [m.question, m.slug, m.event_title, m.event_slug].join(" ").toLowerCase();
      return hay.includes(needle);
    });
  }
  const dir = sortDir === "asc" ? 1 : -1;
  const key = sortKey;
  const strongerRank = { yes: 1, no: -1, tie: 0 };
  const netKeys = new Set(["net_shares_abs", "net_traders_abs", "net_strength_abs"]);

  return [...list].sort((a, b) => {
    let av;
    let bv;
    if (key === "stronger") {
      av = strongerRank[a.stronger] ?? 0;
      bv = strongerRank[b.stronger] ?? 0;
      if (a.strength_status !== "ready") av = null;
      if (b.strength_status !== "ready") bv = null;
    } else if (netKeys.has(key)) {
      av = netAbsForSort(a, key);
      bv = netAbsForSort(b, key);
    } else {
      av = a[key];
      bv = b[key];
    }

    const aNull = av == null || (typeof av === "number" && Number.isNaN(av));
    const bNull = bv == null || (typeof bv === "number" && Number.isNaN(bv));
    if (aNull && bNull) return 0;
    if (aNull) return 1;
    if (bNull) return -1;
    if (av !== bv) return av > bv ? dir : -dir;

    // Tie-break net sorts: larger majority side, then more total, then volume
    if (netKeys.has(key)) {
      const aMaj = key === "net_traders_abs"
        ? Math.max(a.yes_qualified_count || 0, a.no_qualified_count || 0)
        : key === "net_shares_abs"
          ? Math.max(a.yes_qualified_size || 0, a.no_qualified_size || 0)
          : Math.max(a.yes_strength || 0, a.no_strength || 0);
      const bMaj = key === "net_traders_abs"
        ? Math.max(b.yes_qualified_count || 0, b.no_qualified_count || 0)
        : key === "net_shares_abs"
          ? Math.max(b.yes_qualified_size || 0, b.no_qualified_size || 0)
          : Math.max(b.yes_strength || 0, b.no_strength || 0);
      if (aMaj !== bMaj) return aMaj > bMaj ? dir : -dir;

      const aTot = key === "net_traders_abs"
        ? (a.yes_qualified_count || 0) + (a.no_qualified_count || 0)
        : key === "net_shares_abs"
          ? (a.yes_qualified_size || 0) + (a.no_qualified_size || 0)
          : 1;
      const bTot = key === "net_traders_abs"
        ? (b.yes_qualified_count || 0) + (b.no_qualified_count || 0)
        : key === "net_shares_abs"
          ? (b.yes_qualified_size || 0) + (b.no_qualified_size || 0)
          : 1;
      if (aTot !== bTot) return aTot > bTot ? dir : -dir;
    }

    return b.volume_24hr - a.volume_24hr;
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
    `<thead><tr><th>Trader</th><th class="num">Net shares</th><th class="num">Lifetime PnL</th><th>Last trade</th></tr></thead>` +
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

/** eventSlug → icon URL for markets missing market-level icons. */
const eventIconCache = new Map();

function marketIconHtml(url) {
  if (!url) return '<span class="mkt-icon mkt-icon-empty" aria-hidden="true"></span>';
  const safe = String(url).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
  return `<img class="mkt-icon" src="${safe}" alt="" width="32" height="32" loading="lazy" decoding="async" referrerpolicy="no-referrer" />`;
}

async function fetchEventIcon(eventSlug, signal) {
  if (!eventSlug) return "";
  if (eventIconCache.has(eventSlug)) return eventIconCache.get(eventSlug) || "";
  try {
    const data = await getJSON(`${GAMMA}/events?slug=${encodeURIComponent(eventSlug)}`, signal);
    const e = Array.isArray(data) ? data[0] : data;
    const icon = String(e?.icon || e?.image || "").trim();
    eventIconCache.set(eventSlug, icon);
    return icon;
  } catch {
    eventIconCache.set(eventSlug, "");
    return "";
  }
}

async function fillMissingMarketIcons(items, signal) {
  const slugs = [...new Set(
    items.filter((i) => !i.icon && (i.event_slug || i.eventSlug)).map((i) => i.event_slug || i.eventSlug),
  )];
  if (!slugs.length) return;
  const conc = 6;
  for (let i = 0; i < slugs.length; i += conc) {
    await Promise.all(slugs.slice(i, i + conc).map((s) => fetchEventIcon(s, signal)));
  }
  for (const i of items) {
    if (!i.icon) {
      const slug = i.event_slug || i.eventSlug;
      const icon = slug ? eventIconCache.get(slug) : "";
      if (icon) i.icon = icon;
    }
  }
}

function strengthSummaryHtml(m) {
  if (!m || m.strength_status === "pending") {
    return `<div class="strength-summary"><span class="pending">Loading sharps…</span></div>`;
  }
  if (m.strength_status === "error") {
    return `<div class="strength-summary"><span class="na">Could not load sharps.</span></div>`;
  }
  if (m.strength_status === "empty") {
    return `<div class="strength-summary"><span class="na">No &gt;$100k holders</span></div>`;
  }
  const donut = shareDonut(m.yes_qualified_size, m.no_qualified_size);
  return (
    `<div class="strength-summary">` +
    `<span class="shares-wrap">Shares <b>${fmtShares(m.yes_qualified_size)} / ${fmtShares(m.no_qualified_size)}</b>${donut}</span>` +
    `<span>Strength <b>${fmtPct(m.yes_strength)} / ${fmtPct(m.no_strength)}</b></span>` +
    `<span>Traders <b>${m.yes_qualified_count} / ${m.no_qualified_count}</b></span>` +
    `<span>Side <b>${m.stronger}</b></span>` +
    `</div>`
  );
}

function strengthDetailHtml(m) {
  let html = strengthSummaryHtml(m);
  if (m && m.strength_status === "ready") {
    html +=
      `<div class="detail strength-detail">` +
      holdersTable("Yes", m.yes_holders || []) +
      holdersTable("No", m.no_holders || []) +
      `</div>`;
  }
  return html;
}

async function loadStrengthMarket(conditionId, signal) {
  const s = await computeStrength(conditionId, signal);
  const market = { condition_id: conditionId, ...s };
  if (market.strength_status === "ready") {
    await fillLastTrades(market, signal);
  }
  return market;
}

/** Shared with Portfolio / Activity expand panels. */
window.DetectorStrength = {
  load: loadStrengthMarket,
  summaryHtml: strengthSummaryHtml,
  detailHtml: strengthDetailHtml,
};

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
  const colSpan = 8;

  for (const m of list) {
    const tr = document.createElement("tr");
    tr.className = "market-row" + (expanded.has(m.condition_id) ? " open" : "");
    tr.dataset.id = m.condition_id;
    tr.tabIndex = 0;

    let sharesCell = `<span class="pending">…</span>`;
    let strengthCell = `<span class="pending">…</span>`;
    let tradersCell = `<span class="pending">…</span>`;
    let donut = `<span class="donut donut-empty" aria-hidden="true"></span>`;
    if (m.strength_status === "ready") {
      sharesCell = `${fmtShares(m.yes_qualified_size)} / ${fmtShares(m.no_qualified_size)}`;
      strengthCell = `${fmtPct(m.yes_strength)} / ${fmtPct(m.no_strength)}`;
      tradersCell = `${m.yes_qualified_count} / ${m.no_qualified_count}`;
      donut = shareDonut(m.yes_qualified_size, m.no_qualified_size);
    } else if (m.strength_status === "empty") {
      sharesCell = `<span class="na">n/a</span>`;
      strengthCell = `<span class="na">n/a</span>`;
      tradersCell = `<span class="na">n/a</span>`;
    } else if (m.strength_status === "error") {
      sharesCell = `<span class="na">err</span>`;
      strengthCell = `<span class="na">err</span>`;
      tradersCell = `<span class="na">err</span>`;
    }

    tr.innerHTML =
      `<td class="expand"><span class="chev" aria-hidden="true"></span></td>` +
      `<td class="num">${fmtVol(m.volume_24hr)}</td>` +
      `<td class="market"><span class="mkt-line">${marketIconHtml(m.icon)}<span class="mkt-text"><a class="mkt-title" href="${m.url}" target="_blank" rel="noopener noreferrer"></a><span class="evt"></span></span></span></td>` +
      `<td class="num hide-sm">${fmtPrice(m)}</td>` +
      `<td class="num"><span class="shares-wrap"><span class="shares-text">${sharesCell}</span>${donut}</span></td>` +
      `<td class="num">${strengthCell}</td>` +
      `<td class="num">${tradersCell}</td>` +
      `<td class="num hide-sm">${m.stronger === "tie" && m.strength_status !== "ready" ? "—" : m.stronger}</td>`;
    tr.querySelector("a.mkt-title").textContent = m.question;
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
      if (row.strength_status === "ready") {
        await fillLastTrades(row, signal);
      }
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
    await fillMissingMarketIcons(rows, signal);
    $("tagLabel").textContent = `${tagInfo.label} (${tagInfo.id})`;
    render();
    $("load").textContent = "Sharps…";
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
  document.querySelectorAll("#tab-markets th.sortable").forEach((th) => {
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
$("minFavoredSharps").addEventListener("input", render);
$("minRecentSharps").addEventListener("input", render);
$("recentHours").addEventListener("input", render);
$("maxPriceCts").addEventListener("input", render);
$("load").addEventListener("click", load);
$("tag").addEventListener("change", load);
$("minVol").addEventListener("keydown", (e) => {
  if (e.key === "Enter") load();
});
bindSort();
load();

window.DetectorCatalog = {
  listMarkets,
  fillMissingMarketIcons,
  marketIconHtml,
  fmtVol,
  fmtPrice,
  fmtShares,
  shareDonut,
  getJSON,
  mapPool,
};
