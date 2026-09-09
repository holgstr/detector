// Personal Telegram alerts for tracked-sharp non-sports trades.
//
// Setup:
//  1. Message @BotFather → /newbot → copy the token
//  2. export TELEGRAM_BOT_TOKEN=...
//  3. go run ./cmd/activity-bot
//  4. Open the bot in Telegram and send /start
//
// Optional: TELEGRAM_CHAT_ID if you already know it.
// First poll is quiet (seeds the checkpoint). After that, each new
// non-sports TRADE from a tracked wallet is one message, with consecutive
// same-market fills collapsed.
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
	minUSD := flag.Float64("min-usd", 0, "Ignore fills below this USDC notional")
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
			if err := waitForChat(ctx, tg, state, *statePath); err != nil {
				log.Fatalf("telegram: %v", err)
			}
		}
		log.Print("chat bound")
	}

	client := polymarket.NewClient()
	client.Workers = 6

	poll := func() error {
		return runPoll(ctx, client, tg, state, *statePath, *minUSD, *limit, *dryRun)
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

func runPoll(ctx context.Context, api *polymarket.Client, tg *telegram.Client, state *alert.State, statePath string, minUSD float64, limit int, dryRun bool) error {
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

	plan, err := alert.BuildPlan(ctx, api, state, acts, minUSD)
	if err != nil {
		return err
	}
	state.CommitDropped(plan)
	defer func() {
		if err := alert.SaveState(statePath, state); err != nil {
			log.Printf("save state: %v", err)
		}
	}()

	if plan.FirstRun {
		log.Printf("seeded %d fills; waiting for new non-sports trades", len(state.Seen))
		return nil
	}
	if len(plan.Alerts) == 0 {
		return nil
	}

	sent := 0
	for _, a := range plan.Alerts {
		text := alert.Format(a)
		if dryRun {
			fmt.Println(text)
			fmt.Println("---")
			state.CommitSent(a)
			sent++
			continue
		}
		if err := tg.SendMessage(ctx, state.ChatID, text); err != nil {
			log.Printf("send %s: %v", a.Name, err)
			continue
		}
		state.CommitSent(a)
		sent++
	}
	log.Printf("sent %d/%d alerts", sent, len(plan.Alerts))
	return nil
}

func waitForChat(ctx context.Context, tg *telegram.Client, state *alert.State, statePath string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		updates, err := tg.GetUpdates(ctx, state.TelegramOffset, 25)
		if err != nil {
			log.Printf("getUpdates: %v", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			state.TelegramOffset = u.UpdateID + 1
			if u.Message == nil || u.Message.Chat.ID == 0 {
				continue
			}
			state.ChatID = u.Message.Chat.ID
			hello := fmt.Sprintf("Watching %d wallets. I'll ping you on non-sports trades.", len(sharps.Tracked))
			if err := tg.SendMessage(ctx, state.ChatID, hello); err != nil {
				log.Printf("welcome: %v", err)
			}
			if err := alert.SaveState(statePath, state); err != nil {
				return err
			}
			return nil
		}
	}
}
