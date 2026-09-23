// Personal Telegram alerts for tracked-sharp non-sports trades.
//
// Setup:
//  1. Message @BotFather → /newbot → copy the token
//  2. export TELEGRAM_BOT_TOKEN=...
//  3. go run ./cmd/activity-bot
//  4. Open the bot in Telegram and send /start
//
// Optional: TELEGRAM_CHAT_ID if you already know it.
// Chat: /minsize 100 — same floor as the Activity tab "Min size $".
// Same-market same-direction fills are aggregated first, then the floor applies.
// /net 6h Flip and /net Flip 6h are the same; short names match (Flip → Flipadelphia).
// /pos <market> lists tracked holdings; words, slugs, and URLs all resolve.
// /holders <market> lists the top 10 holders on each side (net if a wallet holds both), with acquisition price.
// /port [N] <trader> lists that wallet's open non-sports nets of $100+ (shares, acquisition and current price; any Polymarket name). N keeps the top N by market value.
// /lasttrades [trader] [market] [Nh] lists recent fills (default 24h; names need not be tracked; omit trader = all tracked).
// /kelly <price> <fv> prints full, half, 1/3, and 1/4 Kelly % of bankroll.
// /ob <market> prints the 4 closest Yes CLOB ticks on each side with size
// (keyword queries pick large tracked exposure, else the most traded live market).
// /obp <market> prints the same ladder from Pascal (an event name shows every outcome).
// /obk <market> prints that ladder from Kalshi (an event name shows every outcome).
// /alert <market> finds a market then asks for a Yes ask price and min size;
// it pings once when that size is sitting at that price or lower (take), then every 1h.
// Each ping shows the market name and the same inside ladder as /ob.
// /unalert <market> stops a watch. /cancel aborts the confirm step.
// /pricewatch <market> <YES|NO> <cents> pings when that side's midpoint moves by N cents,
// then re-anchors there. A one-sided book uses that quote. Fills and spread wiggles
// that leave the midpoint inside the band stay quiet.
// /unpricewatch <market> stops one.
// /tracked lists watched names; /add and /unadd take a wallet id or name (name → current id).
// /update pulls origin/main, rebuilds, and restarts (bound chat only).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/holgstr/detector/internal/alert"
	"github.com/holgstr/detector/internal/kalshi"
	"github.com/holgstr/detector/internal/pascal"
	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
	"github.com/holgstr/detector/internal/telegram"
)

func main() {
	interval := flag.Duration("interval", 20*time.Second, "Poll interval")
	statePath := flag.String("state", "data/activity-bot-state.json", "Checkpoint path (seen fills + chat id)")
	minUSD := flag.Float64("min-usd", 100, "Default min USDC (overridden by /minsize in chat)")
	limit := flag.Int("limit", 100, "Activity rows to fetch per wallet (max 500)")
	token := flag.String("token", os.Getenv("TELEGRAM_BOT_TOKEN"), "Bot token (or TELEGRAM_BOT_TOKEN)")
	chat := flag.String("chat", os.Getenv("TELEGRAM_CHAT_ID"), "Chat id (or TELEGRAM_CHAT_ID); otherwise send /start")
	dryRun := flag.Bool("dry-run", false, "Print alerts to stdout instead of Telegram")
	once := flag.Bool("once", false, "Single poll then exit")
	replay := flag.Bool("replay", false, "Alert the current window instead of seeding quietly on first run")
	announceUpdate := flag.String("announce-update", "", "Internal: SHA to report after a /update restart")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("activity-bot ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	state, err := alert.LoadState(*statePath)
	if err != nil {
		log.Fatalf("load state: %v", err)
	}
	if id, err := strconv.ParseInt(strings.TrimSpace(*chat), 10, 64); err == nil && id != 0 {
		state.ChatID = id
	}
	if *replay {
		state.Seeded = true
		state.Seen = map[string]int64{}
	}
	alert.ApplyTracked(state)

	b := &bot{
		state:       state,
		path:        *statePath,
		fallbackMin: *minUSD,
		announce:    strings.TrimSpace(*announceUpdate),
	}

	client := polymarket.NewClient()
	client.Workers = 3
	pascalAPI := pascal.NewClient()
	kalshiAPI := kalshi.NewClient()

	var tg *telegram.Client
	if !*dryRun {
		if strings.TrimSpace(*token) == "" {
			log.Fatal("set TELEGRAM_BOT_TOKEN (or pass -token), or use -dry-run")
		}
		tg = telegram.New(*token)
		me, err := tg.GetMe(ctx)
		if err != nil {
			log.Fatalf("telegram token rejected: %v", err)
		}
		if me.Username != "" {
			log.Printf("bot @%s  https://t.me/%s", me.Username, me.Username)
		}
		if err := tg.DeleteWebhook(ctx); err != nil {
			log.Printf("deleteWebhook: %v", err)
		}
		if state.ChatID == 0 {
			if me.Username != "" {
				log.Printf("waiting for /start — open https://t.me/%s and send any message", me.Username)
			} else {
				log.Print("waiting for /start in Telegram…")
			}
			log.Print("restarts consume the previous /start; ping the bot again if this hangs")
		}
		go listenChat(ctx, tg, client, pascalAPI, kalshiAPI, b)
		if err := b.waitBound(ctx); err != nil {
			log.Fatalf("telegram: %v", err)
		}
		log.Printf("chat %d bound; min size %s", b.chatID(), formatMin(b.minUSD()))
		if b.announce != "" {
			msg := fmt.Sprintf("Now running %s from GitHub.", shortSHA(b.announce))
			if err := tg.SendMessage(ctx, b.chatID(), msg); err != nil {
				log.Printf("announce update: %v", err)
			}
		}
	}

	poll := func() error {
		return runPoll(ctx, client, tg, b, *limit, *dryRun)
	}

	for {
		if err := poll(); err != nil {
			log.Printf("poll: %v", err)
		}
		if *once {
			return
		}
		select {
		case <-ctx.Done():
			log.Print("stopped")
			return
		case <-time.After(*interval):
		}
	}
}

type bot struct {
	mu          sync.Mutex
	state       *alert.State
	path        string
	fallbackMin float64
	quietPolls  int
	announce    string
}

func (b *bot) minUSD() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state.EffectiveMinUSD(b.fallbackMin)
}

