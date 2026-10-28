package apischema

type Alert struct {
	FirstOccurrence string `json:"firstOccurrence"`
	ID              string `json:"id"`
	LastOccurrence  string `json:"lastOccurrence"`
	Link            string `json:"link,omitempty"`
	Message         string `json:"message,omitempty"`
	OccurrenceCount int    `json:"occurrenceCount"`
	Seen            bool   `json:"seen"`
	Severity        string `json:"severity"`
	Source          string `json:"source"`
	Title           string `json:"title"`
}

type AlertList struct {
	Alerts []Alert `json:"alerts"`
	Unseen int     `json:"unseen"`
}

type AlertDismissRequest struct {
	ID string `json:"id"`
}

type AlertIDsRequest struct {
	IDs []string `json:"ids"`
}

// NotificationSettings is the alerts.set_notifications request. SMTPPassword
// is write-only: an empty value keeps the stored one.
type NotificationSettings struct {
	EmailFrom    string   `json:"emailFrom"`
	EmailTo      []string `json:"emailTo"`
	MinSeverity  string   `json:"minSeverity"`
	SMTPHost     string   `json:"smtpHost"`
	SMTPPassword string   `json:"smtpPassword,omitempty" secret:"true"`
	SMTPPort     int      `json:"smtpPort"`
	SMTPUsername string   `json:"smtpUsername"`
	WebhookURL   string   `json:"webhookUrl"`
}

// NotificationSettingsView is the get/set_notifications response. It never
// carries the SMTP password, only whether one is stored.
type NotificationSettingsView struct {
	EmailFrom       string   `json:"emailFrom"`
	EmailTo         []string `json:"emailTo"`
	MinSeverity     string   `json:"minSeverity"`
	SMTPHost        string   `json:"smtpHost"`
	SMTPPasswordSet bool     `json:"smtpPasswordSet"`
	SMTPPort        int      `json:"smtpPort"`
	SMTPUsername    string   `json:"smtpUsername"`
	WebhookURL      string   `json:"webhookUrl"`
}
