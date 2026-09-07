/* Tracked sharps — portfolio + activity tabs.
 * Data: GET /positions, /activity, /trades on data-api.polymarket.com (public).
 */
(function () {
  const DATA = "https://data-api.polymarket.com";
  const GAMMA = "https://gamma-api.polymarket.com";
  const PORT_COLS = 7;
  const ACT_COLS = 8;

  /** Add wallets here — names resolve from Polymarket leaderboard when possible. */
  /** @type {Array<{wallet:string, name?:string}>} */
  const TRACKED = [
    { wallet: "0x23d81ba9371e576015c1e562db09c689f56b0288", name: "flawfence" },
    { wallet: "0x614dc8d3542c12103d2c6a3553fd761e391d1546", name: "mr.ozi" },
    { wallet: "0x7bc14171ccb0d3e6bac219ec6a76211826e28db4", name: "coali10" },
    { wallet: "0x2b9dbf4b6e0e11309a9d6d2a09b72f65f652adc0", name: "seal7" },
    { wallet: "0xc851cd9bee7d262afd78674f861f9f576a12cd2a", name: "betwick" },
    { wallet: "0x1cc16713196d456f86fa9c7387dd326a7f73b8df", name: "Wickier" },
    { wallet: "0xde7be6d489bce070a959e0cb813128ae659b5f4b", name: "wan123" },
    { wallet: "0xc8b9a30184244d427169cf62485dde6041b2b836", name: "SnowLover7" },
    { wallet: "0x55291dc2069439a6de5c93a9bec8da2215a9e5b9", name: "i2dt" },
    { wallet: "0xbaa2bcb5439e985ce4ccf815b4700027d1b92c73", name: "denizz" },
    { wallet: "0xd24b95551eb288ff82bb625dcd7f32f62abdef76", name: "BiDiFakePolls" },
    { wallet: "0x8a4c788f043023b8b28a762216d037e9f148532b", name: "occasionalAwareness" },
    { wallet: "0x448861155279dbf833d041b963e3ac854599e319", name: "Flipadelphia" },
  ];

  const $ = (id) => document.getElementById(id);

  const WALLET_CONCURRENCY = 6;
  const CACHE_PORT = "detector.port.v1";
  const CACHE_ACT = "detector.act.v1";
  const CACHE_TTL_MS = 120_000;
  const CACHE_STALE_MS = 600_000;

  /** @type {Map<string, {wallet:string, name:string, pnl?:number, vol?:number, profileImage?:string}>} */
  const profiles = new Map();

  let portAbort = null;
  let actAbort = null;

  /** eventSlug → {icon, isSports}. Multi-outcome markets often omit market.icon. */
  const eventMetaCache = new Map();

  /** @type {Array<object>} */
  let portRows = [];
  let portSort = "currentValue";
  let portSortDir = "desc";
  let portLoaded = false;

  /** Expanded portfolio position keys */
  const expandedPorts = new Set();
  /** Expanded holder keys: `${positionKey}|${wallet}` */
  const expandedHolders = new Set();
  /** @type {Map<string, {status:string, trades:object[]}>} */
  const holderTrades = new Map();

  /** Expanded activity row keys */
  const expandedActs = new Set();
  /** conditionId → strength market object from DetectorStrength */
  const strengthCache = new Map();
  /** conditionId → in-flight load promise */
  const strengthPromises = new Map();

  /** @type {Array<object>} */
  let actRows = [];
  /** Pre-aggregated activity rows — rebuilt when actRows changes. */
  let actAggregated = [];
  let actLoaded = false;

  function mapPool(items, concurrency, fn, signal) {
    const pool = window.DetectorCatalog?.mapPool;
    if (pool) return pool(items, concurrency, fn, signal);
    return Promise.all(items.map((item, idx) => fn(item, idx)));
  }

  function readCache(key) {
    try {
      const raw = sessionStorage.getItem(key);
      if (!raw) return null;
      const { ts, data } = JSON.parse(raw);
      return { data, age: Date.now() - ts };
    } catch {
      return null;
    }
  }

  function writeCache(key, data) {
    try {
      sessionStorage.setItem(key, JSON.stringify({ ts: Date.now(), data }));
    } catch {
      /* quota */
    }
  }

  async function getJSON(url, signal) {
    const res = await fetch(url, { signal });
    if (!res.ok) throw new Error(`${res.status} ${url}`);
    return res.json();
  }

  function shortName(s, max = 25) {
    const t = String(s || "").trim();
    if (!t) return "—";
    return t.length <= max ? t : `${t.slice(0, max - 1)}…`;
  }

  function fmtWallet(w) {
    if (!w || w.length < 12) return w || "—";
    return `${w.slice(0, 6)}…${w.slice(-4)}`;
  }

  function fmtUsd(n) {
    if (n == null || Number.isNaN(n)) return "—";
    const sign = n < 0 ? "-" : "";
    const abs = Math.abs(n);
    if (abs >= 1e6) return `${sign}$${(abs / 1e6).toFixed(2)}M`;
    if (abs >= 1e3) return `${sign}$${(abs / 1e3).toFixed(1)}k`;
    return `${sign}$${abs.toFixed(abs >= 10 ? 0 : 2)}`;
  }

  function fmtShares(n) {
    if (n == null || Number.isNaN(n)) return "—";
    if (n >= 1e6) return (n / 1e6).toFixed(2) + "M";
    if (n >= 1e3) return (n / 1e3).toFixed(1) + "k";
    return n.toFixed(n >= 10 ? 0 : 1);
  }

  function fmtCts(p) {
    if (p == null || Number.isNaN(p)) return "—";
    return `${Math.round(p * 100)}¢`;
  }

  function fmtWhen(ts) {
    if (!ts) return "—";
    const ms = ts > 1e12 ? ts : ts * 1000;
    const d = new Date(ms);
    if (Number.isNaN(d.getTime())) return "—";
    const mins = Math.max(0, Math.round((Date.now() - d.getTime()) / 60000));
    if (mins < 60) return `${mins}m`;
    const hr = Math.round(mins / 60);
    if (hr < 48) return `${hr}h`;
    return `${Math.round(hr / 24)}d`;
  }

  function marketUrl(p) {
    if (p.eventSlug && p.slug) return `https://polymarket.com/event/${p.eventSlug}/${p.slug}`;
    if (p.eventSlug) return `https://polymarket.com/event/${p.eventSlug}`;
    if (p.slug) return `https://polymarket.com/market/${p.slug}`;
    return `https://polymarket.com/profile/${p.proxyWallet || ""}`;
  }

  function marketIcon(url) {
    if (!url) return '<span class="mkt-icon mkt-icon-empty" aria-hidden="true"></span>';
    const safe = String(url).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
    return `<img class="mkt-icon" src="${safe}" alt="" width="32" height="32" loading="lazy" decoding="async" referrerpolicy="no-referrer" />`;
  }

  function resolvedIcon(p) {
    if (p?.icon) return p.icon;
    if (p?.eventSlug && eventMetaCache.has(p.eventSlug)) {
      return eventMetaCache.get(p.eventSlug)?.icon || "";
    }
    return "";
  }

  async function fetchEventMeta(eventSlug, signal) {
    if (!eventSlug) return { icon: "", isSports: false };
    if (eventMetaCache.has(eventSlug)) return eventMetaCache.get(eventSlug);
    try {
      const data = await getJSON(`${GAMMA}/events?slug=${encodeURIComponent(eventSlug)}`, signal);
      const e = Array.isArray(data) ? data[0] : data;
      const icon = String(e?.icon || e?.image || "").trim();
      const tags = Array.isArray(e?.tags) ? e.tags : [];
      const isSports = tags.some((t) => String(t?.slug || "").toLowerCase() === "sports");
      const meta = { icon, isSports };
      eventMetaCache.set(eventSlug, meta);
      return meta;
    } catch {
      const meta = { icon: "", isSports: false };
      eventMetaCache.set(eventSlug, meta);
      return meta;
    }
  }

  async function prefetchEventMeta(slugs, signal) {
    const need = [...new Set(slugs.filter((s) => s && !eventMetaCache.has(s)))];
    if (!need.length) return;
    const conc = 6;
    for (let i = 0; i < need.length; i += conc) {
      await Promise.all(need.slice(i, i + conc).map((s) => fetchEventMeta(s, signal)));
    }
  }

  function isSportsItem(item) {
    const slug = item?.eventSlug;
    if (!slug) return false;
    const meta = eventMetaCache.get(slug);
    return meta?.isSports === true;
  }

  /** Resolve event icons and sports tags from Gamma (needed for icon fallback + filtering). */
  async function ensureEventMeta(items, signal) {
    const slugs = [...new Set(items.filter((i) => i.eventSlug).map((i) => i.eventSlug))];
    if (!slugs.length) return;
    await prefetchEventMeta(slugs, signal);
    for (const i of items) {
      if (!i.icon && i.eventSlug) {
        const icon = eventMetaCache.get(i.eventSlug)?.icon;
        if (icon) i.icon = icon;
      }
    }
  }

  /** Resolve event meta in the background after first render. */
  function ensureEventMetaLater(items, signal, onDone) {
    if (!items.length) return;
    ensureEventMeta(items, signal).then(() => {
      if (!signal?.aborted) onDone();
    }).catch(() => {});
  }

  function outcomeLabel(outcome) {
    const o = String(outcome || "").trim();
    const low = o.toLowerCase();
    if (low === "yes") return '<span class="mkt-out yes">Yes</span>';
    if (low === "no") return '<span class="mkt-out no">No</span>';
    if (!o || o === "—") return '<span class="mkt-out">—</span>';
    const safe = o.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/"/g, "&quot;");
    return `<span class="mkt-out">${safe}</span>`;
  }

  /** Compact ¢ price shown on mobile rows (desktop keeps the dedicated column). */
  function mobilePriceHtml(p, label) {
    if (p == null || Number.isNaN(Number(p))) return "";
    const title = label ? ` title="${label}"` : "";
    return `<span class="mkt-px"${title}>${fmtCts(Number(p))}</span>`;
  }

  /** Polymarket-style: circular icon + title + colored Yes/No under it. */
  function marketCellHtml(p, linkClass, mobilePrice, priceLabel) {
    const cls = linkClass ? ` ${linkClass}` : "";
    return (
      `<span class="mkt-line">` +
      marketIcon(resolvedIcon(p)) +
      `<span class="mkt-text">` +
      `<a class="mkt-title${cls}" href="${marketUrl(p)}" target="_blank" rel="noopener noreferrer"></a>` +
      `<span class="mkt-meta">${outcomeLabel(p.outcome)}${mobilePriceHtml(mobilePrice, priceLabel)}</span>` +
      `</span></span>`
    );
  }

  function outcomeBadge(outcome) {
    const o = String(outcome || "").trim().toLowerCase();
    if (o === "yes") return '<span class="yn yn-y" title="Yes">Y</span>';
    if (o === "no") return '<span class="yn yn-n" title="No">N</span>';
    const letter = String(outcome || "?").trim().charAt(0).toUpperCase() || "?";
    return `<span class="yn yn-other" title="${String(outcome || "").replace(/"/g, "&quot;")}">${letter}</span>`;
  }

  function holderKey(posKey, wallet) {
    return `${posKey}|${String(wallet || "").toLowerCase()}`;
  }

  function actRowKey(a) {
    return [
      String(a.proxyWallet || "").toLowerCase(),
      String(a.conditionId || ""),
      String(a.side || "").toUpperCase(),
      String(a.outcome || "").toLowerCase(),
      String(a.timestamp || ""),
    ].join("|");
  }

  function strengthBlockHtml(conditionId) {
    if (!conditionId) {
      return `<div class="strength-panel"><span class="na">No market id</span></div>`;
    }
    const api = window.DetectorStrength;
    if (!api) {
      return `<div class="strength-panel"><span class="na">Strength module unavailable</span></div>`;
    }
    const cached = strengthCache.get(conditionId);
    if (!cached) {
      return `<div class="strength-panel">${api.summaryHtml({ strength_status: "pending" })}</div>`;
    }
    return `<div class="strength-panel">${api.detailHtml(cached)}</div>`;
  }

  async function ensureStrength(conditionId) {
    if (!conditionId || !window.DetectorStrength) return null;
    if (strengthCache.has(conditionId)) return strengthCache.get(conditionId);
    if (strengthPromises.has(conditionId)) return strengthPromises.get(conditionId);
    const p = (async () => {
      try {
        const m = await window.DetectorStrength.load(conditionId);
        strengthCache.set(conditionId, m);
        return m;
      } catch {
        const err = { condition_id: conditionId, strength_status: "error", yes_holders: [], no_holders: [] };
        strengthCache.set(conditionId, err);
        return err;
      } finally {
        strengthPromises.delete(conditionId);
      }
    })();
    strengthPromises.set(conditionId, p);
    return p;
  }

  function requestStrengthAndRefresh(conditionId, refreshFn) {
    if (!conditionId) {
      refreshFn();
      return;
    }
    if (strengthCache.has(conditionId)) {
      refreshFn();
      return;
    }
    refreshFn();
    ensureStrength(conditionId).then(() => refreshFn());
  }

  async function loadProfile(wallet, signal) {
    const q = new URLSearchParams({
      user: wallet,
      timePeriod: "ALL",
      orderBy: "PNL",
      limit: "1",
      category: "OVERALL",
    });
    const seeded = TRACKED.find((t) => t.wallet.toLowerCase() === wallet.toLowerCase())?.name;
    const entries = await getJSON(`${DATA}/v1/leaderboard?${q}`, signal);
    const e = entries?.[0];
    const name = (e?.userName || "").trim() || seeded || fmtWallet(wallet);
    const info = {
      wallet,
      name,
      pnl: e ? Number(e.pnl) : undefined,
      vol: e ? Number(e.vol) : undefined,
      profileImage: e?.profileImage || "",
    };
    profiles.set(wallet.toLowerCase(), info);
    return info;
  }

  async function fetchAllPositions(wallet, signal) {
    const out = [];
    let offset = 0;
    const limit = 500;
    for (;;) {
      const q = new URLSearchParams({
        user: wallet,
        sizeThreshold: "1",
        limit: String(limit),
        offset: String(offset),
        sortBy: "CURRENT",
        sortDirection: "DESC",
      });
      const page = await getJSON(`${DATA}/positions?${q}`, signal);
      if (!Array.isArray(page) || !page.length) break;
      out.push(...page);
      if (page.length < limit) break;
      offset += limit;
      if (offset > 5000) break;
    }
    return out;
  }

  async function fetchActivity(wallet, limit, type, signal) {
    const q = new URLSearchParams({
      user: wallet,
      limit: String(limit),
      offset: "0",
    });
    if (type) q.set("type", type);
    const page = await getJSON(`${DATA}/activity?${q}`, signal);
    return Array.isArray(page) ? page : [];
  }

  /** Last trades on a market (both Y and N) for one wallet. */
  async function fetchHolderTrades(wallet, conditionId, signal) {
    const q = new URLSearchParams({
      user: wallet,
      market: conditionId,
      limit: "3",
    });
    const trades = await getJSON(`${DATA}/trades?${q}`, signal);
    return Array.isArray(trades) ? trades : [];
  }

  function aggregatePositions(rawByWallet) {
    /** @type {Map<string, object>} */
    const map = new Map();
    for (const [wallet, positions] of rawByWallet) {
      const prof = profiles.get(wallet.toLowerCase());
      for (const p of positions) {
        const key = `${p.conditionId || ""}|${p.asset || p.outcome || ""}`;
        let row = map.get(key);
        if (!row) {
          row = {
            key,
            conditionId: p.conditionId,
            asset: p.asset,
            title: p.title || p.slug || "—",
            slug: p.slug,
            eventSlug: p.eventSlug,
            outcome: p.outcome || "—",
            icon: p.icon || "",
            size: 0,
            currentValue: 0,
            initialValue: 0,
            cashPnl: 0,
            avgPrice: 0,
            curPrice: Number(p.curPrice) || 0,
            holders: [],
          };
          map.set(key, row);
        }
        const size = Number(p.size) || 0;
        const cur = Number(p.currentValue) || 0;
        const init = Number(p.initialValue) || 0;
        const pnl = Number(p.cashPnl) || 0;
        const avg = Number(p.avgPrice) || 0;
        const prevSize = row.size;
        row.size += size;
        row.currentValue += cur;
        row.initialValue += init;
        row.cashPnl += pnl;
        row.avgPrice = row.size > 0 ? ((prevSize * row.avgPrice) + (size * avg)) / row.size : avg;
        if (Number(p.curPrice) > 0) row.curPrice = Number(p.curPrice);
        row.holders.push({
          wallet,
          name: prof?.name || fmtWallet(wallet),
          size,
          currentValue: cur,
          cashPnl: pnl,
          avgPrice: avg,
          curPrice: Number(p.curPrice) || 0,
        });
      }
    }
    for (const row of map.values()) {
      row.holders.sort((a, b) => b.currentValue - a.currentValue);
    }
    return [...map.values()];
  }

  function filteredPortRows() {
    const needle = $("portQ").value.trim().toLowerCase();
    const minVal = Math.max(0, Number($("portMinValue").value) || 0);
    let list = portRows.filter((r) => (r.currentValue || 0) >= minVal && !isSportsItem(r));
    if (needle) {
      list = list.filter((r) => {
        const hay = [r.title, r.outcome, r.slug, ...r.holders.map((h) => h.name)].join(" ").toLowerCase();
        return hay.includes(needle);
      });
    }
    const dir = portSortDir === "asc" ? 1 : -1;
    const key = portSort;
    return [...list].sort((a, b) => {
      let av = a[key];
      let bv = b[key];
      if (key === "title") {
        av = String(a.title || "").toLowerCase();
        bv = String(b.title || "").toLowerCase();
      }
      if (av === bv) return (b.currentValue || 0) - (a.currentValue || 0);
      return av > bv ? dir : -dir;
    });
  }

  function fmtTradeLine(t) {
    const side = String(t.side || "").toUpperCase();
    const sideCls = side === "BUY" ? "trade-buy" : side === "SELL" ? "trade-sell" : "";
    return (
      `<tr>` +
      `<td class="num">${fmtWhen(t.timestamp)}</td>` +
      `<td class="${sideCls}">${side || "—"}</td>` +
      `<td>${outcomeBadge(t.outcome)}</td>` +
      `<td class="num">${fmtShares(Number(t.size) || 0)}</td>` +
      `<td class="num">${fmtCts(Number(t.price) || 0)}</td>` +
      `</tr>`
    );
  }

  function holderTradesHtml(hk) {
    const cached = holderTrades.get(hk);
    if (!cached || cached.status === "pending") {
      return `<p class="sub nest-empty">Loading trades…</p>`;
    }
    if (cached.status === "error") {
      return `<p class="sub nest-empty">Could not load trades.</p>`;
    }
    if (!cached.trades.length) {
      return `<p class="sub nest-empty">No recent trades on this market.</p>`;
    }
    return (
      `<table class="nest-trades">` +
      `<thead><tr><th>When</th><th>Side</th><th>Out</th><th class="num">Size</th><th class="num">Price</th></tr></thead>` +
      `<tbody>${cached.trades.map(fmtTradeLine).join("")}</tbody>` +
      `</table>`
    );
  }

  function portDetailRow(r) {
    const tr = document.createElement("tr");
    tr.className = "detail-row port-detail-row";
    tr.dataset.portKey = r.key;

    let body = "";
    for (const h of r.holders) {
      const hk = holderKey(r.key, h.wallet);
      const open = expandedHolders.has(hk);
      const pnlCls = (h.cashPnl || 0) >= 0 ? "trade-buy" : "trade-sell";
      body +=
        `<tr class="holder-row${open ? " open" : ""}" data-holder-key="${hk}" data-wallet="${h.wallet}" data-condition="${r.conditionId || ""}" tabindex="0">` +
        `<td class="expand"><span class="chev" aria-hidden="true"></span></td>` +
        `<td class="trader"><a href="https://polymarket.com/profile/${h.wallet}" target="_blank" rel="noopener noreferrer">${shortName(h.name, 22)}</a></td>` +
        `<td class="num">${fmtUsd(h.currentValue)}</td>` +
        `<td class="num">${fmtShares(h.size)}${mobilePriceHtml(h.curPrice, "Now")}</td>` +
        `<td class="num hide-sm">${fmtCts(h.avgPrice)} / ${fmtCts(h.curPrice)}</td>` +
        `<td class="num ${pnlCls}">${fmtUsd(h.cashPnl)}</td>` +
        `</tr>`;
      if (open) {
        body +=
          `<tr class="holder-detail-row">` +
          `<td colspan="6"><div class="holder-trades">${holderTradesHtml(hk)}</div></td>` +
          `</tr>`;
      }
    }

    tr.innerHTML =
      `<td colspan="${PORT_COLS}">` +
      `<div class="port-detail">` +
      strengthBlockHtml(r.conditionId) +
      `<div class="tracked-label">Tracked positions</div>` +
      `<table class="holders port-holders">` +
      `<thead><tr>` +
      `<th class="expand-h" aria-hidden="true"></th>` +
      `<th>Sharp</th>` +
      `<th class="num">Value</th>` +
      `<th class="num">Shares</th>` +
      `<th class="num hide-sm">Avg / Now</th>` +
      `<th class="num">PnL</th>` +
      `</tr></thead>` +
      `<tbody>${body}</tbody>` +
      `</table>` +
      `</div>` +
      `</td>`;
    return tr;
  }

  function renderPortfolio() {
    const list = filteredPortRows();
    const totalVal = list.reduce((s, r) => s + (r.currentValue || 0), 0);
    const totalPnl = list.reduce((s, r) => s + (r.cashPnl || 0), 0);
    $("portMeta").innerHTML =
      `<span>positions <b>${list.length}</b></span>` +
      `<span>value <b>${fmtUsd(totalVal)}</b></span>` +
      `<span>unrealized <b>${fmtUsd(totalPnl)}</b></span>` +
      `<span>wallets <b>${TRACKED.length}</b></span>`;

    document.querySelectorAll("th[data-port-sort]").forEach((th) => {
      const key = th.getAttribute("data-port-sort");
      const active = key === portSort;
      th.classList.toggle("active", active);
      th.dataset.dir = active ? (portSortDir === "asc" ? "↑" : "↓") : "";
    });

    const body = $("portBody");
    body.innerHTML = "";
    if (!list.length) {
      $("portEmpty").hidden = false;
      return;
    }
    $("portEmpty").hidden = true;
    const frag = document.createDocumentFragment();
    for (const r of list) {
      const open = expandedPorts.has(r.key);
      const tr = document.createElement("tr");
      tr.className = "port-row" + (open ? " open" : "");
      tr.dataset.portKey = r.key;
      tr.tabIndex = 0;
      const sharps = r.holders
        .map((h) => `<a href="https://polymarket.com/profile/${h.wallet}" target="_blank" rel="noopener noreferrer" title="${fmtUsd(h.currentValue)}">${shortName(h.name, 18)}</a>`)
        .join(", ");
      const pnlCls = (r.cashPnl || 0) >= 0 ? "trade-buy" : "trade-sell";
      tr.innerHTML =
        `<td class="expand"><span class="chev" aria-hidden="true"></span></td>` +
        `<td class="num">${fmtUsd(r.currentValue)}</td>` +
        `<td class="market">${marketCellHtml(r, "", r.curPrice, "Now")}</td>` +
        `<td class="num">${fmtShares(r.size)}</td>` +
        `<td class="num hide-sm">${fmtCts(r.avgPrice)} / ${fmtCts(r.curPrice)}</td>` +
        `<td class="num ${pnlCls}">${fmtUsd(r.cashPnl)}</td>` +
        `<td class="sharps-cell">${sharps}</td>`;
      tr.querySelector("a.mkt-title").textContent = r.title;
      frag.appendChild(tr);
      if (open) frag.appendChild(portDetailRow(r));
    }
    body.appendChild(frag);
  }

  async function loadHolderTrades(hk, wallet, conditionId) {
    if (!wallet || !conditionId) {
      holderTrades.set(hk, { status: "empty", trades: [] });
      renderPortfolio();
      return;
    }
    const cur = holderTrades.get(hk);
    if (cur && (cur.status === "ready" || cur.status === "pending")) return;
    holderTrades.set(hk, { status: "pending", trades: [] });
    renderPortfolio();
    try {
      const trades = await fetchHolderTrades(wallet, conditionId);
      holderTrades.set(hk, { status: "ready", trades });
    } catch {
      holderTrades.set(hk, { status: "error", trades: [] });
    }
    if (expandedHolders.has(hk)) renderPortfolio();
  }

  function togglePort(key) {
    if (!key) return;
    if (expandedPorts.has(key)) {
      expandedPorts.delete(key);
      for (const hk of [...expandedHolders]) {
        if (hk.startsWith(`${key}|`)) expandedHolders.delete(hk);
      }
      renderPortfolio();
      return;
    }
    expandedPorts.add(key);
    const row = portRows.find((r) => r.key === key);
    requestStrengthAndRefresh(row?.conditionId, renderPortfolio);
  }

  function toggleHolder(hk, wallet, conditionId) {
    if (!hk) return;
    if (expandedHolders.has(hk)) {
      expandedHolders.delete(hk);
      renderPortfolio();
      return;
    }
    expandedHolders.add(hk);
    renderPortfolio();
    loadHolderTrades(hk, wallet, conditionId);
  }

  async function loadPortfolio(opts = {}) {
    const { background = false, force = false } = opts;
    portAbort?.abort();
    const ac = new AbortController();
    portAbort = ac;
    const signal = ac.signal;

    const err = $("portErr");
    err.hidden = true;
    if (!background) {
      $("portLoad").disabled = true;
      $("portLoad").textContent = "Loading…";
    }
    if (!background) {
      expandedPorts.clear();
      expandedHolders.clear();
      holderTrades.clear();
    }

    const cached = force ? null : readCache(CACHE_PORT);
    if (cached && cached.age < CACHE_STALE_MS) {
      portRows = cached.data;
      portLoaded = true;
      renderPortfolio();
      ensureEventMetaLater(portRows, signal, renderPortfolio);
      if (cached.age < CACHE_TTL_MS) {
        if (!background) {
          $("portLoad").disabled = false;
          $("portLoad").textContent = "Refresh";
        }
        return;
      }
    }

    try {
      const needProfiles = TRACKED.filter((t) => !profiles.has(t.wallet.toLowerCase()));
      /** @type {Map<string, object[]>} */
      const byWallet = new Map();
      await Promise.all([
        mapPool(needProfiles, WALLET_CONCURRENCY, (t) => loadProfile(t.wallet, signal), signal),
        mapPool(TRACKED, WALLET_CONCURRENCY, async (t) => {
          const positions = await fetchAllPositions(t.wallet, signal);
          byWallet.set(t.wallet, positions);
        }, signal),
      ]);
      portRows = aggregatePositions(byWallet);
      writeCache(CACHE_PORT, portRows);
      portLoaded = true;
      renderPortfolio();
      ensureEventMetaLater(portRows, signal, renderPortfolio);
    } catch (e) {
      if (e?.name === "AbortError") return;
      if (!portRows.length) {
        err.hidden = false;
        err.textContent = String(e.message || e);
      }
    } finally {
      if (!background && portAbort === ac) {
        $("portLoad").disabled = false;
        $("portLoad").textContent = "Refresh";
      }
    }
  }

  function actGroupKey(a) {
    return [
      String(a.proxyWallet || "").toLowerCase(),
      String(a.conditionId || a.slug || a.title || ""),
      String(a.side || a.type || "").toUpperCase(),
      String(a.outcome || "").toLowerCase(),
      String(a.type || "TRADE").toUpperCase(),
    ].join("|");
  }

  /** Merge consecutive same trader/market/side/outcome rows; size-weighted avg price. */
  function aggregateActRows(rows) {
    const out = [];
    for (const a of rows) {
      const prev = out[out.length - 1];
      if (prev && actGroupKey(prev) === actGroupKey(a)) {
        const sizeA = Number(prev.size) || 0;
        const sizeB = Number(a.size) || 0;
        const total = sizeA + sizeB;
        const priceA = Number(prev.price) || 0;
        const priceB = Number(a.price) || 0;
        prev.size = total;
        prev.price = total > 0 ? (sizeA * priceA + sizeB * priceB) / total : priceA;
        prev.usdcSize = (Number(prev.usdcSize) || 0) + (Number(a.usdcSize) || 0);
        // keep prev.timestamp (newer — feed is newest-first)
        prev._parts = (prev._parts || 1) + 1;
      } else {
        out.push({
          ...a,
          size: Number(a.size) || 0,
          price: a.price != null ? Number(a.price) : null,
          usdcSize: a.usdcSize != null ? Number(a.usdcSize) : null,
          _parts: 1,
        });
      }
    }
    return out;
  }

  function filteredActRows() {
    const needle = $("actQ").value.trim().toLowerCase();
    const minUsdc = Math.max(0, Number($("actMinUsdc").value) || 0);
    let list = actAggregated.filter((a) => !isSportsItem(a));
    if (minUsdc > 0) {
      list = list.filter((a) => (Number(a.usdcSize) || 0) >= minUsdc);
    }
    if (needle) {
      list = list.filter((a) => {
        const hay = [a.title, a.outcome, a.name, a.side, a.type, a.slug].join(" ").toLowerCase();
        return hay.includes(needle);
      });
    }
    return list;
  }

  function rebuildActAggregated() {
    actAggregated = aggregateActRows(actRows);
  }

  function actDetailRow(a, key) {
    const tr = document.createElement("tr");
    tr.className = "detail-row act-detail-row";
    tr.dataset.actKey = key;
    tr.innerHTML =
      `<td colspan="${ACT_COLS}">` +
      `<div class="port-detail">` +
      strengthBlockHtml(a.conditionId) +
      `</div>` +
      `</td>`;
    return tr;
  }

  function renderActivity() {
    const list = filteredActRows();
    $("actMeta").innerHTML =
      `<span>events <b>${list.length}</b></span>` +
      `<span>wallets <b>${TRACKED.length}</b></span>`;

    const body = $("actBody");
    body.innerHTML = "";
    if (!list.length) {
      $("actEmpty").hidden = false;
      return;
    }
    $("actEmpty").hidden = true;
    const frag = document.createDocumentFragment();
    for (const a of list) {
      const key = actRowKey(a);
      const open = expandedActs.has(key);
      const tr = document.createElement("tr");
      tr.className = "act-row" + (open ? " open" : "");
      tr.dataset.actKey = key;
      tr.dataset.condition = a.conditionId || "";
      tr.tabIndex = 0;
      const side = String(a.side || a.type || "").toUpperCase();
      const sideCls = side === "BUY" ? "trade-buy" : side === "SELL" ? "trade-sell" : "";
      const name = shortName(a.name || profiles.get(String(a.proxyWallet || "").toLowerCase())?.name || fmtWallet(a.proxyWallet));
      const title = a._parts > 1 ? `${a._parts} fills` : "";
      tr.innerHTML =
        `<td class="expand"><span class="chev" aria-hidden="true"></span></td>` +
        `<td class="num" title="${title}">${fmtWhen(a.timestamp)}</td>` +
        `<td><a class="act-trader" href="https://polymarket.com/profile/${a.proxyWallet}" target="_blank" rel="noopener noreferrer">${name}</a></td>` +
        `<td class="${sideCls}">${side || "—"}</td>` +
        `<td class="num">${fmtShares(a.size)}</td>` +
        `<td class="num hide-sm">${a.price != null ? fmtCts(a.price) : "—"}</td>` +
        `<td class="market">${marketCellHtml(a, "act-mkt", a.price, "Fill price")}</td>` +
        `<td class="num">${a.usdcSize != null ? fmtUsd(a.usdcSize) : "—"}</td>`;
      tr.querySelector("a.mkt-title").textContent = a.title || a.slug || "—";
      frag.appendChild(tr);
      if (open) frag.appendChild(actDetailRow(a, key));
    }
    body.appendChild(frag);
  }

  function toggleAct(key, conditionId) {
    if (!key) return;
    if (expandedActs.has(key)) {
      expandedActs.delete(key);
      renderActivity();
      return;
    }
    expandedActs.add(key);
    requestStrengthAndRefresh(conditionId, renderActivity);
  }

  async function loadActivity(opts = {}) {
    const { background = false, force = false } = opts;
    actAbort?.abort();
    const ac = new AbortController();
    actAbort = ac;
    const signal = ac.signal;

    const err = $("actErr");
    err.hidden = true;
    if (!background) {
      $("actLoad").disabled = true;
      $("actLoad").textContent = "Loading…";
    }

    const cached = force ? null : readCache(CACHE_ACT);
    if (cached && cached.age < CACHE_STALE_MS) {
      actRows = cached.data;
      rebuildActAggregated();
      actLoaded = true;
      if (!background) expandedActs.clear();
      renderActivity();
      ensureEventMetaLater(actRows, signal, renderActivity);
      if (cached.age < CACHE_TTL_MS) {
        if (!background) {
          $("actLoad").disabled = false;
          $("actLoad").textContent = "Refresh";
        }
        return;
      }
    }

    try {
      const needProfiles = TRACKED.filter((t) => !profiles.has(t.wallet.toLowerCase()));
      const limit = 200;
      const [, chunks] = await Promise.all([
        mapPool(needProfiles, WALLET_CONCURRENCY, (t) => loadProfile(t.wallet, signal), signal),
        mapPool(TRACKED, WALLET_CONCURRENCY, (t) => fetchActivity(t.wallet, limit, "TRADE", signal), signal),
      ]);
      actRows = chunks.flat().sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0));
      rebuildActAggregated();
      writeCache(CACHE_ACT, actRows);
      actLoaded = true;
      if (!background) expandedActs.clear();
      renderActivity();
      ensureEventMetaLater(actRows, signal, renderActivity);
    } catch (e) {
      if (e?.name === "AbortError") return;
      if (!actRows.length) {
        err.hidden = false;
        err.textContent = String(e.message || e);
      }
    } finally {
      if (!background && actAbort === ac) {
        $("actLoad").disabled = false;
        $("actLoad").textContent = "Refresh";
      }
    }
  }

  function showTab(name) {
    document.querySelectorAll(".tab").forEach((btn) => {
      const on = btn.getAttribute("data-tab") === name;
      btn.classList.toggle("active", on);
      btn.setAttribute("aria-selected", on ? "true" : "false");
    });
    document.querySelectorAll(".tab-panel").forEach((panel) => {
      const on = panel.id === `tab-${name}`;
      panel.classList.toggle("active", on);
      panel.hidden = !on;
    });
    if (name === "portfolio" && !portLoaded) loadPortfolio();
    if (name === "activity" && !actLoaded) loadActivity();
    if (name === "flows") window.DetectorFlows?.ensureLoaded();
  }

  document.querySelectorAll(".tab").forEach((btn) => {
    btn.addEventListener("click", () => showTab(btn.getAttribute("data-tab")));
  });

  $("portBody").addEventListener("click", (e) => {
    if (e.target.closest("a")) return;
    const holderRow = e.target.closest("tr.holder-row");
    if (holderRow) {
      toggleHolder(
        holderRow.dataset.holderKey,
        holderRow.dataset.wallet,
        holderRow.dataset.condition,
      );
      return;
    }
    const portRow = e.target.closest("tr.port-row");
    if (portRow) togglePort(portRow.dataset.portKey);
  });

  $("portBody").addEventListener("keydown", (e) => {
    if (e.key !== "Enter" && e.key !== " ") return;
    if (e.target.closest("a")) return;
    const holderRow = e.target.closest("tr.holder-row");
    if (holderRow) {
      e.preventDefault();
      toggleHolder(
        holderRow.dataset.holderKey,
        holderRow.dataset.wallet,
        holderRow.dataset.condition,
      );
      return;
    }
    const portRow = e.target.closest("tr.port-row");
    if (portRow) {
      e.preventDefault();
      togglePort(portRow.dataset.portKey);
    }
  });

  $("actBody").addEventListener("click", (e) => {
    if (e.target.closest("a")) return;
    const actRow = e.target.closest("tr.act-row");
    if (actRow) toggleAct(actRow.dataset.actKey, actRow.dataset.condition);
  });

  $("actBody").addEventListener("keydown", (e) => {
    if (e.key !== "Enter" && e.key !== " ") return;
    if (e.target.closest("a")) return;
    const actRow = e.target.closest("tr.act-row");
    if (actRow) {
      e.preventDefault();
      toggleAct(actRow.dataset.actKey, actRow.dataset.condition);
    }
  });

  $("portLoad").addEventListener("click", () => {
    portLoaded = false;
    loadPortfolio({ force: true });
  });
  $("portQ").addEventListener("input", renderPortfolio);
  $("portMinValue").addEventListener("input", renderPortfolio);
  document.querySelectorAll("th[data-port-sort]").forEach((th) => {
    th.addEventListener("click", () => {
      const key = th.getAttribute("data-port-sort");
      if (portSort === key) portSortDir = portSortDir === "desc" ? "asc" : "desc";
      else {
        portSort = key;
        portSortDir = key === "title" ? "asc" : "desc";
      }
      renderPortfolio();
    });
  });

  $("actLoad").addEventListener("click", () => {
    actLoaded = false;
    loadActivity({ force: true });
  });
  $("actQ").addEventListener("input", renderActivity);
  $("actMinUsdc").addEventListener("input", renderActivity);

  window.DetectorSharps = {
    wallets: new Set(TRACKED.map((t) => t.wallet.toLowerCase())),
  };

  function schedulePreload() {
    const run = () => {
      if (!portLoaded) loadPortfolio({ background: true });
      if (!actLoaded) loadActivity({ background: true });
    };
    if (typeof requestIdleCallback === "function") {
      requestIdleCallback(run, { timeout: 4000 });
    } else {
      setTimeout(run, 2000);
    }
  }
  schedulePreload();
})();
