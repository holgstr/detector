/* Tracked sharps — portfolio + activity tabs.
 * Data: GET /positions and GET /activity on data-api.polymarket.com (public).
 */
(function () {
  const DATA = "https://data-api.polymarket.com";

  /** Add wallets here — names resolve from Polymarket leaderboard when possible. */
  /** @type {Array<{wallet:string, name?:string}>} */
  const TRACKED = [
    { wallet: "0x23d81ba9371e576015c1e562db09c689f56b0288", name: "flawfence" },
    { wallet: "0x614dc8d3542c12103d2c6a3553fd761e391d1546", name: "mr.ozi" },
    { wallet: "0x7bc14171ccb0d3e6bac219ec6a76211826e28db4", name: "coali10" },
    { wallet: "0x60a92c8620846d81f5ea17b0564e0d4b7c545a71", name: "paddaa" },
    { wallet: "0x2b9dbf4b6e0e11309a9d6d2a09b72f65f652adc0", name: "seal7" },
    { wallet: "0xc851cd9bee7d262afd78674f861f9f576a12cd2a", name: "betwick" },
    { wallet: "0x1cc16713196d456f86fa9c7387dd326a7f73b8df", name: "Wickier" },
    { wallet: "0xde7be6d489bce070a959e0cb813128ae659b5f4b", name: "wan123" },
  ];

  const $ = (id) => document.getElementById(id);

  /** @type {Map<string, {wallet:string, name:string, pnl?:number, vol?:number, profileImage?:string}>} */
  const profiles = new Map();

  /** @type {Array<object>} */
  let portRows = [];
  let portSort = "currentValue";
  let portSortDir = "desc";
  let portLoaded = false;

  /** @type {Array<object>} */
  let actRows = [];
  let actLoaded = false;

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
    const hr = Math.max(0, Math.round((Date.now() - d.getTime()) / 3600000));
    if (hr < 48) return `${hr}h`;
    return `${Math.round(hr / 24)}d`;
  }

  function marketUrl(p) {
    if (p.eventSlug && p.slug) return `https://polymarket.com/event/${p.eventSlug}/${p.slug}`;
    if (p.eventSlug) return `https://polymarket.com/event/${p.eventSlug}`;
    if (p.slug) return `https://polymarket.com/market/${p.slug}`;
    return `https://polymarket.com/profile/${p.proxyWallet || ""}`;
  }

  function outcomeBadge(outcome) {
    const o = String(outcome || "").trim().toLowerCase();
    if (o === "yes") return '<span class="yn yn-y" title="Yes">Y</span>';
    if (o === "no") return '<span class="yn yn-n" title="No">N</span>';
    const letter = String(outcome || "?").trim().charAt(0).toUpperCase() || "?";
    return `<span class="yn yn-other" title="${String(outcome || "").replace(/"/g, "&quot;")}">${letter}</span>`;
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
        // size-weighted avg price across wallets
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
    let list = portRows.filter((r) => (r.currentValue || 0) >= minVal);
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

  function renderPortWallets() {
    const el = $("portWallets");
    el.innerHTML = TRACKED.map((t) => {
      const p = profiles.get(t.wallet.toLowerCase());
      const name = shortName(p?.name || fmtWallet(t.wallet));
      const pnl = p?.pnl != null ? fmtUsd(p.pnl) : "—";
      return `<span><a href="https://polymarket.com/profile/${t.wallet}" target="_blank" rel="noopener noreferrer">${name}</a> <b>${pnl}</b> lifetime</span>`;
    }).join("");
  }

  function renderPortfolio() {
    renderPortWallets();
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
      const tr = document.createElement("tr");
      const sharps = r.holders
        .map((h) => `<a href="https://polymarket.com/profile/${h.wallet}" target="_blank" rel="noopener noreferrer" title="${fmtUsd(h.currentValue)}">${shortName(h.name, 18)}</a>`)
        .join(", ");
      const pnlCls = (r.cashPnl || 0) >= 0 ? "trade-buy" : "trade-sell";
      tr.innerHTML =
        `<td class="num">${fmtUsd(r.currentValue)}</td>` +
        `<td class="market"><span class="mkt-line">${outcomeBadge(r.outcome)}<a href="${marketUrl(r)}" target="_blank" rel="noopener noreferrer"></a></span></td>` +
        `<td class="num">${fmtShares(r.size)}</td>` +
        `<td class="num hide-sm">${fmtCts(r.avgPrice)} / ${fmtCts(r.curPrice)}</td>` +
        `<td class="num ${pnlCls}">${fmtUsd(r.cashPnl)}</td>` +
        `<td class="sharps-cell">${sharps}</td>`;
      tr.querySelector("a").textContent = r.title;
      frag.appendChild(tr);
    }
    body.appendChild(frag);
  }

  async function loadPortfolio() {
    const err = $("portErr");
    err.hidden = true;
    $("portLoad").disabled = true;
    $("portLoad").textContent = "Loading…";
    try {
      const signal = undefined;
      await Promise.all(TRACKED.map((t) => loadProfile(t.wallet, signal)));
      /** @type {Map<string, object[]>} */
      const byWallet = new Map();
      await Promise.all(TRACKED.map(async (t) => {
        const positions = await fetchAllPositions(t.wallet, signal);
        byWallet.set(t.wallet, positions);
      }));
      portRows = aggregatePositions(byWallet);
      portLoaded = true;
      renderPortfolio();
    } catch (e) {
      err.hidden = false;
      err.textContent = String(e.message || e);
    } finally {
      $("portLoad").disabled = false;
      $("portLoad").textContent = "Refresh";
    }
  }

  function filteredActRows() {
    const needle = $("actQ").value.trim().toLowerCase();
    let list = actRows;
    if (needle) {
      list = list.filter((a) => {
        const hay = [a.title, a.outcome, a.name, a.side, a.type, a.slug].join(" ").toLowerCase();
        return hay.includes(needle);
      });
    }
    return list;
  }

  function renderActivity() {
    const list = filteredActRows();
    const wallets = TRACKED.map((t) => {
      const p = profiles.get(t.wallet.toLowerCase());
      return shortName(p?.name || t.name || fmtWallet(t.wallet));
    }).join(", ");
    $("actMeta").innerHTML =
      `<span>events <b>${list.length}</b></span>` +
      `<span>wallets <b>${TRACKED.length}</b></span>` +
      `<span>${wallets}</span>`;

    const body = $("actBody");
    body.innerHTML = "";
    if (!list.length) {
      $("actEmpty").hidden = false;
      return;
    }
    $("actEmpty").hidden = true;
    const frag = document.createDocumentFragment();
    for (const a of list) {
      const tr = document.createElement("tr");
      const side = String(a.side || a.type || "").toUpperCase();
      const sideCls = side === "BUY" ? "trade-buy" : side === "SELL" ? "trade-sell" : "";
      const name = shortName(a.name || profiles.get(String(a.proxyWallet || "").toLowerCase())?.name || fmtWallet(a.proxyWallet));
      tr.innerHTML =
        `<td class="num">${fmtWhen(a.timestamp)}</td>` +
        `<td><a class="act-trader" href="https://polymarket.com/profile/${a.proxyWallet}" target="_blank" rel="noopener noreferrer">${name}</a></td>` +
        `<td class="${sideCls}">${side || "—"}</td>` +
        `<td class="num">${fmtShares(a.size)}</td>` +
        `<td class="num hide-sm">${a.price != null ? fmtCts(a.price) : "—"}</td>` +
        `<td class="num hide-sm">${a.usdcSize != null ? fmtUsd(a.usdcSize) : "—"}</td>` +
        `<td class="market"><a class="act-mkt" href="${marketUrl(a)}" target="_blank" rel="noopener noreferrer"></a></td>` +
        `<td class="hide-sm">${a.outcome || "—"}</td>`;
      tr.querySelector("a.act-mkt").textContent = a.title || a.slug || "—";
      frag.appendChild(tr);
    }
    body.appendChild(frag);
  }

  async function loadActivity() {
    const err = $("actErr");
    err.hidden = true;
    $("actLoad").disabled = true;
    $("actLoad").textContent = "Loading…";
    try {
      await Promise.all(TRACKED.map((t) => {
        if (profiles.has(t.wallet.toLowerCase())) return Promise.resolve();
        return loadProfile(t.wallet);
      }));
      const limit = Math.min(500, Math.max(20, Number($("actLimit").value) || 100));
      const type = $("actType").value;
      const chunks = await Promise.all(TRACKED.map((t) => fetchActivity(t.wallet, limit, type)));
      actRows = chunks.flat().sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0));
      actLoaded = true;
      renderActivity();
    } catch (e) {
      err.hidden = false;
      err.textContent = String(e.message || e);
    } finally {
      $("actLoad").disabled = false;
      $("actLoad").textContent = "Refresh";
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
  }

  document.querySelectorAll(".tab").forEach((btn) => {
    btn.addEventListener("click", () => showTab(btn.getAttribute("data-tab")));
  });

  $("portLoad").addEventListener("click", () => {
    portLoaded = false;
    loadPortfolio();
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
    loadActivity();
  });
  $("actQ").addEventListener("input", renderActivity);
  $("actType").addEventListener("change", () => {
    actLoaded = false;
    loadActivity();
  });
})();
