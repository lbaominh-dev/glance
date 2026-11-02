package glance

import (
	"encoding/json"
	"testing"
)

func TestVocabularyCategoryForLevel(t *testing.T) {
	tests := map[string]string{
		"":      "cet4",
		"b1":    "cet4",
		"B1-B2": "cet4",
		"cet4":  "cet4",
		"b2-c1": "cet6",
		"cet6":  "cet6",
		"ielts": "ielts",
		"IELTS": "ielts",
		"toefl": "toefl",
		"gre":   "gre",
		"all":   "all",
	}

	for level, want := range tests {
		got, err := vocabularyCategoryForLevel(level)
		if err != nil {
			t.Errorf("vocabularyCategoryForLevel(%q) returned error: %v", level, err)
			continue
		}

		if got != want {
			t.Errorf("vocabularyCategoryForLevel(%q) = %q, want %q", level, got, want)
		}
	}

	if _, err := vocabularyCategoryForLevel("c2"); err == nil {
		t.Error("vocabularyCategoryForLevel(\"c2\") should return an error")
	}
}

func TestVocabularyWordJsonToVocabularyWord(t *testing.T) {
	data := `{
		"words": [
			{
				"word": " establish ",
				"phonetic": "/i'stæbliʃ/",
				"definition": "v. set up or found v. set up or lay the groundwork for",
				"examples": [{"text": "Our ultimate goal is to establish world peace."}],
				"dictionary": {
					"phonetics": {
						"uk": {"text": "ɪˈstæblɪʃ"},
						"us": {"text": "ɪˈstæblɪʃ"}
					},
					"english_definitions": [
						{
							"part_of_speech": "v.",
							"definition": "set up or found",
							"examples": ["establish a new department"]
						}
					],
					"examples": [{"source": "Ukraine indicated that it would establish its own army."}]
				}
			},
			{
				"word": "proposal",
				"phonetic": "/prəˈpəʊzəl/",
				"definition": "n. something proposed n. the act of making a proposal",
				"dictionary": {
					"english_definitions": []
				}
			}
		]
	}`

	var response vocabularyResponseJson
	if err := json.Unmarshal([]byte(data), &response); err != nil {
		t.Fatalf("could not unmarshal test data: %v", err)
	}

	if len(response.Words) != 2 {
		t.Fatalf("expected 2 words, got %d", len(response.Words))
	}

	establish := response.Words[0].toVocabularyWord()
	if establish.Word != "establish" {
		t.Errorf("word = %q, want %q", establish.Word, "establish")
	}
	if establish.Definition != "set up or found" {
		t.Errorf("definition = %q, want %q", establish.Definition, "set up or found")
	}
	if establish.PartOfSpeech != "v." {
		t.Errorf("part of speech = %q, want %q", establish.PartOfSpeech, "v.")
	}
	if establish.Phonetic != "ɪˈstæblɪʃ" {
		t.Errorf("phonetic = %q, want %q", establish.Phonetic, "ɪˈstæblɪʃ")
	}
	if establish.Example != "Ukraine indicated that it would establish its own army." {
		t.Errorf("example = %q", establish.Example)
	}

	proposal := response.Words[1].toVocabularyWord()
	if proposal.Definition != "something proposed" {
		t.Errorf("fallback definition = %q, want %q", proposal.Definition, "something proposed")
	}
	if proposal.Phonetic != "/prəˈpəʊzəl/" {
		t.Errorf("fallback phonetic = %q, want %q", proposal.Phonetic, "/prəˈpəʊzəl/")
	}
}

func TestAttachVocabularyNavigation(t *testing.T) {
	words := make([]vocabularyWord, 3)
	attachVocabularyNavigation(words)

	wantPrevious := []int{2, 0, 1}
	wantNext := []int{1, 2, 0}

	for index, word := range words {
		if word.Number != index+1 {
			t.Errorf("word %d number = %d, want %d", index, word.Number, index+1)
		}
		if word.PrevIndex != wantPrevious[index] {
			t.Errorf("word %d previous = %d, want %d", index, word.PrevIndex, wantPrevious[index])
		}
		if word.NextIndex != wantNext[index] {
			t.Errorf("word %d next = %d, want %d", index, word.NextIndex, wantNext[index])
		}
	}

	single := make([]vocabularyWord, 1)
	attachVocabularyNavigation(single)
	if single[0].Number != 1 {
		t.Errorf("single word number = %d, want 1", single[0].Number)
	}
}

func TestCleanVocabularyDefinition(t *testing.T) {
	tests := map[string]string{
		"v. set up or found v. set up or lay the groundwork for": "set up or found",
		"n. something proposed n. the act of making a proposal":  "something proposed",
		"adj. very old adv. in an old way":                       "very old",
		"a simple definition":                                    "a simple definition",
	}

	for input, want := range tests {
		if got := cleanVocabularyDefinition(input); got != want {
			t.Errorf("cleanVocabularyDefinition(%q) = %q, want %q", input, got, want)
		}
	}
}
