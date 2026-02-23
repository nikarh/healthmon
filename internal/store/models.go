package store

import "time"

type Container struct {
	ID                  int64
	Name                string
	ContainerID         string
	Image               string
	ImageTag            string
	ImageID             string
	CreatedAt           time.Time
	RegisteredAt        time.Time
	StartedAt           time.Time
	Status              string
	Role                string
	Caps                []string
	ReadOnly            bool
	NoNewPrivileges     bool
	MemoryReservation   int64
	MemoryLimit         int64
	User                string
	LastEventID         int64
	UpdatedAt           time.Time
	Present             bool
	HealthStatus        string
	HealthFailingStreak int
	UnhealthySince      time.Time
	RestartLoop         bool
	RestartStreak       int
	RestartLoopSince    time.Time
	Healthcheck         *Healthcheck
}

type Healthcheck struct {
	Test          []string `json:"test"`
	Interval      string   `json:"interval"`
	Timeout       string   `json:"timeout"`
	StartPeriod   string   `json:"start_period"`
	StartInterval string   `json:"start_interval"`
	Retries       int      `json:"retries"`
}

type Event struct {
	ID          int64
	ContainerPK int64
	Container   string
	ContainerID string
	Type        string
	Severity    string
	Message     string
	Timestamp   time.Time
	OldImage    string
	NewImage    string
	OldImageID  string
	NewImageID  string
	Reason      string
	DetailsJSON string
	ExitCode    *int
}

type Alert struct {
	ID          int64
	ContainerPK int64
	Container   string
	ContainerID string
	Type        string
	Severity    string
	Message     string
	Timestamp   time.Time
	OldImage    string
	NewImage    string
	OldImageID  string
	NewImageID  string
	Reason      string
	DetailsJSON string
	ExitCode    *int
}
