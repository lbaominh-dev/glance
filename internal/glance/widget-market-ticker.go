package glance

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var marketTickerWidgetTemplate = mustParseTemplate("market-ticker.html")

const marketTickerUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

type marketTickerWidget struct {
	widgetBase `yaml:",inline"`
	Gold       bool               `yaml:"gold"`
	Indices    []string           `yaml:"indices"`
	Items      []marketTickerItem `yaml:"-"`
}

type marketTickerItem struct {
	Label          string
	Value          string
	Change         string
	ChangeClass    string
	Title          string
	SvgChartPoints string
}

func (widget *marketTickerWidget) initialize() error {
	widget.withCacheDuration(10 * time.Minute).withError(nil).setHideHeader(true)

	return nil
}

func (widget *marketTickerWidget) update(_ context.Context) {
	resultCount := len(widget.Indices)
	if widget.Gold {
		resultCount++
	}

	items := make([]marketTickerItem, resultCount)
	errs := make([]error, resultCount)

	var wg sync.WaitGroup

	if widget.Gold {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items[0], errs[0] = fetchGoldTickerItem()
		}()
	}

	for i, symbol := range widget.Indices {
		wg.Add(1)
		go func(i int, symbol string) {
			defer wg.Done()
			offset := i
			if widget.Gold {
				offset++
			}
			items[offset], errs[offset] = fetchMarketIndexTickerItem(symbol)
		}(i, symbol)
	}

	wg.Wait()

	widget.Items = items[:0]

	var firstErr error

	for i := range items {
		if errs[i] != nil {
			if firstErr == nil {
				firstErr = errs[i]
			}
			continue
		}

		widget.Items = append(widget.Items, items[i])
	}

	if len(widget.Items) == 0 {
		widget.withError(firstErr)
		return
	}

	widget.withError(nil)
}

func (widget *marketTickerWidget) Render() template.HTML {
	return widget.renderTemplate(widget, marketTickerWidgetTemplate)
}

type goldPricesResponseJson struct {
	Time   string `json:"time"`
	Date   string `json:"date"`
	Prices map[string]struct {
		Name       string  `json:"name"`
		Buy        float64 `json:"buy"`
		Sell       float64 `json:"sell"`
		ChangeBuy  float64 `json:"change_buy"`
		ChangeSell float64 `json:"change_sell"`
	} `json:"prices"`
}

func fetchGoldTickerItem() (marketTickerItem, error) {
	request, _ := http.NewRequest("GET", "https://www.vang.today/api/prices", nil)
	request.Header.Set("User-Agent", marketTickerUserAgent)
	request.Header.Set("Accept", "application/json")
	responseJson, err := decodeJsonFromRequest[goldPricesResponseJson](defaultHTTPClient, request)
	if err != nil {
		return marketTickerItem{}, fmt.Errorf("fetching gold prices: %v", err)
	}

	price, exists := responseJson.Prices["VNGSJC"]
	if !exists || price.Sell == 0 {
		return marketTickerItem{}, errors.New("SJC gold price not available")
	}

	return marketTickerItem{
		Label:          "Vàng SJC",
		Value:          formatCompactVND(price.Sell),
		Change:         formatSignedCompactVND(price.ChangeSell),
		ChangeClass:    tickerChangeClass(price.ChangeSell),
		SvgChartPoints: fetchGoldChartPoints(),
		Title: fmt.Sprintf("Mua %s · Bán %s · Cập nhật %s %s · Chart: giá vàng thế giới (GC=F)",
			formatCompactVND(price.Buy), formatCompactVND(price.Sell),
			responseJson.Time, responseJson.Date),
	}, nil
}

type ssiIndexResponseJson struct {
	Code string `json:"code"`
	Data struct {
		IndexID       string  `json:"indexId"`
		IndexValue    float64 `json:"indexValue"`
		Change        float64 `json:"change"`
		ChangePercent float64 `json:"changePercent"`
		History       []struct {
			IndexValue float64 `json:"indexValue"`
		} `json:"history"`
	} `json:"data"`
}

