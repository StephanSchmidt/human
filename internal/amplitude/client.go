package amplitude

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/apiclient"
)

// Client is an Amplitude API client.
type Client struct {
	api *apiclient.Client
}

// New creates an Amplitude client with the given base URL, API key, and secret key.
func New(baseURL, apiKey, secretKey string) *Client {
	return &Client{
		api: apiclient.New(baseURL,
			apiclient.WithAuth(apiclient.BasicAuth(apiKey, secretKey)),
			apiclient.WithHeader("Accept", "application/json"),
			apiclient.WithProviderName("amplitude"),
		),
	}
}

// SetHTTPDoer replaces the HTTP client used for API requests.
func (c *Client) SetHTTPDoer(doer apiclient.HTTPDoer) {
	c.api.SetHTTPDoer(doer)
}

// ListEvents fetches all event types with WAU counts.
func (c *Client) ListEvents(ctx context.Context) ([]EventType, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/2/events/list", "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var r eventsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding events list response")
	}

	events := make([]EventType, len(r.Data))
	for i, e := range r.Data {
		events[i] = EventType{Name: e.Name, TotalUsers: e.Totals}
	}
	return events, nil
}

// QuerySegmentation runs a segmentation query for an event type.
func (c *Client) QuerySegmentation(ctx context.Context, eventType, start, end, metric, interval string) (*SegmentationResult, error) {
	q := url.Values{}
	q.Set("e", buildEventJSON(eventType))
	q.Set("start", start)
	q.Set("end", end)
	if metric != "" {
		q.Set("m", metric)
	}
	if interval != "" {
		q.Set("i", interval)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/2/events/segmentation", q.Encode())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var r segmentationResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding segmentation response",
			"eventType", eventType)
	}

	result := &SegmentationResult{
		EventType: eventType,
		Dates:     r.Data.XValues,
	}
	if len(r.Data.Series) > 0 {
		result.Values = r.Data.Series[0]
	}
	return result, nil
}

// ListTaxonomyEvents fetches the event type schema.
func (c *Client) ListTaxonomyEvents(ctx context.Context) ([]TaxonomyEvent, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/2/taxonomy/event", "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var r taxonomyResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding taxonomy events response")
	}

	events := make([]TaxonomyEvent, len(r.Data))
	for i, e := range r.Data {
		events[i] = TaxonomyEvent{
			Name:        e.EventType,
			Category:    e.Category,
			Description: e.Description,
		}
	}
	return events, nil
}

// ListTaxonomyUserProperties fetches the user property schema.
func (c *Client) ListTaxonomyUserProperties(ctx context.Context) ([]TaxonomyUserProperty, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/2/taxonomy/user-property", "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var r taxonomyUserPropResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding taxonomy user properties response")
	}

	props := make([]TaxonomyUserProperty, len(r.Data))
	for i, p := range r.Data {
		props[i] = TaxonomyUserProperty{
			Name:        p.UserProperty,
			Description: p.Description,
			Type:        p.Type,
		}
	}
	return props, nil
}

// QueryFunnel runs a funnel analysis for a sequence of events.
func (c *Client) QueryFunnel(ctx context.Context, events []string, start, end string) (*FunnelResult, error) {
	q := url.Values{}
	q.Set("e", buildFunnelEventsJSON(events))
	q.Set("start", start)
	q.Set("end", end)

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/2/funnels", q.Encode())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var r funnelResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding funnel response")
	}

	result := &FunnelResult{
		Steps: make([]FunnelStep, len(r.Data.Steps)),
	}
	for i, s := range r.Data.Steps {
		result.Steps[i] = FunnelStep{
			Name:          s.EventName,
			Count:         s.StepCount,
			ConversionPct: s.ConversionPct,
		}
	}
	return result, nil
}

// QueryRetention runs a retention analysis.
func (c *Client) QueryRetention(ctx context.Context, startEvent, returnEvent, start, end string) (*RetentionResult, error) {
	q := url.Values{}
	q.Set("se", buildEventJSON(startEvent))
	q.Set("re", buildEventJSON(returnEvent))
	q.Set("start", start)
	q.Set("end", end)

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/2/retention", q.Encode())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var r retentionResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding retention response",
			"startEvent", startEvent, "returnEvent", returnEvent)
	}

	result := &RetentionResult{
		StartEvent:  startEvent,
		ReturnEvent: returnEvent,
		Days:        make([]RetentionDay, len(r.Data.Counts)),
	}
	for i, c := range r.Data.Counts {
		result.Days[i] = RetentionDay(c)
	}
	return result, nil
}

// SearchUsers searches for users by query string.
func (c *Client) SearchUsers(ctx context.Context, query string) ([]UserMatch, error) {
	q := url.Values{}
	q.Set("user", query)

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/2/usersearch", q.Encode())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var r userSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding user search response",
			"query", query)
	}

	matches := make([]UserMatch, len(r.Matches))
	for i, m := range r.Matches {
		matches[i] = UserMatch(m)
	}
	return matches, nil
}

// GetUserActivity fetches a user's event history by Amplitude ID.
func (c *Client) GetUserActivity(ctx context.Context, amplitudeID string) (*UserActivity, error) {
	q := url.Values{}
	q.Set("user", amplitudeID)

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/2/useractivity", q.Encode())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var r userActivityResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding user activity response",
			"amplitudeID", amplitudeID)
	}

	activity := &UserActivity{
		AmplitudeID: r.UserData.AmplitudeID,
		Events:      make([]ActivityEvent, len(r.Events)),
	}
	for i, e := range r.Events {
		activity.Events[i] = ActivityEvent{
			Type:       e.EventType,
			Time:       e.EventTime,
			Properties: e.EventProperties,
		}
	}
	return activity, nil
}

// ListCohorts fetches all behavioral cohorts.
func (c *Client) ListCohorts(ctx context.Context) ([]Cohort, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/3/cohorts", "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var r cohortsResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding cohorts response")
	}

	cohorts := make([]Cohort, len(r.Cohorts))
	for i, c := range r.Cohorts {
		cohorts[i] = Cohort(c)
	}
	return cohorts, nil
}

// doRequest performs an authenticated HTTP request to the Amplitude API.
func (c *Client) doRequest(ctx context.Context, method, path, rawQuery string) (*http.Response, error) {
	return c.api.Do(ctx, method, path, rawQuery, nil)
}

// buildEventJSON returns a JSON-encoded event type object for query parameters.
func buildEventJSON(eventType string) string {
	return fmt.Sprintf(`{"event_type":"%s"}`, eventType)
}

// buildFunnelEventsJSON returns a JSON array of event type objects for funnel query parameters.
func buildFunnelEventsJSON(events []string) string {
	result := "["
	for i, e := range events {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf(`{"event_type":"%s"}`, e)
	}
	result += "]"
	return result
}
