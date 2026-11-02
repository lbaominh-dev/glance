package glance

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var vocabularyWidgetTemplate = mustParseTemplate("vocabulary.html", "widget-base.html")

var vocabularyHTTPClient = &http.Client{
	Transport: defaultHTTPClient.Transport,
	Timeout:   15 * time.Second,
}

type vocabularyWidget struct {
	widgetBase `yaml:",inline"`
	Level      string           `yaml:"level"`
	Count      int              `yaml:"count"`
	Words      []vocabularyWord `yaml:"-"`
	category   string           `yaml:"-"`
}

type vocabularyWord struct {
	Word         string
	Phonetic     string
	PartOfSpeech string
	Definition   string
	Example      string
	Number       int
	PrevIndex    int
	NextIndex    int
}

func (widget *vocabularyWidget) initialize() error {
	widget.withTitle("Vocabulary").withCacheDuration(time.Hour)

	if widget.Count == 0 {
		widget.Count = 10
	}

	if widget.Count < 1 || widget.Count > 20 {
		return errors.New("count must be between 1 and 20")
	}

	category, err := vocabularyCategoryForLevel(widget.Level)
	if err != nil {
		return err
	}

	widget.category = category

	return nil
}

func (widget *vocabularyWidget) update(_ context.Context) {
	words, err := fetchVocabularyWords(widget.category, widget.Count)
	if !widget.canContinueUpdateAfterHandlingErr(err) {
		return
	}

	widget.Words = words
}

func (widget *vocabularyWidget) Render() template.HTML {
	return widget.renderTemplate(widget, vocabularyWidgetTemplate)
}

func vocabularyCategoryForLevel(level string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "b1", "b2", "b1-b2", "cet4":
		return "cet4", nil
	case "b2-c1", "cet6":
		return "cet6", nil
	case "ielts", "toefl", "gre", "all":
		return strings.ToLower(strings.TrimSpace(level)), nil
	default:
		return "", fmt.Errorf(
			"invalid level %q, expected one of: b1-b2, b2-c1, cet4, cet6, ielts, toefl, gre, all",
			level,
		)
	}
}

func fetchVocabularyWords(category string, count int) ([]vocabularyWord, error) {
	requestUrl := fmt.Sprintf(
		"https://uapis.cn/api/v1/daily/word?lang=en&category=%s&count=%d&phonetic=true&define=true",
		url.QueryEscape(category),
		count,
	)

	request, err := http.NewRequest(http.MethodGet, requestUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("creating vocabulary request: %w", err)
	}

	request.Header.Set("Accept", "application/json")

	response, err := decodeJsonFromRequest[vocabularyResponseJson](vocabularyHTTPClient, request)
	if err != nil {
		return nil, fmt.Errorf("fetching vocabulary words: %w", err)
	}

	words := make([]vocabularyWord, 0, len(response.Words))

	for _, item := range response.Words {
		word := item.toVocabularyWord()
		if word.Word == "" || word.Definition == "" {
			continue
		}

		words = append(words, word)

		if len(words) == count {
			break
		}
	}

	if len(words) == 0 {
		return nil, errors.New("the vocabulary API returned no usable words")
	}

	attachVocabularyNavigation(words)

	return words, nil
}

func attachVocabularyNavigation(words []vocabularyWord) {
	for index := range words {
		words[index].Number = index + 1

		if len(words) > 1 {
			words[index].PrevIndex = (index - 1 + len(words)) % len(words)
			words[index].NextIndex = (index + 1) % len(words)
		}
	}
}

type vocabularyResponseJson struct {
	Words []vocabularyWordJson `json:"words"`
}

type vocabularyWordJson struct {
	Word       string `json:"word"`
	Phonetic   string `json:"phonetic"`
	Definition string `json:"definition"`
	Examples   []struct {
		Text string `json:"text"`
	} `json:"examples"`
	Dictionary struct {
		Phonetics struct {
			UK struct {
				Text string `json:"text"`
			} `json:"uk"`
			US struct {
				Text string `json:"text"`
			} `json:"us"`
		} `json:"phonetics"`
		EnglishDefinitions []struct {
			PartOfSpeech string   `json:"part_of_speech"`
			Definition   string   `json:"definition"`
			Examples     []string `json:"examples"`
		} `json:"english_definitions"`
		Examples []struct {
			Source string `json:"source"`
		} `json:"examples"`
	} `json:"dictionary"`
}

func (item vocabularyWordJson) toVocabularyWord() vocabularyWord {
	word := vocabularyWord{
		Word:     strings.TrimSpace(item.Word),
		Phonetic: strings.TrimSpace(item.Dictionary.Phonetics.US.Text),
	}

	if word.Phonetic == "" {
		word.Phonetic = strings.TrimSpace(item.Dictionary.Phonetics.UK.Text)
	}

	if word.Phonetic == "" {
		word.Phonetic = strings.TrimSpace(item.Phonetic)
	}

	if len(item.Dictionary.EnglishDefinitions) > 0 {
		definition := item.Dictionary.EnglishDefinitions[0]
		word.Definition = strings.TrimSpace(definition.Definition)
		word.PartOfSpeech = strings.TrimSpace(definition.PartOfSpeech)

		if len(definition.Examples) > 0 {
			word.Example = strings.TrimSpace(definition.Examples[0])
		}
	}

	if word.Definition == "" {
		word.Definition = cleanVocabularyDefinition(item.Definition)
	}

	if len(item.Dictionary.Examples) > 0 {
		if example := strings.TrimSpace(item.Dictionary.Examples[0].Source); example != "" {
			word.Example = example
		}
	}

	if word.Example == "" && len(item.Examples) > 0 {
		word.Example = strings.TrimSpace(item.Examples[0].Text)
	}

	return word
}

func cleanVocabularyDefinition(definition string) string {
	definition = strings.TrimSpace(definition)

	// The top-level definition can contain several concatenated senses,
	// e.g. "v. set up or found v. set up or lay the groundwork for".
	// Keep only the first sense when the structured definitions are missing.
	for _, marker := range []string{" v. ", " n. ", " adj. ", " adv. "} {
		if index := strings.Index(definition, marker); index > 0 {
			definition = definition[:index]
			break
		}
	}

	for _, prefix := range []string{"v. ", "n. ", "adj. ", "adv. "} {
		definition = strings.TrimPrefix(definition, prefix)
	}

	return strings.TrimSpace(definition)
}
