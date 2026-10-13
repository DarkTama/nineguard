package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"nineguard/internal/upstream"
)

// IsChatCommand checks if the latest user message in the conversation is an exact slash command.
func IsChatCommand(messages []chatMessage) (string, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			if text, ok := messages[i].Content.(string); ok {
				trimmed := strings.ToLower(strings.TrimSpace(text))
				switch trimmed {
				case "/quota", "/limits", "/usage":
					return trimmed, true
				}
			}
			break
		}
	}
	return "", false
}

func formatResetIn(t *time.Time) string {
	if t == nil {
		return "-"
	}
	diff := time.Until(*t)
	if diff <= 0 {
		return "now"
	}

	days := int(diff.Hours()) / 24
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("in %dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("in %dh %dm", hours, minutes)
	}
	return fmt.Sprintf("in %dm", minutes)
}

// FormatQuotaMarkdown builds a human-readable Markdown response from quota data.
func FormatQuotaMarkdown(resp *upstream.UnifiedQuotaResponse) string {
	var sb strings.Builder
	sb.WriteString("📊 **NineGuard Quota Report**\n\n")

	// 1. Client key budget
	if resp.KeyQuota != nil {
		kq := resp.KeyQuota
		warnIcon := ""
		if kq.Warning {
			warnIcon = "⚠️ "
		}
		sb.WriteString(fmt.Sprintf("**API Key Budget:** %s%s / %s tokens remaining (%s",
			warnIcon,
			formatTokenCount(kq.Remaining),
			formatTokenCount(kq.Limit),
			kq.Period,
		))
		if kq.ResetAt != nil {
			sb.WriteString(fmt.Sprintf(", resets %s", formatResetIn(kq.ResetAt)))
		}
		sb.WriteString(")\n\n")
	} else if len(resp.ModelQuotas) > 0 {
		sb.WriteString("**API Key Model Quotas:**\n\n")
		sb.WriteString("| Model | Limit | Used | Remaining | Period | Reset |\n")
		sb.WriteString("|---|---|---|---|---|---|\n")
		for _, mq := range resp.ModelQuotas {
			warnIcon := ""
			if mq.Warning {
				warnIcon = "⚠️ "
			}
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s%s | %s | %s |\n",
				mq.ModelID,
				formatTokenCount(mq.Limit),
				formatTokenCount(mq.Used),
				warnIcon,
				formatTokenCount(mq.Remaining),
				mq.Period,
				formatResetIn(mq.ResetAt),
			))
		}
		sb.WriteString("\n")
	}

	// 2. Upstream provider pools
	if len(resp.UpstreamPools) == 0 {
		sb.WriteString("*No active upstream quota trackers configured.*\n")
		return sb.String()
	}

	sb.WriteString("### Upstream Model Pools\n\n")
	sb.WriteString("| Provider / Family | Window | Used / Total | Remaining | Next Reset |\n")
	sb.WriteString("|---|---|---|---|---|\n")

	for _, pool := range resp.UpstreamPools {
		for _, b := range pool.Buckets {
			warnIcon := ""
			if b.Warning {
				warnIcon = "⚠️ "
			}

			usedTotal := fmt.Sprintf("%s / %s", formatTokenCount(b.Used), formatTokenCount(b.Total))
			if b.IsCreditBalance {
				usedTotal = fmt.Sprintf("$%.2f", float64(b.Remaining)/100.0)
			} else if b.Total <= 0 {
				usedTotal = "-"
			}

			remPct := fmt.Sprintf("%s%d%%", warnIcon, b.RemainingPercentage)
			if b.IsCreditBalance {
				remPct = "Active"
			}

			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
				pool.Provider,
				b.Name,
				usedTotal,
				remPct,
				formatResetIn(b.ResetAt),
			))
		}
	}

	return sb.String()
}

// WriteChatCommandResponse sends a synthetic chat completion matching OpenAI protocol.
func WriteChatCommandResponse(w http.ResponseWriter, command, markdown string, stream bool) {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	cmdID := "chatcmpl-ng-cmd-" + hex.EncodeToString(b)
	created := time.Now().Unix()

	if !stream {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		resp := map[string]interface{}{
			"id":      cmdID,
			"object":  "chat.completion",
			"created": created,
			"model":   "nineguard/system",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": markdown,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     0,
				"completion_tokens": 0,
				"total_tokens":      0,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Streaming SSE chunks
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	// Chunk 1: Content
	chunk1 := map[string]interface{}{
		"id":      cmdID,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   "nineguard/system",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"delta": map[string]interface{}{
					"role":    "assistant",
					"content": markdown,
				},
				"finish_reason": nil,
			},
		},
	}
	data1, _ := json.Marshal(chunk1)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data1)
	if canFlush {
		flusher.Flush()
	}

	// Chunk 2: Finish
	chunk2 := map[string]interface{}{
		"id":      cmdID,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   "nineguard/system",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": "stop",
			},
		},
	}
	data2, _ := json.Marshal(chunk2)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data2)
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}
}
