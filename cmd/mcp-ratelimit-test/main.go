package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"
)

type toolCallRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int64          `json:"id"`
	Method  string         `json:"method"`
	Params  toolCallParams `json:"params"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type toolResponse struct {
	Result *toolResult `json:"result"`
	Error  *toolError  `json:"error"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

var requestID atomic.Int64

func callMCP(client *http.Client, mcpURL string) (status string, duration time.Duration, body string) {
	start := time.Now()

	payload := toolCallRequest{
		JSONRPC: "2.0",
		ID:      requestID.Add(1),
		Method:  "tools/call",
		Params: toolCallParams{
			Name:      "get_dashboards",
			Arguments: map[string]any{},
		},
	}

	data, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, mcpURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Sprintf("ERR: %v", err), time.Since(start), ""
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("ERR: %v", err), time.Since(start), ""
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	elapsed := time.Since(start)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("HTTP %d", resp.StatusCode), elapsed, string(rawBody)
	}

	var rpcResp toolResponse
	if err := json.Unmarshal(rawBody, &rpcResp); err != nil {
		return fmt.Sprintf("PARSE_ERR"), elapsed, string(rawBody)
	}

	if rpcResp.Error != nil {
		return fmt.Sprintf("RPC_ERR: %s", rpcResp.Error.Message), elapsed, ""
	}

	if rpcResp.Result != nil && rpcResp.Result.IsError {
		var text strings.Builder
		for _, c := range rpcResp.Result.Content {
			text.WriteString(c.Text)
		}
		return "RATE_LIMITED", elapsed, text.String()
	}

	return "OK", elapsed, ""
}

func main() {
	_ = godotenv.Load(".env", ".env.local")

	mcpURL := os.Getenv("UTMIFY_MCP_URL")
	if mcpURL == "" {
		fmt.Println("ERRO: UTMIFY_MCP_URL não está definida.")
		fmt.Println("Defina via variável de ambiente ou no .env")
		os.Exit(1)
	}

	fmt.Println("==========================================")
	fmt.Println("  MCP Rate Limit Tester")
	fmt.Println("==========================================")
	fmt.Printf("URL: %s\n", mcpURL)
	fmt.Printf("Tool: get_dashboards\n")
	fmt.Printf("Início: %s\n\n", time.Now().Format("15:04:05"))

	client := &http.Client{Timeout: 30 * time.Second}

	// Fase 1: Requests sequenciais rápidos (sem delay)
	fmt.Println("--- FASE 1: Requests sequenciais (sem delay) ---")
	fmt.Println("Enviando requests até receber rate limit...\n")

	successCount := 0
	rateLimitHit := false

	for i := 1; i <= 60; i++ {
		status, duration, body := callMCP(client, mcpURL)

		emoji := "✓"
		if status != "OK" {
			emoji = "✗"
		}

		fmt.Printf("  #%02d  %s  %-15s  %6dms", i, emoji, status, duration.Milliseconds())
		if body != "" && status != "OK" {
			// Truncate body for display
			if len(body) > 80 {
				body = body[:80] + "..."
			}
			fmt.Printf("  → %s", body)
		}
		fmt.Println()

		if status == "OK" {
			successCount++
		}

		if status == "RATE_LIMITED" || strings.Contains(status, "Rate limit") || strings.Contains(body, "Rate limit") {
			rateLimitHit = true
			fmt.Printf("\n⚠ RATE LIMIT atingido após %d requests com sucesso!\n", successCount)
			fmt.Printf("  Request #%d retornou rate limit\n", i)

			// Agora testa quanto tempo leva pra liberar
			fmt.Println("\n--- FASE 2: Aguardando rate limit liberar ---")
			fmt.Println("Testando a cada 5 segundos...\n")

			for j := 1; j <= 24; j++ { // até 2 minutos
				time.Sleep(5 * time.Second)
				elapsed := time.Duration(j*5) * time.Second
				s, d, _ := callMCP(client, mcpURL)

				fmt.Printf("  +%3ds  %-15s  %6dms\n", int(elapsed.Seconds()), s, d.Milliseconds())

				if s == "OK" {
					fmt.Printf("\n✓ Rate limit liberou após ~%d segundos!\n", int(elapsed.Seconds()))
					break
				}
			}
			break
		}

		if strings.HasPrefix(status, "ERR") || strings.HasPrefix(status, "HTTP") {
			fmt.Printf("\n✗ Erro não esperado no request #%d: %s\n", i, status)
			if body != "" {
				fmt.Printf("  Body: %s\n", body)
			}
			break
		}
	}

	if !rateLimitHit {
		fmt.Printf("\n✓ Nenhum rate limit encontrado após %d requests sequenciais!\n", successCount)
	}

	fmt.Printf("\n==========================================\n")
	fmt.Printf("  Resumo\n")
	fmt.Printf("==========================================\n")
	fmt.Printf("  Requests com sucesso antes do limit: %d\n", successCount)
	fmt.Printf("  Rate limit atingido: %v\n", rateLimitHit)
	fmt.Printf("  Fim: %s\n", time.Now().Format("15:04:05"))
}
