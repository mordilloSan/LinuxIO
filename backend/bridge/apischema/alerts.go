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
// is write-only: an empty value keeps the stored one. Levels maps a severity
// (info, warning, error) to where its alerts go, and Report a kind of work to
// what it reports besides failures. A sent map decides alone: a level or kind
// it leaves out takes its default. Left out, as by an older page, the levels
// follow MinSeverity, and Report keeps the stored kinds except Docker updates,
// which follow DockerUpdates.
type NotificationSettings struct {
	DockerUpdates          DockerUpdateNotify              `json:"dockerUpdates"`
	DockerUpdatesAvailable bool                            `json:"dockerUpdatesAvailable"`
	EmailFrom              string                          `json:"emailFrom"`
	EmailTo                []string                        `json:"emailTo"`
	Levels                 map[string]NotificationChannels `json:"levels,omitzero"`
	MinSeverity            string                          `json:"minSeverity"`
	Report                 map[string]string               `json:"report,omitzero"`
	SMTPHost               string                          `json:"smtpHost"`
	SMTPPassword           string                          `json:"smtpPassword,omitempty" secret:"true"`
	SMTPPort               int                             `json:"smtpPort"`
	SMTPUsername           string                          `json:"smtpUsername"`
	WebhookURL             string                          `json:"webhookUrl"`
}

// DockerUpdateNotify is what scheduled Docker updates report: never,
// on_failure or on_change.
type DockerUpdateNotify string

// NotificationChannels says where the alerts of one level go.
type NotificationChannels struct {
	Browser bool `json:"browser"`
	Email   bool `json:"email"`
	Webhook bool `json:"webhook"`
}

// NotificationSettingsView is the get/set_notifications response. It never
// carries the SMTP password, only whether one is stored. Levels and Report
// name every level and kind of work. SettingsUnreadable says the stored file
// does not parse: the rest is then the defaults, and a save replaces the file.
type NotificationSettingsView struct {
	DockerUpdates          DockerUpdateNotify              `json:"dockerUpdates"`
	DockerUpdatesAvailable bool                            `json:"dockerUpdatesAvailable"`
	EmailFrom              string                          `json:"emailFrom"`
	EmailTo                []string                        `json:"emailTo"`
	Levels                 map[string]NotificationChannels `json:"levels"`
	MinSeverity            string                          `json:"minSeverity"`
	Report                 map[string]string               `json:"report"`
	SettingsUnreadable     bool                            `json:"settingsUnreadable,omitzero"`
	SMTPHost               string                          `json:"smtpHost"`
	SMTPPasswordSet        bool                            `json:"smtpPasswordSet"`
	SMTPPort               int                             `json:"smtpPort"`
	SMTPUsername           string                          `json:"smtpUsername"`
	WebhookURL             string                          `json:"webhookUrl"`
}
