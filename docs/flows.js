/* Taker-flow tab — aggressor fills from data-api /trades (takerOnly=true).
 * Rank markets like the Markets tab, but by USDC taken off the book (and
 * order count) over the last 24h or 48h.
 */
(function () {
  const DATA = "https://data-api.polymarket.com";
  const FLOW_CONCURRENCY = 4;
  const TRADE_PAGE = 500;
  const MAX_TRADE_OFFSET = 10000;
  const MAX_TRADE_PAGES = 40;
  const COLS = 8;

  const $ = (id) => document.getElementById(id);

  /** @type {Array<object>} */
  let rows = [];
  let sortKey = "taker_usd";
  let sortDir = "desc";
  let loaded = false;
  let flowAbort = null;
  /** @type {Set<string>} */
  const expanded = new Set();

  function catalog() {
    return window.DetectorCatalog;
  }

  function sharpWallets() {
    const set = window.DetectorSharps?.wallets;
    return set instanceof Set ? set : new Set();
  }

  function fmtUsd(n) {
    if (n == null || Number.isNaN(n)) return "—";
    const sign = n < 0 ? "-" : "";
    const abs = Math.abs(n);
    if (abs >= 1e6) return `${sign}$${(abs / 1e6).toFixed(2)}M`;
    if (abs >= 1e3) return `${sign}$${(abs / 1e3).toFixed(1)}k`;
    return `${sign}$${abs.toFixed(abs >= 10 ? 0 : 2)}`;
  }

  function fmtTrader(h) {
    const raw = h.name || fmtWallet(h.wallet);
    if (raw.length <= 25) return raw;
    return `${raw.slice(0, 24)}…`;
  }

  function fmtWallet(w) {
    if (!w || w.length < 12) return w || "—";
    return `${w.slice(0, 6)}…${w.slice(-4)}`;
  }

  function tradeUSD(t) {
    return (Number(t.size) || 0) * (Number(t.price) || 0);
  }

  function isYesOutcome(outcome) {
    return String(outcome || "").trim().toLowerCase() === "yes";
  }

  function yesPressure(t) {
    const usd = tradeUSD(t);
    const yes = isYesOutcome(t.outcome);
    const buy = String(t.side || "").toUpperCase() === "BUY";
    return yes === buy ? usd : -usd;
  }

  function displayName(t) {
    const name = String(t.name || "").trim();
    const pseudo = String(t.pseudonym || "").trim();
    if (name && !name.toLowerCase().startsWith("0x")) return name;
    if (pseudo) return pseudo;
    return name;
  }

  function flowFilters() {
    return {
      minFill: Math.max(0, Number($("flowMinFill").value) || 0),
      maxFill: Math.max(0, Number($("flowMaxFill").value) || 0),
      skipSharps: $("flowSkipSharps").checked,
      skip: $("flowSkipSharps").checked ? sharpWallets() : new Set(),
    };
  }

  function aggregateTrades(trades, filters) {
    const skip = filters.skip;
    /** @type {Map<string, {wallet:string, name:string, usd:number, orders:number, net:number}>} */
    const byWallet = new Map();
    let takerUsd = 0;
    let orders = 0;
    let yesUsd = 0;
    let noUsd = 0;
    let yesOrders = 0;
    let noOrders = 0;
    let netUsd = 0;

    for (const t of trades || []) {
      const wallet = String(t.proxyWallet || "").toLowerCase();
      if (!wallet) continue;
      if (skip.has(wallet)) continue;
      const usd = tradeUSD(t);
      if (filters.minFill > 0 && usd < filters.minFill) continue;
      if (filters.maxFill > 0 && usd > filters.maxFill) continue;

      takerUsd += usd;
      orders += 1;
      const net = yesPressure(t);
      netUsd += net;
      if (isYesOutcome(t.outcome)) {
        yesUsd += usd;
        yesOrders += 1;
      } else {
        noUsd += usd;
        noOrders += 1;
      }

      let row = byWallet.get(wallet);
      if (!row) {
        row = { wallet, name: displayName(t), usd: 0, orders: 0, net: 0 };
        byWallet.set(wallet, row);
      }
      if (!row.name) row.name = displayName(t);
      row.usd += usd;
      row.orders += 1;
      row.net += net;
    }

    /** @type {Array<object>} */
    const yesList = [];
    /** @type {Array<object>} */
    const noList = [];
    for (const row of byWallet.values()) {
      if (row.net > 0) yesList.push({ ...row, outcome: "yes" });
      else if (row.net < 0) noList.push({ ...row, outcome: "no" });
    }
    yesList.sort((a, b) => b.usd - a.usd);
    noList.sort((a, b) => b.usd - a.usd);

    let stronger = "tie";
    if (netUsd > 0) stronger = "yes";
    else if (netUsd < 0) stronger = "no";

    return {
      taker_usd: takerUsd,
      orders,
      unique: byWallet.size,
      yes_usd: yesUsd,
      no_usd: noUsd,
      yes_orders: yesOrders,
      no_orders: noOrders,
      yes_takers: yesList.length,
      no_takers: noList.length,
      net_usd: netUsd,
      net_usd_abs: Math.abs(netUsd),
      net_takers_abs: Math.abs(yesList.length - noList.length),
      stronger,
      yes_taker_list: yesList,
      no_taker_list: noList,
    };
  }

  function applyFlow(row, filters) {
    const stats = aggregateTrades(row.trades, filters);
    Object.assign(row, stats);
    if (row.flow_status === "error") return row;
    if (row.flow_status === "pending") return row;
    row.flow_status = stats.orders > 0 || (row.trades && row.trades.length) ? "ready" : "empty";
    return row;
  }

  function tradeKey(t) {
    return [
      t.transactionHash || "",
      String(t.proxyWallet || "").toLowerCase(),
      t.asset || "",
      String(t.timestamp || ""),
      String(t.size || ""),
    ].join("|");
  }

  async function fetchTakerTrades(conditionId, startSec, signal) {
    const cat = catalog();
    const out = [];
    const seen = new Set();
    let endSec = 0;
    let offset = 0;
    let truncated = false;

    for (let page = 0; page < MAX_TRADE_PAGES; page++) {
      const q = new URLSearchParams({
        market: conditionId,
        takerOnly: "true",
        start: String(startSec),
        limit: String(TRADE_PAGE),
        offset: String(offset),
      });
      if (endSec > 0) q.set("end", String(endSec));
      const trades = await cat.getJSON(`${DATA}/trades?${q}`, signal);
      if (!Array.isArray(trades) || !trades.length) {
        return { trades: out, truncated };
      }

      let added = 0;
      for (const t of trades) {
        const ts = Number(t.timestamp) || 0;
        if (ts < startSec) continue;
        const id = tradeKey(t);
        if (seen.has(id)) continue;
        seen.add(id);
        out.push(t);
        added += 1;
      }

      const last = Number(trades[trades.length - 1].timestamp) || 0;
      if (trades.length < TRADE_PAGE || last < startSec) {
        return { trades: out, truncated };
      }
      if (added === 0) {
        if (last <= 1 || last - 1 < startSec) return { trades: out, truncated };
        endSec = last - 1;
        offset = 0;
        continue;
      }

      offset += TRADE_PAGE;
      if (offset + TRADE_PAGE > MAX_TRADE_OFFSET) {
        if (last <= startSec) return { trades: out, truncated };
        endSec = last;
        offset = 0;
      }
    }
    truncated = true;
    return { trades: out, truncated };
  }

  function maxOutcomeCents(m) {
    const p = m.outcome_prices || [];
    if (p.length < 2) return null;
    return Math.max(p[0], p[1]) * 100;
  }

  function passesFilters(m) {
    const maxPrice = Number($("flowMaxPriceCts").value);
    if (Number.isFinite(maxPrice) && maxPrice > 0) {
      const cts = maxOutcomeCents(m);
      if (cts == null || cts > maxPrice) return false;
    }
    return true;
  }

  function filteredRows() {
    const filters = flowFilters();
    const needle = $("flowQ").value.trim().toLowerCase();
    let list = rows.map((m) => applyFlow({ ...m }, filters)).filter(passesFilters);
    if (needle) {
      list = list.filter((m) => {
        const hay = [m.question, m.slug, m.event_title, m.event_slug].join(" ").toLowerCase();
        return hay.includes(needle);
      });
    }
    const dir = sortDir === "asc" ? 1 : -1;
    const key = sortKey;
    const strongerRank = { yes: 1, no: -1, tie: 0 };
    const numKeys = new Set([
      "taker_usd", "orders", "net_usd_abs", "net_takers_abs", "unique",
    ]);

    return [...list].sort((a, b) => {
      let av;
      let bv;
      if (key === "stronger") {
        av = strongerRank[a.stronger] ?? 0;
        bv = strongerRank[b.stronger] ?? 0;
        if (a.flow_status !== "ready") av = null;
        if (b.flow_status !== "ready") bv = null;
      } else if (numKeys.has(key)) {
        av = a.flow_status === "pending" || a.flow_status === "error" ? null : a[key];
        bv = b.flow_status === "pending" || b.flow_status === "error" ? null : b[key];
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
      return (b.taker_usd || 0) - (a.taker_usd || 0);
    });
  }

  function setSortHeaders() {
    document.querySelectorAll("#tab-flows th.sortable").forEach((th) => {
      const key = th.getAttribute("data-flow-sort");
      const active = key === sortKey;
      th.classList.toggle("active", active);
      th.dataset.dir = active ? (sortDir === "asc" ? "↑" : "↓") : "";
    });
  }

  function takersTable(sideLabel, takers) {
    const rowsHtml = takers.length
      ? takers.map((h) =>
        `<tr>` +
        `<td class="trader"><a href="https://polymarket.com/profile/${h.wallet}" target="_blank" rel="noopener noreferrer" title="${h.wallet}">${fmtTrader(h)}</a></td>` +
        `<td class="num">${fmtUsd(h.usd)}</td>` +
        `<td class="num">${h.orders}</td>` +
        `<td class="num">${fmtUsd(h.net)}</td>` +
        `</tr>`
      ).join("")
      : `<tr><td colspan="4" class="na">No taker flow</td></tr>`;

    return (
      `<div class="side-block side-${sideLabel.toLowerCase()}">` +
      `<div class="side-label">${sideLabel}</div>` +
      `<table class="holders">` +
      `<thead><tr><th>Trader</th><th class="num">Taker $</th><th class="num">Orders</th><th class="num">Net Yes $</th></tr></thead>` +
      `<tbody>${rowsHtml}</tbody>` +
      `</table>` +
      `</div>`
    );
  }

  function detailRow(m) {
    const tr = document.createElement("tr");
    tr.className = "detail-row";
    tr.dataset.id = m.condition_id;
    if (m.flow_status !== "ready") {
      tr.innerHTML = `<td colspan="${COLS}"><div class="detail">Taker tape not ready.</div></td>`;
      return tr;
    }
    const trunc = m.truncated
      ? `<p class="sub nest-empty">Tape truncated at ${m.trades?.length || 0} fills — totals are a lower bound.</p>`
      : "";
    tr.innerHTML =
      `<td colspan="${COLS}">` +
      trunc +
      `<div class="detail">` +
      takersTable("Yes", m.yes_taker_list || []) +
      takersTable("No", m.no_taker_list || []) +
      `</div>` +
      `</td>`;
    return tr;
  }

  function render() {
    const cat = catalog();
    if (!cat) return;
    setSortHeaders();
    const list = filteredRows();
    const ready = rows.filter((r) => r.flow_status === "ready" || r.flow_status === "empty").length;
    const hours = Math.max(1, Number($("flowHours").value) || 24);
    $("flowMeta").innerHTML =
      `<span>loaded <b>${rows.length}</b></span>` +
      `<span>showing <b>${list.length}</b></span>` +
      `<span>tape <b>${ready}/${rows.length}</b></span>` +
      `<span>taker fills · last <b>${hours}h</b></span>` +
      `<span>sort <b>${sortKey} ${sortDir}</b></span>`;

    const body = $("flowBody");
    body.innerHTML = "";
    if (!list.length) {
      $("flowEmpty").hidden = false;
      return;
    }
    $("flowEmpty").hidden = true;
    const frag = document.createDocumentFragment();

    for (const m of list) {
      const tr = document.createElement("tr");
      tr.className = "flow-row" + (expanded.has(m.condition_id) ? " open" : "");
      tr.dataset.id = m.condition_id;
      tr.tabIndex = 0;

      let usdCell = `<span class="pending">…</span>`;
      let ordersCell = `<span class="pending">…</span>`;
      let flowCell = `<span class="pending">…</span>`;
      let takersCell = `<span class="pending">…</span>`;
      let donut = `<span class="donut donut-empty" aria-hidden="true"></span>`;
      let sideCell = "—";

      if (m.flow_status === "ready") {
        const prefix = m.truncated ? "≥" : "";
        usdCell = prefix + fmtUsd(m.taker_usd);
        ordersCell = prefix + String(m.orders);
        flowCell = `${fmtUsd(m.yes_usd)} / ${fmtUsd(m.no_usd)}`;
        takersCell = `${m.yes_takers} / ${m.no_takers}`;
        donut = cat.shareDonut(m.yes_usd, m.no_usd);
        sideCell = m.stronger;
      } else if (m.flow_status === "empty") {
        usdCell = `<span class="na">n/a</span>`;
        ordersCell = `<span class="na">n/a</span>`;
        flowCell = `<span class="na">n/a</span>`;
        takersCell = `<span class="na">n/a</span>`;
      } else if (m.flow_status === "error") {
        usdCell = `<span class="na">err</span>`;
        ordersCell = `<span class="na">err</span>`;
        flowCell = `<span class="na">err</span>`;
        takersCell = `<span class="na">err</span>`;
      }

      tr.innerHTML =
        `<td class="expand"><span class="chev" aria-hidden="true"></span></td>` +
        `<td class="num">${usdCell}</td>` +
        `<td class="num">${ordersCell}</td>` +
        `<td class="market"><span class="mkt-line">${cat.marketIconHtml(m.icon)}<span class="mkt-text"><a class="mkt-title" href="${m.url}" target="_blank" rel="noopener noreferrer"></a><span class="evt"></span></span></span></td>` +
        `<td class="num hide-sm">${cat.fmtPrice(m)}</td>` +
        `<td class="num"><span class="shares-wrap"><span class="shares-text">${flowCell}</span>${donut}</span></td>` +
        `<td class="num">${takersCell}</td>` +
        `<td class="num hide-sm">${sideCell}</td>`;
      tr.querySelector("a.mkt-title").textContent = m.question;
      tr.querySelector(".evt").textContent = m.event_title || m.event_slug || "";
      frag.appendChild(tr);

      if (expanded.has(m.condition_id)) {
        frag.appendChild(detailRow(m));
      }
    }
    body.appendChild(frag);
  }

  function toggleExpand(id) {
    if (!id) return;
    if (expanded.has(id)) expanded.delete(id);
    else expanded.add(id);
    render();
  }

  async function fillFlows(signal) {
    const cat = catalog();
    const hours = Math.max(1, Number($("flowHours").value) || 24);
    const startSec = Math.floor(Date.now() / 1000) - hours * 3600;
    const filters = flowFilters();

    await cat.mapPool(rows, FLOW_CONCURRENCY, async (row) => {
      try {
        const { trades, truncated } = await fetchTakerTrades(row.condition_id, startSec, signal);
        row.trades = trades;
        row.truncated = truncated;
        row.flow_status = trades.length ? "ready" : "empty";
        applyFlow(row, filters);
      } catch (e) {
        if (e?.name === "AbortError") throw e;
        row.flow_status = "error";
        row.trades = [];
      }
      render();
    }, signal);
  }

  async function load() {
    const cat = catalog();
    if (!cat) return;
    const err = $("flowErr");
    err.hidden = true;
    if (flowAbort) flowAbort.abort();
    flowAbort = new AbortController();
    const { signal } = flowAbort;

    $("flowLoad").disabled = true;
    $("flowLoad").textContent = "Loading…";
    rows = [];
    expanded.clear();
    loaded = false;
    render();

    try {
      const minVol = Number($("flowMinVol").value) || 0;
      const tag = $("flowTag").value;
      const { tag: tagInfo, markets } = await cat.listMarkets({
        tagSlug: tag,
        minVolume24: minVol,
        signal,
      });
      rows = markets.map((m) => ({
        ...m,
        flow_status: "pending",
        trades: [],
        truncated: false,
        taker_usd: 0,
        orders: 0,
        unique: 0,
        yes_usd: 0,
        no_usd: 0,
        yes_takers: 0,
        no_takers: 0,
        net_usd: 0,
        net_usd_abs: 0,
        net_takers_abs: 0,
        stronger: "tie",
        yes_taker_list: [],
        no_taker_list: [],
      }));
      await cat.fillMissingMarketIcons(rows, signal);
      $("flowTagLabel").textContent = `${tagInfo.label} (${tagInfo.id})`;
      render();
      $("flowLoad").textContent = "Tape…";
      await fillFlows(signal);
      loaded = true;
    } catch (e) {
      if (e?.name !== "AbortError") {
        err.hidden = false;
        err.textContent = String(e.message || e);
      }
    } finally {
      $("flowLoad").disabled = false;
      $("flowLoad").textContent = "Load";
    }
  }

  function ensureLoaded() {
    if (!loaded && !$("flowLoad").disabled) load();
  }

  $("flowBody").addEventListener("click", (e) => {
    const t = /** @type {HTMLElement} */ (e.target);
    if (t.closest("a")) return;
    const row = t.closest("tr.flow-row");
    if (!row) return;
    toggleExpand(row.dataset.id);
  });

  $("flowBody").addEventListener("keydown", (e) => {
    if (e.key !== "Enter" && e.key !== " ") return;
    const row = /** @type {HTMLElement} */ (e.target).closest?.("tr.flow-row");
    if (!row) return;
    e.preventDefault();
    toggleExpand(row.dataset.id);
  });

  document.querySelectorAll("#tab-flows th.sortable").forEach((th) => {
    th.addEventListener("click", () => {
      const key = th.getAttribute("data-flow-sort");
      if (sortKey === key) sortDir = sortDir === "desc" ? "asc" : "desc";
      else {
        sortKey = key;
        sortDir = key === "question" ? "asc" : "desc";
      }
      render();
    });
  });

  $("flowQ").addEventListener("input", render);
  $("flowMinFill").addEventListener("input", render);
  $("flowMaxFill").addEventListener("input", render);
  $("flowMaxPriceCts").addEventListener("input", render);
  $("flowSkipSharps").addEventListener("change", render);
  $("flowLoad").addEventListener("click", load);
  $("flowTag").addEventListener("change", load);
  $("flowHours").addEventListener("change", load);
  $("flowMinVol").addEventListener("keydown", (e) => {
    if (e.key === "Enter") load();
  });

  window.DetectorFlows = { load, ensureLoaded, render };
})();
