package jobs

import (
	"errors"
	"strings"
	"time"
)

type SubmitRequest struct {
	RequestID string
	Name      string
}

type Job struct {
	ID        string
	RequestID string
	Name      string
	Status    string
	CreatedAt time.Time
}

func ValidateSubmit(request SubmitRequest) error {
	if strings.TrimSpace(request.RequestID) == "" {
		return errors.New("request ID is required")
	}
	if strings.TrimSpace(request.Name) == "" {
		return errors.New("job name is required")
	}
	return nil
}
