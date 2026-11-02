package glance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"
)

var lolScheduleWidgetTemplate = mustParseTemplate("lol-schedule.html", "widget-base.html")

var lolScheduleHTTPClient = &http.Client{
	Transport: defaultHTTPClient.Transport,
	Timeout:   20 * time.Second,
}

type lolScheduleWidget struct {
	widgetBase    `yaml:",inline"`
	Events        []lolScheduleEvent `yaml:"-"`
	Limit         int                `yaml:"limit"`
	CollapseAfter int                `yaml:"collapse-after"`
}

type lolScheduleEvent struct {
	Teams      string
	Tournament string
	Time       string
}

func (widget *lolScheduleWidget) initialize() error {
	widget.withTitle("Lịch thi đấu LOL").
		withTitleURL("https://lolesports.com/schedule").
		withCacheDuration(30 * time.Minute)

	if widget.Limit <= 0 {
		widget.Limit = 8
	}

	if widget.CollapseAfter == 0 || widget.CollapseAfter < -1 {
		widget.CollapseAfter = 5
	}

	return nil
}

func (widget *lolScheduleWidget) update(_ context.Context) {
	events, err := fetchLolScheduleEvents()

	if !widget.canContinueUpdateAfterHandlingErr(err) {
		return
	}

	if len(events) > widget.Limit {
		events = events[:widget.Limit]
	}

	widget.Events = events
}

func (widget *lolScheduleWidget) Render() template.HTML {
	return widget.renderTemplate(widget, lolScheduleWidgetTemplate)
}

type lolEsportsEventJson struct {
	BlockName string    `json:"blockName"`
	StartTime time.Time `json:"startTime"`
	State     string    `json:"state"`
	League    struct {
		Name string `json:"name"`
	} `json:"league"`
	Tournament struct {
		Name string `json:"name"`
	} `json:"tournament"`
	MatchTeams []struct {
		Name string `json:"name"`
		Code string `json:"code"`
	} `json:"matchTeams"`
}

func fetchLolScheduleEvents() ([]lolScheduleEvent, error) {
	request, _ := http.NewRequest("GET", "https://lolesports.com/schedule", nil)
	request.Header.Set("User-Agent", marketTickerUserAgent)
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Accept-Language", "vi-VN,vi;q=0.9,en;q=0.8")

	response, err := lolScheduleHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetching lolesports schedule: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d from lolesports", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("reading lolesports schedule: %v", err)
	}

	rawEvents, ok := extractFirstJsonArray(string(body), `"events":[`)
	if !ok {
		return nil, errors.New("could not find schedule data in lolesports page")
	}

	var parsedEvents []lolEsportsEventJson
	if err := json.Unmarshal(rawEvents, &parsedEvents); err != nil {
		return nil, fmt.Errorf("parsing lolesports schedule: %v", err)
	}

	location, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		location = time.FixedZone("ICT", 7*60*60)
	}

	events := make([]lolScheduleEvent, 0, len(parsedEvents))

	for _, event := range parsedEvents {
		if event.State != "unstarted" {
			continue
		}

		teams := make([]string, 0, len(event.MatchTeams))

		for _, team := range event.MatchTeams {
			if team.Code != "" {
				teams = append(teams, team.Code)
			} else {
				teams = append(teams, team.Name)
			}
		}

		if len(teams) == 0 {
			continue
		}

		league := strings.TrimSpace(event.League.Name)
		tournament := strings.TrimSpace(event.Tournament.Name)
		blockName := strings.TrimSpace(event.BlockName)

		tournamentLabel := strings.TrimSpace(league + " " + tournament)
		if blockName != "" {
			if tournamentLabel != "" {
				tournamentLabel += " · "
			}
			tournamentLabel += blockName
		}

		events = append(events, lolScheduleEvent{
			Teams:      strings.Join(teams, " vs "),
			Tournament: tournamentLabel,
			Time:       event.StartTime.In(location).Format("02/01 · 15:04"),
		})
	}

	if len(events) == 0 {
		return nil, errors.New("no upcoming matches found")
	}

	return events, nil
}

// extractFirstJsonArray finds marker (which must end with '[') and returns the
// balanced JSON array that follows it. It is used to pull the server-rendered
// Apollo data out of the lolesports HTML shell.
func extractFirstJsonArray(contents, marker string) ([]byte, bool) {
	markerIndex := strings.Index(contents, marker)
	if markerIndex == -1 {
		return nil, false
	}

	start := markerIndex + len(marker) - 1
	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(contents); i++ {
		char := contents[i]

		if inString {
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}

			continue
		}

		switch char {
		case '"':
			inString = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return []byte(contents[start : i+1]), true
			}
		}
	}

	return nil, false
}
