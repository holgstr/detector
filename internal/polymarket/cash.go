package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// pUSD is Polymarket's collateral token on Polygon. The profile "Cash"
// figure is this token's balance on the proxy wallet (6 decimals).
const pUSDAddress = "0xC011a7E12a19f7B1f670d46F03B03f3342E82DFB"

const balanceOfSelector = "70a08231"

func defaultPolygonRPCs() []string {
	return []string{
		"https://polygon-bor-rpc.publicnode.com",
		"https://1rpc.io/matic",
	}
}

func (c *Client) polygonRPCs() []string {
	if c != nil && len(c.PolygonRPCs) > 0 {
		out := make([]string, 0, len(c.PolygonRPCs))
		for _, u := range c.PolygonRPCs {
			u = strings.TrimRight(strings.TrimSpace(u), "/")
			if u != "" {
				out = append(out, u)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return defaultPolygonRPCs()
}

// FetchCash reads the pUSD balance of a Polymarket proxy wallet.
func (c *Client) FetchCash(ctx context.Context, address string) (float64, error) {
	addr, ok := ParseWalletAddress(address)
	if !ok {
		return 0, fmt.Errorf("invalid wallet %q", address)
	}
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_call",
		"params": []any{
			map[string]string{
				"to":   pUSDAddress,
				"data": balanceOfData(addr),
			},
			"latest",
		},
	})
	if err != nil {
		return 0, err
	}

	var last error
	for _, endpoint := range c.polygonRPCs() {
		var resp struct {
			Result string `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := c.postJSON(ctx, endpoint, payload, &resp); err != nil {
			last = err
			continue
		}
		if resp.Error != nil && strings.TrimSpace(resp.Error.Message) != "" {
			last = fmt.Errorf("%s: %s", endpoint, resp.Error.Message)
			continue
		}
		usd, err := parseTokenAmount(resp.Result, 6)
		if err != nil {
			last = fmt.Errorf("%s: %w", endpoint, err)
			continue
		}
		return usd, nil
	}
	if last == nil {
		last = fmt.Errorf("no polygon rpc")
	}
	return 0, last
}

func balanceOfData(addr string) string {
	addr = strings.TrimPrefix(strings.ToLower(addr), "0x")
	return "0x" + balanceOfSelector + strings.Repeat("0", 64-len(addr)) + addr
}

func parseTokenAmount(hex string, decimals int) (float64, error) {
	hex = strings.TrimSpace(hex)
	hex = strings.TrimPrefix(hex, "0x")
	hex = strings.TrimPrefix(hex, "0X")
	if hex == "" {
		return 0, fmt.Errorf("empty token balance")
	}
	raw, ok := new(big.Int).SetString(hex, 16)
	if !ok {
		return 0, fmt.Errorf("bad token balance %q", hex)
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	rat := new(big.Rat).SetFrac(raw, scale)
	v, _ := rat.Float64()
	return v, nil
}