func fetchMarketIndexTickerItem(symbol string) (marketTickerItem, error) {
	requestUrl := fmt.Sprintf(
		"https://iboard-query.ssi.com.vn/exchange-index/%s?hasHistory=true",
		url.PathEscape(symbol),
	)
	request, _ := http.NewRequest("GET", requestUrl, nil)
	request.Header.Set("User-Agent", marketTickerUserAgent)
	request.Header.Set("Accept", "application/json")
	responseJson, err := decodeJsonFromRequest[ssiIndexResponseJson](defaultHTTPClient, request)
	if err != nil {
		return marketTickerItem{}, fmt.Errorf("fetching index %s: %v", symbol, err)
	}

	if responseJson.Data.IndexID == "" {
		return marketTickerItem{}, fmt.Errorf("no data for index %s", symbol)
	}

	label := marketIndexLabel(symbol)

	historyValues := make([]float64, 0, len(responseJson.Data.History))
	for _, point := range responseJson.Data.History {
		if point.IndexValue != 0 {
			historyValues = append(historyValues, point.IndexValue)
		}
	}

	return marketTickerItem{
		Label:          label,
		Value:          formatVietnameseNumber(responseJson.Data.IndexValue, 2),
		Change:         formatSignedVietnameseNumber(responseJson.Data.ChangePercent, 2) + "%",
		ChangeClass:    tickerChangeClass(responseJson.Data.Change),
		SvgChartPoints: tickerChartPoints(historyValues),
		Title: fmt.Sprintf("%s: %s · %s", label,
			formatVietnameseNumber(responseJson.Data.IndexValue, 2),
			formatIndexChange(responseJson.Data.Change, responseJson.Data.ChangePercent)),
	}, nil
}

var marketIndexLabels = map[string]string{
	"VNINDEX":       "VN-Index",
	"VN30":          "VN30",
	"HNXINDEX":      "HNX-Index",
	"HNX30":         "HNX30",
	"HNXUPCOMINDEX": "UPCOM",
	"UPCOMINDEX":    "UPCOM",
}

func marketIndexLabel(symbol string) string {
	if label, exists := marketIndexLabels[strings.ToUpper(symbol)]; exists {
		return label
	}

	return symbol
}

func fetchGoldChartPoints() string {
	request, _ := http.NewRequest("GET", "https://query1.finance.yahoo.com/v8/finance/chart/GC=F?range=1mo&interval=1d", nil)
	setBrowserUserAgentHeader(request)

	responseJson, err := decodeJsonFromRequest[marketResponseJson](defaultHTTPClient, request)
	if err != nil || len(responseJson.Chart.Result) == 0 {
		return ""
	}

	result := responseJson.Chart.Result[0]
	if len(result.Indicators.Quote) == 0 {
		return ""
	}

	prices := result.Indicators.Quote[0].Close
	if len(prices) > marketChartDays {
		prices = prices[len(prices)-marketChartDays:]
	}

	return tickerChartPoints(prices)
}

func tickerChartPoints(values []float64) string {
	values = maybeCopySliceWithoutZeroValues(values)
	if len(values) < 2 {
		return ""
	}

	minimum, maximum := values[0], values[0]

	for _, value := range values[1:] {
		minimum = math.Min(minimum, value)
		maximum = math.Max(maximum, value)
	}

	if minimum == maximum {
		return ""
	}

	return svgPolylineCoordsFromYValues(100, 50, values)
}

func tickerChangeClass(change float64) string {
	if change > 0 {
		return "color-positive"
	}

	if change < 0 {
		return "color-negative"
	}

	return "color-subdue"
}

func formatIndexChange(change, percent float64) string {
	return fmt.Sprintf("%s (%s%%)",
		formatSignedVietnameseNumber(change, 2),
		formatSignedVietnameseNumber(percent, 2),
	)
}

func formatCompactVND(value float64) string {
	return formatVietnameseNumber(value/1_000_000, 1) + " tr"
}

func formatSignedCompactVND(value float64) string {
	if value > 0 {
		return "+" + formatCompactVND(value)
	}

	return formatCompactVND(value)
}

func formatVietnameseNumber(value float64, decimals int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "—"
	}

	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}

	formatted := strconv.FormatFloat(value, 'f', decimals, 64)
	integerPart, fractionPart, hasFraction := strings.Cut(formatted, ".")

	var grouped strings.Builder

	for i, char := range integerPart {
		if i > 0 && (len(integerPart)-i)%3 == 0 {
			grouped.WriteByte('.')
		}
		grouped.WriteRune(char)
	}

	if hasFraction {
		grouped.WriteByte(',')
		grouped.WriteString(fractionPart)
	}

	return sign + grouped.String()
}

func formatSignedVietnameseNumber(value float64, decimals int) string {
	if value > 0 {
		return "+" + formatVietnameseNumber(value, decimals)
	}

	return formatVietnameseNumber(value, decimals)
}
