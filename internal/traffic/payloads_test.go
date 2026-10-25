package traffic_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/traffic"
)

func TestPayloadStorageAndPurge(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	mgr := traffic.NewManager(d)

	// Insert parent traffic row
	res, err := d.Exec(`INSERT INTO traffic_logs (model, status_code, timestamp) VALUES ('gpt-4o', 200, datetime('now'))`)
	if err != nil {
		t.Fatalf("insert traffic: %v", err)
	}
	trafficID, _ := res.LastInsertId()

	reqBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	respBody := []byte(`{"choices":[{"message":{"role":"assistant","content":"world"}}]}`)

	// 1. Save payload
	err = mgr.SavePayload(trafficID, reqBody, respBody)
	if err != nil {
		t.Fatalf("SavePayload: %v", err)
	}

	// 2. Retrieve payload
	p, err := mgr.GetPayload(trafficID)
	if err != nil {
		t.Fatalf("GetPayload: %v", err)
	}
	if p.RequestBody != string(reqBody) {
		t.Errorf("req body mismatch, got: %s", p.RequestBody)
	}
	if p.ResponseBody != string(respBody) {
		t.Errorf("resp body mismatch, got: %s", p.ResponseBody)
	}

	// 3. Test 512KB clamping
	hugeBody := bytes.Repeat([]byte("A"), 600*1024) // 600KB
	err = mgr.SavePayload(trafficID, hugeBody, respBody)
	if err != nil {
		t.Fatalf("SavePayload huge: %v", err)
	}
	pClamped, err := mgr.GetPayload(trafficID)
	if err != nil {
		t.Fatalf("GetPayload clamped: %v", err)
	}
	if len(pClamped.RequestBody) > 512*1024 {
		t.Errorf("expected clamped payload <= 512KB, got %d bytes", len(pClamped.RequestBody))
	}

	// 4. Test Purge
	// Backdate payload creation
	_, _ = d.Exec(`UPDATE traffic_payloads SET created_at = datetime('now', '-8 days') WHERE traffic_id = ?`, trafficID)
	deleted, err := mgr.PurgeOldPayloads(7 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("PurgeOldPayloads: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted row, got %d", deleted)
	}

	_, err = mgr.GetPayload(trafficID)
	if err == nil {
		t.Errorf("expected error after purge, got nil")
	}
}

func TestSafeJSONClamping(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	mgr := traffic.NewManager(d)

	res, err := d.Exec(`INSERT INTO traffic_logs (model, status_code, timestamp) VALUES ('gpt-4o', 200, datetime('now'))`)
	if err != nil {
		t.Fatalf("insert traffic: %v", err)
	}
	trafficID, _ := res.LastInsertId()

	t.Run("Clamp JSON with many messages preserves first and last", func(t *testing.T) {
		// Construct request with 20 messages exceeding 512KB
		var msgs []map[string]string
		msgs = append(msgs, map[string]string{"role": "user", "content": "FIRST_MESSAGE_CONTENT"})
		for i := 1; i <= 18; i++ {
			msgs = append(msgs, map[string]string{
				"role":    "user",
				"content": string(bytes.Repeat([]byte("X"), 35*1024)), // 35KB each -> ~630KB
			})
		}
		msgs = append(msgs, map[string]string{"role": "user", "content": "LAST_MESSAGE_CONTENT"})

		bodyObj := map[string]interface{}{
			"model":    "gpt-4o",
			"messages": msgs,
		}
		hugeJSON, err := json.Marshal(bodyObj)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		if len(hugeJSON) <= 512*1024 {
			t.Fatalf("expected test payload > 512KB, got %d", len(hugeJSON))
		}

		err = mgr.SavePayload(trafficID, hugeJSON, []byte(`{}`))
		if err != nil {
			t.Fatalf("SavePayload: %v", err)
		}

		p, err := mgr.GetPayload(trafficID)
		if err != nil {
			t.Fatalf("GetPayload: %v", err)
		}

		if len(p.RequestBody) > 512*1024 {
			t.Errorf("expected clamped payload <= 512KB, got %d bytes", len(p.RequestBody))
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(p.RequestBody), &parsed); err != nil {
			t.Fatalf("clamped payload is not valid JSON: %v", err)
		}

		if parsed["_truncated"] != true {
			t.Errorf("expected _truncated: true, got %v", parsed["_truncated"])
		}
		if origBytes, ok := parsed["_original_bytes"].(float64); !ok || int(origBytes) != len(hugeJSON) {
			t.Errorf("expected _original_bytes = %d, got %v", len(hugeJSON), parsed["_original_bytes"])
		}

		parsedMsgs, ok := parsed["messages"].([]interface{})
		if !ok || len(parsedMsgs) != 3 {
			t.Fatalf("expected 3 messages (first, marker, last), got %d", len(parsedMsgs))
		}

		firstMsg := parsedMsgs[0].(map[string]interface{})
		if firstMsg["content"] != "FIRST_MESSAGE_CONTENT" {
			t.Errorf("expected first message preserved, got %v", firstMsg["content"])
		}

		marker := parsedMsgs[1].(map[string]interface{})
		if marker["role"] != "system" {
			t.Errorf("expected marker role 'system', got %v", marker["role"])
		}
		markerContent := marker["content"].(string)
		if !strings.Contains(markerContent, "18 messages truncated by NineGuard") {
			t.Errorf("expected marker content with count, got %s", markerContent)
		}

		lastMsg := parsedMsgs[2].(map[string]interface{})
		if lastMsg["content"] != "LAST_MESSAGE_CONTENT" {
			t.Errorf("expected last message preserved, got %v", lastMsg["content"])
		}
	})

	t.Run("Clamp oversized JSON without messages uses wrapper object", func(t *testing.T) {
		bodyObj := map[string]interface{}{
			"prompt": string(bytes.Repeat([]byte("B"), 600*1024)),
		}
		hugeJSON, _ := json.Marshal(bodyObj)

		err = mgr.SavePayload(trafficID, hugeJSON, []byte(`{}`))
		if err != nil {
			t.Fatalf("SavePayload: %v", err)
		}

		p, err := mgr.GetPayload(trafficID)
		if err != nil {
			t.Fatalf("GetPayload: %v", err)
		}

		if len(p.RequestBody) > 512*1024 {
			t.Errorf("expected clamped payload <= 512KB, got %d bytes", len(p.RequestBody))
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(p.RequestBody), &parsed); err != nil {
			t.Fatalf("clamped payload is not valid JSON: %v", err)
		}

		if parsed["_truncated"] != true {
			t.Errorf("expected _truncated: true")
		}
		if _, ok := parsed["_preview"].(string); !ok {
			t.Errorf("expected _preview string")
		}
	})
}
