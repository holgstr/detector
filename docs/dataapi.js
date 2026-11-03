/* Polymarket Data API v2 helpers. v1 routes return 410. */
window.DetectorData = (function () {
  const base = "https://data-api.polymarket.com/v2";

  function num(v) {
    const n = Number(v);
    return Number.isFinite(n) ? n : 0;
  }

  /** @returns {{rows: object[], next: string}} */
  function page(body) {
    if (Array.isArray(body)) return { rows: body, next: "" };
    const rows = Array.isArray(body?.data) ? body.data : [];
    const next = body?.pagination?.next_cursor || "";
    return { rows, next };
  }

  function position(p) {
    if (!p || typeof p !== "object") return p;
    return {
      proxyWallet: p.proxy_wallet || p.proxyWallet || "",
      asset: p.token_id || p.asset || "",
      conditionId: p.condition_id || p.conditionId || "",
      size: num(p.current_size ?? p.size),
      avgPrice: num(p.avg_price ?? p.avgPrice),
      curPrice: num(p.current_price ?? p.curPrice),
      currentValue: num(p.current_value ?? p.currentValue),
      initialValue: num(p.entry_cost_usdc ?? p.initialValue),
      cashPnl: num(p.unrealized_pnl ?? p.cashPnl),
      title: p.title || "",
      slug: p.slug || "",
      icon: p.icon || "",
      eventSlug: p.event_slug || p.eventSlug || "",
      outcome: p.outcome || "",
    };
  }

  function activity(a) {
    if (!a || typeof a !== "object") return a;
    return {
      proxyWallet: a.proxy_wallet || a.proxyWallet || "",
      timestamp: num(a.timestamp),
      conditionId: a.condition_id || a.conditionId || "",
      type: a.type || "",
      size: num(a.size),
      usdcSize: num(a.usdc_size ?? a.usdcSize),
      transactionHash: a.transaction_hash || a.transactionHash || "",
      price: num(a.price),
      asset: a.token_id || a.asset || "",
      side: a.side || "",
      outcomeIndex: a.outcome_index ?? a.outcomeIndex ?? 0,
      title: a.title || "",
      slug: a.slug || "",
      icon: a.icon || "",
      eventSlug: a.event_slug || a.eventSlug || "",
      outcome: a.outcome || "",
      name: a.name || "",
      pseudonym: a.pseudonym || "",
    };
  }

  function trade(t) {
    if (!t || typeof t !== "object") return t;
    return {
      proxyWallet: t.proxy_wallet || t.proxyWallet || "",
      side: t.side || "",
      asset: t.token_id || t.asset || "",
      conditionId: t.condition_id || t.conditionId || "",
      size: num(t.size),
      price: num(t.price),
      timestamp: num(t.timestamp),
      title: t.title || "",
      slug: t.slug || "",
      icon: t.icon || "",
      eventSlug: t.event_slug || t.eventSlug || "",
      outcome: t.outcome || "",
      outcomeIndex: t.outcome_index ?? t.outcomeIndex ?? 0,
      name: t.name || "",
      pseudonym: t.pseudonym || "",
      transactionHash: t.transaction_hash || t.transactionHash || "",
    };
  }

  function holders(body) {
    const { rows } = page(body);
    return rows.map((g) => ({
      token: g.token_id || g.token || "",
      holders: (g.holders || []).map((h) => ({
        proxyWallet: h.proxy_wallet || h.proxyWallet || "",
        name: h.name || "",
        pseudonym: h.pseudonym || "",
        amount: num(h.amount),
        outcomeIndex: h.outcome_index ?? h.outcomeIndex ?? 0,
      })),
    }));
  }

  function leaderboardUser(body) {
    const data = body && Object.prototype.hasOwnProperty.call(body, "data") ? body.data : body;
    if (!data || Array.isArray(data)) return null;
    return {
      proxyWallet: data.user_id || data.proxyWallet || "",
      userName: data.user_name || data.userName || "",
      pnl: data.pnl,
      vol: data.volume ?? data.vol,
      profileImage: data.profile_image || data.profileImage || "",
    };
  }

  return { base, page, position, activity, trade, holders, leaderboardUser };
})();
