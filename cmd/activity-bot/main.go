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

	b := &bot{
		state:       state,
		path:        *statePath,
		fallbackMin: *minUSD,
	}

	client := polymarket.NewClient()
	client.Workers = 3

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
		if state.ChatID == 0 {
			if me.Username != "" {
				log.Printf("waiting for /start — open https://t.me/%s and send any message", me.Username)
			} else {
				log.Print("waiting for /start in Telegram…")
			}
			log.Print("restarts consume the previous /start; ping the bot again if this hangs")
		}
		go listenChat(ctx, tg, client, b)
		if err := b.waitBound(ctx); err != nil {
			log.Fatalf("telegram: %v", err)
		}
		log.Printf("chat %d bound; min size %s", b.chatID(), formatMin(b.minUSD()))
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
	if plan.FirstRun {
		n := len(b.state.Seen)
		b.mu.Unlock()
		log.Printf("seeded %d fills; waiting for new non-sports trades", n)
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
	return nil
}

func listenChat(ctx context.Context, tg *telegram.Client, api *polymarket.Client, b *bot) {
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
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			handleUpdate(ctx, tg, api, b, u)
		}
	}
}

func handleUpdate(ctx context.Context, tg *telegram.Client, api *polymarket.Client, b *bot, u telegram.Update) {
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
	chatID := b.state.ChatID
	if err := alert.SaveState(b.path, b.state); err != nil {
		log.Printf("save state: %v", err)
	}
	b.mu.Unlock()

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
	}
	for _, text := range replies {
		if err := tg.SendMessage(ctx, chatID, text); err != nil {
			log.Printf("reply: %v", err)
		}
	}
	if cmd.Cmd == alert.CmdNet {
		replyNet(ctx, tg, api, chatID, cmd)
	}
}

func welcome(minUSD float64) string {
	return fmt.Sprintf("Watching %d wallets. I'll ping you on non-sports trades.\n%s\n/net 6h for net position changes.\n/help for commands.",
		len(sharps.Tracked), alert.MinSizeStatus(minUSD))
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
