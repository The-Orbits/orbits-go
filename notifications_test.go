package orbits

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The methods added for the bell, the alert webhook and the terminal ticket send what the API reads.
func TestNotificationsWebhookAndTicketRequests(t *testing.T) {
	type seen struct {
		method, path string
		body         map[string]any
	}
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		got = append(got, seen{r.Method, r.URL.RequestURI(), body})
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/teams/acme/notifications":
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_, _ = io.WriteString(w, `{"items":[{"id":"ntf_1","type":"alert.fired","title":"Error rate","detail":null,"link":null,"project":null,"read":false,"created_at":"2026-11-10T09:00:00Z"}],"next_cursor":null,"unread_count":1}`)
		case "/v1/teams/acme/alert-channels/webhook":
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_, _ = io.WriteString(w, `{"webhook":{"url":"https://example.com/hook","secret_last4":"ab12","updated_at":"2026-11-10T09:00:00Z"},"secret":"whsec_1"}`)
		case "/v1/teams/acme/alert-channels/webhook/test":
			_, _ = io.WriteString(w, `{"delivered":true,"status":200,"error":null}`)
		case "/v1/teams/acme/projects/web/sandboxes/dev/terminal-ticket":
			_, _ = io.WriteString(w, `{"ticket":"sbt_abc.def"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	ctx := context.Background()

	var ids []string
	for n, err := range c.Teams.AllNotifications(ctx, "acme", &ListNotificationsParams{Unread: Ptr(true)}) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	if len(ids) != 1 || ids[0] != "ntf_1" || got[0].path != "/v1/teams/acme/notifications?unread=true" {
		t.Fatalf("notifications: %v %+v", ids, got[0])
	}
	if err := c.Teams.MarkNotificationsRead(ctx, "acme", NotificationsReadRequest{All: Ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if last := got[len(got)-1]; last.method != http.MethodPost || last.body["all"] != true || last.body["ids"] != nil {
		t.Fatalf("mark read: %+v", last)
	}

	saved, err := c.Alerts.SetWebhook(ctx, "acme", AlertWebhookSet{URL: "https://example.com/hook"})
	if err != nil || saved.Secret == nil || *saved.Secret != "whsec_1" {
		t.Fatalf("set webhook: %+v %v", saved, err)
	}
	if last := got[len(got)-1]; last.method != http.MethodPut || last.body["url"] != "https://example.com/hook" {
		t.Fatalf("set webhook sent %+v", last)
	}
	res, err := c.Alerts.TestWebhook(ctx, "acme")
	if err != nil || !res.Delivered {
		t.Fatalf("test webhook: %+v %v", res, err)
	}
	if err := c.Alerts.DeleteWebhook(ctx, "acme"); err != nil || got[len(got)-1].method != http.MethodDelete {
		t.Fatalf("delete webhook: %v %+v", err, got[len(got)-1])
	}

	ticket, err := c.Sandboxes.TerminalTicket(ctx, "acme", "web", "dev")
	if err != nil || ticket.Ticket != "sbt_abc.def" || got[len(got)-1].method != http.MethodPost {
		t.Fatalf("terminal ticket: %+v %v", ticket, err)
	}
}