func (b *bot) chatID() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state.ChatID
}

func (b *bot) waitBound(ctx context.Context) error {
	nudge := time.NewTicker(30 * time.Second)
	defer nudge.Stop()
	for {
		b.mu.Lock()
		id := b.state.ChatID
		b.mu.Unlock()
		if id != 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-nudge.C:
			log.Print("still waiting for /start — send any message to the bot (a restart does not reuse the last one)")
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func runPoll(ctx context.Context, api *polymarket.Client, tg *telegram.Client, b *bot, limit int, dryRun bool) error {
	acts, err := api.FetchActivityBatch(ctx, sharps.Addresses(), polymarket.FetchActivityOptions{
		Limit: limit,
		Type:  "TRADE",
	})
	if err != nil && len(acts) == 0 {
		return err
	}
	if err != nil {
		log.Printf("activity fetch (partial): %v", err)
	}

	b.mu.Lock()
	minUSD := b.state.EffectiveMinUSD(b.fallbackMin)
	plan, err := alert.BuildPlan(ctx, api, b.state, acts, minUSD)
	if err != nil {
		b.mu.Unlock()
		return err
	}
	b.state.CommitDropped(plan)
	chatID := b.state.ChatID
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	if plan.Stale > 0 {
		log.Printf("ignored %d fills older than %s", plan.Stale, alert.MaxAlertAge)
	}
	if plan.FirstRun {
		n := len(b.state.Seen)
		b.mu.Unlock()
		log.Printf("seeded %d fills; waiting for new non-sports trades", n)
		if err := runPriceAlerts(ctx, api, tg, b, dryRun); err != nil {
			log.Printf("price alerts: %v", err)
		}
		if err := runPriceWatches(ctx, api, tg, b, dryRun); err != nil {
			log.Printf("price watches: %v", err)
		}
		return nil
	}
	alerts := plan.Alerts
	b.mu.Unlock()

	if len(alerts) == 0 {
		b.mu.Lock()
		b.quietPolls++
		n := b.quietPolls
		b.mu.Unlock()
		if n == 1 || n%15 == 0 {
			log.Printf("poll quiet (%d in a row, min %s)", n, formatMin(minUSD))
		}
		if err := runPriceAlerts(ctx, api, tg, b, dryRun); err != nil {
			log.Printf("price alerts: %v", err)
		}
		if err := runPriceWatches(ctx, api, tg, b, dryRun); err != nil {
			log.Printf("price watches: %v", err)
		}
		return nil
	}
	b.mu.Lock()
	b.quietPolls = 0
	b.mu.Unlock()

	alert.AttachNetPositions(ctx, api, alerts)

	sent := 0
	for _, a := range alerts {
		text := alert.Format(a)
		if dryRun {
			fmt.Println(text)
			fmt.Println("---")
		} else if err := tg.SendMessage(ctx, chatID, text); err != nil {
			log.Printf("send %s: %v", a.Name, err)
			continue
		}
		b.mu.Lock()
		b.state.CommitSent(a)
		if err := alert.SaveState(b.path, b.state); err != nil {
			log.Printf("save state: %v", err)
		}
		b.mu.Unlock()
		sent++
	}
	log.Printf("sent %d/%d alerts", sent, len(alerts))
	if err := runPriceAlerts(ctx, api, tg, b, dryRun); err != nil {
		log.Printf("price alerts: %v", err)
	}
	if err := runPriceWatches(ctx, api, tg, b, dryRun); err != nil {
		log.Printf("price watches: %v", err)
	}
	return nil
}

func listenChat(ctx context.Context, tg *telegram.Client, api *polymarket.Client, books *pascal.Client, kbooks *kalshi.Client, b *bot) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		b.mu.Lock()
		offset := b.state.TelegramOffset
		b.mu.Unlock()

		updates, err := tg.GetUpdates(ctx, offset, 25)
		if err != nil {
			log.Printf("getUpdates: %v", err)
			wait := 5 * time.Second
			if telegram.PollBlocked(err) {
				if dErr := tg.DeleteWebhook(ctx); dErr != nil {
					log.Printf("deleteWebhook: %v", dErr)
				}
				wait = time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			continue
		}
		for _, u := range updates {
			handleUpdate(ctx, tg, api, books, kbooks, b, u)
		}
	}
}

func handleUpdate(ctx context.Context, tg *telegram.Client, api *polymarket.Client, books *pascal.Client, kbooks *kalshi.Client, b *bot, u telegram.Update) {
	b.mu.Lock()
	b.state.TelegramOffset = u.UpdateID + 1
	if u.Message == nil || u.Message.Chat.ID == 0 {
		_ = alert.SaveState(b.path, b.state)
		b.mu.Unlock()
		return
	}

	first := false
	if b.state.ChatID == 0 {
		b.state.ChatID = u.Message.Chat.ID
		first = true
	}
	if u.Message.Chat.ID != b.state.ChatID {
		_ = alert.SaveState(b.path, b.state)
		b.mu.Unlock()
		return
	}

	cmd := alert.ParseCommand(u.Message.Text)
	min := b.state.EffectiveMinUSD(b.fallbackMin)
	if cmd.Cmd == alert.CmdMinSizeSet {
		b.state.SetMinUSD(cmd.MinUSD)
		min = cmd.MinUSD
		log.Printf("min size set to %s", formatMin(min))
	}
	pending := b.state.PendingPriceAlert != nil
	if pending && cmd.Cmd != alert.CmdNone && cmd.Cmd != alert.CmdAlert && cmd.Cmd != alert.CmdUnalert && cmd.Cmd != alert.CmdCancel {
		b.state.ClearPendingPriceAlert()
	}
	chatID := b.state.ChatID
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	log.Printf("chat %d text=%q cmd=%d first=%v", chatID, u.Message.Text, cmd.Cmd, first)
	b.mu.Unlock()

	if pending && cmd.Cmd == alert.CmdNone {
		replyAlertConfirm(ctx, tg, b, chatID, u.Message.Text)
		return
	}

	for _, text := range commandReplies(first, cmd, min) {
		if err := tg.SendMessage(ctx, chatID, text); err != nil {
			log.Printf("reply: %v", err)
		}
	}
	if cmd.Cmd == alert.CmdNet {
		replyNet(ctx, tg, api, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdPos {
		replyPos(ctx, tg, api, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdHolders {
		replyHolders(ctx, tg, api, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdPort {
		replyPort(ctx, tg, api, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdLastTrades {
		replyLastTrades(ctx, tg, api, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdOB {
		replyOB(ctx, tg, api, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdOBP {
		replyOBP(ctx, tg, books, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdOBK {
		replyOBK(ctx, tg, kbooks, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdAlert {
		replyAlert(ctx, tg, api, b, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdUnalert {
		replyUnalert(ctx, tg, b, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdPriceWatch {
		replyPriceWatch(ctx, tg, api, b, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdUnpriceWatch {
		replyUnpriceWatch(ctx, tg, b, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdCancel {
		replyCancel(ctx, tg, b, chatID)
	}
	if cmd.Cmd == alert.CmdTracked {
		replyTracked(ctx, tg, chatID)
	}
	if cmd.Cmd == alert.CmdAdd {
		replyAdd(ctx, tg, api, b, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdUnadd {
		replyUnadd(ctx, tg, api, b, chatID, cmd)
	}
	if cmd.Cmd == alert.CmdUpdate {
		replyUpdate(ctx, tg, chatID)
	}
}

func commandReplies(first bool, cmd alert.ParsedCommand, min float64) []string {
	var replies []string
	if first || cmd.Cmd == alert.CmdStart {
		replies = append(replies, welcome(min))
	}
	switch cmd.Cmd {
	case alert.CmdMinSizeShow, alert.CmdMinSizeSet:
		if !first && cmd.Cmd != alert.CmdStart {
			replies = append(replies, alert.MinSizeStatus(min))
		}
	case alert.CmdHelp:
		replies = append(replies, alert.HelpText(min))
	case alert.CmdKelly:
		if cmd.Price == 0 && cmd.FV == 0 {
			replies = append(replies, alert.KellyUsage)
		} else {
			replies = append(replies, alert.KellyText(cmd.Price, cmd.FV))
		}
	case alert.CmdNone:
		if !first {
			replies = append(replies, "Still here. /help for commands.")
		}
	}
	return replies
}

func welcome(minUSD float64) string {
	return fmt.Sprintf("Watching %d wallets. I'll ping you on new trades.\n%s\n/help for commands.",
		len(sharps.List()), alert.MinSizeStatus(minUSD))
}

func replyUpdate(ctx context.Context, tg *telegram.Client, chatID int64) {
	if !tryBeginUpdate() {
		if err := tg.SendMessage(ctx, chatID, "An update is already running."); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	defer endUpdate()

	if err := tg.SendMessage(ctx, chatID, "Pulling origin/main from GitHub…"); err != nil {
		log.Printf("reply: %v", err)
	}
	workCtx, cancel := context.WithTimeout(ctx, updateTimeout)
	res, err := prepareUpdate(workCtx)
	cancel()
	if err != nil {
		if sendErr := tg.SendMessage(ctx, chatID, "Update failed: "+clipErr(err)); sendErr != nil {
			log.Printf("reply: %v", sendErr)
		}
		log.Printf("update: %v", err)
		return
	}
	if !res.Changed {
		msg := fmt.Sprintf("Already on %s. Nothing to pull.", shortSHA(res.To))
		if err := tg.SendMessage(ctx, chatID, msg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	msg := fmt.Sprintf("Restarting on %s…", shortSHA(res.To))
	if err := tg.SendMessage(ctx, chatID, msg); err != nil {
		log.Printf("reply: %v", err)
	}
	log.Printf("update %s -> %s; exec %s", shortSHA(res.From), shortSHA(res.To), res.Bin)
	if err := restartWithBinary(res.Bin, res.To); err != nil {
		if sendErr := tg.SendMessage(ctx, chatID, "Restart failed (still on the old process): "+clipErr(err)); sendErr != nil {
			log.Printf("reply: %v", sendErr)
		}
		log.Printf("exec: %v", err)
	}
}

func replyTracked(ctx context.Context, tg *telegram.Client, chatID int64) {
	msg := alert.FormatTrackedList(sharps.List())
	if err := tg.SendMessage(ctx, chatID, msg); err != nil {
		log.Printf("reply: %v", err)
	}
}

func replyAdd(ctx context.Context, tg *telegram.Client, api *polymarket.Client, b *bot, chatID int64, cmd alert.ParsedCommand) {
	w, errMsg := alert.ResolveAddWallet(ctx, api, cmd.Query)
	if errMsg != "" {
		if err := tg.SendMessage(ctx, chatID, errMsg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}

	b.mu.Lock()
	already := false
	for _, x := range b.state.ActiveWallets() {
		if x.Address == w.Address {
			already = true
			break
		}
	}
	b.mu.Unlock()
	if already {
		msg := fmt.Sprintf("Already tracking %s.", displayTrader(w))
		if err := tg.SendMessage(ctx, chatID, msg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}

	acts, fetchErr := api.FetchActivity(ctx, polymarket.FetchActivityOptions{
		User:  w.Address,
		Limit: 100,
		Type:  "TRADE",
	})

	b.mu.Lock()
	if fetchErr == nil {
		b.state.MarkActivitySeen(acts)
	}
	already = b.state.AddWallet(w)
	alert.ApplyTracked(b.state)
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	n := len(sharps.List())
	b.mu.Unlock()

	if already {
		msg := fmt.Sprintf("Already tracking %s.", displayTrader(w))
		if err := tg.SendMessage(ctx, chatID, msg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}

	msg := fmt.Sprintf("Now tracking %s (%d wallets).", displayTrader(w), n)
	if fetchErr != nil {
		msg += " Couldn't seed recent fills — the next poll may replay some."
	}
	if err := tg.SendMessage(ctx, chatID, msg); err != nil {
		log.Printf("reply: %v", err)
	}
	log.Printf("add %s %s wallets=%d", w.Name, w.Address, n)
}

func replyUnadd(ctx context.Context, tg *telegram.Client, api *polymarket.Client, b *bot, chatID int64, cmd alert.ParsedCommand) {
	w, errMsg := alert.ResolveUnaddWallet(ctx, api, cmd.Query)
	if errMsg != "" {
		if err := tg.SendMessage(ctx, chatID, errMsg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}

	b.mu.Lock()
	got, ok := b.state.UnaddWallet(w.Address)
	alert.ApplyTracked(b.state)
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	n := len(sharps.List())
	b.mu.Unlock()

	if !ok {
		msg := fmt.Sprintf("Not tracking %s.", displayTrader(w))
		if err := tg.SendMessage(ctx, chatID, msg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	label := displayTrader(got)
	if strings.TrimSpace(got.Name) == "" {
		label = displayTrader(w)
	}
	msg := fmt.Sprintf("Stopped tracking %s (%d wallets).", label, n)
	if err := tg.SendMessage(ctx, chatID, msg); err != nil {
		log.Printf("reply: %v", err)
	}
	log.Printf("unadd %s %s wallets=%d", got.Name, got.Address, n)
}

func displayTrader(w sharps.Wallet) string {
	n := strings.TrimSpace(w.Name)
	if n != "" {
		return n
	}
	addr := strings.ToLower(strings.TrimSpace(w.Address))
	if len(addr) >= 12 {
		return addr[:6] + "…" + addr[len(addr)-4:]
	}
	return addr
}

func replyNet(ctx context.Context, tg *telegram.Client, api *polymarket.Client, chatID int64, cmd alert.ParsedCommand) {
	wallets, errMsg := alert.ResolveNetWallets(cmd.Trader)
	if errMsg != "" {
		if err := tg.SendMessage(ctx, chatID, errMsg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	label := strings.TrimSpace(cmd.Trader)
	if len(wallets) == 1 {
		label = wallets[0].Name
	}
	rep, err := alert.FetchNetReport(ctx, api, wallets, cmd.Window)
	chunks := alert.FormatNetReport(rep, label)
	if err != nil && len(rep.Traders) == 0 {
		chunks = []string{fmt.Sprintf("Couldn't load activity: %v", err)}
	} else if err != nil {
		chunks = append(chunks, fmt.Sprintf("(partial fetch: %v)", err))
	}
	for _, text := range chunks {
		if err := tg.SendMessage(ctx, chatID, text); err != nil {
			log.Printf("reply: %v", err)
		}
	}
	log.Printf("net %s trader=%q markets=%d", rep.Window, cmd.Trader, countMarkets(rep))
}

func replyPort(ctx context.Context, tg *telegram.Client, api *polymarket.Client, chatID int64, cmd alert.ParsedCommand) {
	w, errMsg := alert.ResolvePortWallet(ctx, api, cmd.Trader)
	if errMsg != "" {
		if err := tg.SendMessage(ctx, chatID, errMsg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	rep, err := alert.FetchPortReport(ctx, api, w)
	rep.Limit = cmd.Limit
	if err != nil && len(rep.Holdings) == 0 {
		if sendErr := tg.SendMessage(ctx, chatID, fmt.Sprintf("Couldn't load positions: %v", err)); sendErr != nil {
			log.Printf("reply: %v", sendErr)
		}
		return
	}
	chunks := alert.FormatPortReport(rep)
	if err != nil {
		chunks = append(chunks, fmt.Sprintf("(partial fetch: %v)", err))
	}
	for _, text := range chunks {
		if err := tg.SendMessage(ctx, chatID, text); err != nil {
			log.Printf("reply: %v", err)
		}
	}
	shown := len(rep.Holdings)
	if cmd.Limit > 0 && shown > cmd.Limit {
		shown = cmd.Limit
	}
	log.Printf("port trader=%s markets=%d shown=%d", w.Name, len(rep.Holdings), shown)
}

func replyLastTrades(ctx context.Context, tg *telegram.Client, api *polymarket.Client, chatID int64, cmd alert.ParsedCommand) {
	wallets, market, label, errMsg := alert.ResolveLastTradesWallets(ctx, api, cmd.Trader, cmd.Market)
	if errMsg != "" {
		if err := tg.SendMessage(ctx, chatID, errMsg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	rep, err := alert.FetchLastTradesReport(ctx, api, wallets, cmd.Window, market, label)
	chunks := alert.FormatLastTradesReport(rep)
	if err != nil && len(rep.Trades) == 0 {
		chunks = []string{fmt.Sprintf("Couldn't load activity: %v", err)}
	} else if err != nil {
		chunks = append(chunks, fmt.Sprintf("(partial fetch: %v)", err))
	}
	for _, text := range chunks {
		if err := tg.SendMessage(ctx, chatID, text); err != nil {
			log.Printf("reply: %v", err)
		}
	}
	log.Printf("lasttrades %s trader=%q market=%q fills=%d", rep.Window, cmd.Trader, cmd.Market, len(rep.Trades))
}

func replyOB(ctx context.Context, tg *telegram.Client, api *polymarket.Client, chatID int64, cmd alert.ParsedCommand) {
	query := strings.TrimSpace(cmd.Market)
	if query == "" {
		if err := tg.SendMessage(ctx, chatID, "Usage: /ob <market> — words, slug, or URL."); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	rep, err := alert.FetchOBReport(ctx, api, query, sharps.List())
	if err != nil && len(rep.Books) == 0 {
		msg := fmt.Sprintf("No active market matching %q.", query)
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "resolved") {
			msg = fmt.Sprintf("%q is resolved, not an active market.", query)
		} else if !strings.Contains(low, "active market") && !strings.Contains(low, "no active") {
			msg = fmt.Sprintf("Couldn't load order book: %v", err)
		}
		if err := tg.SendMessage(ctx, chatID, msg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	text := alert.FormatOBReport(rep)
	if err != nil {
		text += fmt.Sprintf("\n(partial fetch: %v)", err)
	}
	if err := tg.SendMessage(ctx, chatID, text); err != nil {
		log.Printf("reply: %v", err)
	}
	log.Printf("ob query=%q market=%q outcomes=%d", query, rep.Title, len(rep.Books))
}

func replyOBP(ctx context.Context, tg *telegram.Client, api *pascal.Client, chatID int64, cmd alert.ParsedCommand) {
	query := strings.TrimSpace(cmd.Market)
	if query == "" {
		if err := tg.SendMessage(ctx, chatID, "Usage: /obp <market> — words or a Pascal symbol."); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	rep, err := alert.FetchPascalOB(ctx, api, query)
	if err != nil && len(rep.Books) == 0 {
		msg := fmt.Sprintf("No live Pascal market matching %q.", query)
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "short query") {
			msg = "Usage: /obp <market> — at least 3 characters, or a symbol like FL_GOV_2026.REP."
		} else if strings.Contains(low, "resolved") {
			msg = fmt.Sprintf("%q is resolved, not a live Pascal market.", query)
		} else if !strings.Contains(low, "no live") && !strings.Contains(low, "matching") {
			msg = fmt.Sprintf("Couldn't load Pascal order book: %v", err)
		}
		if err := tg.SendMessage(ctx, chatID, msg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	text := alert.FormatPascalOB(rep)
	if err != nil {
		text += fmt.Sprintf("\n(partial fetch: %v)", err)
	}
	if err := tg.SendMessage(ctx, chatID, text); err != nil {
		log.Printf("reply: %v", err)
	}
	log.Printf("obp query=%q market=%q outcomes=%d", query, rep.Title, len(rep.Books))
}

func replyOBK(ctx context.Context, tg *telegram.Client, api *kalshi.Client, chatID int64, cmd alert.ParsedCommand) {
	query := strings.TrimSpace(cmd.Market)
	if query == "" {
		if err := tg.SendMessage(ctx, chatID, "Usage: /obk <market> — words or a Kalshi ticker."); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	rep, err := alert.FetchKalshiOB(ctx, api, query)
	if err != nil && len(rep.Books) == 0 {
		msg := fmt.Sprintf("No live Kalshi market matching %q.", query)
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "short query") {
			msg = "Usage: /obk <market> — at least 3 characters, or a ticker like KXFEDDECISION-26OCT-H0."
		} else if strings.Contains(low, "resolved") {
			msg = fmt.Sprintf("%q is resolved, not a live Kalshi market.", query)
		} else if !strings.Contains(low, "no live") && !strings.Contains(low, "matching") {
			msg = fmt.Sprintf("Couldn't load Kalshi order book: %v", err)
		}
		if err := tg.SendMessage(ctx, chatID, msg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	text := alert.FormatKalshiOB(rep)
	if err != nil {
		text += fmt.Sprintf("\n(partial fetch: %v)", err)
	}
	if err := tg.SendMessage(ctx, chatID, text); err != nil {
		log.Printf("reply: %v", err)
	}
	log.Printf("obk query=%q market=%q outcomes=%d", query, rep.Title, len(rep.Books))
}

func runPriceAlerts(ctx context.Context, api *polymarket.Client, tg *telegram.Client, b *bot, dryRun bool) error {
	b.mu.Lock()
	alerts := append([]alert.PriceAlert(nil), b.state.PriceAlerts...)
	chatID := b.state.ChatID
	b.mu.Unlock()
	if len(alerts) == 0 {
		return nil
	}
	fired, next := alert.CheckPriceAlerts(ctx, api, alerts, time.Now())
	b.mu.Lock()
	b.state.ApplyPriceAlertPoll(next)
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	armed := &alert.State{PriceAlerts: append([]alert.PriceAlert(nil), b.state.PriceAlerts...)}
	b.mu.Unlock()

	sent := 0
	for _, hit := range fired {
		if !alert.PriceAlertStillArmed(armed, hit.Alert) {
			continue
		}
		text := alert.PriceAlertPingText(hit.Alert, hit.Size, hit.Book)
		if dryRun {
			fmt.Println(text)
			fmt.Println("---")
			sent++
			continue
		}
		if tg == nil || chatID == 0 {
			continue
		}
		if err := tg.SendMessage(ctx, chatID, text); err != nil {
			log.Printf("price alert %s: %v", hit.Alert.Title, err)
			continue
		}
		sent++
	}
	if sent > 0 {
		log.Printf("sent %d price alerts", sent)
	}
	return nil
}

func runPriceWatches(ctx context.Context, api *polymarket.Client, tg *telegram.Client, b *bot, dryRun bool) error {
	b.mu.Lock()
	watches := append([]alert.PriceWatch(nil), b.state.PriceWatches...)
	chatID := b.state.ChatID
	b.mu.Unlock()
	if len(watches) == 0 {
		return nil
	}
	fired, next := alert.CheckPriceWatches(ctx, api, watches, time.Now())
	b.mu.Lock()
	b.state.ApplyPriceWatchPoll(next)
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	armed := &alert.State{PriceWatches: append([]alert.PriceWatch(nil), b.state.PriceWatches...)}
	b.mu.Unlock()

	sent := 0
	for _, hit := range fired {
		if !alert.PriceWatchStillArmed(armed, hit.Watch) {
			continue
		}
		text := alert.PriceWatchPingText(hit)
		if dryRun {
			fmt.Println(text)
			fmt.Println("---")
			sent++
			continue
		}
		if tg == nil || chatID == 0 {
			continue
		}
		if err := tg.SendMessage(ctx, chatID, text); err != nil {
			log.Printf("price watch %s: %v", hit.Watch.Title, err)
			continue
		}
		sent++
	}
	if sent > 0 {
		log.Printf("sent %d price watches", sent)
	}
	return nil
}

func replyPriceWatch(ctx context.Context, tg *telegram.Client, api *polymarket.Client, b *bot, chatID int64, cmd alert.ParsedCommand) {
	query := strings.TrimSpace(cmd.Market)
	if query == "" {
		b.mu.Lock()
		list := alert.FormatPriceWatchList(b.state.PriceWatches)
		b.mu.Unlock()
		if err := tg.SendMessage(ctx, chatID, list); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	draft, errMsg := alert.ResolvePriceAlertMarket(ctx, api, query, sharps.List())
	if errMsg != "" {
		if err := tg.SendMessage(ctx, chatID, errMsg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	book, err := api.FetchOutcomeBook(ctx, draft.ConditionID, cmd.Outcome)
	if err != nil {
		title := strings.TrimSpace(draft.Title)
		if title == "" {
			title = query
		}
		msg := fmt.Sprintf("Couldn't load the %s book: %v", cmd.Outcome, err)
		if strings.Contains(strings.ToLower(err.Error()), "outcome") {
			msg = fmt.Sprintf("%s has no %s side.", title, cmd.Outcome)
		}
		if err := tg.SendMessage(ctx, chatID, msg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	row, errMsg := alert.NewPriceWatch(draft, cmd.Outcome, cmd.Delta, book, time.Now())
	if errMsg != "" {
		if err := tg.SendMessage(ctx, chatID, errMsg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	b.mu.Lock()
	b.state.UpsertPriceWatch(row)
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	b.mu.Unlock()
	if err := tg.SendMessage(ctx, chatID, alert.PriceWatchSetText(row)); err != nil {
		log.Printf("reply: %v", err)
	}
	log.Printf("price watch set market=%q outcome=%s anchor=%.4f delta=%.4f", row.Title, row.Outcome, row.Anchor, row.Delta)
}

func replyUnpriceWatch(ctx context.Context, tg *telegram.Client, b *bot, chatID int64, cmd alert.ParsedCommand) {
	query := strings.TrimSpace(cmd.Market)
	b.mu.Lock()
	got, ok := b.state.RemovePriceWatch(query)
	if !ok {
		n := len(b.state.PriceWatches)
		list := alert.FormatPriceWatchList(b.state.PriceWatches)
		b.mu.Unlock()
		msg := "No matching price watch."
		if query == "" && n > 1 {
			msg = "Which watch? /unpricewatch <market>\n" + list
		} else if n == 0 {
			msg = list
		} else if n > 1 {
			msg = "Which watch? Add YES or NO if both sides are on.\n" + list
		}
		if err := tg.SendMessage(ctx, chatID, msg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	title := strings.TrimSpace(got.Title)
	if title == "" {
		title = got.Slug
	}
	n := len(b.state.PriceWatches)
	b.mu.Unlock()
	msg := fmt.Sprintf("Stopped price watch on %s %s.", title, strings.ToUpper(got.Outcome))
	if n > 0 {
		msg += fmt.Sprintf(" %d left.", n)
	}
	if err := tg.SendMessage(ctx, chatID, msg); err != nil {
		log.Printf("reply: %v", err)
	}
	log.Printf("price watch removed market=%q outcome=%s", title, got.Outcome)
}

func replyAlert(ctx context.Context, tg *telegram.Client, api *polymarket.Client, b *bot, chatID int64, cmd alert.ParsedCommand) {
	query := strings.TrimSpace(cmd.Market)
	if query == "" {
		b.mu.Lock()
		list := alert.FormatPriceAlertList(b.state.PriceAlerts)
		b.mu.Unlock()
		if err := tg.SendMessage(ctx, chatID, list); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}

	draft, errMsg := alert.ResolvePriceAlertMarket(ctx, api, query, sharps.List())
	if errMsg != "" {
		if err := tg.SendMessage(ctx, chatID, errMsg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}

	if cmd.Price > 0 && cmd.MinSize > 0 {
		row := alert.PriceAlert{
			Title:       draft.Title,
			Slug:        draft.Slug,
			URL:         draft.URL,
			ConditionID: draft.ConditionID,
			Price:       cmd.Price,
			MinSize:     cmd.MinSize,
		}
		b.mu.Lock()
		b.state.UpsertPriceAlert(row)
		if err := alert.SaveState(b.path, b.state); err != nil {
			log.Printf("save state: %v", err)
		}
		b.mu.Unlock()
		if err := tg.SendMessage(ctx, chatID, alert.PriceAlertSetText(row)); err != nil {
			log.Printf("reply: %v", err)
		}
		log.Printf("price alert set market=%q price=%.4f min=%.0f", row.Title, row.Price, row.MinSize)
		return
	}

	b.mu.Lock()
	d := draft
	b.state.PendingPriceAlert = &d
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	b.mu.Unlock()
	if err := tg.SendMessage(ctx, chatID, alert.PriceAlertPrompt(draft)); err != nil {
		log.Printf("reply: %v", err)
	}
	log.Printf("price alert pending market=%q", draft.Title)
}

func replyAlertConfirm(ctx context.Context, tg *telegram.Client, b *bot, chatID int64, text string) {
	price, minSize, ok := alert.ParseAlertConfirm(text)
	b.mu.Lock()
	draft := b.state.PendingPriceAlert
	if !ok || draft == nil {
		b.mu.Unlock()
		if err := tg.SendMessage(ctx, chatID, "32 1000, or /cancel."); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	row := alert.PriceAlert{
		Title:       draft.Title,
		Slug:        draft.Slug,
		URL:         draft.URL,
		ConditionID: draft.ConditionID,
		Price:       price,
		MinSize:     minSize,
	}
	b.state.UpsertPriceAlert(row)
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	b.mu.Unlock()
	if err := tg.SendMessage(ctx, chatID, alert.PriceAlertSetText(row)); err != nil {
		log.Printf("reply: %v", err)
	}
	log.Printf("price alert set market=%q price=%.4f min=%.0f", row.Title, row.Price, row.MinSize)
}

func replyUnalert(ctx context.Context, tg *telegram.Client, b *bot, chatID int64, cmd alert.ParsedCommand) {
	query := strings.TrimSpace(cmd.Market)
	b.mu.Lock()
	got, ok := b.state.RemovePriceAlert(query)
	if !ok {
		n := len(b.state.PriceAlerts)
		list := alert.FormatPriceAlertList(b.state.PriceAlerts)
		b.mu.Unlock()
		msg := "No matching price alert."
		if query == "" && n > 1 {
			msg = "Which alert? /unalert <market>\n" + list
		} else if n == 0 {
			msg = list
		}
		if err := tg.SendMessage(ctx, chatID, msg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	b.state.ClearPendingPriceAlert()
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	title := strings.TrimSpace(got.Title)
	if title == "" {
		title = got.Slug
	}
	n := len(b.state.PriceAlerts)
	b.mu.Unlock()
	msg := fmt.Sprintf("Stopped price alert on %s.", title)
	if n > 0 {
		msg += fmt.Sprintf(" %d left.", n)
	}
	if err := tg.SendMessage(ctx, chatID, msg); err != nil {
		log.Printf("reply: %v", err)
	}
	log.Printf("price alert removed market=%q", title)
}

func replyCancel(ctx context.Context, tg *telegram.Client, b *bot, chatID int64) {
	b.mu.Lock()
	had := b.state.PendingPriceAlert != nil
	b.state.ClearPendingPriceAlert()
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	b.mu.Unlock()
	msg := "Nothing to cancel."
	if had {
		msg = "Cancelled. No price alert set."
	}
	if err := tg.SendMessage(ctx, chatID, msg); err != nil {
		log.Printf("reply: %v", err)
	}
}

func replyPos(ctx context.Context, tg *telegram.Client, api *polymarket.Client, chatID int64, cmd alert.ParsedCommand) {
	query := strings.TrimSpace(cmd.Market)
	if query == "" {
		if err := tg.SendMessage(ctx, chatID, "Usage: /pos <market> — words, slug, or URL."); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	rep, err := alert.FetchPosReport(ctx, api, query, sharps.List())
	if err != nil && len(rep.Holdings) == 0 && rep.Title == "" {
		msg := fmt.Sprintf("No active market matching %q.", query)
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "resolved") {
			msg = fmt.Sprintf("%q is resolved, not an active market.", query)
		} else if !strings.Contains(low, "active market") && !strings.Contains(low, "no active") {
			msg = fmt.Sprintf("Couldn't load market: %v", err)
		}
		if err := tg.SendMessage(ctx, chatID, msg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	chunks := alert.FormatPosReport(rep)
	if err != nil {
		chunks = append(chunks, fmt.Sprintf("(partial fetch: %v)", err))
	}
	for _, text := range chunks {
		if err := tg.SendMessage(ctx, chatID, text); err != nil {
			log.Printf("reply: %v", err)
		}
	}
	log.Printf("pos query=%q market=%q holders=%d", query, rep.Title, len(rep.Holdings))
}

func replyHolders(ctx context.Context, tg *telegram.Client, api *polymarket.Client, chatID int64, cmd alert.ParsedCommand) {
	query := strings.TrimSpace(cmd.Market)
	if query == "" {
		if err := tg.SendMessage(ctx, chatID, "Usage: /holders <market> — words, slug, or URL."); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	rep, err := alert.FetchHoldersReport(ctx, api, query, sharps.List())
	if err != nil && len(rep.Holdings) == 0 && rep.Title == "" {
		msg := fmt.Sprintf("No active market matching %q.", query)
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "resolved") {
			msg = fmt.Sprintf("%q is resolved, not an active market.", query)
		} else if !strings.Contains(low, "active market") && !strings.Contains(low, "no active") {
			msg = fmt.Sprintf("Couldn't load market: %v", err)
		}
		if err := tg.SendMessage(ctx, chatID, msg); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	if err != nil && len(rep.Holdings) == 0 {
		if err := tg.SendMessage(ctx, chatID, fmt.Sprintf("Couldn't load holders: %v", err)); err != nil {
			log.Printf("reply: %v", err)
		}
		return
	}
	chunks := alert.FormatHoldersReport(rep)
	for _, text := range chunks {
		if err := tg.SendMessage(ctx, chatID, text); err != nil {
			log.Printf("reply: %v", err)
		}
	}
	log.Printf("holders query=%q market=%q holders=%d", query, rep.Title, len(rep.Holdings))
}

func countMarkets(r alert.NetReport) int {
	n := 0
	for _, t := range r.Traders {
		n += len(t.Markets)
	}
	return n
}

func formatMin(n float64) string {
	if n <= 0 {
		return "off"
	}
	return fmt.Sprintf("$%.0f", n)
}
