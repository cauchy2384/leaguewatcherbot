package leaguewatcher

import "time"

type Event struct {
	Date   time.Time `json:"date"`
	Action string    `json:"action"`
	User   string    `json:"user"`
}

func NewEvent(action, user string) Event {
	return Event{
		Date:   time.Now(),
		Action: action,
		User:   user,
	}
}
