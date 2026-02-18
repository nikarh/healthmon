package store

import "time"

type Container struct {
	ID          int64
	Name        string
	ContainerID string
	Image       string
	ImageTag    string
	ImageID     string
	CreatedAt   time.Time
	FirstSeenAt time.Time
	Status      string
	Caps        []string
	ReadOnly    bool
	User        string
	LastEventID int64
	UpdatedAt   time.Time
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
}
