package alerts

import (
	"context"
	"os"
	"testing"
)

func TestRaiseWithoutSettingsDeliversNothing(t *testing.T) {
	useTempStore(t)
	if err := Raise(context.Background(), obs("disk")); err != nil {
		t.Fatal(err)
	}
	n, err := LoadNotifications()
	if err != nil || n.wants(SeverityError) {
		t.Fatalf("settings = %+v, %v", n, err)
	}
}

func TestSaveNotificationsKeepsPassword(t *testing.T) {
	useTempStore(t)
	base := NotificationSettings{MinSeverity: SeverityError, SMTPHost: "mail.example.com", SMTPUsername: "nas", SMTPPassword: "secret",
		EmailFrom: "nas@example.com", EmailTo: []string{"me@example.com"}}
	saved, err := SaveNotifications(base)
	if err != nil || saved.SMTPPassword != "secret" || saved.SMTPPort != 587 {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	base.SMTPPassword = "" // a save from the UI never carries it back
	if _, err = SaveNotifications(base); err != nil {
		t.Fatal(err)
	}
	if n, _ := LoadNotifications(); n.SMTPPassword != "secret" {
		t.Fatalf("password not kept: %+v", n)
	}
	base.SMTPUsername = ""
	if _, err = SaveNotifications(base); err != nil {
		t.Fatal(err)
	}
	if n, _ := LoadNotifications(); n.SMTPPassword != "" {
		t.Fatalf("password kept without a username: %+v", n)
	}
	info, err := os.Stat(notificationsPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v", info.Mode(), err)
	}
}

func TestSaveNotificationsValidates(t *testing.T) {
	useTempStore(t)
	email := NotificationSettings{SMTPHost: "mail.example.com", EmailFrom: "nas@example.com", EmailTo: []string{"me@example.com"}}
	for name, n := range map[string]NotificationSettings{
		"severity":  {MinSeverity: "loud"},
		"webhook":   {WebhookURL: "ftp://x"},
		"no host":   {WebhookURL: "https://"},
		"smtp host": {SMTPHost: "a b", EmailFrom: "x@y.z", EmailTo: []string{"x@y.z"}},
		"port":      {SMTPHost: "h", SMTPPort: 70000, EmailFrom: "x@y.z", EmailTo: []string{"x@y.z"}},
		"from":      {SMTPHost: "h", EmailFrom: "nobody", EmailTo: []string{"x@y.z"}},
		"to":        {SMTPHost: "h", EmailFrom: "x@y.z", EmailTo: []string{"x\r\nBcc: evil@y.z"}},
		"no to":     {SMTPHost: "h", EmailFrom: "x@y.z"},
	} {
		if _, err := SaveNotifications(n); err == nil {
			t.Errorf("%s: accepted %+v", name, n)
		}
	}
	if _, err := SaveNotifications(email); err != nil {
		t.Fatalf("valid email settings: %v", err)
	}
}
