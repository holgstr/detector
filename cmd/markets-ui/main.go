package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
)

func main() {
	addr := flag.String("addr", ":8080", "Listen address")
	flag.Parse()

	client := polymarket.NewClient()
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/markets", handleMarkets(client))

	log.Printf("markets UI on http://localhost%s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handleMarkets(client *polymarket.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		tag := q.Get("tag")
		if tag == "" {
			tag = "elections"
		}
		minVol := 10000.0
		if v := q.Get("min_volume_24h"); v != "" {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				http.Error(w, "invalid min_volume_24h", http.StatusBadRequest)
				return
			}
			minVol = f
		}
		limit := 0
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				http.Error(w, "invalid limit", http.StatusBadRequest)
				return
			}
			limit = n
		}

		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()

		catalog, err := client.ListMarkets(ctx, polymarket.ListMarketsOptions{
			TagSlug:     tag,
			MinVolume24: minVol,
			BinaryOnly:  true,
			ActiveOnly:  true,
			Limit:       limit,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(catalog)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>detector · markets</title>
<link rel="preconnect" href="https://fonts.googleapis.com" />
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500&family=Syne:wght@600;700;800&display=swap" rel="stylesheet" />
<style>
  :root {
    --bg0: #e8efe8;
    --bg1: #f4f7f2;
    --ink: #14201a;
    --muted: #5a6b60;
    --line: rgba(20, 32, 26, 0.14);
    --accent: #0f6b4c;
    --accent-soft: rgba(15, 107, 76, 0.12);
    --warn: #8a4b12;
    --card: rgba(255, 255, 255, 0.55);
  }
  * { box-sizing: border-box; }
  html, body { margin: 0; min-height: 100%; }
  body {
    font-family: "IBM Plex Mono", ui-monospace, monospace;
    color: var(--ink);
    background:
      radial-gradient(1200px 600px at 10% -10%, #cfe0d4 0%, transparent 60%),
      radial-gradient(900px 500px at 100% 0%, #d7e4c7 0%, transparent 55%),
      linear-gradient(180deg, var(--bg1), var(--bg0));
  }
  .wrap {
    max-width: 980px;
    margin: 0 auto;
    padding: 2rem 1.25rem 4rem;
  }
  header {
    display: grid;
    gap: 0.75rem;
    margin-bottom: 1.75rem;
  }
  .brand {
    font-family: Syne, system-ui, sans-serif;
    font-weight: 800;
    font-size: clamp(2rem, 5vw, 3rem);
    letter-spacing: -0.04em;
    line-height: 0.95;
  }
  .brand span { color: var(--accent); }
  .sub {
    color: var(--muted);
    font-size: 0.85rem;
    max-width: 42rem;
  }
  .controls {
    display: grid;
    grid-template-columns: 1.4fr 0.7fr 0.7fr auto;
    gap: 0.65rem;
    align-items: end;
    padding: 0.9rem;
    border: 1px solid var(--line);
    background: var(--card);
    backdrop-filter: blur(8px);
  }
  @media (max-width: 760px) {
    .controls { grid-template-columns: 1fr 1fr; }
    .controls .search { grid-column: 1 / -1; }
  }
  label {
    display: grid;
    gap: 0.35rem;
    font-size: 0.68rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--muted);
  }
  input, select, button {
    font: inherit;
    color: var(--ink);
    border: 1px solid var(--line);
    background: #fff;
    padding: 0.7rem 0.75rem;
    width: 100%;
  }
  input:focus, select:focus, button:focus {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  button {
    background: var(--accent);
    color: #f4fff8;
    border-color: transparent;
    cursor: pointer;
    font-weight: 500;
    letter-spacing: 0.02em;
  }
  button:hover { filter: brightness(1.05); }
  button:disabled { opacity: 0.55; cursor: wait; }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem 1.25rem;
    margin: 1rem 0 0.5rem;
    color: var(--muted);
    font-size: 0.78rem;
  }
  .meta strong { color: var(--ink); font-weight: 500; }
  .err {
    margin-top: 1rem;
    padding: 0.85rem 1rem;
    border: 1px solid rgba(138, 75, 18, 0.35);
    background: rgba(138, 75, 18, 0.08);
    color: var(--warn);
    font-size: 0.85rem;
  }
  .list { display: grid; gap: 0.55rem; margin-top: 0.75rem; }
  a.row {
    display: grid;
    grid-template-columns: 7.5rem 1fr auto;
    gap: 0.85rem;
    align-items: start;
    text-decoration: none;
    color: inherit;
    padding: 0.85rem 0.95rem;
    border: 1px solid var(--line);
    background: rgba(255,255,255,0.42);
    transition: background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  a.row:hover {
    background: #fff;
    border-color: rgba(15, 107, 76, 0.45);
    transform: translateY(-1px);
  }
  @media (max-width: 640px) {
    a.row { grid-template-columns: 1fr; gap: 0.35rem; }
  }
  .vol {
    font-variant-numeric: tabular-nums;
    color: var(--accent);
    font-size: 0.85rem;
  }
  .q {
    font-family: Syne, system-ui, sans-serif;
    font-weight: 700;
    font-size: 1.02rem;
    letter-spacing: -0.02em;
    line-height: 1.2;
  }
  .evt {
    margin-top: 0.25rem;
    color: var(--muted);
    font-size: 0.72rem;
  }
  .px {
    text-align: right;
    font-size: 0.78rem;
    color: var(--muted);
    white-space: nowrap;
  }
  .px b { color: var(--ink); font-weight: 500; }
  .empty {
    margin-top: 1.5rem;
    color: var(--muted);
    font-size: 0.9rem;
  }
  .pill {
    display: inline-block;
    padding: 0.15rem 0.45rem;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 0.68rem;
  }
</style>
</head>
<body>
  <div class="wrap">
    <header>
      <div class="brand">detector <span>markets</span></div>
      <div class="sub">Browse active Yes/No markets by tag, filtered on 24h volume. Search locally across the loaded catalog.</div>
    </header>

    <div class="controls">
      <label class="search">Search
        <input id="q" type="search" placeholder="Ethiopia, Fed, Senate…" autocomplete="off" />
      </label>
      <label>Tag
        <select id="tag">
          <option value="elections" selected>elections</option>
          <option value="politics">politics</option>
          <option value="crypto">crypto</option>
          <option value="sports">sports</option>
          <option value="geopolitics">geopolitics</option>
        </select>
      </label>
      <label>Min 24h vol
        <input id="minVol" type="number" min="0" step="1000" value="10000" />
      </label>
      <button id="load" type="button">Load</button>
    </div>

    <div class="meta" id="meta"></div>
    <div id="err" hidden class="err"></div>
    <div class="list" id="list"></div>
    <div class="empty" id="empty" hidden>No markets match.</div>
  </div>
<script>
const $ = (id) => document.getElementById(id);
let catalog = null;

function fmtVol(n) {
  if (n >= 1e6) return (n/1e6).toFixed(2) + "M";
  if (n >= 1e3) return (n/1e3).toFixed(1) + "k";
  return Math.round(n).toString();
}

function prices(m) {
  const p = m.outcome_prices || [];
  if (p.length < 2) return "";
  const y = Math.round(p[0]*100);
  const n = Math.round(p[1]*100);
  return "<b>Y " + y + "¢</b> · N " + n + "¢";
}

function render() {
  const list = $("list");
  const empty = $("empty");
  list.innerHTML = "";
  if (!catalog) { empty.hidden = true; return; }

  const needle = $("q").value.trim().toLowerCase();
  const rows = (catalog.markets || []).filter(m => {
    if (!needle) return true;
    const hay = [m.question, m.slug, m.event_title, m.event_slug].join(" ").toLowerCase();
    return hay.includes(needle);
  });

  $("meta").innerHTML =
    "<span>tag <strong>" + (catalog.tag?.label || catalog.tag?.slug || "") + "</strong></span>" +
    "<span>loaded <strong>" + (catalog.count || 0) + "</strong></span>" +
    "<span>showing <strong>" + rows.length + "</strong></span>" +
    "<span>min 24h <strong>&gt; " + fmtVol(catalog.min_volume_24hr || 0) + "</strong></span>" +
    (catalog.fetched_at ? "<span>fetched <strong>" + catalog.fetched_at + "</strong></span>" : "");

  if (!rows.length) { empty.hidden = false; return; }
  empty.hidden = true;

  const frag = document.createDocumentFragment();
  for (const m of rows) {
    const a = document.createElement("a");
    a.className = "row";
    a.href = m.url || ("https://polymarket.com/market/" + m.slug);
    a.target = "_blank";
    a.rel = "noopener noreferrer";
    a.innerHTML =
      '<div class="vol">' + fmtVol(m.volume_24hr || 0) + ' <span class="pill">24h</span></div>' +
      '<div><div class="q"></div><div class="evt"></div></div>' +
      '<div class="px">' + prices(m) + '</div>';
    a.querySelector(".q").textContent = m.question || m.slug;
    a.querySelector(".evt").textContent = m.event_title || m.event_slug || "";
    frag.appendChild(a);
  }
  list.appendChild(frag);
}

async function load() {
  const btn = $("load");
  const err = $("err");
  err.hidden = true;
  btn.disabled = true;
  btn.textContent = "Loading…";
  try {
    const tag = $("tag").value;
    const min = $("minVol").value || "0";
    const res = await fetch("/api/markets?tag=" + encodeURIComponent(tag) + "&min_volume_24h=" + encodeURIComponent(min));
    if (!res.ok) throw new Error(await res.text());
    catalog = await res.json();
    render();
  } catch (e) {
    err.hidden = false;
    err.textContent = String(e.message || e);
  } finally {
    btn.disabled = false;
    btn.textContent = "Load";
  }
}

$("q").addEventListener("input", render);
$("load").addEventListener("click", load);
$("tag").addEventListener("change", load);
$("minVol").addEventListener("keydown", (e) => { if (e.key === "Enter") load(); });
load();
</script>
</body>
</html>
`
