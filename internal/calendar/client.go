package calendar

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/krystophny/tabura/internal/providerdata"
	"golang.org/x/oauth2/google"
	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

const calendarScope = "https://www.googleapis.com/auth/calendar.readonly"

// Client provides access to Google Calendar API using Application Default Credentials.
type Client struct {
	service *gcal.Service
}

// New creates a new Calendar client using Application Default Credentials.
func New(ctx context.Context) (*Client, error) {
	creds, err := google.FindDefaultCredentials(ctx, calendarScope)
	if err != nil {
		return nil, fmt.Errorf("failed to find default credentials: %w", err)
	}

	service, err := gcal.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to create calendar service: %w", err)
	}

	return &Client{service: service}, nil
}

// ListCalendars returns all calendars accessible to the user.
func (c *Client) ListCalendars(ctx context.Context) ([]providerdata.Calendar, error) {
	result, err := c.service.CalendarList.List().Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list calendars: %w", err)
	}

	calendars := make([]providerdata.Calendar, 0, len(result.Items))
	for _, cal := range result.Items {
		calendars = append(calendars, providerdata.Calendar{
			ID:          cal.Id,
			Name:        cal.Summary,
			Description: cal.Description,
			TimeZone:    cal.TimeZone,
			Primary:     cal.Primary,
		})
	}

	return calendars, nil
}

// GetEventsOptions configures the GetEvents call.
type GetEventsOptions struct {
	CalendarID string
	TimeMin    time.Time
	TimeMax    time.Time
	MaxResults int64
	Query      string
}

// GetEvents retrieves events from a specific calendar.
func (c *Client) GetEvents(ctx context.Context, opts GetEventsOptions) ([]providerdata.Event, error) {
	if opts.CalendarID == "" {
		opts.CalendarID = "primary"
	}
	if opts.TimeMin.IsZero() {
		opts.TimeMin = time.Now()
	}
	if opts.TimeMax.IsZero() {
		opts.TimeMax = opts.TimeMin.Add(30 * 24 * time.Hour)
	}
	if opts.MaxResults == 0 {
		opts.MaxResults = 100
	}

	call := c.service.Events.List(opts.CalendarID).
		Context(ctx).
		TimeMin(opts.TimeMin.Format(time.RFC3339)).
		TimeMax(opts.TimeMax.Format(time.RFC3339)).
		MaxResults(opts.MaxResults).
		SingleEvents(true).
		OrderBy("startTime")

	if opts.Query != "" {
		call = call.Q(opts.Query)
	}

	result, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	events := make([]providerdata.Event, 0, len(result.Items))
	for _, item := range result.Items {
		event := providerdata.Event{
			ID:          item.Id,
			CalendarID:  opts.CalendarID,
			Summary:     item.Summary,
			Description: item.Description,
			Location:    item.Location,
			Status:      item.Status,
			Recurring:   item.RecurringEventId != "",
		}

		if item.Summary == "" {
			event.Summary = "(No title)"
		}

		if item.Organizer != nil {
			event.Organizer = item.Organizer.Email
		}

		for _, att := range item.Attendees {
			event.Attendees = append(event.Attendees, att.Email)
		}

		event.Start, event.AllDay = parseEventTime(item.Start)
		event.End, _ = parseEventTime(item.End)

		events = append(events, event)
	}

	return events, nil
}

// GetAllEvents retrieves events from multiple calendars.
func (c *Client) GetAllEvents(ctx context.Context, calendarIDs []string, timeMin, timeMax time.Time, maxResultsPerCalendar int64, query string) ([]providerdata.Event, error) {
	if len(calendarIDs) == 0 {
		calendars, err := c.ListCalendars(ctx)
		if err != nil {
			return nil, err
		}
		for _, cal := range calendars {
			calendarIDs = append(calendarIDs, cal.ID)
		}
	}

	var allEvents []providerdata.Event
	for _, calID := range calendarIDs {
		events, err := c.GetEvents(ctx, GetEventsOptions{
			CalendarID: calID,
			TimeMin:    timeMin,
			TimeMax:    timeMax,
			MaxResults: maxResultsPerCalendar,
			Query:      query,
		})
		if err != nil {
			continue // Skip calendars that fail
		}
		allEvents = append(allEvents, events...)
	}

	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].Start.Before(allEvents[j].Start)
	})

	return allEvents, nil
}

func parseEventTime(eventTime *gcal.EventDateTime) (time.Time, bool) {
	if eventTime == nil {
		return time.Time{}, false
	}

	if eventTime.DateTime != "" {
		t, err := time.Parse(time.RFC3339, eventTime.DateTime)
		if err == nil {
			return t, false
		}
	}

	if eventTime.Date != "" {
		t, err := time.Parse("2006-01-02", eventTime.Date)
		if err == nil {
			return t, true // All-day event
		}
	}

	return time.Time{}, false
}
