package glance

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"time"
)

var steamGamesWidgetTemplate = mustParseTemplate("steam-games.html", "widget-base.html")

type steamGamesWidget struct {
	widgetBase    `yaml:",inline"`
	List          string      `yaml:"list"`
	Region        string      `yaml:"region"`
	Language      string      `yaml:"language"`
	Limit         int         `yaml:"limit"`
	CollapseAfter int         `yaml:"collapse-after"`
	Games         []steamGame `yaml:"-"`
}

type steamGame struct {
	Name            string
	StoreURL        string
	ImageURL        string
	Price           string
	OriginalPrice   string
	DiscountPercent int
}

func (widget *steamGamesWidget) initialize() error {
	widget.withTitle("Steam").withCacheDuration(time.Hour)

	if widget.List == "" {
		widget.List = "specials"
	}

	if widget.List != "specials" && widget.List != "coming_soon" {
		return errors.New("list must be either 'specials' or 'coming_soon'")
	}

	if widget.Region == "" {
		widget.Region = "vn"
	}

	if widget.Language == "" {
		widget.Language = "vietnamese"
	}

	if widget.Limit <= 0 {
		widget.Limit = 10
	}

	if widget.CollapseAfter == 0 || widget.CollapseAfter < -1 {
		widget.CollapseAfter = 5
	}

	return nil
}

func (widget *steamGamesWidget) update(_ context.Context) {
	games, err := fetchSteamGames(widget.List, widget.Region, widget.Language)

	if !widget.canContinueUpdateAfterHandlingErr(err) {
		return
	}

	if len(games) > widget.Limit {
		games = games[:widget.Limit]
	}

	widget.Games = games
}

func (widget *steamGamesWidget) Render() template.HTML {
	return widget.renderTemplate(widget, steamGamesWidgetTemplate)
}

type steamFeaturedCategoriesJson struct {
	Specials struct {
		Items []steamFeaturedGameJson `json:"items"`
	} `json:"specials"`
	ComingSoon struct {
		Items []steamFeaturedGameJson `json:"items"`
	} `json:"coming_soon"`
}

type steamFeaturedGameJson struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	DiscountPercent int    `json:"discount_percent"`
	OriginalPrice   *int   `json:"original_price"`
	FinalPrice      int    `json:"final_price"`
	SmallCapsule    string `json:"small_capsule_image"`
	HeaderImage     string `json:"header_image"`
}

func fetchSteamGames(list, region, language string) ([]steamGame, error) {
	requestUrl := fmt.Sprintf(
		"https://store.steampowered.com/api/featuredcategories?cc=%s&l=%s",
		url.QueryEscape(region),
		url.QueryEscape(language),
	)

	request, _ := http.NewRequest("GET", requestUrl, nil)
	request.Header.Set("User-Agent", marketTickerUserAgent)
	request.Header.Set("Accept", "application/json")

	responseJson, err := decodeJsonFromRequest[steamFeaturedCategoriesJson](defaultHTTPClient, request)
	if err != nil {
		return nil, fmt.Errorf("fetching Steam featured games: %v", err)
	}

	items := responseJson.Specials.Items
	if list == "coming_soon" {
		items = responseJson.ComingSoon.Items
	}

	games := make([]steamGame, 0, len(items))

	for _, item := range items {
		if item.ID == 0 || item.Name == "" {
			continue
		}

		imageUrl := item.SmallCapsule
		if imageUrl == "" {
			imageUrl = item.HeaderImage
		}

		game := steamGame{
			Name:            item.Name,
			StoreURL:        fmt.Sprintf("https://store.steampowered.com/app/%d", item.ID),
			ImageURL:        imageUrl,
			Price:           formatSteamPrice(&item.FinalPrice),
			DiscountPercent: item.DiscountPercent,
		}

		if item.OriginalPrice != nil && *item.OriginalPrice != item.FinalPrice {
			game.OriginalPrice = formatSteamPrice(item.OriginalPrice)
		}

		games = append(games, game)
	}

	if len(games) == 0 {
		return nil, errors.New("no games found")
	}

	return games, nil
}

func formatSteamPrice(cents *int) string {
	if cents == nil || *cents == 0 {
		return "Chưa có giá"
	}

	return formatVietnameseNumber(float64(*cents)/100, 0) + " ₫"
}
